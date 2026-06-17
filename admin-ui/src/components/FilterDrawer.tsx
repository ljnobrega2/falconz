// Painel de filtros deslizante na lateral direita.
// Uso: <FilterDrawer open={open} onClose={...} onApply={...} onClear={...} title="Filtros">
//        {/* campos de filtro como children */}
//      </FilterDrawer>

import { ReactNode } from 'react'

interface Props {
  open: boolean
  onClose: () => void
  onApply: () => void
  onClear: () => void
  title?: string
  applyLabel?: string
  clearLabel?: string
  children: ReactNode
}

export default function FilterDrawer({ open, onClose, onApply, onClear, title = 'Filtros', applyLabel = 'Aplicar filtros', clearLabel = 'Limpar filtros', children }: Props) {
  return (
    <>
      {/* Overlay — só renderiza quando aberto */}
      {open && (
        <div
          onClick={onClose}
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.3)',
            zIndex: 500,
          }}
        />
      )}

      {/* Drawer — sempre no DOM para animar */}
      <div
        style={{
          position: 'fixed',
          top: 0,
          right: 0,
          height: '100vh',
          width: 340,
          background: 'var(--szv2-surface)',
          borderLeft: '1px solid var(--szv2-divider)',
          zIndex: 501,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          transition: 'transform .25s ease',
          transform: open ? 'translateX(0)' : 'translateX(100%)',
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
          }}
        >
          <span style={{ fontWeight: 700, fontSize: 15, color: 'var(--szv2-text)' }}>{title}</span>
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
            display: 'flex',
            flexDirection: 'column',
            gap: '20px',
          }}
        >
          {children}
        </div>

        {/* Footer */}
        <div
          style={{
            padding: '16px 20px',
            borderTop: '1px solid var(--szv2-divider)',
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
          }}
        >
          <button
            type="button"
            className="szv2-btn szv2-btn-brand"
            style={{ width: '100%' }}
            onClick={onApply}
          >
            {applyLabel}
          </button>
          <button
            type="button"
            onClick={onClear}
            style={{
              background: 'transparent',
              border: 0,
              color: 'var(--szv2-text-muted)',
              cursor: 'pointer',
              fontSize: 13,
              padding: '6px 0',
              textAlign: 'center',
            }}
          >
            {clearLabel}
          </button>
        </div>
      </div>
    </>
  )
}
