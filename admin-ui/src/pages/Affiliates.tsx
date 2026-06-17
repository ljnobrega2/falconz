import { useEffect, useState } from 'react'
import { api } from '../api'

type A = {
  user_id: number
  email: string
  nome: string
  affiliate_code: string | null
  comissao_pct: number
  status: string
  created_at: string
  vinculos: number
  total_vendido_30d: number
  total_comissao_30d: number
  pedidos_count_30d: number
}

const fmtBRL = (v: number) =>
  'R$ ' + (v ?? 0).toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

export default function Affiliates() {
  const [items, setItems] = useState<A[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState('')
  const [err, setErr] = useState('')

  async function load() {
    try {
      const r = await api<{ items: A[]; total: number }>(`/affiliates?q=${encodeURIComponent(q)}&limit=100`)
      setItems(r.items ?? []); setTotal(r.total)
    } catch (e: any) { setErr(e.message) }
  }
  useEffect(() => { load() }, [])

  // KPIs agregados sobre a página atual (últimos 30 dias).
  const sumVendido = items.reduce((s, a) => s + (a.total_vendido_30d || 0), 0)
  const sumComissao = items.reduce((s, a) => s + (a.total_comissao_30d || 0), 0)
  const sumPedidos = items.reduce((s, a) => s + (a.pedidos_count_30d || 0), 0)

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Afiliados</h1>
          <p>{total} afiliados cadastrados</p>
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          <input className="szv2-input" style={{ width: '220px' }} placeholder="buscar…" value={q}
            onChange={e => setQ(e.target.value)} onKeyDown={e => e.key === 'Enter' && load()} />
          <button className="szv2-btn szv2-btn-secondary" onClick={load}>Buscar</button>
        </div>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {/* KPIs últimos 30d (página atual) */}
      <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 12 }}>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Vendido (30d) — página</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{fmtBRL(sumVendido)}</span>
          <span className="szv2-kpi-meta">{sumPedidos} pedido(s)</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Comissões geradas (30d)</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-success)' }}>{fmtBRL(sumComissao)}</span>
          <span className="szv2-kpi-meta">soma da página</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Afiliados ativos (página)</span>
          <span className="szv2-kpi-value">{items.length}</span>
          <span className="szv2-kpi-meta">de {total} total</span>
        </div></div>
      </div>

      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Email</th>
              <th>Nome</th>
              <th>Código</th>
              <th className="szv2-td-num">Comissão</th>
              <th className="szv2-td-num">Vínculos</th>
              <th className="szv2-td-num">Vendido (30d)</th>
              <th className="szv2-td-num">Comissão (30d)</th>
              <th className="szv2-td-num">Pedidos (30d)</th>
              <th>Status</th>
              <th>Desde</th>
            </tr>
          </thead>
          <tbody>
            {items.map(a => (
              <tr key={a.user_id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>#{a.user_id}</td>
                <td style={{ fontWeight: 500 }}>{a.email}</td>
                <td style={{ color: 'var(--szv2-text-soft)' }}>{a.nome}</td>
                <td>
                  {a.affiliate_code
                    ? <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', padding: '2px 8px', borderRadius: '6px' }}>{a.affiliate_code}</span>
                    : <span style={{ color: 'var(--szv2-text-faint)', fontSize: '12px' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-brand)', fontWeight: 700 }}>{a.comissao_pct}%</td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-soft)', fontWeight: 600 }}>{a.vinculos}</td>
                <td className="szv2-td-num" style={{ fontSize: 13 }}>
                  {a.total_vendido_30d > 0 ? fmtBRL(a.total_vendido_30d) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-success)', fontWeight: 600 }}>
                  {a.total_comissao_30d > 0 ? fmtBRL(a.total_comissao_30d) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-muted)' }}>{a.pedidos_count_30d || 0}</td>
                <td>
                  <span className={`szv2-status-badge ${a.status === 'active' || a.status === 'ativo' ? 's-confirmado' : a.status === 'pending' || a.status === 'pendente' ? 's-pendente' : 's-cancelado'}`}>
                    {a.status}
                  </span>
                </td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>{a.created_at ? a.created_at.slice(0, 10) : '—'}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td colSpan={11}><div className="szv2-empty"><h3>Nenhum afiliado</h3><p>Tente ajustar o filtro.</p></div></td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
