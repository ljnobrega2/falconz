import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// ----- Tipos do handler Go ---------------------------------------------------

type AuditRow = {
  id: number
  portal_user_id: number
  user_nome: string
  user_email: string
  action: string
  order_id: number | null
  meta: string | null
  ip: string
  created_at: string
}

type ListResponse = {
  items: AuditRow[]
  total: number
  page: number
  per_page: number
}

type ActionStat = {
  action: string
  count: number
}

type StatsResponse = {
  items: ActionStat[]
  total: number
}

// ----- Helpers ---------------------------------------------------------------

const fmtDate = (s: string | null | undefined) =>
  s ? s.slice(0, 16).replace('T', ' ') : '—'

// Rótulos human-friendly para os actions mais comuns (espelha o map PHP em
// includes/senderzz-audit-log.php). Action desconhecido cai no fallback "raw".
const ACTION_LABELS: Record<string, string> = {
  approve:                'Aprovação',
  cancel:                 'Cancelamento',
  loss:                   'Perda/extravio',
  retry_label:            'Reprocessar etiqueta',
  support:                'Chamado de suporte',
  set_preferred_carrier:  'Modalidades de frete',
  set_blocked_carrier:    'Bloqueios de transp.',
  change_password:        'Senha alterada',
  toggle_2fa:             '2FA alterado',
}

const ACTION_BADGE_CLS: Record<string, string> = {
  approve:                'szv2-badge-success',
  cancel:                 'szv2-badge-danger',
  loss:                   'szv2-badge-danger',
  retry_label:            'szv2-badge-warning',
  support:                'szv2-badge-info',
  set_preferred_carrier:  'szv2-badge-info',
  set_blocked_carrier:    'szv2-badge-warning',
  change_password:        'szv2-badge-neutral',
  toggle_2fa:             'szv2-badge-neutral',
}

function actionLabel(a: string): string {
  return ACTION_LABELS[a] || a
}

function actionBadge(a: string): string {
  return ACTION_BADGE_CLS[a] || 'szv2-badge-neutral'
}

// Truncamento + tooltip helper.
function truncate(s: string | null, max = 50): string {
  if (!s) return '—'
  if (s.length <= max) return s
  return s.slice(0, max) + '…'
}

// Pretty-print JSON com fallback gracioso (se meta não for JSON válido).
function prettyJSON(s: string | null): string {
  if (!s) return '(vazio)'
  try {
    const obj = JSON.parse(s)
    return JSON.stringify(obj, null, 2)
  } catch {
    return s
  }
}

// Data default = hoje-7d / hoje (formato YYYY-MM-DD).
function defaultDateRange(): { from: string; to: string } {
  const today = new Date()
  const from = new Date(today)
  from.setDate(from.getDate() - 7)
  const toISO = (d: Date) => d.toISOString().slice(0, 10)
  return { from: toISO(from), to: toISO(today) }
}

// ----- Página principal -----------------------------------------------------

