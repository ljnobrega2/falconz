// Botão padrão que abre o FilterDrawer.
// Uso: <FilterButton active={hasFilters} onClick={openDrawer} count={activeFilterCount} />

interface Props {
  active?: boolean
  onClick: () => void
  count?: number
}

// Ícone de funil (SVG inline)
const FunnelIcon = () => (
  <svg
    viewBox="0 0 20 20"
    width="16"
    height="16"
    fill="currentColor"
    aria-hidden="true"
    style={{ flexShrink: 0 }}
  >
    <path d="M3 3h14l-5 6v5l-4 2V9L3 3z" />
  </svg>
)

export default function FilterButton({ active = false, onClick, count = 0 }: Props) {
  return (
    <button
      type="button"
      className="szv2-btn szv2-btn-secondary"
      onClick={onClick}
      style={{ position: 'relative', gap: 6 }}
    >
      <FunnelIcon />
      Filtros
      {active && count > 0 && (
        <span
          style={{
            position: 'absolute',
            top: -6,
            right: -6,
            minWidth: 18,
            height: 18,
            borderRadius: 999,
            background: 'var(--szv2-brand)',
            color: '#fff',
            fontSize: 10,
            fontWeight: 700,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: '0 4px',
            lineHeight: 1,
          }}
        >
          {count}
        </span>
      )}
    </button>
  )
}
