/**
 * Toast lateral do checkout (tema FALK do próprio checkout — tokens --falkz-*).
 *
 * Mesmo padrão dos outros UIs (admin-ui/portal-ui): um emitter de módulo permite
 * disparar de qualquer lugar (`emitToast`), e o `CheckoutToastHost` (montado 1× em
 * App.tsx) renderiza o stack fixo no canto superior direito via portal no body.
 *
 * O checkout NÃO usa o tema szv2 — usa as variáveis --falkz-* (ver index.css), então
 * este toast tem CSS próprio (`.fk-toasts`) afinado ao tema da marca da oferta.
 *
 * Uso atual: feedback de ERRO ao finalizar o pedido (createOrder). O SUCESSO não
 * dispara toast — navega para a tela de ThankYou (confirmação plena).
 */
import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'

export type ToastKind = 'ok' | 'err'
export interface ToastItem {
  id: number
  kind: ToastKind
  msg: string
}

type Listener = (kind: ToastKind, msg: string) => void
let _listener: Listener | null = null
let _seq = 0

/** Dispara um toast de qualquer lugar. Se o host ainda não montou, é descartado. */
export function emitToast(kind: ToastKind, msg: string) {
  _listener?.(kind, msg)
}

/** Auto-dismiss por variante (ms). Erro fica mais tempo para ser lido. */
const AUTO_MS: Record<ToastKind, number> = { ok: 3500, err: 6000 }

function ToastRow({ item, onDismiss }: { item: ToastItem; onDismiss: () => void }) {
  useEffect(() => {
    const t = setTimeout(onDismiss, AUTO_MS[item.kind])
    return () => clearTimeout(t)
  }, [item.kind, onDismiss])

  return (
    <div
      className={`fk-toast fk-toast-${item.kind}`}
      role={item.kind === 'err' ? 'alert' : 'status'}
      aria-live={item.kind === 'err' ? 'assertive' : 'polite'}
    >
      <span className="fk-toast-icon" aria-hidden="true">
        {item.kind === 'ok' ? '✓' : '⚠️'}
      </span>
      <span className="fk-toast-msg">{item.msg}</span>
      <button type="button" className="fk-toast-close" onClick={onDismiss} aria-label="Fechar notificação">
        ✕
      </button>
    </div>
  )
}

/** Monta uma vez em App.tsx — renderiza o stack via portal no document.body. */
export function CheckoutToastHost() {
  const [toasts, setToasts] = useState<ToastItem[]>([])

  useEffect(() => {
    _listener = (kind, msg) => {
      const id = ++_seq
      setToasts((prev) => [...prev, { id, kind, msg }])
    }
    return () => {
      _listener = null
    }
  }, [])

  function dismiss(id: number) {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }

  return createPortal(
    <div className="fk-toasts" aria-label="Notificações">
      {toasts.map((t) => (
        <ToastRow key={t.id} item={t} onDismiss={() => dismiss(t.id)} />
      ))}
    </div>,
    document.body,
  )
}
