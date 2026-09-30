/**
 * AUDIT-2026-06-18 Onda3 — Toast centralizado
 * Context + hook para sistema de toast global empilhável.
 * Um emitter de módulo permite disparar toasts de fora do React tree (api.ts).
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from 'react'

// ── Tipos públicos ────────────────────────────────────────────────────────────

export type ToastKind = 'ok' | 'err' | 'warn' | 'info'

export interface ToastItem {
  id: number
  kind: ToastKind
  msg: string
}

export type ShowToastFn = (kind: ToastKind, msg: string) => void

// ── Emitter de módulo (para api.ts e outros contextos fora do React tree) ─────

type EmitterListener = ShowToastFn

let _listener: EmitterListener | null = null
let _idSeq = 0

/** Registra um listener global. Apenas um por vez (o ToastHost). */
export function _registerToastListener(fn: EmitterListener) {
  _listener = fn
}
export function _unregisterToastListener() {
  _listener = null
}

/**
 * Dispara um toast globalmente.
 * Funciona mesmo fora da árvore React (ex.: api.ts).
 * Se o ToastHost ainda não montou, o toast é silenciosamente descartado.
 */
export function emitToast(kind: ToastKind, msg: string) {
  _listener?.(kind, msg)
}

// ── Contexto ──────────────────────────────────────────────────────────────────

interface ToastCtx {
  toasts: ToastItem[]
  showToast: ShowToastFn
  dismissToast: (id: number) => void
}

const Ctx = createContext<ToastCtx | null>(null)

// ── Provider ──────────────────────────────────────────────────────────────────

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([])

  const showToast = useCallback<ShowToastFn>((kind, msg) => {
    const id = ++_idSeq
    setToasts(prev => [...prev, { id, kind, msg }])
  }, [])

  const dismissToast = useCallback((id: number) => {
    setToasts(prev => prev.filter(t => t.id !== id))
  }, [])

  // Registra no emitter global
  useEffect(() => {
    _registerToastListener(showToast)
    return () => _unregisterToastListener()
  }, [showToast])

  return (
    <Ctx.Provider value={{ toasts, showToast, dismissToast }}>
      {children}
    </Ctx.Provider>
  )
}

// ── Hook ──────────────────────────────────────────────────────────────────────

/**
 * useToast() — retorna `showToast(kind, msg)` para usar dentro de componentes.
 * Deve ser usado dentro da árvore envolta por <ToastProvider>.
 */
export function useToast(): ShowToastFn {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('useToast deve ser usado dentro de <ToastProvider>')
  return ctx.showToast
}

/** Hook para o ToastHost acessar a lista + dismiss. */
export function useToastList() {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('useToastList deve ser usado dentro de <ToastProvider>')
  return ctx
}
