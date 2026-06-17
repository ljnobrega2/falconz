import { useEffect, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

type Product = {
  id: number
  wp_post_id: number | null
  produtor_id: number
  produtor_nome: string
  nome: string
  sku: string | null
  preco: number
  descricao: string | null
  categoria: string | null
  status: string
  created_at: string
  afiliados_count: number
}

type Producer = { id: number; nome: string; email: string }

const STATUSES = ['active', 'inactive', 'draft', 'archived']

const STATUS_LABEL: Record<string, string> = {
  active:   'Ativo',
  inactive: 'Inativo',
  draft:    'Rascunho',
  archived: 'Arquivado',
}

const STATUS_CLASS: Record<string, string> = {
  active:   'szv2-badge-success',
  inactive: 'szv2-badge-neutral',
  draft:    'szv2-badge-warning',
  archived: 'szv2-badge-neutral',
}

function emptyForm(): Omit<Product, 'id' | 'produtor_nome' | 'created_at' | 'afiliados_count' | 'wp_post_id'> & { id: number } {
  return {
    id: 0,
    produtor_id: 0,
    nome: '',
    sku: null,
    preco: 0,
    descricao: null,
    categoria: null,
    status: 'active',
  }
}

type ProductsStats = {
  active_count: number
  total_affiliates: number
  revenue_30d: number
}

function fmtBRL(n: number): string {
  return n.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' })
}

export default function Products() {
  const [items, setItems] = useState<Product[]>([])
  const [total, setTotal] = useState(0)
  const [stats, setStats] = useState<ProductsStats | null>(null)
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)

  // Filtros aplicados.
  const [q, setQ] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [produtorID, setProdutorID] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftProdutor, setDraftProdutor] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')

  const [filterOpen, setFilterOpen] = useState(false)

  // Form state
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState(emptyForm())
  const [saving, setSaving] = useState(false)
  const [producers, setProducers] = useState<Producer[]>([])

  async function loadProducers() {
    try {
      const r = await api<{ items: Producer[] }>('/users?limit=200')
      const prs = (r.items ?? []).filter(u => {
        const role = (u as any).role as string
        return role === 'producer' || role === 'produtor'
      })
      setProducers(prs)
    } catch { /* ignora — select fica vazio */ }
  }

  async function loadStats() {
    try {
      const s = await api<ProductsStats>('/products/stats')
      setStats(s)
    } catch { /* silencia — KPIs ficam zerados */ }
  }

  async function load() {
    setLoading(true)
    try {
      const p = new URLSearchParams()
      if (q.trim()) p.set('q', q.trim())
      if (statusFilter) p.set('status', statusFilter)
      if (produtorID) p.set('produtor_id', produtorID)
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      p.set('limit', '100')
      const r = await api<{ items: Product[]; total: number }>(`/products?${p.toString()}`)
      setItems(r.items ?? [])
      setTotal(r.total ?? 0)
    } catch (e: any) { setErr(e.message) }
    finally { setLoading(false) }
  }

  // Sincroniza produtos a partir do histórico de pedidos (idempotente no backend).
  async function syncFromOrders() {
    setSyncing(true)
    setErr('')
    try {
      const r = await api<{ ok: boolean; synced: number }>('/products/sync-from-orders', { method: 'POST' })
      if (r.synced === 0) {
        setErr('Nenhum produto novo foi encontrado no histórico de pedidos.')
      }
      await load()
      await loadStats()
    } catch (e: any) { setErr(e.message) }
    finally { setSyncing(false) }
  }

  useEffect(() => { load(); loadStats() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [q, statusFilter, produtorID, dataIni, dataFim])

  // Lista de produtores para o filtro do painel.
  useEffect(() => { loadProducers() }, [])

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setErr('')
    try {
      const payload = {
        wp_post_id:  null,
        produtor_id: form.produtor_id,
        nome:        form.nome,
        sku:         form.sku || null,
        preco:       Number(form.preco),
        descricao:   form.descricao || null,
        categoria:   form.categoria || null,
        status:      form.status,
      }
      if (form.id) {
        await api(`/products/${form.id}`, { method: 'PUT', body: JSON.stringify(payload) })
      } else {
        await api('/products', { method: 'POST', body: JSON.stringify(payload) })
      }
      setShowForm(false)
      setForm(emptyForm())
      load()
      loadStats()
    } catch (e: any) { setErr(e.message) }
    finally { setSaving(false) }
  }

  async function del(p: Product) {
    if (!window.confirm(`Excluir produto "${p.nome}"? Esta ação não pode ser desfeita.`)) return
    try {
      await api(`/products/${p.id}`, { method: 'DELETE' })
      load()
      loadStats()
    } catch (e: any) { setErr(e.message) }
  }

  function openCreate() {
    setForm(emptyForm())
    loadProducers()
    setShowForm(true)
  }

  function openEdit(p: Product) {
    setForm({
      id:         p.id,
      produtor_id: p.produtor_id,
      nome:       p.nome,
      sku:        p.sku,
      preco:      p.preco,
      descricao:  p.descricao,
      categoria:  p.categoria,
      status:     p.status,
    })
    loadProducers()
    setShowForm(true)
  }

  const qNorm = q.trim().toLowerCase()
  const filtered = items.filter(p => {
    if (qNorm && !`${p.nome} ${p.sku ?? ''} ${p.produtor_nome}`.toLowerCase().includes(qNorm)) return false
    if (statusFilter && p.status !== statusFilter) return false
    if (produtorID && String(p.produtor_id) !== produtorID) return false
    return true
  })

  function openPanel() {
    setDraftQ(q); setDraftStatus(statusFilter); setDraftProdutor(produtorID); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setStatusFilter(draftStatus); setProdutorID(draftProdutor); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setStatusFilter(''); setProdutorID(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftStatus(''); setDraftProdutor(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (statusFilter) chips.push({ key: 'status', label: `Status: ${STATUS_LABEL[statusFilter] ?? statusFilter}`, onRemove: () => setStatusFilter('') })
  if (produtorID) {
    const p = producers.find(pp => String(pp.id) === produtorID)
    chips.push({ key: 'prod', label: `Produtor: ${p?.nome || p?.email || `#${produtorID}`}`, onRemove: () => setProdutorID('') })
  }
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Produtos</h1>
          <p>{filtered.length} de {total} produto(s)</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          {items.length === 0 && (
            <button
              className="szv2-btn szv2-btn-secondary"
              onClick={syncFromOrders}
              disabled={syncing}
              title="Importa produtos do histórico de pedidos"
            >
              {syncing ? 'Sincronizando…' : '🔄 Sincronizar do histórico de pedidos'}
            </button>
          )}
          <button className="szv2-btn szv2-btn-brand" onClick={openCreate}>
            + Novo Produto
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* KPIs */}
      {stats && (
        <div
          className="szv2-kpi-grid"
          style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', gap: 16, marginBottom: 16 }}
        >
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Produtos ativos</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>
                {stats.active_count.toLocaleString('pt-BR')}
              </span>
              <span className="szv2-kpi-meta">com status = ativo</span>
            </div>
          </div>
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Total afiliados vinculados</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-success)' }}>
                {stats.total_affiliates.toLocaleString('pt-BR')}
              </span>
              <span className="szv2-kpi-meta">vínculos produtor ↔ afiliado</span>
            </div>
          </div>
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Receita 30d</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>
                {fmtBRL(stats.revenue_30d)}
              </span>
              <span className="szv2-kpi-meta">soma dos itens dos pedidos</span>
            </div>
          </div>
        </div>
      )}

      {/* Form modal inline */}
      {showForm && (
        <div className="szv2-card" style={{ marginBottom: 24 }}>
          <div className="szv2-card-head">
            <div><h2>{form.id ? 'Editar Produto' : 'Novo Produto'}</h2></div>
            <button className="szv2-modal-x" onClick={() => setShowForm(false)}>✕</button>
          </div>
          <form onSubmit={save}>
            <div className="sz-form-grid sz-form-grid-3" style={{ marginBottom: 16 }}>
              <div className="szv2-field" style={{ gridColumn: '1 / 3' }}>
                <label className="szv2-label">Nome *</label>
                <input
                  className="szv2-input"
                  required
                  value={form.nome}
                  onChange={e => setForm({ ...form, nome: e.target.value })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Produtor *</label>
                <select
                  className="szv2-input"
                  required
                  value={form.produtor_id || ''}
                  onChange={e => setForm({ ...form, produtor_id: Number(e.target.value) })}
                >
                  <option value="">Selecione…</option>
                  {producers.map(p => (
                    <option key={p.id} value={p.id}>{p.nome || p.email}</option>
                  ))}
                </select>
              </div>
              <div className="szv2-field">
                <label className="szv2-label">SKU</label>
                <input
                  className="szv2-input"
                  value={form.sku ?? ''}
                  onChange={e => setForm({ ...form, sku: e.target.value || null })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Preço (R$)</label>
                <input
                  className="szv2-input"
                  type="number"
                  step="0.01"
                  min="0"
                  value={form.preco}
                  onChange={e => setForm({ ...form, preco: Number(e.target.value) })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Categoria</label>
                <input
                  className="szv2-input"
                  value={form.categoria ?? ''}
                  onChange={e => setForm({ ...form, categoria: e.target.value || null })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Status</label>
                <select
                  className="szv2-input"
                  value={form.status}
                  onChange={e => setForm({ ...form, status: e.target.value })}
                >
                  {STATUSES.map(s => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
                </select>
              </div>
              <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
                <label className="szv2-label">Descrição</label>
                <textarea
                  className="szv2-input"
                  rows={3}
                  value={form.descricao ?? ''}
                  onChange={e => setForm({ ...form, descricao: e.target.value || null })}
                />
              </div>
            </div>
            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={saving}>
                {saving ? 'Salvando…' : form.id ? 'Salvar Alterações' : 'Criar Produto'}
              </button>
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setShowForm(false)}>
                Cancelar
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Tabela */}
      {loading && <p style={{ color: 'var(--szv2-text-muted)', marginBottom: 8 }}>Carregando…</p>}

      {!loading && filtered.length === 0 && (
        <div className="szv2-card" style={{ textAlign: 'center', padding: 48, color: 'var(--szv2-text-muted)' }}>
          <p style={{ fontSize: 32, margin: '0 0 8px' }}>📦</p>
          <p style={{ margin: '0 0 16px' }}>
            Nenhum produto cadastrado ainda. Importe automaticamente dos pedidos existentes ou crie manualmente.
          </p>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'center', flexWrap: 'wrap' }}>
            <button
              className="szv2-btn szv2-btn-secondary"
              onClick={syncFromOrders}
              disabled={syncing}
            >
              {syncing ? 'Sincronizando…' : '🔄 Sincronizar do histórico de pedidos'}
            </button>
            <button className="szv2-btn szv2-btn-brand" onClick={openCreate}>
              + Novo Produto
            </button>
          </div>
        </div>
      )}

      {filtered.length > 0 && (
        <div className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
          <table className="szv2-table" style={{ width: '100%' }}>
            <thead>
              <tr>
                <th>ID</th>
                <th>Produtor</th>
                <th>Nome</th>
                <th>SKU</th>
                <th>Preço</th>
                <th>Categoria</th>
                <th>Status</th>
                <th>Afiliados</th>
                <th>Data</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(p => (
                <tr key={p.id}>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>#{p.id}</td>
                  <td style={{ maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {p.produtor_nome || '—'}
                  </td>
                  <td style={{ fontWeight: 600 }}>{p.nome}</td>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>{p.sku ?? '—'}</td>
                  <td>R$ {Number(p.preco).toFixed(2)}</td>
                  <td>{p.categoria ?? '—'}</td>
                  <td>
                    <span className={`sz-badge ${STATUS_CLASS[p.status] ?? 'szv2-badge-neutral'}`}>
                      {STATUS_LABEL[p.status] ?? p.status}
                    </span>
                  </td>
                  <td style={{ textAlign: 'center' }}>{p.afiliados_count}</td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    {p.created_at ? p.created_at.substring(0, 10) : '—'}
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: 8 }}>
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                        onClick={() => openEdit(p)}
                      >
                        Editar
                      </button>
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-danger"
                        onClick={() => del(p)}
                      >
                        Excluir
                      </button>
                    </div>
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
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value)}
          >
            <option value="">Todos</option>
            {STATUSES.map(s => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
          </select>
        </FilterField>
        <FilterField label="Produtor">
          <select
            style={filterInputStyle}
            value={draftProdutor}
            onChange={e => setDraftProdutor(e.target.value)}
          >
            <option value="">Todos</option>
            {producers.map(p => (
              <option key={p.id} value={String(p.id)}>{p.nome || p.email}</option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Busca (nome / SKU)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex: Curso, Recarga…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
