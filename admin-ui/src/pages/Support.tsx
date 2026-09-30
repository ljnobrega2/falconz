// Área de SUPORTE do admin (#45).
//
// Consome go/admin support.go:
//   GET /support/tickets        → todos os chamados (todos os usuários) + filtros
//   GET /support/tickets/{id}   → chamado + thread de mensagens
//
// SOMENTE LEITURA: o admin lista, abre o detalhe e lê a conversa. Responder/fechar
// pelo admin é escopo futuro (a tabela suporta autor_tipo='admin', mas o pedido
// é "área de suporte no admin com detalhamento").
//
// O drawer lateral (DetailDrawer) mostra o cabeçalho do chamado (quem abriu,
// categoria, prioridade, datas) + a thread de mensagens em estilo conversa.
//
// "Pedido relacionado": sz_portal_tickets NÃO tem coluna de pedido (só a
// categoria ENUM 'pedido'). Mostramos a categoria — não há FK a um pedido.
import { useEffect, useState } from 'react'
import { api } from '../api'
import DetailDrawer from '../components/DetailDrawer'
import StatusBadge from '../components/StatusBadge'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'
import FilterButton from '../components/FilterButton'
import FalkSelect from '../components/FalkSelect'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

type TicketListItem = {
  id: number
  portal_user_id: number
  usuario_nome: string
  usuario_email: string
  assunto: string
  categoria: string
  status: string
  prioridade: string
  created_at: string
  updated_at: string
  total_msgs: number
  ultima_msg_autor: string | null
}

type TicketMessage = {
  id: number
  autor_tipo: string
  autor_nome: string | null
  mensagem: string
  created_at: string
}

type TicketDetail = {
  id: number
  portal_user_id: number
  usuario_nome: string
  usuario_email: string
  assunto: string
  categoria: string
  status: string
  prioridade: string
  created_at: string
  updated_at: string
  fechado_at: string | null
}

const STATUS_LABELS: Record<string, string> = {
  '': 'Todos',
  aberto: 'Aberto',
  em_analise: 'Em análise',
  respondido: 'Respondido',
  fechado: 'Fechado',
}

const CATEGORIA_LABELS: Record<string, string> = {
  '': 'Todas',
  financeiro: 'Financeiro',
  pedido: 'Pedido',
  tecnico: 'Técnico',
  outro: 'Outro',
}

function fmtDateTime(raw: string | null): string {
  if (!raw) return '—'
  const d = raw.slice(0, 10)
  const t = raw.slice(11, 16)
  const [y, m, day] = d.split('-')
  if (!y || !m || !day) return raw
  return `${day}/${m}/${y}${t ? ' ' + t : ''}`
}

