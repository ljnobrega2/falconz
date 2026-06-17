// OrderDetail — tela consolidada de um pedido para o painel admin.
// Espelha 3 metaboxes do plugin WordPress legado em uma única página:
//   1) Motoboy COD            (includes/motoboy/order-metabox.php)
//   2) Resumo afiliado        (includes/senderzz-affiliates.php:5636)
//   3) Etiqueta Melhor Envio  (src/Admin/Orders/Metabox.php)
//
// Rota: /orders/:id  →  o id é sz_orders.id (não wp_order_id).
// Backend: GET /orders/{id}                       (payload consolidado)
//          GET /orders/{id}/notes                 (lista anotações)
//          POST /orders/{id}/force-motoboy-status (admin força transição)
//          POST /orders/{id}/note                 (anotação interna)
//          POST /labels/{label_id}/cancel         (cancela etiqueta ME)

import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'

// ── Tipos ─────────────────────────────────────────────────────────────────────

type OrderCustomer = { nome: string; email: string; telefone: string; cpf: string }
type OrderAddress = {
  endereco: string; numero: string; complemento: string
  bairro: string; cidade: string; uf: string; cep: string
}
type OrderItem = { produto: string; qty: number; preco_unit: number; subtotal: number }
type OrderTotals = { subtotal: number; shipping: number; discount: number; total: number }

type OrderHead = {
  id: number
  wc_order_id: number | null
  status: string
  created_at: string
  customer: OrderCustomer
  address: OrderAddress
  items: OrderItem[]
  totals: OrderTotals
}

type Comprovante = {
  id: number
  tipo_pgto: string
  foto_url: string
  baixa_por: string
  created_at: string
}

type MotoboyAudit = {
  id: number
  actor_tipo: string
  actor_id: number | null
  acao: string
  de_status: string | null
  para_status: string | null
  meta_json: unknown
  created_at: string
}

type MotoboySection = {
  exists: boolean
  motoboy_id: number | null
  motoboy_nome: string
  cd_nome: string
  zona_nome: string
  status: string
  dest_produto: string
  valor_pedido: number
  pgto_dinheiro: number
  pgto_pix: number
  pgto_cartao: number
  recebedor_nome: string
  recebedor_tipo: string
  recebedor_cpf: string
  baixa_por: string
  baixa_admin_user_id: number | null
  baixa_motoboy_id: number | null
  baixa_at: string | null
  entrega_lat: number | null
  entrega_lng: number | null
  frustrado_motivo: string
  frustrado_observacao: string
  repasse_confirmado: boolean
  repasse_ts: string | null
  comprovantes: Comprovante[]
  audit: MotoboyAudit[]
}

type AffiliateSection = {
  exists: boolean
  affiliate_id: number | null
  affiliate_nome: string
  affiliate_email: string
  commission_pct: number
  commission_amount: number
  status_transacao: string
  producer_id: number | null
  producer_nome: string
}

type LabelSection = {
  exists: boolean
  label_id: number | null
  invoice_id: string | null
  custom_service_id: string | null
  item_id: string | null
  me_order_id: string | null
  print_url: string | null
  pdf_local_url: string | null
  status: string
  error: string | null
  generated_at: string | null
  bought_shipping: string | null
  service_name: string | null
  tracking_code: string | null
  reverse: { item_id: string | null; order_id: string | null }
}

type OrderNote = {
  id: number
  note: string
  author_id: number | null
  author_nome: string
  created_at: string
}

type OrderDetailPayload = {
  order: OrderHead
  motoboy: MotoboySection
  affiliate: AffiliateSection
  label: LabelSection
}

// ── Helpers ───────────────────────────────────────────────────────────────────

const fmtBRL = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtDateTime = (s: string | null | undefined) => {
  if (!s) return '—'
  // ISO ou "YYYY-MM-DD HH:mm:ss" → "DD/MM/YYYY HH:mm"
  const isoLike = s.replace(' ', 'T')
  const d = new Date(isoLike)
  if (Number.isNaN(d.getTime())) return s
  return d.toLocaleString('pt-BR', {
    day: '2-digit', month: '2-digit', year: 'numeric',
    hour: '2-digit', minute: '2-digit',
  })
}

