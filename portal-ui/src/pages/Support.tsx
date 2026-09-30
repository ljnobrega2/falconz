// Suporte (Tickets) — ligada ao go/portal (/portal/support/tickets…).
// Port fiel de templates/portal/v2/sections/support.php + a lógica JS de
// assets/js/senderzz-dashboard-v2.js (bloco "Suporte — tickets", Fase 9).
//
// Contrato go/portal (SupportHandler) — chaves exatas, NÃO usar r.data:
//   GET  /portal/support/tickets               → {ok, tickets[], total}
//   POST /portal/support/tickets               → {ok, ticket_id}
//   GET  /portal/support/tickets/{id}          → {ok, ticket, msgs[]}
//   POST /portal/support/tickets/{id}/messages → {ok}
//   POST /portal/support/tickets/{id}/close    → {ok}
//
// UX idêntica ao WP: lista ↔ detalhe via estado (não sub-rota), modal "novo
// chamado" (szv2-modal-overlay/szv2-open), fechar via confirmAsync.
import { FormEvent, useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'
import FalkSelect from '../components/FalkSelect'

// ── Tipos (espelham os structs do support_portal.go) ─────────────────────────
type TicketListItem = {
  id: number
  assunto: string
  categoria: string
  status: string
  prioridade: string
  created_at: string
  updated_at: string
  total_msgs: number
  ultima_msg_autor: string | null
}

type TicketDetail = {
  id: number
  assunto: string
  categoria: string
  status: string
  prioridade: string
  created_at: string
  updated_at: string
  fechado_at: string | null
}

type TicketMessage = {
  id: number
  autor_tipo: string
  autor_nome: string | null
  mensagem: string
  created_at: string
}

type ListResp = { ok: boolean; tickets: TicketListItem[]; total: number }
type DetailResp = { ok: boolean; ticket: TicketDetail; msgs: TicketMessage[] }
type CreateResp = { ok: boolean; ticket_id: number }

// Espelha STATUS_MAP do JS V2.
const STATUS_LABEL: Record<string, string> = {
  aberto: 'Aberto',
  em_analise: 'Em análise',
  respondido: 'Respondido',
  fechado: 'Fechado',
}

// Espelha a escolha de badge no JS V2.
function statusBadgeClass(status: string): string {
  if (status === 'fechado') return 'szv2-badge-neutral'
  if (status === 'respondido') return 'szv2-badge-success'
  return 'szv2-badge-warning'
}

// Categorias do select do modal (espelha support.php).
const CATEGORIAS: { value: string; label: string }[] = [
  { value: 'pedido', label: 'Pedido' },
  { value: 'financeiro', label: 'Financeiro' },
  { value: 'tecnico', label: 'Técnico' },
  { value: 'outro', label: 'Outro' },
]

// Datas chegam como "2026-06-18 12:34:56" — corta para exibir d/m/Y H:i.
function fmtDate(s: string | null | undefined): string {
  if (!s) return '—'
  return s.slice(0, 10)
}
function fmtDateTime(s: string | null | undefined): string {
  if (!s) return ''
  return s.slice(0, 16)
}

export default function Support() {
  const toast = useToast()

  // Lista
  const [tickets, setTickets] = useState<TicketListItem[]>([])
  const [loadingList, setLoadingList] = useState(true)
  const [listErr, setListErr] = useState('')

  // Detalhe (lista ↔ detalhe via estado, como no WP)
  const [currentId, setCurrentId] = useState<number | null>(null)
  const [detail, setDetail] = useState<TicketDetail | null>(null)
  const [msgs, setMsgs] = useState<TicketMessage[]>([])
  const [loadingDetail, setLoadingDetail] = useState(false)

  // Resposta
  const [reply, setReply] = useState('')
  const [sending, setSending] = useState(false)

  // Fechar
  const [closing, setClosing] = useState(false)

  // Modal: novo chamado
  const [modalOpen, setModalOpen] = useState(false)
  const [assunto, setAssunto] = useState('')
  const [categoria, setCategoria] = useState('pedido')
  const [mensagem, setMensagem] = useState('')
  const [createErr, setCreateErr] = useState('')
  const [creating, setCreating] = useState(false)

  function loadTickets() {
    setLoadingList(true)
    setListErr('')
    api<ListResp>('/portal/support/tickets')
      .then(r => setTickets(r.tickets || []))
      .catch(e => setListErr(e.message || 'Erro ao carregar chamados.'))
      .finally(() => setLoadingList(false))
  }

  useEffect(loadTickets, [])

  function openTicket(id: number) {
    setCurrentId(id)
    setReply('')
    setLoadingDetail(true)
    setDetail(null)
    setMsgs([])
    api<DetailResp>(`/portal/support/tickets/${id}`)
      .then(r => {
        setDetail(r.ticket || null)
        setMsgs(r.msgs || [])
      })
      .catch(e => toast('err', e.message || 'Erro ao carregar mensagens.'))
      .finally(() => setLoadingDetail(false))
  }

  function back() {
    setCurrentId(null)
    setDetail(null)
    setMsgs([])
    loadTickets()
  }

  async function sendReply() {
    if (!currentId) return
    const msg = reply.trim()
    if (msg.length < 3) {
      toast('warn', 'Escreva uma mensagem antes de enviar.')
      return
    }
    setSending(true)
    try {
      await api(`/portal/support/tickets/${currentId}/messages`, {
        method: 'POST',
        body: JSON.stringify({ mensagem: msg }),
      })
      toast('ok', 'Resposta enviada.')
      setReply('')
      openTicket(currentId)
    } catch (e: any) {
      toast('err', e.message || 'Erro ao enviar resposta.')
    } finally {
      setSending(false)
    }
  }

  async function closeTicket() {
    if (!currentId) return
    // JS V2 usa danger:false mesmo com botão estilizado como danger — manter.
    const ok = await confirmAsync({
      title: 'Fechar chamado',
      message: `Fechar o chamado #${currentId}?`,
      confirmLabel: 'Fechar chamado',
      danger: false,
    })
    if (!ok) return
    setClosing(true)
    try {
      await api(`/portal/support/tickets/${currentId}/close`, { method: 'POST' })
      toast('ok', 'Chamado fechado.')
      openTicket(currentId)
    } catch (e: any) {
      toast('err', e.message || 'Erro ao fechar chamado.')
    } finally {
      setClosing(false)
    }
  }

  async function createTicket(e: FormEvent) {
    e.preventDefault()
    setCreateErr('')
    // Validações client-side idênticas ao JS V2 (feedback instantâneo).
    if (assunto.trim().length < 5) {
      setCreateErr('Assunto muito curto (mín. 5 caracteres).')
      return
    }
    if (mensagem.trim().length < 10) {
      setCreateErr('Mensagem muito curta (mín. 10 caracteres).')
      return
    }
    setCreating(true)
    try {
      const r = await api<CreateResp>('/portal/support/tickets', {
        method: 'POST',
        body: JSON.stringify({ assunto, categoria, mensagem }),
      })
      toast('ok', 'Chamado aberto!')
      setModalOpen(false)
      setAssunto('')
      setMensagem('')
      setCategoria('pedido')
      if (r.ticket_id) openTicket(r.ticket_id)
      else loadTickets()
    } catch (e: any) {
      setCreateErr(e.message || 'Erro ao abrir chamado.')
    } finally {
      setCreating(false)
    }
  }

  const isClosed = detail?.status === 'fechado'

  return (
    <section id="sec-support" className="sz-sec">
      {/* Lista de chamados */}
      {currentId === null && (
        <>
          <div className="szv2-page-head" style={{ marginBottom: 16 }}>
            <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>Suporte</h2>
            <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>Abra chamados e acompanhe nossas respostas.</p>
          </div>
          {/* #77: CTA "+ Novo chamado" duplicado eliminado. Antes o botão do canto
              superior direito aparecia SEMPRE, duplicando o CTA central do estado
              vazio. Agora o do topo só aparece quando JÁ existem chamados (lista não
              vazia) — no estado vazio fica só o botão central do EmptyState. Nunca
              os dois ao mesmo tempo, e a lista cheia mantém a forma de criar. */}
          {listErr && <div className="sz-alert-danger">{listErr}</div>}

          {!loadingList && tickets.length > 0 && (
            <div className="szv2-page-actions" style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 16 }}>
              <button type="button" className="szv2-btn szv2-btn-brand" onClick={() => setModalOpen(true)}>
                + Novo chamado
              </button>
            </div>
          )}

          <div className="szv2-card szv2-card-table">
            {loadingList ? (
              <p className="szv2-empty-inline" style={{ padding: 20 }}>Carregando chamados…</p>
            ) : tickets.length === 0 ? (
              <EmptyState
                icon="🎧"
                title="Nenhum chamado aberto"
                description='Use o botão "Novo chamado" para começar.'
                action={{ label: '+ Novo chamado', onClick: () => setModalOpen(true) }}
              />
            ) : (
              <div className="szv2-table-wrap szv2-table-flush">
                <table className="szv2-table" style={{ width: '100%' }}>
                  <thead>
                    <tr>
                      <th>#</th>
                      <th>Assunto</th>
                      <th>Categoria</th>
                      <th>Status</th>
                      <th>Atualizado</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {tickets.map(t => (
                      <tr key={t.id}>
                        <td className="szv2-num">#{t.id}</td>
                        <td>{t.assunto}</td>
                        <td>{t.categoria || '—'}</td>
                        <td>
                          <span className={`sz-badge ${statusBadgeClass(t.status)}`}>
                            {STATUS_LABEL[t.status] || t.status}
                          </span>
                        </td>
                        <td className="szv2-td-mono">{fmtDate(t.updated_at)}</td>
                        <td>
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                            onClick={() => openTicket(t.id)}
                          >
                            Abrir
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}

      {/* Detalhe do chamado */}
      {currentId !== null && (
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div>
              <h2>{detail ? `Chamado #${detail.id}` : `Chamado #${currentId}`}</h2>
              {detail && (
                <span className={`sz-badge ${statusBadgeClass(detail.status)}`}>
                  {STATUS_LABEL[detail.status] || detail.status}
                </span>
              )}
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-secondary" onClick={back}>
                ← Voltar
              </button>
              {!isClosed && (
                <button
                  type="button"
                  className="szv2-btn szv2-btn-sm szv2-btn-danger"
                  onClick={closeTicket}
                  disabled={closing}
                >
                  {closing ? 'Fechando…' : 'Fechar chamado'}
                </button>
              )}
            </div>
          </div>

          <div className="szv2-ticket-msgs">
            {loadingDetail ? (
              <p className="szv2-empty-inline">Carregando…</p>
            ) : msgs.length === 0 ? (
              <p className="szv2-empty-inline">Escreva sua resposta abaixo para iniciar a conversa.</p>
            ) : (
              msgs.map(m => {
                const isClient = m.autor_tipo === 'cliente'
                return (
                  <div key={m.id} className={`szv2-ticket-msg szv2-ticket-msg-${isClient ? 'client' : 'staff'}`}>
                    <span className="szv2-ticket-msg-author">{m.autor_nome || m.autor_tipo}</span>
                    <p className="szv2-ticket-msg-text">{m.mensagem}</p>
                    <span className="szv2-ticket-msg-date">{fmtDateTime(m.created_at)}</span>
                  </div>
                )
              })
            )}
          </div>

          {/* Área de resposta — escondida quando o chamado está fechado */}
          {!isClosed && (
            <div className="szv2-ticket-reply-row">
              <textarea
                className="szv2-input"
                placeholder="Escreva sua resposta…"
                rows={3}
                value={reply}
                onChange={e => setReply(e.target.value)}
                style={{ flex: 1, resize: 'vertical' }}
              />
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                onClick={sendReply}
                disabled={sending}
                style={{ alignSelf: 'flex-end' }}
              >
                {sending ? 'Enviando…' : 'Enviar'}
              </button>
            </div>
          )}
        </div>
      )}

      {/* Modal: novo chamado */}
      {modalOpen && (
        <div className="szv2-modal-overlay szv2-open" role="dialog" aria-modal="true">
          <div className="szv2-modal">
            <div className="szv2-modal-head">
              <h3>Novo chamado</h3>
              <button type="button" className="szv2-modal-x" onClick={() => setModalOpen(false)} aria-label="Fechar">
                ×
              </button>
            </div>
            <form onSubmit={createTicket}>
              <div className="szv2-modal-body">
                <div className="szv2-input-group">
                  <label className="szv2-label">Assunto</label>
                  <input
                    type="text"
                    className="szv2-input"
                    placeholder="Descreva brevemente o problema"
                    value={assunto}
                    onChange={e => setAssunto(e.target.value)}
                  />
                </div>
                <div className="szv2-input-group">
                  <label className="szv2-label">Categoria</label>
                  <FalkSelect
                    aria-label="Categoria"
                    value={categoria}
                    onChange={v => setCategoria(v)}
                    options={CATEGORIAS.map(c => ({ value: c.value, label: c.label }))}
                  />
                </div>
                <div className="szv2-input-group">
                  <label className="szv2-label">Mensagem</label>
                  <textarea
                    className="szv2-input"
                    rows={4}
                    placeholder="Descreva seu problema em detalhes…"
                    value={mensagem}
                    onChange={e => setMensagem(e.target.value)}
                  />
                </div>
                {createErr && (
                  <div style={{ color: 'var(--szv2-danger)', fontSize: 13 }}>{createErr}</div>
                )}
              </div>
              <div className="szv2-modal-foot">
                <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setModalOpen(false)}>
                  Cancelar
                </button>
                <button type="submit" className="szv2-btn szv2-btn-brand" disabled={creating}>
                  {creating ? 'Abrindo…' : 'Abrir chamado'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </section>
  )
}
