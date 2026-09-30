// Carteira — porte fiel de templates/portal/v2/sections/wallet.php para React.
// Ligada ao go/portal (namespace /wp-json/senderzz/v1):
//   GET  /portal/wallet/summary      — KPIs (available/pending/analysis)
//   GET  /portal/wallet/history      — extrato de movimentação (?period=)
//   GET  /portal/wallet/future       — lançamentos futuros (?period=)
//   GET  /portal/wallet/withdrawals  — ordens de saque (produtor)
//   GET  /portal/wallet/accounts     — contas PIX (produtor)
//   POST /portal/wallet/withdraw     — solicitar saque COD (produtor + flag)
//   POST /portal/wallet/anticipate   — antecipar recebíveis pendentes
//
// Toda decisão de visibilidade/labels é dirigida pelo campo "scope" devolvido
// em cada leitura ('cod' | 'affiliate') — NUNCA pelo role do Layout (o Layout
// usa 'affiliate'/'client'; o backend chaveia pelo role cru do banco 'afiliado').
//
// DESVIOS FIÉIS (limitações do contrato/migração — documentados):
//   - Aba "Carteira de Expedição" (recarga PIX via admin-ajax generate_pix/check_pix)
//     NÃO faz parte do contrato go/portal — fica na seção Expedição. Sem a aba de
//     expedição, a carteira tem uma única visão (= o caso !has_exp do WP, sem abas).
//   - "Exportar CSV" (sz_cod_wallet_history_export_url) não tem rota — omitido.
//   - A flag senderzz_dashboard_v2_withdraw_enabled não é legível no front. Para
//     scope 'cod' os botões Saque/Antecipar ficam habilitados e o backend aplica
//     o guard da flag (403) — a mensagem PT-BR do backend é exibida via toast.
//     Para scope 'affiliate' os botões já nascem desabilitados (caso WP is_aff).
import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import EmptyState from '../components/EmptyState'
import InlineLoading from '../components/InlineLoading'
import AlertError from '../components/AlertError'
import Drawer from '../components/Drawer'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import { brl } from '../utils/format'

// ── Shapes (espelham go/portal/internal/handlers/wallet.go) ─────────────────────

type Scope = 'cod' | 'affiliate'

type Summary = { available: number; pending: number; analysis: number }
type SummaryResp = { ok: boolean; summary: Summary; scope: Scope }

type HistoryRow = {
  date: string // já formatado "dd/mm/aaaa hh:mm"
  description: string
  order: string // "#123" | "—"
  movement: string // "Disponível" | "Pendente"
  type?: string // tipo cru da tx ('withdrawal' etc); ausente no ledger afiliado
  value: number // bruto
  fee: number
  net: number // líquido recebido (no saque = bruto - taxa)
  status: string
}
type HistoryResp = { ok: boolean; data: HistoryRow[]; total: number; scope: Scope }

type FutureRow = {
  date: string // já formatado "dd/mm/aaaa"
  description: string
  order: string
  commission: number
  release_at: string
}
type FutureResp = { ok: boolean; data: FutureRow[]; total: number; scope: Scope }

type WithdrawalRow = {
  id: number
  created_at: string // já formatado "dd/mm/aaaa hh:mm"
  amount: number
  fee: number
  net: number
  holder_name: string
  pix_key: string
  pix_type: string
  status: 'analysis' | 'pending' | 'approved' | 'paid' | 'rejected' | 'cancelled' | string
  proof_url: string | null
}
type WithdrawalsResp = { ok: boolean; data: WithdrawalRow[]; total: number; scope: Scope }

type Account = {
  id: number
  holder_name: string
  holder_cpf: string
  pix_type: string
  pix_key: string
  is_default: boolean
}
type AccountsResp = { ok: boolean; accounts: Account[]; scope: Scope }

// COD (produtor): {net, wd_id}. Afiliado (/portal/affiliate-wallet/withdraw):
// {net_amount, withdrawal_id}. Os campos do afiliado são opcionais para o mesmo
// tipo cobrir ambos os endpoints — wdSubmit lê `resp.net ?? resp.net_amount`.
type WithdrawResp = {
  ok: boolean
  success: boolean
  message: string
  fee: number
  net: number
  wd_id: number
  net_amount?: number
  withdrawal_id?: number
}

// ── Períodos (espelham os botões do WP) ─────────────────────────────────────────

const HIST_PERIODS: Array<[string, string]> = [
  ['Hoje', 'hoje'],
  ['Ontem', 'ontem'],
  ['7d', '7d'],
  ['30d', '30d'],
  ['Mês', 'mes'],
]
const FUT_PERIODS: Array<[string, string]> = [
  ['Hoje', 'hoje'],
  ['Amanhã', 'amanha'],
  ['7d', '7d'],
  ['15d', '15d'],
  ['30d', '30d'],
]

// Mapa de status de saque → variante de badge + label (espelha wallet.php).
// portal-ui não tem variante 'completed' → 'paid' usa 'success'.
const WD_STATUS_MAP: Record<string, [string, string]> = {
  analysis: ['warning', 'Em análise'],
  pending: ['warning', 'Pendente'],
  approved: ['success', 'Aprovado'],
  paid: ['success', 'Concluído'],
  rejected: ['neutral', 'Recusado'],
  cancelled: ['neutral', 'Cancelado'],
}

// Formata moeda local para o modal (espelha szFmtMoney do WP: "R$ 1.234,56").
function fmtMoney(v: number): string {
  return brl(v)
}

// Texto compacto da conta PIX (espelha label do select no WP).
function accountLabel(a: Account): string {
  return `${a.holder_name || 'Conta'} · PIX ${(a.pix_type || '').toUpperCase()}: ${a.pix_key || ''}`
}

// Aplica regra pt-BR no input de valor (aceita "1.234,56").
function parseMoneyInput(s: string): number {
  s = s.trim()
  if (!s) return 0
  if (s.includes(',')) s = s.replace(/\./g, '').replace(',', '.')
  const n = parseFloat(s)
  return Number.isFinite(n) ? n : 0
}

// Datas em ISO "YYYY-MM-DD" para os FalkDatePicker (mirror dos
// defaults do WP: histórico -7d→hoje, futuros hoje→+30d).
function isoDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}
function isoAddDays(days: number): string {
  const d = new Date()
  d.setDate(d.getDate() + days)
  return isoDate(d)
}

