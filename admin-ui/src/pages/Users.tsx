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

const ROLES = ['admin', 'operator', 'producer', 'produtor', 'affiliate', 'afiliado']

export default function Users() {
  const [items, setItems] = useState<User[]>([])
  const [q, setQ] = useState('')
  const [role, setRole] = useState('')
  const [ativo, setAtivo] = useState('') // '' | 'sim' | 'nao'
  const [total, setTotal] = useState(0)
  const [err, setErr] = useState('')

  async function load() {
    try {
      const p = new URLSearchParams()
      if (q.trim()) p.set('q', q.trim())
      if (role) p.set('role', role)
      if (ativo) p.set('ativo', ativo === 'sim' ? '1' : '0')
      p.set('limit', '100')
      const r = await api<{ items: User[]; total: number }>(`/users?${p.toString()}`)
      setItems(r.items ?? [])
      setTotal(r.total)
    } catch (e: any) { setErr(e.message) }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [role, ativo])

  // Aplica filtros client-side complementares (caso o backend ignore params)
  const qNorm = q.trim().toLowerCase()
  const filtered = items.filter(u => {
    if (qNorm) {
      const hay = `${u.nome} ${u.email}`.toLowerCase()
      if (!hay.includes(qNorm)) return false
    }
    if (role && u.role !== role) return false
    if (ativo === 'sim' && !u.ativo) return false
    if (ativo === 'nao' && u.ativo) return false
    return true
  })

  function limpar() {
    setQ('')
    setRole('')
    setAtivo('')
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Usuários do Portal</h1>
          <p>{filtered.length} de {total} usuário(s)</p>
        </div>
      </div>

      {/* Filtros */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <div className="szv2-field">
            <label className="szv2-label">Busca</label>
            <input
              type="search"
              className="szv2-input"
              placeholder="email / nome"
              style={{ width: 240 }}
              value={q}
              onChange={e => setQ(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && load()}
            />
          </div>
          <div className="szv2-field">
            <label className="szv2-label">Role</label>
            <select
              className="szv2-select"
              style={{ width: 180 }}
              value={role}
              onChange={e => setRole(e.target.value)}
            >
              <option value="">Todas roles</option>
              {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
            </select>
          </div>
          <div className="szv2-field">
            <label className="szv2-label">Ativo</label>
            <select
              className="szv2-select"
              style={{ width: 140 }}
              value={ativo}
              onChange={e => setAtivo(e.target.value)}
            >
              <option value="">Todos</option>
              <option value="sim">Sim</option>
              <option value="nao">Não</option>
            </select>
          </div>
          <button className="szv2-btn szv2-btn-secondary" onClick={load}>Buscar</button>
          {(q || role || ativo) && (
            <button className="szv2-btn szv2-btn-secondary" onClick={limpar}>
              Limpar filtros
            </button>
          )}
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
            {filtered.map(u => (
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
            {filtered.length === 0 && (
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
