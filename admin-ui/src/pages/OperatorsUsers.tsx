import { useEffect, useState } from 'react'
import { api } from '../api'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import { brDate } from '../utils/format'

type User = {
  id: number
  email: string
  nome: string
  telefone?: string
  ativo: boolean
  created_at: string
}

// Shape cru do backend: /users-by-role devolve user_id + status (string "ativo"),
// não id/ativo. Mapeamos em load() para o tipo User da UI.
type ApiUser = {
  user_id?: number
  id?: number
  email: string
  nome: string
  telefone?: string
  status?: string
  ativo?: boolean
  created_at: string
}

function KpiCard({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>
          {value}
        </span>
      </div>
    </div>
  )
}

export default function OperatorsUsers() {
  const [items, setItems] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [err, setErr] = useState('')
  const [q, setQ] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<{ items: ApiUser[]; total: number }>(
        '/users-by-role?role=operator&limit=200'
      )
      // Normaliza shape do backend (user_id/status) → tipo da UI (id/ativo).
      const list: User[] = (r.items ?? []).map((it) => ({
        id: it.user_id ?? it.id ?? 0,
        email: it.email,
        nome: it.nome,
        telefone: it.telefone,
        ativo: it.status ? it.status === 'ativo' : !!it.ativo,
        created_at: it.created_at,
      }))
      setItems(list)
      setTotal(r.total ?? list.length)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const qNorm = q.trim().toLowerCase()
  const filtered = items.filter((u) => {
    if (!qNorm) return true
    // Regra do dono: busca só por nome, telefone e CPF (sem e-mail).
    // Este tipo não tem CPF → escopo = nome + telefone.
    const hay = `${u.nome} ${u.telefone ?? ''}`.toLowerCase()
    return hay.includes(qNorm)
  })

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Operadores Logísticos</h1>
          <p>{filtered.length} de {total} operador(es)</p>
        </div>
      </div>

      <div className="szv2-kpi-grid" style={{ marginBottom: 24 }}>
        <KpiCard label="Operadores cadastrados" value={total} />
      </div>

      <div className="szv2-card" style={{ marginBottom: 24 }}>
        <input
          className="szv2-input"
          type="search"
          aria-label="Buscar operadores por nome, telefone ou CPF"
          placeholder="Buscar por nome, telefone ou CPF"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {loading && filtered.length === 0 ? (
        <TableSkeleton rows={6} cols={4} />
      ) : !loading && filtered.length === 0 ? (
        <EmptyState
          icon="🚚"
          title={
            items.length === 0
              ? 'Nenhum operador logístico cadastrado ainda.'
              : 'Nenhum operador encontrado com essa busca.'
          }
          description={
            items.length === 0
              ? 'Os operadores logísticos aparecerão aqui assim que forem cadastrados.'
              : 'Ajuste o termo de busca ou limpe o filtro.'
          }
        />
      ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>Nome / Email</th>
                <th>Telefone</th>
                <th>Status</th>
                <th>Cadastrado em</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((u) => (
                <tr key={u.id}>
                  <td>
                    <div style={{ fontWeight: 500 }}>{u.nome || '—'}</div>
                    <div style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                      {u.email}
                    </div>
                  </td>
                  <td style={{ color: 'var(--szv2-text-soft)' }}>{u.telefone || '—'}</td>
                  <td>
                    {u.ativo ? (
                      <span className="sz-badge szv2-badge-success">Ativo</span>
                    ) : (
                      <span className="sz-badge szv2-badge-neutral">Inativo</span>
                    )}
                  </td>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                    {u.created_at ? brDate(u.created_at) : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
