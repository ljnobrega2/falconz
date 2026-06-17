import { useEffect, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'

type A = {
  user_id: number
  email: string
  nome: string
  telefone: string
  cpf: string
  pix_key: string
  affiliate_code: string | null
  comissao_pct: number
  status: string
  created_at: string
  vinculos: number
  links_count: number
  total_clicks: number
  total_vendido_30d: number
  total_comissao_30d: number
  pedidos_count_30d: number
  last_order_at: string | null
}

const fmtBRL = (v: number) =>
  'R$ ' + (v ?? 0).toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

// Status disponíveis no select. Backend pode não filtrar todos — o filtro
// status passa via querystring; se o handler ignorar, vira no-op gracioso.
const STATUS_OPTS = [
  { key: '',            label: 'Todos' },
  { key: 'active',      label: 'Ativo' },
  { key: 'sem_vinculo', label: 'Sem vínculo' },
]

export default function Affiliates() {
  const [items, setItems] = useState<A[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Filtros aplicados — disparam fetch.
  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')

  const [filterOpen, setFilterOpen] = useState(false)

  function buildQs() {
    const p = new URLSearchParams()
    if (q.trim()) p.set('q', q.trim())
    if (status) p.set('status', status)
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    p.set('limit', '100')
    return p.toString()
  }

  async function load() {
    setLoading(true)
    try {
      const r = await api<{ items: A[]; total: number }>(`/affiliates?${buildQs()}`)
      setItems(r.items ?? [])
      setTotal(r.total)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load() }, [q, status, dataIni, dataFim])

  function openPanel() {
    setDraftQ(q)
    setDraftStatus(status)
    setDraftIni(dataIni)
    setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ)
    setStatus(draftStatus)
    setDataIni(draftIni)
    setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setStatus(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftStatus(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // KPIs agregados sobre a página atual (últimos 30 dias).
  const sumVendido = items.reduce((s, a) => s + (a.total_vendido_30d || 0), 0)
  const sumComissao = items.reduce((s, a) => s + (a.total_comissao_30d || 0), 0)
  const sumPedidos = items.reduce((s, a) => s + (a.pedidos_count_30d || 0), 0)

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (status) chips.push({ key: 'status', label: `Status: ${status}`, onRemove: () => setStatus('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Afiliados</h1>
          <p>{total} afiliados cadastrados</p>
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          <FilterButton
            active={activeCount > 0}
            count={activeCount}
            onClick={openPanel}
          />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger">{err}</div>}

      {/* KPIs últimos 30d (página atual) */}
      <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 12 }}>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Vendido (30d) — página</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{fmtBRL(sumVendido)}</span>
          <span className="szv2-kpi-meta">{sumPedidos} pedido(s)</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Comissões geradas (30d)</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-success)' }}>{fmtBRL(sumComissao)}</span>
          <span className="szv2-kpi-meta">soma da página</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Afiliados ativos (página)</span>
          <span className="szv2-kpi-value">{items.length}</span>
          <span className="szv2-kpi-meta">de {total} total</span>
        </div></div>
      </div>

      {loading && items.length === 0 ? (
        <TableSkeleton rows={6} cols={15} />
      ) : !loading && items.length === 0 ? (
        <EmptyState
          icon="🤝"
          title="Nenhum afiliado cadastrado ainda."
          description="Quando houver afiliados, eles aparecem aqui."
        />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Nome / Email</th>
              <th>Telefone</th>
              <th>CPF</th>
              <th>PIX</th>
              <th>Cod afiliado</th>
              <th className="szv2-td-num">Comissão</th>
              <th className="szv2-td-num">Vínculos</th>
              <th className="szv2-td-num">Links</th>
              <th className="szv2-td-num">Clicks</th>
              <th className="szv2-td-num">Vendido (30d)</th>
              <th className="szv2-td-num">Comissão (30d)</th>
              <th>Status</th>
              <th>Último pedido</th>
              <th>Cadastrado em</th>
            </tr>
          </thead>
          <tbody>
            {items.map(a => (
              <tr key={a.user_id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>#{a.user_id}</td>
                {/* Nome / Email empilhados: nome em cima, email logo abaixo em fonte menor. */}
                <td style={{ fontSize: 13 }}>
                  <div style={{ fontWeight: 600 }}>{a.nome || <span style={{ color: 'var(--szv2-text-faint)' }}>(sem nome)</span>}</div>
                  <div style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>{a.email}</div>
                </td>
                <td style={{ fontSize: 12 }}>
                  {a.telefone
                    ? <a href={`tel:${a.telefone.replace(/[^0-9+]/g, '')}`} style={{ color: 'var(--szv2-brand)', textDecoration: 'none' }}>{a.telefone}</a>
                    : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12, color: 'var(--szv2-text-soft)' }}>
                  {a.cpf || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12, color: 'var(--szv2-text-soft)' }}>
                  {a.pix_key || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td>
                  {a.affiliate_code
                    ? <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', padding: '2px 8px', borderRadius: '6px' }}>{a.affiliate_code}</span>
                    : <span style={{ color: 'var(--szv2-text-faint)', fontSize: '12px' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-brand)', fontWeight: 700 }}>{a.comissao_pct}%</td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-soft)', fontWeight: 600 }}>{a.vinculos}</td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-soft)' }}>{a.links_count || 0}</td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-muted)' }}>{a.total_clicks || 0}</td>
                <td className="szv2-td-num" style={{ fontSize: 13 }}>
                  {a.total_vendido_30d > 0 ? fmtBRL(a.total_vendido_30d) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-success)', fontWeight: 600 }}>
                  {a.total_comissao_30d > 0 ? fmtBRL(a.total_comissao_30d) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td>
                  <span className={`szv2-status-badge ${a.status === 'active' || a.status === 'ativo' ? 's-confirmado' : a.status === 'pending' || a.status === 'pendente' ? 's-pendente' : 's-cancelado'}`}>
                    {a.status}
                  </span>
                </td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  {a.last_order_at ? a.last_order_at.slice(0, 10) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>{a.created_at ? a.created_at.slice(0, 10) : '—'}</td>
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
            {STATUS_OPTS.map(s => <option key={s.key} value={s.key}>{s.label}</option>)}
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
