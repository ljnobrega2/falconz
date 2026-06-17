import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'

type Summary = {
  bruto_cod: number
  afiliados: number
  taxas_senderzz: number
  liquido_produtor: number
  previsto_produtor: number
}

type OrderRow = {
  order_id: number
  data_pedido: string
  situacao: 'Recebido' | 'Estornado' | 'Previsto' | string
  affiliate_id: number
  affiliate_name: string
  affiliate_email: string
  producer_id: number
  commission_pct: number
  valor_pedido: number
  bruto_valido: number
  taxas_senderzz: number
  taxa_entrega: number
  taxa_transacao: number
  valor_afiliado: number
  liquido_produtor: number
  valor_nao_recebido: number
  bruto_estornado: number
  frustrado_afiliado: number
  frustrado_produtor: number
  repasse_pendente: number
  repasse_disponivel: number
  repasse_estornado: number
  status_repasse: string
}

type AffSummary = {
  affiliate_id: number
  affiliate_name: string
  affiliate_email: string
  pedidos: number
  recebidos: number
  previstos: number
  frustrados: number
  pendente: number
  disponivel: number
  previsto_valor: number
}

type ProdSummary = {
  producer_id: number
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

// ── Helpers ─────────────────────────────────────────────────────────────────
const moneyFmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const money = (v: number) => `R$ ${moneyFmt(v)}`
const fmtDate = (s: string) => {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  return d.toLocaleDateString('pt-BR')
}
const fmtPct = (v: number) =>
  `${v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}%`

function defaultRange(): { from: string; to: string } {
  const today = new Date()
  const past = new Date(today)
  past.setDate(past.getDate() - 7)
  const fmt = (d: Date) => d.toISOString().slice(0, 10)
  return { from: fmt(past), to: fmt(today) }
}

// ── KPI Card (local — espelha AuditEngine) ──────────────────────────────────
function KpiCard({
  label,
  value,
  sub,
  danger,
}: {
  label: string
  value: number | string
  sub?: string
  danger?: boolean
}) {
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span
          className="szv2-kpi-value"
          style={danger ? { color: 'var(--szv2-warning)' } : { color: 'var(--szv2-brand)' }}
        >
          {value}
        </span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

// ── Badge da Situação ───────────────────────────────────────────────────────
function SituacaoBadge({ situacao }: { situacao: string }) {
  if (situacao === 'Recebido')
    return <span className="sz-badge szv2-badge-success">Recebido</span>
  if (situacao === 'Estornado')
    return <span className="sz-badge szv2-badge-danger">Estornado</span>
  if (situacao === 'Previsto')
    return <span className="sz-badge szv2-badge-warning">Previsto</span>
  return <span className="sz-badge szv2-badge-neutral">{situacao || '—'}</span>
}

// ── Badge do Status de Repasse ──────────────────────────────────────────────
function RepasseBadge({ status }: { status: string }) {
  const map: Record<string, string> = {
    'Disponível': 'szv2-badge-success',
    'Pendente': 'szv2-badge-warning',
    'Previsto': 'szv2-badge-warning',
    'Não repassar': 'szv2-badge-danger',
    'Sem afiliado': 'szv2-badge-neutral',
  }
  const cls = map[status] || 'szv2-badge-neutral'
  return <span className={`sz-badge ${cls}`}>{status || '—'}</span>
}

export default function CodLivro() {
  const init = useMemo(defaultRange, [])
  const [from, setFrom] = useState(init.from)
  const [to, setTo] = useState(init.to)

  const [summary, setSummary] = useState<Summary | null>(null)
  const [orders, setOrders] = useState<OrderRow[]>([])
  const [affs, setAffs] = useState<AffSummary[]>([])
  const [prods, setProds] = useState<ProdSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = `?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`
      const [s, o, a, p] = await Promise.all([
        api<Summary>(`/cod-livro/summary${qs}`),
        api<{ items: OrderRow[]; count: number }>(`/cod-livro/orders${qs}&limit=300`),
        api<{ items: AffSummary[]; count: number }>(`/cod-livro/affiliates-summary${qs}`),
        api<{ items: ProdSummary[]; count: number }>(`/cod-livro/producers-summary${qs}`),
      ])
      setSummary(s)
      setOrders(o.items || [])
      setAffs(a.items || [])
      setProds(p.items || [])
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Erro ao carregar'
      setErr(msg)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from, to])

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Livro COD</h1>
          <p>
            Período: {fmtDate(from)} até {fmtDate(to)} — receita, repasses e taxas por pedido.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 12, alignItems: 'flex-end' }}>
          <div className="szv2-field">
            <label className="szv2-label" htmlFor="cod-from">De</label>
            <input
              id="cod-from"
              type="date"
              className="szv2-input"
              value={from}
              onChange={e => setFrom(e.target.value)}
              max={to}
              style={{ width: 160 }}
            />
          </div>
          <div className="szv2-field">
            <label className="szv2-label" htmlFor="cod-to">Até</label>
            <input
              id="cod-to"
              type="date"
              className="szv2-input"
              value={to}
              onChange={e => setTo(e.target.value)}
              min={from}
              style={{ width: 160 }}
            />
          </div>
        </div>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* KPIs */}
      {summary && (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(5, minmax(0,1fr))' }}>
          <KpiCard label="Bruto COD"        value={money(summary.bruto_cod)}         sub="sem frustrados" />
          <KpiCard label="Afiliados"        value={money(summary.afiliados)}         sub="repasse" />
          <KpiCard label="Taxas Senderzz"   value={money(summary.taxas_senderzz)}    sub="operação" />
          <KpiCard label="Líquido produtor" value={money(summary.liquido_produtor)}  sub="líquido" />
          <KpiCard label="Previsto produtor" value={money(summary.previsto_produtor)} sub="agendados/em aberto" danger />
        </div>
      )}

      {/* Tabela de pedidos (22 colunas) */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Livro COD por pedido</h2>
            <p className="szv2-card-sub">{orders.length} registro(s) — limite 300</p>
          </div>
        </div>

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : orders.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum pedido no período selecionado.
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table" style={{ minWidth: 2400 }}>
              <thead>
                <tr>
                  <th>Pedido</th>
                  <th>Data</th>
                  <th>Situação</th>
                  <th>Afiliado</th>
                  <th>E-mail afiliado</th>
                  <th>Produtor</th>
                  <th style={{ textAlign: 'right' }}>Comissão %</th>
                  <th style={{ textAlign: 'right' }}>Valor pedido</th>
                  <th style={{ textAlign: 'right' }}>Bruto válido</th>
                  <th style={{ textAlign: 'right' }}>Taxas Senderzz</th>
                  <th style={{ textAlign: 'right' }}>Taxa entrega</th>
                  <th style={{ textAlign: 'right' }}>Taxa transação</th>
                  <th style={{ textAlign: 'right' }}>Valor afiliado</th>
                  <th style={{ textAlign: 'right' }}>Líquido produtor</th>
                  <th style={{ textAlign: 'right' }}>Valor não recebido</th>
                  <th style={{ textAlign: 'right' }}>Bruto estornado</th>
                  <th style={{ textAlign: 'right' }}>Frustrado afiliado</th>
                  <th style={{ textAlign: 'right' }}>Frustrado produtor</th>
                  <th style={{ textAlign: 'right' }}>Repasse pendente</th>
                  <th style={{ textAlign: 'right' }}>Repasse disponível</th>
                  <th style={{ textAlign: 'right' }}>Repasse estornado</th>
                  <th>Status repasse</th>
                </tr>
              </thead>
              <tbody>
                {orders.map(o => (
                  <tr key={o.order_id}>
                    <td><strong>#{o.order_id}</strong></td>
                    <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>{fmtDate(o.data_pedido)}</td>
                    <td><SituacaoBadge situacao={o.situacao} /></td>
                    <td style={{ fontSize: 13 }}>
                      {o.affiliate_id > 0
                        ? o.affiliate_name
                          ? <span title={`#${o.affiliate_id}`}>{o.affiliate_name}</span>
                          : `#${o.affiliate_id}`
                        : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{o.affiliate_email || '—'}</td>
                    <td style={{ fontSize: 13 }}>
                      {o.producer_id > 0 ? `#${o.producer_id}` : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td className="szv2-td-num">{fmtPct(o.commission_pct)}</td>
                    <td className="szv2-td-num">{money(o.valor_pedido)}</td>
                    <td className="szv2-td-num">{money(o.bruto_valido)}</td>
                    <td className="szv2-td-num">{money(o.taxas_senderzz)}</td>
                    <td className="szv2-td-num">{money(o.taxa_entrega)}</td>
                    <td className="szv2-td-num">{money(o.taxa_transacao)}</td>
                    <td className="szv2-td-num">{money(o.valor_afiliado)}</td>
                    <td className="szv2-td-num" style={{ fontWeight: 600 }}>{money(o.liquido_produtor)}</td>
                    <td className="szv2-td-num" style={o.valor_nao_recebido > 0 ? { color: 'var(--szv2-danger)' } : undefined}>
                      {money(o.valor_nao_recebido)}
                    </td>
                    <td className="szv2-td-num">{money(o.bruto_estornado)}</td>
                    <td className="szv2-td-num">{money(o.frustrado_afiliado)}</td>
                    <td className="szv2-td-num">{money(o.frustrado_produtor)}</td>
                    <td className="szv2-td-num">{money(o.repasse_pendente)}</td>
                    <td className="szv2-td-num">{money(o.repasse_disponivel)}</td>
                    <td className="szv2-td-num">{money(o.repasse_estornado)}</td>
                    <td><RepasseBadge status={o.status_repasse} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Resumo por afiliado */}
      <details className="szv2-card" style={{ marginTop: 24 }}>
        <summary
          style={{
            cursor: 'pointer',
            padding: '4px 0',
            fontWeight: 600,
            fontSize: 'var(--szv2-text-lg)',
          }}
        >
          Resumo por afiliado ({affs.length})
        </summary>
        <div style={{ overflowX: 'auto', marginTop: 12 }}>
          {affs.length === 0 ? (
            <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Sem afiliados no período.
            </div>
          ) : (
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>E-mail</th>
                  <th style={{ textAlign: 'right' }}>Pedidos</th>
                  <th style={{ textAlign: 'right' }}>Recebidos</th>
                  <th style={{ textAlign: 'right' }}>Previstos</th>
                  <th style={{ textAlign: 'right' }}>Frustrados</th>
                  <th style={{ textAlign: 'right' }}>Pendente</th>
                  <th style={{ textAlign: 'right' }}>Disponível</th>
                  <th style={{ textAlign: 'right' }}>Previsto (valor)</th>
                </tr>
              </thead>
              <tbody>
                {affs.map(a => (
                  <tr key={a.affiliate_id}>
                    <td>
                      <strong>{a.affiliate_name || `#${a.affiliate_id}`}</strong>
                      {a.affiliate_name && <span style={{ fontSize: 11, color: 'var(--szv2-text-faint)', marginLeft: 4 }}>#{a.affiliate_id}</span>}
                    </td>
                    <td style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>{a.affiliate_email || '—'}</td>
                    <td className="szv2-td-num">{a.pedidos}</td>
                    <td className="szv2-td-num">{a.recebidos}</td>
                    <td className="szv2-td-num">{a.previstos}</td>
                    <td className="szv2-td-num">{a.frustrados}</td>
                    <td className="szv2-td-num">{money(a.pendente)}</td>
                    <td className="szv2-td-num">{money(a.disponivel)}</td>
                    <td className="szv2-td-num">{money(a.previsto_valor)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </details>

      {/* Resumo por produtor */}
      <details className="szv2-card" style={{ marginTop: 16 }}>
        <summary
          style={{
            cursor: 'pointer',
            padding: '4px 0',
            fontWeight: 600,
            fontSize: 'var(--szv2-text-lg)',
          }}
        >
          Resumo por produtor ({prods.length})
        </summary>
        <div style={{ overflowX: 'auto', marginTop: 12 }}>
          {prods.length === 0 ? (
            <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Sem produtores no período.
            </div>
          ) : (
            <table className="szv2-table" style={{ minWidth: 1400 }}>
              <thead>
                <tr>
                  <th>ID</th>
                  <th>E-mail</th>
                  <th style={{ textAlign: 'right' }}>Pedidos</th>
                  <th style={{ textAlign: 'right' }}>Recebidos</th>
                  <th style={{ textAlign: 'right' }}>Frustrados</th>
                  <th style={{ textAlign: 'right' }}>Previstos</th>
                  <th style={{ textAlign: 'right' }}>Bruto</th>
                  <th style={{ textAlign: 'right' }}>Bruto previsto</th>
                  <th style={{ textAlign: 'right' }}>Taxas Senderzz</th>
                  <th style={{ textAlign: 'right' }}>Afiliado</th>
                  <th style={{ textAlign: 'right' }}>Líquido produtor</th>
                  <th style={{ textAlign: 'right' }}>Frustrado produtor</th>
                  <th style={{ textAlign: 'right' }}>Frustrado afiliados</th>
                  <th style={{ textAlign: 'right' }}>Frustrado (valor)</th>
                </tr>
              </thead>
              <tbody>
                {prods.map(p => (
                  <tr key={p.producer_id}>
                    <td><strong>#{p.producer_id}</strong></td>
                    <td style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>{p.producer_email || '—'}</td>
                    <td className="szv2-td-num">{p.pedidos}</td>
                    <td className="szv2-td-num">{p.recebidos}</td>
                    <td className="szv2-td-num">{p.frustrados}</td>
                    <td className="szv2-td-num">{p.previstos}</td>
                    <td className="szv2-td-num">{money(p.bruto)}</td>
                    <td className="szv2-td-num">{money(p.bruto_previsto)}</td>
                    <td className="szv2-td-num">{money(p.taxas_senderzz)}</td>
                    <td className="szv2-td-num">{money(p.afiliado)}</td>
                    <td className="szv2-td-num" style={{ fontWeight: 600 }}>{money(p.liquido_produtor)}</td>
                    <td className="szv2-td-num">{money(p.frustrado_produtor)}</td>
                    <td className="szv2-td-num">{money(p.frustrado_afiliados)}</td>
                    <td className="szv2-td-num">{money(p.frustrado_valor)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </details>
    </div>
  )
}
