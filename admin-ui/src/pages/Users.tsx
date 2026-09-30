import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import { brDate } from '../utils/format'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'

type User = { id: number; email: string; nome: string; role: string; ativo: boolean; created_at: string }

function roleBadge(role: string) {
  const map: Record<string, string> = {
    produtor:  'role-produtor',
    affiliate: 'role-affiliate',
    afiliado:  'role-affiliate',
    operator:  'role-operator',
  }
  return <span className={`szv2-role-badge ${map[role] || 'role-default'}`}>{role}</span>
}

const ROLES = ['admin', 'operator', 'producer', 'produtor', 'affiliate', 'afiliado']

export default function Users() {
  const navigate = useNavigate()
  const showToast = useToast()
  const [items, setItems] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [err, setErr] = useState('')
  const [promoting, setPromoting] = useState<number | null>(null)

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
    setLoading(true)
    setErr('')
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
    finally { setLoading(false) }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [q, role, ativo, dataIni, dataFim])

  async function promoteToProducer(u: User) {
    if (!confirm(`Promover ${u.nome || u.email} a produtor?`)) return
    setPromoting(u.id)
    try {
      await api(`/users/${u.id}`, { method: 'PUT', body: JSON.stringify({ role: 'produtor' }) })
      showToast('ok', `${u.nome || u.email} promovido a produtor.`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao promover a produtor')
    } finally {
      setPromoting(null)
    }
  }

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
    if (dataIni && u.created_at.slice(0, 10) < dataIni) return false
    if (dataFim && u.created_at.slice(0, 10) > dataFim) return false
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
          {/* FEAT-DOC-CHANGE-2026-07-03: fila de trocas CPF⇄CNPJ p/ aprovação. */}
          <button className="szv2-btn szv2-btn-secondary" onClick={() => navigate('/document-changes')}>
            Trocas CPF/CNPJ
          </button>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger">{err}</div>}

      {loading && filtered.length === 0 ? (
        <TableSkeleton rows={6} cols={7} />
      ) : !loading && filtered.length === 0 ? (
        <EmptyState
          icon="👤"
          title={items.length === 0 ? 'Nenhum usuário cadastrado ainda.' : 'Nenhum usuário encontrado com esses filtros.'}
          description={items.length === 0
            ? 'Usuários do portal são criados ao aprovar uma solicitação de onboarding.'
            : 'Ajuste o filtro de busca ou remova os filtros aplicados.'}
          // O serviço admin não expõe criação direta de usuário; o fluxo oficial é
          // aprovar uma solicitação em "Solicitações de onboarding" (rota existente).
          action={items.length === 0 ? {
            label: 'Ir para Solicitações de onboarding',
            onClick: () => navigate('/onboarding-requests'),
          } : undefined}
        />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Email</th>
              <th>Nome</th>
              <th>Role</th>
              <th>Ativo</th>
              <th>Criado</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {filtered.map(u => (
              <tr key={u.id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>#{u.id}</td>
                <td style={{ fontWeight: 500 }}>{u.email}</td>
                <td style={{ color: 'var(--szv2-text-soft)' }}>{u.nome}</td>
                <td>{roleBadge(u.role)}</td>
                <td>
                  {u.ativo
                    ? <span className="sz-badge szv2-badge-success">Ativo</span>
                    : <span className="sz-badge szv2-badge-neutral">Inativo</span>}
                </td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  {brDate(u.created_at)}
                </td>
                <td>
                  {u.role !== 'produtor' && (
                    <button
                      className="szv2-btn szv2-btn-secondary"
                      disabled={promoting === u.id}
                      onClick={() => promoteToProducer(u)}
                    >
                      {promoting === u.id ? 'Promovendo…' : 'Promover a produtor'}
                    </button>
                  )}
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
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftIni}
            max={draftFim || undefined}
            onChange={v => setDraftIni(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftFim}
            min={draftIni || undefined}
            onChange={v => setDraftFim(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Role">
          <FalkSelect
            value={draftRole}
            onChange={v => setDraftRole(v)}
            aria-label="Role"
            options={[
              { value: '', label: 'Todas roles' },
              ...ROLES.map(r => ({ value: r, label: r })),
            ]}
          />
        </FilterField>
        <FilterField label="Ativo">
          <FalkSelect
            value={draftAtivo}
            onChange={v => setDraftAtivo(v)}
            aria-label="Ativo"
            options={[
              { value: '', label: 'Todos' },
              { value: 'sim', label: 'Sim' },
              { value: 'nao', label: 'Não' },
            ]}
          />
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
