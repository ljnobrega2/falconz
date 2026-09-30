import { useEffect, useState } from 'react'
import { api } from '../api'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import { brDate } from '../utils/format'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// Shape do endpoint users-by-role (users_by_role.go): user_id (não id),
// status string 'ativo'/'inativo' (não boolean ativo).
type User = {
  user_id: number
  email: string
  nome: string
  telefone?: string
  status: string
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

export default function AdminUsers() {
  const [items, setItems] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [err, setErr] = useState('')
  const [q, setQ] = useState('')

  // Painel de filtros (FilterTopPanel) + draft (aplicado só ao confirmar).
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftQ, setDraftQ] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<{ items: User[]; total: number }>(
        '/users-by-role?role=admin&limit=200'
      )
      const list = r.items ?? []
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
    const hay = `${u.nome} ${u.email} ${u.telefone ?? ''}`.toLowerCase()
    return hay.includes(qNorm)
  })

  function openPanel() { setDraftQ(q); setFilterOpen(true) }
  function applyFilters() { setQ(draftQ); setFilterOpen(false) }
  function clearFilters() { setQ(''); setDraftQ(''); setFilterOpen(false) }

  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Administradores</h1>
          <p>{filtered.length} de {total} administrador(es)</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={chips.length > 0} count={chips.length} onClick={openPanel} />
        </div>
      </div>

      <div className="szv2-kpi-grid" style={{ marginBottom: 24 }}>
        <KpiCard label="Administradores cadastrados" value={total} />
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger">{err}</div>}

      {loading && filtered.length === 0 ? (
        <TableSkeleton rows={6} cols={4} />
      ) : !loading && filtered.length === 0 ? (
        <EmptyState
          icon="🛡️"
          title={
            items.length === 0
              ? 'Nenhum administrador cadastrado ainda.'
              : 'Nenhum administrador encontrado com essa busca.'
          }
          description={
            items.length === 0
              ? 'Os administradores aparecerão aqui assim que forem cadastrados.'
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
                <tr key={u.user_id}>
                  <td>
                    <div style={{ fontWeight: 500 }}>{u.nome || '—'}</div>
                    <div style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                      {u.email}
                    </div>
                  </td>
                  <td style={{ color: 'var(--szv2-text-soft)' }}>{u.telefone || '—'}</td>
                  <td>
                    {u.status === 'ativo' ? (
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

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Busca (nome / e-mail / telefone)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: joao@…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
