// Estado vazio padronizado: emoji + título + descrição opcional + CTA opcional.
// Substitui blocos ad-hoc de "Nenhum X encontrado" espalhados pelo painel.
//
// Uso simples:
//   <EmptyState title="Nenhum pedido encontrado com esses filtros." />
//
// Com CTA:
//   <EmptyState
//     icon="🛵"
//     title="Nenhum motoboy cadastrado"
//     description="Cadastre o primeiro motoboy para começar."
//     action={{ label: 'Cadastrar motoboy', onClick: openCreate }}
//   />

type EmptyStateProps = {
  icon?: string
  title: string
  description?: string
  action?: {
    label: string
    onClick: () => void
  }
}

export default function EmptyState({
  icon = '📭',
  title,
  description,
  action,
}: EmptyStateProps) {
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        textAlign: 'center',
        padding: '48px 20px',
        border: '1px dashed var(--szv2-border)',
        borderRadius: 12,
        background: 'var(--szv2-surface-alt)',
        color: 'var(--szv2-text-muted)',
        gap: 12,
      }}
    >
      <div
        aria-hidden="true"
        style={{
          fontSize: 48,
          lineHeight: 1,
          marginBottom: 4,
          opacity: 0.85,
        }}
      >
        {icon}
      </div>
      <h3
        style={{
          margin: 0,
          fontSize: 15,
          fontWeight: 700,
          color: 'var(--szv2-text)',
        }}
      >
        {title}
      </h3>
      {description && (
        <p
          style={{
            margin: 0,
            maxWidth: 420,
            fontSize: 13,
            lineHeight: 1.5,
            color: 'var(--szv2-text-muted)',
          }}
        >
          {description}
        </p>
      )}
      {action && (
        <button
          type="button"
          className="szv2-btn szv2-btn-brand"
          onClick={action.onClick}
          style={{ marginTop: 8 }}
        >
          {action.label}
        </button>
      )}
    </div>
  )
}
