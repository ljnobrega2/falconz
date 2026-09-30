/**
 * AUDIT-2026-06-18 Onda3 — ToastHost
 * Renderiza stack de toasts via createPortal em document.body.
 *
 * Feedback FALK — 3 variantes principais (identidade azul de marca):
 *   SUCESSO (kind='ok')   — ícone de check, discreto, auto-fecha ~3,5 s.
 *   ERRO    (kind='err')  — ícone de erro, sem auto-fechamento (só ✕).
 *   AVISO   (kind='warn') — ícone de atenção, destaque moderado, auto-fecha ~6 s.
 *   INFO    (kind='info') — neutro, auto-fecha ~5 s.
 *
 * Cores: borda/ícone usam as semânticas do tema (verde de status p/ sucesso,
 * vermelho p/ erro, âmbar p/ aviso, azul FALK p/ info). Tema claro+escuro
 * herdam via tokens (ver styles/components.css → .szv2-toasts).
 *
 * Montado uma vez em Layout.tsx. NÃO altera a API pública (emitToast/showToast).
 */
import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import { type ToastItem, type ToastKind, useToastList } from '../hooks/useToast'

/** Duração de auto-dismiss por variante (ms). `null` = não fecha sozinho. */
const AUTO_DISMISS_MS: Record<ToastKind, number | null> = {
  ok:   3500,   // sucesso: discreto, some rápido
  warn: 6000,   // aviso: fica mais tempo para ser lido
  info: 5000,   // info: padrão neutro
  err:  null,   // erro: exige fechamento manual (explicação clara permanece)
}

/** Mapeamento variante → classe de acento (borda esquerda) já definida no CSS. */
const KIND_CLS: Record<ToastKind, string> = {
  ok:   'szv2-toast-success',
  err:  'szv2-toast-danger',
  warn: 'szv2-toast-warning',
  info: 'szv2-toast-info',
}

/** Cor do ícone por variante — semânticas do tema (claro/escuro herdam os tokens). */
const ICON_COLOR: Record<ToastKind, string> = {
  ok:   'var(--szv2-success, #16a34a)',
  err:  'var(--szv2-danger, #dc2626)',
  warn: 'var(--szv2-warning, #d97706)',
  info: 'var(--szv2-info, #2563eb)',
}

/** Rótulo acessível da variante (PT-BR). */
const KIND_LABEL: Record<ToastKind, string> = {
  ok:   'Sucesso',
  err:  'Erro',
  warn: 'Aviso',
  info: 'Informação',
}

/** Ícone SVG por variante (16px, herda a cor via currentColor). */
function ToastIcon({ kind }: { kind: ToastKind }) {
  const common = {
    width: 16,
    height: 16,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 2,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true,
    focusable: false,
  }
  switch (kind) {
    case 'ok': // check discreto
      return (
        <svg {...common}>
          <path d="M20 6 9 17l-5-5" />
        </svg>
      )
    case 'err': // x-circle (erro)
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="9" />
          <path d="m15 9-6 6M9 9l6 6" />
        </svg>
      )
    case 'warn': // triângulo de atenção
      return (
        <svg {...common}>
          <path d="M10.3 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.7 3.86a2 2 0 0 0-3.4 0Z" />
          <path d="M12 9v4" />
          <path d="M12 17h.01" />
        </svg>
      )
    default: // info
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="9" />
          <path d="M12 11v5M12 8h.01" />
        </svg>
      )
  }
}

function Toast({ item, onDismiss }: { item: ToastItem; onDismiss: () => void }) {
  const kind = item.kind
  const autoMs = AUTO_DISMISS_MS[kind]

  useEffect(() => {
    // Erro não fecha sozinho — só pelo botão ✕.
    if (autoMs == null) return
    const t = setTimeout(onDismiss, autoMs)
    return () => clearTimeout(t)
  }, [onDismiss, autoMs])

  // Erro/aviso anunciam com prioridade; sucesso/info de forma educada.
  const assertive = kind === 'err' || kind === 'warn'

  return (
    <div
      className={`szv2-toast ${KIND_CLS[kind]}`}
      role={assertive ? 'alert' : 'status'}
      aria-live={assertive ? 'assertive' : 'polite'}
    >
      <span
        className="szv2-toast-icon"
        style={{ color: ICON_COLOR[kind], position: 'relative', display: 'flex', flexShrink: 0, marginTop: 1 }}
      >
        <ToastIcon kind={kind} />
        <span style={{ position: 'absolute', width: 1, height: 1, overflow: 'hidden', clip: 'rect(0 0 0 0)' }}>
          {KIND_LABEL[kind]}:
        </span>
      </span>
      <span className="szv2-toast-msg">{item.msg}</span>
      <button
        type="button"
        className="szv2-toast-close"
        onClick={onDismiss}
        aria-label="Fechar notificação"
      >
        ✕
      </button>
    </div>
  )
}

/**
 * ToastHost — monta na raiz do documento via portal.
 * Deve ser inserido UMA vez em Layout.tsx.
 */
export default function ToastHost() {
  const { toasts, dismissToast } = useToastList()

  return createPortal(
    <div className="szv2-toasts" aria-label="Notificações">
      {toasts.map(t => (
        <Toast key={t.id} item={t} onDismiss={() => dismissToast(t.id)} />
      ))}
    </div>,
    document.body,
  )
}
