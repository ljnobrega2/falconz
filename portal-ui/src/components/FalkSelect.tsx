// FalkSelect — select CUSTOM no padrão do site (identidade FALK, azul #1E6FF2).
// Substitui o dropdown nativo do SO por um botão + lista de opções estilizada,
// com suporte a tema claro/escuro, navegação por teclado, acessibilidade ARIA e
// fechamento ao clicar fora.
//
// API drop-in (compatível com <select> nativo simples):
//   <FalkSelect
//     value={value}
//     onChange={(v) => setValue(v)}
//     options={[{ value: '', label: 'Todos' }, { value: 'a', label: 'Opção A' }]}
//     placeholder="Selecione"
//   />
//
// `onChange` recebe a STRING do value (não um evento) — mais simples que o
// nativo. Onde o consumidor usa `e.target.value`, troque por `v` direto.
//
// Tokens: usa as variáveis --szv2-* (definidas sob .sz-dashboard-v2 em
// styles/tokens.css), então herda tema claro e escuro automaticamente.

import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import { createPortal } from 'react-dom'

export type FalkSelectOption = {
  value: string
  label: string
  /** Desabilita a opção (não selecionável, atenuada). */
  disabled?: boolean
}

type FalkSelectProps = {
  value: string
  onChange: (value: string) => void
  options: FalkSelectOption[]
  placeholder?: string
  /** Desabilita o select inteiro. */
  disabled?: boolean
  /** Largura. Default 100% (preenche o FilterField). */
  style?: CSSProperties
  /** id do botão de trigger (para <label htmlFor>). */
  id?: string
  /** rótulo acessível quando não há <label> associado. */
  'aria-label'?: string
  /** className extra no wrapper. */
  className?: string
}

