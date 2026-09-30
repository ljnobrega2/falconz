import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FalkDatePicker from '../components/FalkDatePicker'

type ChannelSummary = {
  revenue: number
  costs: number
  profit: number
  margin_pct: number
  orders: number
  transactions?: number
  unavailable?: number
}

type ExpeditionRow = {
  number: number
  date: string
  carrier: string
  charged: number
  real_cost: number
  margin: number
  status: string
}

type ExpeditionData = {
  total_charged: number
  total_real: number
  total_margin: number
  margin_pct: number
  total_orders: number
  unavailable: number
  rows: ExpeditionRow[]
}

type ExpeditionSummary = ChannelSummary & { items: ExpeditionRow[] }

type CODItem = {
  order_id: number
  order_number: string
  date: string
  status: string
  delivery_fee: number
  affiliate_fee: number
  producer_fee: number
  frustrated_fee: number
  other_revenue: number
  revenue: number
  motoboy_cost: number
  profit: number
}

type CODSummary = ChannelSummary & {
  other_fees: number
  items: CODItem[]
}

// Compatibilidade durante deploy gradual: o labels-service usa { data },
// enquanto versões antigas do admin-service devolviam o objeto diretamente.
type ExpeditionResponse = { data: ExpeditionData } | ExpeditionData

function expeditionData(response: ExpeditionResponse): ExpeditionData {
  const data = 'data' in response ? response.data : response
  if (!data || typeof data.total_charged !== 'number') {
    throw new Error('Resposta inválida do relatório de expedição.')
  }
  return data
}

function isoLocal(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

function initialPeriod() {
  const now = new Date()
  return { from: isoLocal(new Date(now.getFullYear(), now.getMonth(), 1)), to: isoLocal(now) }
}

function brl(value: number) {
  return Number(value || 0).toLocaleString('pt-BR', {
    style: 'currency',
    currency: 'BRL',
    minimumFractionDigits: 2,
  })
}

function pct(value: number) {
  return `${Number(value || 0).toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}%`
}

function dateBR(value: string) {
  const [y, m, d] = String(value || '').slice(0, 10).split('-')
  return y && m && d ? `${d}/${m}/${y}` : '—'
}

function statusLabel(value: string) {
  const labels: Record<string, string> = {
    entregue: 'Entregue',
    frustrado: 'Frustrado',
    completo: 'Completo',
    completed: 'Completo',
    posted: 'Postada',
    delivered: 'Entregue',
    canceled: 'Cancelada',
  }
  return labels[value] || value || '—'
}

function ChannelCard({ title, subtitle, data, loading, error }: {
  title: string
  subtitle: string
  data: ChannelSummary | null
  loading: boolean
  error: string
}) {
  return (
    <section className="szv2-card" style={{ minWidth: 0 }}>
      <div className="szv2-card-head" style={{ marginBottom: 20 }}>
        <div>
          <h2 style={{ margin: 0 }}>{title}</h2>
          <p style={{ margin: '4px 0 0', color: 'var(--szv2-text-muted)', fontSize: 13 }}>{subtitle}</p>
        </div>
      </div>

      {loading ? (
        <div style={{ color: 'var(--szv2-text-muted)', padding: '28px 0' }}>Calculando…</div>
      ) : error ? (
        <div className="sz-alert-danger">{error}</div>
      ) : data && (
        <>
          <div style={{ padding: '18px 0 22px', borderBottom: '1px solid var(--szv2-divider)' }}>
            <div style={{ color: 'var(--szv2-text-muted)', fontSize: 13, fontWeight: 700 }}>LUCRO OPERACIONAL</div>
            <div style={{ color: data.profit >= 0 ? 'var(--szv2-success)' : 'var(--szv2-danger)', fontSize: 36, fontWeight: 800, lineHeight: 1.2, marginTop: 6 }}>
              {brl(data.profit)}
            </div>
            <div style={{ color: 'var(--szv2-text-muted)', fontSize: 13, marginTop: 4 }}>Margem de {pct(data.margin_pct)}</div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, paddingTop: 20 }}>
            <div>
              <div style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>Receita</div>
              <strong style={{ display: 'block', fontSize: 21, marginTop: 4 }}>{brl(data.revenue)}</strong>
            </div>
            <div>
              <div style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>Custos</div>
              <strong style={{ display: 'block', fontSize: 21, marginTop: 4 }}>{brl(data.costs)}</strong>
            </div>
          </div>

          <p style={{ margin: '20px 0 0', color: 'var(--szv2-text-muted)', fontSize: 12 }}>
            {data.orders.toLocaleString('pt-BR')} pedido(s) contabilizado(s)
            {!!data.unavailable && ` · ${data.unavailable} custo(s) indisponível(is)`}
          </p>
        </>
      )}
    </section>
  )
}

