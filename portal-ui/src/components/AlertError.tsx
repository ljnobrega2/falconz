// Faixa de erro padronizada (.sz-alert-danger) COM semântica a11y:
//   role="alert" → leitores de tela anunciam o erro imediatamente.
// Opcionalmente renderiza um botão "Tentar novamente" quando onRetry é passado.
// Substitui os <div className="sz-alert-danger">{err}</div> ad-hoc (que eram
// silenciosos para tecnologias assistivas).

export default function AlertError({
  message,
  onRetry,
}: {
  message: string
  onRetry?: () => void
}) {
  if (!message) return null
  return (
    <div
      role="alert"
      className="sz-alert-danger"
      style={
        onRetry
          ? { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }
          : undefined
      }
    >
      <span>{message}</span>
      {onRetry && (
        <button
          type="button"
          className="szv2-btn szv2-btn-secondary szv2-btn-sm"
          onClick={onRetry}
          style={{ flexShrink: 0 }}
        >
          Tentar novamente
        </button>
      )}
    </div>
  )
}
