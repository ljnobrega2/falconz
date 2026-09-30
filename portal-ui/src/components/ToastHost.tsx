/**
 * AUDIT-2026-06-18 Onda3 — ToastHost
 * Renderiza stack de toasts via createPortal. Auto-dismiss em 5 s.
 * Montado uma vez em Layout.tsx.
 */
import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import { type ToastItem, useToastList } from '../hooks/useToast'

const AUTO_DISMISS_MS = 5000

function Toast({ item, onDismiss }: { item: ToastItem; onDismiss: () => void }) {
  useEffect(() => {
    const t = setTimeout(onDismiss, AUTO_DISMISS_MS)
    return () => clearTimeout(t)
  }, [onDismiss])

  const kindCls =
    item.kind === 'ok'   ? 'szv2-toast-success' :
    item.kind === 'err'  ? 'szv2-toast-danger'  :
    item.kind === 'warn' ? 'szv2-toast-warning' :
                           'szv2-toast-info'

  return (
    <div className={`szv2-toast ${kindCls}`} role="alert" aria-live="assertive">
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

  // #39 (mesmo bug do ConfirmDialog): as regras .szv2-toasts/.szv2-toast são
  // escopadas em .sz-dashboard-v2 (tokens --szv2-*). Portar pra document.body
  // renderiza os toasts SEM CSS (sem position:fixed, sem z-index) — existem no
  // DOM mas ficam invisíveis/perdidos no fluxo da página. Porta pra .sz-root.
  const portalTarget =
    (typeof document !== 'undefined' && document.querySelector('.sz-root')) || document.body

  return createPortal(
    <div className="szv2-toasts" aria-label="Notificações">
      {toasts.map(t => (
        <Toast key={t.id} item={t} onDismiss={() => dismissToast(t.id)} />
      ))}
    </div>,
    portalTarget,
  )
}
