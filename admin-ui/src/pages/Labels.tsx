import { useEffect, useState } from 'react'
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

export default function Labels() {
  const [items, setItems] = useState<L[]>([])
  const [total, setTotal] = useState(0)
  const [status, setStatus] = useState('')
  const [err, setErr] = useState('')

  useEffect(() => {
    api<{ items: L[]; total: number }>(`/labels?status=${status}&limit=100`)
      .then(r => { setItems(r.items || []); setTotal(r.total) })
      .catch(e => setErr(e.message))
  }, [status])

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Etiquetas Melhor Envio</h1>
          <p>{total} etiquetas no total</p>
        </div>
        <select className="szv2-select" style={{ width: '200px' }} value={status} onChange={e => setStatus(e.target.value)}>
          <option value="">Todos status</option>
          {Object.keys(ST_CLS).map(s => <option key={s} value={s}>{s}</option>)}
        </select>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Pedido WC</th>
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
