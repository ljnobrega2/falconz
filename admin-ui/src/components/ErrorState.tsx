// Estado de ERRO padronizado para falha de carregamento (fetch inicial).
// Mesma anatomia visual do EmptyState (ícone + título + descrição), mas com
// tom de alerta e um CTA "Tentar novamente" que re-dispara o load da página.
//
// Quando usar:
//   - SOMENTE quando o carregamento inicial falhou E não há dados para mostrar
//     (`err && items.length === 0`). Assim o usuário consegue se recuperar sem
//     recarregar a página inteira.
//   - Erros de mutação (excluir/salvar) ou de refresh com dados já na tela
//     continuam no banner `.sz-alert-danger` do topo — NÃO troque aquele banner
//     por este componente, senão uma falha de mutação apaga a tabela.
//
// Uso:
//   {loading && items.length === 0
//     ? <TableSkeleton ... />
//     : err && items.length === 0
//       ? <ErrorState message={err} onRetry={load} />
//       : items.length === 0
//         ? <EmptyState ... />
//         : <table>...</table>}
//
// `onRetry` é opcional: páginas cujo fetch vive inline no useEffect (sem um
// `load()` nomeado) podem omitir e exibir só a mensagem.

type ErrorStateProps = {
  message?: string
  title?: string
  onRetry?: () => void
  /** Texto do botão de retry. Default: "Tentar novamente". */
  retryLabel?: string
}

export default function ErrorState({
  message,
  title = 'Não foi possível carregar',
  onRetry,
  retryLabel = 'Tentar novamente',
}: ErrorStateProps) {
  return (
    <div
      role="alert"
      aria-live="assertive"
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        textAlign: 'center',
        padding: '48px 20px',
        border: '1px solid var(--szv2-danger)',
        borderRadius: 12,
        background: 'var(--szv2-danger-bg)',
        color: 'var(--szv2-text)',
        gap: 12,
      }}
    >
      <div
        aria-hidden="true"
        style={{ fontSize: 44, lineHeight: 1, marginBottom: 2 }}
      >
        ⚠️
      </div>
      <h3
        style={{
          margin: 0,
          fontSize: 15,
          fontWeight: 700,
          color: 'var(--szv2-danger)',
        }}
      >
        {title}
      </h3>
      {message && (
        <p
          style={{
            margin: 0,
            maxWidth: 460,
            fontSize: 13,
            lineHeight: 1.5,
            color: 'var(--szv2-text-soft)',
          }}
        >
          {message}
        </p>
      )}
      {onRetry && (
        <button
          type="button"
          className="szv2-btn szv2-btn-brand"
          onClick={onRetry}
          style={{ marginTop: 8 }}
        >
          {retryLabel}
        </button>
      )}
    </div>
  )
}
