import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterDrawer from '../components/FilterDrawer'
import FilterButton from '../components/FilterButton'

type P = {
  id: number
  wc_order_id?: number | null
  sz_order_id?: number | null
  motoboy_id?: number | null
  status: string
  valor: number
  taxa_motoboy: number
  taxa_frustrado: number
  dest_nome: string
  dest_cep: string
  dest_cidade: string
  dest_uf: string
  cliente_nome: string
  produto: string
  afiliado_nome: string
  oferta_link: string
  comissao: number
  created_at: string
}

const STATUS_CLS: Record<string, string> = {
  pendente:  's-pendente',
  agendado:  'szv2-badge-info',
  embalado:  'szv2-badge-info',
  em_rota:   'szv2-badge-brand',
  entregue:  'szv2-badge-success',
  frustrado: 'szv2-badge-danger',
  cancelado: 'szv2-badge-neutral',
}

const fmt = (v: number) => 'R$ ' + v.toLocaleString('pt-BR', { minimumFractionDigits: 2 })

function defaultRange() {
  const today = new Date()
  const past = new Date(today)
  past.setDate(past.getDate() - 30)
  const fmtDate = (d: Date) => d.toISOString().slice(0, 10)
  return { ini: fmtDate(past), fim: fmtDate(today) }
}