export default function OperationalProfit() {
  const initial = useMemo(initialPeriod, [])
  const [from, setFrom] = useState(initial.from)
  const [to, setTo] = useState(initial.to)
  const [expedition, setExpedition] = useState<ExpeditionSummary | null>(null)
  const [cod, setCod] = useState<CODSummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [expeditionError, setExpeditionError] = useState('')
  const [codError, setCodError] = useState('')

  useEffect(() => {
    if (!from || !to || from > to) return
    const params = new URLSearchParams({ date_from: from, date_to: to }).toString()
    setLoading(true)
    setExpeditionError('')
    setCodError('')

    Promise.allSettled([
      api<ExpeditionResponse>(`/labels/margin-report?${params}`),
      api<CODSummary>(`/operational-profit/cod?${params}`),
    ]).then(([expResult, codResult]) => {
      if (expResult.status === 'fulfilled') {
        try {
          const d = expeditionData(expResult.value)
          setExpedition({
            revenue: d.total_charged,
            costs: d.total_real,
            profit: d.total_margin,
            margin_pct: d.margin_pct,
            orders: d.total_orders,
            unavailable: d.unavailable,
            items: d.rows ?? [],
          })
        } catch (error) {
          setExpedition(null)
          setExpeditionError(error instanceof Error ? error.message : 'Não foi possível calcular a expedição.')
        }
      } else {
        setExpedition(null)
        setExpeditionError(expResult.reason instanceof Error ? expResult.reason.message : 'Não foi possível calcular a expedição.')
      }
      if (codResult.status === 'fulfilled') {
        setCod(codResult.value)
      } else {
        setCod(null)
        setCodError(codResult.reason instanceof Error ? codResult.reason.message : 'Não foi possível calcular o COD.')
      }
    }).finally(() => setLoading(false))
  }, [from, to])

  const invalidPeriod = !!from && !!to && from > to

  return (
    <div>
      <div className="szv2-section-head" style={{ marginBottom: 20 }}>
        <div>
          <h1>Lucro operacional</h1>
          <p>Resultado simples da operação, separado por canal</p>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '150px 150px', gap: 8 }}>
          <FalkDatePicker value={from} onChange={setFrom} max={to || undefined} aria-label="Data inicial" />
          <FalkDatePicker value={to} onChange={setTo} min={from || undefined} aria-label="Data final" />
        </div>
      </div>

      {invalidPeriod && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>A data inicial deve ser anterior à data final.</div>}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: 20 }}>
        <ChannelCard
          title="Expedição"
          subtitle="Cobrado − custo real do Melhor Envio"
          data={expedition}
          loading={loading}
          error={expeditionError}
        />
        <ChannelCard
          title="COD"
          subtitle="Taxas da operação − ganhos dos motoboys"
          data={cod}
          loading={loading}
          error={codError}
        />
      </div>

      <section className="szv2-card" style={{ marginTop: 20 }}>
        <div className="szv2-card-head" style={{ marginBottom: 8 }}>
          <div>
            <h2 style={{ margin: 0 }}>Expedição por pedido</h2>
            <p style={{ margin: '4px 0 0', color: 'var(--szv2-text-muted)', fontSize: 13 }}>
              Cobrado é o valor da etiqueta; custo ME é o débito real no Melhor Envio.
            </p>
          </div>
        </div>
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>Pedido</th><th>Data</th><th>Transportadora</th><th>Status</th>
                <th>Cobrado</th><th>Custo ME</th><th>Lucro</th>
              </tr>
            </thead>
            <tbody>
              {(expedition?.items ?? []).map(item => (
                <tr key={`${item.number}-${item.date}`}>
                  <td><strong>#{item.number}</strong></td>
                  <td>{dateBR(item.date)}</td>
                  <td>{item.carrier || '—'}</td>
                  <td>{statusLabel(item.status)}</td>
                  <td>{brl(item.charged)}</td>
                  <td>{brl(item.real_cost)}</td>
                  <td style={{ color: item.margin >= 0 ? 'var(--szv2-success)' : 'var(--szv2-danger)', fontWeight: 700 }}>{brl(item.margin)}</td>
                </tr>
              ))}
              {!loading && !expeditionError && (expedition?.items.length ?? 0) === 0 && (
                <tr><td colSpan={7} style={{ textAlign: 'center', color: 'var(--szv2-text-muted)' }}>Nenhuma etiqueta ativa no período.</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      <section className="szv2-card" style={{ marginTop: 20 }}>
        <div className="szv2-card-head" style={{ marginBottom: 8 }}>
          <div>
            <h2 style={{ margin: 0 }}>COD por pedido</h2>
            <p style={{ margin: '4px 0 0', color: 'var(--szv2-text-muted)', fontSize: 13 }}>
              Entrega + taxas da plataforma − repasse do motoboy = lucro do pedido.
              {!!cod?.other_fees && ` Taxas de saque/antecipação sem pedido: ${brl(cod.other_fees)}.`}
            </p>
          </div>
        </div>
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>Pedido</th><th>Data</th><th>Status</th><th>Entrega</th>
                <th>Taxa afiliado</th><th>Taxa produtor</th><th>Frustrado</th>
                <th>Outras</th><th>Receita</th><th>Repasse motoboy</th><th>Lucro</th>
              </tr>
            </thead>
            <tbody>
              {(cod?.items ?? []).map(item => (
                <tr key={item.order_id}>
                  <td><strong>#{item.order_number || item.order_id}</strong></td>
                  <td>{dateBR(item.date)}</td>
                  <td>{statusLabel(item.status)}</td>
                  <td>{brl(item.delivery_fee)}</td>
                  <td>{brl(item.affiliate_fee)}</td>
                  <td>{brl(item.producer_fee)}</td>
                  <td>{brl(item.frustrated_fee)}</td>
                  <td>{brl(item.other_revenue)}</td>
                  <td><strong>{brl(item.revenue)}</strong></td>
                  <td>{brl(item.motoboy_cost)}</td>
                  <td style={{ color: item.profit >= 0 ? 'var(--szv2-success)' : 'var(--szv2-danger)', fontWeight: 700 }}>{brl(item.profit)}</td>
                </tr>
              ))}
              {!loading && !codError && (cod?.items.length ?? 0) === 0 && (
                <tr><td colSpan={11} style={{ textAlign: 'center', color: 'var(--szv2-text-muted)' }}>Nenhum pedido COD no período.</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
