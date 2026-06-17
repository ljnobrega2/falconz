import { useEffect, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import CopyButton from '../components/CopyButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

type Aff = {
  id: number
  produtor_nome: string
  afiliado_nome: string
  produto_nome: string | null
  comissao_pct: number
  status: string
  created_at: string
  link_token: string
  link_url: string
  link_active: boolean
}

type Commission = {
  id: number
  order_id: number | null
  order_number: string
  order_total: number
  order_status: string
  afiliado_nome: string
  afiliado_email: string
  produtor_nome: string
  produtor_email: string
  produto_nome: string | null
  produto_id: number | null
  comissao_pct: number
  valor: number
  tipo: string
  status_tx: string
  link_token: string
  link_url: string
  available_at: string | null
  created_at: string
}

const STATUS_CLS: Record<string, string> = {
  active: 'szv2-badge-success',
  pending: 'szv2-badge-warning',
  paused: 'szv2-badge-neutral',
  revoked: 'szv2-badge-danger',
}

const VINCULOS_STATUSES = ['active', 'pending', 'paused', 'revoked']
const PER_PAGE = 50

export default function Commissions() {
  const [tab, setTab] = useState<'vinculos' | 'comissoes'>('vinculos')
  const [err, setErr] = useState('')

  // --- Aba Vínculos ---
  const [affs, setAffs] = useState<Aff[]>([])
  const [affsTotal, setAffsTotal] = useState(0)
  const [affsPage, setAffsPage] = useState(1)
  const [affsStatus, setAffsStatus] = useState('')
  const [affsQ, setAffsQ] = useState('')

  // --- Aba Comissões ---
  const [comms, setComms] = useState<Commission[]>([])
  const [commsTotal, setCommsTotal] = useState(0)
  const [commsPage, setCommsPage] = useState(1)
  const [commsTipo, setCommsTipo] = useState('')
  const [commsDataIni, setCommsDataIni] = useState('')
  const [commsDataFim, setCommsDataFim] = useState('')
  const [commsQ, setCommsQ] = useState('')

  // --- Painéis ---
  const [vincOpen, setVincOpen] = useState(false)
  const [commsOpen, setCommsOpen] = useState(false)

  // Drafts (aplicados só ao confirmar).
  const [draftAffsStatus, setDraftAffsStatus] = useState('')
  const [draftAffsQ, setDraftAffsQ] = useState('')
  const [draftCommsTipo, setDraftCommsTipo] = useState('')
  const [draftCommsDataIni, setDraftCommsDataIni] = useState('')
  const [draftCommsDataFim, setDraftCommsDataFim] = useState('')
  const [draftCommsQ, setDraftCommsQ] = useState('')

  // Carrega vínculos sempre que filtros ou página mudam
  useEffect(() => {
    if (tab !== 'vinculos') return
    const p = new URLSearchParams()
    if (affsStatus) p.set('status', affsStatus)
    if (affsQ.trim()) p.set('q', affsQ.trim())
    p.set('limit', String(PER_PAGE))
    p.set('offset', String((affsPage - 1) * PER_PAGE))
    api<{ items: Aff[]; total: number }>(`/affiliates/links?${p}`)
      .then(r => { setAffs(r.items ?? []); setAffsTotal(r.total ?? 0) })
      .catch(e => setErr(e.message))
  }, [tab, affsStatus, affsQ, affsPage])

  // Carrega comissões sempre que filtros ou página mudam
  useEffect(() => {
    if (tab !== 'comissoes') return
    const p = new URLSearchParams()
    if (commsTipo) p.set('status', commsTipo)
    if (commsDataIni) p.set('data_ini', commsDataIni)
    if (commsDataFim) p.set('data_fim', commsDataFim)
    if (commsQ.trim()) p.set('q', commsQ.trim())
    p.set('limit', String(PER_PAGE))
    p.set('offset', String((commsPage - 1) * PER_PAGE))
    api<{ items: Commission[]; total: number }>(`/affiliates/commissions?${p}`)
      .then(r => { setComms(r.items ?? []); setCommsTotal(r.total ?? 0) })
      .catch(e => setErr(e.message))
  }, [tab, commsTipo, commsDataIni, commsDataFim, commsQ, commsPage])

  const affsTotalPages = Math.max(1, Math.ceil(affsTotal / PER_PAGE))
  const commsTotalPages = Math.max(1, Math.ceil(commsTotal / PER_PAGE))

  // ── Helpers de abertura/aplicação/limpeza por aba ───────────────────
  function openVincPanel() {
    setDraftAffsStatus(affsStatus); setDraftAffsQ(affsQ)
    setVincOpen(true)
  }
  function applyVinc() {
    setAffsStatus(draftAffsStatus); setAffsQ(draftAffsQ); setAffsPage(1)
    setVincOpen(false)
  }
  function clearVinc() {
    setAffsStatus(''); setAffsQ(''); setAffsPage(1)
    setDraftAffsStatus(''); setDraftAffsQ('')
    setVincOpen(false)
  }

  function openCommsPanel() {
    setDraftCommsTipo(commsTipo); setDraftCommsDataIni(commsDataIni)
    setDraftCommsDataFim(commsDataFim); setDraftCommsQ(commsQ)
    setCommsOpen(true)
  }
  function applyComms() {
    setCommsTipo(draftCommsTipo); setCommsDataIni(draftCommsDataIni)
    setCommsDataFim(draftCommsDataFim); setCommsQ(draftCommsQ); setCommsPage(1)
    setCommsOpen(false)
  }
  function clearComms() {
    setCommsTipo(''); setCommsDataIni(''); setCommsDataFim(''); setCommsQ(''); setCommsPage(1)
    setDraftCommsTipo(''); setDraftCommsDataIni(''); setDraftCommsDataFim(''); setDraftCommsQ('')
    setCommsOpen(false)
  }

  // Chips ativos por aba.
  const vincChips: ActiveChip[] = []
  if (affsStatus) vincChips.push({ key: 'status', label: `Status: ${affsStatus}`, onRemove: () => { setAffsStatus(''); setAffsPage(1) } })
  if (affsQ) vincChips.push({ key: 'q', label: `Busca: ${affsQ}`, onRemove: () => { setAffsQ(''); setAffsPage(1) } })

  const commsChips: ActiveChip[] = []
  if (commsTipo) commsChips.push({ key: 'tipo', label: `Tipo: ${commsTipo}`, onRemove: () => { setCommsTipo(''); setCommsPage(1) } })
  if (commsDataIni) commsChips.push({ key: 'ini', label: `De: ${commsDataIni}`, onRemove: () => { setCommsDataIni(''); setCommsPage(1) } })
  if (commsDataFim) commsChips.push({ key: 'fim', label: `Até: ${commsDataFim}`, onRemove: () => { setCommsDataFim(''); setCommsPage(1) } })
  if (commsQ) commsChips.push({ key: 'q', label: `Busca: ${commsQ}`, onRemove: () => { setCommsQ(''); setCommsPage(1) } })

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Afiliados &amp; Comissões</h1>
          <p>Vínculos e histórico de comissões</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          {tab === 'vinculos'
            ? <FilterButton active={vincChips.length > 0} count={vincChips.length} onClick={openVincPanel} />
            : <FilterButton active={commsChips.length > 0} count={commsChips.length} onClick={openCommsPanel} />}
        </div>
      </div>

      {err && (
        <div className="sz-alert-danger" style={{ marginBottom: 12 }}>
          {err}
          <button onClick={() => setErr('')} style={{ marginLeft: 8, background: 'none', border: 'none', cursor: 'pointer' }}>✕</button>
        </div>
      )}

      <div className="szv2-tabs">
        <button className="szv2-tab" aria-selected={tab === 'vinculos'} onClick={() => setTab('vinculos')}>Vínculos</button>
        <button className="szv2-tab" aria-selected={tab === 'comissoes'} onClick={() => setTab('comissoes')}>Comissões</button>
      </div>

      {/* -------- ABA VÍNCULOS -------- */}
      {tab === 'vinculos' && (
        <>
          <ActiveFilterChips chips={vincChips} onClearAll={clearVinc} />

          <div className="szv2-table-wrap">
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>ID</th><th>Produtor</th><th>Afiliado</th><th>Produto</th><th>Oferta (link)</th><th>Regra %</th><th>Status</th><th>Criado</th>
                </tr>
              </thead>
              <tbody>
                {affs.map(a => (
                  <tr key={a.id}>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{a.id}</td>
                    <td style={{ fontSize: '13px', fontWeight: 600 }}>{a.produtor_nome}</td>
                    <td style={{ fontSize: '13px' }}>{a.afiliado_nome}</td>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-soft)' }}>{a.produto_nome ?? '—'}</td>
                    <td style={{ fontSize: 12 }}>
                      {a.link_token
                        ? (
                          <span title={a.link_url || a.link_token} style={{
                            fontFamily: 'var(--szv2-font-mono)',
                            background: a.link_active ? 'var(--szv2-brand-light)' : 'var(--szv2-neutral-bg)',
                            color: a.link_active ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
                            padding: '2px 6px', borderRadius: 4,
                          }}>{a.link_token}{!a.link_active && ' (inativo)'}</span>
                        )
                        : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)', fontWeight: 700 }}>{a.comissao_pct}%</td>
                    <td><span className={`sz-badge ${STATUS_CLS[a.status] ?? 'szv2-badge-neutral'}`}>{a.status}</span></td>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{a.created_at.slice(0, 10)}</td>
                  </tr>
                ))}
                {affs.length === 0 && (
                  <tr><td colSpan={8}><div className="szv2-empty"><h3>Sem vínculos</h3></div></td></tr>
                )}
              </tbody>
            </table>
          </div>

          {/* Paginação Vínculos */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 12, justifyContent: 'space-between' }}>
            <span style={{ fontSize: '13px', color: 'var(--szv2-text-muted)' }}>
              {affsTotal} vínculo(s) · página {affsPage} de {affsTotalPages}
            </span>
            <div style={{ display: 'flex', gap: 8 }}>
              <button
                className="szv2-btn szv2-btn-sm"
                disabled={affsPage <= 1}
                onClick={() => setAffsPage(p => p - 1)}
              >
                Anterior
              </button>
              <button
                className="szv2-btn szv2-btn-sm"
                disabled={affsPage >= affsTotalPages}
                onClick={() => setAffsPage(p => p + 1)}
              >
                Próxima
              </button>
            </div>
          </div>
        </>
      )}

      {/* -------- ABA COMISSÕES -------- */}
      {tab === 'comissoes' && (
        <>
          <ActiveFilterChips chips={commsChips} onClearAll={clearComms} />

          <div className="szv2-table-wrap">
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Pedido #</th>
                  <th className="szv2-td-num">Total pedido</th>
                  <th>Cliente / Status pedido</th>
                  <th>Afiliado</th>
                  <th>Email afiliado</th>
                  <th>Produtor</th>
                  <th>Email produtor</th>
                  <th>Produto</th>
                  <th>Link checkout</th>
                  <th className="szv2-td-num">Regra %</th>
                  <th className="szv2-td-num">Valor</th>
                  <th>Tipo</th>
                  <th>Status tx</th>
                  <th>Liberação em</th>
                  <th>Data</th>
                </tr>
              </thead>
              <tbody>
                {comms.map(c => (
                  <tr key={c.id}>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{c.id}</td>
                    {/* Pedido #: prioriza order_number (SZ-xxxxx). Fallback pra #id. */}
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-soft)', fontFamily: 'var(--szv2-font-mono)' }}>
                      {c.order_number
                        ? c.order_number
                        : c.order_id ? `#${c.order_id}` : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td className="szv2-td-num" style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                      {c.order_total > 0
                        ? `R$ ${Number(c.order_total).toFixed(2)}`
                        : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    {/* Cliente / Status pedido empilhados — nome do afiliado é nosso cliente "comprador" da comissão. */}
                    <td style={{ fontSize: 12 }}>
                      <div style={{ fontWeight: 500 }}>{c.afiliado_nome || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}</div>
                      {c.order_status && (
                        <div>
                          <span className="sz-badge szv2-badge-neutral" style={{ fontSize: 10 }}>{c.order_status}</span>
                        </div>
                      )}
                    </td>
                    <td style={{ fontSize: '13px', fontWeight: 600 }}>{c.afiliado_nome}</td>
                    <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                      {c.afiliado_email || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td style={{ fontSize: '13px' }}>{c.produtor_nome}</td>
                    <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                      {c.produtor_email || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-soft)' }}>{c.produto_nome ?? '—'}</td>
                    {/* Link checkout: copia URL completa quando disponível; fallback exibe token cru. */}
                    <td style={{ fontSize: 12 }}>
                      {c.link_url
                        ? (
                          <CopyButton
                            text={c.link_url}
                            title={c.link_url}
                            copiedLabel="✓ copiado"
                            style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 11 }}
                          >
                            {c.link_token ? `${c.link_token.slice(0, 12)}…` : 'copiar'}
                          </CopyButton>
                        )
                        : c.link_token
                          ? <span title={c.link_token} style={{ fontFamily: 'var(--szv2-font-mono)', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', padding: '2px 6px', borderRadius: 4 }}>{c.link_token.slice(0, 12)}…</span>
                          : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td className="szv2-td-num" style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)' }}>
                      {c.comissao_pct > 0 ? `${c.comissao_pct}%` : '—'}
                    </td>
                    <td className="szv2-td-num" style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-success)', fontWeight: 700 }}>R$ {Number(c.valor).toFixed(2)}</td>
                    <td><span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '11px', background: 'var(--szv2-neutral-bg)', padding: '2px 8px', borderRadius: '6px' }}>{c.tipo}</span></td>
                    <td>
                      {c.status_tx
                        ? <span className={`sz-badge ${c.status_tx === 'available' ? 'szv2-badge-success' : c.status_tx === 'pending' ? 'szv2-badge-warning' : 'szv2-badge-neutral'}`}>{c.status_tx}</span>
                        : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>
                      {c.available_at ? c.available_at.slice(0, 10) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    </td>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{c.created_at.slice(0, 10)}</td>
                  </tr>
                ))}
                {comms.length === 0 && (
                  <tr><td colSpan={16}><div className="szv2-empty"><h3>Sem comissões</h3></div></td></tr>
                )}
              </tbody>
            </table>
          </div>

          {/* Paginação Comissões */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 12, justifyContent: 'space-between' }}>
            <span style={{ fontSize: '13px', color: 'var(--szv2-text-muted)' }}>
              {commsTotal} registro(s) · página {commsPage} de {commsTotalPages}
            </span>
            <div style={{ display: 'flex', gap: 8 }}>
              <button
                className="szv2-btn szv2-btn-sm"
                disabled={commsPage <= 1}
                onClick={() => setCommsPage(p => p - 1)}
              >
                Anterior
              </button>
              <button
                className="szv2-btn szv2-btn-sm"
                disabled={commsPage >= commsTotalPages}
                onClick={() => setCommsPage(p => p + 1)}
              >
                Próxima
              </button>
            </div>
          </div>
        </>
      )}

      {/* Painel — Vínculos */}
      <FilterTopPanel
        open={vincOpen}
        onClose={() => setVincOpen(false)}
        onApply={applyVinc}
        onClear={clearVinc}
        title="Filtros — Vínculos"
      >
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftAffsStatus}
            onChange={e => setDraftAffsStatus(e.target.value)}
          >
            <option value="">Todos status</option>
            {VINCULOS_STATUSES.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </FilterField>
        <FilterField label="Busca (afiliado / produtor)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="Nome…"
            value={draftAffsQ}
            onChange={e => setDraftAffsQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>

      {/* Painel — Comissões */}
      <FilterTopPanel
        open={commsOpen}
        onClose={() => setCommsOpen(false)}
        onApply={applyComms}
        onClear={clearComms}
        title="Filtros — Comissões"
      >
        <FilterField label="Data inicial">
          <input
            type="date"
            style={filterInputStyle}
            value={draftCommsDataIni}
            onChange={e => setDraftCommsDataIni(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftCommsDataFim}
            onChange={e => setDraftCommsDataFim(e.target.value)}
          />
        </FilterField>
        <FilterField label="Tipo">
          <select
            style={filterInputStyle}
            value={draftCommsTipo}
            onChange={e => setDraftCommsTipo(e.target.value)}
          >
            <option value="">Todos os tipos</option>
            <option value="commission">commission</option>
            <option value="penalty">penalty</option>
            <option value="approval">approval</option>
          </select>
        </FilterField>
        <FilterField label="Busca (afiliado / produtor)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="Nome…"
            value={draftCommsQ}
            onChange={e => setDraftCommsQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