export default function Orders() {
  const init = useMemo(defaultRange, [])

  // Dados
  const [items, setItems] = useState<P[]>([])
  const [err, setErr]     = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [busyId, setBusyId] = useState<number | null>(null)

  // Estado dos filtros (rascunho dentro do drawer)
  const [draftStatus,  setDraftStatus]  = useState('')
  const [draftDataIni, setDraftDataIni] = useState(init.ini)
  const [draftDataFim, setDraftDataFim] = useState(init.fim)
  const [draftCidade,  setDraftCidade]  = useState('')
  const [draftSearch,  setDraftSearch]  = useState('')
  const [draftStopped, setDraftStopped] = useState(false)

  // Filtros aplicados (disparam fetch)
  const [status,  setStatus]  = useState('')
  const [dataIni, setDataIni] = useState(init.ini)
  const [dataFim, setDataFim] = useState(init.fim)
  const [cidade,  setCidade]  = useState('')
  const [search,  setSearch]  = useState('')
  const [stopped, setStopped] = useState(false)

  // UI
  const [filterOpen,    setFilterOpen]    = useState(false)
  const [selectedOrder, setSelectedOrder] = useState<P | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  function buildQS() {
    const p = new URLSearchParams()
    if (status) p.set('status', status)
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    if (cidade) p.set('cidade', cidade)
    if (search) p.set('s', search)
    if (stopped) p.set('stopped', '1')
    p.set('limit', '100')
    return p.toString()
  }

  function load() {
    setErr('')
    api<{ items: P[] }>(`/orders/motoboy?${buildQS()}`)
      .then(r => setItems(r.items || []))
      .catch(e => setErr(e.message))
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load() }, [status, dataIni, dataFim, cidade, search, stopped])

  // Sincroniza rascunho ao abrir o drawer
  function openFilterDrawer() {
    setDraftStatus(status)
    setDraftDataIni(dataIni)
    setDraftDataFim(dataFim)
    setDraftCidade(cidade)
    setDraftSearch(search)
    setDraftStopped(stopped)
    setFilterOpen(true)
  }

  function applyFilters() {
    setStatus(draftStatus)
    setDataIni(draftDataIni)
    setDataFim(draftDataFim)
    setCidade(draftCidade)
    setSearch(draftSearch)
    setStopped(draftStopped)
    setFilterOpen(false)
  }

  function clearFilters() {
    setDraftStatus('')
    setDraftDataIni(init.ini)
    setDraftDataFim(init.fim)
    setDraftCidade('')
    setDraftSearch('')
    setDraftStopped(false)
    // Aplica imediatamente
    setStatus('')
    setDataIni(init.ini)
    setDataFim(init.fim)
    setCidade('')
    setSearch('')
    setStopped(false)
    setFilterOpen(false)
  }

  // Conta filtros ativos (exclui range padrão)
  const activeFilterCount = [
    status !== '',
    cidade !== '',
    search !== '',
    stopped,
    dataIni !== init.ini,
    dataFim !== init.fim,
  ].filter(Boolean).length

  async function auditFix(p: P) {
    if (!window.confirm(`Auditar e corrigir pedido motoboy #${p.id} (#${p.wc_order_id ?? '—'})?\n\nCorrege comissão de afiliado e carteira COD do produtor caso haja divergência.`)) return
    setBusyId(p.id)
    try {
      await api(`/orders/motoboy/${p.id}/audit-fix`, { method: 'POST' })
      showToast('ok', `Pedido #${p.id} auditado com sucesso.`)
      load()
    } catch (err: unknown) {
      showToast('err', (err as Error).message || 'Falha ao auditar')
    } finally {
      setBusyId(null)
    }
  }

  function handleAuditFix(e: React.MouseEvent, p: P) {
    e.stopPropagation()
    auditFix(p)
  }

  // ── Label helpers ──────────────────────────────────────────────────
  const fieldStyle: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 6 }
  const labelStyle: React.CSSProperties = { fontSize: 12, fontWeight: 600, color: 'var(--szv2-text-soft)' }
  const inputStyle: React.CSSProperties = {
    height: 38,
    padding: '0 12px',
    background: 'var(--szv2-surface-alt)',
    border: '1px solid var(--szv2-border)',
    borderRadius: 10,
    color: 'var(--szv2-text)',
    font: 'inherit',
    fontSize: 13,
  }

  return (
    <div>
      {/* ── Section head ──────────────────────────────────────── */}
      <div className="szv2-section-head" style={{ flexWrap: 'wrap', gap: 8 }}>
        <div>
          <h1>Pedidos Motoboy</h1>
          <p>
            {items.length} pedido(s) encontrado(s)
            {stopped ? ' — parados 24h+' : ''}
            {activeFilterCount > 0 ? ` · ${activeFilterCount} filtro(s) ativo(s)` : ''}
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <FilterButton
            active={activeFilterCount > 0}
            count={activeFilterCount}
            onClick={openFilterDrawer}
          />
        </div>
      </div>

      {/* ── Alertas ──────────────────────────────────────────── */}
      {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 12 }}
        >
          {toast.msg}
        </div>
      )}

      {/* ── Tabela ───────────────────────────────────────────── */}
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>Pedido</th>
              <th>Cliente</th>
              <th>Destino</th>
              <th>Status</th>
              <th>Produto</th>
              <th>Afiliado</th>
              <th>Oferta</th>
              <th className="szv2-td-num">Valor</th>
              <th className="szv2-td-num">Taxa motoboy</th>
              <th className="szv2-td-num">Comissão</th>
              <th>Criado</th>
              <th>Ações</th>
            </tr>
          </thead>
          <tbody>
            {items.map(p => (
              <tr
                key={p.id}
                onClick={() => setSelectedOrder(p)}
                style={{ cursor: 'pointer' }}
                title="Clique para ver detalhes"
              >
                {/* Pedido */}
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  <div>#{p.id}</div>
                  {p.wc_order_id && (
                    <div style={{ color: 'var(--szv2-text-faint)', fontSize: 11 }}>
                      #{p.wc_order_id}
                    </div>
                  )}
                </td>
                {/* Cliente */}
                <td style={{ fontWeight: 500 }}>
                  {p.cliente_nome || p.dest_nome || '—'}
                </td>
                {/* Destino */}
                <td style={{ fontSize: 13 }}>
                  <div>{p.dest_cidade || '—'}{p.dest_uf ? `/${p.dest_uf}` : ''}</div>
                  {p.dest_cep && (
                    <div style={{ color: 'var(--szv2-text-faint)', fontSize: 11, fontFamily: 'var(--szv2-font-mono)' }}>
                      CEP {p.dest_cep}
                    </div>
                  )}
                </td>
                {/* Status */}
                <td>
                  <span className={`sz-badge ${STATUS_CLS[p.status] || 'szv2-badge-neutral'}`}>
                    {p.status}
                  </span>
                </td>
                {/* Produto */}
                <td style={{ fontSize: 13 }}>{p.produto || '—'}</td>
                {/* Afiliado */}
                <td style={{ fontSize: 13 }}>{p.afiliado_nome || '—'}</td>
                {/* Oferta */}
                <td style={{ fontSize: 12 }}>
                  {p.oferta_link
                    ? <span style={{ fontFamily: 'var(--szv2-font-mono)', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', padding: '2px 6px', borderRadius: 4 }} title={p.oferta_link}>{p.oferta_link}</span>
                    : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                {/* Valor */}
                <td className="szv2-td-num" style={{ fontWeight: 600 }}>
                  {p.valor > 0 ? fmt(p.valor) : '—'}
                </td>
                {/* Taxa motoboy */}
                <td className="szv2-td-num" style={{ fontSize: 13 }}>
                  {p.status === 'frustrado' && p.taxa_frustrado > 0
                    ? <span title="Taxa frustrado" style={{ color: 'var(--szv2-warning)' }}>{fmt(p.taxa_frustrado)}</span>
                    : p.taxa_motoboy > 0 ? fmt(p.taxa_motoboy) : '—'}
                </td>
                {/* Comissão */}
                <td className="szv2-td-num" style={{ fontWeight: 600 }}>
                  {p.comissao > 0 ? fmt(p.comissao) : '—'}
                </td>
                {/* Criado */}
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  {p.created_at ? p.created_at.slice(0, 16).replace('T', ' ') : '—'}
                </td>
                {/* Ações */}
                <td onClick={e => e.stopPropagation()}>
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'nowrap' }}>
                    <button
                      type="button"
                      className="szv2-btn-secondary"
                      style={{ fontSize: 12, padding: '3px 8px' }}
                      disabled={busyId === p.id}
                      onClick={e => handleAuditFix(e, p)}
                    >
                      {busyId === p.id ? '…' : 'Auditar'}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={12}>
                  <div className="szv2-empty">
                    <h3>Sem pedidos</h3>
                    <p>Tente outro filtro ou remova os filtros aplicados.</p>
                  </div>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* ── Drawer de Filtros ────────────────────────────────── */}
      <FilterDrawer
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        {/* De */}
        <div style={fieldStyle}>
          <label style={labelStyle}>De</label>
          <input
            type="date"
            style={inputStyle}
            value={draftDataIni}
            max={draftDataFim}
            onChange={e => setDraftDataIni(e.target.value)}
          />
        </div>

        {/* Até */}
        <div style={fieldStyle}>
          <label style={labelStyle}>Até</label>
          <input
            type="date"
            style={inputStyle}
            value={draftDataFim}
            min={draftDataIni}
            onChange={e => setDraftDataFim(e.target.value)}
          />
        </div>

        {/* Status */}
        <div style={fieldStyle}>
          <label style={labelStyle}>Status</label>
          <select
            style={inputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value)}
          >
            <option value="">Todos status</option>
            {Object.keys(STATUS_CLS).map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </div>

        {/* Cidade */}
        <div style={fieldStyle}>
          <label style={labelStyle}>Cidade</label>
          <input
            type="text"
            style={inputStyle}
            placeholder="Filtrar por cidade"
            value={draftCidade}
            onChange={e => setDraftCidade(e.target.value)}
          />
        </div>

        {/* Busca */}
        <div style={fieldStyle}>
          <label style={labelStyle}>Busca</label>
          <input
            type="search"
            style={inputStyle}
            placeholder="Pedido / nome"
            value={draftSearch}
            onChange={e => setDraftSearch(e.target.value)}
          />
        </div>

        {/* Parados 24h+ */}
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, cursor: 'pointer' }}>
          <input
            type="checkbox"
            checked={draftStopped}
            onChange={e => setDraftStopped(e.target.checked)}
          />
          <span>Parados 24h+</span>
        </label>
      </FilterDrawer>

      {/* ── Drawer de Detalhe do Pedido ──────────────────────── */}
      <FilterDrawer
        open={selectedOrder !== null}
        onClose={() => setSelectedOrder(null)}
        onApply={() => { if (selectedOrder) { auditFix(selectedOrder) } }}
        onClear={() => setSelectedOrder(null)}
        applyLabel="Auditar pedido"
        clearLabel="Fechar"
        title={selectedOrder ? `#${selectedOrder.id} — ${selectedOrder.cliente_nome || selectedOrder.dest_nome || 'Pedido'}` : 'Detalhes'}
      >
        {selectedOrder && (
          <>
            {/* Status */}
            <div>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Status</span>
              <div style={{ marginTop: 6 }}>
                <span className={`sz-badge ${STATUS_CLS[selectedOrder.status] || 'szv2-badge-neutral'}`}>
                  {selectedOrder.status}
                </span>
              </div>
            </div>

            {/* Endereço */}
            <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Destino</span>
              <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.6 }}>
                <div style={{ fontWeight: 600 }}>{selectedOrder.dest_nome || '—'}</div>
                {selectedOrder.dest_cidade && (
                  <div>{selectedOrder.dest_cidade}{selectedOrder.dest_uf ? `/${selectedOrder.dest_uf}` : ''}</div>
                )}
                {selectedOrder.dest_cep && (
                  <div style={{ color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                    CEP {selectedOrder.dest_cep}
                  </div>
                )}
              </div>
            </div>

            {/* Valores */}
            <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Financeiro</span>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Valor do pedido</span>
                  <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)' }}>
                    {selectedOrder.valor > 0 ? fmt(selectedOrder.valor) : '—'}
                  </span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>
                    {selectedOrder.status === 'frustrado' ? 'Taxa frustrado' : 'Taxa motoboy'}
                  </span>
                  <span style={{ fontFamily: 'var(--szv2-font-mono)', color: selectedOrder.status === 'frustrado' ? 'var(--szv2-warning)' : undefined }}>
                    {selectedOrder.status === 'frustrado' && selectedOrder.taxa_frustrado > 0
                      ? fmt(selectedOrder.taxa_frustrado)
                      : selectedOrder.taxa_motoboy > 0 ? fmt(selectedOrder.taxa_motoboy) : '—'}
                  </span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Comissão afiliado</span>
                  <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)' }}>
                    {selectedOrder.comissao > 0 ? fmt(selectedOrder.comissao) : '—'}
                  </span>
                </div>
              </div>
            </div>

            {/* Produto + Afiliado */}
            <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Produto / Afiliado</span>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Produto</span>
                  <span style={{ fontWeight: 500 }}>{selectedOrder.produto || '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Afiliado</span>
                  <span>{selectedOrder.afiliado_nome || '—'}</span>
                </div>
                {selectedOrder.oferta_link && (
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span style={{ color: 'var(--szv2-text-muted)' }}>Oferta</span>
                    <span style={{ fontFamily: 'var(--szv2-font-mono)', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', padding: '2px 8px', borderRadius: 4, fontSize: 11 }}>
                      {selectedOrder.oferta_link}
                    </span>
                  </div>
                )}
              </div>
            </div>

            {/* IDs + Data */}
            <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Identificação</span>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 12, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)' }}>
                <div>ID Motoboy: #{selectedOrder.id}</div>
                {selectedOrder.wc_order_id && <div>ID WC: #{selectedOrder.wc_order_id}</div>}
                {selectedOrder.sz_order_id && <div>ID SZ: #{selectedOrder.sz_order_id}</div>}
                {selectedOrder.motoboy_id && <div>Motoboy: #{selectedOrder.motoboy_id}</div>}
                {selectedOrder.created_at && (
                  <div>Criado: {selectedOrder.created_at.slice(0, 16).replace('T', ' ')}</div>
                )}
              </div>
            </div>
          </>
        )}
      </FilterDrawer>
    </div>
  )
}