// O backend devolve SOMENTE strings já formatadas — HistoryRow.date é
// "dd/mm/aaaa hh:mm" e FutureRow.date/release_at é "dd/mm/aaaa". Não há ISO/raw
// timestamp; logo o filtro de intervalo personalizado parseia o "dd/mm/aaaa"
// manualmente (split em "/"), nunca new Date(str). Devolve "YYYY-MM-DD" ou ''
// se a string não tiver data reconhecível.
function brDateToIso(s: string): string {
  const m = /(\d{2})\/(\d{2})\/(\d{4})/.exec(s || '')
  if (!m) return ''
  return `${m[3]}-${m[2]}-${m[1]}`
}

// Mantém apenas linhas cuja data (campo `date` formatado pt-BR) caia no
// intervalo [from,to] inclusivo. from/to em ISO "YYYY-MM-DD". Linhas sem data
// reconhecível são mantidas (não somem por falta de parse).
function filterByDateRange<T extends { date: string }>(rows: T[], from: string, to: string): T[] {
  if (!from && !to) return rows
  return rows.filter(r => {
    const iso = brDateToIso(r.date)
    if (!iso) return true
    if (from && iso < from) return false
    if (to && iso > to) return false
    return true
  })
}

const TXT_MUTED = 'var(--szv2-text-muted)'

