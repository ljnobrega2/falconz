// Página "Gestão de Links de Checkout".
// CRUD para senderzz_affiliate_links — admin cria/edita/exclui links
// e visualiza KPIs (cliques, conversões, receita) por link.
//
// Convenção: o filtro/form usa wp_user_id do afiliado (mesma chave que o
// endpoint /affiliates retorna). O backend resolve o vínculo internamente.

import { useEffect, useState } from 'react'
import { api } from '../api'
import CopyButton from '../components/CopyButton'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// ─── Tipos ───────────────────────────────────────────────────────────────────

type CheckoutLink = {
  id: number
  affiliate_id: number       // senderzz_affiliates.id (vínculo)
  afiliado_nome: string
  afiliado_email: string
  produtor_nome: string
  link_token: string
  link_url: string
  produto_id: number
  produto_nome: string
  active: boolean
  clicks: number
  conversoes: number
  receita_gerada: number
  created_at: string
}

// /affiliates retorna user_id = wp_user_id.
type AffiliateOpt = {
  user_id: number
  nome: string
  email: string
}

// /products retorna { id (sz_products.id), wp_post_id, nome, ... }.
// Para senderzz_affiliate_links.produto_id usa-se wp_post_id (quando existe).
type ProductOpt = {
  id: number
  wp_post_id: number | null
  nome: string
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

const fmtBRL = (v: number) =>
  'R$ ' + (v ?? 0).toLocaleString('pt-BR', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })

const ACTIVE_OPTS = [
  { key: '',  label: 'Todos' },
  { key: '1', label: 'Ativo' },
  { key: '0', label: 'Inativo' },
]

// Form de criação — admin define o vínculo (afiliado + produto) e o active.
type FormState = {
  affiliate_id: number   // wp_user_id do afiliado
  produto_id: number     // wp_post_id
  active: boolean
}

function emptyForm(): FormState {
  return { affiliate_id: 0, produto_id: 0, active: true }
}

// ─── Componente ──────────────────────────────────────────────────────────────

