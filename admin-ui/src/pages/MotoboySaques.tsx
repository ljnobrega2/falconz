// MotoboySaques — fila de saques / pagamentos de motoboys.
// Espelha sz_mb_tab_saques() em includes/motoboy/admin.php:2016 (§2.10).
//
// Layout: 4 KPI cards (total pago, total aguardando, count pago, count
// aguardando) → top bar com filtros (motoboy, status, range de data)
// → tabela com colunas ID/Motoboy/Telefone/Valor/Data saque/Status/Obs/Criado.

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
import CardKpiSkeleton from '../components/CardKpiSkeleton'

// ─── Tipos ────────────────────────────────────────────────────────────────

type Saque = {
  id: number
  motoboy_id: number
  motoboy_nome: string
  telefone: string
  valor_total: number
  data_pagamento: string
  status: string
  obs: string
  created_at: string
}

type Summary = {
  total_pago: number
  total_aguardando: number
  count_pago: number
  count_aguardando: number
}

type StatusFiltro = '' | 'aguardando' | 'pago'

// ─── Helpers ──────────────────────────────────────────────────────────────

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtDate = (s: string) => {
  if (!s) return '—'
  const [y, m, d] = s.slice(0, 10).split('-')
  if (!y || !m || !d) return s
  return `${d}/${m}/${y}`
}

const fmtTs = (s: string | null | undefined) => {
  if (!s) return ''
  // Aceita 'YYYY-MM-DDTHH:MM:SS' ou 'YYYY-MM-DD HH:MM:SS'.
  const t = s.slice(0, 16).replace('T', ' ')
  const [d, hm] = t.split(' ')
  if (!d) return s
  return `${fmtDate(d)} ${hm || ''}`.trim()
}

function daysAgoISO(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d.toISOString().slice(0, 10)
}
const todayISO = () => new Date().toISOString().slice(0, 10)

const STATUS_FILTERS: { key: StatusFiltro; label: string }[] = [
  { key: '',           label: 'Todos'      },
  { key: 'aguardando', label: 'Aguardando' },
  { key: 'pago',       label: 'Pagos'      },
]

// ─── KPI Card ─────────────────────────────────────────────────────────────

