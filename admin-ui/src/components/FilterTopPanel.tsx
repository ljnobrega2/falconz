// Painel de filtros que desce do topo (slide-down from top).
// Substitui o FilterDrawer lateral por um painel horizontal estilo Logzz.
//
// Uso:
//   <FilterTopPanel
//     open={open}
//     onClose={closePanel}
//     onApply={applyFilters}
//     onClear={clearFilters}
//     title="Filtros"
//   >
//     <FilterField label="De"><input type="date" /></FilterField>
//     <FilterField label="Até"><input type="date" /></FilterField>
//     ...
//   </FilterTopPanel>
//
// Animação: o painel é sempre renderizado para permitir transição CSS;
// o overlay (rgba(0,0,0,.3)) só renderiza quando aberto e fecha ao clicar fora.
// Tecla Esc fecha o painel.

import { ReactNode, useEffect } from 'react'

type FilterTopPanelProps = {
  open: boolean
  onClose: () => void
  onApply: () => void
  onClear: () => void
  title?: string
  applyLabel?: string
  clearLabel?: string
  children: ReactNode
}

export default function FilterTopPanel({
  open,
  onClose,
  onApply,
  onClear,
  title = 'Filtros',
  applyLabel = 'Aplicar filtros',
  clearLabel = 'Limpar',
  children,
}: FilterTopPanelProps) {
  // Esc fecha o painel — registra/remove handler apenas quando aberto.
  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  return (
    <>
      {/* Overlay — só renderiza quando aberto. Click fecha. */}
      {open && (
        <div
          onClick={onClose}
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.3)',
            zIndex: 501,
          }}
        />
      )}

      {/* Painel — sempre no DOM para animar via transform. */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        style={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          maxWidth: 1100,
          margin: '0 auto',
          background: 'var(--szv2-surface)',
          border: '1px solid var(--szv2-divider)',
          borderTop: 0,
          borderRadius: '0 0 16px 16px',
          boxShadow: '0 12px 32px rgba(0,0,0,.18)',
          zIndex: 502,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          transition: 'transform .25s ease',
          transform: open ? 'translateY(0)' : 'translateY(-100%)',
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
          }}
        >
          <span style={{ fontWeight: 700, fontSize: 15, color: 'var(--szv2-text)' }}>
            {title}
          </span>
          <button
            type="button"
            onClick={onClose}
            aria-label="Fechar painel de filtros"
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
            }}
          >
            ✕
          </button>
        </div>

        {/* Body — grid responsivo de blocos de filtro. */}
        <div
          style={{
            padding: '20px',
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
            gap: 16,
            maxHeight: '70vh',
            overflowY: 'auto',
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
            justifyContent: 'flex-end',
            alignItems: 'center',
            gap: 12,
          }}
        >
          <button
            type="button"
            onClick={onClear}
            style={{
              background: 'transparent',
              border: 0,
              color: 'var(--szv2-text-muted)',
              cursor: 'pointer',
              fontSize: 13,
              padding: '8px 12px',
            }}
          >
            {clearLabel}
          </button>
          <button
            type="button"
            className="szv2-btn szv2-btn-brand"
            onClick={onApply}
          >
            {applyLabel}
          </button>
        </div>
      </div>
    </>
  )
}

// ─── FilterField ─────────────────────────────────────────────────────────────
// Bloco padrão para envolver um campo de filtro com label discreto em cima.

type FilterFieldProps = {
  label: string
  hint?: string
  children: ReactNode
}

export function FilterField({ label, hint, children }: FilterFieldProps) {
  return (
    <label
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        minWidth: 0,
      }}
    >
      <span
        style={{
          fontSize: 11,
          fontWeight: 700,
          color: 'var(--szv2-text-muted)',
          textTransform: 'uppercase',
          letterSpacing: 0.4,
        }}
      >
        {label}
      </span>
      {children}
      {hint && (
        <span style={{ fontSize: 11, color: 'var(--szv2-text-faint)' }}>{hint}</span>
      )}
    </label>
  )
}

// Estilo padrão para inputs/selects usados como filhos de FilterField.
// Exportado para reutilizar nos consumidores e manter consistência visual.
export const filterInputStyle: React.CSSProperties = {
  height: 38,
  padding: '0 12px',
  background: 'var(--szv2-surface-alt)',
  border: '1px solid var(--szv2-border)',
  borderRadius: 10,
  color: 'var(--szv2-text)',
  font: 'inherit',
  fontSize: 13,
  width: '100%',
  boxSizing: 'border-box',
}

// ─── ActiveFilterChips ───────────────────────────────────────────────────────
// Renderiza chips clicáveis com os filtros aplicados, cada um com X para
// remover. Usado abaixo do header da página, acima da tabela.

export type ActiveChip = {
  key: string
  label: string
  onRemove: () => void
}

type ActiveFilterChipsProps = {
  chips: ActiveChip[]
  onClearAll?: () => void
}

export function ActiveFilterChips({ chips, onClearAll }: ActiveFilterChipsProps) {
  if (chips.length === 0) return null
  return (
    <div
      style={{
        display: 'flex',
        flexWrap: 'wrap',
        gap: 6,
        margin: '0 0 12px',
        alignItems: 'center',
      }}
    >
      {chips.map(c => (
        <span
          key={c.key}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '4px 4px 4px 10px',
            background: 'var(--szv2-brand-light, rgba(234,88,12,.10))',
            color: 'var(--szv2-brand)',
            borderRadius: 999,
            fontSize: 12,
            fontWeight: 600,
            lineHeight: 1,
          }}
        >
          {c.label}
          <button
            type="button"
            onClick={c.onRemove}
            aria-label={`Remover filtro ${c.label}`}
            style={{
              width: 18,
              height: 18,
              border: 0,
              background: 'transparent',
              color: 'inherit',
              cursor: 'pointer',
              borderRadius: 999,
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 12,
              lineHeight: 1,
              padding: 0,
            }}
          >
            ✕
          </button>
        </span>
      ))}
      {onClearAll && chips.length > 1 && (
        <button
          type="button"
          onClick={onClearAll}
          style={{
            background: 'transparent',
            border: 0,
            color: 'var(--szv2-text-muted)',
            cursor: 'pointer',
            fontSize: 12,
            padding: '4px 8px',
            textDecoration: 'underline',
          }}
        >
          Limpar todos
        </button>
      )}
    </div>
  )
}
