import { useEffect, useMemo, useState } from 'react'
import { api, getToken } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// ---------------------------------------------------------------------------
// Tipos espelhados de internal/handlers/cod_wallet_transactions.go
// ---------------------------------------------------------------------------

type Tx = {
  id: number
  user_id: number
  user_nome: string
  order_id: number | null
  type: string
  status: string
  gross: number
  fee: number
  net: number
  release_at: string | null
  created_at: string
}

type ListResp = {
  items: Tx[]
  total: number
  page: number
  per_page: number
}

type StatsBucketType = {
  type: string
  count: number
  gross: number
  fee: number
  total: number // net
}

type StatsBucketStatus = {
  status: string
  count: number
  gross: number
  fee: number
  total: number // net
}

type StatsTotals = {
  count: number
  gross: number
  fee: number
  net: number
}

type StatsResp = {
  totals:    StatsTotals
  by_type:   StatsBucketType[]
  by_status: StatsBucketStatus[]
}

// ---------------------------------------------------------------------------
// Helpers de formatação
// ---------------------------------------------------------------------------

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const money = (v: number) => 'R$ ' + fmt(v)

function fmtDateBR(iso: string | null | undefined): string {
  if (!iso) return '—'
  return iso.replace('T', ' ').slice(0, 16)
}

// Type badge color map — espelha o enum aceito pelo backend (/types).
const TYPE_BADGE: Record<string, string> = {
  credit:        'szv2-badge-success',
  withdrawal:    'szv2-badge-danger',
  manual_credit: 'szv2-badge-info',
  manual_debit:  'szv2-badge-warning',
  refund:        'szv2-badge-info',
  adjustment:    'szv2-badge-warning',
}

// Status badge color map.
const STATUS_BADGE: Record<string, string> = {
  pending:              'szv2-badge-warning',
  available:            'szv2-badge-success',
  paid:                 'szv2-badge-success',
  anticipation_pending: 'szv2-badge-info',
  cancelled:            'szv2-badge-danger',
  rejected:             'szv2-badge-danger',
  analysis:             'szv2-badge-info',
}

// Todos os status conhecidos (filtro dropdown).
const STATUS_LIST = [
  'pending',
  'available',
  'paid',
  'anticipation_pending',
  'cancelled',
  'rejected',
  'analysis',
]

// Tipos que somam negativamente ao saldo (mostrados com sinal "−").
const NEGATIVE_TYPES = new Set(['withdrawal', 'manual_debit'])

// Read query string filter (user_id) na primeira render — vindo do drawer
// de CodWalletProducer ("Ver todas as transações").
function readInitialUserID(): string {
  if (typeof window === 'undefined') return ''
  const p = new URLSearchParams(window.location.search)
  return p.get('user_id') || ''
}

// ---------------------------------------------------------------------------
// Página principal
// ---------------------------------------------------------------------------

