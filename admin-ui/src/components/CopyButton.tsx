// CopyButton — botão reutilizável que copia texto pro clipboard e mostra feedback.
// Usado em OrderDetail.tsx (tracking, NFe chave) e em Orders.tsx (drawer inline).
//
// Modos de exibição:
//   default — botão que mostra o texto e copia ao clicar (estilo monospace laranja).
//   icon    — apenas ícone 📋 ao lado, exibe o texto separadamente como children.

import { useState, type CSSProperties, type ReactNode } from 'react'

type Props = {
  text: string
  /** Conteúdo opcional do botão; se omitido renderiza o próprio texto. */
  children?: ReactNode
  /** "icon" mostra só o ícone 📋, ideal pra colocar ao lado de um label. */
  variant?: 'inline' | 'icon'
  /** Sobrescreve estilo do botão (cores, tamanho). */
  style?: CSSProperties
  /** Texto exibido no estado "copiado" — default "copiado". */
  copiedLabel?: string
  /** Quanto tempo o feedback permanece visível em ms. */
  copiedMs?: number
  title?: string
}

export default function CopyButton({
  text,
  children,
  variant = 'inline',
  style,
  copiedLabel = '✓ copiado',
  copiedMs = 1500,
  title = 'Copiar',
}: Props) {
  const [copied, setCopied] = useState(false)

  async function doCopy(e: React.MouseEvent) {
    e.stopPropagation()
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), copiedMs)
    } catch {
      // Fallback silencioso — alguns navegadores em http (sem TLS) bloqueiam clipboard.
    }
  }

  if (variant === 'icon') {
    return (
      <button
        type="button"
        onClick={doCopy}
        title={title}
        style={{
          background: 'transparent',
          border: 'none',
          padding: '0 4px',
          cursor: 'pointer',
          fontSize: 13,
          color: copied ? 'var(--szv2-success)' : 'var(--szv2-text-muted)',
          ...style,
        }}
      >
        {copied ? '✓' : '📋'}
      </button>
    )
  }

  return (
    <button
      type="button"
      onClick={doCopy}
      title={title}
      style={{
        background: 'transparent',
        border: 'none',
        padding: 0,
        cursor: 'pointer',
        color: 'var(--szv2-brand)',
        fontFamily: 'var(--szv2-font-mono)',
        fontSize: 13,
        fontWeight: 600,
        textAlign: 'left',
        ...style,
      }}
    >
      {copied ? copiedLabel : (children ?? text)}
    </button>
  )
}