function KpiCard(props: {
  label: string
  value: number | string
  sub?: string
  tone?: 'brand' | 'success' | 'warning' | 'danger'
}) {
  const { label, value, sub, tone = 'brand' } = props
  const color =
    tone === 'success' ? 'var(--szv2-success)' :
    tone === 'warning' ? 'var(--szv2-warning)' :
    tone === 'danger'  ? 'var(--szv2-danger)'  :
                         'var(--szv2-brand)'
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span className="szv2-kpi-value" style={{ color }}>{value}</span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

// ─── Página ───────────────────────────────────────────────────────────────

export default function MotoboySaques() {
  const [from, setFrom] = useState<string>(daysAgoISO(30))
  const [to, setTo]     = useState<string>(todayISO())
  const [status, setStatus]       = useState<StatusFiltro>('')
  const [motoboyID, setMotoboyID] = useState<string>('')

  const [items, setItems] = useState<Saque[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Painel
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftFrom, setDraftFrom] = useState(from)
  const [draftTo, setDraftTo] = useState(to)
  const [draftStatus, setDraftStatus] = useState<StatusFiltro>(status)
  const [draftMotoboyID, setDraftMotoboyID] = useState(motoboyID)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = new URLSearchParams()
      if (from)      qs.set('date_from', from)
      if (to)        qs.set('date_to', to)
      if (status)    qs.set('status', status)
      if (motoboyID) qs.set('motoboy_id', motoboyID)
      qs.set('limit', '200')

      const [list, sum] = await Promise.all([
        api<{ items: Saque[]; count: number }>(`/motoboy-saques?${qs.toString()}`),
        api<Summary>(`/motoboy-saques/summary?${qs.toString()}`),
      ])
      setItems(list.items || [])
      setSummary(sum)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line */ }, [from, to, status])

  function handleApply(e?: React.FormEvent) {
    if (e) e.preventDefault()
    load()
  }

  function openPanel() {
    setDraftFrom(from); setDraftTo(to); setDraftStatus(status); setDraftMotoboyID(motoboyID)
    setFilterOpen(true)
  }
  function applyFilters() {
    setFrom(draftFrom); setTo(draftTo); setStatus(draftStatus); setMotoboyID(draftMotoboyID)
    setFilterOpen(false)
  }
  function clearFilters() {
    const def_from = daysAgoISO(30); const def_to = todayISO()
    setFrom(def_from); setTo(def_to); setStatus(''); setMotoboyID('')
    setDraftFrom(def_from); setDraftTo(def_to); setDraftStatus(''); setDraftMotoboyID('')
    setFilterOpen(false)
  }
  const chips: ActiveChip[] = []
  if (from !== daysAgoISO(30)) chips.push({ key: 'from', label: `De: ${from}`, onRemove: () => setFrom(daysAgoISO(30)) })
  if (to !== todayISO()) chips.push({ key: 'to', label: `Até: ${to}`, onRemove: () => setTo(todayISO()) })
  if (status) chips.push({ key: 'status', label: `Status: ${status}`, onRemove: () => setStatus('') })
  if (motoboyID) chips.push({ key: 'mb', label: `Motoboy: #${motoboyID}`, onRemove: () => setMotoboyID('') })

  const kpis = summary ?? {
    total_pago: 0,
    total_aguardando: 0,
    count_pago: 0,
    count_aguardando: 0,
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Saques / pagamentos</h1>
          <p>Fila de pedidos de saque dos motoboys. Solicitações criadas pelo PWA aparecem como aguardando.</p>
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

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* 4 KPI cards */}
      {!summary && loading ? <CardKpiSkeleton count={4} /> : (
      <div
        className="szv2-kpi-grid"
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(4, minmax(0,1fr))',
          gap: 16,
          marginBottom: 16,
        }}
      >
        <KpiCard
          label="Total pago"
          value={`R$ ${fmt(kpis.total_pago)}`}
          sub="somatório de saques pagos"
          tone="success"
        />
        <KpiCard
          label="Total aguardando"
          value={`R$ ${fmt(kpis.total_aguardando)}`}
          sub="solicitações pendentes"
          tone="warning"
        />
        <KpiCard
          label="Saques pagos"
          value={kpis.count_pago}
          sub="quantidade no período"
          tone="success"
        />
        <KpiCard
          label="Aguardando"
          value={kpis.count_aguardando}
          sub="quantidade no período"
          tone="warning"
        />
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
            value={draftFrom}
            onChange={e => setDraftFrom(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftTo}
            onChange={e => setDraftTo(e.target.value)}
          />
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value as StatusFiltro)}
          >
            {STATUS_FILTERS.map(f => <option key={f.key || 'all'} value={f.key}>{f.label}</option>)}
          </select>
        </FilterField>
        <FilterField label="Motoboy (ID) / Busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ID do motoboy"
            value={draftMotoboyID}
            onChange={e => setDraftMotoboyID(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>

      {/* Tabela */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Lançamentos</h2>
            <p className="szv2-card-sub">{items.length} registro(s)</p>
          </div>
        </div>

        {loading && items.length === 0 ? (
          <TableSkeleton rows={6} cols={8} />
        ) : !loading && items.length === 0 ? (
          <EmptyState
            icon="💸"
            title="Nenhum saque registrado no período."
            description="Solicitações de saque criadas pelos motoboys aparecem aqui."
          />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Motoboy</th>
                  <th>Telefone</th>
                  <th style={{ textAlign: 'right' }}>Valor</th>
                  <th>Data saque</th>
                  <th>Status</th>
                  <th>Observação</th>
                  <th>Criado em</th>
                </tr>
              </thead>
              <tbody>
                {items.map(s => {
                  const isPago = s.status === 'pago'
                  return (
                    <tr key={s.id}>
                      <td><strong>#{s.id}</strong></td>
                      <td>
                        <div style={{ fontWeight: 700 }}>
                          {s.motoboy_nome || `Motoboy ${s.motoboy_id}`}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                          ID {s.motoboy_id}
                        </div>
                      </td>
                      <td>{s.telefone || '—'}</td>
                      <td style={{ textAlign: 'right', fontWeight: 700 }}>
                        R$ {fmt(s.valor_total)}
                      </td>
                      <td>{fmtDate(s.data_pagamento)}</td>
                      <td>
                        <span
                          className={`sz-badge ${isPago ? 'szv2-badge-success' : 'szv2-badge-warning'}`}
                        >
                          {isPago ? '✅ Pago' : '⏳ Aguardando'}
                        </span>
                      </td>
                      <td
                        style={{
                          maxWidth: 240,
                          whiteSpace: 'pre-wrap',
                          fontSize: 12,
                          color: 'var(--szv2-text-muted)',
                        }}
                      >
                        {s.obs || '—'}
                      </td>
                      <td style={{ fontSize: 12 }}>{fmtTs(s.created_at)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
