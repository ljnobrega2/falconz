// Loading padronizado INLINE — para loaders dentro de painéis/cards/modais
// (histórico de carteira, painel de relatório, modal de pedido), onde um
// SectionLoading de 48px de padding seria grande demais.
// Acessível: role="status" + aria-live="polite". Substitui os
// <p style={{padding:16}}>Carregando…</p> ad-hoc.

export default function InlineLoading({ label = 'Carregando…' }: { label?: string }) {
  return (
    <p
      role="status"
      aria-live="polite"
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: 16,
        margin: 0,
        fontSize: 13,
        color: 'var(--szv2-text-muted)',
      }}
    >
      <span
        aria-hidden="true"
        style={{
          width: 14,
          height: 14,
          borderRadius: '50%',
          border: '2px solid var(--szv2-divider)',
          borderTopColor: 'var(--szv2-brand)',
          animation: 'szSpin .7s linear infinite',
        }}
      />
      {label}
    </p>
  )
}