export default function CodWalletTransactions() {
  // Filtros (top bar)
  const [userID, setUserID]         = useState<string>(readInitialUserID())
  const [search, setSearch]         = useState<string>('')  // busca nome/email
  const [orderID, setOrderID]       = useState<string>('')
  const [typeF, setTypeF]           = useState<string>('') // '' = todos
  const [statusF, setStatusF]       = useState<string>('') // '' = todos
  const [dateFrom, setDateFrom]     = useState<string>('')
  const [dateTo, setDateTo]         = useState<string>('')
  const [releaseFrom, setReleaseFrom] = useState<string>('')
  const [releaseTo, setReleaseTo]   = useState<string>('')

  // Paginação
  const [page, setPage]         = useState<number>(1)
  const [perPage, setPerPage]   = useState<number>(50)

  // Dados
  const [items, setItems]     = useState<Tx[]>([])
  const [total, setTotal]     = useState<number>(0)
  const [stats, setStats]     = useState<StatsResp | null>(null)
  const [types, setTypes]     = useState<string[]>([])
  const [loading, setLoading] = useState<boolean>(true)
  const [err, setErr]         = useState<string>('')
  const [exporting, setExporting] = useState<boolean>(false)

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil(total / perPage)),
    [total, perPage],
  )

  // Reset page quando filtro mudar — wrapper para evitar paginação stale.
  function withResetPage<T>(setter: (v: T) => void): (v: T) => void {
    return (v: T) => { setter(v); setPage(1) }
  }

  // ── Loaders ─────────────────────────────────────────────────────────────

  function buildQuery(extra?: Record<string, string>): string {
    const qs = new URLSearchParams()
    if (userID)      qs.set('user_id', userID)
    if (search)      qs.set('q', search)
    if (orderID)     qs.set('order_id', orderID)
    if (typeF)       qs.set('type', typeF)
    if (statusF)     qs.set('status', statusF)
    if (dateFrom)    qs.set('date_from', dateFrom)
    if (dateTo)      qs.set('date_to', dateTo)
    if (releaseFrom) qs.set('release_from', releaseFrom)
    if (releaseTo)   qs.set('release_to', releaseTo)
    if (extra) Object.entries(extra).forEach(([k, v]) => qs.set(k, v))
    return qs.toString()
  }

  async function loadList() {
    setLoading(true); setErr('')
    try {
      const qs = buildQuery({ page: String(page), per_page: String(perPage) })
      const r = await api<ListResp>(`/cod-wallet-transactions?${qs}`)
      setItems(r.items || [])
      setTotal(r.total || 0)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar transações')
    } finally {
      setLoading(false)
    }
  }

  async function loadStats() {
    try {
      const r = await api<StatsResp>(`/cod-wallet-transactions/stats?${buildQuery()}`)
      setStats(r)
    } catch {
      // Stats são best-effort — falha não bloqueia a tabela.
      setStats(null)
    }
  }

  async function loadTypes() {
    try {
      const r = await api<{ items: string[] }>('/cod-wallet-transactions/types')
      setTypes(r.items || [])
    } catch {
      setTypes([
        'credit', 'withdrawal', 'manual_credit',
        'manual_debit', 'refund', 'adjustment',
      ])
    }
  }

  // Carrega types só uma vez; recarrega lista/stats sempre que filtro muda.
  useEffect(() => { loadTypes() }, [])

  useEffect(() => {
    loadList()
    loadStats()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userID, search, orderID, typeF, statusF, dateFrom, dateTo, releaseFrom, releaseTo, page, perPage])

  function handleClearFilters() {
    setUserID('')
    setSearch('')
    setOrderID('')
    setTypeF('')
    setStatusF('')
    setDateFrom('')
    setDateTo('')
    setReleaseFrom('')
    setReleaseTo('')
    setPage(1)
    setDraftUserID(''); setDraftSearch(''); setDraftOrderID('')
    setDraftTypeF(''); setDraftStatusF('')
    setDraftDateFrom(''); setDraftDateTo('')
    setDraftReleaseFrom(''); setDraftReleaseTo('')
    setFilterOpen(false)
  }

  // Painel
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftUserID, setDraftUserID] = useState('')
  const [draftSearch, setDraftSearch] = useState('')
  const [draftOrderID, setDraftOrderID] = useState('')
  const [draftTypeF, setDraftTypeF] = useState('')
  const [draftStatusF, setDraftStatusF] = useState('')
  const [draftDateFrom, setDraftDateFrom] = useState('')
  const [draftDateTo, setDraftDateTo] = useState('')
  const [draftReleaseFrom, setDraftReleaseFrom] = useState('')
  const [draftReleaseTo, setDraftReleaseTo] = useState('')

  function openPanel() {
    setDraftUserID(userID); setDraftSearch(search); setDraftOrderID(orderID)
    setDraftTypeF(typeF); setDraftStatusF(statusF)
    setDraftDateFrom(dateFrom); setDraftDateTo(dateTo)
    setDraftReleaseFrom(releaseFrom); setDraftReleaseTo(releaseTo)
    setFilterOpen(true)
  }
  function applyFilters() {
    setUserID(draftUserID); setSearch(draftSearch); setOrderID(draftOrderID)
    setTypeF(draftTypeF); setStatusF(draftStatusF)
    setDateFrom(draftDateFrom); setDateTo(draftDateTo)
    setReleaseFrom(draftReleaseFrom); setReleaseTo(draftReleaseTo)
    setPage(1)
    setFilterOpen(false)
  }
  const chips: ActiveChip[] = []
  if (userID) chips.push({ key: 'uid', label: `User: #${userID}`, onRemove: () => setUserID('') })
  if (search) chips.push({ key: 'q', label: `Busca: ${search}`, onRemove: () => setSearch('') })
  if (orderID) chips.push({ key: 'oid', label: `Order: #${orderID}`, onRemove: () => setOrderID('') })
  if (typeF) chips.push({ key: 'type', label: `Tipo: ${typeF}`, onRemove: () => setTypeF('') })
  if (statusF) chips.push({ key: 'status', label: `Status: ${statusF}`, onRemove: () => setStatusF('') })
  if (dateFrom) chips.push({ key: 'df', label: `De: ${dateFrom}`, onRemove: () => setDateFrom('') })
  if (dateTo) chips.push({ key: 'dt', label: `Até: ${dateTo}`, onRemove: () => setDateTo('') })
  if (releaseFrom) chips.push({ key: 'rf', label: `Liberação ≥ ${releaseFrom}`, onRemove: () => setReleaseFrom('') })
  if (releaseTo) chips.push({ key: 'rt', label: `Liberação ≤ ${releaseTo}`, onRemove: () => setReleaseTo('') })

  // Export CSV — fetch direto com Bearer token, dispara download.
  async function handleExportCSV() {
    setExporting(true)
    try {
      const tok = getToken()
      const base = (import.meta as any).env?.VITE_API_BASE || '/wp-json/senderzz/v1/admin'
      const qs = buildQuery()
      const res = await fetch(`${base}/cod-wallet-transactions/export-csv?${qs}`, {
        headers: tok ? { Authorization: `Bearer ${tok}` } : {},
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      const suffix = dateFrom ? `-${dateFrom}` : ''
      a.download = `transacoes-cod${suffix}.csv`
      a.click()
      URL.revokeObjectURL(url)
    } catch (e: any) {
      alert('Erro ao exportar CSV: ' + (e.message || 'falha desconhecida'))
    } finally {
      setExporting(false)
    }
  }

  // ── Render ──────────────────────────────────────────────────────────────

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Transações COD</h1>
          <p>Livro razão completo da carteira COD (todos os produtores)</p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <FilterButton
            active={chips.length > 0}
            count={chips.length}
            onClick={openPanel}
          />
          <select
            className="szv2-select"
            value={perPage}
            onChange={e => { setPerPage(Number(e.target.value)); setPage(1) }}
            style={{ height: 38 }}
            title="Itens por página"
          >
            <option value={50}>50/pág</option>
            <option value={100}>100/pág</option>
            <option value={200}>200/pág</option>
          </select>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={handleExportCSV}
            disabled={exporting || loading}
          >
            {exporting ? 'Exportando…' : '↓ Exportar CSV'}
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={handleClearFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={handleClearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial (criado)">
          <input
            type="date"
            style={filterInputStyle}
            value={draftDateFrom}
            onChange={e => setDraftDateFrom(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final (criado)">
          <input
            type="date"
            style={filterInputStyle}
            value={draftDateTo}
            onChange={e => setDraftDateTo(e.target.value)}
          />
        </FilterField>
        <FilterField label="Liberação de">
          <input
            type="date"
            style={filterInputStyle}
            value={draftReleaseFrom}
            onChange={e => setDraftReleaseFrom(e.target.value)}
          />
        </FilterField>
        <FilterField label="Liberação até">
          <input
            type="date"
            style={filterInputStyle}
            value={draftReleaseTo}
            onChange={e => setDraftReleaseTo(e.target.value)}
          />
        </FilterField>
        <FilterField label="Tipo">
          <select
            style={filterInputStyle}
            value={draftTypeF}
            onChange={e => setDraftTypeF(e.target.value)}
          >
            <option value="">Todos</option>
            {types.map(t => <option key={t} value={t}>{t}</option>)}
          </select>
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatusF}
            onChange={e => setDraftStatusF(e.target.value)}
          >
            <option value="">Todos</option>
            {STATUS_LIST.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </FilterField>
        <FilterField label="User ID">
          <input
            type="number"
            style={filterInputStyle}
            value={draftUserID}
            onChange={e => setDraftUserID(e.target.value)}
            placeholder="ex.: 1234"
          />
        </FilterField>
        <FilterField label="Order ID">
          <input
            type="number"
            style={filterInputStyle}
            value={draftOrderID}
            onChange={e => setDraftOrderID(e.target.value)}
            placeholder="ex.: 5678"
          />
        </FilterField>
        <FilterField label="Busca (nome / e-mail)">
          <input
            type="search"
            style={filterInputStyle}
            value={draftSearch}
            onChange={e => setDraftSearch(e.target.value)}
            placeholder="buscar produtor…"
          />
        </FilterField>
      </FilterTopPanel>

      {/* ── Stats: totalizador + mini KPIs por tipo + status ────────── */}
      {stats && (
        <div
          className="szv2-card"
          style={{ marginBottom: 16, padding: 16 }}
        >
          {/* Resumo do escopo filtrado */}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(4, 1fr)',
              gap: 12,
              marginBottom: 16,
              padding: '12px 0',
              borderBottom: '1px solid var(--szv2-border)',
            }}
          >
            {[
              { label: 'Transações', value: stats.totals.count.toLocaleString('pt-BR'), highlight: false },
              { label: 'Gross total', value: money(stats.totals.gross), highlight: false },
              { label: 'Fee total', value: money(stats.totals.fee), highlight: false },
              { label: 'Net total', value: money(stats.totals.net), highlight: true },
            ].map(({ label, value, highlight }) => (
              <div key={label} style={{ textAlign: 'center' }}>
                <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 4 }}>
                  {label}
                </div>
                <div style={{ fontSize: highlight ? 18 : 15, fontWeight: 700, color: highlight ? 'var(--szv2-brand)' : undefined }}>
                  {value}
                </div>
              </div>
            ))}
          </div>

          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '1fr 1fr',
              gap: 16,
            }}
          >
            {/* By type */}
            <div>
              <div
                style={{
                  fontSize: 11,
                  color: 'var(--szv2-text-muted)',
                  textTransform: 'uppercase',
                  letterSpacing: 0.5,
                  marginBottom: 8,
                }}
              >
                Por tipo
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {stats.by_type.length === 0 ? (
                  <span style={{ color: 'var(--szv2-text-faint)', fontSize: 12 }}>
                    sem dados no período
                  </span>
                ) : stats.by_type.map(b => (
                  <div
                    key={b.type}
                    style={{
                      padding: '6px 10px',
                      borderRadius: 8,
                      background: 'var(--szv2-bg-soft)',
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                    }}
                  >
                    <span className={`sz-badge ${TYPE_BADGE[b.type] || 'szv2-badge-neutral'}`}>
                      {b.type || '—'}
                    </span>
                    <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                      {b.count.toLocaleString('pt-BR')}
                    </span>
                    <span style={{ fontSize: 11, color: 'var(--szv2-text-faint)' }}>
                      gross {money(b.gross)} · fee {money(b.fee)}
                    </span>
                    <strong style={{ fontSize: 12 }}>net {money(b.total)}</strong>
                  </div>
                ))}
              </div>
            </div>

            {/* By status */}
            <div>
              <div
                style={{
                  fontSize: 11,
                  color: 'var(--szv2-text-muted)',
                  textTransform: 'uppercase',
                  letterSpacing: 0.5,
                  marginBottom: 8,
                }}
              >
                Por status
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {stats.by_status.length === 0 ? (
                  <span style={{ color: 'var(--szv2-text-faint)', fontSize: 12 }}>
                    sem dados no período
                  </span>
                ) : stats.by_status.map(b => (
                  <div
                    key={b.status}
                    style={{
                      padding: '6px 10px',
                      borderRadius: 8,
                      background: 'var(--szv2-bg-soft)',
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                    }}
                  >
                    <span className={`sz-badge ${STATUS_BADGE[b.status] || 'szv2-badge-neutral'}`}>
                      {b.status || '—'}
                    </span>
                    <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                      {b.count.toLocaleString('pt-BR')}
                    </span>
                    <span style={{ fontSize: 11, color: 'var(--szv2-text-faint)' }}>
                      gross {money(b.gross)} · fee {money(b.fee)}
                    </span>
                    <strong style={{ fontSize: 12 }}>net {money(b.total)}</strong>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Tabela ───────────────────────────────────────────────────── */}
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Usuário</th>
              <th>Order</th>
              <th>Tipo</th>
              <th>Status</th>
              <th style={{ textAlign: 'right' }}>Gross</th>
              <th style={{ textAlign: 'right' }}>Fee</th>
              <th style={{ textAlign: 'right' }}>Net</th>
              <th>Liberação</th>
              <th>Criado em</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={10}>
                  <div className="szv2-empty"><h3>Carregando…</h3></div>
                </td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={10}>
                  <div className="szv2-empty">
                    <h3>Nenhuma transação no período.</h3>
                    <p>Ajuste o range de datas ou os filtros aplicados.</p>
                  </div>
                </td>
              </tr>
            ) : items.map(t => {
              const isNeg = NEGATIVE_TYPES.has(t.type)
              return (
                <tr key={t.id}>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{t.id}</td>
                  <td style={{ fontSize: 13 }}>
                    <div style={{ fontWeight: 500 }}>{t.user_nome || '—'}</div>
                    <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                      #{t.user_id}
                    </div>
                  </td>
                  <td>
                    {t.order_id ? (
                      <span style={{ fontWeight: 600, color: 'var(--szv2-brand)' }}>
                        #{t.order_id}
                      </span>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                    )}
                  </td>
                  <td>
                    <span className={`sz-badge ${TYPE_BADGE[t.type] || 'szv2-badge-neutral'}`}>
                      {t.type}
                    </span>
                  </td>
                  <td>
                    <span className={`sz-badge ${STATUS_BADGE[t.status] || 'szv2-badge-neutral'}`}>
                      {t.status}
                    </span>
                  </td>
                  <td style={{ textAlign: 'right' }}>{money(t.gross)}</td>
                  <td style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>
                    {money(t.fee)}
                  </td>
                  <td
                    style={{
                      textAlign: 'right',
                      fontWeight: 700,
                      color: isNeg ? 'var(--szv2-danger)' : 'var(--szv2-success)',
                    }}
                  >
                    {isNeg ? '−' : '+'} {money(Math.abs(t.net))}
                  </td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    {fmtDateBR(t.release_at)}
                  </td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    {fmtDateBR(t.created_at)}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* ── Paginação ────────────────────────────────────────────────── */}
      {total > perPage && (
        <div
          className="szv2-card"
          style={{
            marginTop: 12,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={page <= 1 || loading}
            onClick={() => setPage(p => Math.max(1, p - 1))}
          >
            ← Anterior
          </button>
          <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            Página <strong>{page}</strong> de <strong>{totalPages}</strong>
            <span style={{ marginLeft: 12, color: 'var(--szv2-text-faint)' }}>
              ({total.toLocaleString('pt-BR')} transações)
            </span>
          </span>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={page >= totalPages || loading}
            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
          >
            Próximo →
          </button>
        </div>
      )}
    </div>
  )
}