export default function Wallet() {
  const toast = useToast()

  const [scope, setScope] = useState<Scope>('cod')
  const [summary, setSummary] = useState<Summary>({ available: 0, pending: 0, analysis: 0 })
  const [loadingSummary, setLoadingSummary] = useState(true)
  const [err, setErr] = useState('')

  // Carteira = SÓ COD (decisão do dono 2026-06-24): a Carteira de Expedição saiu das
  // abas e virou MENU próprio "Recarga de frete" (gated a produtor habilitado, ver
  // Layout). walletTab fica const 'cod' p/ o wrapper de painel seguir válido.
  const [walletTab] = useState<'cod' | 'exp'>('cod')

  // Sub-aba ativa: Transações | Saques.
  const [codTab, setCodTab] = useState<'transacoes' | 'saques'>('transacoes')

  // Histórico de movimentação.
  // histPeriod='' significa "modo intervalo personalizado" (datas from/to ativas
  // e nenhum pill marcado) — espelha o WP, onde mexer nas datas desativa os pills.
  const [history, setHistory] = useState<HistoryRow[]>([])
  const [histPeriod, setHistPeriod] = useState('7d')
  const [histFrom, setHistFrom] = useState(() => isoAddDays(-7))
  const [histTo, setHistTo] = useState(() => isoDate(new Date()))
  const [loadingHist, setLoadingHist] = useState(true)

  // Lançamentos futuros.
  const [future, setFuture] = useState<FutureRow[]>([])
  const [futureTotal, setFutureTotal] = useState(0)
  const [futPeriod, setFutPeriod] = useState('hoje')
  const [futFrom, setFutFrom] = useState(() => isoDate(new Date()))
  const [futTo, setFutTo] = useState(() => isoAddDays(30))
  const [loadingFuture, setLoadingFuture] = useState(true)

  // Ordens de saque.
  const [withdrawals, setWithdrawals] = useState<WithdrawalRow[]>([])
  const [loadingWd, setLoadingWd] = useState(true)

  // Contas PIX (carregadas sob demanda nos modais).
  const [accounts, setAccounts] = useState<Account[]>([])

  const isAffiliate = scope === 'affiliate'

  // ── Carregamentos ─────────────────────────────────────────────────────────

  const loadSummary = useCallback(() => {
    setLoadingSummary(true)
    setErr('')
    api<SummaryResp>('/portal/wallet/summary')
      .then(r => {
        setScope(r.scope)
        setSummary(r.summary || { available: 0, pending: 0, analysis: 0 })
      })
      .catch(e => setErr(e.message || 'Erro ao carregar carteira'))
      .finally(() => setLoadingSummary(false))
  }, [])

  // Carrega o histórico. Se `range` vier preenchido (modo intervalo
  // personalizado), busca period=all e filtra client-side por [from,to] — o
  // backend só aceita ?period= (não from/to nativos; ver backendReqs). Caso
  // contrário, busca pelo período do pill.
  const loadHistory = useCallback((period: string, range?: { from: string; to: string }) => {
    setLoadingHist(true)
    const q = range ? 'all' : period
    api<HistoryResp>(`/portal/wallet/history?period=${encodeURIComponent(q)}`)
      .then(r => {
        const rows = r.data || []
        setHistory(range ? filterByDateRange(rows, range.from, range.to) : rows)
        setScope(r.scope)
      })
      .catch(() => setHistory([]))
      .finally(() => setLoadingHist(false))
  }, [])

  const loadFuture = useCallback((period: string, range?: { from: string; to: string }) => {
    setLoadingFuture(true)
    const q = range ? 'all' : period
    api<FutureResp>(`/portal/wallet/future?period=${encodeURIComponent(q)}`)
      .then(r => {
        const rows = r.data || []
        const filtered = range ? filterByDateRange(rows, range.from, range.to) : rows
        setFuture(filtered)
        // Badge do cabeçalho: total das linhas exibidas (no modo range usa o
        // somatório filtrado; no modo pill usa o total devolvido pelo backend).
        if (range) {
          setFutureTotal(Math.round(filtered.reduce((s, ft) => s + (ft.commission || 0), 0) * 100) / 100)
        } else {
          setFutureTotal(r.total || 0)
        }
      })
      .catch(() => {
        setFuture([])
        setFutureTotal(0)
      })
      .finally(() => setLoadingFuture(false))
  }, [])

  const loadWithdrawals = useCallback(() => {
    setLoadingWd(true)
    api<WithdrawalsResp>('/portal/wallet/withdrawals')
      .then(r => setWithdrawals(r.data || []))
      .catch(() => setWithdrawals([]))
      .finally(() => setLoadingWd(false))
  }, [])

  const loadAccounts = useCallback(async (): Promise<Account[]> => {
    try {
      const r = await api<AccountsResp>('/portal/wallet/accounts')
      const accs = r.accounts || []
      setAccounts(accs)
      return accs
    } catch {
      setAccounts([])
      return []
    }
  }, [])

  useEffect(() => {
    loadSummary()
    loadHistory('7d')
    loadFuture('hoje')
    loadWithdrawals()
  }, [loadSummary, loadHistory, loadFuture, loadWithdrawals])


  function reloadAll() {
    loadSummary()
    // Reload respeita o modo ativo de cada card (pill vs intervalo personalizado).
    loadHistory(histPeriod, histPeriod === '' ? { from: histFrom, to: histTo } : undefined)
    loadFuture(futPeriod, futPeriod === '' ? { from: futFrom, to: futTo } : undefined)
    loadWithdrawals()
  }

  // Selecionar um pill de período desativa o modo de intervalo personalizado
  // (histPeriod/futPeriod != '') — espelha o WP, onde clicar no pill é o modo
  // padrão e mexer nas datas desativa os pills.
  function selectHistPeriod(p: string) {
    setHistPeriod(p)
    loadHistory(p)
  }
  function selectFutPeriod(p: string) {
    setFutPeriod(p)
    loadFuture(p)
  }

  // Alterar uma data limpa o pill ativo (histPeriod='') e refaz a busca em modo
  // intervalo personalizado (period=all + filtro client-side por [from,to]).
  function changeHistFrom(v: string) {
    setHistFrom(v)
    setHistPeriod('')
    loadHistory('', { from: v, to: histTo })
  }
  function changeHistTo(v: string) {
    setHistTo(v)
    setHistPeriod('')
    loadHistory('', { from: histFrom, to: v })
  }
  function changeFutFrom(v: string) {
    setFutFrom(v)
    setFutPeriod('')
    loadFuture('', { from: v, to: futTo })
  }
  function changeFutTo(v: string) {
    setFutTo(v)
    setFutPeriod('')
    loadFuture('', { from: futFrom, to: v })
  }

  // ── Modais (estado) ───────────────────────────────────────────────────────

  // Saque: 2 passos (form → confirmação).
  const [wdOpen, setWdOpen] = useState(false)
  const [wdStep, setWdStep] = useState<'form' | 'confirm'>('form')
  const [wdAmount, setWdAmount] = useState('')
  const [wdAccountId, setWdAccountId] = useState<number>(0)
  const [wdMsg, setWdMsg] = useState('')
  const [wdSubmitting, setWdSubmitting] = useState(false)

  // Antecipação: resumo + select de conta.
  // antTotal é a soma de TODOS os recebíveis pendentes (period=all) — espelha a
  // regra do backend (antecipa todos os pendentes), NÃO o total da tabela (que é
  // filtrado pelo pill de período e serve só ao badge do cabeçalho).
  const [antOpen, setAntOpen] = useState(false)
  const [antTotal, setAntTotal] = useState(0)
  const [antMsg, setAntMsg] = useState('')
  const [antSubmitting, setAntSubmitting] = useState(false)

  // Transferência p/ Carteira de Frete — só produtor (scope 'cod'). Não executa
  // na hora: cria pedido 'pending', admin aprova/rejeita em go/admin.
  const [ftOpen, setFtOpen] = useState(false)
  const [ftAmount, setFtAmount] = useState('')
  const [ftMsg, setFtMsg] = useState('')
  const [ftSubmitting, setFtSubmitting] = useState(false)

  function openFreightTransfer() {
    setFtAmount('')
    setFtMsg('')
    setFtOpen(true)
  }

  async function ftSubmit() {
    const amount = parseMoneyInput(ftAmount)
    if (amount < 10) {
      setFtMsg('Valor mínimo para transferência é R$ 10,00.')
      return
    }
    if (amount > summary.available) {
      setFtMsg('Valor indisponível para transferência.')
      return
    }
    setFtSubmitting(true)
    setFtMsg('')
    try {
      const resp = await api<WithdrawResp>('/portal/wallet/freight-transfer', {
        method: 'POST',
        body: JSON.stringify({ amount: ftAmount }),
      })
      setFtOpen(false)
      toast('ok', resp.message || 'Transferência solicitada. Aguarde a aprovação do admin.')
      reloadAll()
    } catch (e: any) {
      setFtMsg(e.message || 'Erro ao solicitar transferência. Tente novamente.')
    } finally {
      setFtSubmitting(false)
    }
  }

  const antFeePctDefault = 4.99 // taxa de antecipação global (informativa; backend recalcula)
  // Taxa de saque global informativa (#44 — rodapé de saque). Espelha o default
  // do backend (sz_cod_withdraw_fee = R$ 2,99 em senderzz_options, lido em
  // go/portal .../wallet.go withdrawFeeGlobal). É valor FIXO em R$ por saque.
  // TODO: nenhum GET expõe sz_cod_withdraw_fee / sz_cod_anticipation_fee_pct ao
  // front (fees.go é só doc). Quando existir /portal/wallet/config (ou o summary
  // passar a devolvê-las), substituir estes defaults pela leitura real.
  const withdrawFeeDefault = 2.99
  const antFee = Math.round(antTotal * (antFeePctDefault / 100) * 100) / 100
  const antNet = Math.round(Math.max(0, antTotal - antFee) * 100) / 100

  async function openWithdraw() {
    setWdMsg('')
    setWdAmount('')
    setWdStep('form')
    setWdOpen(true)
    const accs = await loadAccounts()
    setWdAccountId(accs.length > 0 ? accs[0].id : 0)
  }

  // #95 — TETO no "Valor a sacar": o valor não pode exceder o saldo disponível.
  // Clampa enquanto digita (limpa o input vazio sem forçar "0,00") — o backend
  // também valida amount > available (422), mas o teto evita o pedido inválido.
  function changeWdAmount(raw: string) {
    const trimmed = raw.trim()
    if (trimmed === '') {
      setWdAmount('')
      return
    }
    const parsed = parseMoneyInput(trimmed)
    if (parsed > summary.available) {
      // Excedeu o disponível: fixa no teto (saldo disponível) com 2 casas pt-BR.
      setWdAmount(summary.available.toFixed(2).replace('.', ','))
      return
    }
    setWdAmount(raw)
  }

  function wdNext() {
    // Afiliado sem saldo disponível: hoje toda a comissão está pendente (R$0
    // disponível). O saque exige disponível — avisa explicitamente em vez de
    // cair no "valor mínimo" (que descreve a causa errada).
    if (isAffiliate && summary.available <= 0) {
      setWdMsg('Você não tem saldo disponível para saque. Use Antecipar para liberar a comissão pendente.')
      return
    }
    const amount = parseMoneyInput(wdAmount)
    if (amount < 10) {
      setWdMsg('Valor mínimo para saque é R$ 10,00.')
      return
    }
    if (amount > summary.available) {
      setWdMsg('Valor indisponível para saque.')
      return
    }
    if (!wdAccountId) {
      setWdMsg('Selecione uma conta PIX para recebimento.')
      return
    }
    setWdMsg('')
    setWdStep('confirm')
  }

  async function wdSubmit() {
    setWdSubmitting(true)
    setWdMsg('')
    try {
      // Mesmo body ({account_id, amount}) para os dois endpoints — o handler do
      // afiliado (withdrawAffRequest) também aceita account_id + amount.
      const url = isAffiliate ? '/portal/affiliate-wallet/withdraw' : '/portal/wallet/withdraw'
      const resp = await api<WithdrawResp>(url, {
        method: 'POST',
        body: JSON.stringify({ account_id: wdAccountId, amount: wdAmount }),
      })
      // Campo do líquido difere por endpoint: COD devolve `net`, afiliado `net_amount`.
      const net = resp.net ?? resp.net_amount ?? 0
      setWdOpen(false)
      toast('ok', `${resp.message || 'Saque solicitado com sucesso.'} Líquido: ${brl(net)}.`)
      reloadAll()
    } catch (e: any) {
      setWdStep('form')
      setWdMsg(e.message || 'Erro ao solicitar saque. Tente novamente.')
    } finally {
      setWdSubmitting(false)
    }
  }

  async function openAnticipate() {
    // O total a antecipar é a soma de TODOS os recebíveis pendentes (period=all),
    // independente do pill de período da tabela — espelha o backend, que antecipa
    // todos os pendentes. Mirror WP: sem valores futuros → toast e não abre.
    // #94 — NÃO carrega contas PIX: a antecipação não pede conta de recebimento
    // (só credita o líquido no disponível). O saque para PIX é outro fluxo.
    const allTotal = await api<FutureResp>('/portal/wallet/future?period=all')
      .then(r => r.total || 0)
      .catch(() => 0)
    if (allTotal <= 0) {
      // Sem recebíveis pendentes: aviso claro, não abre formulário/drawer vazio.
      toast('info', 'Não há valores para antecipar.')
      return
    }
    setAntTotal(allTotal)
    setAntMsg('')
    setAntOpen(true)
  }

  async function antSubmit() {
    // #94 — antecipação NÃO pede conta PIX: só move o pendente → disponível,
    // descontando a taxa de antecipação (4,99%). O saque para PIX é outro fluxo.
    setAntSubmitting(true)
    setAntMsg('')
    try {
      // Afiliado antecipa TODA a comissão pendente (body vazio), igual ao COD.
      const url = isAffiliate ? '/portal/affiliate-wallet/anticipate' : '/portal/wallet/anticipate'
      const resp = await api<WithdrawResp>(url, {
        method: 'POST',
        body: JSON.stringify({}),
      })
      setAntOpen(false)
      toast('ok', resp.message || 'Antecipação aprovada!')
      reloadAll()
    } catch (e: any) {
      setAntMsg(e.message || 'Erro ao processar. Tente novamente.')
    } finally {
      setAntSubmitting(false)
    }
  }

  // ── Render ──────────────────────────────────────────────────────────────────

  const confAmount = parseMoneyInput(wdAmount)
  const confAccount = accounts.find(a => a.id === wdAccountId)
  // #95 — taxa de saque EXPLÍCITA. É valor FIXO em R$ por solicitação
  // (sz_cod_withdraw_fee, default R$ 2,99), NÃO um percentual — espelha o
  // backend: fee = min(amount, withdraw_fee); net = max(0, amount - fee).
  const wdFee = Math.min(withdrawFeeDefault, Math.max(0, confAmount))
  const wdNet = Math.round(Math.max(0, confAmount - wdFee) * 100) / 100

  return (
    <section id="sec-wallet" className="sz-sec">
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Carteira
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>Saldos, recebimentos e saques em um só lugar.</p>
      </div>

      {/* Carteira = SÓ COD. A Carteira de Expedição saiu daqui e virou menu próprio
          "Recarga de frete" (gated a produtor habilitado — ver Layout/App). */}
      {walletTab === 'cod' && (
      <>

      {!!err && <AlertError message={err} onRetry={loadSummary} />}

      {/* Sub-abas Transações / Saques + ações (Saque/Antecipar) na MESMA linha do
          header das abas — abas à esquerda, botões alinhados à direita. A classe
          .szv2-prod-subtabs traz width:100% (regra global) + border-bottom; aqui
          ela é sobrescrita para largura de conteúdo (flex:'0 1 auto') para que os
          botões caibam à direita na mesma row, e a row usa nowrap p/ não quebrar. */}
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          margin: 'var(--szv2-space-4) 0 var(--szv2-space-3)',
          gap: 8,
          flexWrap: 'nowrap',
        }}
      >
        <div className="szv2-prod-subtabs" role="tablist" style={{ marginBottom: 0, width: 'auto', flex: '0 1 auto' }}>
          <button
            type="button"
            className={`szv2-prod-subtab ${codTab === 'transacoes' ? 'szv2-prod-subtab--active' : ''}`}
            role="tab"
            aria-selected={codTab === 'transacoes'}
            onClick={() => setCodTab('transacoes')}
          >
            Transações
          </button>
          <button
            type="button"
            className={`szv2-prod-subtab ${codTab === 'saques' ? 'szv2-prod-subtab--active' : ''}`}
            role="tab"
            aria-selected={codTab === 'saques'}
            onClick={() => setCodTab('saques')}
          >
            Saques
          </button>
        </div>
        {/* Saque/Antecipar — PRODUTOR (scope 'cod', /portal/wallet/*) E AFILIADO
            (scope 'affiliate', /portal/affiliate-wallet/*). O backend novo do
            afiliado (affiliate_wallet_portal.go) saca/antecipa a própria comissão
            com taxa fixa (sz_aff_withdraw_fee) e % de antecipação (4,99%). Os modais
            são reusados — só os endpoints/título/teto mudam por scope. */}
        <div style={{ display: 'flex', gap: 8, flex: '0 0 auto', paddingBottom: 4 }}>
          <button type="button" className="szv2-btn szv2-btn-brand" onClick={openWithdraw}>
            Saque
          </button>
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={openAnticipate}>
            Antecipar
          </button>
          {/* Transferir p/ Carteira de Frete — SÓ produtor (scope 'cod'). Afiliado
              não tem carteira de frete/TPC. */}
          {!isAffiliate && (
            <button type="button" className="szv2-btn szv2-btn-secondary" onClick={openFreightTransfer}>
              Transferir p/ Frete
            </button>
          )}
        </div>
      </div>

      {/* KPIs por sub-aba (#46): Transações → Disponível + Pendente;
          Saques → Saque em análise (só produtor). Cada card aparece somente
          na aba a que pertence — nenhuma área vazia, sem duplicação. O wrapper
          .szv2-kpi-grid só renderiza quando há card (afiliado na aba Saques não
          tem KPI → evita div vazia com gap).
          Pedido do dono: os cards (KPIs) ficam ENTRE o submenu (Transações/Saques)
          e os históricos da parte inferior — por isso renderizam aqui, logo após as
          sub-abas e antes dos painéis de histórico/lançamentos. */}
      {codTab === 'transacoes' && (
        <div className="szv2-kpi-grid">
          <div className="szv2-card szv2-kpi">
            <span className="szv2-kpi-label">Saldo disponível</span>
            <span className="szv2-kpi-value szv2-num">{loadingSummary ? '…' : brl(summary.available)}</span>
            <span className="szv2-kpi-meta">{isAffiliate ? 'Comissão disponível' : 'Disponível para saque'}</span>
          </div>
          <div className="szv2-card szv2-kpi">
            <span className="szv2-kpi-label">Saldo pendente</span>
            <span className="szv2-kpi-value szv2-num">{loadingSummary ? '…' : brl(summary.pending)}</span>
            <span className="szv2-kpi-meta">Aguardando liberação</span>
          </div>
          {/* "Saque em análise" também aqui (não só na aba Saques): quando há saque
              em aprovação, o produtor vê pra onde o disponível foi (não sumiu). */}
          <div className="szv2-card szv2-kpi">
            <span className="szv2-kpi-label">Saque em análise</span>
            <span className="szv2-kpi-value szv2-num">{loadingSummary ? '…' : brl(summary.analysis)}</span>
            <span className="szv2-kpi-meta">Aguardando aprovação</span>
          </div>
        </div>
      )}
      {codTab === 'saques' && !isAffiliate && (
        <div className="szv2-kpi-grid">
          <div className="szv2-card szv2-kpi" style={{ maxWidth: 360 }}>
            <span className="szv2-kpi-label">Saque em análise</span>
            <span className="szv2-kpi-value szv2-num">{loadingSummary ? '…' : brl(summary.analysis)}</span>
            <span className="szv2-kpi-meta">Aguardando aprovação</span>
          </div>
        </div>
      )}

      {/* ── Painel: Transações ── */}
      {codTab === 'transacoes' && (
        <>
          <div className="szv2-dash-row" style={{ alignItems: 'flex-start' }}>
            {/* Histórico de movimentação */}
            <div className="szv2-card" style={{ flex: 1 }}>
              <div className="szv2-card-head">
                <div>
                  <h2>Histórico de movimentação</h2>
                  <p className="szv2-card-sub">Recebimentos passados confirmados.</p>
                </div>
              </div>
              {/* Filtros de período compactos + intervalo personalizado (espelha wallet.php).
                  Mexer numa data desativa os pills e busca por [from,to] (client-side). */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap', marginBottom: 12 }}>
                {HIST_PERIODS.map(([lbl, slug]) => (
                  <button
                    key={slug}
                    type="button"
                    className={`szv2-period-btn ${histPeriod === slug ? 'szv2-period-btn--active' : ''}`}
                    onClick={() => selectHistPeriod(slug)}
                  >
                    {lbl}
                  </button>
                ))}
                <FalkDatePicker
                  value={histFrom}
                  onChange={v => changeHistFrom(v)}
                  style={{ width: 138, marginLeft: 4 }}
                  aria-label="Data inicial do histórico"
                />
                <span style={{ fontSize: 12, color: TXT_MUTED }}>–</span>
                <FalkDatePicker
                  value={histTo}
                  onChange={v => changeHistTo(v)}
                  style={{ width: 138 }}
                  aria-label="Data final do histórico"
                />
              </div>
              <div className="szv2-table-wrap szv2-table-flush">
                {loadingHist ? (
                  <InlineLoading label="Carregando histórico…" />
                ) : history.length > 0 ? (
                  <table className="szv2-table">
                    <thead>
                      <tr>
                        <th>Data</th>
                        <th>Descrição</th>
                        <th>Pedido</th>
                        <th>Tipo</th>
                        <th className="szv2-td-num">Valor</th>
                        <th className="szv2-td-num">Taxa</th>
                        <th className="szv2-td-num">Líquido</th>
                      </tr>
                    </thead>
                    <tbody>
                      {history.slice(0, 50).map((tx, i) => (
                        <tr key={i}>
                          <td style={{ color: TXT_MUTED }}>{tx.date}</td>
                          <td>{tx.description}</td>
                          <td className="szv2-num">{tx.order}</td>
                          {/* TIPO: saque (type='withdrawal') mostra "Saque"; demais usam o
                              rótulo de movimento (Disponível/Pendente). LÍQUIDO = recebido. */}
                          <td>{tx.type === 'withdrawal' ? 'Saque' : tx.movement}</td>
                          <td className="szv2-td-num szv2-num">{brl(tx.value)}</td>
                          <td className="szv2-td-num szv2-num">{brl(tx.fee)}</td>
                          <td className="szv2-td-num szv2-num">{brl(tx.net)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <p style={{ padding: 16, fontSize: 13, color: TXT_MUTED }}>Seus recebimentos confirmados aparecem aqui.</p>
                )}
              </div>
            </div>

            {/* Lançamentos futuros */}
            <div className="szv2-card" style={{ flex: 1 }}>
              <div className="szv2-card-head">
                <div>
                  <h2>Lançamentos futuros</h2>
                  <p className="szv2-card-sub">Comissões a receber e datas de liberação.</p>
                </div>
                {futureTotal > 0 && (
                  <span className="szv2-num" style={{ fontSize: 13, color: 'var(--szv2-brand)' }}>{brl(futureTotal)}</span>
                )}
              </div>
              {/* Filtros de período compactos + intervalo personalizado (espelha wallet.php). */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap', marginBottom: 12 }}>
                {FUT_PERIODS.map(([lbl, slug]) => (
                  <button
                    key={slug}
                    type="button"
                    className={`szv2-period-btn ${futPeriod === slug ? 'szv2-period-btn--active' : ''}`}
                    onClick={() => selectFutPeriod(slug)}
                  >
                    {lbl}
                  </button>
                ))}
                <FalkDatePicker
                  value={futFrom}
                  onChange={v => changeFutFrom(v)}
                  style={{ width: 138, marginLeft: 4 }}
                  aria-label="Data inicial dos lançamentos futuros"
                />
                <span style={{ fontSize: 12, color: TXT_MUTED }}>–</span>
                <FalkDatePicker
                  value={futTo}
                  onChange={v => changeFutTo(v)}
                  style={{ width: 138 }}
                  aria-label="Data final dos lançamentos futuros"
                />
              </div>
              <div className="szv2-table-wrap szv2-table-flush">
                {loadingFuture ? (
                  <InlineLoading label="Carregando lançamentos…" />
                ) : future.length > 0 ? (
                  <table className="szv2-table">
                    <thead>
                      <tr>
                        <th>Data</th>
                        <th>Descrição</th>
                        <th>Pedido</th>
                        <th className="szv2-td-num">Comissão</th>
                        <th className="szv2-td-num">Liberação</th>
                      </tr>
                    </thead>
                    <tbody>
                      {future.slice(0, 30).map((ft, i) => (
                        <tr key={i}>
                          <td style={{ color: TXT_MUTED }}>{ft.date}</td>
                          <td>{ft.description}</td>
                          <td className="szv2-num">{ft.order}</td>
                          <td className="szv2-td-num szv2-num">{brl(ft.commission)}</td>
                          <td className="szv2-td-num" style={{ color: TXT_MUTED }}>{ft.release_at || ft.date || '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <p style={{ padding: 16, fontSize: 13, color: TXT_MUTED }}>Comissões a liberar aparecem aqui.</p>
                )}
              </div>
            </div>
          </div>

          {/* Dica de antecipação — vale p/ produtor (recebíveis) e afiliado
              (comissão pendente). Ambos agora têm o botão Antecipar. */}
          {futureTotal > 0 && (
            <p className="szv2-card-sub" style={{ marginTop: 8 }}>
              Clique em <strong>Antecipar</strong> para receber agora os valores pendentes, com taxa de{' '}
              {antFeePctDefault.toFixed(2).replace('.', ',')}%.
            </p>
          )}
        </>
      )}

      {/* ── Painel: Saques ── */}
      {codTab === 'saques' && (
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div>
              <h2>Ordens de saque</h2>
              <p className="szv2-card-sub">Acompanhe solicitações, taxas, comprovantes e conclusão.</p>
            </div>
          </div>
          {loadingWd ? (
            <InlineLoading label="Carregando saques…" />
          ) : withdrawals.length > 0 ? (
            <div className="szv2-table-wrap szv2-table-flush">
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Data</th>
                    <th>Valor</th>
                    <th>Taxa</th>
                    <th>Líquido</th>
                    <th>Conta</th>
                    <th>Status</th>
                    <th>Comprovante</th>
                  </tr>
                </thead>
                <tbody>
                  {withdrawals.slice(0, 50).map(wd => {
                    const [variant, label] = WD_STATUS_MAP[wd.status] || ['neutral', wd.status]
                    const holder = (wd.holder_name || '').trim()
                    const pix = (wd.pix_key || '').trim()
                    const ptype = (wd.pix_type || '').toUpperCase()
                    return (
                      <tr key={wd.id}>
                        <td style={{ color: TXT_MUTED }}>{wd.created_at}</td>
                        <td className="szv2-num">{brl(wd.amount)}</td>
                        <td className="szv2-num">{brl(wd.fee)}</td>
                        <td className="szv2-num szv2-td-num">{brl(wd.net)}</td>
                        <td style={{ color: TXT_MUTED }}>
                          {holder ? (
                            <>
                              {holder}
                              {!!pix && (
                                <>
                                  <br />
                                  <span style={{ fontSize: 11, color: 'var(--szv2-text-faint)' }}>
                                    PIX {ptype}: {pix}
                                  </span>
                                </>
                              )}
                            </>
                          ) : (
                            <span style={{ color: TXT_MUTED }}>—</span>
                          )}
                        </td>
                        <td>
                          <span className={`sz-badge szv2-badge-${variant}`}>{label}</span>
                        </td>
                        <td>
                          {wd.proof_url ? (
                            <a href={wd.proof_url} target="_blank" rel="noopener noreferrer" className="szv2-link-btn">
                              Ver
                            </a>
                          ) : (
                            <span style={{ color: TXT_MUTED }}>—</span>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState icon="💸" title="Nenhum saque solicitado" description="Saques realizados aparecem aqui." />
          )}
        </div>
      )}

      {/* ── Rodapé de saque (#44) — sempre no fim da página, nunca área vazia ──
          Produtor e Afiliado agora sacam/antecipam aqui: a taxa fixa de saque
          (R$ 2,99 = sz_cod_withdraw_fee / sz_aff_withdraw_fee) e a % de antecipação
          (4,99% = sz_*_anticipation_fee_pct) coincidem nos dois ledgers. */}
      <div
        style={{
          marginTop: 'var(--szv2-space-4)',
          padding: '14px 16px',
          borderTop: '1px solid var(--szv2-divider)',
          fontSize: 12.5,
          color: TXT_MUTED,
          lineHeight: 1.6,
        }}
      >
        <div style={{ fontWeight: 600, color: 'var(--szv2-text)', marginBottom: 6 }}>
          Sobre os saques
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 28px' }}>
          <span>
            Taxa de saque: <strong style={{ color: '#1E6FF2' }}>{brl(withdrawFeeDefault)}</strong> por solicitação
          </span>
          <span>
            Prazo de análise: <strong style={{ color: '#1E6FF2' }}>normalmente em até 1 dia útil</strong>
          </span>
          <span>
            Prazo para cair na conta: <strong style={{ color: '#1E6FF2' }}>até 1 dia útil após a aprovação</strong>
          </span>
        </div>
        <p style={{ margin: '8px 0 0' }}>
          Valores informativos. A taxa final e os prazos são confirmados pelo
          processamento do saque.{' '}
          {isAffiliate ? 'Antecipação de comissão' : 'Antecipação de recebíveis'}: taxa de{' '}
          <strong style={{ color: '#1E6FF2' }}>{antFeePctDefault.toFixed(2).replace('.', ',')}%</strong>{' '}
          sobre o valor antecipado.
        </p>
      </div>

      {/* ── Drawer de saque COD (2 passos) ── */}
      <Drawer
        open={wdOpen}
        onClose={() => { if (!wdSubmitting) setWdOpen(false) }}
        title={isAffiliate ? 'Solicitar saque de comissão' : 'Solicitar saque COD'}
        ariaLabel={isAffiliate ? 'Solicitar saque de comissão' : 'Solicitar saque COD'}
      >
        <>
            {wdStep === 'form' ? (
              <div>
                <div className="szv2-wd-summary-row">
                  <span className="szv2-label">Saldo disponível</span>
                  <strong>{brl(summary.available)}</strong>
                </div>
                {/* Afiliado sem disponível: a comissão está toda pendente. Aviso
                    explícito no topo do form + Continuar desabilitado (abaixo). */}
                {isAffiliate && summary.available <= 0 && (
                  <div
                    style={{
                      fontSize: 13,
                      color: 'var(--szv2-text-muted)',
                      background: 'rgba(30,111,242,.06)',
                      borderRadius: 'var(--szv2-radius-sm)',
                      padding: '10px 12px',
                      marginBottom: 14,
                      lineHeight: 1.5,
                    }}
                  >
                    Você não tem <strong>saldo disponível</strong> para saque no momento. Sua
                    comissão está pendente — use <strong>Antecipar</strong> para liberá-la.
                  </div>
                )}
                <div className="szv2-field">
                  <label className="szv2-label" htmlFor="szv2-wd-amount">Valor a sacar</label>
                  <input
                    type="text"
                    id="szv2-wd-amount"
                    className="szv2-input"
                    inputMode="numeric"
                    autoComplete="off"
                    placeholder="0,00"
                    value={wdAmount}
                    onChange={e => changeWdAmount(e.target.value)}
                  />
                  {/* #95 — TETO explícito: o valor a sacar não pode exceder o disponível. */}
                  <span className="szv2-field-hint">Máximo: {brl(summary.available)}</span>
                </div>
                {/* #95 — TAXA DE SAQUE EXPLÍCITA + valor líquido (espelha o modal de
                    antecipação). A taxa é FIXA em R$ por solicitação, não percentual.
                    Só aparece quando há valor digitado. */}
                {confAmount > 0 && (
                  <div style={{ background: 'rgba(30,111,242,.08)', borderRadius: 'var(--szv2-radius-sm)', padding: '12px 14px', marginBottom: 14 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', padding: '0 0 6px', fontSize: 13 }}>
                      <span style={{ color: TXT_MUTED }}>Valor a sacar</span>
                      <span className="szv2-num" style={{ fontSize: 15, fontWeight: 600 }}>{fmtMoney(confAmount)}</span>
                    </div>
                    <div style={{ display: 'flex', justifyContent: 'space-between', padding: '6px 0', fontSize: 13 }}>
                      <span style={{ color: TXT_MUTED }}>Taxa de saque</span>
                      <span>{fmtMoney(wdFee)}</span>
                    </div>
                    <div
                      style={{
                        display: 'flex',
                        justifyContent: 'space-between',
                        padding: '6px 0',
                        fontSize: 14,
                        fontWeight: 600,
                        borderTop: '1px solid var(--szv2-border)',
                        marginTop: 4,
                      }}
                    >
                      <span>Valor a receber (líquido)</span>
                      <span style={{ color: 'var(--szv2-brand)' }}>{fmtMoney(wdNet)}</span>
                    </div>
                  </div>
                )}
                <div className="szv2-field">
                  <label className="szv2-label" htmlFor="szv2-wd-account-select">Conta para recebimento</label>
                  <FalkSelect
                    id="szv2-wd-account-select"
                    value={String(wdAccountId)}
                    onChange={v => setWdAccountId(parseInt(v, 10) || 0)}
                    options={
                      accounts.length === 0
                        ? [{ value: '0', label: 'Nenhuma conta PIX cadastrada' }]
                        : accounts.map(a => ({ value: String(a.id), label: accountLabel(a) }))
                    }
                  />
                  {accounts.length === 0 && (
                    <span className="szv2-field-hint">
                      Nenhuma conta PIX cadastrada. Adicione em <strong>Configurações → Saques</strong>.
                    </span>
                  )}
                </div>
                {!!wdMsg && <div style={{ fontSize: 13, color: 'var(--szv2-danger)', marginBottom: 8 }}>{wdMsg}</div>}
              </div>
            ) : (
              <div>
                <p className="szv2-card-sub" style={{ marginTop: 0 }}>Confirme os dados antes de solicitar:</p>
                <div style={{ background: 'rgba(30,111,242,.08)', borderRadius: 'var(--szv2-radius-sm)', padding: '12px 14px', marginBottom: 14 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', padding: '6px 0', fontSize: 13 }}>
                    <span style={{ color: TXT_MUTED }}>Valor solicitado</span>
                    <strong className="szv2-num">{fmtMoney(confAmount)}</strong>
                  </div>
                  {/* #95 — taxa de saque EXPLÍCITA também na confirmação. */}
                  <div style={{ display: 'flex', justifyContent: 'space-between', padding: '6px 0', fontSize: 13 }}>
                    <span style={{ color: TXT_MUTED }}>Taxa de saque</span>
                    <span>{fmtMoney(wdFee)}</span>
                  </div>
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      padding: '6px 0',
                      fontSize: 13,
                      fontWeight: 600,
                      borderTop: '1px solid var(--szv2-border)',
                      marginTop: 2,
                    }}
                  >
                    <span>Valor a receber (líquido)</span>
                    <span style={{ color: 'var(--szv2-brand)' }}>{fmtMoney(wdNet)}</span>
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'space-between', padding: '6px 0', fontSize: 13 }}>
                    <span style={{ color: TXT_MUTED }}>Conta PIX</span>
                    <span>{confAccount ? accountLabel(confAccount) : '—'}</span>
                  </div>
                </div>
                <p className="szv2-card-sub">O saque ficará em análise. Processamento em até 1 dia útil.</p>
                {!!wdMsg && <div style={{ fontSize: 13, color: 'var(--szv2-danger)', marginBottom: 8 }}>{wdMsg}</div>}
              </div>
            )}

            {/* Rodapé inline (justify-end): o modal-foot original assume a borda
                edge-to-edge do modal; dentro do corpo padded do Drawer usamos flex
                inline + separador superior p/ manter o alinhamento. */}
            <div
              style={{
                display: 'flex',
                justifyContent: 'flex-end',
                gap: 8,
                marginTop: 18,
                paddingTop: 14,
                borderTop: '1px solid var(--szv2-divider)',
              }}
            >
              {wdStep === 'confirm' && (
                <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setWdStep('form')} disabled={wdSubmitting}>
                  Voltar
                </button>
              )}
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setWdOpen(false)} disabled={wdSubmitting}>
                Cancelar
              </button>
              {wdStep === 'form' ? (
                <button
                  type="button"
                  className="szv2-btn szv2-btn-brand"
                  onClick={wdNext}
                  disabled={accounts.length === 0 || (isAffiliate && summary.available <= 0)}
                >
                  Continuar
                </button>
              ) : (
                <button type="button" className="szv2-btn szv2-btn-brand" onClick={wdSubmit} disabled={wdSubmitting}>
                  {wdSubmitting ? 'Processando…' : 'Confirmar saque'}
                </button>
              )}
            </div>
        </>
      </Drawer>

      {/* ── Drawer de antecipação ── */}
      <Drawer
        open={antOpen}
        onClose={() => { if (!antSubmitting) setAntOpen(false) }}
        title="Antecipar recebíveis"
        ariaLabel="Antecipar recebíveis"
        width={460}
      >
        <div>
              <div style={{ background: 'rgba(30,111,242,.08)', borderRadius: 'var(--szv2-radius-sm)', padding: '12px 14px', marginBottom: 14 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', padding: '0 0 6px', fontSize: 13 }}>
                  <span style={{ color: TXT_MUTED }}>Total a antecipar</span>
                  <span className="szv2-num" style={{ fontSize: 15, fontWeight: 600 }}>{fmtMoney(antTotal)}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', padding: '6px 0', fontSize: 13 }}>
                  <span style={{ color: TXT_MUTED }}>Taxa de antecipação ({antFeePctDefault.toFixed(2).replace('.', ',')}%)</span>
                  <span>{fmtMoney(antFee)}</span>
                </div>
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    padding: '6px 0',
                    fontSize: 14,
                    fontWeight: 600,
                    borderTop: '1px solid var(--szv2-border)',
                    marginTop: 4,
                  }}
                >
                  <span>Valor a receber (líquido)</span>
                  <span style={{ color: 'var(--szv2-brand)' }}>{fmtMoney(antNet)}</span>
                </div>
              </div>
              {/* #94 — antecipação NÃO pede conta PIX: o valor líquido é creditado
                  direto no saldo disponível (o saque para PIX é outro fluxo). */}
              <p className="szv2-card-sub" style={{ marginTop: 0, marginBottom: 14 }}>
                O valor líquido será creditado no seu <strong>saldo disponível</strong>.
                A antecipação não exige conta para recebimento — o saque para PIX é
                feito separadamente pelo botão <strong>Saque</strong>.
              </p>
              {!!antMsg && <div style={{ fontSize: 13, color: 'var(--szv2-danger)', marginBottom: 8 }}>{antMsg}</div>}
            <div
              style={{
                display: 'flex',
                justifyContent: 'flex-end',
                gap: 8,
                marginTop: 18,
                paddingTop: 14,
                borderTop: '1px solid var(--szv2-divider)',
              }}
            >
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setAntOpen(false)} disabled={antSubmitting}>
                Cancelar
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                onClick={antSubmit}
                disabled={antSubmitting}
              >
                {antSubmitting ? 'Processando…' : 'Confirmar antecipação'}
              </button>
            </div>
        </div>
      </Drawer>

      {/* ── Drawer de transferência p/ Carteira de Frete ── */}
      <Drawer
        open={ftOpen}
        onClose={() => { if (!ftSubmitting) setFtOpen(false) }}
        title="Transferir p/ Carteira de Frete"
        ariaLabel="Transferir para carteira de frete"
        width={460}
      >
        <div>
          <p className="szv2-card-sub" style={{ marginTop: 0, marginBottom: 14 }}>
            O valor sai do seu saldo disponível na Carteira COD e fica em análise
            até o admin aprovar. Após aprovado, o crédito aparece na sua
            Carteira de Expedição (frete pré-pago).
          </p>
          <label className="szv2-field-label" htmlFor="ft-amount">Valor a transferir</label>
          <input
            id="ft-amount"
            type="text"
            inputMode="decimal"
            className="szv2-input"
            placeholder="0,00"
            value={ftAmount}
            onChange={e => setFtAmount(e.target.value)}
            disabled={ftSubmitting}
          />
          <p className="szv2-card-sub" style={{ marginTop: 6 }}>
            Disponível: {fmtMoney(summary.available)} · Mínimo R$ 10,00
          </p>
          {!!ftMsg && <div style={{ fontSize: 13, color: 'var(--szv2-danger)', marginBottom: 8 }}>{ftMsg}</div>}
          <div
            style={{
              display: 'flex',
              justifyContent: 'flex-end',
              gap: 8,
              marginTop: 18,
              paddingTop: 14,
              borderTop: '1px solid var(--szv2-divider)',
            }}
          >
            <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setFtOpen(false)} disabled={ftSubmitting}>
              Cancelar
            </button>
            <button
              type="button"
              className="szv2-btn szv2-btn-brand"
              onClick={ftSubmit}
              disabled={ftSubmitting}
            >
              {ftSubmitting ? 'Enviando…' : 'Solicitar transferência'}
            </button>
          </div>
        </div>
      </Drawer>

      </>
      )}
    </section>
  )
}
