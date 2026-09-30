// AUDIT-2026-06-19 — Motor compartilhado de multi-seleção + ações em lote.
//
// As filas de aprovação (Saques COD, Onboarding, Pedidos) ganham seleção por
// checkbox + barra de ação sticky no rodapé, espelhando o padrão já validado em
// BulkActions.tsx (etiquetas). Como o backend NÃO expõe endpoints em lote, cada
// ação é um loop client-side sobre os endpoints por-ID existentes (ver
// runBulk()). Só linhas acionáveis devem ser selecionáveis — quem consome filtra
// os ids elegíveis.
//
// Uso:
//   const bulk = useBulkSelection(visibleActionableIds)
//   <input type="checkbox" checked={bulk.has(id)} onChange={() => bulk.toggle(id)} />
//   ...
//   <BulkBar
//     count={bulk.size}
//     onClear={bulk.clear}
//     actions={[
//       { label: 'Aprovar', variant: 'brand', onClick: () => doApprove(bulk.ids) },
//       { label: 'Rejeitar', variant: 'danger', onClick: () => doReject(bulk.ids) },
//     ]}
//     busy={busy}
//   />

import type { ReactNode } from 'react'
import { useCallback, useMemo, useState } from 'react'

// Hook de seleção por id. `selectableIds` define o universo elegível p/ o
// "selecionar todos" e mantém a seleção consistente quando a lista recarrega.
export function useBulkSelection(selectableIds: number[]) {
  const [selected, setSelected] = useState<Set<number>>(new Set())

  const selectableSet = useMemo(() => new Set(selectableIds), [selectableIds])

  const toggle = useCallback((id: number) => {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }, [])

  const clear = useCallback(() => setSelected(new Set()), [])

  const toggleAll = useCallback(() => {
    setSelected(prev =>
      prev.size === selectableSet.size && selectableSet.size > 0
        ? new Set()
        : new Set(selectableSet),
    )
  }, [selectableSet])

  // Garante que a seleção só contenha ids ainda elegíveis (ex.: após reload).
  const ids = useMemo(
    () => Array.from(selected).filter(id => selectableSet.has(id)),
    [selected, selectableSet],
  )

  const allSelected = selectableSet.size > 0 && ids.length === selectableSet.size
  const someSelected = ids.length > 0 && !allSelected

  return {
    selected,
    ids,
    size: ids.length,
    has: (id: number) => selected.has(id),
    toggle,
    toggleAll,
    clear,
    allSelected,
    someSelected,
  }
}

// runBulk — executa `fn(id)` para cada id e contabiliza ok/falha. Sequencial
// (não paraleliza) para não estourar o backend e manter ordem previsível.
// Retorna {ok, fail, errors} — o caller mostra o toast e recarrega.
export async function runBulk(
  ids: number[],
  fn: (id: number) => Promise<unknown>,
): Promise<{ ok: number; fail: number; errors: string[] }> {
  let ok = 0
  let fail = 0
  const errors: string[] = []
  for (const id of ids) {
    try {
      await fn(id)
      ok++
    } catch (e: unknown) {
      fail++
      errors.push(`#${id}: ${(e as Error)?.message || 'erro'}`)
    }
  }
  return { ok, fail, errors }
}

type BulkActionDef = {
  label: string
  onClick: () => void
  variant?: 'brand' | 'secondary' | 'danger'
  disabled?: boolean
  title?: string
}

type BulkBarProps = {
  count: number
  onClear: () => void
  actions: BulkActionDef[]
  busy?: boolean
  /** Rótulo singular do item (default "selecionado"). */
  noun?: string
  /** Plural explícito (PT-BR não pluraliza só com "s"). Default = noun + "s". */
  nounPlural?: string
  /** Conteúdo extra (ex.: seletor de motoboy) — renderizado antes dos botões, sempre visível junto das ações. */
  children?: ReactNode
}

const VARIANT_CLS: Record<NonNullable<BulkActionDef['variant']>, string> = {
  brand:     'szv2-btn szv2-btn-brand',
  secondary: 'szv2-btn szv2-btn-secondary',
  danger:    'szv2-btn szv2-btn-danger',
}

// Barra de ação fixa no rodapé — só aparece com seleção ativa. Espelha o sticky
// footer de BulkActions.tsx (mesmo visual/posição).
export default function BulkBar({ count, onClear, actions, busy, noun = 'selecionado', nounPlural, children }: BulkBarProps) {
  if (count <= 0) return null
  const label = count === 1 ? noun : (nounPlural ?? `${noun}s`)
  return (
    <div
      style={{
        position: 'fixed',
        bottom: 24,
        left: '50%',
        transform: 'translateX(-50%)',
        background: 'var(--szv2-card-bg, #fff)',
        border: '1.5px solid var(--szv2-border)',
        borderRadius: 12,
        boxShadow: '0 8px 32px rgba(0,0,0,.18)',
        padding: '12px 20px',
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        zIndex: 1000,
        flexWrap: 'wrap',
      }}
      role="region"
      aria-label="Ações em lote"
    >
      <span style={{ fontWeight: 700, color: 'var(--szv2-brand)', whiteSpace: 'nowrap' }}>
        {count} {label}
      </span>
      {children}
      {actions.map((a, i) => (
        <button
          key={i}
          type="button"
          className={VARIANT_CLS[a.variant ?? 'secondary']}
          onClick={a.onClick}
          disabled={busy || a.disabled}
          title={a.title}
        >
          {a.label}
        </button>
      ))}
      <button
        type="button"
        className="szv2-btn szv2-btn-secondary"
        onClick={onClear}
        disabled={busy}
        style={{ marginLeft: 4 }}
      >
        Limpar seleção
      </button>
    </div>
  )
}
