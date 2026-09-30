// FilterChips — exibe os filtros ATIVOS como chips removíveis (#28). Marca FALK
// (azul #1E6FF2). Clicar no "x" remove AQUELE filtro e re-aplica imediatamente
// (não é batch — diferente do "Filtrar" do FilterDrawer; assimetria intencional).
//
// Lê o estado APLICADO (não o rascunho do drawer). Cada chip mostra um rótulo
// legível (label do campo + valor já mapeado p/ exibição). Classe própria
// (szv2-active-chip) — NÃO reusa szv2-filter-chip (esse é toggle de status).
import type { ReactNode } from 'react'

export interface ActiveChip {
  /** Chave do filtro (usada no onRemove). */
  key: string
  /** Texto exibido no chip (ex.: "Status: Entregue"). */
  label: ReactNode
}

export interface FilterChipsProps {
  chips: ActiveChip[]
  /** Remove um filtro (limpa a chave e re-aplica na hora). */
  onRemove: (key: string) => void
  /** Opcional: "Limpar tudo" quando há ≥ 2 chips. */
  onClearAll?: () => void
}

export default function FilterChips({ chips, onRemove, onClearAll }: FilterChipsProps) {
  if (chips.length === 0) return null

  return (
    <div
      style={{
        display: 'flex',
        gap: 6,
        flexWrap: 'wrap',
        alignItems: 'center',
      }}
    >
      {chips.map(c => (
        <span
          key={c.key}
          className="szv2-active-chip"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '4px 8px 4px 10px',
            fontSize: 12,
            fontWeight: 600,
            lineHeight: 1.3,
            color: 'var(--szv2-brand)',
            background: 'var(--szv2-brand-light)',
            border: '1px solid var(--szv2-brand)',
            borderRadius: 'var(--szv2-radius-pill)',
          }}
        >
          {c.label}
          <button
            type="button"
            aria-label="Remover filtro"
            onClick={() => onRemove(c.key)}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 16,
              height: 16,
              border: 0,
              padding: 0,
              borderRadius: '50%',
              background: 'var(--szv2-brand)',
              color: 'var(--szv2-on-brand)',
              fontSize: 12,
              lineHeight: 1,
              cursor: 'pointer',
            }}
          >
            &times;
          </button>
        </span>
      ))}

      {onClearAll && chips.length >= 2 && (
        <button
          type="button"
          onClick={onClearAll}
          style={{
            border: 0,
            background: 'transparent',
            color: 'var(--szv2-text-muted)',
            fontSize: 12,
            fontWeight: 600,
            cursor: 'pointer',
            textDecoration: 'underline',
            padding: '4px 6px',
          }}
        >
          Limpar tudo
        </button>
      )}
    </div>
  )
}