export default function FalkSelect({
  value,
  onChange,
  options,
  placeholder = 'Selecione',
  disabled = false,
  style,
  id,
  'aria-label': ariaLabel,
  className,
}: FalkSelectProps) {
  const [open, setOpen] = useState(false)
  // Índice destacado via teclado (-1 = nenhum).
  const [activeIdx, setActiveIdx] = useState<number>(-1)
  const wrapRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  // Posição fixa do dropdown (portal no body) — evita clipping dentro de
  // containers com overflow:hidden/auto (ex.: FilterTopPanel body rolável).
  const [pos, setPos] = useState<{
    left: number
    top: number
    width: number
    openUp: boolean
    maxHeight: number
  } | null>(null)
  // Tema do ancestral .sz-dashboard-v2 — replicado no wrapper do portal para
  // que as variáveis --szv2-* resolvam fora da árvore do app (portal no body).
  const [portalTheme, setPortalTheme] = useState<string>('')
  const autoId = useId()
  const listboxId = `${id ?? 'falk-select'}-${autoId}-listbox`

  const selected = options.find(o => o.value === value)
  const currentIdx = options.findIndex(o => o.value === value)

  // Calcula a posição do dropdown a partir do retângulo do trigger.
  // Abre para baixo por padrão; se não couber, abre para cima.
  function computePosition() {
    const el = triggerRef.current
    if (!el) return
    const r = el.getBoundingClientRect()
    const vh = window.innerHeight
    const gap = 4
    const desired = 260
    const spaceBelow = vh - r.bottom - gap - 8
    const spaceAbove = r.top - gap - 8
    const openUp = spaceBelow < Math.min(desired, 160) && spaceAbove > spaceBelow
    const maxHeight = Math.max(
      120,
      Math.min(desired, openUp ? spaceAbove : spaceBelow),
    )
    setPos({
      left: r.left,
      top: openUp ? r.top - gap : r.bottom + gap,
      width: r.width,
      openUp,
      maxHeight,
    })
  }

  // Posiciona ao abrir e reposiciona em scroll/resize (qualquer ancestral).
  useLayoutEffect(() => {
    if (!open) {
      setPos(null)
      return
    }
    computePosition()
    // Captura o tema do ancestral .sz-dashboard-v2 (claro/escuro).
    const scope = triggerRef.current?.closest('.sz-dashboard-v2')
    setPortalTheme(scope?.getAttribute('data-theme') ?? '')
    function onReflow() {
      computePosition()
    }
    window.addEventListener('scroll', onReflow, true)
    window.addEventListener('resize', onReflow)
    return () => {
      window.removeEventListener('scroll', onReflow, true)
      window.removeEventListener('resize', onReflow)
    }
  }, [open])

  // Fecha ao clicar fora (considera o trigger E a lista portada).
  useEffect(() => {
    if (!open) return
    function onDocClick(e: MouseEvent) {
      const t = e.target as Node
      if (wrapRef.current?.contains(t)) return
      if (listRef.current?.contains(t)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', onDocClick)
    return () => document.removeEventListener('mousedown', onDocClick)
  }, [open])

  // Ao abrir, destaca a opção selecionada (ou a primeira habilitada).
  useEffect(() => {
    if (!open) {
      setActiveIdx(-1)
      return
    }
    const start =
      currentIdx >= 0 ? currentIdx : options.findIndex(o => !o.disabled)
    setActiveIdx(start)
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  // Mantém a opção destacada visível na rolagem.
  useEffect(() => {
    if (!open || activeIdx < 0 || !listRef.current) return
    const el = listRef.current.children[activeIdx] as HTMLElement | undefined
    el?.scrollIntoView({ block: 'nearest' })
  }, [activeIdx, open])

  function commit(idx: number) {
    const opt = options[idx]
    if (!opt || opt.disabled) return
    onChange(opt.value)
    setOpen(false)
  }

  function moveActive(dir: 1 | -1) {
    if (options.length === 0) return
    let i = activeIdx
    for (let step = 0; step < options.length; step++) {
      i = (i + dir + options.length) % options.length
      if (!options[i].disabled) {
        setActiveIdx(i)
        return
      }
    }
  }

  function onTriggerKeyDown(e: React.KeyboardEvent) {
    if (disabled) return
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        if (!open) setOpen(true)
        else moveActive(1)
        break
      case 'ArrowUp':
        e.preventDefault()
        if (!open) setOpen(true)
        else moveActive(-1)
        break
      case 'Enter':
      case ' ':
        e.preventDefault()
        if (!open) setOpen(true)
        else if (activeIdx >= 0) commit(activeIdx)
        break
      case 'Escape':
        if (open) {
          e.preventDefault()
          setOpen(false)
        }
        break
      case 'Home':
        if (open) {
          e.preventDefault()
          const first = options.findIndex(o => !o.disabled)
          if (first >= 0) setActiveIdx(first)
        }
        break
      case 'End':
        if (open) {
          e.preventDefault()
          for (let i = options.length - 1; i >= 0; i--) {
            if (!options[i].disabled) {
              setActiveIdx(i)
              break
            }
          }
        }
        break
      case 'Tab':
        if (open) setOpen(false)
        break
    }
  }

  const wrapStyle: CSSProperties = {
    position: 'relative',
    width: '100%',
    ...style,
  }

  const triggerStyle: CSSProperties = {
    height: 38,
    padding: '0 36px 0 12px',
    background: 'var(--szv2-surface-alt)',
    border: `1px solid ${open ? 'var(--szv2-brand)' : 'var(--szv2-border)'}`,
    borderRadius: 10,
    color: selected ? 'var(--szv2-text)' : 'var(--szv2-text-faint)',
    font: 'inherit',
    fontSize: 13,
    width: '100%',
    boxSizing: 'border-box',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
    cursor: disabled ? 'not-allowed' : 'pointer',
    opacity: disabled ? 0.6 : 1,
    textAlign: 'left',
    outline: 'none',
    boxShadow: open ? 'var(--szv2-shadow-focus)' : 'none',
    transition: 'border-color 120ms ease, box-shadow 120ms ease',
  }

  return (
    <div ref={wrapRef} className={className} style={wrapStyle}>
      <button
        ref={triggerRef}
        type="button"
        id={id}
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listboxId}
        aria-label={ariaLabel}
        aria-disabled={disabled || undefined}
        disabled={disabled}
        style={triggerStyle}
        onClick={() => !disabled && setOpen(o => !o)}
        onKeyDown={onTriggerKeyDown}
      >
        <span
          style={{
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {selected ? selected.label : placeholder}
        </span>
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          aria-hidden="true"
          style={{
            flexShrink: 0,
            color: 'var(--szv2-text-muted)',
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 140ms ease',
          }}
        >
          <path
            d="M6 9l6 6 6-6"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </button>

      {open &&
        pos &&
        createPortal(
          <div
            className="sz-dashboard-v2"
            data-theme={portalTheme || undefined}
            style={{
              position: 'fixed',
              left: pos.left,
              top: pos.openUp ? undefined : pos.top,
              bottom: pos.openUp ? window.innerHeight - pos.top : undefined,
              width: pos.width,
              // O wrapper só usa .sz-dashboard-v2 para resolver os tokens --szv2-*
              // do tema. Mas essa classe-base traz `display:flex` + `min-height:100vh`
              // (layout do app). Aplicados aqui, o div fixed ficava 100vh de altura e
              // o <ul> (flex item, align-items:stretch) ESTICAVA até o teto maxHeight,
              // deixando a caixa enorme com espaço vazio mesmo com 1 opção. Forçamos
              // block + min-height:0 para a caixa caber no conteúdo (maxHeight = só teto).
              display: 'block',
              minHeight: 0,
              // z-index acima do DetailDrawer (painel = 1001). O dropdown é portado
              // p/ document.body (irmão do drawer), então com 1000 ele pintava ATRÁS
              // do painel opaco do drawer — opções invisíveis/inclicáveis (ex.: SELECT
              // de motoboy em Ações em lote). 1100 fica acima do drawer e abaixo de
              // CommandPalette/confirm (9998+), que devem sobrepor o select.
              zIndex: 1100,
            }}
          >
            <ul
              ref={listRef}
              id={listboxId}
              role="listbox"
              tabIndex={-1}
              style={{
                margin: 0,
                padding: 4,
                listStyle: 'none',
                background: 'var(--szv2-surface)',
                border: '1px solid var(--szv2-border)',
                borderRadius: 10,
                boxShadow:
                  'var(--szv2-shadow-float, 0 12px 30px rgba(15,23,42,.12))',
                maxHeight: pos.maxHeight,
                overflowY: 'auto',
              }}
            >
          {options.map((opt, idx) => {
            const isSelected = opt.value === value
            const isActive = idx === activeIdx
            return (
              <li
                key={`${opt.value}-${idx}`}
                role="option"
                aria-selected={isSelected}
                aria-disabled={opt.disabled || undefined}
                onMouseEnter={() => !opt.disabled && setActiveIdx(idx)}
                onMouseDown={e => e.preventDefault() /* não perde foco do trigger */}
                onClick={() => commit(idx)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 8,
                  padding: '8px 10px',
                  borderRadius: 8,
                  fontSize: 13,
                  lineHeight: 1.3,
                  cursor: opt.disabled ? 'not-allowed' : 'pointer',
                  color: opt.disabled
                    ? 'var(--szv2-text-faint)'
                    : isSelected
                      ? 'var(--szv2-brand)'
                      : 'var(--szv2-text)',
                  fontWeight: isSelected ? 700 : 500,
                  background: isActive
                    ? 'var(--szv2-brand-light, rgba(30,111,242,.10))'
                    : 'transparent',
                  opacity: opt.disabled ? 0.6 : 1,
                  transition: 'background 100ms ease',
                }}
              >
                <span
                  style={{
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {opt.label}
                </span>
                {isSelected && (
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    aria-hidden="true"
                    style={{ flexShrink: 0, color: 'var(--szv2-brand)' }}
                  >
                    <path
                      d="M5 13l4 4L19 7"
                      stroke="currentColor"
                      strokeWidth="2.5"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                )}
              </li>
            )
          })}
          {options.length === 0 && (
            <li
              role="option"
              aria-selected={false}
              aria-disabled
              style={{
                padding: '8px 10px',
                fontSize: 13,
                color: 'var(--szv2-text-faint)',
              }}
            >
              Nenhuma opção
            </li>
          )}
            </ul>
          </div>,
          document.body,
        )}
    </div>
  )
}
