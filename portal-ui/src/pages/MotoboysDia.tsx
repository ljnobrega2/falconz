// Motoboys — dia (OL) — porte fiel de templates/portal/v2/sections/motoboys-dia.php para React.
// Ligada ao go/portal:
//   GET /portal/motoboys-dia — KPIs por motoboy do DIA + pedidos sem motoboy (user-scoped)
//
// DESVIOS FIÉIS (contrato Go ≠ shape WP — o contrato vence):
//   - A section WP é OL-only e agrega FROM sz_motoboys (escopo GLOBAL do operador). O
//     handler Go re-escopa por dono: produtor vê os motoboys que carregam OS PEDIDOS DELE.
//     Consequência: cards = motoboys com pedidos do usuário hoje (não a lista global).
//   - Por isso o empty-state diz "sem pedidos hoje", não "cadastre motoboys" (que pressupunha
//     o registro global do OL — enganoso sob escopo por dono).
//   - "hoje" vem PRONTO do envelope (computado em America/Sao_Paulo no backend). NUNCA passar
//     por dt()/dtTime() de utils/format: new Date("2026-06-18") parseia como UTC e ao render
//     em SP volta um dia. Formatar por string-slice (fmtDateBr), igual ao Motoboy.tsx.
//
// FIDELIDADE: laranja do WP (#EA580C/#ea580c, caixa #fff7ed/#fed7aa) → var(--szv2-brand)/brand-light.
// Verde (#16a34a) → var(--szv2-success); vermelho (#dc2626) → var(--szv2-danger). Pendentes cinza.
// Barra de progresso: ≥80 success, ≥50 brand, senão danger. "Atualizar" = refetch (load), não reload.
import { useEffect, useState } from 'react'
import { api } from '../api'
import { brl } from '../utils/format'
import EmptyState from '../components/EmptyState'
import StatusBadge from '../components/StatusBadge'

// ── Shape (espelha go/portal/internal/handlers/motoboys_dia_portal.go) ───────────
type MdPedidoRow = {
  wc_order_id: number | null
  dest_nome: string
  status: string
  valor_pedido: number
}

type MdMotoboyCard = {
  id: number
  nome: string
  telefone: string
  total: number
  entregues: number
  frustrados: number
  em_rota: number
  pendentes: number
  pct: number
  total_valor: number
  pedidos: MdPedidoRow[]
}

type ListResp = {
  ok: boolean
  data: MdMotoboyCard[]
  total: number
  sem_motoboy: number
  hoje: string // "YYYY-MM-DD" — já no fuso de SP
  role: string
  is_affiliate: boolean
}

// ── Data BR "dd/mm/aaaa" a partir de "Y-m-d" (sem parse de timezone) ──────────────
function fmtDateBr(raw: string | null | undefined): string {
  if (!raw) return '—'
  const m = String(raw).slice(0, 10).match(/^(\d{4})-(\d{2})-(\d{2})$/)
  return m ? `${m[3]}/${m[2]}/${m[1]}` : '—'
}

// Status do pedido na lista: componente compartilhado StatusBadge (cor única por
// status, label sem underscore, sem bolinha) — src/components/StatusBadge.tsx.

// Cor da barra de progresso: ≥80 verde, ≥50 brand, senão vermelho (motoboys-dia.php:134).
function progressColor(pct: number): string {
  if (pct >= 80) return 'var(--szv2-success)'
  if (pct >= 50) return 'var(--szv2-brand)'
  return 'var(--szv2-danger)'
}

// Inicial do motoboy (avatar).
function inicial(nome: string): string {
  const n = (nome || '').trim()
  return n ? n.charAt(0).toUpperCase() : '?'
}

