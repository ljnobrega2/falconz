import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterDrawer from '../components/FilterDrawer'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import CopyButton from '../components/CopyButton'

// ── Tipos do detalhe enriquecido (GET /orders/{id}) ──────────────────────────
// Mantém em sync com OrderDetail.tsx — só os campos exibidos no drawer.
type DetailMarketing = {
  utm_source: string; utm_medium: string; utm_campaign: string
  utm_term: string; utm_content: string; referrer: string; landing_page: string
}
type DetailFiscal = {
  nfe_chave: string; nfe_numero: string; nfe_serie: string
  nfe_url: string; nfe_status: string
}
type DetailTracking = { code: string; url: string; carrier: string }
type DetailMotoboy = {
  motoboy_nome?: string
  motoboy_telefone?: string
  motoboy_placa?: string
}
type OrderDetailLite = {
  marketing?: DetailMarketing
  fiscal?: DetailFiscal
  tracking?: DetailTracking
  motoboy?: DetailMotoboy
}

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

  // Detalhe enriquecido (lazy fetch GET /orders/{sz_order_id}).
  // sz_order_id é a PK em sz_orders — NÃO confundir com selectedOrder.id (pedido motoboy).
  const [detail, setDetail]               = useState<OrderDetailLite | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  useEffect(() => {
    if (!selectedOrder) { setDetail(null); return }
    const szId = selectedOrder.sz_order_id
    if (!szId) { setDetail(null); return }
    setDetailLoading(true)
    setDetail(null)
    api<OrderDetailLite>(`/orders/${szId}`)
      .then(r => setDetail(r))
      .catch(() => setDetail(null))
      .finally(() => setDetailLoading(false))
  }, [selectedOrder])

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

  // Constrói chips ativos para exibir abaixo do header.
  const activeChips: ActiveChip[] = []
  if (status) activeChips.push({ key: 'status', label: `Status: ${status}`, onRemove: () => setStatus('') })
  if (cidade) activeChips.push({ key: 'cidade', label: `Cidade: ${cidade}`, onRemove: () => setCidade('') })
  if (search) activeChips.push({ key: 'search', label: `Busca: ${search}`, onRemove: () => setSearch('') })
  if (stopped) activeChips.push({ key: 'stopped', label: 'Parados 24h+', onRemove: () => setStopped(false) })
  if (dataIni !== init.ini) activeChips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni(init.ini) })
  if (dataFim !== init.fim) activeChips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim(init.fim) })

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

      {/* ── Chips de filtros ativos ──────────────────────────── */}
      <ActiveFilterChips chips={activeChips} onClearAll={clearFilters} />

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

      {/* ── Painel de Filtros (topo) ────────────────────────── */}
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
            value={draftDataIni}
            max={draftDataFim}
            onChange={e => setDraftDataIni(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftDataFim}
            min={draftDataIni}
            onChange={e => setDraftDataFim(e.target.value)}
          />
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value)}
          >
            <option value="">Todos status</option>
            {Object.keys(STATUS_CLS).map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </FilterField>
        <FilterField label="Cidade">
          <input
            type="text"
            style={filterInputStyle}
            placeholder="Filtrar por cidade"
            value={draftCidade}
            onChange={e => setDraftCidade(e.target.value)}
          />
        </FilterField>
        <FilterField label="Busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="Pedido / produto / afiliado / nome"
            value={draftSearch}
            onChange={e => setDraftSearch(e.target.value)}
          />
        </FilterField>
        <FilterField label="Parados 24h+">
          <label
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              fontSize: 13,
              cursor: 'pointer',
              height: 38,
              padding: '0 12px',
              background: 'var(--szv2-surface-alt)',
              border: '1px solid var(--szv2-border)',
              borderRadius: 10,
            }}
          >
            <input
              type="checkbox"
              checked={draftStopped}
              onChange={e => setDraftStopped(e.target.checked)}
            />
            <span>Apenas parados 24h+</span>
          </label>
        </FilterField>
      </FilterTopPanel>

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

            {/* ── Detalhe enriquecido (lazy fetch /orders/{sz_order_id}) ────── */}
            {!selectedOrder.sz_order_id ? null : detailLoading ? (
              <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16, fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                Carregando detalhes…
              </div>
            ) : detail ? (
              <>
                {/* Motoboy / entregador */}
                {detail.motoboy && (detail.motoboy.motoboy_nome || detail.motoboy.motoboy_telefone || detail.motoboy.motoboy_placa) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>🛵 Motoboy</span>
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                      {detail.motoboy.motoboy_nome && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Nome</span>
                          <span style={{ fontWeight: 600 }}>{detail.motoboy.motoboy_nome}</span>
                        </div>
                      )}
                      {detail.motoboy.motoboy_telefone && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Telefone</span>
                          <a
                            href={`tel:${detail.motoboy.motoboy_telefone.replace(/\D/g, '')}`}
                            style={{ color: 'var(--szv2-brand)', fontFamily: 'var(--szv2-font-mono)', textDecoration: 'none' }}
                          >
                            {detail.motoboy.motoboy_telefone}
                          </a>
                        </div>
                      )}
                      {detail.motoboy.motoboy_placa && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Placa</span>
                          <span style={{ fontFamily: 'var(--szv2-font-mono)', fontWeight: 700, letterSpacing: '.05em' }}>{detail.motoboy.motoboy_placa}</span>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {/* Rastreio */}
                {detail.tracking && (detail.tracking.code || detail.tracking.url || detail.tracking.carrier) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>📦 Rastreio</span>
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                      {detail.tracking.code && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Código</span>
                          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                            <span style={{ fontFamily: 'var(--szv2-font-mono)', fontWeight: 700 }}>{detail.tracking.code}</span>
                            <CopyButton text={detail.tracking.code} variant="icon" />
                          </span>
                        </div>
                      )}
                      {detail.tracking.carrier && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Transportadora</span>
                          <span>{detail.tracking.carrier}</span>
                        </div>
                      )}
                      {detail.tracking.url && (
                        <a
                          href={detail.tracking.url}
                          target="_blank"
                          rel="noreferrer"
                          className="szv2-btn-brand"
                          style={{ marginTop: 4, textAlign: 'center', fontSize: 12 }}
                        >
                          Abrir rastreio
                        </a>
                      )}
                    </div>
                  </div>
                )}

                {/* Nota Fiscal */}
                {detail.fiscal && (detail.fiscal.nfe_chave || detail.fiscal.nfe_numero || detail.fiscal.nfe_url) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>📄 Nota Fiscal</span>
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                      {detail.fiscal.nfe_numero && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Número</span>
                          <span style={{ fontFamily: 'var(--szv2-font-mono)' }}>{detail.fiscal.nfe_numero}</span>
                        </div>
                      )}
                      {detail.fiscal.nfe_serie && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Série</span>
                          <span style={{ fontFamily: 'var(--szv2-font-mono)' }}>{detail.fiscal.nfe_serie}</span>
                        </div>
                      )}
                      {detail.fiscal.nfe_status && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Status</span>
                          <span>{detail.fiscal.nfe_status}</span>
                        </div>
                      )}
                      {detail.fiscal.nfe_chave && (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                          <span style={{ color: 'var(--szv2-text-muted)', fontSize: 11 }}>Chave (44 dígitos)</span>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 4, padding: 6, background: 'var(--szv2-surface-alt)', borderRadius: 6 }}>
                            <code style={{ flex: 1, fontFamily: 'var(--szv2-font-mono)', fontSize: 11, wordBreak: 'break-all' }}>{detail.fiscal.nfe_chave}</code>
                            <CopyButton text={detail.fiscal.nfe_chave} variant="icon" />
                          </div>
                        </div>
                      )}
                      {detail.fiscal.nfe_url && (
                        <a
                          href={detail.fiscal.nfe_url}
                          target="_blank"
                          rel="noreferrer"
                          className="szv2-btn-secondary"
                          style={{ marginTop: 4, textAlign: 'center', fontSize: 12 }}
                        >
                          Abrir XML
                        </a>
                      )}
                    </div>
                  </div>
                )}

                {/* Marketing / UTM */}
                {detail.marketing && (
                  detail.marketing.utm_source || detail.marketing.utm_medium || detail.marketing.utm_campaign ||
                  detail.marketing.utm_term || detail.marketing.utm_content || detail.marketing.referrer || detail.marketing.landing_page
                ) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>🎯 Marketing / UTM</span>
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 12, fontFamily: 'var(--szv2-font-mono)' }}>
                      {detail.marketing.utm_source && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>source</span><span>{detail.marketing.utm_source}</span>
                        </div>
                      )}
                      {detail.marketing.utm_medium && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>medium</span><span>{detail.marketing.utm_medium}</span>
                        </div>
                      )}
                      {detail.marketing.utm_campaign && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>campaign</span><span>{detail.marketing.utm_campaign}</span>
                        </div>
                      )}
                      {detail.marketing.utm_term && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>term</span><span>{detail.marketing.utm_term}</span>
                        </div>
                      )}
                      {detail.marketing.utm_content && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>content</span><span>{detail.marketing.utm_content}</span>
                        </div>
                      )}
                      {detail.marketing.referrer && (
                        <div>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>referrer: </span>
                          <a href={detail.marketing.referrer} target="_blank" rel="noreferrer" style={{ color: 'var(--szv2-brand)', wordBreak: 'break-all' }}>{detail.marketing.referrer}</a>
                        </div>
                      )}
                      {detail.marketing.landing_page && (
                        <div>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>landing: </span>
                          <a href={detail.marketing.landing_page} target="_blank" rel="noreferrer" style={{ color: 'var(--szv2-brand)', wordBreak: 'break-all' }}>{detail.marketing.landing_page}</a>
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </>
            ) : null}
          </>
        )}
      </FilterDrawer>
    </div>
  )
}