const STATUS_BADGE: Record<string, string> = {
  pending: 'szv2-badge-warning',
  processing: 'szv2-badge-info',
  aguardando: 'szv2-badge-warning',
  'on-hold': 'szv2-badge-warning',
  em_separacao: 'szv2-badge-info',
  embalado: 'szv2-badge-info',
  enviado: 'szv2-badge-brand',
  entregue: 'szv2-badge-success',
  completo: 'szv2-badge-success',
  cancelled: 'szv2-badge-neutral',
  cancelado: 'szv2-badge-neutral',
  frustrado: 'szv2-badge-danger',
  reembolsado: 'szv2-badge-neutral',
  agendado: 'szv2-badge-warning',
  em_rota: 'szv2-badge-brand',
}

const MB_STATUS_BADGE: Record<string, string> = {
  agendado: 'szv2-badge-warning',
  embalado: 'szv2-badge-info',
  em_rota: 'szv2-badge-brand',
  entregue: 'szv2-badge-success',
  frustrado: 'szv2-badge-danger',
  cancelado: 'szv2-badge-neutral',
}

const AFF_STATUS_BADGE: Record<string, string> = {
  pending: 'szv2-badge-warning',
  available: 'szv2-badge-info',
  paid: 'szv2-badge-success',
  cancelled: 'szv2-badge-neutral',
}

const COMP_ICON: Record<string, string> = {
  dinheiro: '💵', pix: '📱', cartao: '💳',
}

function StatusBadge({ status, map }: { status: string; map?: Record<string, string> }) {
  const cls = (map ?? STATUS_BADGE)[status] || 'szv2-badge-neutral'
  return <span className={`sz-badge ${cls}`}>{(status || '—').replace(/_/g, ' ')}</span>
}

// ── Página ────────────────────────────────────────────────────────────────────

const FORCE_STATUS_BUTTONS: { target: string; label: string; cls: string; style?: CSSProperties }[] = [
  { target: 'agendado',  label: 'Definir Agendado', cls: 'szv2-btn-brand' },
  { target: 'embalado',  label: 'Marcar Embalado',  cls: 'szv2-btn-secondary', style: { background: '#111827', color: '#fff', borderColor: 'transparent' } },
  { target: 'entregue',  label: 'Marcar Entregue',  cls: 'szv2-btn-secondary', style: { background: '#16a34a', color: '#fff', borderColor: 'transparent' } },
  { target: 'frustrado', label: 'Marcar Frustrado', cls: 'szv2-btn-secondary', style: { background: '#dc2626', color: '#fff', borderColor: 'transparent' } },
  { target: 'cancelado', label: 'Cancelar COD',     cls: 'szv2-btn-secondary', style: { background: '#dc2626', color: '#fff', borderColor: 'transparent' } },
]

// ── ConfirmDialog — modal de confirmação com CSS do layout.css (szv2-confirm-*) ──
type ConfirmDialogProps = {
  open: boolean
  title: string
  message: string
  btnLabel?: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}
