/**
 * AUDIT-2026-06-18 Onda3 — ConfirmDialog + confirmAsync()
 * Reescrito 2026-06-22: identidade FALK via Tailwind (sem classes szv2-* do
 * portal, que ficavam sem estilo ao serem portadas para document.body fora do
 * escopo .sz-dashboard-v2). Agora usa utilitários Tailwind globais + detecção
 * de tema em JS (lê data-theme de .sz-root), funcionando em claro e escuro.
 *
 * API pública (retrocompatível):
 *   const ok = await confirmAsync({ title, message, confirmLabel?, cancelLabel?, danger?, variant? })
 *   → true  se usuário confirmou
 *   → false se cancelou ou fechou
 *
 * Variantes (opts.variant; default 'confirm'). danger=true mapeia para 'danger'.
 *   - 'danger'  → exclusão: ícone de alerta vermelho, botão confirmar vermelho.
 *   - 'confirm' → ação positiva: ícone check azul, botão confirmar azul (default).
 *   - 'warning' → atenção: ícone aviso âmbar, botão confirmar azul.
 *
 * <ConfirmHost> deve ser montado UMA vez em Layout.tsx (já recebe estado
 * do emitter de módulo — não precisa de contexto React).
 */
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

// ── Tipos ─────────────────────────────────────────────────────────────────────

export type ConfirmVariant = 'danger' | 'confirm' | 'warning'

