import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import DetailDrawer from '../components/DetailDrawer'

// CronStatus — HISTÓRICO (somente leitura) dos crons do plugin.
// O site NÃO executa crons; os jobs rodam no worker (Asynq/wp-cron). Esta tela
// só mostra última execução, status e histórico, e destaca falhas (alerta no
// topo + KPI "Com erro"). Sem "executar agora" / "pular próxima".

type CronItem = {
  name: string
  frequency: string
  description: string
  tables: string[]
  handler_path: string
  last_run: string | null
  last_status: 'ok' | 'err' | 'never' | 'manual_trigger' | 'skipped' | string
  last_message: string
  last_duration_ms: number
  next_run: string | null
}

type RecentRun = {
  started_at?: string
  duration_ms?: number
  status?: string
  message?: string
}

type StatusFilter = '' | 'ok' | 'err' | 'never'

const STATUS_FILTER_LABELS: Record<StatusFilter, string> = {
  '':    'Todos',
  ok:    'OK',
  err:   'Com erro',
  never: 'Nunca executado',
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// Relative time formatter usando Intl. Retorna "—" se data ausente.
const rtf = new Intl.RelativeTimeFormat('pt-BR', { numeric: 'auto' })

function formatRelative(iso: string | null): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (!isFinite(t)) return '—'
  const diffSec = Math.round((t - Date.now()) / 1000)
  const abs = Math.abs(diffSec)
  if (abs < 60)        return rtf.format(diffSec, 'second')
  if (abs < 3600)      return rtf.format(Math.round(diffSec / 60), 'minute')
  if (abs < 86400)     return rtf.format(Math.round(diffSec / 3600), 'hour')
  if (abs < 86400 * 7) return rtf.format(Math.round(diffSec / 86400), 'day')
  return new Date(t).toLocaleString('pt-BR')
}

