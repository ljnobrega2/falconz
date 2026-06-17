import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'

// MotoboyDashboard — espelho da aba Dashboard do módulo Motoboy do plugin PHP.
// Endpoint: GET /motoboy-dashboard?date=YYYY-MM-DD (default hoje em America/Sao_Paulo).

type DashboardKPIs = {
  agendados: number
  embalados: number
  em_rota: number
  entregues: number
  frustrados: number
  fechamentos_pendentes: number
}

type RankingItem = {
  motoboy_id: number
  motoboy_nome: string
  cd_nome: string
  zona_nome: string
  pedidos: number
  entregues: number
  frustrados: number
  taxa_sucesso: number
}

type DashboardResp = {
  date: string
  kpis: DashboardKPIs
  ranking: RankingItem[]
}

// ── Helpers de data ──────────────────────────────────────────────────────────
// Retorna a data "hoje" no fuso America/Sao_Paulo no formato ISO YYYY-MM-DD,
// usando Intl em vez de Date.toISOString() (que sempre devolve UTC).
function todayInSaoPaulo(): string {
  const fmt = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'America/Sao_Paulo',
    year: 'numeric', month: '2-digit', day: '2-digit',
  })
  return fmt.format(new Date()) // en-CA → "YYYY-MM-DD"
}

// Formata YYYY-MM-DD → dd/mm/yyyy para exibição no header.
function formatBR(iso: string): string {
  if (!iso || iso.length < 10) return iso
  const y = iso.slice(0, 4)
  const m = iso.slice(5, 7)
  const d = iso.slice(8, 10)
  return `${d}/${m}/${y}`
}

// Formata taxa com 1 casa decimal estilo "80.0%".
function formatTaxa(v: number): string {
  if (!isFinite(v) || v <= 0) return '0.0%'
  return `${v.toFixed(1)}%`
}

// ── Card de KPI ──────────────────────────────────────────────────────────────
type KpiTone = 'warning' | 'purple' | 'info' | 'success' | 'danger'

const TONE_COLORS: Record<KpiTone, string> = {
  warning: '#f59e0b',
  purple:  '#a855f7',
  info:    '#3b82f6',
  success: '#16a34a',
  danger:  '#dc2626',
}

