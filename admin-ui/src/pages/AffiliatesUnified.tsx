// Tela única "Afiliados" — funde o que antes eram 2 sub-abas (Lista / Carteira)
// num só lugar: uma tabela com identidade + saldos de carteira lado a lado, e
// um único drawer com abas Perfil / Carteira. Antes o operador precisava
// trocar de aba pra ver quem é o afiliado (Lista) e depois trocar de novo pra
// ver quanto ele tem disponível/pendente (Carteira) — duas fontes, dois
// drawers, duas listas que nem sempre casavam 1:1 visualmente.
//
// Fusão client-side: GET /affiliates (identidade/status/vendas) + GET
// /affiliates-wallet (saldos/livro razão) casados por EMAIL (mais confiável
// que id — os dois endpoints usam espaços de id historicamente diferentes:
// /affiliates.user_id = portal id; /affiliates-wallet.affiliate_id = wp_user_id).
// Afiliado sem carteira ainda (comissão nunca gerada) aparece com saldos zerados.
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { brl, brDate } from '../utils/format'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'
import CopyButton from '../components/CopyButton'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import { confirmAsync } from '../components/ConfirmDialog'
import { emitToast } from '../hooks/useToast'
import { useToast } from '../hooks/useToast'
import { drawerTabsStyle, drawerTabBtnStyle } from '../components/drawerTabs'
import StatusBadge from '../components/StatusBadge'

const REFERRAL_BASE = 'https://falklog.com.br/r/'

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const money = (v: number) => 'R$ ' + fmt(v)

// ── Tipos: GET /affiliates ───────────────────────────────────────────────
type A = {
  user_id: number
  email: string
  nome: string
  telefone: string
  cpf: string
  pix_key: string
  affiliate_code: string | null
  comissao_pct: number
  status: string
  created_at: string
  vinculos: number
  links_count: number
  total_clicks: number
  total_vendido_30d: number
  total_comissao_30d: number
  pedidos_count_30d: number
  last_order_at: string | null
  comissao_pendente: number
  comissao_disponivel: number
  valor_vendido_total: number
}

// ── Tipos: GET /affiliates-wallet ────────────────────────────────────────
type WalletRow = {
  affiliate_id: number
  nome: string
  email: string
  pending_balance: number
  balance: number
  debt_amount: number
  saques_total: number
  penalidades_total: number
  pedidos_validos: number
}

type Summary = {
  total_pendente: number
  total_disponivel: number
  total_debt: number
  affiliates_count: number
}

type Tx = {
  id: number
  order_id: number | null
  type: string
  status: string
  amount: number
  available_at: string | null
  meta_json: string | null
  created_at: string
}

// ── Detalhe de perfil: GET /affiliates/{user_id}/detail ─────────────────
type ProdutorVinculado = {
  produtor_nome?: string
  produto_id?: number | null
  produto_nome?: string | null
  comissao_pct?: number
  status?: string
}
type ContaCadastrada = {
  nome?: string | null
  pix_key?: string | null
  pix_type?: string | null
  is_default?: boolean
}
type AffiliateTaxas = {
  taxa_transacao_total?: number
  penalidades_frustracao_total?: number
  taxa_saque_total?: number
  total_taxas?: number
}
type AffiliateDetail = {
  user_id?: number
  nome?: string
  email?: string
  telefone?: string
  cpf?: string
  pix_key?: string
  pix_tipo?: string
  vinculos?: ProdutorVinculado[]
  contas?: ContaCadastrada[]
  taxas?: AffiliateTaxas
}

// Linha unificada — identidade (A) + saldos (WalletRow, opcional: pode não
// existir carteira ainda).
type Unified = A & {
  wallet: WalletRow | null
}

const STATUS_OPTS = [
  { value: '',            label: 'Todos' },
  { value: 'active',      label: 'Ativo' },
  { value: 'sem_vinculo', label: 'Sem vínculo' },
]
const VENDAS_OPTS = [
  { value: '',    label: 'Todos' },
  { value: 'com', label: 'Com vendas' },
  { value: 'sem', label: 'Sem vendas' },
]

