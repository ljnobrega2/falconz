import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'

type L = { id: number; wc_order_id: number; me_shipment_id: string | null; status: string; service_name: string | null; tracking_code: string | null; created_at: string }

const ST_CLS: Record<string, string> = {
  draft:    'szv2-badge-neutral',
  pending:  'szv2-badge-warning',
  queued:   'szv2-badge-info',
  printed:  'szv2-badge-info',
  released: 'szv2-badge-success',
  cancelled:'szv2-badge-danger',
}

function defaultRange() {
  const today = new Date()
  const past = new Date(today)
  past.setDate(past.getDate() - 30)
  const fmt = (d: Date) => d.toISOString().slice(0, 10)
  return { ini: fmt(past), fim: fmt(today) }
}

export default function Labels() {
  const init = useMemo(defaultRange, [])
  const [items, setItems] = useState<L[]>([])
  const [total, setTotal] = useState(0)
  const [status, setStatus] = useState('')
  const [dataIni, setDataIni] = useState(init.ini)
  const [dataFim, setDataFim] = useState(init.fim)
  const [q, setQ] = useState('')
  const [err, setErr] = useState('')

  function buildQs() {
    const p = new URLSearchParams()
    if (status) p.set('status', status)
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    if (q.trim()) p.set('q', q.trim())
    p.set('limit', '100')
    return p.toString()
  }

  useEffect(() => {
    api<{ items: L[]; total: number }>(`/labels?${buildQs()}`)
      .then(r => { setItems(r.items || []); setTotal(r.total) })
      .catch(e => setErr(e.message))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, dataIni, dataFim, q])

  function limpar() {
    setStatus('')
    setDataIni(init.ini)
    setDataFim(init.fim)
    setQ('')
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Etiquetas Melhor Envio</h1>
          <p>{total} etiquetas no total</p>
        </div>
      </div>

      {/* Filtros */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <div className="szv2-field">
            <label className="szv2-label">De</label>
            <input
              type="date"
              className="szv2-input"
              value={dataIni}
              onChange={e => setDataIni(e.target.value)}
              max={dataFim}
              style={{ width: 160 }}
            />
          </div>
          <div className="szv2-field">
            <label className="szv2-label">Até</label>
            <input
              type="date"
              className="szv2-input"
              value={dataFim}
              onChange={e => setDataFim(e.target.value)}
              min={dataIni}
              style={{ width: 160 }}
            />
          </div>
          <div className="szv2-field">
            <label className="szv2-label">Status</label>
            <select
              className="szv2-select"
              style={{ width: 180 }}
              value={status}
              onChange={e => setStatus(e.target.value)}
            >
              <option value="">Todos status</option>
              {Object.keys(ST_CLS).map(s => <option key={s} value={s}>{s}</option>)}
            </select>
          </div>
          <div className="szv2-field">
            <label className="szv2-label">Busca</label>
            <input
              type="search"
              className="szv2-input"
              placeholder="pedido ou rastreio"
              style={{ width: 200 }}
              value={q}
              onChange={e => setQ(e.target.value)}
            />
          </div>
          <button className="szv2-btn szv2-btn-secondary" onClick={limpar}>
            Limpar filtros
          </button>
        </div>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Pedido</th>
              <th>Shipment ID</th>
              <th>Serviço</th>
              <th>Rastreio</th>
              <th>Status</th>
              <th>Criado</th>
            </tr>
          </thead>
          <tbody>
            {items.map(l => (
              <tr key={l.id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>#{l.id}</td>
                <td style={{ fontWeight: 600 }}>#{l.wc_order_id}</td>
                <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '11px', color: 'var(--szv2-text-muted)', maxWidth: '120px', overflow: 'hidden', textOverflow: 'ellipsis' }}>{l.me_shipment_id ?? '—'}</td>
                <td style={{ fontSize: '13px' }}>{l.service_name ?? '—'}</td>
                <td>
                  {l.tracking_code
                    ? <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px', background: 'var(--szv2-neutral-bg)', padding: '2px 8px', borderRadius: '6px' }}>{l.tracking_code}</span>
                    : <span style={{ color: 'var(--szv2-text-faint)', fontSize: '12px' }}>—</span>}
                </td>
                <td><span className={`sz-badge ${ST_CLS[l.status] || 'szv2-badge-neutral'}`}>{l.status}</span></td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>{l.created_at.slice(0, 16).replace('T', ' ')}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td colSpan={7}><div className="szv2-empty"><h3>Nenhuma etiqueta</h3><p>Tente outro filtro.</p></div></td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