export default function Support() {
  const [items, setItems] = useState<TicketListItem[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Filtros aplicados.
  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [categoria, setCategoria] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftCategoria, setDraftCategoria] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  // Detalhe (drawer).
  const [detail, setDetail] = useState<TicketDetail | null>(null)
  const [msgs, setMsgs] = useState<TicketMessage[]>([])
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailErr, setDetailErr] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const p = new URLSearchParams()
      if (q.trim()) p.set('q', q.trim())
      if (status) p.set('status', status)
      if (categoria) p.set('categoria', categoria)
      p.set('limit', '200')
      const r = await api<{ items: TicketListItem[]; total: number }>(
        `/support/tickets?${p.toString()}`,
      )
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load() /* eslint-disable-next-line react-hooks/exhaustive-deps */
  }, [q, status, categoria])

  async function openDetail(t: TicketListItem) {
    setDetail({
      id: t.id,
      portal_user_id: t.portal_user_id,
      usuario_nome: t.usuario_nome,
      usuario_email: t.usuario_email,
      assunto: t.assunto,
      categoria: t.categoria,
      status: t.status,
      prioridade: t.prioridade,
      created_at: t.created_at,
      updated_at: t.updated_at,
      fechado_at: null,
    })
    setMsgs([])
    setDetailErr('')
    setDetailLoading(true)
    try {
      const r = await api<{ ticket: TicketDetail; msgs: TicketMessage[] }>(
        `/support/tickets/${t.id}`,
      )
      setDetail(r.ticket)
      setMsgs(r.msgs || [])
    } catch (e: any) {
      setDetailErr(e.message || 'Erro ao carregar chamado')
    } finally {
      setDetailLoading(false)
    }
  }

  function openPanel() {
    setDraftQ(q)
    setDraftStatus(status)
    setDraftCategoria(categoria)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ)
    setStatus(draftStatus)
    setCategoria(draftCategoria)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ('')
    setStatus('')
    setCategoria('')
    setDraftQ('')
    setDraftStatus('')
    setDraftCategoria('')
    setFilterOpen(false)
  }

  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (status) chips.push({ key: 'status', label: `Status: ${STATUS_LABELS[status] || status}`, onRemove: () => setStatus('') })
  if (categoria) chips.push({ key: 'cat', label: `Categoria: ${CATEGORIA_LABELS[categoria] || categoria}`, onRemove: () => setCategoria('') })
  const activeCount = chips.length

  const abertos = items.filter(t => t.status !== 'fechado').length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Suporte</h1>
          <p>
            {items.length} chamado(s){abertos > 0 ? ` — ${abertos} em aberto` : ''}
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && items.length > 0 && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>
      )}

      {loading && items.length === 0 ? (
        <TableSkeleton rows={5} cols={7} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={load} />
      ) : !loading && items.length === 0 ? (
        <EmptyState
          icon="💬"
          title="Nenhum chamado de suporte."
          description="Chamados abertos pelos usuários no portal aparecem aqui."
        />
      ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Assunto</th>
                <th>Usuário</th>
                <th>Categoria</th>
                <th>Status</th>
                <th>Msgs</th>
                <th>Atualizado</th>
                <th style={{ textAlign: 'right' }}>Ações</th>
              </tr>
            </thead>
            <tbody>
              {items.map(t => (
                <tr key={t.id}>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{t.id}</td>
                  <td style={{ fontWeight: 600 }}>{t.assunto}</td>
                  <td style={{ fontSize: 13 }}>
                    {t.usuario_nome || `#${t.portal_user_id}`}
                    {t.usuario_email && (
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{t.usuario_email}</div>
                    )}
                  </td>
                  <td style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
                    {CATEGORIA_LABELS[t.categoria] || t.categoria}
                  </td>
                  <td><StatusBadge status={t.status} /></td>
                  <td style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>{t.total_msgs}</td>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>{fmtDateTime(t.updated_at)}</td>
                  <td style={{ textAlign: 'right' }}>
                    <button
                      className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                      onClick={() => openDetail(t)}
                    >
                      Detalhes
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Drawer lateral — cabeçalho do chamado + thread de mensagens */}
      <DetailDrawer
        open={!!detail}
        onClose={() => setDetail(null)}
        large
        title={detail ? `Chamado #${detail.id}` : ''}
      >
        {detail && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            <div>
              <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--szv2-text)', marginBottom: 6 }}>
                {detail.assunto}
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                <StatusBadge status={detail.status} />
                <span className="sz-badge szv2-badge-neutral">
                  {CATEGORIA_LABELS[detail.categoria] || detail.categoria}
                </span>
                <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  prioridade: {detail.prioridade}
                </span>
              </div>
            </div>

            <div
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(2, 1fr)',
                gap: 10,
                padding: '12px 14px',
                borderRadius: 10,
                border: '1px solid var(--szv2-divider)',
                background: 'var(--szv2-surface-alt)',
              }}
            >
              <Meta label="Usuário" value={detail.usuario_nome || `#${detail.portal_user_id}`} />
              <Meta label="E-mail" value={detail.usuario_email || '—'} />
              <Meta label="Aberto em" value={fmtDateTime(detail.created_at)} />
              <Meta label="Atualizado em" value={fmtDateTime(detail.updated_at)} />
              {detail.fechado_at && (
                <Meta label="Fechado em" value={fmtDateTime(detail.fechado_at)} />
              )}
            </div>

            <div>
              <div
                style={{
                  fontSize: 11,
                  fontWeight: 700,
                  textTransform: 'uppercase',
                  letterSpacing: '.04em',
                  color: 'var(--szv2-text-muted)',
                  marginBottom: 10,
                }}
              >
                Conversa
              </div>

              {detailLoading ? (
                <div style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando mensagens…</div>
              ) : detailErr ? (
                <div className="sz-alert-danger">{detailErr}</div>
              ) : msgs.length === 0 ? (
                <div style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Sem mensagens.</div>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {msgs.map(m => {
                    const isAdmin = m.autor_tipo === 'admin'
                    return (
                      <div
                        key={m.id}
                        style={{
                          alignSelf: isAdmin ? 'flex-end' : 'flex-start',
                          maxWidth: '85%',
                          padding: '10px 12px',
                          borderRadius: 12,
                          border: '1px solid var(--szv2-divider)',
                          background: isAdmin
                            ? 'rgba(30,111,242,.08)'
                            : 'var(--szv2-surface-alt)',
                        }}
                      >
                        <div
                          style={{
                            fontSize: 11,
                            fontWeight: 700,
                            color: isAdmin ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
                            marginBottom: 4,
                          }}
                        >
                          {m.autor_nome || (isAdmin ? 'Admin' : 'Cliente')}
                          <span style={{ fontWeight: 400, marginLeft: 8, color: 'var(--szv2-text-muted)' }}>
                            {fmtDateTime(m.created_at)}
                          </span>
                        </div>
                        <div style={{ fontSize: 13, whiteSpace: 'pre-wrap', color: 'var(--szv2-text)' }}>
                          {m.mensagem}
                        </div>
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          </div>
        )}
      </DetailDrawer>

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Status">
          <FalkSelect
            value={draftStatus}
            onChange={v => setDraftStatus(v)}
            options={Object.keys(STATUS_LABELS).map(k => ({
              value: k,
              label: STATUS_LABELS[k],
            }))}
          />
        </FilterField>
        <FilterField label="Categoria">
          <FalkSelect
            value={draftCategoria}
            onChange={v => setDraftCategoria(v)}
            options={Object.keys(CATEGORIA_LABELS).map(k => ({
              value: k,
              label: CATEGORIA_LABELS[k],
            }))}
          />
        </FilterField>
        <FilterField label="Busca (assunto / usuário / e-mail)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: cobrança…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{label}</div>
      <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', wordBreak: 'break-word' }}>{value}</div>
    </div>
  )
}