export default function MotoboysDia() {
  const [cards, setCards] = useState<MdMotoboyCard[]>([])
  const [semMotoboy, setSemMotoboy] = useState(0)
  const [hoje, setHoje] = useState('')
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  function load() {
    setLoading(true)
    setErr('')
    api<ListResp>('/portal/motoboys-dia')
      .then(r => {
        setCards(r.data || [])
        setSemMotoboy(typeof r.sem_motoboy === 'number' ? r.sem_motoboy : 0)
        setHoje(r.hoje || '')
      })
      .catch(e => setErr(e.message || 'Erro ao carregar motoboys do dia'))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const dataBr = fmtDateBr(hoje)

  return (
    <section id="sec-motoboys-dia" className="sz-sec" data-szv2-label="Motoboys — dia">
      {/* ── Cabeçalho + Atualizar (motoboys-dia.php:50-58) ─────────────────── */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 18,
          flexWrap: 'wrap',
          gap: 8,
        }}
      >
        <div>
          <h2 style={{ margin: '0 0 2px', fontSize: 18, fontWeight: 800, color: 'var(--szv2-text)' }}>
            {hoje ? `Motoboys — ${dataBr}` : 'Motoboys'}
          </h2>
          <p style={{ margin: 0, fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            Pedidos atribuídos a cada motoboy hoje.
          </p>
        </div>
        <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={load} disabled={loading}>
          {loading ? 'Atualizando…' : 'Atualizar'}
        </button>
      </div>

      {!!err && <div className="sz-alert-danger">{err}</div>}

      {/* ── Alerta: pedidos sem motoboy hoje (motoboys-dia.php:60-65) ───────── */}
      {semMotoboy > 0 && (
        <div
          style={{
            background: 'var(--szv2-brand-light)',
            border: '1px solid var(--szv2-brand)',
            borderRadius: 10,
            padding: '12px 16px',
            marginBottom: 18,
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            fontSize: 13,
            color: 'var(--szv2-text)',
          }}
        >
          <svg viewBox="0 0 20 20" style={{ width: 16, height: 16, fill: 'var(--szv2-brand)', flexShrink: 0 }} aria-hidden="true">
            <path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm0 3v5l3 2-1 1.7L9 11V5h1z" />
          </svg>
          <span>
            <strong>{semMotoboy} pedido(s)</strong> sem motoboy atribuído hoje.
          </span>
        </div>
      )}

      {/* ── Loading / Empty / Grid de cards ─────────────────────────────────── */}
      {loading ? (
        <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
      ) : cards.length === 0 ? (
        <EmptyState
          icon="🛵"
          title="Nenhum motoboy com pedidos hoje"
          description="Os motoboys aparecem aqui assim que tiverem pedidos atribuídos no dia de hoje."
        />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
            gap: 18,
          }}
        >
          {cards.map(mb => (
            <div key={mb.id} className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
              {/* ── Header motoboy (motoboys-dia.php:92-105) ─────────────── */}
              <div
                style={{
                  padding: '14px 16px',
                  background: 'var(--szv2-surface-alt)',
                  borderBottom: '1px solid var(--szv2-divider)',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                }}
              >
                <div
                  style={{
                    width: 36,
                    height: 36,
                    borderRadius: '50%',
                    background: 'var(--szv2-brand)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    flexShrink: 0,
                    color: '#fff',
                    fontWeight: 800,
                    fontSize: 14,
                  }}
                >
                  {inicial(mb.nome)}
                </div>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontWeight: 700, fontSize: 14, color: 'var(--szv2-text)' }}>{mb.nome}</div>
                  <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{mb.telefone || '—'}</div>
                </div>
                <div style={{ marginLeft: 'auto', textAlign: 'right', flexShrink: 0 }}>
                  <div style={{ fontSize: 20, fontWeight: 800, color: 'var(--szv2-brand)' }}>{mb.total}</div>
                  <div style={{ fontSize: 10, color: 'var(--szv2-text-muted)' }}>pedidos</div>
                </div>
              </div>

              {/* ── KPIs mini (motoboys-dia.php:108-125) ─────────────────── */}
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(4, 1fr)',
                  borderBottom: '1px solid var(--szv2-divider)',
                }}
              >
                <div style={{ padding: 8, textAlign: 'center', borderRight: '1px solid var(--szv2-divider)' }}>
                  <div style={{ fontSize: 16, fontWeight: 800, color: 'var(--szv2-success)' }}>{mb.entregues}</div>
                  <div style={kpiLabelStyle}>Entregues</div>
                </div>
                <div style={{ padding: 8, textAlign: 'center', borderRight: '1px solid var(--szv2-divider)' }}>
                  <div style={{ fontSize: 16, fontWeight: 800, color: 'var(--szv2-danger)' }}>{mb.frustrados}</div>
                  <div style={kpiLabelStyle}>Frustrados</div>
                </div>
                <div style={{ padding: 8, textAlign: 'center', borderRight: '1px solid var(--szv2-divider)' }}>
                  <div style={{ fontSize: 16, fontWeight: 800, color: 'var(--szv2-brand)' }}>{mb.em_rota}</div>
                  <div style={kpiLabelStyle}>Em rota</div>
                </div>
                <div style={{ padding: 8, textAlign: 'center' }}>
                  <div style={{ fontSize: 16, fontWeight: 800, color: 'var(--szv2-text-muted)' }}>{mb.pendentes}</div>
                  <div style={kpiLabelStyle}>Pendentes</div>
                </div>
              </div>

              {/* ── Barra de progresso (motoboys-dia.php:128-137) ────────── */}
              {mb.total > 0 && (
                <div style={{ padding: '8px 14px', borderBottom: '1px solid var(--szv2-divider)' }}>
                  <div
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      fontSize: 11,
                      color: 'var(--szv2-text-muted)',
                      marginBottom: 4,
                    }}
                  >
                    <span>Taxa de entrega</span>
                    <span>{mb.pct}%</span>
                  </div>
                  <div style={{ height: 6, background: 'var(--szv2-divider)', borderRadius: 99, overflow: 'hidden' }}>
                    <div
                      style={{
                        height: '100%',
                        width: `${mb.pct}%`,
                        background: progressColor(mb.pct),
                        borderRadius: 99,
                        transition: 'width .4s',
                      }}
                    />
                  </div>
                </div>
              )}

              {/* ── Lista de pedidos do dia (motoboys-dia.php:140-156) ───── */}
              {mb.pedidos && mb.pedidos.length > 0 ? (
                <div style={{ maxHeight: 200, overflowY: 'auto' }}>
                  {mb.pedidos.map((sp, i) => {
                    return (
                      <div
                        key={`${sp.wc_order_id ?? 'x'}-${i}`}
                        style={{
                          padding: '8px 14px',
                          borderBottom: '1px solid var(--szv2-divider)',
                          display: 'flex',
                          alignItems: 'center',
                          gap: 8,
                          fontSize: 12,
                        }}
                      >
                        <span style={{ fontFamily: 'monospace', color: 'var(--szv2-text-muted)', fontSize: 11 }}>
                          #{sp.wc_order_id ?? '—'}
                        </span>
                        <span
                          style={{
                            flex: 1,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                            color: 'var(--szv2-text)',
                          }}
                        >
                          {sp.dest_nome || '—'}
                        </span>
                        <StatusBadge status={sp.status} style={{ fontSize: 10, padding: '2px 8px' }} />
                        <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>
                          {brl(sp.valor_pedido)}
                        </span>
                      </div>
                    )
                  })}
                </div>
              ) : (
                <div style={{ padding: 16, textAlign: 'center', fontSize: 12, color: 'var(--szv2-text-faint)' }}>
                  Sem pedidos atribuídos a este motoboy hoje.
                </div>
              )}

              {/* ── Total do dia (motoboys-dia.php:159-164) ──────────────── */}
              {mb.total_valor > 0 && (
                <div
                  style={{
                    padding: '10px 14px',
                    background: 'var(--szv2-surface-alt)',
                    fontSize: 12,
                    display: 'flex',
                    justifyContent: 'space-between',
                    color: 'var(--szv2-text)',
                  }}
                >
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Total do dia</span>
                  <strong>{brl(mb.total_valor)}</strong>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

// Rótulo dos KPIs mini (motoboys-dia.php:111 etc.) — uppercase + letter-spacing.
const kpiLabelStyle: React.CSSProperties = {
  fontSize: 9,
  color: 'var(--szv2-text-muted)',
  textTransform: 'uppercase',
  letterSpacing: '.04em',
}
