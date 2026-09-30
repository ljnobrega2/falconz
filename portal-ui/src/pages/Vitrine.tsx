// Vitrine de produtos — porte fiel de templates/portal/v2/sections/vitrine.php.
// Visível para produtores E afiliados. Catálogo GLOBAL (mesmo p/ os dois);
// só o overlay por card (aff_status / is_own / comm_*) é por usuário.
//
// Ligada ao go/portal (namespace /wp-json/senderzz/v1):
//   GET  /portal/vitrine                          — catálogo + CDs + overlay do usuário
//   POST /portal/vitrine/affiliate                — solicita afiliação (request_affiliation)
//   POST /portal/products/{id}/vitrine-description — salva descrição (só produtor dono)
//
// PONTOS CRÍTICOS (verificados contra vitrine.go):
//   - Envelope da resposta é TOP-LEVEL: {ok, products, cds, total} — NÃO {data:{...}}.
//     httpx.WriteOK só injeta ok=true no mesmo nível. Lemos r.products / r.cds.
//   - POST /affiliate responde 200 com {success:false, message} nos erros de negócio
//     (produtor inválido / próprio produtor / vínculo existente). api() só lança em
//     status != 2xx, então checamos r.success — não o catch. Sem confirm prévio
//     (o WP dispara direto); o resultado vira toast.
//   - Brand: o WP usava laranja #ea580c inline; aqui só var(--szv2-brand) (azul #1E6FF2).
//     Confirm = ConfirmDialog/toast, nunca window.confirm. Sem verde #22c55e.
import { useEffect, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import EmptyState from '../components/EmptyState'
import SectionLoading from '../components/SectionLoading'
import AlertError from '../components/AlertError'
import Drawer from '../components/Drawer'
import { brl } from '../utils/format'
import { sanitizeProductHtml } from '../utils/sanitize'
import type { PortalMe } from '../components/Layout'

// ── Shapes (espelham go/portal/internal/handlers/vitrine.go) ────────────────────

type ProductCheckout = {
  id: number
  name: string
  price_label: string
  display_value: number
  tipo: string
  url: string
  slug: string
  affiliate_visible: boolean
  affiliate_commission_pct: number
}

type VitrineCD = {
  id: number
  nome: string
  cidade: string
  uf: string
}

type VitrineCard = {
  pid: number
  wp_post_id: number | null
  name: string
  description: string
  description_raw: string
  image: string
  qty_sold: number
  revenue: number
  comm_paid: number
  comm_pct: number
  my_comm_pct: number
  comm_max: number
  producer_id: number
  is_own: boolean
  aff_status: 'active' | 'pending' | null
  links: ProductCheckout[]
  cds: VitrineCD[]
}

// Envelope TOP-LEVEL (não {data:{...}}) — ver doc no topo.
type ListResp = {
  ok: boolean
  products: VitrineCard[]
  cds: VitrineCD[]
  total: number
}

type AffiliateResp = { ok: boolean; success: boolean; message: string }

type ModalStep = 0 | 1 | 2

// Decodifica \uXXXX / uXXXX literais que podem vir do banco (espelha _szDecodeU do WP).
function decodeU(s: string): string {
  return (s || '').replace(/u([0-9a-fA-F]{4})/g, (_m, h, idx, full) => {
    const prev = full.charAt(idx - 1)
    return /[0-9a-fA-F]/i.test(prev) ? `u${h}` : String.fromCharCode(parseInt(h, 16))
  })
}

// ── Placeholder de imagem (4 quadrados) — espelha o SVG do WP (image === "") ────
function ImagePlaceholder() {
  return (
    <div
      style={{
        height: 160,
        background: 'var(--szv2-surface-alt)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <svg viewBox="0 0 20 20" style={{ width: 48, height: 48, fill: 'var(--szv2-text-faint)' }}>
        <path d="M4 4h5v5H4zM11 4h5v5h-5zM4 11h5v5H4zM11 11h5v5h-5z" />
      </svg>
    </div>
  )
}

export default function Vitrine() {
  const toast = useToast()
  // Role vem do Outlet do Layout (já carregou /portal/me) — sem fetch extra.
  const { me } = useOutletContext<{ me: PortalMe | null }>() ?? { me: null }
  const role = (me?.role || '').toLowerCase()
  // Afiliado só promove: a aba "Localidades" (CDs/cidades) é info logística de baixa
  // relevância p/ ele → escondida do modal (UX-AUDIT §2.3). Produtor mantém as 3 abas.
  const isAffiliate = role === 'affiliate' || role === 'afiliado' || role === 'afiliada'
  const modalTabs: [ModalStep, string][] = isAffiliate
    ? [[0, 'Informações'], [1, 'Ofertas']]
    : [[0, 'Informações'], [1, 'Ofertas'], [2, 'Localidades']]

  const [cards, setCards] = useState<VitrineCard[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Modal (3 telas). modalPid = card aberto (null = fechado).
  const [modalPid, setModalPid] = useState<number | null>(null)
  const [step, setStep] = useState<ModalStep>(0)

  // Edição inline da descrição (só produtor dono).
  const [editingDesc, setEditingDesc] = useState(false)
  const [descDraft, setDescDraft] = useState('')
  const [savingDesc, setSavingDesc] = useState(false)
  const [descMsg, setDescMsg] = useState<{ kind: 'info' | 'ok' | 'err'; text: string } | null>(null)

  // Afiliação em andamento (por producer_id) — desabilita o botão.
  const [affiliating, setAffiliating] = useState<number | null>(null)
  // Cancelamento de solicitação pendente em andamento (por producer_id). // #72
  const [cancelling, setCancelling] = useState<number | null>(null)

  function load() {
    setLoading(true)
    setErr('')
    // Envelope top-level: lemos r.products diretamente (NÃO r.data).
    api<ListResp>('/portal/vitrine')
      .then(r => setCards(r.products || []))
      .catch(e => setErr(e.message || 'Erro ao carregar a vitrine'))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const modalCard = modalPid != null ? cards.find(c => c.pid === modalPid) || null : null

  function openModal(card: VitrineCard) {
    setModalPid(card.pid)
    setStep(0)
    setEditingDesc(false)
    setDescDraft(card.description_raw || '')
    setDescMsg(null)
  }

  function closeModal() {
    setModalPid(null)
    setEditingDesc(false)
    setDescMsg(null)
  }

  // ── Afiliar-me (request_affiliation) ──────────────────────────────────────────
  // POST responde 200 com success:false nos erros de negócio → checamos r.success.
  async function affiliate(card: VitrineCard) {
    if (!card.producer_id || card.is_own) return
    setAffiliating(card.producer_id)
    try {
      const r = await api<AffiliateResp>('/portal/vitrine/affiliate', {
        method: 'POST',
        body: JSON.stringify({ producer_id: card.producer_id }),
      })
      if (r.success) {
        // Marca todos os cards do mesmo produtor como pendentes (espelha o WP,
        // que atualiza todos os botões com aquele data-producer-id).
        setCards(prev =>
          prev.map(c => (c.producer_id === card.producer_id ? { ...c, aff_status: 'pending' as const } : c)),
        )
        toast('ok', r.message || 'Solicitação enviada. Aguarde aprovação do produtor.')
      } else {
        toast('err', r.message || 'Erro ao registrar afiliação.')
      }
    } catch (e: any) {
      toast('err', e.message || 'Erro ao registrar afiliação.')
    } finally {
      setAffiliating(null)
    }
  }

  // ── Cancelar solicitação de afiliação PENDENTE (#72) ──────────────────────────
  // Só faz sentido quando aff_status === 'pending'. POST responde 200 com
  // success:false nos casos de negócio (nada pendente) → checamos r.success.
  async function cancelAffiliation(card: VitrineCard) {
    if (!card.producer_id || card.aff_status !== 'pending') return
    setCancelling(card.producer_id)
    try {
      const r = await api<AffiliateResp>('/portal/vitrine/affiliate/cancel', {
        method: 'POST',
        body: JSON.stringify({ producer_id: card.producer_id }),
      })
      if (r.success) {
        // Limpa o overlay de TODOS os cards do mesmo produtor (afiliação é por
        // produtor — cobre todos os produtos dele de uma vez). // #72
        setCards(prev =>
          prev.map(c => (c.producer_id === card.producer_id ? { ...c, aff_status: null } : c)),
        )
        toast('ok', r.message || 'Solicitação cancelada.')
      } else {
        toast('err', r.message || 'Não foi possível cancelar a solicitação.')
      }
    } catch (e: any) {
      toast('err', e.message || 'Erro ao cancelar a solicitação.')
    } finally {
      setCancelling(null)
    }
  }

  // ── Salvar descrição da vitrine (só produtor dono) ────────────────────────────
  // URL usa card.pid (= sz_products.id, casa com o WHERE id= do handler).
  async function saveDesc(card: VitrineCard) {
    setSavingDesc(true)
    setDescMsg({ kind: 'info', text: 'Salvando…' })
    try {
      await api(`/portal/products/${card.pid}/vitrine-description`, {
        method: 'POST',
        body: JSON.stringify({ description: descDraft }),
      })
      setCards(prev =>
        prev.map(c => (c.pid === card.pid ? { ...c, description: descDraft, description_raw: descDraft } : c)),
      )
      setDescMsg({ kind: 'ok', text: '✓ Salvo' })
      toast('ok', 'Descrição salva com sucesso.')
      setTimeout(() => {
        setEditingDesc(false)
        setDescMsg(null)
      }, 800)
    } catch (e: any) {
      setDescMsg({ kind: 'err', text: e.message || 'Erro ao salvar.' })
    } finally {
      setSavingDesc(false)
    }
  }

  // Botão/estado de afiliação reutilizado no footer do modal (espelha _affBtn do WP).
  function renderAffControl(card: VitrineCard) {
    if (card.aff_status === 'active') {
      return (
        <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--szv2-brand)' }}>✓ Afiliado</span>
      )
    }
    if (card.aff_status === 'pending') {
      // #72 — pendente passa a ser CANCELÁVEL pelo próprio solicitante.
      return (
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Aguardando aprovação</span>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary szv2-btn-sm"
            disabled={cancelling === card.producer_id}
            onClick={() => cancelAffiliation(card)}
          >
            {cancelling === card.producer_id ? 'Cancelando…' : 'Cancelar solicitação'}
          </button>
        </div>
      )
    }
    if (!!card.producer_id && !card.is_own) {
      return (
        <button
          type="button"
          className="szv2-btn szv2-btn-brand"
          style={{ height: 38, padding: '0 20px', fontSize: 13 }}
          disabled={affiliating === card.producer_id}
          onClick={() => affiliate(card)}
        >
          {affiliating === card.producer_id ? 'Aguarde…' : 'Afiliar-me'}
        </button>
      )
    }
    return null
  }

  return (
    <section id="sec-vitrine" className="sz-sec" data-szv2-label="Vitrine" aria-busy={loading || undefined}>
      {/* Header vitrine — sem ícone */}
      <div style={{ marginBottom: 28 }}>
        <h2
          style={{
            margin: '0 0 4px',
            fontSize: 22,
            fontWeight: 800,
            letterSpacing: '-.3px',
            color: 'var(--szv2-text)',
          }}
        >
          Vitrine de produtos
        </h2>
        <p style={{ margin: 0, fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Explore produtos disponíveis, veja estatísticas e afilie-se com um clique.
        </p>
      </div>

      {!!err && <AlertError message={err} onRetry={load} />}

      {loading ? (
        <SectionLoading label="Carregando a vitrine…" />
      ) : cards.length === 0 ? (
        <EmptyState
          title="Nenhum produto encontrado"
          description="Produtos registrados na plataforma aparecerão aqui."
        />
      ) : (
        <div
          style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 20 }}
          id="szv2-vitrine-grid"
        >
          {cards.map(card => {
            const showAffiliarBtn = !card.aff_status && !!card.producer_id && !card.is_own
            return (
              <div
                key={card.pid}
                style={{
                  background: 'var(--szv2-surface)',
                  borderRadius: 16,
                  overflow: 'hidden',
                  boxShadow: '0 2px 12px rgba(0,0,0,.1)',
                  border: '1px solid var(--szv2-border)',
                  display: 'flex',
                  flexDirection: 'column',
                  transition: 'box-shadow .2s, transform .2s',
                }}
                onMouseEnter={e => {
                  e.currentTarget.style.boxShadow = '0 8px 32px rgba(0,0,0,.2)'
                  e.currentTarget.style.transform = 'translateY(-2px)'
                }}
                onMouseLeave={e => {
                  e.currentTarget.style.boxShadow = '0 2px 12px rgba(0,0,0,.1)'
                  e.currentTarget.style.transform = ''
                }}
              >
                {/* Imagem hero (ou placeholder) */}
                {card.image ? (
                  <div
                    style={{
                      height: 210,
                      overflow: 'hidden',
                      background: 'var(--szv2-surface-alt)',
                      flexShrink: 0,
                      position: 'relative',
                    }}
                  >
                    <img
                      src={card.image}
                      alt={card.name}
                      style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
                    />
                    {card.aff_status === 'active' && (
                      <span
                        style={{
                          position: 'absolute',
                          top: 10,
                          right: 10,
                          background: 'var(--szv2-brand)',
                          color: '#fff',
                          fontSize: 10,
                          fontWeight: 800,
                          padding: '3px 9px',
                          borderRadius: 99,
                          letterSpacing: '.04em',
                        }}
                      >
                        ✓ AFILIADO
                      </span>
                    )}
                    {card.aff_status === 'pending' && (
                      <span
                        style={{
                          position: 'absolute',
                          top: 10,
                          right: 10,
                          background: 'rgba(0,0,0,.55)',
                          color: '#fff',
                          fontSize: 10,
                          fontWeight: 700,
                          padding: '3px 9px',
                          borderRadius: 99,
                        }}
                      >
                        PENDENTE
                      </span>
                    )}
                  </div>
                ) : (
                  <ImagePlaceholder />
                )}

                {/* Info */}
                <div style={{ padding: '16px 18px 18px', flex: 1, display: 'flex', flexDirection: 'column' }}>
                  <h3
                    style={{
                      fontSize: 15,
                      fontWeight: 800,
                      margin: '0 0 10px',
                      color: 'var(--szv2-text)',
                      lineHeight: 1.3,
                      letterSpacing: '-.2px',
                    }}
                  >
                    {card.name}
                  </h3>

                  {/* Headline: valor que o afiliado ganha — elemento PRINCIPAL (paridade WP) */}
                  <div
                    style={{
                      background: 'rgba(30,111,242,.10)',
                      border: '1px solid rgba(30,111,242,.20)',
                      borderRadius: 12,
                      padding: '12px 14px',
                      marginBottom: 12,
                    }}
                  >
                    <div
                      style={{
                        fontSize: 11,
                        fontWeight: 700,
                        color: 'var(--szv2-brand)',
                        textTransform: 'uppercase',
                        letterSpacing: '.05em',
                        marginBottom: 2,
                      }}
                    >
                      Ganhe até
                    </div>
                    <div
                      style={{
                        fontSize: 28,
                        fontWeight: 800,
                        lineHeight: 1.05,
                        color: 'var(--szv2-brand)',
                        letterSpacing: '-.5px',
                      }}
                    >
                      {card.comm_max > 0 ? brl(card.comm_max) : `${card.comm_pct || 0}%`}
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontWeight: 500, marginTop: 1 }}>
                      por venda{card.comm_max > 0 && card.comm_pct ? ` · ${card.comm_pct}% de comissão` : ''}
                    </div>
                  </div>

                  {/* Stats: vendas + total distribuído */}
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 16,
                      marginBottom: 12,
                      padding: '10px 0',
                      borderBottom: '1px solid var(--szv2-divider)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'baseline', gap: 3 }}>
                      <span style={{ fontSize: 18, fontWeight: 800, color: 'var(--szv2-text)' }}>
                        {(card.qty_sold ?? 0).toLocaleString('pt-BR')}
                      </span>
                      <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontWeight: 500 }}>
                        vendas
                      </span>
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                      Total distribuído:{' '}
                      <strong
                        style={{
                          color: card.comm_paid > 0 ? 'var(--szv2-brand)' : 'var(--szv2-text-faint)',
                        }}
                      >
                        {card.comm_paid > 0 ? brl(card.comm_paid) : 'R$ 0,00'}
                      </strong>
                    </div>
                  </div>

                  {/* Ação */}
                  <div style={{ marginTop: 'auto', display: 'flex', gap: 8 }}>
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-secondary"
                      style={{ flex: 1, borderRadius: 10, padding: 10, fontSize: 13, fontWeight: 600 }}
                      onClick={() => openModal(card)}
                    >
                      Ver detalhes
                    </button>
                    {showAffiliarBtn && (
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-brand"
                        style={{ flex: 1, borderRadius: 10, padding: 10, fontSize: 13, fontWeight: 700 }}
                        disabled={affiliating === card.producer_id}
                        onClick={() => affiliate(card)}
                      >
                        {affiliating === card.producer_id ? 'Aguarde…' : 'Afiliar-me'}
                      </button>
                    )}
                    {/* #72 — pendente: cancelar a solicitação direto do card. */}
                    {card.aff_status === 'pending' && !card.is_own && (
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-secondary"
                        style={{ flex: 1, borderRadius: 10, padding: 10, fontSize: 13, fontWeight: 600 }}
                        disabled={cancelling === card.producer_id}
                        onClick={() => cancelAffiliation(card)}
                      >
                        {cancelling === card.producer_id ? 'Cancelando…' : 'Cancelar solicitação'}
                      </button>
                    )}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* ── Drawer lateral direito (3 telas: Produto / Ofertas / Localidades) ────── */}
      {/* Marca-registrada FALK: painel desliza da DIREITA (Drawer reutilizado),     */}
      {/* não modal central. ESC/clique-fora/X já tratados pelo Drawer.              */}
      <Drawer
        open={modalCard != null}
        onClose={closeModal}
        // Submenus NO TOPO (no lugar do nome do produto): o header do Drawer recebe as
        // tabs como título → "tira o nome, sobe os submenus". O nome já aparece no card.
        title={
          <div style={{ display: 'flex', gap: 22 }}>
            {modalTabs.map(([s, label]) => {
              const active = step === s
              return (
                <button
                  key={s}
                  type="button"
                  onClick={() => setStep(s)}
                  style={{
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    padding: '2px 0',
                    fontSize: 14,
                    fontWeight: 700,
                    color: active ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
                    borderBottom: `2px solid ${active ? 'var(--szv2-brand)' : 'transparent'}`,
                    transition: 'color .15s',
                  }}
                >
                  {label}
                </button>
              )
            })}
          </div>
        }
        width={520}
        ariaLabel={modalCard ? `Detalhes de ${modalCard.name}` : 'Detalhes do produto'}
      >
        {!!modalCard && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              // O corpo do Drawer já tem padding 18px; negamos com margin -18 p/ as
              // tabs/footer encostarem nas bordas. minHeight = altura do conteúdo
              // (100%) + os 36px do padding negado → footer "Afiliar-me" no rodapé.
              minHeight: 'calc(100% + 36px)',
              margin: -18,
            }}
          >
            {/* Tabs movidas pro header do Drawer (title) — corpo começa direto no conteúdo. */}

            {/* Conteúdo da aba — flex column p/ a aba Informações esticar o card até o
                rodapé do drawer (card preenche o tamanho do drawer; sem faixa branca). */}
            <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', padding: '20px 18px 24px' }}>
              {/* Tela "Informações": SÓ o card HTML do produtor (sem foto/KPIs) — decisão do
                  dono: o card estica completo. Foto/comissão/stats vivem nas outras abas. */}
              {step === 0 && (
                <>
                  {/* Gatilho "Editar descrição" (só dono, quando não editando) — acima da descrição */}
                  {modalCard.is_own && !editingDesc && (
                    <button
                      type="button"
                      onClick={() => {
                        setEditingDesc(true)
                        setDescDraft(modalCard.description_raw || '')
                        setDescMsg(null)
                      }}
                      style={{
                        background: 'none',
                        border: 'none',
                        fontSize: 12,
                        color: 'var(--szv2-brand)',
                        cursor: 'pointer',
                        padding: 0,
                        fontWeight: 600,
                        marginBottom: 12,
                        display: 'block',
                      }}
                    >
                      ✏ Editar descrição da vitrine
                    </button>
                  )}

                  {/* Form de edição inline (só dono) */}
                  {modalCard.is_own && editingDesc && (
                    <div style={{ marginBottom: 12 }}>
                      <textarea
                        value={descDraft}
                        onChange={e => setDescDraft(e.target.value)}
                        placeholder="Descreva o produto para os afiliados: benefícios, público-alvo, diferenciais..."
                        style={{
                          width: '100%',
                          boxSizing: 'border-box',
                          border: '1px solid var(--szv2-border)',
                          borderRadius: 8,
                          padding: '10px 12px',
                          fontSize: 13,
                          lineHeight: 1.5,
                          resize: 'vertical',
                          minHeight: 80,
                          fontFamily: 'inherit',
                          background: 'var(--szv2-surface)',
                          color: 'var(--szv2-text)',
                        }}
                      />
                      <div style={{ display: 'flex', gap: 8, marginTop: 6 }}>
                        <button
                          type="button"
                          className="szv2-btn szv2-btn-brand szv2-btn-sm"
                          disabled={savingDesc}
                          onClick={() => saveDesc(modalCard)}
                        >
                          {savingDesc ? 'Salvando…' : 'Salvar'}
                        </button>
                        <button
                          type="button"
                          className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                          onClick={() => {
                            setEditingDesc(false)
                            setDescMsg(null)
                          }}
                        >
                          Cancelar
                        </button>
                      </div>
                      {!!descMsg && (
                        <div
                          style={{
                            fontSize: 12,
                            marginTop: 4,
                            color:
                              descMsg.kind === 'ok'
                                ? 'var(--szv2-brand)'
                                : descMsg.kind === 'err'
                                ? 'var(--szv2-danger)'
                                : 'var(--szv2-text-muted)',
                          }}
                        >
                          {descMsg.text}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Descrição (abaixo do gatilho/form — espelha kpis + editBtn + desc do WP).
                      HTML rico do produtor (tags) → renderiza sanitizado, SEM o callout box
                      (o card traz fundo/padding próprios; box dobrado fica feio). Texto puro
                      → mantém o callout. sanitizeProductHtml é o ÚNICO portão de HTML. */}
                  {modalCard.description ? (
                    /<[a-z][\s\S]*>/i.test(modalCard.description) ? (
                      <div
                        // Confinamento: overflow+contain+isolation impedem o HTML do
                        // produtor de pintar/clicar FORA desta caixa (anti-clickjacking
                        // dos controles do modal). position:relative = containing block.
                        // Full-bleed: margem negativa cancela o padding do corpo.
                        // flex '1 0 auto' + bg dark = o card PREENCHE o tamanho do drawer
                        // (sem faixa branca embaixo) e, se o conteúdo for maior, cresce e
                        // rola — nunca é clipado (basis auto, shrink 0).
                        style={{
                          flex: '1 0 auto',
                          marginTop: modalCard.is_own ? 0 : -20,
                          marginInline: -18,
                          marginBottom: -24,
                          background: '#0d0d0d',
                          overflow: 'hidden',
                          isolation: 'isolate',
                          contain: 'paint',
                          position: 'relative',
                        }}
                        // eslint-disable-next-line react/no-danger
                        dangerouslySetInnerHTML={{ __html: sanitizeProductHtml(modalCard.description) }}
                      />
                    ) : (
                      <div
                        style={{
                          background: 'var(--szv2-surface-alt)',
                          borderLeft: '3px solid var(--szv2-brand)',
                          borderRadius: '0 8px 8px 0',
                          padding: '12px 14px',
                          fontSize: 13,
                          color: 'var(--szv2-text-soft)',
                          lineHeight: 1.6,
                          marginBottom: 12,
                          whiteSpace: 'pre-wrap',
                        }}
                      >
                        {modalCard.description}
                      </div>
                    )
                  ) : (
                    <p
                      style={{
                        fontSize: 13,
                        color: 'var(--szv2-text-muted)',
                        fontStyle: 'italic',
                        marginBottom: 12,
                      }}
                    >
                      Sem descrição cadastrada.
                    </p>
                  )}
                </>
              )}
              {step === 1 && <ModalOfertas card={modalCard} />}
              {/* Localidades só p/ não-afiliado (a aba some do header para o afiliado). */}
              {step === 2 && !isAffiliate && <ModalLocalidades card={modalCard} />}
            </div>

            {/* Footer do drawer: controle de afiliação (espelha _affBtn).            */}
            {/* sticky bottom:0 mantém o CTA "Afiliar-me" sempre visível ao rolar o   */}
            {/* corpo do Drawer (que é o único container de scroll — Drawer.tsx       */}
            {/* imutável aqui).                                                       */}
            {/* SÓ renderiza quando há ação de afiliação (não-dono com produtor). Pro */}
            {/* DONO renderAffControl é null → footer vazio aparecia como FAIXA BRANCA */}
            {/* (surface clara) sobre o card escuro. Sem conteúdo → sem footer.        */}
            {!modalCard.is_own && !!modalCard.producer_id && (
              <div
                style={{
                  padding: '12px 18px 16px',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                  flexShrink: 0,
                  borderTop: '1px solid var(--szv2-divider)',
                  minHeight: 52,
                  position: 'sticky',
                  bottom: 0,
                  background: 'var(--szv2-surface)',
                }}
              >
                {/* #72 — clareza: a afiliação é por PRODUTOR (cobre todos os produtos
                    dele). O dono pediu afiliação em 1 produto e o overlay marcou os 2
                    do mesmo produtor — comportamento correto, agora explicado na UI. */}
                {!modalCard.aff_status ? (
                  <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', lineHeight: 1.4, maxWidth: 280 }}>
                    Afiliar-se cobre <strong>todos os produtos</strong> deste produtor.
                  </span>
                ) : (
                  <span />
                )}
                {renderAffControl(modalCard)}
              </div>
            )}
          </div>
        )}
      </Drawer>
    </section>
  )
}

// ── Tela 0: Produto ─────────────────────────────────────────────────────────────
function ModalProduto({ card }: { card: VitrineCard }) {
  // X = comissão máxima em R$ por venda (melhor oferta). O backend já entrega
  // comm_max = maior(display_value * comm_pct/100). Fallback client-side: maior
  // valor de oferta * comm_pct/100 (mesma fórmula de ModalOfertas, comm_pct do
  // produtor — NUNCA affiliate_commission_pct).
  const pct = card.comm_pct || 0
  const bestPrice = (card.links || []).reduce((m, lk) => Math.max(m, lk.display_value || 0), 0)
  const commMax = card.comm_max > 0 ? card.comm_max : (bestPrice * pct) / 100
  const hasValue = commMax > 0

  return (
    <>
      {!!card.image && (
        <img
          src={card.image}
          alt={card.name}
          style={{
            width: '100%',
            height: 170,
            objectFit: 'cover',
            borderRadius: 10,
            marginBottom: 14,
            display: 'block',
          }}
        />
      )}

      {/* HERO: valor em destaque — "Ganhe até R$ X por venda" (28px bold, fundo
          brand tênue). % de comissão vira secundário (subtítulo). */}
      <div
        style={{
          background: 'var(--szv2-brand-light)',
          borderRadius: 12,
          padding: '16px 18px',
          textAlign: 'center',
          border: '1px solid var(--szv2-border)',
          marginBottom: 12,
        }}
      >
        <div
          style={{
            fontSize: 10,
            fontWeight: 700,
            color: 'var(--szv2-brand)',
            textTransform: 'uppercase',
            letterSpacing: '.06em',
            marginBottom: 4,
          }}
        >
          {hasValue ? 'Ganhe até' : 'Comissão'}
        </div>
        <div style={{ fontSize: 28, fontWeight: 800, color: 'var(--szv2-brand)', lineHeight: 1.1 }}>
          {hasValue ? brl(commMax) : `${pct}%`}
        </div>
        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--szv2-brand)', marginTop: 4 }}>
          {hasValue ? `por venda · ${pct}% de comissão` : 'por venda'}
        </div>
      </div>

      {/* KPIs secundários: Vendidos / Distribuído (% migrou pro hero acima) */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginBottom: 16 }}>
        <div style={{ background: 'var(--szv2-surface-alt)', borderRadius: 10, padding: 12, textAlign: 'center' }}>
          <div
            style={{
              fontSize: 9,
              fontWeight: 700,
              color: 'var(--szv2-text-muted)',
              textTransform: 'uppercase',
              letterSpacing: '.06em',
              marginBottom: 3,
            }}
          >
            Vendidos
          </div>
          <div style={{ fontSize: 20, fontWeight: 800, color: 'var(--szv2-text)' }}>{card.qty_sold || 0}</div>
        </div>
        <div style={{ background: 'var(--szv2-surface-alt)', borderRadius: 10, padding: 12, textAlign: 'center' }}>
          <div
            style={{
              fontSize: 9,
              fontWeight: 700,
              color: 'var(--szv2-text-muted)',
              textTransform: 'uppercase',
              letterSpacing: '.06em',
              marginBottom: 3,
            }}
          >
            Distribuído
          </div>
          <div style={{ fontSize: 14, fontWeight: 800, color: 'var(--szv2-text)' }}>
            {card.comm_paid > 0 ? brl(card.comm_paid) : '—'}
          </div>
        </div>
      </div>
    </>
  )
}

// ── Tela 1: Ofertas ─────────────────────────────────────────────────────────────
function ModalOfertas({ card }: { card: VitrineCard }) {
  const links = card.links || []
  // GATE DO LINK (#69): o link de checkout da oferta só é LIBERADO quando a afiliação
  // ao produtor está APROVADA (aff_status === 'active'). O produtor dono (is_own) vê
  // sempre os próprios links. Pendente/null → "Disponível após aprovação" (sem link).
  // O usuário SEMPRE vê valor + comissão; só o ABRIR fica travado até aprovar.
  const linkUnlocked = card.aff_status === 'active' || card.is_own
  if (links.length === 0) {
    return (
      <div style={{ padding: 32, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
        <div style={{ fontSize: 32, marginBottom: 8 }}>🛒</div>
        <p style={{ fontSize: 13, margin: 0 }}>Este produtor ainda não publicou ofertas. Quando publicar, os links de checkout aparecem aqui.</p>
      </div>
    )
  }
  return (
    <>
      {links.map(lk => {
        const nm = lk.name || 'Oferta'
        const price = lk.display_value || 0
        // PCT EFETIVO da oferta (#69), resolvido UMA vez e usado p/ R$ E (Y%) — nunca
        // par inconsistente. Precedência fiel às migrações 428/429:
        //   1. % própria da OFERTA (affiliate_commission_pct > 0 — comissão do checkout link)
        //   2. % do VÍNCULO ATIVO do próprio viewer (my_comm_pct)
        //   3. padrão do PRODUTOR (comm_pct) / global (fallback final do backend)
        const offerPct = lk.affiliate_commission_pct > 0
          ? lk.affiliate_commission_pct
          : card.my_comm_pct > 0
          ? card.my_comm_pct
          : card.comm_pct || 0
        const comm = price > 0 ? (price * offerPct) / 100 : 0
        const checkoutUrl = lk.url || lk.slug || ''
        return (
          <div
            key={lk.id}
            style={{
              border: '1px solid var(--szv2-border)',
              borderRadius: 10,
              padding: '14px 16px',
              marginBottom: 10,
              background: 'var(--szv2-surface-alt)',
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'flex-start',
                justifyContent: 'space-between',
                gap: 10,
                marginBottom: 10,
              }}
            >
              <span style={{ fontSize: 14, fontWeight: 700, color: 'var(--szv2-text)', lineHeight: 1.3 }}>
                {nm}
              </span>
              {!!checkoutUrl &&
                (linkUnlocked ? (
                  <a
                    href={checkoutUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--szv2-brand)',
                      whiteSpace: 'nowrap',
                      textDecoration: 'none',
                      padding: '3px 10px',
                      border: '1px solid var(--szv2-border)',
                      borderRadius: 99,
                      background: 'var(--szv2-brand-light)',
                      flexShrink: 0,
                    }}
                  >
                    Abrir ↗
                  </a>
                ) : (
                  // GATE (#69): link travado até aprovação — mostra estado, sem href.
                  <span
                    title="O link de checkout fica disponível após o produtor aprovar sua afiliação."
                    style={{
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--szv2-text-muted)',
                      whiteSpace: 'nowrap',
                      padding: '3px 10px',
                      border: '1px solid var(--szv2-border)',
                      borderRadius: 99,
                      background: 'var(--szv2-surface-alt)',
                      flexShrink: 0,
                      cursor: 'not-allowed',
                    }}
                  >
                    🔒 Disponível após aprovação
                  </span>
                ))}
            </div>
            <div style={{ display: 'flex', gap: 20 }}>
              <div>
                <div
                  style={{
                    fontSize: 9,
                    fontWeight: 700,
                    color: 'var(--szv2-text-muted)',
                    textTransform: 'uppercase',
                    letterSpacing: '.05em',
                    marginBottom: 2,
                  }}
                >
                  Valor da oferta
                </div>
                <div style={{ fontSize: 18, fontWeight: 800, color: 'var(--szv2-text)' }}>
                  {price > 0 ? brl(price) : '—'}
                </div>
              </div>
              <div>
                <div
                  style={{
                    fontSize: 9,
                    fontWeight: 700,
                    color: 'var(--szv2-text-muted)',
                    textTransform: 'uppercase',
                    letterSpacing: '.05em',
                    marginBottom: 2,
                  }}
                >
                  Sua comissão
                </div>
                <div style={{ fontSize: 18, fontWeight: 800, color: 'var(--szv2-brand)' }}>
                  {comm > 0 ? brl(comm) : `${offerPct}%`}
                </div>
                {/* #69: percentual ao lado da comissão R$ (mesma fonte efetiva). */}
                {comm > 0 && offerPct > 0 && (
                  <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--szv2-text-muted)', marginTop: 1 }}>
                    {offerPct}% de comissão
                  </div>
                )}
              </div>
            </div>
          </div>
        )
      })}
    </>
  )
}

// ── Tela 2: Localidades ─────────────────────────────────────────────────────────
function ModalLocalidades({ card }: { card: VitrineCard }) {
  const cds = card.cds || []
  if (cds.length === 0) {
    return (
      <div style={{ padding: 32, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
        <div style={{ fontSize: 32, marginBottom: 8 }}>📍</div>
        <p style={{ fontSize: 13, margin: 0 }}>Sem localidades de entrega definidas. Fale com o produtor para saber onde o produto é entregue.</p>
      </div>
    )
  }
  return (
    <>
      {cds.map(cd => {
        const nm = decodeU(cd.nome || '')
        // Pedido do dono: mostrar SÓ o nome (em negrito) + badge "Disponível".
        // A 2ª linha "cidade – uf" foi removida; `city`/`uf`/`sub` saíram junto.
        return (
          <div
            key={cd.id}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 12,
              padding: '12px 14px',
              border: '1px solid var(--szv2-border)',
              borderRadius: 10,
              marginBottom: 8,
              background: 'var(--szv2-surface-alt)',
            }}
          >
            <div
              style={{
                width: 36,
                height: 36,
                borderRadius: 8,
                background: 'var(--szv2-brand-light)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                flexShrink: 0,
              }}
            >
              <svg viewBox="0 0 20 20" style={{ width: 16, height: 16, fill: 'var(--szv2-brand)' }}>
                <path d="M10 2a5 5 0 0 0-5 5c0 3.5 5 11 5 11s5-7.5 5-11a5 5 0 0 0-5-5zm0 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4z" />
              </svg>
            </div>
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--szv2-text)' }}>{nm}</div>
            </div>
            <span
              style={{
                marginLeft: 'auto',
                flexShrink: 0,
                fontSize: 11,
                fontWeight: 700,
                color: 'var(--szv2-brand)',
                background: 'var(--szv2-brand-muted)',
                padding: '3px 10px',
                borderRadius: 99,
                border: '1px solid var(--szv2-brand-light)',
              }}
            >
              Disponível
            </span>
          </div>
        )
      })}
    </>
  )
}
