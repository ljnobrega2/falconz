// Drawer lateral direito — marca registrada FALK (azul #1E6FF2, ZERO laranja).
//
// Painel desliza da DIREITA sobre um overlay escurecido. Fecha em: ESC, clique
// fora (no overlay), botão X. Reutilizável p/ qualquer detalhe/ação de entidade —
// substitui o modal central nas telas de detalhe (Pedidos, Motoboy COD).
//
// Por que inline + tokens (e não classe szv2-modal-overlay): o overlay de modal é
// CENTRALIZADO (align/justify center) e seu CSS está escopado em `.sz-dashboard-v2`.
// Aqui o painel é PINNED à direita com transform de slide — usamos estilos inline
// com `var(--szv2-*)` (custom properties cascateiam do tema, independem de ancestral),
// e renderizamos INLINE (dentro de `.sz-dashboard-v2`) igual aos modais existentes.
import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'

export interface DrawerProps {
  open: boolean
  onClose: () => void
  title?: ReactNode
  /** Largura do painel (px). Default 480. */
  width?: number
  children: ReactNode
  /** Rótulo acessível do diálogo (fallback p/ title quando este é nó). */
  ariaLabel?: string
}

export default function Drawer({ open, onClose, title, width = 480, children, ariaLabel }: DrawerProps) {
  const panelRef = useRef<HTMLDivElement>(null)

  // ESC fecha (captura na fase de captura p/ ganhar de handlers internos).
  useEffect(() => {
    if (!open) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
      }
    }
    document.addEventListener('keydown', onKeyDown, true)
    return () => document.removeEventListener('keydown', onKeyDown, true)
  }, [open, onClose])

  // Foco no painel ao abrir (acessibilidade — leitor de tela entra no diálogo).
  useEffect(() => {
    if (open) requestAnimationFrame(() => panelRef.current?.focus())
  }, [open])

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={ariaLabel || (typeof title === 'string' ? title : 'Detalhes')}
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(15, 23, 42, 0.45)',
        display: 'flex',
        justifyContent: 'flex-end',
        zIndex: 1000,
        animation: 'szv2DrawerFade .18s ease',
      }}
    >
      <div
        ref={panelRef}
        tabIndex={-1}
        onClick={e => e.stopPropagation()}
        style={{
          width,
          maxWidth: '96vw',
          height: '100%',
          background: 'var(--szv2-surface)',
          borderLeft: '1px solid var(--szv2-border)',
          boxShadow: 'var(--szv2-shadow-float)',
          display: 'flex',
          flexDirection: 'column',
          animation: 'szv2DrawerSlide .22s cubic-bezier(.22,.61,.36,1)',
          outline: 'none',
        }}
      >
        {/* Cabeçalho — acento azul brand na borda inferior */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '14px 18px',
            borderBottom: '1px solid var(--szv2-divider)',
            boxShadow: 'inset 0 -2px 0 var(--szv2-brand)',
            flexShrink: 0,
          }}
        >
          <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>{title}</h3>
          <button
            type="button"
            aria-label="Fechar"
            onClick={onClose}
            style={{
              width: 32,
              height: 32,
              border: 0,
              background: 'transparent',
              borderRadius: 8,
              color: 'var(--szv2-text-muted)',
              fontSize: 20,
              lineHeight: 1,
              cursor: 'pointer',
            }}
          >
            &times;
          </button>
        </div>

        {/* Corpo rolável */}
        <div style={{ padding: '18px', overflowY: 'auto', flex: 1 }}>{children}</div>
      </div>
    </div>
  )
}
