import { useEffect, useState } from 'react'
import { api } from '../api'

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

export default function Products() {
  const [items, setItems] = useState<Product[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

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

  async function load() {
    setLoading(true)
    try {
      const p = new URLSearchParams()
      if (q.trim()) p.set('q', q.trim())
      if (statusFilter) p.set('status', statusFilter)
      p.set('limit', '100')
      const r = await api<{ items: Product[]; total: number }>(`/products?${p.toString()}`)
      setItems(r.items ?? [])
      setTotal(r.total ?? 0)
    } catch (e: any) { setErr(e.message) }
    finally { setLoading(false) }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [statusFilter])

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
    } catch (e: any) { setErr(e.message) }
    finally { setSaving(false) }
  }

  async function del(p: Product) {
    if (!window.confirm(`Excluir produto "${p.nome}"? Esta ação não pode ser desfeita.`)) return
    try {
      await api(`/products/${p.id}`, { method: 'DELETE' })
      load()
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
    return true
  })

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Produtos</h1>
          <p>{filtered.length} de {total} produto(s)</p>
        </div>
        <button className="szv2-btn szv2-btn-brand" onClick={openCreate}>
          + Novo Produto
        </button>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* Filtros inline */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <div className="szv2-field">
            <label className="szv2-label">Busca (nome / SKU)</label>
            <input
              className="szv2-input"
              placeholder="ex: Curso, Recarga…"
              value={q}
              onChange={e => setQ(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && load()}
            />
          </div>
          <div className="szv2-field">
            <label className="szv2-label">Status</label>
            <select className="szv2-input" value={statusFilter} onChange={e => setStatusFilter(e.target.value)}>
              <option value="">Todos</option>
              {STATUSES.map(s => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
            </select>
          </div>
          <button className="szv2-btn szv2-btn-brand" onClick={load}>Buscar</button>
          {(q || statusFilter) && (
            <button className="szv2-btn szv2-btn-secondary" onClick={() => { setQ(''); setStatusFilter('') }}>
              Limpar
            </button>
          )}
        </div>
      </div>

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
          <p style={{ margin: 0 }}>Nenhum produto cadastrado ainda.</p>
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
    </div>
  )
}
