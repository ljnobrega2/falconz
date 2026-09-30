import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import FalkDatePicker from '../components/FalkDatePicker'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import FilterDrawer from '../components/FilterDrawer'
import { drawerTabsStyle, drawerTabBtnStyle } from '../components/drawerTabs'
import { confirmAsync } from '../components/ConfirmDialog'
import { emitToast } from '../hooks/useToast'

// ---------------------------------------------------------------------------
// Tela "Carteira COD — Produtores"
//
// As abas "Carteira / Saldos" e "Financeiro / P&L" foram UNIFICADAS numa única
// visão ("Carteira & Financeiro"): os KPIs e a tabela por produtor das duas
// abas viram um conjunto só, sem dados duplicados. "Regras de repasse"
// permanece como aba separada.
//
// Merge de id-space (crítico): os dois back-ends usam chaves diferentes para o
// mesmo produtor —
//   - cod-livro/producers-summary: producer_id = sz_orders.produtor_id = portal id (Gabriel = 15)
//   - cod-wallet-producer:        user_id     = sz_cod_wallet_transactions.user_id = wp_user_id (Gabriel = 21)
// O wallet handler passou a expor `portal_id` (senderzz_portal_users.id). A
// fusão das linhas é feita por portal id, evitando o mesmo produtor aparecer
// em duas linhas (dados duplicados).
// ---------------------------------------------------------------------------

// Linha do P&L financeiro (cod_livro.go::ProducersSummary).
type FinProducerRow = {
  producer_id: number
  producer_name: string
  producer_email: string
  pedidos: number
  recebidos: number
  frustrados: number
  previstos: number
  bruto: number
  bruto_previsto: number
  taxas_senderzz: number
  afiliado: number
  liquido_produtor: number
  frustrado_produtor: number
  frustrado_afiliados: number
  frustrado_valor: number
}

type FinSummary = {
  bruto_cod: number
  afiliados: number
  taxas_senderzz: number
  liquido_produtor: number
  previsto_produtor: number
}

type PixDefault = {
  holder: string
  key: string
  type: string // cpf | cnpj | email | telefone | aleatoria
}

// Linha da carteira COD (cod_wallet_producer.go::List).
type WalletRow = {
  user_id: number   // wp_user_id-space (chave para /accounts e /release-pending)
  portal_id: number // senderzz_portal_users.id — chave canônica do merge
  nome: string
  email: string
  saldo_pending: number
  saldo_available: number
  saldo_paid_30d: number
  pix_default: PixDefault | null
  ultima_movimentacao: string | null
}

type WalletSummary = {
  total_pending: number
  total_available: number
  total_paid_30d: number
  producers_count: number
}

type Account = {
  id: number
  holder_name: string
  holder_cpf: string
  pix_type: string
  pix_key: string
  is_default: boolean
}

// Linha unificada exibida na tabela. Reúne P&L (período) + carteira (saldos
// atuais/cumulativos). `portalId` é a chave de fusão.
type MergedRow = {
  portalId: number
  nome: string
  email: string
  // P&L (governado pelo filtro de data)
  fin: FinProducerRow | null
  // Carteira (saldos atuais — não dependem do período)
  wallet: WalletRow | null
}

// ---------------------------------------------------------------------------
// Tipos de regras globais e overrides (cod_saques.go)
// ---------------------------------------------------------------------------

type GlobalRules = {
  retention_days: number
  withdraw_fee: number
  anticipation_fee_pct: number
  motoboy_fee: number
  operational_fund_fee: number
}

type ProducerOverride = {
  user_id: number
  nome: string
  email: string
  retention_days: number | null
  withdraw_fee: number | null
  anticipation_fee_pct: number | null
  eff_retention_days: number
  eff_withdraw_fee: number
  eff_anticipation_fee: number
}

// ---------------------------------------------------------------------------
// Helpers de formatação
// ---------------------------------------------------------------------------

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const money = (v: number) => 'R$ ' + fmt(v)

// Máscara da key PIX para a coluna "PIX padrão".
function maskPixKey(type: string, key: string): string {
  if (!key) return '—'
  const t = (type || '').toLowerCase()
  if (t === 'cpf' || t === 'cnpj') {
    const digits = key.replace(/\D/g, '')
    if (digits.length <= 4) return '•••' + digits
    return '•••' + digits.slice(-4)
  }
  if (key.length > 22) return key.slice(0, 22) + '…'
  return key
}

const PIX_TYPE_BADGE: Record<string, string> = {
  cpf:        'szv2-badge-info',
  cnpj:       'szv2-badge-info',
  email:      'szv2-badge-neutral',
  telefone:   'szv2-badge-neutral',
  aleatoria:  'szv2-badge-warning',
}