function KpiCard({
  label, value, icon, tone, sub,
}: {
  label: string
  value: number
  icon: string
  tone: KpiTone
  sub?: string
}) {
  const color = TONE_COLORS[tone]
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{icon} {label}</span>
        <span className="szv2-kpi-value" style={{ color }}>{value.toLocaleString('pt-BR')}</span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

// ── Medalha do top-3 ─────────────────────────────────────────────────────────
const MEDALS = ['🥇', '🥈', '🥉']
const MEDAL_BORDERS = [
  '2px solid #facc15', // gold
  '2px solid #cbd5e1', // silver
  '2px solid #b45309', // bronze
]

export default function MotoboyDashboard() {
  const [date, setDate] = useState<string>(todayInSaoPaulo())
  const [data, setData] = useState<DashboardResp | null>(null)
  const [loading, setLoading] = useState<boolean>(true)
  const [err, setErr] = useState<string>('')

  // Carrega novamente sempre que a data muda.
  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoading(true)
      setErr('')
      try {
        const r = await api<DashboardResp>(`/motoboy-dashboard?date=${encodeURIComponent(date)}`)
        if (!cancelled) setData(r)
      } catch (e: any) {
        if (!cancelled) setErr(e?.message || 'Erro ao carregar dashboard')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [date])

  // Ordena ranking por taxa_sucesso DESC para o top-3 / tabela.
  // Mantém uma cópia ordenada — o backend traz por entregues DESC.
  const ranking = useMemo<RankingItem[]>(() => {
    if (!data?.ranking?.length) return []
    return [...data.ranking].sort((a, b) => {
      if (b.taxa_sucesso !== a.taxa_sucesso) return b.taxa_sucesso - a.taxa_sucesso
      if (b.entregues !== a.entregues) return b.entregues - a.entregues
      return b.pedidos - a.pedidos
    })
  }, [data])

  const kpis = data?.kpis
  const dateLabel = formatBR(data?.date || date)

  return (
    <div>
      {/* Top bar: data + título */}
      <div className="szv2-section-head">
        <div>
          <h1>Dashboard Motoboy</h1>
          <p>Visão operacional do dia — {dateLabel}</p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <label
            htmlFor="szv2-mb-dash-date"
            style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}
          >
            Data:
          </label>
          <input
            id="szv2-mb-dash-date"
            type="date"
            className="szv2-input"
            value={date}
            max={todayInSaoPaulo()}
            onChange={e => setDate(e.target.value || todayInSaoPaulo())}
            disabled={loading}
            style={{ minWidth: 160 }}
          />
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={() => setDate(todayInSaoPaulo())}
            disabled={loading || date === todayInSaoPaulo()}
          >
            Hoje
          </button>
        </div>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* KPI grid — 5 cards (espelha sz_mb_tab_dashboard PHP) */}
      <div
        className="szv2-kpi-grid"
        style={{
          gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
          marginBottom: 24,
        }}
      >
        <KpiCard label="Agendados"  value={kpis?.agendados  ?? 0} icon="🟠" tone="warning" sub="aguardando saída" />
        <KpiCard label="Embalados"  value={kpis?.embalados  ?? 0} icon="🟣" tone="purple"  sub="prontos para coleta" />
        <KpiCard label="Em rota"    value={kpis?.em_rota    ?? 0} icon="🔵" tone="info"    sub="entregadores na rua" />
        <KpiCard label="Entregues"  value={kpis?.entregues  ?? 0} icon="✅" tone="success" sub="concluídos no dia" />
        <KpiCard label="Frustrados" value={kpis?.frustrados ?? 0} icon="❌" tone="danger"  sub="tentativas sem sucesso" />
      </div>

      {/* Banner de fechamentos pendentes — exibido apenas quando count > 0, igual ao PHP */}
      {(kpis?.fechamentos_pendentes ?? 0) > 0 && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>
          ⚠️ {kpis!.fechamentos_pendentes} fechamento(s) de caixa pendente(s) de confirmação.
        </div>
      )}

      {/* Ranking de motoboys */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Ranking de motoboys</h2>
            <p className="szv2-card-sub">
              {ranking.length} motoboy(s) ativos — ordenado por taxa de sucesso
            </p>
          </div>
        </div>

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : ranking.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum motoboy ativo no dia
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th style={{ width: 64 }}>#</th>
                  <th>Motoboy</th>
                  <th>CD</th>
                  <th>Zona</th>
                  <th style={{ textAlign: 'right' }}>Pedidos</th>
                  <th style={{ textAlign: 'right' }}>Entregues</th>
                  <th style={{ textAlign: 'right' }}>Frustrados</th>
                  <th style={{ textAlign: 'right' }}>Taxa de sucesso</th>
                </tr>
              </thead>
              <tbody>
                {ranking.map((m, idx) => {
                  const medal = idx < 3 ? MEDALS[idx] : null
                  const border = idx < 3 ? MEDAL_BORDERS[idx] : undefined
                  return (
                    <tr
                      key={m.motoboy_id}
                      style={border ? { boxShadow: `inset 3px 0 0 0 ${border.split(' ')[2]}` } : undefined}
                    >
                      <td>
                        <strong style={{ color: 'var(--szv2-text-muted)' }}>
                          {medal ? `${medal} ${idx + 1}` : idx + 1}
                        </strong>
                      </td>
                      <td><strong>{m.motoboy_nome}</strong></td>
                      <td>{m.cd_nome || '—'}</td>
                      <td>{m.zona_nome || '—'}</td>
                      <td style={{ textAlign: 'right' }}>{m.pedidos}</td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-success)', fontWeight: 700 }}>
                        {m.entregues}
                      </td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-danger)', fontWeight: 700 }}>
                        {m.frustrados}
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        <strong>{formatTaxa(m.taxa_sucesso)}</strong>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
