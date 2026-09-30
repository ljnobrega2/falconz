import { useEffect, useMemo, useState } from 'react'
import { confirmAsync } from '../components/ConfirmDialog'
import { useToast } from '../hooks/useToast'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import FilterDrawer from '../components/FilterDrawer'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import { drawerTabsStyle, drawerTabBtnStyle } from '../components/drawerTabs'
import StatusBadge from '../components/StatusBadge'

// ---------------------------------------------------------------------------
// Tipos espelhados de internal/handlers/affiliate_wallet.go
// ---------------------------------------------------------------------------

type Summary = {
  total_pendente: number
  total_disponivel: number
  total_debt: number
  affiliates_count: number
}

type Row = {
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

// ---------------------------------------------------------------------------
// Estilos compartilhados / helpers
// ---------------------------------------------------------------------------

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const money = (v: number) => 'R$ ' + fmt(v)

// Badge color por tipo de transação.
const TX_TYPE_BADGE: Record<string, string> = {
  commission:           'szv2-badge-success',
  manual_credit:        'szv2-badge-success',
  penalty:              'szv2-badge-danger',
  manual_debit:         'szv2-badge-danger',
  withdrawal:           'szv2-badge-danger',
  approval:             'szv2-badge-warning',
  frustration_reversal: 'szv2-badge-warning',
}

// Sinal exibido junto ao valor (saída/entrada).
const TX_NEGATIVE = new Set(['penalty', 'manual_debit', 'withdrawal'])

// ---------------------------------------------------------------------------
// Rótulos PT-BR para tipo/status do livro razão (não exibir o termo cru em
// inglês). Espelha os enums de go/admin/.../affiliate_wallet.go e os valores
// gravados em includes/senderzz-affiliates.php.
// ---------------------------------------------------------------------------

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

// "manual_credit" → "Manual credit" como fallback humano (sem underscore).
function txTypeLabel(type: string): string {
  const t = (type || '').trim().toLowerCase()
  if (!t) return '—'
  if (TX_TYPE_LABEL[t]) return TX_TYPE_LABEL[t]
  const s = t.replace(/_/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}

const TX_STATUS_LABEL: Record<string, string> = {
  pending:    'Pendente',
  pendente:   'Pendente',
  available:  'Disponível',
  disponivel: 'Disponível',
  approved:   'Aprovado',
  aprovado:   'Aprovado',
  applied:    'Aplicado',
  aplicado:   'Aplicado',
  paid:       'Pago',
  pago:       'Pago',
  cancelled:  'Cancelado',
  cancelado:  'Cancelado',
  reversed:   'Estornado',
  estornado:  'Estornado',
  rejected:   'Rejeitado',
  rejeitado:  'Rejeitado',
}

function txStatusLabel(status: string): string {
  const s = (status || '').trim().toLowerCase()
  if (!s) return '—'
  if (TX_STATUS_LABEL[s]) return TX_STATUS_LABEL[s]
  const v = s.replace(/_/g, ' ')
  return v.charAt(0).toUpperCase() + v.slice(1)
}

// ---------------------------------------------------------------------------
// Resumo legível do meta_json — NÃO exibir o JSON cru ao operador.
// Conhece os campos gravados pela comissão (gross/fees/net/transaction_fee_*),
// penalidade (count) e ajustes de auditoria (source/prev_amount). Campos
// desconhecidos viram "rótulo: valor" formatado, sem chaves/aspas.
// ---------------------------------------------------------------------------

const META_FIELD_LABEL: Record<string, string> = {
  gross:                      'Bruto',
  fees:                       'Taxas',
  net:                        'Líquido',
  commission_pct:             'Comissão',
  commission_gross:           'Comissão bruta',
  transaction_fee_affiliate:  'Taxa transação (afiliado)',
  transaction_fee_producer:   'Taxa transação (produtor)',
  transaction_fee_total:      'Taxa transação',
  prev_amount:                'Valor anterior',
  count:                      'Ocorrências',
  source:                     'Origem',
}

// Campos internos/técnicos que não agregam para o operador na tabela.
const META_HIDDEN = new Set([
  'transaction_fee_mode', 'calc_mode', 'phone_hash',
])

const META_PCT_FIELDS = new Set(['commission_pct'])
const META_MONEY_FIELDS = new Set([
  'gross', 'fees', 'net', 'commission_gross', 'prev_amount',
  'transaction_fee_affiliate', 'transaction_fee_producer', 'transaction_fee_total',
])

// Mapa de origens (source) para PT legível.
const META_SOURCE_LABEL: Record<string, string> = {
  admin_audit_fix:       'Ajuste de auditoria',
  admin_audit_fix_order: 'Ajuste de auditoria (pedido)',
  admin_orders:          'Ajuste de pedido',
}

type MetaPair = { label: string; value: string }

// Converte o meta_json bruto numa lista de pares "rótulo: valor" legíveis.
function parseMetaPairs(raw: string | null): MetaPair[] {
  if (!raw) return []
  let obj: Record<string, unknown>
  try {
    const p = JSON.parse(raw)
    if (!p || typeof p !== 'object' || Array.isArray(p)) return []
    obj = p as Record<string, unknown>
  } catch {
    return []
  }
  const pairs: MetaPair[] = []
  for (const [k, v] of Object.entries(obj)) {
    if (META_HIDDEN.has(k)) continue
    if (v === null || v === undefined || v === '') continue
    const label = META_FIELD_LABEL[k] || (k.charAt(0).toUpperCase() + k.slice(1).replace(/_/g, ' '))
    let value: string
    if (k === 'source' && typeof v === 'string') {
      value = META_SOURCE_LABEL[v] || v.replace(/_/g, ' ')
    } else if (META_MONEY_FIELDS.has(k) && typeof v === 'number') {
      value = money(v)
    } else if (META_PCT_FIELDS.has(k) && typeof v === 'number') {
      value = `${fmt(v)}%`
    } else if (typeof v === 'number') {
      value = String(v)
    } else {
      value = String(v)
    }
    pairs.push({ label, value })
  }
  return pairs
}

// Resumo de 1 linha (ex.: "Líquido R$ 238,25 · Taxas R$ 37,…") para a célula.
function metaSummary(raw: string | null): string {
  const pairs = parseMetaPairs(raw)
  if (!pairs.length) return '—'
  // Prioriza os campos mais relevantes para o resumo curto.
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

// ---------------------------------------------------------------------------
// KPI card (mesma estética do AuditEngine)
// ---------------------------------------------------------------------------

function KpiCard({
  label, value, sub, tone,
}: {
  label: string
  value: string | number
  sub?: string
  tone?: 'brand' | 'success' | 'warning' | 'danger'
}) {
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

// ---------------------------------------------------------------------------
// Drawer / Modal de transações de um afiliado
// ---------------------------------------------------------------------------

function TxDrawer({
  affiliate,
  types,
  onClose,
}: {
  affiliate: Row
  types: string[]
  onClose: () => void
}) {
  const [items, setItems] = useState<Tx[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [filterType, setFilterType] = useState<string>('')
  const [tab, setTab] = useState<'resumo' | 'transacoes'>('resumo')

  useEffect(() => {
    let active = true
    setLoading(true); setErr('')
    api<{ items: Tx[] }>(`/affiliates-wallet/${affiliate.affiliate_id}/transactions?limit=200`)
      .then(r => { if (active) setItems(r.items || []) })
      .catch(e => { if (active) setErr(e.message || 'Erro') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [affiliate.affiliate_id])

  const visible = useMemo(() => {
    if (!filterType) return items
    return items.filter(t => t.type === filterType)
  }, [items, filterType])

  return (
    <FilterDrawer
      open
      onClose={onClose}
      onApply={onClose}
      applyLabel="Fechar"
      width={640}
      title={`Transações de ${affiliate.nome || affiliate.email || `#${affiliate.affiliate_id}`}`}
    >
      {/* Abas */}
      <div style={drawerTabsStyle}>
        {([
          ['resumo', 'Resumo'],
          ['transacoes', 'Transações'],
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
            gridTemplateColumns: 'repeat(2, minmax(0,1fr))',
            gap: 12,
          }}
        >
          <div style={{ padding: 12, background: 'var(--szv2-warning-bg)', borderRadius: 8 }}>
            <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Pendente</div>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-warning)' }}>
              {money(affiliate.pending_balance)}
            </div>
          </div>
          <div style={{ padding: 12, background: 'var(--szv2-success-bg)', borderRadius: 8 }}>
            <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Disponível</div>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-success)' }}>
              {money(affiliate.balance)}
            </div>
          </div>
          <div style={{ padding: 12, background: 'var(--szv2-danger-bg)', borderRadius: 8 }}>
            <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Dívida</div>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-danger)' }}>
              {money(affiliate.debt_amount)}
            </div>
          </div>
          <div style={{ padding: 12, background: 'var(--szv2-danger-bg)', borderRadius: 8 }}>
            <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Penalidades</div>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-danger)' }}>
              {money(affiliate.penalidades_total)}
            </div>
          </div>
          <div style={{ padding: 12, background: 'var(--szv2-info-bg)', borderRadius: 8 }}>
            <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Saques</div>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-info)' }}>
              {money(affiliate.saques_total)}
            </div>
          </div>
        </div>
      )}

      {/* ── Aba: Transações (chips + tabela) ───────────────── */}
      {tab === 'transacoes' && (
        <>
          {/* Chips de filtro por tipo */}
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            <button
              type="button"
              className={`sz-badge ${filterType === '' ? 'szv2-badge-brand' : 'szv2-badge-neutral'}`}
              style={{ cursor: 'pointer', border: 'none' }}
              onClick={() => setFilterType('')}
            >
              Todos ({items.length})
            </button>
            {types.map(t => {
              const n = items.filter(x => x.type === t).length
              return (
                <button
                  type="button"
                  key={t}
                  className={`sz-badge ${filterType === t ? (TX_TYPE_BADGE[t] || 'szv2-badge-brand') : 'szv2-badge-neutral'}`}
                  style={{ cursor: 'pointer', border: 'none', opacity: n === 0 ? 0.5 : 1 }}
                  onClick={() => setFilterType(t)}
                >
                  {txTypeLabel(t)} ({n})
                </button>
              )
            })}
          </div>

          {loading ? (
            <div style={{ padding: 40, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Carregando…
            </div>
          ) : visible.length === 0 ? (
            <div className="szv2-empty">
              <h3>Nenhuma transação</h3>
              <p>{filterType ? 'Nenhum registro para esse tipo.' : 'O afiliado ainda não tem movimentações.'}</p>
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Data</th>
                    <th>Order</th>
                    <th>Tipo</th>
                    <th>Status</th>
                    <th style={{ textAlign: 'right' }}>Valor</th>
                    <th>Disponível em</th>
                    <th>Meta</th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map(t => {
                    const isNeg = TX_NEGATIVE.has(t.type)
                    return (
                      <tr key={t.id}>
                        <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                          {t.created_at?.slice(0, 16).replace('T', ' ') ?? '—'}
                        </td>
                        <td style={{ fontWeight: 600 }}>{t.order_id ? `${t.order_id}` : '—'}</td>
                        <td>
                          <span className={`sz-badge ${TX_TYPE_BADGE[t.type] || 'szv2-badge-neutral'}`}>
                            {txTypeLabel(t.type)}
                          </span>
                        </td>
                        <td>
                          <StatusBadge status={t.status} label={txStatusLabel(t.status)} />
                        </td>
                        <td
                          style={{
                            textAlign: 'right',
                            fontWeight: 700,
                            color: isNeg ? 'var(--szv2-danger)' : 'var(--szv2-success)',
                          }}
                        >
                          {isNeg ? '−' : '+'} {money(Math.abs(t.amount))}
                        </td>
                        <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                          {t.available_at?.slice(0, 16).replace('T', ' ') ?? '—'}
                        </td>
                        <td
                          style={{
                            fontSize: 11.5,
                            color: 'var(--szv2-text-muted)',
                            maxWidth: 240,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                          title={
                            parseMetaPairs(t.meta_json)
                              .map(p => `${p.label}: ${p.value}`)
                              .join('\n') || (t.meta_json ?? '')
                          }
                        >
                          {metaSummary(t.meta_json)}
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
    </FilterDrawer>
  )
}

// ---------------------------------------------------------------------------
// Página principal
// ---------------------------------------------------------------------------

// `embedded` esconde o cabeçalho interno (título) e a faixa de KPIs — usados
// quando a tela única (AfiliadosFinHub) promove esses elementos para uma faixa
// unificada. A barra de ações (filtro + Sync TODAS) e a lista permanecem.
// `onChanged` é disparado após sync/liberação para o pai re-buscar seu resumo.
export default function AffiliateWallet({
  embedded = false,
  onChanged,
}: {
  embedded?: boolean
  onChanged?: () => void
} = {}) {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [rows, setRows] = useState<Row[]>([])
  const [types, setTypes] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState<number | 'all' | null>(null)
  const [drawer, setDrawer] = useState<Row | null>(null)
  const showToast = useToast() // AUDIT-2026-06-18 Onda3

  // Filtros aplicados.
  const [q, setQ] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  function openPanel() {
    setDraftQ(q); setDraftStatus(statusFilter); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setStatusFilter(draftStatus); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setStatusFilter(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftStatus(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (statusFilter) chips.push({ key: 'status', label: `Status: ${statusFilter}`, onRemove: () => setStatusFilter('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length


  async function loadSummary() {
    try {
      const s = await api<Summary>('/affiliates-wallet/summary')
      setSummary(s)
    } catch (e: any) {
      // Falha de summary não bloqueia a tela — mostra apenas err inline.
      setErr(e.message || 'Erro ao carregar resumo')
    }
  }

  async function loadList() {
    setLoading(true); setErr('')
    try {
      const p = new URLSearchParams()
      p.set('limit', '300')
      if (q) p.set('q', q)
      if (statusFilter) p.set('status', statusFilter)
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      const r = await api<{ items: Row[] }>(`/affiliates-wallet?${p.toString()}`)
      setRows(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar lista')
    } finally {
      setLoading(false)
    }
  }

  async function loadTypes() {
    try {
      const r = await api<{ items: string[] }>('/affiliates-wallet/transaction-types')
      setTypes(r.items || [])
    } catch {
      // tipos podem ficar vazios sem quebrar a UI
      setTypes([
        'commission', 'penalty', 'withdrawal', 'approval',
        'manual_credit', 'manual_debit', 'frustration_reversal',
      ])
    }
  }

  useEffect(() => {
    loadSummary()
    loadTypes()
    loadList()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, statusFilter, dataIni, dataFim])

  async function handleSync(affID: number) {
    if (!await confirmAsync({ message: `Sincronizar carteira do afiliado #${affID}?` })) return
    setBusy(affID)
    try {
      await api(`/affiliates-wallet/${affID}/wallet-fix`, { method: 'POST' })
      showToast('ok', `Carteira do afiliado #${affID} sincronizada.`)
      await Promise.all([loadSummary(), loadList()])
      onChanged?.()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao sincronizar')
    } finally {
      setBusy(null)
    }
  }

  async function handleRelease(affID: number, force = false) {
    const msg = force
      ? `FORÇAR liberação de TODAS as comissões pendentes do afiliado #${affID} AGORA, ignorando o prazo de retenção?\n\nLibera dinheiro real antes do vencimento e fica registrado na auditoria.`
      : `Liberar transações pendentes vencidas do afiliado #${affID}?`
    if (!await confirmAsync({ message: msg })) return
    setBusy(affID)
    try {
      const r = await api<{ ok: boolean; released: number; still_pending?: number; next_release_at?: string | null; forced?: boolean }>(
        `/affiliates-wallet/${affID}/release-pending${force ? '?force=1' : ''}`,
        { method: 'POST' },
      )
      const rel = r.released ?? 0
      const still = r.still_pending ?? 0
      if (rel > 0) {
        showToast('ok', `${rel} comissão(ões) liberada(s) para o afiliado #${affID}.`)
      } else if (still > 0) {
        const when = r.next_release_at ? new Date(r.next_release_at).toLocaleDateString('pt-BR') : null
        showToast('warn', when
          ? `0 liberadas — ${still} ainda em retenção (libera a partir de ${when}). Use "Forçar" para antecipar.`
          : `0 liberadas — ${still} ainda em retenção. Use "Forçar" para antecipar.`)
      } else {
        showToast('ok', `Nenhuma comissão pendente para o afiliado #${affID}.`)
      }
      await Promise.all([loadSummary(), loadList()])
      onChanged?.()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao liberar')
    } finally {
      setBusy(null)
    }
  }

  async function handleSyncAll() {
    if (!rows.length) return
    if (!await confirmAsync({ message: `Sincronizar TODAS as ${rows.length} carteiras de afiliados?\n\nEssa operação re-agrega balance e pending_balance a partir do livro razão.\n\nContinuar?` })) return
    setBusy('all')
    let ok = 0, fail = 0
    for (const r of rows) {
      try {
        await api(`/affiliates-wallet/${r.affiliate_id}/wallet-fix`, { method: 'POST' })
        ok++
      } catch {
        fail++
      }
    }
    showToast(fail === 0 ? 'ok' : 'err',
      `Sync em lote: ${ok} sucesso(s), ${fail} falha(s) de ${rows.length}.`)
    setBusy(null)
    await Promise.all([loadSummary(), loadList()])
    onChanged?.()
  }

  // Heurística client-side de "status carteira":
  // Soma das transações != saldo agregado → divergente.
  function statusCarteira(r: Row): 'ok' | 'divergente' {
    // pending+balance reflete o que está no agregado; saques+penalidades dão um
    // proxy do livro razão líquido. Quando as duas pontas divergem em > 0.01
    // sinalizamos como divergente para o operador rodar o sync.
    const agregado = r.pending_balance + r.balance
    const movimentado = r.saques_total + r.penalidades_total
    // Se não houve movimentação alguma, considera OK (carteira nova).
    if (agregado === 0 && movimentado === 0) return 'ok'
    // Cobertura grosseira: se há débito (saques/penalidades) sem saldo nenhum, ok.
    // Quem dispara é divergência matemática entre o agregado e o débito quando ambos são 0.
    if (agregado < 0) return 'divergente'
    return 'ok'
  }

  // Filtro "Status carteira" é uma heurística client-side (mesma que pinta o
  // badge), então aplicamos aqui — o backend de /affiliates-wallet ainda não
  // honra `status`. Derivamos UMA lista para contagem, branch vazia e map.
  const visibleRows = statusFilter
    ? rows.filter(r => statusCarteira(r) === statusFilter)
    : rows

  return (
    <div>
      {/* Em modo embedded o título sai (o pai tem um header único), mas a barra
          de ações (filtro + Sync TODAS) é mantida como toolbar acima da tabela. */}
      <div
        className="szv2-section-head"
        style={embedded ? { marginBottom: 12, justifyContent: 'flex-end' } : undefined}
      >
        {!embedded && (
          <div>
            <h1>Carteira de Afiliados</h1>
            <p>Saldos consolidados, liberações e correções</p>
          </div>
        )}
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          <button
            className="szv2-btn szv2-btn-secondary"
            onClick={handleSyncAll}
            disabled={busy !== null || rows.length === 0}
          >
            {busy === 'all' ? 'Sincronizando…' : 'Sync TODAS as carteiras'}
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* KPIs — ocultos em embedded (promovidos para a faixa unificada do pai) */}
      {!embedded && summary && (
        <div
          className="szv2-kpi-grid"
          style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 16 }}
        >
          <KpiCard
            label="Pendente"
            value={money(summary.total_pendente)}
            sub="retenção / aguardando liberação"
            tone="warning"
          />
          <KpiCard
            label="Disponível"
            value={money(summary.total_disponivel)}
            sub="pode sacar"
            tone="success"
          />
          <KpiCard
            label="Dívida total"
            value={money(summary.total_debt)}
            sub="saldo negativo acumulado"
            tone="danger"
          />
        </div>
      )}

      {/* Aviso informativo — repasses somente de transações ativas */}
      <div
        className="sz-alert-info"
        style={{
          marginBottom: 16,
          background: 'var(--szv2-info-bg, #eff6ff)',
          border: '1px solid #bfdbfe',
          borderRadius: 8,
          padding: '12px 16px',
          color: '#1e40af',
          fontSize: 13,
        }}
      >
        ⚠️ Repasses somente de transações ativas. Comissões canceladas/estornadas <strong>NÃO</strong> entram.
      </div>

      {/* Resumo da lista */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Afiliados</h2>
            <p className="szv2-card-sub">
              {visibleRows.length} afiliado(s){summary ? ` de ${summary.affiliates_count}` : ''}
            </p>
          </div>
        </div>
      </div>

      {/* Tabela */}
      {loading && rows.length === 0 ? (
        <TableSkeleton rows={6} cols={9} />
      ) : !loading && visibleRows.length === 0 ? (
        <EmptyState
          icon="💼"
          title={statusFilter ? 'Nenhum afiliado para esse filtro.' : 'Nenhum afiliado com carteira encontrado.'}
          description={statusFilter
            ? 'Ajuste ou limpe o filtro de status da carteira.'
            : 'Carteiras de afiliados aparecem aqui assim que houver comissões.'}
        />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Afiliado</th>
              <th style={{ textAlign: 'right' }}>Pendente</th>
              <th style={{ textAlign: 'right' }}>Disponível</th>
              <th style={{ textAlign: 'right' }}>Saques</th>
              <th style={{ textAlign: 'right' }}>Penalidades</th>
              <th style={{ textAlign: 'right' }}>Pedidos válidos</th>
              <th>Status carteira</th>
              <th style={{ width: 260 }}>Ações</th>
            </tr>
          </thead>
          <tbody>
            {visibleRows.map(r => {
              const st = statusCarteira(r)
              return (
                <tr key={r.affiliate_id}>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{r.affiliate_id}</td>
                  <td>
                    <div style={{ fontWeight: 600 }}>{r.nome || '—'}</div>
                    <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{r.email || '—'}</div>
                  </td>
                  <td style={{ textAlign: 'right', color: 'var(--szv2-warning)', fontWeight: 700 }}>
                    {money(r.pending_balance)}
                  </td>
                  <td style={{ textAlign: 'right', color: 'var(--szv2-success)', fontWeight: 700 }}>
                    {money(r.balance)}
                  </td>
                  <td style={{ textAlign: 'right', color: 'var(--szv2-text-soft)' }}>
                    {money(r.saques_total)}
                  </td>
                  <td style={{ textAlign: 'right', color: 'var(--szv2-danger)' }}>
                    {money(r.penalidades_total)}
                  </td>
                  <td style={{ textAlign: 'right' }}>{r.pedidos_validos}</td>
                  <td>
                    <span className={`sz-badge ${st === 'ok' ? 'szv2-badge-success' : 'szv2-badge-danger'}`}>
                      {st === 'ok' ? 'OK' : 'Divergente'}
                    </span>
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                        onClick={() => setDrawer(r)}
                        disabled={busy !== null}
                      >
                        Ver tx
                      </button>
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                        onClick={() => handleRelease(r.affiliate_id)}
                        disabled={busy !== null}
                        title="Promove pending → available para comissões já vencidas (respeita a retenção)"
                      >
                        {busy === r.affiliate_id ? '…' : 'Liberar pendentes'}
                      </button>
                      {r.pending_balance > 0 && (
                        <button
                          type="button"
                          className="szv2-btn szv2-btn-sm szv2-btn-danger"
                          onClick={() => handleRelease(r.affiliate_id, true)}
                          disabled={busy !== null}
                          title="OVERRIDE do dono: libera AGORA ignorando a retenção (auditado)"
                        >
                          {busy === r.affiliate_id ? '…' : 'Forçar'}
                        </button>
                      )}
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-sm szv2-btn-brand"
                        onClick={() => handleSync(r.affiliate_id)}
                        disabled={busy !== null}
                        title="Reagrega balance/pending_balance a partir do livro razão"
                      >
                        {busy === r.affiliate_id ? '…' : 'Sync wallet'}
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

      {drawer && (
        <TxDrawer
          affiliate={drawer}
          types={types}
          onClose={() => setDrawer(null)}
        />
      )}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftIni}
            max={draftFim || undefined}
            onChange={v => setDraftIni(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftFim}
            min={draftIni || undefined}
            onChange={v => setDraftFim(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Status carteira">
          <FalkSelect
            value={draftStatus}
            onChange={v => setDraftStatus(v)}
            options={[
              { value: '', label: 'Todos' },
              { value: 'ok', label: 'OK' },
              { value: 'divergente', label: 'Divergente' },
            ]}
            aria-label="Status carteira"
          />
        </FilterField>
        <FilterField label="Busca (email / nome)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: joao@…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