function ConfirmDialog({ open, title, message, btnLabel = 'Confirmar', danger = false, onConfirm, onCancel }: ConfirmDialogProps) {
  const overlayRef = useRef<HTMLDivElement>(null)
  if (!open) return null
  return (
    <div
      className="szv2-confirm-overlay"
      ref={overlayRef}
      onClick={e => { if (e.target === overlayRef.current) onCancel() }}
    >
      <div className="szv2-confirm-box">
        <div className="szv2-confirm-icon">
          <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
            <path d="M12 2a10 10 0 100 20A10 10 0 0012 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z"/>
          </svg>
        </div>
        <div className="szv2-confirm-title">{title}</div>
        <div className="szv2-confirm-msg">{message}</div>
        <div className="szv2-confirm-actions">
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={onCancel}>Cancelar</button>
          <button
            type="button"
            className={`szv2-btn ${danger ? 'szv2-btn-danger' : 'szv2-btn-brand'}`}
            onClick={onConfirm}
          >
            {btnLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

export default function OrderDetail() {
  const { id = '' } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const [data, setData] = useState<OrderDetailPayload | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [note, setNote] = useState('')
  const [notes, setNotes] = useState<OrderNote[]>([])
  const [notesLoading, setNotesLoading] = useState(false)

  // Modal de confirmação — força status motoboy
  const [confirmForce, setConfirmForce] = useState<{ target: string; label: string } | null>(null)
  // Modal de confirmação — cancelar etiqueta
  const [confirmCancel, setConfirmCancel] = useState(false)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<OrderDetailPayload>(`/orders/${encodeURIComponent(id)}`)
      setData(r)
    } catch (e: any) {
      setErr(e?.message || 'Falha ao carregar pedido')
    } finally {
      setLoading(false)
    }
  }

  async function loadNotes() {
    setNotesLoading(true)
    try {
      const r = await api<{ notes: OrderNote[]; total: number; migrated: boolean }>(
        `/orders/${encodeURIComponent(id)}/notes`,
      )
      setNotes(r.notes ?? [])
    } catch {
      // graceful — tabela pode não existir ainda
    } finally {
      setNotesLoading(false)
    }
  }

  useEffect(() => {
    if (id) {
      load()
      loadNotes()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    window.setTimeout(() => setToast(null), 5000)
  }

  async function doForceStatus(target: string) {
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; new_status: string }>(
        `/orders/${encodeURIComponent(id)}/force-motoboy-status`,
        { method: 'POST', body: JSON.stringify({ target_status: target }) },
      )
      showToast('ok', `Status alterado para "${r.new_status}".`)
      await load()
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao alterar status')
    } finally {
      setBusy(false)
    }
  }

  function handleForceStatus(target: string) {
    const labelMap: Record<string, string> = {
      agendado: 'Agendado', embalado: 'Embalado',
      entregue: 'Entregue', frustrado: 'Frustrado', cancelado: 'Cancelado',
    }
    setConfirmForce({ target, label: labelMap[target] || target })
  }

  async function handleCancelLabel(labelId: number) {
    setBusy(true)
    try {
      await api(`/labels/${labelId}/cancel`, { method: 'POST' })
      showToast('ok', 'Cancelamento de etiqueta solicitado.')
      await load()
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao cancelar etiqueta')
    } finally {
      setBusy(false)
    }
  }

  async function handleAddNote() {
    if (!note.trim()) return
    setBusy(true)
    try {
      await api(`/orders/${encodeURIComponent(id)}/note`, {
        method: 'POST',
        body: JSON.stringify({ note: note.trim() }),
      })
      showToast('ok', 'Anotação registrada.')
      setNote('')
      await loadNotes()
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao registrar anotação')
    } finally {
      setBusy(false)
    }
  }

  if (loading) {
    return (
      <div className="szv2-card" style={{ padding: 48, textAlign: 'center' }}>
        <span style={{ color: 'var(--szv2-text-muted)' }}>Carregando pedido…</span>
      </div>
    )
  }

  if (err || !data) {
    return (
      <div>
        <div className="szv2-section-head">
          <div>
            <h1>Pedido</h1>
            <p>Erro ao carregar</p>
          </div>
          <button className="szv2-btn-secondary" onClick={() => navigate(-1)}>Voltar</button>
        </div>
        {err && <div className="sz-alert-danger">{err}</div>}
      </div>
    )
  }

  const { order, motoboy, affiliate, label } = data

  return (
    <div>
      {/* ── Modais de confirmação ─────────────────────────────────── */}
      <ConfirmDialog
        open={confirmForce !== null}
        title={`Forçar status: ${confirmForce?.label ?? ''}`}
        message="Ação será registrada em auditoria e não pode ser desfeita automaticamente."
        btnLabel="Confirmar"
        onConfirm={() => {
          const t = confirmForce
          setConfirmForce(null)
          if (t) doForceStatus(t.target)
        }}
        onCancel={() => setConfirmForce(null)}
      />
      <ConfirmDialog
        open={confirmCancel}
        title="Cancelar etiqueta Melhor Envio"
        message="Solicita cancelamento da etiqueta. O worker processa o cancelamento na API ME e atualiza para 'cancelada'."
        btnLabel="Cancelar etiqueta"
        danger
        onConfirm={() => {
          const lid = label.label_id
          setConfirmCancel(false)
          if (lid) handleCancelLabel(lid)
        }}
        onCancel={() => setConfirmCancel(false)}
      />

      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {/* ── Header ─────────────────────────────────────────────────── */}
      <div className="szv2-section-head">
        <div>
          <h1>
            Pedido #{order.id}
            {order.wc_order_id ? (
              <span style={{ marginLeft: 12, fontSize: 14, color: 'var(--szv2-text-muted)', fontWeight: 500 }}>
                WC #{order.wc_order_id}
              </span>
            ) : null}
          </h1>
          <p>
            <StatusBadge status={order.status} />
            <span style={{ marginLeft: 12, color: 'var(--szv2-text-muted)' }}>
              criado em {fmtDateTime(order.created_at)}
            </span>
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="szv2-btn-secondary" onClick={() => load()} disabled={busy}>Atualizar</button>
          <button className="szv2-btn-secondary" onClick={() => navigate(-1)}>Voltar</button>
        </div>
      </div>

      {/* ── Painel Motoboy COD (laranja) ───────────────────────────── */}
      {motoboy.exists && (
        <div
          className="szv2-card"
          style={{
            background: '#fff7ed',
            border: '1px solid #fed7aa',
            marginBottom: 16,
          }}
        >
          <div className="szv2-card-head">
            <div>
              <h2>🛵 Operação Motoboy COD</h2>
              <p className="szv2-card-sub">Use para colocar o pedido na fila COD. Em rota é exclusivo do motoboy lendo QR via PWA.</p>
            </div>
            <div style={{ textAlign: 'right' }}>
              <div style={{ fontSize: 11, color: '#9a3412', fontWeight: 700, letterSpacing: '.04em' }}>STATUS ATUAL</div>
              <div style={{ marginTop: 4 }}>
                <StatusBadge status={motoboy.status || '—'} map={MB_STATUS_BADGE} />
              </div>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 12 }}>
            {FORCE_STATUS_BUTTONS.map(b => (
              <button
                key={b.target}
                type="button"
                className={b.cls}
                style={b.style}
                disabled={busy}
                onClick={() => handleForceStatus(b.target)}
              >
                {b.label}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* ── Customer ───────────────────────────────────────────────── */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>👤 Cliente</h2>
            <p className="szv2-card-sub">Dados do destinatário</p>
          </div>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12, marginTop: 8 }}>
          <KV label="Nome"     value={order.customer.nome} />
          <KV label="E-mail"   value={order.customer.email} mono />
          <KV label="Telefone" value={order.customer.telefone} mono />
          <KV label="CPF"      value={order.customer.cpf} mono />
        </div>
        <div style={{ marginTop: 16, paddingTop: 12, borderTop: '1px solid var(--szv2-divider)' }}>
          <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>📍 Endereço de entrega</div>
          <div style={{ fontSize: 14 }}>
            {addrLine(order.address) || <span style={{ color: 'var(--szv2-text-muted)' }}>Sem dados</span>}
          </div>
        </div>
      </div>

      {/* ── Itens ──────────────────────────────────────────────────── */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>📦 Itens do pedido</h2>
            <p className="szv2-card-sub">{order.items.length} item(ns)</p>
          </div>
        </div>
        {order.items.length === 0 ? (
          <div className="szv2-empty"><h3>Sem itens</h3><p>Pedido sem itens registrados em sz_order_items.</p></div>
        ) : (
          <div className="szv2-table-wrap" style={{ marginTop: 12 }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Produto</th>
                  <th style={{ textAlign: 'right', width: 80 }}>Qty</th>
                  <th style={{ textAlign: 'right', width: 140 }}>Preço unit</th>
                  <th style={{ textAlign: 'right', width: 140 }}>Subtotal</th>
                </tr>
              </thead>
              <tbody>
                {order.items.map((it, i) => (
                  <tr key={i}>
                    <td>{it.produto || '—'}</td>
                    <td className="szv2-td-num">{it.qty}</td>
                    <td className="szv2-td-num">R$ {fmtBRL(it.preco_unit)}</td>
                    <td className="szv2-td-num">R$ {fmtBRL(it.subtotal)}</td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr><td colSpan={3} style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>Subtotal</td><td className="szv2-td-num">R$ {fmtBRL(order.totals.subtotal)}</td></tr>
                <tr><td colSpan={3} style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>Frete</td><td className="szv2-td-num">R$ {fmtBRL(order.totals.shipping)}</td></tr>
                {order.totals.discount > 0 && (
                  <tr><td colSpan={3} style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>Desconto</td><td className="szv2-td-num">- R$ {fmtBRL(order.totals.discount)}</td></tr>
                )}
                <tr><td colSpan={3} style={{ textAlign: 'right', fontWeight: 700 }}>Total</td><td className="szv2-td-num" style={{ fontWeight: 700, fontSize: 16, color: 'var(--szv2-brand)' }}>R$ {fmtBRL(order.totals.total)}</td></tr>
              </tfoot>
            </table>
          </div>
        )}
      </div>

      {/* ── Motoboy detail ─────────────────────────────────────────── */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>🏍️ Motoboy — entrega</h2>
            <p className="szv2-card-sub">{motoboy.exists ? 'Dados operacionais COD' : 'Sem registro Motoboy para este pedido'}</p>
          </div>
        </div>
        {!motoboy.exists ? (
          <div className="szv2-empty"><h3>Sem dados</h3><p>Este pedido ainda não está na operação Motoboy COD.</p></div>
        ) : (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12 }}>
              <KV label="Motoboy"        value={motoboy.motoboy_nome || `#${motoboy.motoboy_id ?? '—'}`} />
              <KV label="CD"             value={motoboy.cd_nome} />
              <KV label="Zona"           value={motoboy.zona_nome} />
              <KV label="Status"         value={<StatusBadge status={motoboy.status || '—'} map={MB_STATUS_BADGE} />} />
              {motoboy.dest_produto ? (
                <KV label="Produto" value={
                  <span style={{ fontFamily: 'inherit', fontWeight: 600 }}>{motoboy.dest_produto}</span>
                } />
              ) : null}
              <KV label="Confirmação motoboy" value={
                motoboy.repasse_confirmado
                  ? <span className="sz-badge szv2-badge-success">✅ Confirmado{motoboy.repasse_ts ? ` em ${fmtDateTime(motoboy.repasse_ts)}` : ''}</span>
                  : motoboy.baixa_por === 'admin'
                    ? <span className="sz-badge szv2-badge-warning">⏳ Pendente</span>
                    : '—'
              } />
              <KV label="Baixa por"
                  value={
                    motoboy.baixa_por === 'admin'
                      ? <span className="sz-badge szv2-badge-info">👤 Admin{motoboy.baixa_admin_user_id ? ` #${motoboy.baixa_admin_user_id}` : ''}</span>
                      : motoboy.baixa_por === 'motoboy'
                        ? <span className="sz-badge szv2-badge-success">🛵 Motoboy</span>
                        : '—'
                  } />
              <KV label="Data/hora baixa" value={fmtDateTime(motoboy.baixa_at)} />
              <KV label="Recebedor"
                  value={
                    motoboy.recebedor_nome
                      ? <>
                          {motoboy.recebedor_nome}
                          {' '}
                          <span className="sz-badge szv2-badge-neutral">
                            {motoboy.recebedor_tipo === 'terceiro' ? '👥 Terceiro' : '👤 Cliente'}
                          </span>
                        </>
                      : '—'
                  } />
              <KV label="CPF recebedor"  value={motoboy.recebedor_cpf} mono />
              <KV label="Total recebido" value={
                <span style={{ color: 'var(--szv2-success)', fontWeight: 700 }}>
                  R$ {fmtBRL((motoboy.pgto_dinheiro || 0) + (motoboy.pgto_pix || 0) + (motoboy.pgto_cartao || 0))}
                </span>
              } />
              <KV label="└ Dinheiro" value={`R$ ${fmtBRL(motoboy.pgto_dinheiro)}`} />
              <KV label="└ PIX"      value={`R$ ${fmtBRL(motoboy.pgto_pix)}`} />
              <KV label="└ Cartão"   value={`R$ ${fmtBRL(motoboy.pgto_cartao)}`} />
              <KV label="GPS entrega"
                  value={
                    motoboy.entrega_lat != null && motoboy.entrega_lng != null
                      ? (
                        <a
                          href={`https://maps.google.com/?q=${motoboy.entrega_lat},${motoboy.entrega_lng}`}
                          target="_blank"
                          rel="noreferrer"
                          style={{ color: 'var(--szv2-brand)' }}
                        >
                          📍 Ver no mapa
                        </a>
                      )
                      : '—'
                  } />
            </div>

            {motoboy.status === 'frustrado' && (motoboy.frustrado_motivo || motoboy.frustrado_observacao) && (
              <div style={{ marginTop: 12, padding: 12, background: 'var(--szv2-danger-bg)', borderRadius: 8, color: 'var(--szv2-danger)' }}>
                <div style={{ fontWeight: 700, marginBottom: 4 }}>Frustração</div>
                {motoboy.frustrado_motivo && <div>Motivo: {motoboy.frustrado_motivo}</div>}
                {motoboy.frustrado_observacao && <div>Observação: {motoboy.frustrado_observacao}</div>}
              </div>
            )}
          </>
        )}
      </div>

      {/* ── Comprovantes ───────────────────────────────────────────── */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>📸 Comprovantes</h2>
            <p className="szv2-card-sub">{motoboy.comprovantes.length} foto(s)</p>
          </div>
        </div>
        {motoboy.comprovantes.length === 0 ? (
          <div className="szv2-empty"><h3>Sem comprovantes</h3><p>Nenhum comprovante de pagamento registrado.</p></div>
        ) : (
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(90px, 1fr))',
              gap: 8, marginTop: 12,
            }}
          >
            {motoboy.comprovantes.map(c => (
              <a
                key={c.id}
                href={c.foto_url}
                target="_blank"
                rel="noreferrer"
                style={{ position: 'relative', display: 'block' }}
                title={`${c.tipo_pgto} · ${c.baixa_por} · ${fmtDateTime(c.created_at)}`}
              >
                <img
                  src={c.foto_url}
                  alt={c.tipo_pgto}
                  style={{ width: 72, height: 72, objectFit: 'cover', borderRadius: 8, border: '2px solid var(--szv2-border)' }}
                />
                <span
                  style={{
                    position: 'absolute', top: -6, right: -6,
                    background: 'var(--szv2-brand)', color: 'var(--szv2-on-brand)',
                    borderRadius: 99, width: 22, height: 22,
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 12, fontWeight: 700,
                  }}
                >
                  {COMP_ICON[c.tipo_pgto] || '•'}
                </span>
              </a>
            ))}
          </div>
        )}
      </div>

      {/* ── Afiliado ───────────────────────────────────────────────── */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>🤝 Afiliado</h2>
            <p className="szv2-card-sub">{affiliate.exists ? 'Vínculo de afiliação e comissão' : 'Pedido sem afiliado'}</p>
          </div>
          {affiliate.exists && (
            <span className={`sz-badge ${AFF_STATUS_BADGE[affiliate.status_transacao] || 'szv2-badge-neutral'}`}>
              {affiliate.status_transacao || '—'}
            </span>
          )}
        </div>
        {!affiliate.exists ? (
          <div className="szv2-empty"><h3>Sem dados</h3><p>Nenhum afiliado vinculado a este pedido.</p></div>
        ) : (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12, marginTop: 8 }}>
            <KV label="Afiliado"          value={affiliate.affiliate_nome || `#${affiliate.affiliate_id ?? '—'}`} />
            <KV label="E-mail"            value={affiliate.affiliate_email} mono />
            <KV label="Produtor"          value={affiliate.producer_nome || `#${affiliate.producer_id ?? '—'}`} />
            <KV label="Comissão %"        value={`${affiliate.commission_pct.toLocaleString('pt-BR', { maximumFractionDigits: 2 })}%`} />
            <KV label="Comissão R$"       value={
              <span style={{ color: 'var(--szv2-brand)', fontWeight: 700 }}>
                R$ {fmtBRL(affiliate.commission_amount)}
              </span>
            } />
          </div>
        )}
      </div>

      {/* ── Etiqueta ME ────────────────────────────────────────────── */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>🏷️ Etiqueta Melhor Envio</h2>
            <p className="szv2-card-sub">{label.exists ? `Gerada em ${fmtDateTime(label.generated_at)}` : 'Sem etiqueta gerada'}</p>
          </div>
          {label.exists && <StatusBadge status={label.status || '—'} />}
        </div>
        {!label.exists ? (
          <div className="szv2-empty"><h3>Sem etiqueta</h3><p>Nenhuma etiqueta ME vinculada a este pedido.</p></div>
        ) : (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12, marginTop: 8 }}>
              <KV label="Invoice ID"        value={label.invoice_id || '—'} mono />
              <KV label="ME Order ID"       value={label.me_order_id || '—'} mono />
              <KV label="Item ID"           value={label.item_id || '—'} mono />
              <KV label="Custom Service ID" value={label.custom_service_id || '—'} mono />
              <KV label="Serviço"           value={label.service_name || '—'} />
              <KV label="Tracking"          value={
                label.tracking_code
                  ? <CopyText text={label.tracking_code} />
                  : '—'
              } />
              <KV label="Bought shipping"   value={label.bought_shipping || '—'} mono />
              {label.error && (
                <KV label="Erro" value={<span style={{ color: 'var(--szv2-danger)' }}>{label.error}</span>} />
              )}
              {(label.reverse.item_id || label.reverse.order_id) && (
                <KV label="Reversa" value={
                  <span style={{ fontSize: 12 }}>
                    {label.reverse.item_id && <>item: <code>{label.reverse.item_id}</code></>}
                    {label.reverse.order_id && <> · order: <code>{label.reverse.order_id}</code></>}
                  </span>
                } />
              )}
            </div>
            <div style={{ display: 'flex', gap: 8, marginTop: 16, flexWrap: 'wrap' }}>
              {label.print_url && (
                <a className="szv2-btn-brand" href={label.print_url} target="_blank" rel="noreferrer">
                  Imprimir PDF
                </a>
              )}
              {label.pdf_local_url && (
                <a className="szv2-btn-secondary" href={label.pdf_local_url} target="_blank" rel="noreferrer">
                  Baixar local
                </a>
              )}
              {label.label_id && !['cancel_requested','cancelled'].includes(label.status) && (
                <button
                  type="button"
                  className="szv2-btn-secondary"
                  style={{ color: 'var(--szv2-danger)', borderColor: 'var(--szv2-danger)' }}
                  disabled={busy}
                  onClick={() => setConfirmCancel(true)}
                >
                  Cancelar etiqueta
                </button>
              )}
              {label.status === 'cancel_requested' && (
                <span className="sz-badge szv2-badge-warning" style={{ alignSelf: 'center' }}>
                  Cancelamento solicitado
                </span>
              )}
              {label.status === 'cancelled' && (
                <span className="sz-badge szv2-badge-neutral" style={{ alignSelf: 'center' }}>
                  Etiqueta cancelada
                </span>
              )}
            </div>
          </>
        )}
      </div>

      {/* ── Auditoria ──────────────────────────────────────────────── */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>📜 Histórico de auditoria</h2>
            <p className="szv2-card-sub">{motoboy.audit.length} evento(s)</p>
          </div>
        </div>
        {motoboy.audit.length === 0 ? (
          <div className="szv2-empty"><h3>Sem eventos</h3><p>Este pedido ainda não gerou trilha de auditoria Motoboy.</p></div>
        ) : (
          <div className="szv2-table-wrap" style={{ marginTop: 12 }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th style={{ width: 160 }}>Quando</th>
                  <th style={{ width: 120 }}>Actor</th>
                  <th>Ação</th>
                  <th style={{ width: 220 }}>Transição</th>
                  <th>Meta</th>
                </tr>
              </thead>
              <tbody>
                {motoboy.audit.map(a => (
                  <tr key={a.id}>
                    <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)' }}>
                      {fmtDateTime(a.created_at)}
                    </td>
                    <td>
                      <span className={`sz-badge ${actorBadgeCls(a.actor_tipo)}`}>
                        {a.actor_tipo || '—'}
                      </span>
                      {a.actor_id ? <span style={{ marginLeft: 4, fontSize: 11, color: 'var(--szv2-text-muted)' }}>#{a.actor_id}</span> : null}
                    </td>
                    <td>{a.acao || '—'}</td>
                    <td>
                      {a.de_status || a.para_status ? (
                        <>
                          {a.de_status ? <StatusBadge status={a.de_status} map={MB_STATUS_BADGE} /> : <span style={{ color: 'var(--szv2-text-muted)' }}>—</span>}
                          <span style={{ margin: '0 6px', color: 'var(--szv2-text-muted)' }}>→</span>
                          {a.para_status ? <StatusBadge status={a.para_status} map={MB_STATUS_BADGE} /> : <span style={{ color: 'var(--szv2-text-muted)' }}>—</span>}
                        </>
                      ) : '—'}
                    </td>
                    <td style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)' }}>
                      <MetaInline meta={a.meta_json} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* ── Anotação interna ──────────────────────────────────────── */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>📝 Anotações internas</h2>
            <p className="szv2-card-sub">
              {notes.length > 0 ? `${notes.length} anotação(ões) · visível apenas para o time admin` : 'Visível apenas para o time admin'}
            </p>
          </div>
        </div>

        {/* Histórico de notas */}
        {notesLoading ? (
          <div style={{ padding: '12px 0', color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando notas…</div>
        ) : notes.length > 0 ? (
          <div style={{ marginTop: 12, display: 'flex', flexDirection: 'column', gap: 8 }}>
            {notes.map(n => (
              <div
                key={n.id}
                style={{
                  padding: '10px 14px',
                  background: 'var(--szv2-bg, #f9fafb)',
                  borderRadius: 8,
                  border: '1px solid var(--szv2-border)',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
                  <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--szv2-text-muted)' }}>
                    {n.author_nome || (n.author_id ? `Admin #${n.author_id}` : 'Sistema')}
                  </span>
                  <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)' }}>
                    {fmtDateTime(n.created_at)}
                  </span>
                </div>
                <div style={{ fontSize: 14, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{n.note}</div>
              </div>
            ))}
          </div>
        ) : null}

        {/* Nova anotação */}
        <textarea
          value={note}
          onChange={e => setNote(e.target.value)}
          placeholder="Escreva uma observação sobre o pedido..."
          rows={3}
          style={{
            width: '100%', marginTop: 12, padding: 10,
            border: '1px solid var(--szv2-border)', borderRadius: 8,
            font: 'inherit', resize: 'vertical',
          }}
          disabled={busy}
        />
        <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 8 }}>
          <button
            type="button"
            className="szv2-btn-brand"
            onClick={handleAddNote}
            disabled={busy || note.trim().length === 0}
          >
            Salvar anotação
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Subcomponentes ────────────────────────────────────────────────────────────

function KV({ label, value, mono }: { label: string; value: ReactNode; mono?: boolean }) {
  const v = value == null || value === '' ? <span style={{ color: 'var(--szv2-text-muted)' }}>—</span> : value
  return (
    <div>
      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', textTransform: 'uppercase', letterSpacing: '.04em', marginBottom: 2 }}>
        {label}
      </div>
      <div style={{ fontSize: 14, fontWeight: 600, fontFamily: mono ? 'var(--szv2-font-mono)' : undefined }}>
        {v}
      </div>
    </div>
  )
}

function CopyText({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      style={{
        background: 'transparent', border: 'none', padding: 0, cursor: 'pointer',
        color: 'var(--szv2-brand)', fontFamily: 'var(--szv2-font-mono)',
        fontSize: 13, fontWeight: 600,
      }}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1500)
        } catch { /* ignore */ }
      }}
      title="Copiar"
    >
      {copied ? '✓ copiado' : text}
    </button>
  )
}

function MetaInline({ meta }: { meta: unknown }) {
  if (meta == null) return <span>—</span>
  if (typeof meta === 'string') {
    if (!meta || meta === 'null') return <span>—</span>
    try {
      meta = JSON.parse(meta)
    } catch {
      return <span>{String(meta)}</span>
    }
  }
  if (typeof meta !== 'object') return <span>{String(meta)}</span>
  const entries = Object.entries(meta as Record<string, unknown>)
  if (entries.length === 0) return <span>—</span>
  return (
    <span>
      {entries.map(([k, v], i) => (
        <span key={k}>
          {i > 0 && ' · '}
          <span style={{ color: 'var(--szv2-text-muted)' }}>{k}=</span>
          <span>{typeof v === 'object' ? JSON.stringify(v) : String(v)}</span>
        </span>
      ))}
    </span>
  )
}

function addrLine(a: OrderAddress) {
  const parts = [
    a.endereco,
    a.numero ? `${a.numero}` : '',
    a.complemento ? `(${a.complemento})` : '',
    a.bairro,
    a.cidade && a.uf ? `${a.cidade}/${a.uf}` : a.cidade || a.uf,
    a.cep ? `CEP ${a.cep}` : '',
  ].filter(Boolean)
  return parts.join(', ')
}

function actorBadgeCls(t: string) {
  switch (t) {
    case 'admin':    return 'szv2-badge-info'
    case 'motoboy':  return 'szv2-badge-success'
    case 'sistema':  return 'szv2-badge-neutral'
    case 'alan':     return 'szv2-badge-brand'
    default:         return 'szv2-badge-neutral'
  }
}
