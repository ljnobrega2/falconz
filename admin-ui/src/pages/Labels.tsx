import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

type L = { id: number; wc_order_id: number; me_shipment_id: string | null; status: string; service_name: string | null; tracking_code: string | null; created_at: string }

const ST_CLS: Record<string, string> = {
  draft:    'szv2-badge-neutral',
  pending:  'szv2-badge-warning',
  queued:   'szv2-badge-info',
  printed:  'szv2-badge-info',
  released: 'szv2-badge-success',
  cancelled:'szv2-badge-danger',
}

function defaultRange() {
  const today = new Date()
  const past = new Date(today)
  past.setDate(past.getDate() - 30)
  const fmt = (d: Date) => d.toISOString().slice(0, 10)
  return { ini: fmt(past), fim: fmt(today) }
}

export default function Labels() {
  const init = useMemo(defaultRange, [])
  const [items, setItems] = useState<L[]>([])
  const [total, setTotal] = useState(0)

  // Filtros aplicados.
  const [status, setStatus] = useState('')
  const [dataIni, setDataIni] = useState(init.ini)
  const [dataFim, setDataFim] = useState(init.fim)
  const [q, setQ] = useState('')
  const [err, setErr] = useState('')

  // Drafts dentro do painel.
  const [draftStatus, setDraftStatus] = useState('')
  const [draftIni, setDraftIni] = useState(init.ini)
  const [draftFim, setDraftFim] = useState(init.fim)
  const [draftQ, setDraftQ] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  function buildQs() {
    const p = new URLSearchParams()
    if (status) p.set('status', status)
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    if (q.trim()) p.set('q', q.trim())
    p.set('limit', '100')
    return p.toString()
  }

  useEffect(() => {
    api<{ items: L[]; total: number }>(`/labels?${buildQs()}`)
      .then(r => { setItems(r.items || []); setTotal(r.total) })
      .catch(e => setErr(e.message))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, dataIni, dataFim, q])

  function openPanel() {
    setDraftStatus(status); setDraftIni(dataIni); setDraftFim(dataFim); setDraftQ(q)
    setFilterOpen(true)
  }
  function applyFilters() {
    setStatus(draftStatus); setDataIni(draftIni); setDataFim(draftFim); setQ(draftQ)
    setFilterOpen(false)
  }
  function clearFilters() {
    setStatus(''); setDataIni(init.ini); setDataFim(init.fim); setQ('')
    setDraftStatus(''); setDraftIni(init.ini); setDraftFim(init.fim); setDraftQ('')
    setFilterOpen(false)
  }

  const chips: ActiveChip[] = []
  if (status) chips.push({ key: 'status', label: `Status: ${status}`, onRemove: () => setStatus('') })
  if (dataIni !== init.ini) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni(init.ini) })
  if (dataFim !== init.fim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim(init.fim) })
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Etiquetas Melhor Envio</h1>
          <p>{total} etiquetas no total</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton
            active={chips.length > 0}
            count={chips.length}
            onClick={openPanel}
          />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger">{err}</div>}

      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Pedido</th>
              <th>Shipment ID</th>
              <th>Serviço</th>
              <th>Rastreio</th>
              <th>Status</th>
              <th>Criado</th>
            </tr>
          </thead>
          <tbody>
            {items.map(l => (
              <tr key={l.id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>#{l.id}</td>
                <td style={{ fontWeight: 600 }}>#{l.wc_order_id}</td>
                <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '11px', color: 'var(--szv2-text-muted)', maxWidth: '120px', overflow: 'hidden', textOverflow: 'ellipsis' }}>{l.me_shipment_id ?? '—'}</td>
                <td style={{ fontSize: '13px' }}>{l.service_name ?? '—'}</td>
                <td>
                  {l.tracking_code
                    ? <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px', background: 'var(--szv2-neutral-bg)', padding: '2px 8px', borderRadius: '6px' }}>{l.tracking_code}</span>
                    : <span style={{ color: 'var(--szv2-text-faint)', fontSize: '12px' }}>—</span>}
                </td>
                <td><span className={`sz-badge ${ST_CLS[l.status] || 'szv2-badge-neutral'}`}>{l.status}</span></td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>{l.created_at.slice(0, 16).replace('T', ' ')}</td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td colSpan={7}><div className="szv2-empty"><h3>Nenhuma etiqueta</h3><p>Tente outro filtro.</p></div></td></tr>
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
            max={draftFim}
            onChange={e => setDraftIni(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFim}
            min={draftIni}
            onChange={e => setDraftFim(e.target.value)}
          />
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value)}
          >
            <option value="">Todos status</option>
            {Object.keys(ST_CLS).map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </FilterField>
        <FilterField label="Busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="Pedido ou rastreio"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