const TX_TYPE_BADGE: Record<string, string> = {
  commission:           'szv2-badge-success',
  manual_credit:        'szv2-badge-success',
  penalty:              'szv2-badge-danger',
  manual_debit:         'szv2-badge-danger',
  withdrawal:           'szv2-badge-danger',
  approval:             'szv2-badge-warning',
  frustration_reversal: 'szv2-badge-warning',
}
const TX_NEGATIVE = new Set(['penalty', 'manual_debit', 'withdrawal'])
const TX_TYPE_LABEL: Record<string, string> = {
  commission:           'Comissão',
  penalty:              'Penalidade',
  withdrawal:           'Saque',
  approval:             'Liberação',
  manual_credit:        'Crédito manual',
  manual_debit:         'Débito manual',
  frustration_reversal: 'Estorno (frustrado)',
  senderzz_fee:         'Taxa FALK',
}
function txTypeLabel(type: string): string {
  const t = (type || '').trim().toLowerCase()
  if (!t) return '—'
  if (TX_TYPE_LABEL[t]) return TX_TYPE_LABEL[t]
  const s = t.replace(/_/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}
const TX_STATUS_LABEL: Record<string, string> = {
  pending: 'Pendente', pendente: 'Pendente',
  available: 'Disponível', disponivel: 'Disponível',
  approved: 'Aprovado', aprovado: 'Aprovado',
  applied: 'Aplicado', aplicado: 'Aplicado',
  paid: 'Pago', pago: 'Pago',
  cancelled: 'Cancelado', cancelado: 'Cancelado',
  reversed: 'Estornado', estornado: 'Estornado',
  rejected: 'Rejeitado', rejeitado: 'Rejeitado',
}
function txStatusLabel(status: string): string {
  const s = (status || '').trim().toLowerCase()
  if (!s) return '—'
  if (TX_STATUS_LABEL[s]) return TX_STATUS_LABEL[s]
  const v = s.replace(/_/g, ' ')
  return v.charAt(0).toUpperCase() + v.slice(1)
}
const META_FIELD_LABEL: Record<string, string> = {
  gross: 'Bruto', fees: 'Taxas', net: 'Líquido', commission_pct: 'Comissão',
  commission_gross: 'Comissão bruta',
  transaction_fee_affiliate: 'Taxa transação (afiliado)',
  transaction_fee_producer: 'Taxa transação (produtor)',
  transaction_fee_total: 'Taxa transação',
  prev_amount: 'Valor anterior', count: 'Ocorrências', source: 'Origem',
}
const META_HIDDEN = new Set(['transaction_fee_mode', 'calc_mode', 'phone_hash'])
const META_PCT_FIELDS = new Set(['commission_pct'])
const META_MONEY_FIELDS = new Set([
  'gross', 'fees', 'net', 'commission_gross', 'prev_amount',
  'transaction_fee_affiliate', 'transaction_fee_producer', 'transaction_fee_total',
])
const META_SOURCE_LABEL: Record<string, string> = {
  admin_audit_fix: 'Ajuste de auditoria',
  admin_audit_fix_order: 'Ajuste de auditoria (pedido)',
  admin_orders: 'Ajuste de pedido',
}
type MetaPair = { label: string; value: string }
function parseMetaPairs(raw: string | null): MetaPair[] {
  if (!raw) return []
  let obj: Record<string, unknown>
  try {
    const p = JSON.parse(raw)
    if (!p || typeof p !== 'object' || Array.isArray(p)) return []
    obj = p as Record<string, unknown>
  } catch { return [] }
  const pairs: MetaPair[] = []
  for (const [k, v] of Object.entries(obj)) {
    if (META_HIDDEN.has(k)) continue
    if (v === null || v === undefined || v === '') continue
    const label = META_FIELD_LABEL[k] || (k.charAt(0).toUpperCase() + k.slice(1).replace(/_/g, ' '))
    let value: string
    if (k === 'source' && typeof v === 'string') value = META_SOURCE_LABEL[v] || v.replace(/_/g, ' ')
    else if (META_MONEY_FIELDS.has(k) && typeof v === 'number') value = money(v)
    else if (META_PCT_FIELDS.has(k) && typeof v === 'number') value = `${fmt(v)}%`
    else value = String(v)
    pairs.push({ label, value })
  }
  return pairs
}
function metaSummary(raw: string | null): string {
  const pairs = parseMetaPairs(raw)
  if (!pairs.length) return '—'
  const order = ['net', 'gross', 'fees', 'transaction_fee_total', 'count', 'source']
  const byLabel = (k: string) => META_FIELD_LABEL[k]
  const picked: MetaPair[] = []
  for (const k of order) {
    const lbl = byLabel(k)
    const found = lbl ? pairs.find(p => p.label === lbl) : undefined
    if (found && !picked.includes(found)) picked.push(found)
    if (picked.length >= 2) break
  }
  const list = picked.length ? picked : pairs.slice(0, 2)
  return list.map(p => `${p.label} ${p.value}`).join(' · ')
}

function Dash() {
  return <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
}

function KpiCard({
  label, value, sub, tone,
}: { label: string; value: string | number; sub?: string; tone?: 'brand' | 'success' | 'warning' | 'danger' }) {
  const color = (() => {
    switch (tone) {
      case 'success': return 'var(--szv2-success)'
      case 'warning': return 'var(--szv2-warning)'
      case 'danger':  return 'var(--szv2-danger)'
      default:        return 'var(--szv2-brand)'
    }
  })()
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span className="szv2-kpi-value" style={{ color }}>{value}</span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────
// Drawer único — abas Perfil / Carteira. Substitui os 2 drawers antigos
// (AffiliateDetailDrawer + TxDrawer) que existiam quando Lista e Carteira
// eram telas separadas.
// ─────────────────────────────────────────────────────────────────────────
function AffiliateDrawer({
  affiliate,
  types,
  onClose,
  onSaved,
  onWalletChanged,
  initialTab = 'perfil',
}: {
  affiliate: Unified
  types: string[]
  onClose: () => void
  onSaved: () => void
  onWalletChanged: () => void
  initialTab?: 'perfil' | 'carteira'
}) {
  const [tab, setTab] = useState<'perfil' | 'carteira'>(initialTab)
  const showToast = useToast()

  // ── Aba Perfil ──────────────────────────────────────────────────────
  const [detail, setDetail] = useState<AffiliateDetail | null>(null)
  const [loadingDetail, setLoadingDetail] = useState(true)
  const [detailErr, setDetailErr] = useState('')
  const [unavailable, setUnavailable] = useState(false)
  const [editCpf, setEditCpf] = useState('')
  const [editPhone, setEditPhone] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedOk, setSavedOk] = useState(false)

  useEffect(() => {
    function onKey(e: KeyboardEvent) { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  useEffect(() => {
    let active = true
    setLoadingDetail(true); setDetailErr(''); setUnavailable(false); setSavedOk(false)
    setEditCpf(affiliate.cpf ?? '')
    setEditPhone(affiliate.telefone ?? '')
    api<AffiliateDetail>(`/affiliates/${affiliate.user_id}/detail`)
      .then(d => {
        if (!active) return
        setDetail(d)
        setEditCpf(d.cpf ?? affiliate.cpf ?? '')
        setEditPhone(d.telefone ?? affiliate.telefone ?? '')
      })
      .catch(e => {
        if (!active) return
        const msg = String(e?.message || '')
        if (/404|não encontrado|nao encontrado|not_found|not found/i.test(msg)) setUnavailable(true)
        else setDetailErr(msg || 'Erro ao carregar detalhe')
      })
      .finally(() => { if (active) setLoadingDetail(false) })
    return () => { active = false }
  }, [affiliate.user_id, affiliate.cpf, affiliate.telefone])

  async function handleSave() {
    setSaving(true); setDetailErr(''); setSavedOk(false)
    try {
      await api(`/affiliates/${affiliate.user_id}`, {
        method: 'PUT',
        body: JSON.stringify({ cpf: editCpf.trim(), phone: editPhone.trim() }),
      })
      setDetail(d => d ? { ...d, cpf: editCpf.trim(), telefone: editPhone.trim() } : d)
      setSavedOk(true)
      emitToast('ok', 'Dados do afiliado salvos.')
      onSaved()
    } catch (e: any) {
      setDetailErr(e.message || 'Erro ao salvar dados do afiliado')
      emitToast('err', e.message || 'Erro ao salvar dados do afiliado')
    } finally {
      setSaving(false)
    }
  }

  const pixKey = detail?.pix_key ?? affiliate.pix_key
  const pixType = detail?.pix_tipo ?? ''
  const produtores = detail?.vinculos ?? []
  const contas = detail?.contas ?? []
  const taxas = detail?.taxas
  const refCode = (affiliate.affiliate_code || '').trim()
  const refLink = refCode ? `${REFERRAL_BASE}${refCode}` : ''

  const labelStyle: React.CSSProperties = {
    fontSize: 11, fontWeight: 700, color: 'var(--szv2-text-muted)',
    textTransform: 'uppercase', letterSpacing: 0.4, marginBottom: 8,
  }

  // ── Aba Carteira ────────────────────────────────────────────────────
  const wallet = affiliate.wallet
  const [txItems, setTxItems] = useState<Tx[]>([])
  const [loadingTx, setLoadingTx] = useState(true)
  const [txErr, setTxErr] = useState('')
  const [filterType, setFilterType] = useState<string>('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!wallet) { setTxItems([]); setLoadingTx(false); return }
    let active = true
    setLoadingTx(true); setTxErr('')
    api<{ items: Tx[] }>(`/affiliates-wallet/${wallet.affiliate_id}/transactions?limit=200`)
      .then(r => { if (active) setTxItems(r.items || []) })
      .catch(e => { if (active) setTxErr(e.message || 'Erro') })
      .finally(() => { if (active) setLoadingTx(false) })
    return () => { active = false }
  }, [wallet?.affiliate_id])

  const visibleTx = useMemo(() => {
    if (!filterType) return txItems
    return txItems.filter(t => t.type === filterType)
  }, [txItems, filterType])

  async function handleSync() {
    if (!wallet) return
    setBusy(true)
    try {
      await api(`/affiliates-wallet/${wallet.affiliate_id}/wallet-fix`, { method: 'POST' })
      showToast('ok', `Carteira sincronizada.`)
      onWalletChanged()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao sincronizar')
    } finally {
      setBusy(false)
    }
  }

  async function handleRelease(force = false) {
    if (!wallet) return
    const msg = force
      ? `FORÇAR liberação de TODAS as comissões pendentes AGORA, ignorando o prazo de retenção?\n\nLibera dinheiro real antes do vencimento e fica registrado na auditoria.`
      : `Liberar transações pendentes vencidas?`
    if (!await confirmAsync({ message: msg })) return
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; released: number; still_pending?: number; next_release_at?: string | null }>(
        `/affiliates-wallet/${wallet.affiliate_id}/release-pending${force ? '?force=1' : ''}`,
        { method: 'POST' },
      )
      const rel = r.released ?? 0
      const still = r.still_pending ?? 0
      if (rel > 0) showToast('ok', `${rel} comissão(ões) liberada(s).`)
      else if (still > 0) {
        const when = r.next_release_at ? new Date(r.next_release_at).toLocaleDateString('pt-BR') : null
        showToast('warn', when
          ? `0 liberadas — ${still} ainda em retenção (libera a partir de ${when}). Use "Forçar" para antecipar.`
          : `0 liberadas — ${still} ainda em retenção. Use "Forçar" para antecipar.`)
      } else showToast('ok', `Nenhuma comissão pendente.`)
      onWalletChanged()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao liberar')
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div onClick={onClose} style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)', zIndex: 501 }} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Detalhe do afiliado"
        style={{
          position: 'fixed', top: 0, right: 0, height: '100vh', width: 520, maxWidth: '96vw',
          background: 'var(--szv2-surface)', borderLeft: '1px solid var(--szv2-divider)',
          boxShadow: '-12px 0 32px rgba(0,0,0,.18)', zIndex: 502,
          display: 'flex', flexDirection: 'column', overflow: 'hidden',
        }}
      >
        <div style={{
          padding: '16px 20px', borderBottom: '1px solid var(--szv2-divider)',
          display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12,
        }}>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontWeight: 700, fontSize: 15, color: 'var(--szv2-text)' }}>
              {affiliate.nome || '(sem nome)'}
            </div>
            <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {affiliate.email}
            </div>
          </div>
          <button
            type="button" onClick={onClose} aria-label="Fechar painel"
            style={{
              width: 32, height: 32, border: 0, background: 'transparent', borderRadius: 8,
              cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center',
              color: 'var(--szv2-text-muted)', fontSize: 18, lineHeight: 1, flexShrink: 0,
            }}
          >✕</button>
        </div>

        {/* Abas Perfil / Carteira */}
        <div style={{ padding: '12px 20px 0' }}>
          <div style={drawerTabsStyle}>
            {([
              ['perfil', 'Perfil'],
              ['carteira', 'Carteira'],
            ] as const).map(([key, label]) => (
              <button key={key} type="button" onClick={() => setTab(key)} style={drawerTabBtnStyle(tab === key)}>
                {label}
              </button>
            ))}
          </div>
        </div>

        <div style={{ flex: 1, padding: '16px 20px 20px', display: 'flex', flexDirection: 'column', gap: 20, overflowY: 'auto' }}>
          {tab === 'perfil' && (
            <>
              {detailErr && <div className="sz-alert-danger">{detailErr}</div>}
              {unavailable && (
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', background: 'var(--szv2-surface-alt)', border: '1px solid var(--szv2-border)', borderRadius: 8, padding: '8px 12px' }}>
                  Detalhe completo indisponível para este afiliado. Exibindo dados da lista.
                </div>
              )}
              {loadingDetail ? (
                <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>Carregando…</div>
              ) : (
                <>
                  <section>
                    <div style={labelStyle}>Produtores vinculados</div>
                    {produtores.length > 0 ? (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                        {produtores.map((p, i) => (
                          <div key={i} style={{ padding: '10px 12px', background: 'var(--szv2-surface-alt)', border: '1px solid var(--szv2-border)', borderRadius: 10 }}>
                            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                              <span style={{ fontWeight: 600, fontSize: 13 }}>{p.produtor_nome || <Dash />}</span>
                              <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--szv2-brand)', background: 'var(--szv2-brand-light, rgba(30,111,242,.10))', padding: '2px 8px', borderRadius: 6, whiteSpace: 'nowrap' }}>
                                {(p.comissao_pct ?? 0)}%
                              </span>
                            </div>
                            <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 2 }}>
                              {p.produto_nome || 'Todos os produtos'}
                            </div>
                          </div>
                        ))}
                      </div>
                    ) : <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Nenhum produtor vinculado.</div>}
                  </section>

                  {taxas && (
                    <section>
                      <div style={labelStyle}>Taxas cobradas</div>
                      {(taxas.total_taxas ?? 0) > 0 ? (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 12 }}>
                            <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Taxa de transação (4,99%)</span>
                            <span style={{ fontSize: 13, color: 'var(--szv2-text)', fontWeight: 600 }}>{brl(taxas.taxa_transacao_total ?? 0)}</span>
                          </div>
                          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 12 }}>
                            <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Penalidades de frustração</span>
                            <span style={{ fontSize: 13, color: 'var(--szv2-text)', fontWeight: 600 }}>{brl(taxas.penalidades_frustracao_total ?? 0)}</span>
                          </div>
                          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 12 }}>
                            <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Taxa de saque</span>
                            <span style={{ fontSize: 13, color: 'var(--szv2-text)', fontWeight: 600 }}>{brl(taxas.taxa_saque_total ?? 0)}</span>
                          </div>
                          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 12, marginTop: 4, paddingTop: 10, borderTop: '1px solid var(--szv2-divider)' }}>
                            <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--szv2-text)' }}>Total de taxas</span>
                            <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--szv2-brand)' }}>{brl(taxas.total_taxas ?? 0)}</span>
                          </div>
                        </div>
                      ) : <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Nenhuma taxa cobrada ainda.</div>}
                    </section>
                  )}

                  <section>
                    <div style={labelStyle}>Dados</div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                      <label style={{ display: 'block' }}>
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 4 }}>CPF</div>
                        <input type="text" className="szv2-input" style={{ width: '100%', fontFamily: 'var(--szv2-font-mono)', fontSize: 13 }}
                          placeholder="000.000.000-00" value={editCpf}
                          onChange={e => { setEditCpf(e.target.value); setSavedOk(false) }} />
                      </label>
                      <label style={{ display: 'block' }}>
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 4 }}>Telefone</div>
                        <input type="text" className="szv2-input" style={{ width: '100%', fontFamily: 'var(--szv2-font-mono)', fontSize: 13 }}
                          placeholder="(00) 00000-0000" value={editPhone}
                          onChange={e => { setEditPhone(e.target.value); setSavedOk(false) }} />
                      </label>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                        <button type="button" className="szv2-btn szv2-btn-brand" disabled={saving} onClick={handleSave}>
                          {saving ? 'Salvando…' : 'Salvar dados'}
                        </button>
                        {savedOk && <span style={{ fontSize: 12, color: 'var(--szv2-success, #16a34a)', fontWeight: 600 }}>✓ Salvo</span>}
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                        <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Chave PIX</span>
                        <span style={{ fontSize: 13, textAlign: 'right', fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-text-soft)', wordBreak: 'break-all' }}>
                          {pixKey ? <>{!!pixType && <span style={{ color: 'var(--szv2-text-muted)' }}>{pixType.toUpperCase()} · </span>}{pixKey}</> : <Dash />}
                        </span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
                        <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>Link de convite</span>
                        <span style={{ fontSize: 13, textAlign: 'right', wordBreak: 'break-all', display: 'flex', alignItems: 'center', gap: 6, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                          {refLink ? (
                            <>
                              <span style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-text-soft)', fontSize: 12 }}>{refLink}</span>
                              <CopyButton text={refLink} variant="icon" title="Copiar link de convite" />
                            </>
                          ) : <Dash />}
                        </span>
                      </div>
                    </div>
                  </section>

                  <section>
                    <div style={labelStyle}>Contas cadastradas</div>
                    {contas.length > 0 ? (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                        {contas.map((c, i) => (
                          <div key={i} style={{ padding: '10px 12px', background: 'var(--szv2-surface-alt)', border: '1px solid var(--szv2-border)', borderRadius: 10 }}>
                            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                              {!!c.nome && <div style={{ fontWeight: 600, fontSize: 13 }}>{c.nome}</div>}
                              {c.is_default && <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--szv2-brand)', whiteSpace: 'nowrap' }}>Padrão</span>}
                            </div>
                            {!!c.pix_key && (
                              <div style={{ fontSize: 12, color: 'var(--szv2-text-soft)', fontFamily: 'var(--szv2-font-mono)', marginTop: !!c.nome ? 4 : 0, wordBreak: 'break-all' }}>
                                {!!c.pix_type && <span style={{ color: 'var(--szv2-text-muted)' }}>{c.pix_type.toUpperCase()} · </span>}{c.pix_key}
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    ) : <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Nenhuma conta cadastrada.</div>}
                  </section>
                </>
              )}
            </>
          )}

          {tab === 'carteira' && (
            <>
              {!wallet ? (
                <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)', padding: 24, textAlign: 'center' }}>
                  Este afiliado ainda não tem carteira (nenhuma comissão gerada até agora).
                </div>
              ) : (
                <>
                  {/* Resumo */}
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0,1fr))', gap: 12 }}>
                    <div style={{ padding: 12, background: 'var(--szv2-warning-bg)', borderRadius: 8 }}>
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Pendente</div>
                      <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-warning)' }}>{money(wallet.pending_balance)}</div>
                    </div>
                    <div style={{ padding: 12, background: 'var(--szv2-success-bg)', borderRadius: 8 }}>
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Disponível</div>
                      <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-success)' }}>{money(wallet.balance)}</div>
                    </div>
                    <div style={{ padding: 12, background: 'var(--szv2-info-bg)', borderRadius: 8 }}>
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Saques</div>
                      <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-info)' }}>{money(wallet.saques_total)}</div>
                    </div>
                    <div style={{ padding: 12, background: 'var(--szv2-danger-bg)', borderRadius: 8 }}>
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Penalidades</div>
                      <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-danger)' }}>{money(wallet.penalidades_total)}</div>
                    </div>
                  </div>

                  {/* Ações de carteira */}
                  <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                    <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-secondary" onClick={() => handleRelease(false)} disabled={busy}
                      title="Promove pending → available para comissões já vencidas (respeita a retenção)">
                      {busy ? '…' : 'Liberar pendentes'}
                    </button>
                    {wallet.pending_balance > 0 && (
                      <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-danger" onClick={() => handleRelease(true)} disabled={busy}
                        title="OVERRIDE do dono: libera AGORA ignorando a retenção (auditado)">
                        {busy ? '…' : 'Forçar liberação'}
                      </button>
                    )}
                    <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-brand" onClick={handleSync} disabled={busy}
                      title="Reagrega balance/pending_balance a partir do livro razão">
                      {busy ? '…' : 'Sync wallet'}
                    </button>
                  </div>

                  {/* Chips de filtro por tipo */}
                  <div>
                    <div style={labelStyle}>Transações</div>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 12 }}>
                      <button type="button" className={`sz-badge ${filterType === '' ? 'szv2-badge-brand' : 'szv2-badge-neutral'}`}
                        style={{ cursor: 'pointer', border: 'none' }} onClick={() => setFilterType('')}>
                        Todos ({txItems.length})
                      </button>
                      {types.map(t => {
                        const n = txItems.filter(x => x.type === t).length
                        return (
                          <button type="button" key={t}
                            className={`sz-badge ${filterType === t ? (TX_TYPE_BADGE[t] || 'szv2-badge-brand') : 'szv2-badge-neutral'}`}
                            style={{ cursor: 'pointer', border: 'none', opacity: n === 0 ? 0.5 : 1 }}
                            onClick={() => setFilterType(t)}>
                            {txTypeLabel(t)} ({n})
                          </button>
                        )
                      })}
                    </div>

                    {txErr && <div className="sz-alert-danger">{txErr}</div>}
                    {loadingTx ? (
                      <div style={{ padding: 40, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>Carregando…</div>
                    ) : visibleTx.length === 0 ? (
                      <div className="szv2-empty">
                        <h3>Nenhuma transação</h3>
                        <p>{filterType ? 'Nenhum registro para esse tipo.' : 'O afiliado ainda não tem movimentações.'}</p>
                      </div>
                    ) : (
                      <div style={{ overflowX: 'auto' }}>
                        <table className="szv2-table">
                          <thead>
                            <tr>
                              <th>Data</th><th>Pedido</th><th>Tipo</th><th>Status</th>
                              <th style={{ textAlign: 'right' }}>Valor</th><th>Disponível em</th><th>Detalhe</th>
                            </tr>
                          </thead>
                          <tbody>
                            {visibleTx.map(t => {
                              const isNeg = TX_NEGATIVE.has(t.type)
                              return (
                                <tr key={t.id}>
                                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                                    {t.created_at?.slice(0, 16).replace('T', ' ') ?? '—'}
                                  </td>
                                  <td style={{ fontWeight: 600 }}>{t.order_id ? `#${t.order_id}` : '—'}</td>
                                  <td><span className={`sz-badge ${TX_TYPE_BADGE[t.type] || 'szv2-badge-neutral'}`}>{txTypeLabel(t.type)}</span></td>
                                  <td><StatusBadge status={t.status} label={txStatusLabel(t.status)} /></td>
                                  <td style={{ textAlign: 'right', fontWeight: 700, color: isNeg ? 'var(--szv2-danger)' : 'var(--szv2-success)' }}>
                                    {isNeg ? '−' : '+'} {money(Math.abs(t.amount))}
                                  </td>
                                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                                    {t.available_at?.slice(0, 16).replace('T', ' ') ?? '—'}
                                  </td>
                                  <td style={{ fontSize: 11.5, color: 'var(--szv2-text-muted)', maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                                    title={parseMetaPairs(t.meta_json).map(p => `${p.label}: ${p.value}`).join('\n') || (t.meta_json ?? '')}>
                                    {metaSummary(t.meta_json)}
                                  </td>
                                </tr>
                              )
                            })}
                          </tbody>
                        </table>
                      </div>
                    )}
                  </div>
                </>
              )}
            </>
          )}
        </div>
      </div>
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────────
// Página principal — tabela única (identidade + carteira) + drawer único.
// ─────────────────────────────────────────────────────────────────────────
export default function AffiliatesUnified() {
  const [items, setItems] = useState<A[]>([])
  const [total, setTotal] = useState(0)
  const [walletByEmail, setWalletByEmail] = useState<Map<string, WalletRow>>(new Map())
  const [summary, setSummary] = useState<Summary | null>(null)
  const [types, setTypes] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const showToast = useToast()

  const [selected, setSelected] = useState<Unified | null>(null)
  const [drawerTab, setDrawerTab] = useState<'perfil' | 'carteira'>('perfil')
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [syncingAll, setSyncingAll] = useState(false)

  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')
  const [vendas, setVendas] = useState('')
  const [valorMin, setValorMin] = useState('')
  const [produtor, setProdutor] = useState('')
  const [producers, setProducers] = useState<{ user_id: number; nome: string; email: string }[]>([])

  const [draftQ, setDraftQ] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [draftVendas, setDraftVendas] = useState('')
  const [draftValorMin, setDraftValorMin] = useState('')
  const [draftProdutor, setDraftProdutor] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  function buildQs() {
    const p = new URLSearchParams()
    if (q.trim()) p.set('q', q.trim())
    if (status) p.set('status', status)
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    if (vendas) p.set('vendas', vendas)
    if (valorMin.trim()) p.set('valor_min', valorMin.trim())
    if (produtor.trim()) p.set('produtor', produtor.trim())
    p.set('limit', '100')
    return p.toString()
  }

  async function loadList() {
    setLoading(true)
    try {
      const [r, w] = await Promise.all([
        api<{ items: A[]; total: number }>(`/affiliates?${buildQs()}`),
        api<{ items: WalletRow[] }>('/affiliates-wallet?limit=1000').catch(() => ({ items: [] as WalletRow[] })),
      ])
      setItems(r.items ?? [])
      setTotal(r.total)
      const m = new Map<string, WalletRow>()
      for (const wr of w.items ?? []) {
        const key = (wr.email || '').trim().toLowerCase()
        if (key) m.set(key, wr)
      }
      setWalletByEmail(m)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function loadSummary() {
    try { setSummary(await api<Summary>('/affiliates-wallet/summary')) }
    catch { /* KPIs de carteira degradam em silêncio */ }
  }

  async function loadTypes() {
    try {
      const r = await api<{ items: string[] }>('/affiliates-wallet/transaction-types')
      setTypes(r.items || [])
    } catch {
      setTypes(['commission', 'penalty', 'withdrawal', 'approval', 'manual_credit', 'manual_debit', 'frustration_reversal'])
    }
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { loadList() }, [q, status, dataIni, dataFim, vendas, valorMin, produtor])
  useEffect(() => { loadSummary(); loadTypes() }, [])

  useEffect(() => {
    let active = true
    api<{ items: { user_id: number; nome: string; email: string }[] }>('/producers?limit=300')
      .then(r => { if (active) setProducers(r.items ?? []) })
      .catch(() => {})
    return () => { active = false }
  }, [])

  const produtorOpts = [
    { value: '', label: 'Todos' },
    ...Array.from(new Set(producers.map(p => p.nome).filter(n => n && n.trim()))).map(n => ({ value: n, label: n })),
  ]

  // Linhas unificadas — identidade + carteira casada por email.
  const unified: Unified[] = useMemo(() => {
    return items.map(a => {
      const key = (a.email || '').trim().toLowerCase()
      return { ...a, wallet: (key && walletByEmail.get(key)) || null }
    })
  }, [items, walletByEmail])

  async function handleDelete(e: React.MouseEvent, a: Unified) {
    e.stopPropagation()
    const ok = await confirmAsync({
      variant: 'danger',
      title: 'Excluir afiliado',
      message: `Excluir o afiliado ${a.nome || a.email || `#${a.user_id}`}? Sai da listagem; histórico preservado.`,
      confirmLabel: 'Excluir',
    })
    if (!ok) return
    setDeletingId(a.user_id)
    try {
      await api(`/clientes/${a.user_id}`, { method: 'DELETE' })
      emitToast('ok', 'Afiliado excluído.')
      await loadList()
    } catch (err: any) {
      emitToast('err', err.message || 'Erro ao excluir afiliado')
    } finally {
      setDeletingId(null)
    }
  }

  async function handleSyncAll() {
    const withWallet = unified.filter(u => u.wallet)
    if (!withWallet.length) return
    if (!await confirmAsync({ message: `Sincronizar TODAS as ${withWallet.length} carteiras de afiliados?\n\nEssa operação re-agrega balance e pending_balance a partir do livro razão.\n\nContinuar?` })) return
    setSyncingAll(true)
    let ok = 0, fail = 0
    for (const u of withWallet) {
      try {
        await api(`/affiliates-wallet/${u.wallet!.affiliate_id}/wallet-fix`, { method: 'POST' })
        ok++
      } catch { fail++ }
    }
    showToast(fail === 0 ? 'ok' : 'err', `Sync em lote: ${ok} sucesso(s), ${fail} falha(s) de ${withWallet.length}.`)
    setSyncingAll(false)
    await Promise.all([loadList(), loadSummary()])
  }

  function openPanel() {
    setDraftQ(q); setDraftStatus(status); setDraftIni(dataIni); setDraftFim(dataFim)
    setDraftVendas(vendas); setDraftValorMin(valorMin); setDraftProdutor(produtor)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setStatus(draftStatus); setDataIni(draftIni); setDataFim(draftFim)
    setVendas(draftVendas); setValorMin(draftValorMin); setProdutor(draftProdutor)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setStatus(''); setDataIni(''); setDataFim('')
    setVendas(''); setValorMin(''); setProdutor('')
    setDraftQ(''); setDraftStatus(''); setDraftIni(''); setDraftFim('')
    setDraftVendas(''); setDraftValorMin(''); setDraftProdutor('')
    setFilterOpen(false)
  }

  const sumVendido = items.reduce((s, a) => s + (a.total_vendido_30d || 0), 0)
  const sumComissao = items.reduce((s, a) => s + (a.total_comissao_30d || 0), 0)
  const sumPedidos = items.reduce((s, a) => s + (a.pedidos_count_30d || 0), 0)

  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (status) {
    const sl = STATUS_OPTS.find(s => s.value === status)?.label ?? status
    chips.push({ key: 'status', label: `Status: ${sl}`, onRemove: () => setStatus('') })
  }
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  if (vendas) {
    const vl = VENDAS_OPTS.find(v => v.value === vendas)?.label ?? vendas
    chips.push({ key: 'vendas', label: `Vendas: ${vl}`, onRemove: () => setVendas('') })
  }
  if (valorMin) chips.push({ key: 'valorMin', label: `Vendido ≥ ${brl(Number(valorMin) || 0)}`, onRemove: () => setValorMin('') })
  if (produtor) chips.push({ key: 'produtor', label: `Produtor: ${produtor}`, onRemove: () => setProdutor('') })
  const activeCount = chips.length

  function openDrawer(a: Unified, tab: 'perfil' | 'carteira' = 'perfil') {
    setSelected(a)
    setDrawerTab(tab)
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Afiliados</h1>
          <p>{total} afiliados cadastrados · identidade e carteira num só lugar</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          <button
            className="szv2-btn szv2-btn-secondary"
            onClick={handleSyncAll}
            disabled={syncingAll || unified.every(u => !u.wallet)}
          >
            {syncingAll ? 'Sincronizando…' : 'Sync TODAS as carteiras'}
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && items.length > 0 && <div className="sz-alert-danger">{err}</div>}

      {/* KPIs — vendas 30d + carteira consolidada, num só painel */}
      <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(5, minmax(0,1fr))', marginBottom: 12, gap: 12 }}>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Vendido (30d) — página</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{brl(sumVendido)}</span>
          <span className="szv2-kpi-meta">{sumPedidos} pedido(s)</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Comissões geradas (30d)</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-success)' }}>{brl(sumComissao)}</span>
          <span className="szv2-kpi-meta">soma da página</span>
        </div></div>
        <KpiCard label="Pendente (carteira)" value={summary ? money(summary.total_pendente) : '—'} sub="retenção / aguardando" tone="warning" />
        <KpiCard label="Disponível (carteira)" value={summary ? money(summary.total_disponivel) : '—'} sub="pode sacar" tone="success" />
        <KpiCard label="Dívida total" value={summary ? money(summary.total_debt) : '—'} sub="saldo negativo" tone="danger" />
      </div>

      {/* Aviso informativo — repasses somente de transações ativas */}
      <div
        className="sz-alert-info"
        style={{
          marginBottom: 16, background: 'var(--szv2-info-bg, #eff6ff)', border: '1px solid #bfdbfe',
          borderRadius: 8, padding: '12px 16px', color: '#1e40af', fontSize: 13,
        }}
      >
        ⚠️ Repasses somente de transações ativas. Comissões canceladas/estornadas <strong>NÃO</strong> entram.
      </div>

      {loading && items.length === 0 ? (
        <TableSkeleton rows={6} cols={9} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={() => { setErr(''); loadList() }} />
      ) : !loading && items.length === 0 ? (
        <EmptyState icon="🤝" title="Nenhum afiliado cadastrado ainda." description="Quando houver afiliados, eles aparecem aqui." />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th rowSpan={2}>Nome / Email</th>
              <th colSpan={2} style={{ textAlign: 'center', borderBottom: '1px solid var(--szv2-divider)' }}>Vendas gerais</th>
              <th colSpan={3} style={{ textAlign: 'center', borderBottom: '1px solid var(--szv2-divider)' }}>Carteira</th>
              <th rowSpan={2}>Status</th>
              <th rowSpan={2}>Último pedido</th>
              <th rowSpan={2} style={{ textAlign: 'right' }}>Ações</th>
            </tr>
            <tr>
              <th className="szv2-td-num">Valor vendido</th>
              <th className="szv2-td-num">Pedidos válidos</th>
              <th className="szv2-td-num">Pendente</th>
              <th className="szv2-td-num">Disponível</th>
              <th className="szv2-td-num">Saques</th>
            </tr>
          </thead>
          <tbody>
            {unified.map(a => {
              const w = a.wallet
              return (
                <tr key={a.user_id} onClick={() => openDrawer(a)} style={{ cursor: 'pointer' }} title="Ver detalhes do afiliado">
                  <td style={{ fontSize: 13 }}>
                    <div style={{ fontWeight: 600 }}>{a.nome || <span style={{ color: 'var(--szv2-text-faint)' }}>(sem nome)</span>}</div>
                    <div style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>{a.email}</div>
                  </td>
                  <td className="szv2-td-num" style={{ fontSize: 13 }}>
                    {a.valor_vendido_total > 0 ? brl(a.valor_vendido_total) : <Dash />}
                  </td>
                  <td className="szv2-td-num" style={{ fontSize: 13 }}>
                    {w ? w.pedidos_validos : <Dash />}
                  </td>
                  <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-warning)', fontWeight: 600 }}>
                    {w && w.pending_balance > 0 ? money(w.pending_balance) : <Dash />}
                  </td>
                  <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-success)', fontWeight: 600 }}>
                    {w && w.balance > 0 ? money(w.balance) : <Dash />}
                  </td>
                  <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-text-soft)' }}>
                    {w && w.saques_total > 0 ? money(w.saques_total) : <Dash />}
                  </td>
                  <td>
                    <span className={`szv2-status-badge ${a.status === 'active' || a.status === 'ativo' || a.status === 'aprovado' || a.status === 'approved' ? 's-confirmado' : a.status === 'pending' || a.status === 'pendente' ? 's-pendente' : 's-cancelado'}`}>
                      {a.status === 'active' || a.status === 'ativo' || a.status === 'aprovado' || a.status === 'approved' ? 'Confirmado'
                        : a.status === 'pending' || a.status === 'pendente' ? 'Pendente'
                        : a.status === 'sem_vinculo' ? 'Sem vínculo'
                        : a.status}
                    </span>
                  </td>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                    {a.last_order_at ? brDate(a.last_order_at) : <Dash />}
                  </td>
                  <td style={{ textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                    <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                      {w && (
                        <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-secondary" onClick={() => openDrawer(a, 'carteira')}>
                          Ver tx
                        </button>
                      )}
                      <button
                        type="button" className="szv2-btn szv2-btn-danger szv2-btn-sm"
                        disabled={deletingId === a.user_id} onClick={(e) => handleDelete(e, a)}
                      >
                        {deletingId === a.user_id ? 'Excluindo…' : 'Excluir'}
                      </button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      )}

      {selected && (
        <AffiliateDrawer
          affiliate={selected}
          types={types}
          initialTab={drawerTab}
          onClose={() => setSelected(null)}
          onSaved={() => { loadList() }}
          onWalletChanged={() => { loadList(); loadSummary() }}
        />
      )}

      <FilterTopPanel open={filterOpen} onClose={() => setFilterOpen(false)} onApply={applyFilters} onClear={clearFilters} title="Filtros">
        <FilterField label="Data inicial">
          <FalkDatePicker value={draftIni} max={draftFim || undefined} onChange={v => setDraftIni(v)} placeholder="dd/mm/aaaa" />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker value={draftFim} min={draftIni || undefined} onChange={v => setDraftFim(v)} placeholder="dd/mm/aaaa" />
        </FilterField>
        <FilterField label="Status">
          <FalkSelect value={draftStatus} onChange={v => setDraftStatus(v)} options={STATUS_OPTS} aria-label="Status" />
        </FilterField>
        <FilterField label="Vendas">
          <FalkSelect value={draftVendas} onChange={v => setDraftVendas(v)} options={VENDAS_OPTS} aria-label="Vendas" />
        </FilterField>
        <FilterField label="Valor vendido mínimo (R$)">
          <input type="number" inputMode="decimal" min={0} step="0.01" style={filterInputStyle}
            placeholder="ex.: 1000" value={draftValorMin} onChange={e => setDraftValorMin(e.target.value)} />
        </FilterField>
        <FilterField label="Produtor vinculado">
          <FalkSelect value={draftProdutor} onChange={v => setDraftProdutor(v)} options={produtorOpts} placeholder="Todos" aria-label="Produtor vinculado" />
        </FilterField>
        <FilterField label="Busca (email / nome)">
          <input type="search" style={filterInputStyle} placeholder="ex.: joao@…" value={draftQ} onChange={e => setDraftQ(e.target.value)} />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
