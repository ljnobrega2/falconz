import { Component, ErrorInfo, ReactNode } from 'react'

type Props = { children: ReactNode }
type State = { hasError: boolean; error: Error | null }

export default class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info)
  }

  handleReload = () => {
    window.location.reload()
  }

  render() {
    if (this.state.hasError) {
      return (
        <div style={{ padding: '40px 24px', textAlign: 'center', maxWidth: '560px', margin: '60px auto' }}>
          <h2 style={{ marginBottom: '12px', color: 'var(--szv2-text)' }}>Algo deu errado</h2>
          <p style={{ marginBottom: '24px', color: 'var(--szv2-text-soft)' }}>
            Não foi possível carregar esta tela. Tente novamente ou contate o suporte.
          </p>
          <button className="szv2-btn-brand" onClick={this.handleReload}>Recarregar</button>
        </div>
      )
    }
    return this.props.children
  }
}
