/**
 * AUDIT-2026-06-18 Onda3 — ConfirmDialog + confirmAsync()
 * Substitui window.confirm/alert por diálogo estilizado.
 *
 * API pública:
 *   const ok = await confirmAsync({ title, message, confirmLabel?, danger? })
 *   → true  se usuário confirmou
 *   → false se cancelou ou fechou
 *
 * <ConfirmHost> deve ser montado UMA vez em Layout.tsx (já recebe estado
 * do emitter de módulo — não precisa de contexto React).
 */
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

// ── Tipos ─────────────────────────────────────────────────────────────────────

export interface ConfirmOptions {
  title?: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  /** Se true, botão confirmar fica vermelho (szv2-btn-danger) */
  danger?: boolean
}

// ── Estado de módulo (fora do React tree) ─────────────────────────────────────

type PendingConfirm = {
  opts: ConfirmOptions
  resolve: (ok: boolean) => void
}

let _pending: PendingConfirm | null = null
let _setState: ((p: PendingConfirm | null) => void) | null = null

/**
 * Solicita confirmação de forma assíncrona.
 * Retorna Promise<boolean> — true = confirmado, false = cancelado/fechado.
 */
export function confirmAsync(opts: ConfirmOptions): Promise<boolean> {
  return new Promise(resolve => {
    _pending = { opts, resolve }
    _setState?.({ opts, resolve })
  })
}

// ── Componente host ───────────────────────────────────────────────────────────

const FOCUSABLE =
  'button:not([disabled]),input:not([disabled]),[tabindex]:not([tabindex="-1"])'

/**
 * Monta na raiz do documento via portal.
 * Deve ser incluído UMA vez em Layout.tsx.
 */
export function ConfirmHost() {
  const [dialog, setDialog] = useState<PendingConfirm | null>(null)
  const cancelBtnRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  // Liga o emitter de módulo ao estado local
  useEffect(() => {
    _setState = setDialog
    return () => { _setState = null }
  }, [])

  // Foco no botão cancelar ao abrir
  useEffect(() => {
    if (dialog) {
      requestAnimationFrame(() => cancelBtnRef.current?.focus())
    }
  }, [dialog])

  // Escape cancela
  useEffect(() => {
    if (!dialog) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') { e.stopPropagation(); respond(false) }
    }
    document.addEventListener('keydown', onKeyDown, true)
    return () => document.removeEventListener('keydown', onKeyDown, true)
  }, [dialog])

  // Focus trap
  function handleKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.key !== 'Tab' || !dialogRef.current) return
    const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>(FOCUSABLE))
    if (focusable.length === 0) return
    const first = focusable[0]; const last = focusable[focusable.length - 1]
    if (e.shiftKey) {
      if (document.activeElement === first) { e.preventDefault(); last.focus() }
    } else {
      if (document.activeElement === last) { e.preventDefault(); first.focus() }
    }
  }

  function respond(ok: boolean) {
    const p = _pending
    _pending = null
    setDialog(null)
    p?.resolve(ok)
  }

  if (!dialog) return null

  const { opts } = dialog
  const title = opts.title ?? (opts.danger ? 'Confirmar ação' : 'Confirmação')
  const confirmLabel = opts.confirmLabel ?? (opts.danger ? 'Confirmar' : 'OK')
  const cancelLabel = opts.cancelLabel ?? 'Cancelar'

  // #39: portar para dentro de .sz-root (que carrega .sz-dashboard-v2 + data-theme),
  // não para document.body. As regras do modal (szv2-modal-*) e os tokens --szv2-*
  // são escopados em .sz-dashboard-v2 — fora dele o diálogo renderizava cru
  // ("CancelarLimpar" no rodapé). Em .sz-root, claro E escuro resolvem corretamente.
  const portalTarget =
    (typeof document !== 'undefined' && document.querySelector('.sz-root')) || document.body

  return createPortal(
    <div
      className="szv2-modal-overlay szv2-open"
      onClick={() => respond(false)}
      aria-hidden="false"
    >
      <div
        ref={dialogRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="szv2-confirm-title"
        aria-describedby="szv2-confirm-msg"
        className="szv2-modal szv2-confirm-dialog"
        onClick={e => e.stopPropagation()}
        onKeyDown={handleKeyDown}
        tabIndex={-1}
        style={{ maxWidth: 440 }}
      >
        <div className="szv2-modal-head">
          <h3 id="szv2-confirm-title" style={{ color: opts.danger ? 'var(--szv2-danger)' : undefined }}>
            {opts.danger && (
              <span aria-hidden="true" style={{ marginRight: 8 }}>⚠</span>
            )}
            {title}
          </h3>
        </div>
        <div className="szv2-modal-body">
          <p id="szv2-confirm-msg" style={{ margin: 0, lineHeight: 1.6, fontSize: 14, color: 'var(--szv2-text-soft)' }}>
            {opts.message}
          </p>
        </div>
        <div className="szv2-modal-foot" style={{ justifyContent: 'flex-end' }}>
          <button
            ref={cancelBtnRef}
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={() => respond(false)}
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            className={`szv2-btn ${opts.danger ? 'szv2-btn-danger' : 'szv2-btn-brand'}`}
            onClick={() => respond(true)}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>,
    portalTarget,
  )
}