function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return '—'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60_000).toFixed(1)}min`
}

// Mapa de classes de badge por status (cores definidas em components.css).
function statusBadge(status: string): { className: string; label: string } {
  switch (status) {
    case 'ok':              return { className: 'sz-badge szv2-badge-success', label: '✓ OK' }
    case 'err':             return { className: 'sz-badge szv2-badge-danger',  label: '✗ ERRO' }
    case 'never':           return { className: 'sz-badge szv2-badge-warning', label: 'Nunca executado' }
    case 'manual_trigger':  return { className: 'sz-badge szv2-badge-info',    label: 'Manual' }
    case 'skipped':         return { className: 'sz-badge szv2-badge-neutral', label: 'Pulado' }
    default:                return { className: 'sz-badge szv2-badge-neutral', label: status || '—' }
  }
}

// ── KPI Card (espelha AuditEngine.tsx) ──────────────────────────────────────
function KpiCard({
  label, value, sub, danger, success,
}: {
  label: string
  value: number | string
  sub?: string
  danger?: boolean
  success?: boolean
}) {
  const color = danger && Number(value) > 0
    ? 'var(--szv2-danger)'
    : success
      ? 'var(--szv2-success)'
      : 'var(--szv2-brand)'
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

// ── Modal de histórico ──────────────────────────────────────────────────────
function HistoryModal({
  cronName, onClose,
}: { cronName: string; onClose: () => void }) {
  const [runs, setRuns] = useState<RecentRun[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  useEffect(() => {
    let alive = true
    setLoading(true)
    setErr('')
    api<{ items: RecentRun[]; note?: string }>(`/crons/${encodeURIComponent(cronName)}/recent-runs?limit=20`)
      .then(r => { if (alive) setRuns(r.items || []) })
      .catch(e => { if (alive) setErr(e.message || 'Erro') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [cronName])

  // 2026-06-19 — migrado para DetailDrawer (lateral direito, marca registrada)
  return (
    <DetailDrawer open onClose={onClose} large title={`Histórico — ${cronName}`}>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}
      {loading ? (
        <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando…
        </div>
      ) : runs.length === 0 ? (
        <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Nenhuma execução registrada ainda.<br />
          <small>Execuções manuais aparecem aqui após o primeiro disparo.</small>
        </div>
      ) : (
        <table className="szv2-table">
          <thead>
            <tr>
              <th>Início</th>
              <th>Duração</th>
              <th>Status</th>
              <th>Mensagem</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r, i) => {
              const b = statusBadge(r.status || 'never')
              return (
                <tr key={i}>
                  <td>{r.started_at ? new Date(r.started_at).toLocaleString('pt-BR') : '—'}</td>
                  <td>{formatDuration(r.duration_ms || 0)}</td>
                  <td><span className={b.className}>{b.label}</span></td>
                  <td>{r.message || '—'}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </DetailDrawer>
  )
}

// ── Página principal ────────────────────────────────────────────────────────
export default function CronStatus() {
  const [items, setItems] = useState<CronItem[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [filter, setFilter] = useState<StatusFilter>('')
  const [historyOf, setHistoryOf] = useState<string | null>(null)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<{ items: CronItem[] }>('/crons')
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])


  // KPIs derivados — total, rodando OK (ok + last_run ≤ 24h), com erro.
  const kpis = useMemo(() => {
    const total = items.length
    const okRecent = items.filter(c => {
      if (c.last_status !== 'ok' || !c.last_run) return false
      const t = Date.parse(c.last_run)
      return isFinite(t) && (Date.now() - t) <= 24 * 3600 * 1000
    }).length
    const erro = items.filter(c => c.last_status === 'err').length
    return { total, okRecent, erro }
  }, [items])

  // Filtro client-side por status (chips do topo).
  const filtered = useMemo(() => {
    if (!filter) return items
    return items.filter(c => c.last_status === filter)
  }, [items, filter])

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* Alerta de falha — dispara no topo se algum cron está com erro */}
      {kpis.erro > 0 && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>
          ⚠️ {kpis.erro} cron(s) com falha na última execução — verifique abaixo.
        </div>
      )}

      {/* Top bar: reload + filtro */}
      <div className="szv2-card" style={{ marginBottom: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Histórico de crons</h2>
            <p className="szv2-card-sub">
              Somente leitura — o site não executa crons. Mostra última execução, status e histórico de cada job; falhas disparam alerta no topo e no dashboard.
            </p>
          </div>
          <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
            <div style={{ display: 'flex', gap: 6 }}>
              {(Object.keys(STATUS_FILTER_LABELS) as StatusFilter[]).map(k => (
                <button
                  key={k}
                  type="button"
                  className={filter === k ? 'szv2-btn-brand' : 'szv2-btn-secondary'}
                  onClick={() => setFilter(k)}
                  disabled={loading}
                  style={{ padding: '6px 12px', fontSize: 13 }}
                >
                  {STATUS_FILTER_LABELS[k]}
                </button>
              ))}
            </div>
            <button
              type="button"
              className="szv2-btn-secondary"
              onClick={load}
              disabled={loading}
            >
              🔄 Atualizar
            </button>
          </div>
        </div>
      </div>

      {/* KPIs */}
      <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))' }}>
        <KpiCard label="Total de crons"        value={kpis.total}    sub="catálogo do plugin" />
        <KpiCard label="Rodando OK (24h)"      value={kpis.okRecent} sub="execução recente bem-sucedida" success />
        <KpiCard label="Com erro"              value={kpis.erro}     sub="precisam de atenção" danger />
      </div>

      {/* Tabela */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Jobs</h2>
            <p className="szv2-card-sub">{filtered.length} de {items.length} cron(s)</p>
          </div>
        </div>

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : filtered.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum cron corresponde ao filtro.
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Nome</th>
                  <th>Frequência</th>
                  <th>Descrição</th>
                  <th>Última execução</th>
                  <th>Duração</th>
                  <th>Status</th>
                  <th>Próxima</th>
                  <th style={{ width: 120 }}>Histórico</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map(c => {
                  const b = statusBadge(c.last_status)
                  return (
                    <tr key={c.name}>
                      <td>
                        <strong style={{ fontFamily: 'monospace', fontSize: 12 }}>{c.name}</strong>
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginTop: 2 }}>
                          {c.handler_path}
                        </div>
                      </td>
                      <td>
                        <span className="sz-badge szv2-badge-brand">{c.frequency}</span>
                      </td>
                      <td style={{ maxWidth: 280, fontSize: 13 }}>{c.description}</td>
                      <td>{formatRelative(c.last_run)}</td>
                      <td>{formatDuration(c.last_duration_ms)}</td>
                      <td><span className={b.className}>{b.label}</span></td>
                      <td>{formatRelative(c.next_run)}</td>
                      <td>
                        <button
                          type="button"
                          className="szv2-btn-secondary"
                          onClick={() => setHistoryOf(c.name)}
                          disabled={loading}
                          style={{ padding: '4px 10px', fontSize: 12 }}
                        >
                          Ver histórico
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {historyOf && (
        <HistoryModal cronName={historyOf} onClose={() => setHistoryOf(null)} />
      )}
    </div>
  )
}
