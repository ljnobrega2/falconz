import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

type MB = {
  id: number
  nome: string
  telefone: string
  entregues: number
  frustrados: number
  em_rota: number
  pendentes: number
  total_r: number
  cd_nome: string
  zona_nome: string
  taxa_sucesso: number
}

type DiaResp = {
  items: MB[]
  date: string
  sem_motoboy_count: number
}

type DashKPIs = {
  agendados: number
  embalados: number
  em_rota: number
  entregues: number
  frustrados: number
  fechamentos_pendentes: number
}

type DashResp = {
  date: string
  kpis: DashKPIs
}

const hoje = () => new Date().toISOString().slice(0, 10)

export default function MotoboysDay() {
  const [date, setDate] = useState(hoje())
  const [motoboyID, setMotoboyID] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [mbs, setMbs] = useState<MB[]>([])
  const [semMotoboy, setSemMotoboy] = useState(0)
  const [kpis, setKpis] = useState<DashKPIs | null>(null)
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(true)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Drafts no painel.
  const [draftDate, setDraftDate] = useState(date)
  const [draftMotoboy, setDraftMotoboy] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  const fetchData = (d: string, isFirst = false) => {
    if (isFirst) setLoading(true)
    setErr('')
    const qDia = new URLSearchParams({ date: d })
    if (motoboyID) qDia.set('motoboy_id', motoboyID)
    if (statusFilter) qDia.set('status', statusFilter)
    Promise.all([
      api<DiaResp>(`/motoboys/dia?${qDia.toString()}`),
      api<DashResp>(`/motoboy-dashboard?date=${d}`),
    ])
      .then(([dia, dash]) => {
        setMbs(dia.items || [])
        setSemMotoboy(dia.sem_motoboy_count ?? 0)
        setKpis(dash.kpis)
      })
      .catch(e => setErr(e.message))
      .finally(() => { if (isFirst) setLoading(false) })
  }

  useEffect(() => {
    fetchData(date, true)

    // Auto-refresh a cada 30 segundos
    intervalRef.current = setInterval(() => fetchData(date), 30_000)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [date, motoboyID, statusFilter])

  function openPanel() {
    setDraftDate(date); setDraftMotoboy(motoboyID); setDraftStatus(statusFilter)
    setFilterOpen(true)
  }
  function applyFilters() {
    if (draftDate) setDate(draftDate)
    setMotoboyID(draftMotoboy); setStatusFilter(draftStatus)
    setFilterOpen(false)
  }
  function clearFilters() {
    const t = hoje()
    setDate(t); setMotoboyID(''); setStatusFilter('')
    setDraftDate(t); setDraftMotoboy(''); setDraftStatus('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const today = hoje()
  const chips: ActiveChip[] = []
  if (date !== today) chips.push({ key: 'date', label: `Data: ${date}`, onRemove: () => setDate(today) })
  if (motoboyID) chips.push({ key: 'mb', label: `Motoboy #${motoboyID}`, onRemove: () => setMotoboyID('') })
  if (statusFilter) chips.push({ key: 'status', label: `Status: ${statusFilter}`, onRemove: () => setStatusFilter('') })
  const activeCount = chips.length

  return (
    <div>
      {/* Cabeçalho */}
      <div className="szv2-section-head">
        <div>
          <h1>Motoboys do Dia</h1>
          <p>Visão em tempo real — {mbs.length} motoboy(s) com pedidos hoje</p>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          <button className="szv2-btn szv2-btn-secondary" onClick={() => fetchData(date, true)}>
            Atualizar
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger">{err}</div>}

      {loading && (
        <div className="szv2-card" style={{ padding: '48px', textAlign: 'center' }}>
          <span style={{ color: 'var(--szv2-text-muted)' }}>Carregando…</span>
        </div>
      )}

      {/* Alerta: pedidos sem motoboy */}
      {!loading && semMotoboy > 0 && (
        <div style={{
          background: '#fff7ed',
          border: '1px solid #fed7aa',
          borderRadius: '10px',
          padding: '12px 16px',
          marginBottom: '16px',
          display: 'flex',
          alignItems: 'center',
          gap: '10px',
          fontSize: '13px',
        }}>
          <svg viewBox="0 0 20 20" style={{ width: '16px', height: '16px', fill: '#ea580c', flexShrink: 0 }}>
            <path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm0 3v5l3 2-1 1.7L9 11V5h1z" />
          </svg>
          <span>
            <strong>{semMotoboy} pedido(s)</strong> sem motoboy atribuído hoje.
          </span>
        </div>
      )}

      {/* KPIs agregados do dia */}
      {!loading && kpis && (
        <div style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(130px, 1fr))',
          gap: '12px',
          marginBottom: '20px',
        }}>
          {[
            { label: 'Agendados',            value: kpis.agendados,             color: '#6b7280' },
            { label: 'Embalados',            value: kpis.embalados,             color: '#ca8a04' },
            { label: 'Em Rota',              value: kpis.em_rota,               color: 'var(--szv2-brand)' },
            { label: 'Entregues',            value: kpis.entregues,             color: '#16a34a' },
            { label: 'Frustrados',           value: kpis.frustrados,            color: '#dc2626' },
            { label: 'Fechamentos Pend.',    value: kpis.fechamentos_pendentes, color: '#7c3aed' },
          ].map(k => (
            <div key={k.label} className="szv2-card" style={{ padding: '14px 12px', textAlign: 'center' }}>
              <div style={{ fontSize: '26px', fontWeight: 800, color: k.color }}>{k.value}</div>
              <div style={{ fontSize: '11px', color: 'var(--szv2-text-muted)', marginTop: '2px' }}>{k.label}</div>
            </div>
          ))}
        </div>
      )}

      {!loading && mbs.length === 0 && !err && (
        <div className="szv2-card">
          <div className="szv2-empty"><h3>Nenhum motoboy com pedidos hoje</h3></div>
        </div>
      )}

      {/* Grid de cards por motoboy */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: '16px' }}>
        {mbs.map(m => {
          const total_m = m.entregues + m.frustrados + m.em_rota + m.pendentes
          const pct = total_m > 0 ? Math.round((m.entregues / total_m) * 100) : 0
          const barColor = pct >= 80 ? '#16a34a' : pct >= 50 ? '#ea580c' : '#dc2626'

          return (
            <div key={m.id} className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
              {/* Header motoboy */}
              <div style={{
                padding: '14px 16px',
                background: 'var(--szv2-surface-alt)',
                borderBottom: '1px solid var(--szv2-divider)',
                display: 'flex',
                alignItems: 'center',
                gap: '10px',
              }}>
                <div style={{
                  width: '36px', height: '36px', borderRadius: '50%',
                  background: 'var(--szv2-brand)', display: 'flex',
                  alignItems: 'center', justifyContent: 'center',
                  flexShrink: 0, color: '#fff', fontWeight: 800, fontSize: '14px',
                }}>
                  {m.nome.charAt(0).toUpperCase()}
                </div>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: '15px', fontWeight: 700, color: 'var(--szv2-text)' }}>{m.nome}</div>
                  <div style={{ fontSize: '12px', color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)' }}>
                    {m.telefone || '—'}
                  </div>
                  {(m.cd_nome || m.zona_nome) && (
                    <div style={{ fontSize: '11px', color: 'var(--szv2-text-muted)', marginTop: '2px' }}>
                      {[m.cd_nome, m.zona_nome].filter(Boolean).join(' · ')}
                    </div>
                  )}
                </div>
                <div style={{ marginLeft: 'auto', textAlign: 'right', flexShrink: 0 }}>
                  <div style={{ fontSize: '18px', fontWeight: 800, color: 'var(--szv2-brand)' }}>
                    R$ {m.total_r.toFixed(2)}
                  </div>
                  <div style={{ fontSize: '11px', color: 'var(--szv2-text-muted)' }}>total do dia</div>
                </div>
              </div>

              {/* KPIs mini */}
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', borderBottom: '1px solid var(--szv2-divider)' }}>
                {[
                  { label: 'Entregues',  value: m.entregues,  color: '#16a34a' },
                  { label: 'Frustrados', value: m.frustrados, color: '#dc2626' },
                  { label: 'Em rota',    value: m.em_rota,    color: 'var(--szv2-brand)' },
                  { label: 'Pendentes',  value: m.pendentes,  color: 'var(--szv2-text-muted)' },
                ].map((k, i) => (
                  <div key={k.label} style={{
                    padding: '8px',
                    textAlign: 'center',
                    borderRight: i < 3 ? '1px solid var(--szv2-divider)' : undefined,
                  }}>
                    <div style={{ fontSize: '16px', fontWeight: 800, color: k.color }}>{k.value}</div>
                    <div style={{ fontSize: '9px', color: 'var(--szv2-text-muted)', textTransform: 'uppercase', letterSpacing: '.04em' }}>
                      {k.label}
                    </div>
                  </div>
                ))}
              </div>

              {/* Barra de progresso + taxa sucesso */}
              {total_m > 0 && (
                <div style={{ padding: '8px 14px', borderBottom: '1px solid var(--szv2-divider)' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', color: 'var(--szv2-text-muted)', marginBottom: '4px' }}>
                    <span>Taxa de entrega</span>
                    <span>{m.taxa_sucesso.toFixed(1)}%</span>
                  </div>
                  <div style={{ height: '6px', background: 'var(--szv2-divider)', borderRadius: '99px', overflow: 'hidden' }}>
                    <div style={{
                      height: '100%',
                      width: `${pct}%`,
                      background: barColor,
                      borderRadius: '99px',
                      transition: 'width .4s',
                    }} />
                  </div>
                </div>
              )}
            </div>
          )
        })}
      </div>

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data">
          <input
            type="date"
            style={filterInputStyle}
            value={draftDate}
            onChange={e => setDraftDate(e.target.value)}
          />
        </FilterField>
        <FilterField label="Motoboy ID">
          <input
            type="number"
            style={filterInputStyle}
            placeholder="ex.: 12"
            value={draftMotoboy}
            onChange={e => setDraftMotoboy(e.target.value)}
          />
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value)}
          >
            <option value="">Todos</option>
            <option value="agendado">Agendado</option>
            <option value="embalado">Embalado</option>
            <option value="em_rota">Em rota</option>
            <option value="entregue">Entregue</option>
            <option value="frustrado">Frustrado</option>
          </select>
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
