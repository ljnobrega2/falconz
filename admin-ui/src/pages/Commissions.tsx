import { useEffect, useState } from 'react'
import { api } from '../api'

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
  afiliado_nome: string
  produtor_nome: string
  produto_nome: string | null
  comissao_pct: number
  valor: number
  tipo: string
  status_tx: string
  link_token: string
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

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Afiliados &amp; Comissões</h1>
          <p>Vínculos e histórico de comissões</p>
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
          {/* Filtros */}
          <div className="szv2-card" style={{ marginBottom: 16 }}>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
              <div>
                <label className="szv2-label" style={{ display: 'block', marginBottom: 4 }}>Status</label>
                <select
                  className="szv2-select"
                  style={{ minWidth: 160 }}
                  value={affsStatus}
                  onChange={e => { setAffsStatus(e.target.value); setAffsPage(1) }}
                >
                  <option value="">Todos status</option>
                  {VINCULOS_STATUSES.map(s => <option key={s} value={s}>{s}</option>)}
                </select>
              </div>
              <div>
                <label className="szv2-label" style={{ display: 'block', marginBottom: 4 }}>Buscar afiliado / produtor</label>
                <input
                  type="text"
                  className="szv2-input"
                  placeholder="Nome…"
                  style={{ minWidth: 200 }}
                  value={affsQ}
                  onChange={e => { setAffsQ(e.target.value); setAffsPage(1) }}
                />
              </div>
              {(affsStatus || affsQ) && (
                <button
                  className="szv2-btn szv2-btn-sm"
                  onClick={() => { setAffsStatus(''); setAffsQ(''); setAffsPage(1) }}
                >
                  Limpar filtros
                </button>
              )}
            </div>
          </div>

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
          {/* Filtros */}
          <div className="szv2-card" style={{ marginBottom: 16 }}>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
              <div>
                <label className="szv2-label" style={{ display: 'block', marginBottom: 4 }}>Tipo</label>
                <select
                  className="szv2-select"
                  style={{ minWidth: 160 }}
                  value={commsTipo}
                  onChange={e => { setCommsTipo(e.target.value); setCommsPage(1) }}
                >
                  <option value="">Todos os tipos</option>
                  <option value="commission">commission</option>
                  <option value="penalty">penalty</option>
                  <option value="approval">approval</option>
                </select>
              </div>
              <div>
                <label className="szv2-label" style={{ display: 'block', marginBottom: 4 }}>De</label>
                <input
                  type="date"
                  className="szv2-input"
                  value={commsDataIni}
                  onChange={e => { setCommsDataIni(e.target.value); setCommsPage(1) }}
                />
              </div>
              <div>
                <label className="szv2-label" style={{ display: 'block', marginBottom: 4 }}>Até</label>
                <input
                  type="date"
                  className="szv2-input"
                  value={commsDataFim}
                  onChange={e => { setCommsDataFim(e.target.value); setCommsPage(1) }}
                />
              </div>
              <div>
                <label className="szv2-label" style={{ display: 'block', marginBottom: 4 }}>Buscar afiliado / produtor</label>
                <input
                  type="text"
                  className="szv2-input"
                  placeholder="Nome…"
                  style={{ minWidth: 200 }}
                  value={commsQ}
                  onChange={e => { setCommsQ(e.target.value); setCommsPage(1) }}
                />
              </div>
              {(commsTipo || commsDataIni || commsDataFim || commsQ) && (
                <button
                  className="szv2-btn szv2-btn-sm"
                  onClick={() => { setCommsTipo(''); setCommsDataIni(''); setCommsDataFim(''); setCommsQ(''); setCommsPage(1) }}
                >
                  Limpar filtros
                </button>
              )}
            </div>
          </div>

          <div className="szv2-table-wrap">
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Pedido</th>
                  <th>Afiliado</th>
                  <th>Produtor</th>
                  <th>Produto</th>
                  <th>Oferta</th>
                  <th className="szv2-td-num">Regra %</th>
                  <th className="szv2-td-num">Valor</th>
                  <th>Tipo</th>
                  <th>Status tx</th>
                  <th>Data</th>
                </tr>
              </thead>
              <tbody>
                {comms.map(c => (
                  <tr key={c.id}>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{c.id}</td>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>
                      {c.order_id ? `#${c.order_id}` : '—'}
                    </td>
                    <td style={{ fontSize: '13px', fontWeight: 600 }}>{c.afiliado_nome}</td>
                    <td style={{ fontSize: '13px' }}>{c.produtor_nome}</td>
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-soft)' }}>{c.produto_nome ?? '—'}</td>
                    <td style={{ fontSize: 12 }}>
                      {c.link_token
                        ? <span title={c.link_token} style={{ fontFamily: 'var(--szv2-font-mono)', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', padding: '2px 6px', borderRadius: 4 }}>{c.link_token}</span>
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
                    <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{c.created_at.slice(0, 10)}</td>
                  </tr>
                ))}
                {comms.length === 0 && (
                  <tr><td colSpan={11}><div className="szv2-empty"><h3>Sem comissões</h3></div></td></tr>
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
    </div>
  )
}
