import { useEffect, useState } from 'react'
import { api } from '../api'

type User = { id: number; email: string; nome: string; role: string; ativo: boolean; plano: string; created_at: string }

function roleBadge(role: string) {
  const map: Record<string, string> = {
    produtor:  'role-produtor',
    affiliate: 'role-affiliate',
    afiliado:  'role-affiliate',
    operator:  'role-operator',
  }
  return <span className={`szv2-role-badge ${map[role] || 'role-default'}`}>{role}</span>
}

function planBadge(plano: string) {
  const colors: Record<string, string> = { pro: 'var(--szv2-brand)', enterprise: 'var(--szv2-info)' }
  const color = colors[plano] || 'var(--szv2-neutral)'
  return <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '11px', color }}>{plano}</span>
}

export default function Users() {
  const [items, setItems] = useState<User[]>([])
  const [q, setQ] = useState('')
  const [total, setTotal] = useState(0)
  const [err, setErr] = useState('')

  async function load() {
    try {
      const r = await api<{ items: User[]; total: number }>(`/users?q=${encodeURIComponent(q)}&limit=100`)
      setItems(r.items ?? []); setTotal(r.total)
    } catch (e: any) { setErr(e.message) }
  }
  useEffect(() => { load() }, [])

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Usuários do Portal</h1>
          <p>{total} usuários cadastrados</p>
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          <input
            className="szv2-input"
            style={{ width: '240px' }}
            placeholder="buscar email / nome…"
            value={q}
            onChange={e => setQ(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && load()}
          />
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
              <th>Role</th>
              <th>Plano</th>
              <th>Ativo</th>
              <th>Criado</th>
            </tr>
          </thead>
          <tbody>
            {items.map(u => (
              <tr key={u.id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>#{u.id}</td>
                <td style={{ fontWeight: 500 }}>{u.email}</td>
                <td style={{ color: 'var(--szv2-text-soft)' }}>{u.nome}</td>
                <td>{roleBadge(u.role)}</td>
                <td>{planBadge(u.plano)}</td>
                <td>
                  {u.ativo
                    ? <span className="sz-badge szv2-badge-success">Ativo</span>
                    : <span className="sz-badge szv2-badge-neutral">Inativo</span>}
                </td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  {u.created_at.slice(0, 10)}
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td colSpan={7}>
                <div className="szv2-empty">
                  <h3>Nenhum usuário</h3>
                  <p>Tente ajustar o filtro de busca.</p>
                </div>
              </td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