export default function AuditLogViewer() {
  const defaults = useMemo(defaultDateRange, [])
  const [dateFrom, setDateFrom] = useState(defaults.from)
  const [dateTo, setDateTo] = useState(defaults.to)
  const [action, setAction] = useState('')
  const [orderID, setOrderID] = useState('')
  const [portalUserID, setPortalUserID] = useState('')

  const [actionOpts, setActionOpts] = useState<string[]>([])
  const [stats, setStats] = useState<ActionStat[]>([])

  const [items, setItems] = useState<AuditRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const perPage = 50

  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [detail, setDetail] = useState<AuditRow | null>(null)

  // Painel de filtros
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftFrom, setDraftFrom] = useState(defaults.from)
  const [draftTo, setDraftTo] = useState(defaults.to)
  const [draftAction, setDraftAction] = useState('')
  const [draftOrderID, setDraftOrderID] = useState('')
  const [draftPortalUserID, setDraftPortalUserID] = useState('')

  // ── Carrega lista de actions (filtro) — uma única vez ─────────────────
  useEffect(() => {
    api<{ items: string[] }>('/audit-log/actions')
      .then(r => setActionOpts(r.items || []))
      .catch(() => setActionOpts([]))
  }, [])

  // ── Carrega stats sempre que datas mudam ──────────────────────────────
  async function loadStats() {
    try {
      const qs = new URLSearchParams()
      if (dateFrom) qs.set('date_from', dateFrom)
      if (dateTo) qs.set('date_to', dateTo)
      const r = await api<StatsResponse>(`/audit-log/stats?${qs.toString()}`)
      setStats(r.items || [])
    } catch {
      setStats([])
    }
  }

  // ── Carrega lista paginada ────────────────────────────────────────────
  async function loadList() {
    setLoading(true)
    setErr('')
    try {
      const qs = new URLSearchParams()
      if (action) qs.set('action', action)
      if (orderID) qs.set('order_id', orderID)
      if (portalUserID) qs.set('portal_user_id', portalUserID)
      if (dateFrom) qs.set('date_from', dateFrom)
      if (dateTo) qs.set('date_to', dateTo)
      qs.set('page', String(page))
      qs.set('per_page', String(perPage))
      const r = await api<ListResponse>(`/audit-log?${qs.toString()}`)
      setItems(r.items || [])
      setTotal(r.total || 0)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
      setItems([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }

  // Recarrega lista quando filtros ou página mudam.
  // Nota: mudança de filtro deve voltar pra página 1, mas isso é feito pelos
  // setters via withResetPage() — evita fetch duplicado de useEffect separado.
  useEffect(() => { loadList() }, [action, orderID, portalUserID, dateFrom, dateTo, page])
  // Recarrega stats quando o range muda.
  useEffect(() => { loadStats() }, [dateFrom, dateTo])

  function clearFilters() {
    const d = defaultDateRange()
    setDateFrom(d.from)
    setDateTo(d.to)
    setAction('')
    setOrderID('')
    setPortalUserID('')
    setPage(1)
    // Reseta drafts também para refletir no painel ao reabrir.
    setDraftFrom(d.from)
    setDraftTo(d.to)
    setDraftAction('')
    setDraftOrderID('')
    setDraftPortalUserID('')
    setFilterOpen(false)
  }

  function openPanel() {
    setDraftFrom(dateFrom)
    setDraftTo(dateTo)
    setDraftAction(action)
    setDraftOrderID(orderID)
    setDraftPortalUserID(portalUserID)
    setFilterOpen(true)
  }
  function applyFilters() {
    setDateFrom(draftFrom)
    setDateTo(draftTo)
    setAction(draftAction)
    setOrderID(draftOrderID)
    setPortalUserID(draftPortalUserID)
    setPage(1)
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (dateFrom !== defaults.from) chips.push({ key: 'from', label: `De: ${dateFrom}`, onRemove: () => { setDateFrom(defaults.from); setPage(1) } })
  if (dateTo !== defaults.to) chips.push({ key: 'to', label: `Até: ${dateTo}`, onRemove: () => { setDateTo(defaults.to); setPage(1) } })
  if (action) chips.push({ key: 'action', label: `Ação: ${actionLabel(action)}`, onRemove: () => { setAction(''); setPage(1) } })
  if (orderID) chips.push({ key: 'order', label: `Order: #${orderID}`, onRemove: () => { setOrderID(''); setPage(1) } })
  if (portalUserID) chips.push({ key: 'user', label: `User: #${portalUserID}`, onRemove: () => { setPortalUserID(''); setPage(1) } })

  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const topStats = stats.slice(0, 5)

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Auditoria do Portal</h1>
          <p>{total} registro(s) — ações de usuários do portal</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton
            active={chips.length > 0}
            count={chips.length}
            onClick={openPanel}
          />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFrom}
            onChange={e => setDraftFrom(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftTo}
            onChange={e => setDraftTo(e.target.value)}
          />
        </FilterField>
        <FilterField label="Ação">
          <select
            style={filterInputStyle}
            value={draftAction}
            onChange={e => setDraftAction(e.target.value)}
          >
            <option value="">Todas</option>
            {actionOpts.map(a => (
              <option key={a} value={a}>{actionLabel(a)}</option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Order ID">
          <input
            type="number"
            style={filterInputStyle}
            value={draftOrderID}
            onChange={e => setDraftOrderID(e.target.value)}
            placeholder="ex.: 1234"
          />
        </FilterField>
        <FilterField label="Portal user ID (ator)">
          <input
            type="number"
            style={filterInputStyle}
            value={draftPortalUserID}
            onChange={e => setDraftPortalUserID(e.target.value)}
            placeholder="ex.: 42"
          />
        </FilterField>
      </FilterTopPanel>

      {/* ── Stats banner (top 5 actions) ─────────────────────────────── */}
      {topStats.length > 0 && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: `repeat(${topStats.length}, minmax(0,1fr))`,
            gap: 12,
            marginBottom: 16,
          }}
        >
          {topStats.map(s => (
            <div key={s.action} className="szv2-card" style={{ padding: 12, margin: 0 }}>
              <div
                style={{
                  fontSize: 11,
                  color: 'var(--szv2-text-muted)',
                  textTransform: 'uppercase',
                  letterSpacing: 0.5,
                }}
              >
                {actionLabel(s.action)}
              </div>
              <div
                style={{
                  fontSize: 22,
                  fontWeight: 700,
                  color: 'var(--szv2-brand)',
                  marginTop: 4,
                }}
              >
                {s.count.toLocaleString('pt-BR')}
              </div>
              <div style={{ fontSize: 10, color: 'var(--szv2-text-faint)', marginTop: 2 }}>
                {s.action}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* ── Tabela ──────────────────────────────────────────────────── */}
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Data</th>
              <th>Usuário</th>
              <th>Action</th>
              <th>Order</th>
              <th>IP</th>
              <th>Meta</th>
              <th style={{ width: 120 }}>Detalhes</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={8}>
                  <div className="szv2-empty"><h3>Carregando…</h3></div>
                </td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={8}>
                  <div className="szv2-empty">
                    <h3>Nenhum registro de auditoria no período.</h3>
                    <p>Ajuste o range de datas ou os filtros aplicados.</p>
                  </div>
                </td>
              </tr>
            ) : items.map(row => (
              <tr key={row.id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{row.id}</td>
                <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  {fmtDate(row.created_at)}
                </td>
                <td style={{ fontSize: 13 }}>
                  {row.user_nome || row.user_email ? (
                    <div>
                      <div style={{ fontWeight: 500 }}>{row.user_nome || '—'}</div>
                      {row.user_email && (
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                          {row.user_email}
                        </div>
                      )}
                    </div>
                  ) : (
                    <span style={{ color: 'var(--szv2-text-faint)' }}>
                      #{row.portal_user_id}
                    </span>
                  )}
                </td>
                <td>
                  <span
                    className={`sz-badge ${actionBadge(row.action)}`}
                    title={row.action}
                  >
                    {actionLabel(row.action)}
                  </span>
                </td>
                <td style={{ fontSize: 13 }}>
                  {row.order_id ? (
                    <strong>#{row.order_id}</strong>
                  ) : (
                    <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                  )}
                </td>
                <td
                  style={{
                    fontFamily: 'var(--szv2-font-mono)',
                    fontSize: 11,
                    color: 'var(--szv2-text-muted)',
                  }}
                >
                  {row.ip || '—'}
                </td>
                <td
                  style={{
                    fontFamily: 'var(--szv2-font-mono)',
                    fontSize: 11,
                    color: 'var(--szv2-text-soft)',
                    maxWidth: 220,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                  title={row.meta || ''}
                >
                  {truncate(row.meta, 50)}
                </td>
                <td>
                  <button
                    type="button"
                    className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                    onClick={() => setDetail(row)}
                  >
                    Ver detalhes
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* ── Paginação ─────────────────────────────────────────────── */}
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
              ({total.toLocaleString('pt-BR')} registros)
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

      {/* ── Modal de detalhes ─────────────────────────────────────── */}
      {detail && <DetailModal row={detail} onClose={() => setDetail(null)} />}
    </div>
  )
}

// ----- Modal: detalhes do registro ------------------------------------------

function DetailModal({ row, onClose }: { row: AuditRow; onClose: () => void }) {
  const meta = prettyJSON(row.meta)

  return (
    <div className="szv2-modal-overlay szv2-open" onClick={onClose}>
      <div
        className="szv2-modal szv2-modal-lg"
        onClick={e => e.stopPropagation()}
      >
        <div className="szv2-modal-head">
          <h3>
            Registro #{row.id}
            <span
              style={{
                fontWeight: 400,
                fontSize: 13,
                color: 'var(--szv2-text-muted)',
                marginLeft: 8,
              }}
            >
              {fmtDate(row.created_at)}
            </span>
          </h3>
          <button className="szv2-modal-x" onClick={onClose}>✕</button>
        </div>

        <div className="szv2-modal-body">
          {/* Resumo do row */}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(2, 1fr)',
              gap: 12,
              marginBottom: 16,
            }}
          >
            <Field label="Action">
              <span className={`sz-badge ${actionBadge(row.action)}`}>
                {actionLabel(row.action)}
              </span>
              <span
                style={{
                  marginLeft: 8,
                  fontFamily: 'var(--szv2-font-mono)',
                  fontSize: 11,
                  color: 'var(--szv2-text-muted)',
                }}
              >
                {row.action}
              </span>
            </Field>
            <Field label="Portal user">
              {row.user_nome || row.user_email ? (
                <span>
                  {row.user_nome || '—'}
                  {row.user_email && (
                    <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                      {' '}({row.user_email})
                    </span>
                  )}
                  <span style={{ color: 'var(--szv2-text-faint)', fontSize: 11 }}>
                    {' '}· #{row.portal_user_id}
                  </span>
                </span>
              ) : (
                <span>#{row.portal_user_id}</span>
              )}
            </Field>
            <Field label="Order ID">
              {row.order_id ? (
                <a
                  href={`/admin/orders?id=${row.order_id}`}
                  style={{
                    color: 'var(--szv2-brand)',
                    textDecoration: 'none',
                    fontWeight: 600,
                  }}
                >
                  #{row.order_id} →
                </a>
              ) : (
                <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
              )}
            </Field>
            <Field label="IP">
              <span
                style={{
                  fontFamily: 'var(--szv2-font-mono)',
                  fontSize: 12,
                }}
              >
                {row.ip || '—'}
              </span>
            </Field>
          </div>

          {/* Meta JSON pretty-printed */}
          <div className="szv2-field">
            <label className="szv2-label">Meta (JSON)</label>
            <pre
              style={{
                background: 'var(--szv2-neutral-bg)',
                border: '1px solid var(--szv2-divider)',
                borderRadius: 6,
                padding: 12,
                fontSize: 12,
                fontFamily: 'var(--szv2-font-mono)',
                color: 'var(--szv2-text-soft)',
                maxHeight: 360,
                overflow: 'auto',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                margin: 0,
              }}
            >
              {meta}
            </pre>
          </div>
        </div>

        <div className="szv2-modal-foot">
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={onClose}
          >
            Fechar
          </button>
        </div>
      </div>
    </div>
  )
}

// Wrapper para field label + valor (mantém visual padronizado).
function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div
        style={{
          fontSize: 11,
          color: 'var(--szv2-text-muted)',
          textTransform: 'uppercase',
          letterSpacing: 0.5,
          marginBottom: 4,
        }}
      >
        {label}
      </div>
      <div style={{ fontSize: 14 }}>{children}</div>
    </div>
  )
}