export interface ConfirmOptions {
  title?: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  /** Retrocompat: equivale a variant: 'danger' (botão confirmar vermelho). */
  danger?: boolean
  /** Variante visual. Default 'confirm'. danger=true tem precedência → 'danger'. */
  variant?: ConfirmVariant
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

// ── Configuração por variante ─────────────────────────────────────────────────

function resolveVariant(opts: ConfirmOptions): ConfirmVariant {
  // danger=true tem precedência (retrocompat com chamadas antigas).
  if (opts.danger) return 'danger'
  return opts.variant ?? 'confirm'
}

const VARIANT_TITLE: Record<ConfirmVariant, string> = {
  danger: 'Confirmar exclusão',
  confirm: 'Confirmar ação',
  warning: 'Atenção',
}

const VARIANT_CONFIRM_LABEL: Record<ConfirmVariant, string> = {
  danger: 'Excluir',
  confirm: 'Confirmar',
  warning: 'Continuar',
}

// Ícone (em círculo) por variante. Cores chumbadas na marca FALK.
function VariantIcon({ variant }: { variant: ConfirmVariant }) {
  if (variant === 'danger') {
    return (
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-red-100 ring-1 ring-red-200">
        <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6 text-red-600" aria-hidden="true">
          <path d="M12 9v4M12 17h.01M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"
            stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </div>
    )
  }
  if (variant === 'warning') {
    return (
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-amber-100 ring-1 ring-amber-200">
        <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6 text-amber-500" aria-hidden="true">
          <path d="M12 9v4M12 17h.01M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"
            stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </div>
    )
  }
  // confirm (default) — check azul FALK
  return (
    <div className="flex h-12 w-12 items-center justify-center rounded-full bg-falk-blue/15 ring-1 ring-falk-blue/25">
      <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6 text-falk-blue" aria-hidden="true">
        <path d="M20 6 9 17l-5-5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </div>
  )
}

// Detecta o tema atual lido do root .sz-root[data-theme]. O toggle do Layout
// usa data-theme (não a classe .dark do Tailwind), portanto não dá pra usar
// variantes dark: — resolvemos as classes em JS no momento de abrir.
function readIsDark(): boolean {
  return document.querySelector('.sz-root')?.getAttribute('data-theme') === 'dark'
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
  const [isDark, setIsDark] = useState(false)
  const [shown, setShown] = useState(false) // flag de animação de entrada
  const confirmBtnRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  // Liga o emitter de módulo ao estado local.
  useEffect(() => {
    _setState = (p) => {
      if (p) setIsDark(readIsDark()) // captura o tema no momento de abrir
      setDialog(p)
    }
    return () => { _setState = null }
  }, [])

  // Foco no botão CONFIRMAR ao abrir + dispara animação de entrada.
  useEffect(() => {
    if (dialog) {
      setShown(false)
      requestAnimationFrame(() => {
        setShown(true)
        confirmBtnRef.current?.focus()
      })
    } else {
      setShown(false)
    }
  }, [dialog])

  // Escape cancela.
  useEffect(() => {
    if (!dialog) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') { e.stopPropagation(); respond(false) }
    }
    document.addEventListener('keydown', onKeyDown, true)
    return () => document.removeEventListener('keydown', onKeyDown, true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dialog])

  // Focus trap dentro do diálogo.
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
  const variant = resolveVariant(opts)
  const title = opts.title ?? VARIANT_TITLE[variant]
  const confirmLabel = opts.confirmLabel ?? VARIANT_CONFIRM_LABEL[variant]
  const cancelLabel = opts.cancelLabel ?? 'Cancelar'

  // Botão confirmar: vermelho para danger, azul FALK para confirm/warning.
  const confirmBtnClass = variant === 'danger'
    ? 'bg-red-600 text-white hover:bg-red-700 focus-visible:ring-red-500/40'
    : 'bg-falk-blue text-white hover:bg-falk-bluedark focus-visible:ring-falk-blue/40'

  // Card e textos comutam por tema (data-theme, não dark: do Tailwind).
  const cardClass = isDark
    ? 'bg-falk-graphite border border-falk-line text-falk-steel'
    : 'bg-white border border-slate-200 text-slate-600'
  const titleClass = isDark ? 'text-white' : 'text-falk-ink'
  const messageClass = isDark ? 'text-falk-steel' : 'text-slate-600'
  const cancelBtnClass = isDark
    ? 'border border-falk-line text-falk-steel hover:bg-white/[0.06] focus-visible:ring-falk-steel/30'
    : 'border border-slate-300 text-slate-700 hover:bg-slate-50 focus-visible:ring-slate-400/30'

  return createPortal(
    <div
      className={`fixed inset-0 z-[9999] flex items-center justify-center p-4 bg-black/55 backdrop-blur-sm transition-opacity duration-200 ${shown ? 'opacity-100' : 'opacity-0'}`}
      onClick={() => respond(false)}
      aria-hidden="false"
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="szv2-confirm-title"
        aria-describedby="szv2-confirm-msg"
        className={`w-full max-w-[420px] rounded-2xl p-6 shadow-[0_24px_60px_-12px_rgba(0,0,0,.45)] transition-all duration-200 ${cardClass} ${shown ? 'opacity-100 scale-100 translate-y-0' : 'opacity-0 scale-95 translate-y-2'}`}
        onClick={e => e.stopPropagation()}
        onKeyDown={handleKeyDown}
        tabIndex={-1}
      >
        {/* Ícone */}
        <div className="mb-4 flex justify-center">
          <VariantIcon variant={variant} />
        </div>

        {/* Título */}
        <h3
          id="szv2-confirm-title"
          className={`text-center text-[17px] font-semibold leading-snug ${titleClass}`}
        >
          {title}
        </h3>

        {/* Descrição */}
        <p
          id="szv2-confirm-msg"
          className={`mt-2 whitespace-pre-line text-center text-[13.5px] leading-relaxed ${messageClass}`}
        >
          {opts.message}
        </p>

        {/* Botões */}
        <div className="mt-6 flex gap-3">
          <button
            type="button"
            onClick={() => respond(false)}
            className={`flex-1 rounded-xl px-4 py-2.5 text-sm font-medium bg-transparent transition focus:outline-none focus-visible:ring-2 ${cancelBtnClass}`}
          >
            {cancelLabel}
          </button>
          <button
            ref={confirmBtnRef}
            type="button"
            onClick={() => respond(true)}
            className={`flex-1 rounded-xl px-4 py-2.5 text-sm font-semibold shadow-sm transition focus:outline-none focus-visible:ring-2 ${confirmBtnClass}`}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