function fmtDateBR(iso: string | null | undefined): string {
  if (!iso) return '—'
  const safe = iso.replace('T', ' ').slice(0, 16)
  return safe
}

// Data padrão: últimos 7 dias
function defaultDateRange(): { from: string; to: string } {
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - 7)
  const fmtD = (d: Date) => d.toISOString().slice(0, 10)
  return { from: fmtD(from), to: fmtD(to) }
}

// ---------------------------------------------------------------------------
// KPI card (mesma estética de AuditEngine / AffiliateWallet)
// ---------------------------------------------------------------------------

function KpiCard({
  label, value, sub, tone,
}: {
  label: string
  value: string | number
  sub?: string
  tone?: 'brand' | 'success' | 'warning' | 'info'
}) {
  const color = (() => {
    switch (tone) {
      case 'success': return 'var(--szv2-success)'
      case 'warning': return 'var(--szv2-warning)'
      case 'info':    return 'var(--szv2-info)'
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

// ---------------------------------------------------------------------------
// Drawer: contas PIX + atalho para a tela de transações
// ---------------------------------------------------------------------------

function AccountsDrawer({
  row, onClose,
}: {
  row: WalletRow
  onClose: () => void
}) {
  const [items, setItems] = useState<Account[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [tab, setTab] = useState<'resumo' | 'contas'>('resumo')

  useEffect(() => {
    let active = true
    setLoading(true); setErr('')
    api<{ items: Account[] }>(`/cod-wallet-producer/${row.user_id}/accounts`)
      .then(r => { if (active) setItems(r.items || []) })
      .catch(e => { if (active) setErr(e.message || 'Erro ao carregar contas') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [row.user_id])

  return (
    <FilterDrawer
      open
      onClose={onClose}
      onApply={onClose}
      applyLabel="Fechar"
      width={600}
      title={`Carteira COD — ${row.nome || row.email || `#${row.user_id}`}`}
    >
      {/* Abas */}
      <div style={drawerTabsStyle}>
        {([
          ['resumo', 'Resumo'],
          ['contas', 'Contas PIX'],
        ] as const).map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            style={drawerTabBtnStyle(tab === key)}
          >
            {label}
          </button>
        ))}
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {/* ── Aba: Resumo (KPIs) ─────────────────────────────── */}
      {tab === 'resumo' && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(3, minmax(0,1fr))',
            gap: 12,
          }}
        >
          <div style={{ padding: 12, background: 'var(--szv2-warning-bg)', borderRadius: 8 }}>
            <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Pendente</div>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-warning)' }}>
              {money(row.saldo_pending)}
            </div>
          </div>
          <div style={{ padding: 12, background: 'var(--szv2-success-bg)', borderRadius: 8 }}>
            <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Disponível</div>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-success)' }}>
              {money(row.saldo_available)}
            </div>
          </div>
          <div style={{ padding: 12, background: 'var(--szv2-info-bg)', borderRadius: 8 }}>
            <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Pago (30d)</div>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-info)' }}>
              {money(row.saldo_paid_30d)}
            </div>
          </div>
        </div>
      )}

      {/* ── Aba: Contas PIX cadastradas ────────────────────── */}
      {tab === 'contas' && (
        loading ? (
          <div style={{ padding: 40, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : items.length === 0 ? (
          <div className="szv2-empty">
            <h3>Nenhuma conta PIX</h3>
            <p>O produtor ainda não cadastrou contas para receber saques COD.</p>
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Titular</th>
                  <th>CPF</th>
                  <th>Tipo</th>
                  <th>Chave</th>
                  <th>Padrão</th>
                </tr>
              </thead>
              <tbody>
                {items.map(a => (
                  <tr key={a.id}>
                    <td style={{ fontWeight: 600 }}>{a.holder_name || '—'}</td>
                    <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                      {a.holder_cpf || '—'}
                    </td>
                    <td>
                      <span
                        className={`sz-badge ${PIX_TYPE_BADGE[a.pix_type] || 'szv2-badge-neutral'}`}
                      >
                        {a.pix_type || '—'}
                      </span>
                    </td>
                    <td
                      style={{
                        fontFamily: 'var(--szv2-font-mono)',
                        fontSize: 12,
                        maxWidth: 220,
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                      title={a.pix_key}
                    >
                      {a.pix_key || '—'}
                    </td>
                    <td>
                      {a.is_default
                        ? <span className="sz-badge szv2-badge-success">padrão</span>
                        : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      )}

    </FilterDrawer>
  )
}

// ---------------------------------------------------------------------------
// Modal: Antecipar (sem taxa) — o admin DIGITA o valor exato a antecipar
// ---------------------------------------------------------------------------

function AnticipateModal({
  row, busy, onClose, onConfirm,
}: {
  row: WalletRow
  busy: boolean
  onClose: () => void
  onConfirm: (amount: number) => void
}) {
  const pendente = row.saldo_pending ?? 0
  // Pré-sugere o pendente inteiro como valor (máximo permitido). O input é
  // type="number" → formato com PONTO decimal (ex.: "252.21"); vírgula seria
  // rejeitada pelo navegador e o campo apareceria vazio.
  const [raw, setRaw] = useState<string>(pendente > 0 ? pendente.toFixed(2) : '')

  // Parse direto do formato do input number (ponto decimal). Centavos p/ comparar sem drift.
  const amount = parseFloat(raw)
  const amountCents = Number.isFinite(amount) ? Math.round(amount * 100) : NaN
  const pendCents = Math.round(pendente * 100)

  const tooHigh = Number.isFinite(amountCents) && amountCents > pendCents
  const invalid = !Number.isFinite(amountCents) || amountCents <= 0 || tooHigh

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (invalid || busy) return
    onConfirm(amountCents / 100)
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      style={{
        position: 'fixed', inset: 0, zIndex: 1000,
        background: 'rgba(0,0,0,.45)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        padding: 16,
      }}
    >
      <form
        onClick={e => e.stopPropagation()}
        onSubmit={submit}
        className="szv2-card"
        style={{ width: 420, maxWidth: '100%', padding: 20 }}
      >
        <h2 style={{ marginTop: 0, marginBottom: 4 }}>Antecipar (sem taxa)</h2>
        <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', marginTop: 0 }}>
          {row.nome || row.email || `#${row.user_id}`} · pendente atual{' '}
          <strong>{money(pendente)}</strong>
        </p>

        <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13, marginTop: 12 }}>
          Valor a antecipar (R$)
          <input
            type="number"
            min="0.01"
            step="0.01"
            max={pendente}
            className="szv2-input"
            autoFocus
            value={raw}
            onChange={e => setRaw(e.target.value)}
            disabled={busy}
          />
        </label>

        {tooHigh && (
          <div style={{ fontSize: 12, color: 'var(--szv2-danger)', marginTop: 6 }}>
            Valor maior que o pendente ({money(pendente)}).
          </div>
        )}

        <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 10 }}>
          O valor digitado sai do pendente e entra no disponível para saque,{' '}
          <strong>sem cobrar taxa de antecipação</strong>.
        </p>

        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={onClose}
            disabled={busy}
          >
            Cancelar
          </button>
          <button
            type="submit"
            className="szv2-btn szv2-btn-danger"
            disabled={invalid || busy}
          >
            {busy ? 'Antecipando…' : 'Antecipar (sem taxa)'}
          </button>
        </div>
      </form>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Seção Regras de repasse (espelha sz_cod_admin_page do WP)
// ---------------------------------------------------------------------------

function RulesSection() {
  const [rules, setRules] = useState<GlobalRules>({
    retention_days: 7,
    withdraw_fee: 2.99,
    anticipation_fee_pct: 4.99,
    motoboy_fee: 18,
    operational_fund_fee: 2,
  })
  const [overrides, setOverrides] = useState<ProducerOverride[]>([])
  const [overrideEdits, setOverrideEdits] = useState<Record<number, Partial<ProducerOverride>>>({})
  const [loading, setLoading] = useState(true)
  const [savingRules, setSavingRules] = useState(false)
  const [savingOverrides, setSavingOverrides] = useState(false)
  const [err, setErr] = useState('')

  function showToast(kind: 'ok' | 'err', msg: string) {
    emitToast(kind, msg)
  }

  async function loadAll() {
    setLoading(true); setErr('')
    try {
      const [gr, ov] = await Promise.all([
        api<{ rules: GlobalRules }>('/cod-saques/global-rules'),
        api<{ items: ProducerOverride[] }>('/cod-saques/producer/overrides'),
      ])
      setRules(gr.rules)
      setOverrides(ov.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar regras')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { loadAll() }, [])

  async function handleSaveRules(e: React.FormEvent) {
    e.preventDefault()
    setSavingRules(true)
    try {
      await api('/cod-saques/global-rules', { method: 'POST', body: JSON.stringify(rules) })
      showToast('ok', 'Regras globais salvas.')
    } catch (e: any) {
      showToast('err', e.message || 'Erro ao salvar regras')
    } finally {
      setSavingRules(false)
    }
  }

  async function handleSaveOverrides(e: React.FormEvent) {
    e.preventDefault()
    setSavingOverrides(true)
    const items = overrides.map(o => {
      const edit = overrideEdits[o.user_id] || {}
      return {
        user_id: o.user_id,
        retention_days: edit.retention_days !== undefined ? edit.retention_days : o.retention_days,
        withdraw_fee: edit.withdraw_fee !== undefined ? edit.withdraw_fee : o.withdraw_fee,
        anticipation_fee_pct:
          edit.anticipation_fee_pct !== undefined
            ? edit.anticipation_fee_pct
            : o.anticipation_fee_pct,
      }
    })
    try {
      await api('/cod-saques/producer/overrides', {
        method: 'POST',
        body: JSON.stringify({ items }),
      })
      showToast('ok', 'Overrides por produtor salvos.')
      await loadAll()
    } catch (e: any) {
      showToast('err', e.message || 'Erro ao salvar overrides')
    } finally {
      setSavingOverrides(false)
    }
  }

  function setOverrideField(
    userId: number,
    field: keyof ProducerOverride,
    value: string,
  ) {
    setOverrideEdits(prev => ({
      ...prev,
      [userId]: {
        ...prev[userId],
        [field]: value === '' ? null : field === 'retention_days' ? parseInt(value, 10) : parseFloat(value.replace(',', '.')),
      },
    }))
  }

  if (loading) {
    return (
      <div className="szv2-card" style={{ marginBottom: 24 }}>
        <div style={{ padding: 32, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando regras…
        </div>
      </div>
    )
  }

  return (
    <div style={{ marginBottom: 24 }}>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}

      {/* Formulário de regras globais + taxas motoboy */}
      <form onSubmit={handleSaveRules}>
        <div className="szv2-card" style={{ marginBottom: 16 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Regras padrão de repasse</h2>
              <p className="szv2-card-sub">
                Fallback global. Campos vazios no produtor usam estes valores automaticamente.
              </p>
            </div>
          </div>

          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))',
              gap: 16,
              marginBottom: 20,
            }}
          >
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Retenção após entrega (dias)
              <input
                type="number"
                min="0"
                className="szv2-input"
                value={rules.retention_days}
                onChange={e =>
                  setRules(r => ({ ...r, retention_days: Math.max(0, parseInt(e.target.value, 10) || 0) }))
                }
              />
            </label>
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Taxa de saque padrão (R$)
              <input
                type="number"
                min="0"
                step="0.01"
                className="szv2-input"
                value={rules.withdraw_fee}
                onChange={e =>
                  setRules(r => ({ ...r, withdraw_fee: Math.max(0, parseFloat(e.target.value) || 0) }))
                }
              />
            </label>
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Taxa de antecipação padrão (%)
              <input
                type="number"
                min="0"
                step="0.01"
                className="szv2-input"
                value={rules.anticipation_fee_pct}
                onChange={e =>
                  setRules(r => ({
                    ...r,
                    anticipation_fee_pct: Math.max(0, parseFloat(e.target.value) || 0),
                  }))
                }
              />
            </label>
          </div>

          <h3 style={{ fontSize: 14, fontWeight: 600, marginBottom: 8 }}>
            Taxas administrativas Motoboy
          </h3>
          <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 12 }}>
            Usadas nas variáveis de comissão administrativa do push:{' '}
            <code>{'{{comissao_admin_liquida}}'}</code> e{' '}
            <code>{'{{comissao_admin_liquida_total}}'}</code>.
          </p>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))',
              gap: 16,
              marginBottom: 20,
            }}
          >
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Taxa motoboy admin (R$)
              <input
                type="number"
                min="0"
                step="0.01"
                className="szv2-input"
                value={rules.motoboy_fee}
                onChange={e =>
                  setRules(r => ({ ...r, motoboy_fee: Math.max(0, parseFloat(e.target.value) || 0) }))
                }
              />
            </label>
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Fundo operacional (R$)
              <input
                type="number"
                min="0"
                step="0.01"
                className="szv2-input"
                value={rules.operational_fund_fee}
                onChange={e =>
                  setRules(r => ({
                    ...r,
                    operational_fund_fee: Math.max(0, parseFloat(e.target.value) || 0),
                  }))
                }
              />
            </label>
          </div>

          <button
            type="submit"
            className="szv2-btn szv2-btn-brand"
            disabled={savingRules}
          >
            {savingRules ? 'Salvando…' : 'Salvar regras'}
          </button>
        </div>
      </form>

      {/* Tabela de overrides por produtor */}
      {overrides.length > 0 && (
        <form onSubmit={handleSaveOverrides}>
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Overrides por produtor</h2>
                <p className="szv2-card-sub">
                  Campos vazios herdam os valores globais acima.
                </p>
              </div>
            </div>

            <div className="szv2-table-wrap">
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Produtor</th>
                    <th>Retenção (dias)</th>
                    <th>Taxa saque (R$)</th>
                    <th>Taxa antecipação (%)</th>
                    <th>Efetivo</th>
                  </tr>
                </thead>
                <tbody>
                  {overrides.map(o => {
                    const edit = overrideEdits[o.user_id] || {}
                    return (
                      <tr key={o.user_id}>
                        <td>
                          <div style={{ fontWeight: 600 }}>{o.nome || o.email || `#${o.user_id}`}</div>
                          <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                            {o.email} · ID {o.user_id}
                          </div>
                        </td>
                        <td>
                          <input
                            type="number"
                            min="0"
                            className="szv2-input"
                            style={{ width: 90 }}
                            placeholder={String(rules.retention_days)}
                            value={
                              edit.retention_days !== undefined
                                ? (edit.retention_days ?? '')
                                : (o.retention_days ?? '')
                            }
                            onChange={e => setOverrideField(o.user_id, 'retention_days', e.target.value)}
                          />
                        </td>
                        <td>
                          <input
                            type="number"
                            min="0"
                            step="0.01"
                            className="szv2-input"
                            style={{ width: 90 }}
                            placeholder={fmt(rules.withdraw_fee)}
                            value={
                              edit.withdraw_fee !== undefined
                                ? (edit.withdraw_fee ?? '')
                                : (o.withdraw_fee ?? '')
                            }
                            onChange={e => setOverrideField(o.user_id, 'withdraw_fee', e.target.value)}
                          />
                        </td>
                        <td>
                          <input
                            type="number"
                            min="0"
                            step="0.01"
                            className="szv2-input"
                            style={{ width: 90 }}
                            placeholder={fmt(rules.anticipation_fee_pct)}
                            value={
                              edit.anticipation_fee_pct !== undefined
                                ? (edit.anticipation_fee_pct ?? '')
                                : (o.anticipation_fee_pct ?? '')
                            }
                            onChange={e =>
                              setOverrideField(o.user_id, 'anticipation_fee_pct', e.target.value)
                            }
                          />
                        </td>
                        <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                          {o.eff_retention_days} dias · R${' '}
                          {fmt(o.eff_withdraw_fee)} · {fmt(o.eff_anticipation_fee)}%
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            <div style={{ marginTop: 16 }}>
              <button
                type="submit"
                className="szv2-btn szv2-btn-brand"
                disabled={savingOverrides}
              >
                {savingOverrides ? 'Salvando…' : 'Salvar overrides'}
              </button>
            </div>
          </div>
        </form>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Página principal
// ---------------------------------------------------------------------------

export default function CodWalletProducer() {
  // Aba ativa: 'carteira' (unificada) | 'regras'
  const [tab, setTab] = useState<'carteira' | 'regras'>('carteira')

  // ── Filtro de período (governa SÓ as colunas de P&L) ──
  const def = defaultDateRange()
  const [from, setFrom] = useState(def.from)
  const [to, setTo] = useState(def.to)
  const [draftFrom, setDraftFrom] = useState(def.from)
  const [draftTo, setDraftTo] = useState(def.to)
  const [q, setQ] = useState('')
  const [draftQ, setDraftQ] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  // ── Dados das duas fontes (fundidos por portal id) ──
  const [finSummary, setFinSummary] = useState<FinSummary | null>(null)
  const [finRows, setFinRows] = useState<FinProducerRow[]>([])
  const [walletSummary, setWalletSummary] = useState<WalletSummary | null>(null)
  const [walletRows, setWalletRows] = useState<WalletRow[]>([])

  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState<number | null>(null)
  const [drawer, setDrawer] = useState<WalletRow | null>(null)
  // Modal "Antecipar (sem taxa)": o admin digita um VALOR exato a antecipar.
  const [anticipateRow, setAnticipateRow] = useState<WalletRow | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    emitToast(kind, msg)
  }

  function openPanel() { setDraftFrom(from); setDraftTo(to); setDraftQ(q); setFilterOpen(true) }
  function applyFilters() { setFrom(draftFrom); setTo(draftTo); setQ(draftQ); setFilterOpen(false) }
  function clearFilters() {
    setDraftFrom(def.from); setDraftTo(def.to); setDraftQ('')
    setFrom(def.from); setTo(def.to); setQ('')
    setFilterOpen(false)
  }

  const chips: ActiveChip[] = []
  if (from !== def.from) chips.push({ key: 'from', label: `De: ${from}`, onRemove: () => setFrom(def.from) })
  if (to   !== def.to)   chips.push({ key: 'to',   label: `Até: ${to}`,   onRemove: () => setTo(def.to) })
  if (q)                 chips.push({ key: 'q',    label: `Busca: ${q}`,  onRemove: () => setQ('') })
  const activeCount = chips.length

  // P&L (financeiro) é recarregado quando o período muda; carteira (saldos
  // atuais) e seu summary não dependem de data.
  async function loadFinancial() {
    const [s, r] = await Promise.all([
      api<FinSummary>(`/cod-livro/summary?from=${from}&to=${to}`),
      api<{ items: FinProducerRow[] }>(`/cod-livro/producers-summary?from=${from}&to=${to}`),
    ])
    setFinSummary(s)
    setFinRows(r.items || [])
  }

  async function loadWallet() {
    const [s, r] = await Promise.all([
      api<WalletSummary>('/cod-wallet-producer/summary'),
      api<{ items: WalletRow[] }>('/cod-wallet-producer?limit=300'),
    ])
    setWalletSummary(s)
    setWalletRows(r.items || [])
  }

  async function loadAll() {
    setLoading(true); setErr('')
    try {
      await Promise.all([loadFinancial(), loadWallet()])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar dados')
    } finally {
      setLoading(false)
    }
  }

  // Carrega tudo no mount e a cada mudança de período. Recarregar a carteira
  // junto com o financeiro é barato (query pequena) e evita a classe de bugs de
  // "skip" por estado de loading em voo — não vale otimizar.
  useEffect(() => { loadAll() }, [from, to]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleRelease(row: WalletRow) {
    const ok = await confirmAsync({
      variant: 'warning',
      title: 'Liberar saldo pendente',
      message:
        `Tem certeza que deseja liberar as transações pendentes vencidas de ${row.nome || row.email || `#${row.user_id}`}?\n\n` +
        'Os registros cujo prazo de retenção já venceu passarão de pendente para disponível para saque.',
      confirmLabel: 'Liberar',
    })
    if (!ok) return
    setBusy(row.user_id)
    try {
      const resp = await api<{ ok: boolean; released_count: number }>(
        `/cod-wallet-producer/${row.user_id}/release-pending`,
        { method: 'POST' },
      )
      showToast('ok',
        `${resp.released_count ?? 0} transação(ões) liberada(s) para o produtor #${row.user_id}.`)
      await loadWallet()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao liberar')
    } finally {
      setBusy(null)
    }
  }

  // Antecipação administrativa SEM taxa: o admin DIGITA um VALOR e antecipa
  // exatamente esse valor (pendente → disponível), SEM cobrar taxa. O back-end
  // insere um par de lançamentos type='adjustment' (+valor available / -valor
  // pending). amount precisa ser > 0 e <= pendente atual.
  async function submitAnticipate(row: WalletRow, amount: number) {
    setBusy(row.user_id)
    try {
      const resp = await api<{
        ok: boolean
        anticipated: number
        novo_disponivel: number
        novo_pendente: number
      }>(
        `/cod-wallet-producer/${row.user_id}/anticipate`,
        { method: 'POST', body: JSON.stringify({ amount }) },
      )
      showToast('ok',
        `Antecipado ${money(resp.anticipated ?? amount)} sem taxa · ` +
        `disponível ${money(resp.novo_disponivel)} · pendente ${money(resp.novo_pendente)}.`)
      setAnticipateRow(null)
      await loadWallet()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao antecipar')
    } finally {
      setBusy(null)
    }
  }

  // ── Fusão das duas fontes por portal id (sem duplicar produtores) ──
  const mergedRows = useMemo<MergedRow[]>(() => {
    const byPortal = new Map<number, MergedRow>()

    const ensure = (portalId: number, nome: string, email: string): MergedRow => {
      let m = byPortal.get(portalId)
      if (!m) {
        m = { portalId, nome, email, fin: null, wallet: null }
        byPortal.set(portalId, m)
      }
      // Mantém o melhor nome/email disponível.
      if (!m.nome && nome) m.nome = nome
      if (!m.email && email) m.email = email
      return m
    }

    // P&L (chave = producer_id = portal id).
    for (const f of finRows) {
      const m = ensure(f.producer_id, f.producer_name, f.producer_email)
      m.fin = f
    }
    // Carteira (chave = portal_id, exposto pelo back-end).
    for (const wlt of walletRows) {
      const m = ensure(wlt.portal_id, wlt.nome, wlt.email)
      m.wallet = wlt
    }

    let arr = Array.from(byPortal.values())

    // Filtro client-side por nome/email.
    const term = q.trim().toLowerCase()
    if (term) {
      arr = arr.filter(m =>
        (m.nome || '').toLowerCase().includes(term) ||
        (m.email || '').toLowerCase().includes(term))
    }

    // Ordena por bruto do período DESC, depois por saldo disponível DESC.
    arr.sort((a, b) => {
      const ab = a.fin?.bruto ?? 0
      const bb = b.fin?.bruto ?? 0
      if (bb !== ab) return bb - ab
      const aw = a.wallet?.saldo_available ?? 0
      const bw = b.wallet?.saldo_available ?? 0
      return bw - aw
    })
    return arr
  }, [finRows, walletRows, q])

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Carteira COD — Produtores</h1>
          <p>Saldos, P&amp;L financeiro e regras de repasse por produtor</p>
        </div>
        {tab === 'carteira' && (
          <div style={{ display: 'flex', gap: 8 }}>
            <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
            <button
              className="szv2-btn szv2-btn-secondary"
              onClick={loadAll}
              disabled={loading}
            >
              {loading ? 'Buscando…' : 'Atualizar'}
            </button>
          </div>
        )}
      </div>

      {tab === 'carteira' && <ActiveFilterChips chips={chips} onClearAll={clearFilters} />}

      {/* Tabs de navegação — abas Carteira e Financeiro foram unificadas */}
      <div style={{ display: 'flex', gap: 4, marginBottom: 20, borderBottom: '1px solid var(--szv2-border)' }}>
        {([
          { key: 'carteira', label: 'Carteira & Financeiro' },
          { key: 'regras',   label: 'Regras de repasse' },
        ] as const).map(t => (
          <button
            key={t.key}
            type="button"
            onClick={() => setTab(t.key)}
            style={{
              padding: '8px 16px',
              border: 'none',
              background: 'transparent',
              cursor: 'pointer',
              fontWeight: tab === t.key ? 700 : 400,
              color: tab === t.key ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
              borderBottom: tab === t.key ? '2px solid var(--szv2-brand)' : '2px solid transparent',
              fontSize: 14,
            }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* ── Aba unificada: Carteira & Financeiro ── */}
      {tab === 'carteira' && (
        <>
          {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

          {/* KPIs financeiros do período (P&L) */}
          {finSummary && (
            <>
              <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--szv2-text-muted)', margin: '4px 0 8px', textTransform: 'uppercase', letterSpacing: '.04em' }}>
                Financeiro do período ({from} → {to})
              </div>
              <div
                className="szv2-kpi-grid"
                style={{ gridTemplateColumns: 'repeat(5, minmax(0,1fr))', marginBottom: 16 }}
              >
                <KpiCard label="Bruto COD"          value={money(finSummary.bruto_cod)}         sub="sem frustrados" />
                <KpiCard label="Afiliados"          value={money(finSummary.afiliados)}         sub="repasse"            tone="warning" />
                <KpiCard label="Taxas FALK"     value={money(finSummary.taxas_senderzz)}    sub="entrega + transação" tone="info" />
                <KpiCard label="Líquido produtor"   value={money(finSummary.liquido_produtor)}  sub="líquido"            tone="success" />
                <KpiCard label="Previsto produtor"  value={money(finSummary.previsto_produtor)} sub="agendados/em aberto" tone="warning" />
              </div>
            </>
          )}

          {/* KPIs de carteira (saldos atuais — não dependem do período) */}
          {walletSummary && (
            <>
              <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--szv2-text-muted)', margin: '4px 0 8px', textTransform: 'uppercase', letterSpacing: '.04em' }}>
                Carteira (saldos atuais)
              </div>
              <div
                className="szv2-kpi-grid"
                style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))', marginBottom: 16 }}
              >
                <KpiCard label="Pendente"    value={money(walletSummary.total_pending)}                     sub="aguardando release_at" tone="warning" />
                <KpiCard label="Disponível"  value={money(walletSummary.total_available)}                   sub="pronto para saque"     tone="success" />
                <KpiCard label="Pago (30d)"  value={money(walletSummary.total_paid_30d)}                    sub="saques concluídos"     tone="info" />
                <KpiCard label="Produtores"  value={walletSummary.producers_count.toLocaleString('pt-BR')} sub="com movimentação COD"  tone="brand" />
              </div>
            </>
          )}

          {/* Tabela unificada por produtor */}
          <div className="szv2-card" style={{ marginBottom: 16 }}>
            <div className="szv2-card-head">
              <div>
                <h2>Produtores</h2>
                <p className="szv2-card-sub">
                  {mergedRows.length} produtor(es) · colunas financeiras referentes ao período;
                  saldos de carteira são atuais
                </p>
              </div>
            </div>
          </div>

          {loading && mergedRows.length === 0 ? (
            <TableSkeleton rows={6} cols={11} />
          ) : !loading && mergedRows.length === 0 ? (
            <EmptyState
              icon="💼"
              title="Nenhum produtor encontrado."
              description="Ajuste o período/busca ou aguarde a primeira movimentação COD."
            />
          ) : (
          <div className="szv2-table-wrap">
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Produtor</th>
                  {/* P&L do período */}
                  <th style={{ textAlign: 'right' }}>Bruto COD</th>
                  <th style={{ textAlign: 'right' }}>Afiliados</th>
                  <th style={{ textAlign: 'right' }}>Taxas FALK</th>
                  <th style={{ textAlign: 'right' }}>Líquido produtor</th>
                  <th style={{ textAlign: 'right' }}>Pedidos</th>
                  {/* Carteira (saldos atuais) */}
                  <th style={{ textAlign: 'right' }}>Pendente</th>
                  <th style={{ textAlign: 'right' }}>Disponível</th>
                  <th style={{ textAlign: 'right' }}>Pago (30d)</th>
                  <th>PIX padrão</th>
                  <th style={{ width: 220 }}>Ações</th>
                </tr>
              </thead>
              <tbody>
                {mergedRows.map(m => {
                  const fin = m.fin
                  const wlt = m.wallet
                  return (
                    <tr key={m.portalId}>
                      <td>
                        <div style={{ fontWeight: 600 }}>{m.nome || '—'}</div>
                        <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                          {m.email || `portal #${m.portalId}`}
                        </div>
                      </td>
                      {/* P&L */}
                      <td style={{ textAlign: 'right' }}>
                        {fin ? money(fin.bruto) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-warning)' }}>
                        {fin ? money(fin.afiliado) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        {fin ? money(fin.taxas_senderzz) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-success)', fontWeight: 700 }}>
                        {fin ? money(fin.liquido_produtor) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        {fin ? fin.pedidos : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                      {/* Carteira */}
                      <td style={{ textAlign: 'right', color: 'var(--szv2-warning)', fontWeight: 700 }}>
                        {wlt ? money(wlt.saldo_pending) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-success)', fontWeight: 700 }}>
                        {wlt ? money(wlt.saldo_available) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-info)' }}>
                        {wlt ? money(wlt.saldo_paid_30d) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                      <td>
                        {wlt && wlt.pix_default ? (
                          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                            <span
                              className={`sz-badge ${PIX_TYPE_BADGE[wlt.pix_default.type] || 'szv2-badge-neutral'}`}
                            >
                              {wlt.pix_default.type || '—'}
                            </span>
                            <span
                              style={{
                                fontFamily: 'var(--szv2-font-mono)',
                                fontSize: 12,
                                color: 'var(--szv2-text-soft)',
                              }}
                              title={wlt.pix_default.key}
                            >
                              {maskPixKey(wlt.pix_default.type, wlt.pix_default.key)}
                            </span>
                          </div>
                        ) : (
                          <span style={{ color: 'var(--szv2-text-faint)' }}>sem conta</span>
                        )}
                      </td>
                      <td>
                        {wlt ? (
                          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-sm szv2-btn-brand"
                              onClick={() => handleRelease(wlt)}
                              disabled={busy !== null}
                              title="Promove pending → available para tx cujo release_at já venceu"
                            >
                              {busy === wlt.user_id ? '…' : 'Liberar vencidos'}
                            </button>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-sm szv2-btn-danger"
                              onClick={() => setAnticipateRow(wlt)}
                              disabled={busy !== null || (wlt.saldo_pending ?? 0) <= 0}
                              title="Antecipa um VALOR digitado do pendente → available, SEM cobrar taxa (override admin)"
                            >
                              {busy === wlt.user_id ? '…' : 'Antecipar (sem taxa)'}
                            </button>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                              onClick={() => setDrawer(wlt)}
                              disabled={busy !== null}
                            >
                              Ver tx
                            </button>
                          </div>
                        ) : (
                          <span style={{ fontSize: 12, color: 'var(--szv2-text-faint)' }}>sem carteira</span>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          )}
        </>
      )}

      {/* ── Aba: Regras de repasse ── */}
      {tab === 'regras' && <RulesSection />}

      {drawer && (
        <AccountsDrawer row={drawer} onClose={() => setDrawer(null)} />
      )}

      {anticipateRow && (
        <AnticipateModal
          row={anticipateRow}
          busy={busy === anticipateRow.user_id}
          onClose={() => { if (busy === null) setAnticipateRow(null) }}
          onConfirm={(amount) => submitAnticipate(anticipateRow, amount)}
        />
      )}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros — período (financeiro)"
      >
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftFrom}
            max={draftTo || undefined}
            onChange={v => setDraftFrom(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftTo}
            min={draftFrom || undefined}
            onChange={v => setDraftTo(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Busca (nome / email)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: gabriel…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
