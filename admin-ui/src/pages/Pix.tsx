import { useEffect, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// Linha da listagem de recargas (inclui nome/email via JOIN com portal_users)
type R = {
  id: number
  user_id: number
  nome: string
  email: string
  valor: number
  status: string
  me_pix_id: string | null
  expires_at: string | null
  paid_at: string | null
  created_at: string
}

// Detalhe completo de uma recarga (inclui QR + copia-cola)
type Detail = R & {
  pix_qr: string | null
  pix_codigo: string | null
  tx_id: number | null
}

// Status do cron de reconciliação e KPIs de pendentes por faixa etária
type ReconcileStatus = {
  cron_last_run: string | null
  cron_age_minutes: number | null
  cron_stale: boolean
  pending_3_29: number
  pending_over_29: number
  divergence_last_run: string | null
  divergence_count: number
  recent_errors: string[]
}

const STATUS_CLS: Record<string, string> = {
  pendente:   's-pendente',
  analise:    's-pendente',  // mesmo estilo visual de pendente
  confirmado: 's-confirmado',
  expirado:   's-expirado',
  cancelado:  's-cancelado',
}

const ALL_STATUSES = ['pendente', 'analise', 'confirmado', 'expirado', 'cancelado']

const fmt = (v: number) => 'R$ ' + v.toLocaleString('pt-BR', { minimumFractionDigits: 2 })
const fmtDate = (s: string | null) => s ? s.slice(0, 16).replace('T', ' ') : '—'

function KpiCard({ label, value, sub, danger }: { label: string; value: number | string; sub?: string; danger?: boolean }) {
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span
          className="szv2-kpi-value"
          style={danger && Number(value) > 0
            ? { color: 'var(--szv2-danger)' }
            : { color: 'var(--szv2-brand)' }}
        >
          {value}
        </span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

export default function Pix() {
  const [items, setItems] = useState<R[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [perPage] = useState(25)

  // Filtros
  const [status, setStatus] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')
  const [userSearch, setUserSearch] = useState('')

  // Estado de ações
  const [err, setErr] = useState('')
  const [acting, setActing] = useState<number | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [verifyMsg, setVerifyMsg] = useState<string | null>(null)

  // Reconciliação / admin notices
  const [reconcile, setReconcile] = useState<ReconcileStatus | null>(null)

  // Drill-down detalhe
  const [detail, setDetail] = useState<Detail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  // Painel
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftStatus, setDraftStatus] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [draftUser, setDraftUser] = useState('')

  function buildQs() {
    const p = new URLSearchParams()
    if (status) p.set('status', status)
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    // user_id: aceita busca numérica direta
    const uid = parseInt(userSearch, 10)
    if (!isNaN(uid) && uid > 0) p.set('user_id', String(uid))
    p.set('page', String(page))
    p.set('per_page', String(perPage))
    return p.toString()
  }

  async function load() {
    try {
      const r = await api<{ items: R[]; total: number; page: number; per_page: number }>(
        `/pix?${buildQs()}`
      )
      setItems(r.items ?? [])
      setTotal(r.total)
    } catch (e: any) { setErr(e.message) }
  }

  async function loadReconcile() {
    try {
      const r = await api<ReconcileStatus>('/pix/reconcile-status')
      setReconcile(r)
    } catch { /* graceful — não bloqueia a tela */ }
  }

  // Recarrega lista quando filtros ou página mudam
  useEffect(() => { load() }, [status, dataIni, dataFim, userSearch, page])

  // Carrega status de reconciliação uma vez na montagem
  useEffect(() => { loadReconcile() }, [])

  async function setPixStatus(id: number, s: string) {
    setActing(id)
    try {
      await api(`/pix/${id}/status`, { method: 'PUT', body: JSON.stringify({ status: s }) })
      load()
      loadReconcile()
    } catch (e: any) { setErr(e.message) }
    finally { setActing(null) }
  }

  async function handleVerificar() {
    setVerifying(true)
    setVerifyMsg(null)
    try {
      const r = await api<{ ok: boolean; queued: number }>('/pix/verificar', { method: 'POST', body: '{}' })
      setVerifyMsg(`Verificação iniciada. ${r.queued} recarga(s) pendente(s) na fila.`)
      load()
      loadReconcile()
    } catch (e: any) { setErr(e.message) }
    finally { setVerifying(false) }
  }

  async function handleDetail(id: number) {
    setDetailLoading(true)
    try {
      const d = await api<Detail>(`/pix/${id}`)
      setDetail(d)
    } catch (e: any) { setErr(e.message) }
    finally { setDetailLoading(false) }
  }

  const totalPages = Math.max(1, Math.ceil(total / perPage))

  function openPanel() {
    setDraftStatus(status); setDraftIni(dataIni); setDraftFim(dataFim); setDraftUser(userSearch)
    setFilterOpen(true)
  }
  function applyFilters() {
    setStatus(draftStatus); setDataIni(draftIni); setDataFim(draftFim); setUserSearch(draftUser)
    setPage(1); setFilterOpen(false)
  }
  function clearFilters() {
    setStatus(''); setDataIni(''); setDataFim(''); setUserSearch(''); setPage(1)
    setDraftStatus(''); setDraftIni(''); setDraftFim(''); setDraftUser('')
    setFilterOpen(false)
  }
  const chips: ActiveChip[] = []
  if (status) chips.push({ key: 'status', label: `Status: ${status}`, onRemove: () => { setStatus(''); setPage(1) } })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => { setDataIni(''); setPage(1) } })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => { setDataFim(''); setPage(1) } })
  if (userSearch) chips.push({ key: 'user', label: `User: #${userSearch}`, onRemove: () => { setUserSearch(''); setPage(1) } })

  return (
    <div>
      {/* Admin notices de reconciliação */}
      {reconcile?.cron_stale && (
        <div className="sz-alert-danger" style={{ marginBottom: 12 }}>
          Cron <strong>sz_pix_auto_reconcile_cron</strong> parou há {reconcile.cron_age_minutes} min (limite: 15 min). Verifique o agendador WordPress.
        </div>
      )}
      {reconcile && reconcile.divergence_count > 0 && (
        <div className="sz-alert-danger" style={{ marginBottom: 12 }}>
          Divergência de saldo detectada em {reconcile.divergence_count} carteira(s). Consulte a aba Divergências ou verifique o log sz_wallet_divergence_check.
        </div>
      )}
      {reconcile && reconcile.recent_errors.length > 0 && (
        <div className="sz-alert-danger" style={{ marginBottom: 12 }}>
          {reconcile.recent_errors.length} erro(s) recente(s) no log de reconciliação PIX.
        </div>
      )}

      {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}<button onClick={() => setErr('')} style={{ marginLeft: 8, background: 'none', border: 'none', cursor: 'pointer' }}>✕</button></div>}
      {verifyMsg && <div className="sz-alert-success" style={{ marginBottom: 12 }}>{verifyMsg}<button onClick={() => setVerifyMsg(null)} style={{ marginLeft: 8, background: 'none', border: 'none', cursor: 'pointer' }}>✕</button></div>}

      {/* KPIs de reconciliação */}
      {reconcile && (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))', marginBottom: 24 }}>
          <KpiCard
            label="Última reconciliação"
            value={reconcile.cron_last_run ? fmtDate(reconcile.cron_last_run) : 'Nunca'}
            sub={reconcile.cron_age_minutes !== null ? `${reconcile.cron_age_minutes} min atrás` : undefined}
            danger={reconcile.cron_stale}
          />
          <KpiCard
            label="Pendentes 3–29 min"
            value={reconcile.pending_3_29}
            sub="em análise normal"
            danger={reconcile.pending_3_29 > 0}
          />
          <KpiCard
            label="Pendentes >29 min"
            value={reconcile.pending_over_29}
            sub="vencidos sem confirmação"
            danger={reconcile.pending_over_29 > 0}
          />
          <KpiCard
            label="Divergências de saldo"
            value={reconcile.divergence_count}
            sub={reconcile.divergence_last_run ? `última: ${fmtDate(reconcile.divergence_last_run)}` : 'check nunca executado'}
            danger={reconcile.divergence_count > 0}
          />
        </div>
      )}

      {/* Cabeçalho + ações principais */}
      <div className="szv2-section-head" style={{ marginBottom: 16 }}>
        <div>
          <h1>PIX / Recargas</h1>
          <p>{total} registro(s) · página {page} de {totalPages}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton
            active={chips.length > 0}
            count={chips.length}
            onClick={openPanel}
          />
          <button
            className="szv2-btn szv2-btn-brand"
            disabled={verifying}
            onClick={handleVerificar}
          >
            {verifying ? 'Verificando…' : '🔄 Verificar PIX Pendentes Agora'}
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

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
            value={draftIni}
            onChange={e => setDraftIni(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFim}
            onChange={e => setDraftFim(e.target.value)}
          />
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value)}
          >
            <option value="">Todos status</option>
            {ALL_STATUSES.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </FilterField>
        <FilterField label="Busca (User ID)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex: 42"
            value={draftUser}
            onChange={e => setDraftUser(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>

      {/* Tabela */}
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Usuário</th>
              <th className="szv2-td-num">Valor</th>
              <th>Status</th>
              <th>PIX ID</th>
              <th>Expira</th>
              <th>Pago em</th>
              <th>Criado</th>
              <th>Ações</th>
            </tr>
          </thead>
          <tbody>
            {items.map(r => (
              <tr key={r.id}>
                <td>
                  <button
                    className="szv2-btn szv2-btn-sm"
                    style={{ height: 24, fontSize: 11, padding: '0 6px', background: 'none', border: 'none', color: 'var(--szv2-text-muted)', cursor: 'pointer', textDecoration: 'underline' }}
                    onClick={() => handleDetail(r.id)}
                    title="Ver detalhe completo"
                  >#{r.id}</button>
                </td>
                <td style={{ fontSize: '13px' }}>
                  {r.nome
                    ? <><strong>{r.nome}</strong><br /><span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{r.email || `#${r.user_id}`}</span></>
                    : `#${r.user_id}`
                  }
                </td>
                <td className="szv2-td-num" style={{ fontWeight: 700, color: 'var(--szv2-success)' }}>{fmt(r.valor)}</td>
                <td><span className={`szv2-status-badge ${STATUS_CLS[r.status] || 's-pendente'}`}>{r.status}</span></td>
                <td style={{ maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis', fontSize: 11, color: 'var(--szv2-text-muted)' }}>{r.me_pix_id ?? '—'}</td>
                <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(r.expires_at)}</td>
                <td style={{ fontSize: 12, color: r.paid_at ? 'var(--szv2-success)' : 'var(--szv2-text-muted)' }}>{fmtDate(r.paid_at)}</td>
                <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(r.created_at)}</td>
                <td>
                  {(r.status === 'pendente' || r.status === 'analise') && (
                    <div style={{ display: 'flex', gap: 6 }}>
                      <button
                        disabled={acting === r.id}
                        onClick={() => setPixStatus(r.id, 'confirmado')}
                        className="szv2-btn szv2-btn-sm"
                        style={{ height: 28, fontSize: 11, background: 'var(--szv2-success-bg)', color: 'var(--szv2-success)', borderColor: 'var(--szv2-success)' }}
                      >✓ Confirmar</button>
                      <button
                        disabled={acting === r.id}
                        onClick={() => setPixStatus(r.id, 'cancelado')}
                        className="szv2-btn szv2-btn-sm szv2-btn-danger"
                        style={{ height: 28, fontSize: 11 }}
                      >✕ Cancelar</button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={9}>
                  <div className="szv2-empty">
                    <h3>Nenhuma recarga encontrada</h3>
                    <p>Tente outro filtro de status, data ou usuário.</p>
                  </div>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* Paginação */}
      {totalPages > 1 && (
        <div style={{ display: 'flex', gap: 8, justifyContent: 'center', marginTop: 16 }}>
          <button
            className="szv2-btn szv2-btn-sm"
            disabled={page <= 1}
            onClick={() => setPage(p => Math.max(1, p - 1))}
          >← Anterior</button>
          <span style={{ lineHeight: '30px', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            {page} / {totalPages}
          </span>
          <button
            className="szv2-btn szv2-btn-sm"
            disabled={page >= totalPages}
            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
          >Próxima →</button>
        </div>
      )}

      {/* Modal de detalhe da recarga */}
      {detail && (
        <div
          style={{
            position: 'fixed', inset: 0, background: 'rgba(0,0,0,.5)', zIndex: 9999,
            display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24,
          }}
          onClick={e => { if (e.target === e.currentTarget) setDetail(null) }}
        >
          <div className="szv2-card" style={{ maxWidth: 640, width: '100%', maxHeight: '90vh', overflowY: 'auto' }}>
            <div className="szv2-card-head" style={{ marginBottom: 16 }}>
              <h2>Recarga #{detail.id}</h2>
              <button
                className="szv2-btn szv2-btn-sm"
                onClick={() => setDetail(null)}
                style={{ height: 28, fontSize: 11 }}
              >✕ Fechar</button>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px 16px', marginBottom: 16 }}>
              <div><span className="szv2-label">Usuário</span>
                <p style={{ margin: '2px 0', fontWeight: 600 }}>{detail.nome || `#${detail.user_id}`}</p>
                {detail.email && <p style={{ margin: 0, fontSize: 12, color: 'var(--szv2-text-muted)' }}>{detail.email}</p>}
              </div>
              <div><span className="szv2-label">Valor</span>
                <p style={{ margin: '2px 0', fontWeight: 700, color: 'var(--szv2-success)' }}>{fmt(detail.valor)}</p>
              </div>
              <div><span className="szv2-label">Status</span>
                <p style={{ margin: '2px 0' }}><span className={`szv2-status-badge ${STATUS_CLS[detail.status] || 's-pendente'}`}>{detail.status}</span></p>
              </div>
              <div><span className="szv2-label">PIX ID (ME)</span>
                <p style={{ margin: '2px 0', fontSize: 12, wordBreak: 'break-all' }}>{detail.me_pix_id ?? '—'}</p>
              </div>
              <div><span className="szv2-label">Criado em</span>
                <p style={{ margin: '2px 0', fontSize: 12 }}>{fmtDate(detail.created_at)}</p>
              </div>
              <div><span className="szv2-label">Expira em</span>
                <p style={{ margin: '2px 0', fontSize: 12 }}>{fmtDate(detail.expires_at)}</p>
              </div>
              <div><span className="szv2-label">Pago em</span>
                <p style={{ margin: '2px 0', fontSize: 12 }}>{fmtDate(detail.paid_at)}</p>
              </div>
              {detail.tx_id && (
                <div><span className="szv2-label">Transação</span>
                  <p style={{ margin: '2px 0', fontSize: 12 }}>#{detail.tx_id}</p>
                </div>
              )}
            </div>

            {detail.pix_codigo && (
              <div style={{ marginBottom: 12 }}>
                <span className="szv2-label">Copia e cola PIX</span>
                <textarea
                  readOnly
                  value={detail.pix_codigo}
                  style={{ width: '100%', marginTop: 4, fontSize: 11, fontFamily: 'monospace', resize: 'vertical', minHeight: 60 }}
                  onClick={e => (e.target as HTMLTextAreaElement).select()}
                />
              </div>
            )}

            {detail.pix_qr && (
              <div style={{ marginBottom: 12, textAlign: 'center' }}>
                <span className="szv2-label">QR Code</span>
                {detail.pix_qr.startsWith('data:') || detail.pix_qr.startsWith('http')
                  ? <img src={detail.pix_qr} alt="QR PIX" style={{ display: 'block', margin: '8px auto', maxWidth: 200 }} />
                  : <img src={`data:image/png;base64,${detail.pix_qr}`} alt="QR PIX" style={{ display: 'block', margin: '8px auto', maxWidth: 200 }} />
                }
              </div>
            )}

            {(detail.status === 'pendente' || detail.status === 'analise') && (
              <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
                <button
                  disabled={acting === detail.id}
                  onClick={() => { setPixStatus(detail.id, 'confirmado'); setDetail(null) }}
                  className="szv2-btn-brand"
                >✓ Confirmar recarga</button>
                <button
                  disabled={acting === detail.id}
                  onClick={() => { setPixStatus(detail.id, 'cancelado'); setDetail(null) }}
                  className="szv2-btn szv2-btn-danger"
                >✕ Cancelar recarga</button>
              </div>
            )}
          </div>
        </div>
      )}

      {detailLoading && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.3)', zIndex: 9998, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div className="szv2-card" style={{ padding: 24 }}>Carregando detalhe…</div>
        </div>
      )}
    </div>
  )
}