export default function CheckoutLinks() {
  // Dados/UI principal
  const [items, setItems] = useState<CheckoutLink[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')

  // Filtros aplicados (disparam fetch).
  const [q, setQ] = useState('')
  const [active, setActive] = useState('')
  const [produtoID, setProdutoID] = useState('')
  const [afiliadoID, setAfiliadoID] = useState('')

  // Painel de filtros — drafts (aplicados ao confirmar).
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftQ, setDraftQ] = useState('')
  const [draftActive, setDraftActive] = useState('')
  const [draftProdutoID, setDraftProdutoID] = useState('')
  const [draftAfiliadoID, setDraftAfiliadoID] = useState('')

  // Form modal
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState<FormState>(emptyForm())
  const [saving, setSaving] = useState(false)

  // Selects do form/filtro — carregados sob demanda para não pesar o load inicial.
  const [affiliates, setAffiliates] = useState<AffiliateOpt[]>([])
  const [products, setProducts] = useState<ProductOpt[]>([])
  const [selectsLoaded, setSelectsLoaded] = useState(false)

  // ─── Fetch list ────────────────────────────────────────────────────────────

  function buildQs() {
    const p = new URLSearchParams()
    if (q.trim()) p.set('q', q.trim())
    if (active) p.set('active', active)
    if (produtoID) p.set('produto_id', produtoID)
    if (afiliadoID) p.set('affiliate_id', afiliadoID)
    p.set('limit', '100')
    return p.toString()
  }

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<{ items: CheckoutLink[]; total: number }>(
        `/checkout-links?${buildQs()}`
      )
      setItems(r.items ?? [])
      setTotal(r.total ?? 0)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load() }, [q, active, produtoID, afiliadoID])

  // ─── Selects (afiliados + produtos) ────────────────────────────────────────

  async function loadSelects() {
    if (selectsLoaded) return
    try {
      // /affiliates retorna role='affiliate'; user_id é o wp_user_id.
      const ra = await api<{ items: AffiliateOpt[] }>(`/affiliates?limit=200`)
      setAffiliates(ra.items ?? [])
    } catch { /* mantém vazio */ }
    try {
      const rp = await api<{ items: ProductOpt[] }>(`/products?limit=200`)
      setProducts(rp.items ?? [])
    } catch { /* mantém vazio */ }
    setSelectsLoaded(true)
  }

  // ─── Painel de filtros ─────────────────────────────────────────────────────

  function openPanel() {
    setDraftQ(q)
    setDraftActive(active)
    setDraftProdutoID(produtoID)
    setDraftAfiliadoID(afiliadoID)
    loadSelects()
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ)
    setActive(draftActive)
    setProdutoID(draftProdutoID)
    setAfiliadoID(draftAfiliadoID)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setActive(''); setProdutoID(''); setAfiliadoID('')
    setDraftQ(''); setDraftActive(''); setDraftProdutoID(''); setDraftAfiliadoID('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (active) chips.push({
    key: 'active',
    label: `Status: ${active === '1' ? 'Ativo' : 'Inativo'}`,
    onRemove: () => setActive(''),
  })
  if (produtoID) {
    const p = products.find(x => String(x.wp_post_id ?? x.id) === produtoID)
    chips.push({
      key: 'produto',
      label: `Produto: ${p?.nome ?? `#${produtoID}`}`,
      onRemove: () => setProdutoID(''),
    })
  }
  if (afiliadoID) {
    const a = affiliates.find(x => String(x.user_id) === afiliadoID)
    chips.push({
      key: 'afiliado',
      label: `Afiliado: ${a?.nome || a?.email || `#${afiliadoID}`}`,
      onRemove: () => setAfiliadoID(''),
    })
  }
  const activeCount = chips.length

  // ─── Form (create/edit) ────────────────────────────────────────────────────

  async function openCreate() {
    await loadSelects()
    setForm(emptyForm())
    setShowForm(true)
  }

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setErr('')
    try {
      await api('/checkout-links', {
        method: 'POST',
        body: JSON.stringify({
          affiliate_id: Number(form.affiliate_id),
          produto_id: Number(form.produto_id),
          active: form.active,
        }),
      })
      setShowForm(false)
      setForm(emptyForm())
      load()
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setSaving(false)
    }
  }

  async function toggleActive(l: CheckoutLink) {
    try {
      await api(`/checkout-links/${l.id}`, {
        method: 'PUT',
        body: JSON.stringify({ active: !l.active }),
      })
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function del(l: CheckoutLink) {
    if (!window.confirm(
      `Excluir o link de "${l.afiliado_nome || l.afiliado_email}" para "${l.produto_nome || '—'}"? Esta ação não pode ser desfeita.`
    )) return
    try {
      await api(`/checkout-links/${l.id}`, { method: 'DELETE' })
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  // ─── KPIs (da página corrente) ─────────────────────────────────────────────

  const kpiAtivos = items.filter(l => l.active).length
  const kpiClicks = items.reduce((s, l) => s + (l.clicks || 0), 0)
  const kpiReceita = items.reduce((s, l) => s + (l.receita_gerada || 0), 0)

  // ─── Render ────────────────────────────────────────────────────────────────

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Gestão de Links de Checkout</h1>
          <p>{items.length} de {total} link(s) — gere links rastreados para afiliados</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          <button className="szv2-btn szv2-btn-brand" onClick={openCreate}>
            + Novo Link
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* KPIs (página atual) */}
      <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 12 }}>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Links ativos</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{kpiAtivos}</span>
          <span className="szv2-kpi-meta">de {items.length} na página</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Total de cliques</span>
          <span className="szv2-kpi-value">{kpiClicks.toLocaleString('pt-BR')}</span>
          <span className="szv2-kpi-meta">soma da página</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Receita gerada</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-success)' }}>{fmtBRL(kpiReceita)}</span>
          <span className="szv2-kpi-meta">via pedidos atribuídos</span>
        </div></div>
      </div>

      {/* Form modal inline */}
      {showForm && (
        <div className="szv2-card" style={{ marginBottom: 24 }}>
          <div className="szv2-card-head">
            <div><h2>Novo Link de Checkout</h2></div>
            <button className="szv2-modal-x" onClick={() => setShowForm(false)}>✕</button>
          </div>
          <form onSubmit={save}>
            <div className="sz-form-grid sz-form-grid-2" style={{ marginBottom: 16 }}>
              <div className="szv2-field">
                <label className="szv2-label">Afiliado *</label>
                <select
                  className="szv2-input"
                  required
                  value={form.affiliate_id || ''}
                  onChange={e => setForm({ ...form, affiliate_id: Number(e.target.value) })}
                >
                  <option value="">Selecione um afiliado…</option>
                  {affiliates.map(a => (
                    <option key={a.user_id} value={a.user_id}>
                      {a.nome ? `${a.nome} — ${a.email}` : a.email}
                    </option>
                  ))}
                </select>
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Produto *</label>
                <select
                  className="szv2-input"
                  required
                  value={form.produto_id || ''}
                  onChange={e => setForm({ ...form, produto_id: Number(e.target.value) })}
                >
                  <option value="">Selecione um produto…</option>
                  {products.map(p => (
                    <option key={p.id} value={p.wp_post_id ?? p.id}>{p.nome}</option>
                  ))}
                </select>
              </div>
              <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
                <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={form.active}
                    onChange={e => setForm({ ...form, active: e.target.checked })}
                  />
                  <span>Link ativo (libera redirect e contabiliza cliques)</span>
                </label>
              </div>
            </div>
            <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 12 }}>
              O vínculo afiliado↔produto precisa estar cadastrado em senderzz_affiliates. O token é gerado automaticamente (32 bytes hex).
            </p>
            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={saving}>
                {saving ? 'Criando…' : 'Criar Link'}
              </button>
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setShowForm(false)}>
                Cancelar
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Tabela / Empty state */}
      {loading && <p style={{ color: 'var(--szv2-text-muted)', marginBottom: 8 }}>Carregando…</p>}

      {!loading && items.length === 0 && (
        <div className="szv2-card" style={{ textAlign: 'center', padding: 48, color: 'var(--szv2-text-muted)' }}>
          <p style={{ fontSize: 32, margin: '0 0 8px' }}>🔗</p>
          <p style={{ margin: 0 }}>Nenhum link criado ainda. Crie um link de checkout para um afiliado.</p>
        </div>
      )}

      {items.length > 0 && (
        <div className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
          <table className="szv2-table" style={{ width: '100%' }}>
            <thead>
              <tr>
                <th>ID</th>
                <th>Afiliado</th>
                <th>Produto</th>
                <th>Token</th>
                <th>URL Completa</th>
                <th className="szv2-td-num">Cliques</th>
                <th className="szv2-td-num">Conversões</th>
                <th className="szv2-td-num">Receita</th>
                <th>Status</th>
                <th>Criado em</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map(l => (
                <tr key={l.id}>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>#{l.id}</td>
                  <td style={{ maxWidth: 200 }}>
                    <div style={{ fontWeight: 600 }}>{l.afiliado_nome || '—'}</div>
                    <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{l.afiliado_email}</div>
                  </td>
                  <td style={{ maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {l.produto_nome || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                  </td>
                  <td>
                    <CopyButton
                      text={l.link_token}
                      variant="inline"
                      style={{ fontSize: 11, maxWidth: 100, overflow: 'hidden', textOverflow: 'ellipsis', display: 'inline-block', whiteSpace: 'nowrap' }}
                    >
                      {l.link_token.slice(0, 12)}…
                    </CopyButton>
                  </td>
                  <td style={{ maxWidth: 280 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                      <span style={{ fontSize: 11, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>
                        {l.link_url}
                      </span>
                      <CopyButton text={l.link_url} variant="icon" />
                    </div>
                  </td>
                  <td className="szv2-td-num">{(l.clicks || 0).toLocaleString('pt-BR')}</td>
                  <td className="szv2-td-num" style={{ color: 'var(--szv2-brand)', fontWeight: 600 }}>
                    {(l.conversoes || 0).toLocaleString('pt-BR')}
                  </td>
                  <td className="szv2-td-num" style={{ color: 'var(--szv2-success)', fontWeight: 600 }}>
                    {l.receita_gerada > 0 ? fmtBRL(l.receita_gerada) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                  </td>
                  <td>
                    <span className={`sz-badge ${l.active ? 'szv2-badge-success' : 'szv2-badge-neutral'}`}>
                      {l.active ? 'Ativo' : 'Inativo'}
                    </span>
                  </td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    {l.created_at ? l.created_at.substring(0, 10) : '—'}
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'nowrap' }}>
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                        onClick={() => toggleActive(l)}
                        title={l.active ? 'Desativar link' : 'Ativar link'}
                      >
                        {l.active ? 'Pausar' : 'Ativar'}
                      </button>
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-danger"
                        onClick={() => del(l)}
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

      {/* Painel de filtros */}
      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros — Links de Checkout"
      >
        <FilterField label="Busca (token / afiliado / produto)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: joao@…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftActive}
            onChange={e => setDraftActive(e.target.value)}
          >
            {ACTIVE_OPTS.map(o => (
              <option key={o.key} value={o.key}>{o.label}</option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Produto">
          <select
            style={filterInputStyle}
            value={draftProdutoID}
            onChange={e => setDraftProdutoID(e.target.value)}
          >
            <option value="">Todos os produtos</option>
            {products.map(p => (
              <option key={p.id} value={p.wp_post_id ?? p.id}>{p.nome}</option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Afiliado">
          <select
            style={filterInputStyle}
            value={draftAfiliadoID}
            onChange={e => setDraftAfiliadoID(e.target.value)}
          >
            <option value="">Todos os afiliados</option>
            {affiliates.map(a => (
              <option key={a.user_id} value={a.user_id}>
                {a.nome || a.email}
              </option>
            ))}
          </select>
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
