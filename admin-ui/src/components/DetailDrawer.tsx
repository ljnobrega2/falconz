/**
 * DetailDrawer — painel de detalhe deslizando da lateral DIREITA.
 *
 * Marca registrada do painel: TODO menu pós-clique abre lateralmente, nunca em
 * modal central. Substitui o uso de <Modal> (modal central, via createPortal no
 * document.body) em telas de detalhe/edição de um registro.
 *
 * Padrão de casca herdado de FilterDrawer / AffiliateDetailDrawer (in-tree,
 * position:fixed, estilos inline na casca, classes szv2-btn* nos botões). NÃO
 * usa createPortal — fica dentro de .sz-dashboard-v2 para que os tokens/escopos
 * (var(--szv2-*), .szv2-btn-brand) resolvam corretamente.
 *
 * Acentos AZUL FALK (var(--szv2-brand) = #1E6FF2). Nunca laranja.
 *
 * A11y: ESC fecha · role="dialog"/aria-modal · aria-labelledby no título ·
 * foco restaurado ao elemento que abriu · clique-fora e botão ✕ fecham.
 *
 * API drop-in compatível com <Modal> (open, onClose, title, children, footer?,
 * large?, width?) para facilitar a troca Modal → DetailDrawer sem mexer na lógica.
 */
import {
  type ReactNode,
  useEffect,
  useId,
  useRef,
} from 'react'

export interface DetailDrawerProps {
  open: boolean
  onClose: () => void
  /** Título do cabeçalho (aceita JSX, igual ao Modal) */
  title: ReactNode
  /** Conteúdo do corpo */
  children: ReactNode
  /** Botões do rodapé (normalmente szv2-btn*) */
  footer?: ReactNode
  /** Drawer largo para conteúdo com tabela (~640px). Equivale ao `large` do Modal. */
  large?: boolean
  /** Largura customizada em px (sobrescreve `large`). Default 520 / large 640. */
  width?: number
}

export default function DetailDrawer({
  open,
  onClose,
  title,
  children,
  footer,
  large,
  width,
}: DetailDrawerProps) {
  const previousFocusRef = useRef<HTMLElement | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const titleId = useId()

  // ESC fecha o painel.
  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') { e.stopPropagation(); onClose() }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [open, onClose])

  // Salva o foco ao abrir; move foco para dentro do painel; restaura ao fechar.
  useEffect(() => {
    if (open) {
      previousFocusRef.current = document.activeElement as HTMLElement
      requestAnimationFrame(() => {
        const first = panelRef.current?.querySelector<HTMLElement>(
          'a[href],button:not([disabled]),input:not([disabled]),' +
          'select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])',
        )
        first?.focus()
      })
    } else {
      previousFocusRef.current?.focus()
    }
  }, [open])

  const drawerWidth = width ?? (large ? 640 : 520)

  return (
    <>
      {/* Overlay escurecido — só renderiza aberto; clique fecha. */}
      {open && (
        <div
          onClick={onClose}
          aria-hidden="true"
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.3)',
            zIndex: 1000,
          }}
        />
      )}

      {/* Painel lateral direito — sempre no DOM para animar via transform. */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        style={{
          position: 'fixed',
          top: 0,
          right: 0,
          height: '100vh',
          width: drawerWidth,
          maxWidth: '100vw',
          background: 'var(--szv2-surface)',
          borderLeft: '1px solid var(--szv2-divider)',
          boxShadow: '-12px 0 32px rgba(0,0,0,.18)',
          zIndex: 1001,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          transition: 'transform .25s ease',
          transform: open ? 'translateX(0)' : 'translateX(100%)',
          // Tira o painel fechado do fluxo de interação para não capturar cliques.
          pointerEvents: open ? 'auto' : 'none',
        }}
      >
        {/* Header */}
        <div
          style={{
            padding: '16px 20px',
            borderBottom: '1px solid var(--szv2-divider)',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            gap: 12,
            flexShrink: 0,
          }}
        >
          <h3
            id={titleId}
            style={{
              margin: 0,
              fontWeight: 700,
              fontSize: 15,
              color: 'var(--szv2-text)',
              minWidth: 0,
            }}
          >
            {title}
          </h3>
          <button
            type="button"
            onClick={onClose}
            aria-label="Fechar painel"
            style={{
              width: 32,
              height: 32,
              border: 0,
              background: 'transparent',
              borderRadius: 8,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--szv2-text-muted)',
              fontSize: 18,
              lineHeight: 1,
              flexShrink: 0,
            }}
          >
            ✕
          </button>
        </div>

        {/* Body */}
        <div
          style={{
            flex: 1,
            overflowY: 'auto',
            padding: '20px',
          }}
        >
          {/* Render do conteúdo apenas com o painel aberto: garante que a
              animação de entrada toque (casca montada, conteúdo entra ao abrir). */}
          {open ? children : null}
        </div>

        {/* Footer */}
        {footer && (
          <div
            style={{
              padding: '16px 20px',
              borderTop: '1px solid var(--szv2-divider)',
              display: 'flex',
              justifyContent: 'flex-end',
              gap: 8,
              flexShrink: 0,
            }}
          >
            {footer}
          </div>
        )}
      </div>
    </>
  )
}
