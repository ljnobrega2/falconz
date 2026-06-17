import { useEffect, useState } from 'react'
import { api } from '../api'

type A = { id: number; user_id: number; email: string; nome: string; affiliate_code: string | null; comissao_pct: number; status: string; created_at: string }

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

      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Email</th>
              <th>Nome</th>
              <th>Código</th>
              <th className="szv2-td-num">Comissão</th>
              <th>Status</th>
              <th>Desde</th>
            </tr>
          </thead>
          <tbody>
            {items.map(a => (
              <tr key={a.id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>#{a.id}</td>
                <td style={{ fontWeight: 500 }}>{a.email}</td>
                <td style={{ color: 'var(--szv2-text-soft)' }}>{a.nome}</td>
                <td>
                  {a.affiliate_code
                    ? <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', padding: '2px 8px', borderRadius: '6px' }}>{a.affiliate_code}</span>
                    : <span style={{ color: 'var(--szv2-text-faint)', fontSize: '12px' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-brand)', fontWeight: 700 }}>{a.comissao_pct}%</td>
                <td>
                  <span className={`szv2-status-badge ${a.status === 'ativo' ? 's-confirmado' : a.status === 'pendente' ? 's-pendente' : 's-cancelado'}`}>
                    {a.status}
                  </span>
                </td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>{a.created_at.slice(0, 10)}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td colSpan={7}><div className="szv2-empty"><h3>Nenhum afiliado</h3><p>Tente ajustar o filtro.</p></div></td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
