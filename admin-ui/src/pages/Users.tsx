import { useEffect, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

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
  const [total, setTotal] = useState(0)
  const [err, setErr] = useState('')

  // Filtros aplicados (disparam fetch).
  const [q, setQ] = useState('')
  const [role, setRole] = useState('')
  const [ativo, setAtivo] = useState('') // '' | 'sim' | 'nao'
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts editados no painel — só aplicam ao confirmar.
  const [draftQ, setDraftQ] = useState('')
  const [draftRole, setDraftRole] = useState('')
  const [draftAtivo, setDraftAtivo] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')

  const [filterOpen, setFilterOpen] = useState(false)

  async function load() {
    try {
      const p = new URLSearchParams()
      if (q.trim()) p.set('q', q.trim())
      if (role) p.set('role', role)
      if (ativo) p.set('ativo', ativo === 'sim' ? '1' : '0')
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      p.set('limit', '100')
      const r = await api<{ items: User[]; total: number }>(`/users?${p.toString()}`)
      setItems(r.items ?? [])
      setTotal(r.total)
    } catch (e: any) { setErr(e.message) }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [q, role, ativo, dataIni, dataFim])

  // Aplica filtros client-side complementares (caso o backend ignore params).
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

  function openPanel() {
    setDraftQ(q); setDraftRole(role); setDraftAtivo(ativo); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setRole(draftRole); setAtivo(draftAtivo); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setRole(''); setAtivo(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftRole(''); setDraftAtivo(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (role) chips.push({ key: 'role', label: `Role: ${role}`, onRemove: () => setRole('') })
  if (ativo) chips.push({ key: 'ativo', label: `Ativo: ${ativo === 'sim' ? 'Sim' : 'Não'}`, onRemove: () => setAtivo('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Usuários do Portal</h1>
          <p>{filtered.length} de {total} usuário(s)</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

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

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <input
            type="date"
            style={filterInputStyle}
            value={draftIni}
            max={draftFim || undefined}
            onChange={e => setDraftIni(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFim}
            min={draftIni || undefined}
            onChange={e => setDraftFim(e.target.value)}
          />
        </FilterField>
        <FilterField label="Role">
          <select
            style={filterInputStyle}
            value={draftRole}
            onChange={e => setDraftRole(e.target.value)}
          >
            <option value="">Todas roles</option>
            {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
          </select>
        </FilterField>
        <FilterField label="Ativo">
          <select
            style={filterInputStyle}
            value={draftAtivo}
            onChange={e => setDraftAtivo(e.target.value)}
          >
            <option value="">Todos</option>
            <option value="sim">Sim</option>
            <option value="nao">Não</option>
          </select>
        </FilterField>
        <FilterField label="Busca (email / nome)">
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
