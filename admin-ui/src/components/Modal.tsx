/**
 * AUDIT-2026-06-18 Onda3 — Modal acessível reutilizável
 * - Escape fecha
 * - Focus trap dentro do modal
 * - Foco restaurado ao elemento que abriu ao fechar
 * - aria-modal, role=dialog, aria-labelledby
 * - Botão ✕ com aria-label
 * Reutiliza as classes CSS existentes (.szv2-modal-overlay, .szv2-modal, …)
 */
import {
  type KeyboardEvent,
  type ReactNode,
  useEffect,
  useId,
  useRef,
} from 'react'
import { createPortal } from 'react-dom'

const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]),' +
  'select:not([disabled]),textarea:not([disabled]),' +
  '[tabindex]:not([tabindex="-1"])'

export interface ModalProps {
  open: boolean
  onClose: () => void
  /** Texto exibido no <h3> do cabeçalho */
  title: ReactNode
  /** Se true, aplica szv2-modal-lg */
  large?: boolean
  /** Conteúdo do body */
  children: ReactNode
  /** Botões no rodapé (normalmente szv2-btn*) */
  footer?: ReactNode
  /** Largura customizada (ex.: '720px') */
  width?: string
}

export default function Modal({
  open,
  onClose,
  title,
  large,
  children,
  footer,
  width,
}: ModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const previousFocusRef = useRef<HTMLElement | null>(null)
  const titleId = useId()

  // Salva referência do elemento com foco antes de abrir
  useEffect(() => {
    if (open) {
      previousFocusRef.current = document.activeElement as HTMLElement
      // Move foco para o diálogo após render
      requestAnimationFrame(() => {
        const first = dialogRef.current?.querySelector<HTMLElement>(FOCUSABLE)
        first?.focus()
      })
    } else {
      // Restaura foco ao fechar
      previousFocusRef.current?.focus()
    }
  }, [open])

  // Fecha ao pressionar Escape
  useEffect(() => {
    if (!open) return
    function onKeyDown(e: globalThis.KeyboardEvent) {
      if (e.key === 'Escape') { e.stopPropagation(); onClose() }
    }
    document.addEventListener('keydown', onKeyDown, true)
    return () => document.removeEventListener('keydown', onKeyDown, true)
  }, [open, onClose])

  // Trava foco dentro do modal (focus trap)
  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key !== 'Tab') return
    const el = dialogRef.current
    if (!el) return
    const focusable = Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE))
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (e.shiftKey) {
      if (document.activeElement === first) { e.preventDefault(); last.focus() }
    } else {
      if (document.activeElement === last) { e.preventDefault(); first.focus() }
    }
  }

  if (!open) return null

  // O CSS de .szv2-modal-* é escopado sob .sz-dashboard-v2 (ver components.css).
  // createPortal em document.body escapa esse escopo — portar para dentro do
  // wrapper .sz-dashboard-v2 (se existir) mantém o CSS aplicado.
  const portalTarget = document.querySelector('.sz-dashboard-v2') ?? document.body

  return createPortal(
    <div
      className="szv2-modal-overlay szv2-open"
      onClick={onClose}
      aria-hidden="false"
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={`szv2-modal${large ? ' szv2-modal-lg' : ''}`}
        style={width ? { width } : undefined}
        onClick={e => e.stopPropagation()}
        onKeyDown={handleKeyDown}
        tabIndex={-1}
      >
        <div className="szv2-modal-head">
          <h3 id={titleId}>{title}</h3>
          <button
            type="button"
            className="szv2-modal-x"
            onClick={onClose}
            aria-label="Fechar modal"
          >
            ✕
          </button>
        </div>
        <div className="szv2-modal-body">
          {children}
        </div>
        {footer && (
          <div className="szv2-modal-foot">
            {footer}
          </div>
        )}
      </div>
    </div>,
    portalTarget,
  )
}
