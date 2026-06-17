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

type Tab = 'webhooks' | 'integrations' | 'motoboy'
type WH = { id: number; webhook_id: number | null; event_type: string; response_code: number | null; response_body: string | null; created_at: string }
type IL = { id: number; user_id: number | null; event: string; payload: unknown; created_at: string }
type AU = { id: number; pedido_id: number | null; motoboy_id: number | null; actor_tipo: string; acao: string; de_status: string | null; para_status: string | null; meta_json: string | null; created_at: string }

export default function Logs() {
  const [tab, setTab] = useState<Tab>('webhooks')
  const [wh, setWh] = useState<WH[]>([])
  const [il, setIl] = useState<IL[]>([])
  const [au, setAu] = useState<AU[]>([])
  const [totals, setTotals] = useState({ wh: 0, il: 0, au: 0 })
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Filtros aplicados — compartilhados entre tabs.
  // ação/ator só fazem sentido na aba Motoboy mas mantemos sempre no painel.
  const [q, setQ] = useState('')
  const [acao, setAcao] = useState('')
  const [ator, setAtor] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftAcao, setDraftAcao] = useState('')
  const [draftAtor, setDraftAtor] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  useEffect(() => {
    setErr('')
    setLoading(true)
    const p = new URLSearchParams()
    if (q.trim()) p.set('q', q.trim())
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    if (tab === 'motoboy') {
      if (acao) p.set('acao', acao)
      if (ator) p.set('ator', ator)
    }
    p.set('limit', '100')
    const qs = p.toString()
    if (tab === 'webhooks')
      api<{ items: WH[]; total: number }>(`/logs/webhooks?${qs}`).then(r => { setWh(r.items ?? []); setTotals(t => ({ ...t, wh: r.total ?? 0 })) }).catch(e => setErr(e.message)).finally(() => setLoading(false))
    if (tab === 'integrations')
      api<{ items: IL[]; total: number }>(`/logs/integrations?${qs}`).then(r => { setIl(r.items ?? []); setTotals(t => ({ ...t, il: r.total ?? 0 })) }).catch(e => setErr(e.message)).finally(() => setLoading(false))
    if (tab === 'motoboy')
      api<{ items: AU[]; total: number }>(`/logs/motoboy?${qs}`).then(r => { setAu(r.items ?? []); setTotals(t => ({ ...t, au: r.total ?? 0 })) }).catch(e => setErr(e.message)).finally(() => setLoading(false))
  }, [tab, q, acao, ator, dataIni, dataFim])

  function openPanel() {
    setDraftQ(q); setDraftAcao(acao); setDraftAtor(ator); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setAcao(draftAcao); setAtor(draftAtor); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setAcao(''); setAtor(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftAcao(''); setDraftAtor(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  if (tab === 'motoboy' && acao) chips.push({ key: 'acao', label: `Ação: ${acao}`, onRemove: () => setAcao('') })
  if (tab === 'motoboy' && ator) chips.push({ key: 'ator', label: `Ator: ${ator}`, onRemove: () => setAtor('') })
  const activeCount = chips.length

  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: 'webhooks',     label: 'Webhooks',        count: totals.wh },
    { key: 'integrations', label: 'Integrações',     count: totals.il },
    { key: 'motoboy',      label: 'Auditoria Motoboy', count: totals.au },
  ]

  return (
    <div>
      <div className="szv2-section-head">
        <div><h1>Logs do Sistema</h1><p>Histórico de eventos e auditoria</p></div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      <div className="szv2-tabs">
        {tabs.map(t => (
          <button key={t.key} className="szv2-tab" aria-selected={tab === t.key} onClick={() => setTab(t.key)}>
            {t.label}
            {t.count > 0 && <span style={{ marginLeft: '6px', fontSize: '11px', color: 'var(--szv2-text-faint)' }}>({t.count})</span>}
          </button>
        ))}
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {tab === 'webhooks' && (
        loading && wh.length === 0 ? (
          <TableSkeleton rows={6} cols={5} />
        ) : !loading && wh.length === 0 ? (
          <EmptyState icon="🔗" title="Sem logs de webhook no período." />
        ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr>
              <th>ID</th><th>Webhook</th><th>Evento</th><th>HTTP</th><th>Quando</th>
            </tr></thead>
            <tbody>
              {wh.map(l => (
                <tr key={l.id}>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{l.id}</td>
                  <td style={{ fontSize: '13px' }}>{l.webhook_id ?? '—'}</td>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px' }}>{l.event_type}</td>
                  <td>
                    <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px', color: l.response_code && l.response_code < 300 ? 'var(--szv2-success)' : 'var(--szv2-danger)' }}>
                      {l.response_code ?? '—'}
                    </span>
                  </td>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{l.created_at.slice(0, 16).replace('T', ' ')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        )
      )}

      {tab === 'integrations' && (
        loading && il.length === 0 ? (
          <TableSkeleton rows={6} cols={5} />
        ) : !loading && il.length === 0 ? (
          <EmptyState icon="🔌" title="Sem logs de integração no período." />
        ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr>
              <th>ID</th><th>Usuário</th><th>Tipo</th><th>Mensagem</th><th>Quando</th>
            </tr></thead>
            <tbody>
              {il.map(l => (
                <tr key={l.id}>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{l.id}</td>
                  <td style={{ fontSize: '13px' }}>{l.user_id ?? '—'}</td>
                  <td><span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '11px', background: 'var(--szv2-neutral-bg)', padding: '2px 8px', borderRadius: '6px' }}>{l.event}</span></td>
                  <td style={{ fontSize: '13px', color: 'var(--szv2-text-soft)', maxWidth: '300px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{typeof l.payload === 'string' ? l.payload : JSON.stringify(l.payload)}</td>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{l.created_at.slice(0, 16).replace('T', ' ')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        )
      )}

      {tab === 'motoboy' && (
        loading && au.length === 0 ? (
          <TableSkeleton rows={6} cols={5} />
        ) : !loading && au.length === 0 ? (
          <EmptyState icon="🛵" title="Sem auditoria motoboy no período." />
        ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr>
              <th>ID</th><th>Pedido</th><th>Ação</th><th>Descrição</th><th>Quando</th>
            </tr></thead>
            <tbody>
              {au.map(a => (
                <tr key={a.id}>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{a.id}</td>
                  <td style={{ fontSize: '13px' }}>{a.pedido_id ? `#${a.pedido_id}` : '—'}</td>
                  <td><span style={{ background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', fontSize: '11px', fontWeight: 700, padding: '2px 8px', borderRadius: '6px' }}>{a.acao}</span></td>
                  <td style={{ fontSize: '13px', color: 'var(--szv2-text-soft)', maxWidth: '300px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{a.de_status && a.para_status ? `${a.de_status} → ${a.para_status}` : (a.meta_json ?? '—')}</td>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{a.created_at.slice(0, 16).replace('T', ' ')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        )
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
        <FilterField label="Busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: order_id / event"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
        {tab === 'motoboy' && (
          <>
            <FilterField label="Ação">
              <input
                type="search"
                style={filterInputStyle}
                placeholder="ex.: aceitou, recusou"
                value={draftAcao}
                onChange={e => setDraftAcao(e.target.value)}
              />
            </FilterField>
            <FilterField label="Ator">
              <input
                type="search"
                style={filterInputStyle}
                placeholder="ex.: motoboy, ol, admin"
                value={draftAtor}
                onChange={e => setDraftAtor(e.target.value)}
              />
            </FilterField>
          </>
        )}
      </FilterTopPanel>
    </div>
  )
}
