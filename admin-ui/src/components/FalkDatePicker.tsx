// FalkDatePicker — date picker CUSTOM no padrão do site (identidade FALK, azul #1E6FF2).
// Substitui o calendário NATIVO do SO (<input type="date">) por um botão + calendário
// estilizado, com suporte a tema claro/escuro, navegação por teclado, acessibilidade
// ARIA e fechamento ao clicar fora / ESC.
//
// API drop-in (compatível com <input type="date"> — facilita a troca em massa):
//   <FalkDatePicker
//     value={stringYYYYMMDD}          // 'YYYY-MM-DD' ou '' (MESMO formato do nativo)
//     onChange={(v) => setX(v)}       // emite 'YYYY-MM-DD' (ou '' ao limpar) — STRING
//     placeholder="dd/mm/aaaa"        // opcional
//     min="2024-01-01" max="2030-12-31" // opcionais 'YYYY-MM-DD'
//     disabled={bool} style={...} id={...} aria-label={...}
//   />
//
// `onChange` recebe a STRING do value (não um evento) — mais simples que o nativo.
// Onde o consumidor usa `e.target.value`, troque por `v` direto.
//
// Tokens: usa as variáveis --szv2-* (definidas sob .sz-dashboard-v2 em
// styles/tokens.css), então herda tema claro e escuro automaticamente.
//
// Date/timezone: o calendário é construído com o construtor NUMÉRICO `new Date(y, m, d)`
// (hora local, sem o pitfall UTC de `new Date('YYYY-MM-DD')`). A string de entrada é
// parseada em inteiros Y/M/D via regex. "Hoje" usa getFullYear/getMonth/getDate (local).

import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import { createPortal } from 'react-dom'

type FalkDatePickerProps = {
  /** Valor em 'YYYY-MM-DD' ou '' (mesmo formato do <input type="date"> nativo). */
  value: string
  /** Emite 'YYYY-MM-DD' ao selecionar ou '' ao limpar — STRING, não evento. */
  onChange: (value: string) => void
  /** Texto exibido quando não há data selecionada. */
  placeholder?: string
  /** Data mínima selecionável em 'YYYY-MM-DD'. Dias anteriores ficam desabilitados. */
  min?: string
  /** Data máxima selecionável em 'YYYY-MM-DD'. Dias posteriores ficam desabilitados. */
  max?: string
  /**
   * Predicado opcional de desabilitação por data (além de min/max). Recebe a data
   * em 'YYYY-MM-DD'; retornar `true` deixa o dia DESABILITADO (não clicável, atenuado).
   * Usado p/ regra de zona (dias de funcionamento + cutoff) no reagendamento.
   * NÃO quebra a API atual — prop opcional; ausente = só min/max valem.
   */
  isDateDisabled?: (iso: string) => boolean
  /** Desabilita o picker inteiro. */
  disabled?: boolean
  /** Largura/estilo do wrapper. Default 100% (preenche o FilterField). */
  style?: CSSProperties
  /** id do botão de trigger (para <label htmlFor>). */
  id?: string
  /** rótulo acessível quando não há <label> associado. */
  'aria-label'?: string
  /** className extra no wrapper. */
  className?: string
}

// ── Helpers de data (sem pitfall UTC) ─────────────────────────────────────────

type YMD = { y: number; m: number; d: number } // m = 0..11 (índice de mês)

const WEEKDAYS = ['D', 'S', 'T', 'Q', 'Q', 'S', 'S'] // domingo-primeiro (getDay() 0..6)
const MONTHS = [
  'Janeiro',
  'Fevereiro',
  'Março',
  'Abril',
  'Maio',
  'Junho',
  'Julho',
  'Agosto',
  'Setembro',
  'Outubro',
  'Novembro',
  'Dezembro',
]
// Abreviações p/ a grade do seletor rápido de mês (3 letras, Title Case).
const MONTHS_SHORT = [
  'Jan',
  'Fev',
  'Mar',
  'Abr',
  'Mai',
  'Jun',
  'Jul',
  'Ago',
  'Set',
  'Out',
  'Nov',
  'Dez',
]

const ISO_RE = /^(\d{4})-(\d{2})-(\d{2})$/

/** Parseia 'YYYY-MM-DD' em inteiros (m = 0..11). Retorna null se vazio/inválido. */
function parseISO(s: string | undefined): YMD | null {
  if (!s) return null
  const mt = ISO_RE.exec(s.trim())
  if (!mt) return null
  const y = Number(mt[1])
  const m = Number(mt[2]) - 1
  const d = Number(mt[3])
  if (m < 0 || m > 11 || d < 1 || d > 31) return null
  return { y, m, d }
}

/** Formata inteiros em 'YYYY-MM-DD' zero-padded (m = 0..11). */
function toISO(y: number, m: number, d: number): string {
  const mm = String(m + 1).padStart(2, '0')
  const dd = String(d).padStart(2, '0')
  return `${y}-${mm}-${dd}`
}

/** Formata 'YYYY-MM-DD' em 'DD/MM/AAAA' pt-BR. '' se vazio/inválido. */
function toBR(s: string): string {
  const p = parseISO(s)
  if (!p) return ''
  return `${String(p.d).padStart(2, '0')}/${String(p.m + 1).padStart(2, '0')}/${p.y}`
}

/** Hoje em hora LOCAL (getFullYear/getMonth/getDate). */
function todayLocal(): YMD {
  const n = new Date()
  return { y: n.getFullYear(), m: n.getMonth(), d: n.getDate() }
}

// Modo interno do painel: grade de dias (default) ou seletores rápidos.
type PickerMode = 'days' | 'months' | 'years'

// Token de hover sutil da marca — reaproveita --szv2-brand-light, com fallback rgba.
const HOVER_BG = 'var(--szv2-brand-light, rgba(30,111,242,.10))'

export default function FalkDatePicker({
  value,
  onChange,
  placeholder = 'dd/mm/aaaa',
  min,
  max,
  isDateDisabled,
  disabled = false,
  style,
  id,
  'aria-label': ariaLabel,
  className,
}: FalkDatePickerProps) {
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const popRef = useRef<HTMLDivElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  // Mês exibido no calendário ({year, month}) — separado do `value` selecionado.
  const [view, setView] = useState<{ y: number; m: number }>(() => {
    const p = parseISO(value)
    const t = p ?? todayLocal()
    return { y: t.y, m: t.m }
  })
  // Dia destacado via teclado em 'YYYY-MM-DD' ('' = nenhum).
  const [activeISO, setActiveISO] = useState<string>('')
  // Controle sob o ponteiro (feedback de hover, separado do active-teclado).
  // Guarda uma chave livre: 'iso' do dia, 'trigger', 'prev'/'next'/'caption',
  // 'clear'/'today', ou 'm<idx>' / 'y<ano>' nos seletores rápidos.
  const [hoverKey, setHoverKey] = useState<string>('')
  // Painel ativo: grade de dias, ou seletor rápido de mês/ano.
  const [mode, setMode] = useState<PickerMode>('days')

  // Posição fixa do popup (portal no body) — evita clipping em containers com
  // overflow (ex.: FilterTopPanel rolável). Reposiciona em scroll/resize.
  const [pos, setPos] = useState<{
    left: number
    top: number
    openUp: boolean
  } | null>(null)
  // Tema do ancestral .sz-dashboard-v2 — replicado no wrapper do portal para que
  // as variáveis --szv2-* resolvam fora da árvore do app (portal no body).
  const [portalTheme, setPortalTheme] = useState<string>('')
  const autoId = useId()
  const dialogId = `${id ?? 'falk-date'}-${autoId}-dialog`

  const today = useMemo(() => todayLocal(), [])
  const todayISO = toISO(today.y, today.m, today.d)
  const selected = parseISO(value)

  // Dimensões fixas do calendário (não rola — altura previsível p/ flip openUp).
  // O painel ficou mais largo e respirável; a altura cobre o painel mais alto
  // (grade de dias ~ a grade de meses/anos) p/ o cálculo de flip não "vazar".
  const POP_WIDTH = 296
  const POP_HEIGHT = 360

  // Calcula posição a partir do retângulo do trigger. Abre para baixo; se não
  // couber, abre para cima. Garante que o popup não saia da viewport à direita.
  function computePosition() {
    const el = triggerRef.current
    if (!el) return
    const r = el.getBoundingClientRect()
    const vw = window.innerWidth
    const vh = window.innerHeight
    const gap = 6
    const spaceBelow = vh - r.bottom - gap - 8
    const spaceAbove = r.top - gap - 8
    const openUp = spaceBelow < POP_HEIGHT && spaceAbove > spaceBelow
    let left = r.left
    if (left + POP_WIDTH > vw - 8) left = Math.max(8, vw - 8 - POP_WIDTH)
    // Clamp: em telas curtas (drawer mobile) nem sempre sobra POP_HEIGHT de espaço
    // em nenhuma direção — trava dentro da viewport em vez de deixar vazar/cortar.
    let top = openUp ? r.top - gap : r.bottom + gap
    if (!openUp) top = Math.min(top, vh - 8 - POP_HEIGHT)
    top = Math.max(8, top)
    setPos({
      left,
      top,
      openUp,
    })
  }

  // Posiciona ao abrir, sincroniza mês exibido e captura o tema do ancestral.
  useLayoutEffect(() => {
    if (!open) {
      setPos(null)
      setMode('days')
      setHoverKey('')
      return
    }
    const p = parseISO(value)
    const base = p ?? todayLocal()
    setView({ y: base.y, m: base.m })
    setActiveISO(p ? value : todayISO)
    setMode('days')
    computePosition()
    const scope = triggerRef.current?.closest('.sz-dashboard-v2')
    setPortalTheme(scope?.getAttribute('data-theme') ?? '')
    // Foca o dialog (após o portal montar) para que a navegação por teclado
    // (setas/PageUp-Down/Enter/ESC em onGridKeyDown) seja alcançável.
    // preventScroll: sem isso, o focus() do dialog (portado no body, position:fixed)
    // dispara o scroll-into-view PADRÃO do browser na página inteira — dentro de um
    // drawer curto isso rola o container até o topo do dialog, deixando só o rodapé
    // (Limpar/Hoje) visível e o resto (grade de dias, input) fora da viewport.
    const focusRaf = requestAnimationFrame(() =>
      dialogRef.current?.focus({ preventScroll: true }),
    )
    function onReflow() {
      computePosition()
    }
    window.addEventListener('scroll', onReflow, true)
    window.addEventListener('resize', onReflow)
    return () => {
      cancelAnimationFrame(focusRaf)
      window.removeEventListener('scroll', onReflow, true)
      window.removeEventListener('resize', onReflow)
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  // Fecha ao clicar fora (considera o trigger E o popup portado).
  useEffect(() => {
    if (!open) return
    function onDocClick(e: MouseEvent) {
      const t = e.target as Node
      if (wrapRef.current?.contains(t)) return
      if (popRef.current?.contains(t)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', onDocClick)
    return () => document.removeEventListener('mousedown', onDocClick)
  }, [open])

  // Grade 7×6 = 42 células, construída com o construtor NUMÉRICO (hora local).
  // `new Date(y, m, 1 - firstWeekday + i)` rola dias negativos/overflow p/ os
  // meses adjacentes automaticamente — sem tabela de dias/ano bissexto manual.
  const cells = useMemo(() => {
    const firstWeekday = new Date(view.y, view.m, 1).getDay() // 0=Dom..6=Sáb
    const out: { iso: string; day: number; inMonth: boolean }[] = []
    for (let i = 0; i < 42; i++) {
      const dt = new Date(view.y, view.m, 1 - firstWeekday + i)
      out.push({
        iso: toISO(dt.getFullYear(), dt.getMonth(), dt.getDate()),
        day: dt.getDate(),
        inMonth: dt.getMonth() === view.m && dt.getFullYear() === view.y,
      })
    }
    return out
  }, [view])

  // Janela de anos exibida no seletor rápido (12 anos, centrada no view atual).
  const years = useMemo(() => {
    const start = view.y - 6
    return Array.from({ length: 12 }, (_, i) => start + i)
  }, [view.y])

  // min/max via comparação lexicográfica (ISO zero-padded ordena cronologicamente).
  // isDateDisabled (opcional) é OR-ado aqui — chokepoint único que propaga p/ o
  // render das células, selectISO e o botão "Hoje" automaticamente.
  function isDisabledISO(iso: string): boolean {
    if (min && iso < min) return true
    if (max && iso > max) return true
    if (isDateDisabled && isDateDisabled(iso)) return true
    return false
  }

  function selectISO(iso: string) {
    if (isDisabledISO(iso)) return
    onChange(iso)
    setOpen(false)
    triggerRef.current?.focus()
  }

  function clear() {
    onChange('')
    setOpen(false)
    triggerRef.current?.focus()
  }

  function goToday() {
    setView({ y: today.y, m: today.m })
    setActiveISO(todayISO)
    setMode('days')
  }

  function shiftMonth(delta: number) {
    setView(v => {
      const dt = new Date(v.y, v.m + delta, 1)
      return { y: dt.getFullYear(), m: dt.getMonth() }
    })
  }

  // Move o dia destacado por N dias via construtor numérico; ajusta o mês exibido
  // quando o destaque cruza a fronteira do mês.
  function moveActive(deltaDays: number) {
    const base = parseISO(activeISO) ?? selected ?? today
    const dt = new Date(base.y, base.m, base.d + deltaDays)
    const iso = toISO(dt.getFullYear(), dt.getMonth(), dt.getDate())
    setActiveISO(iso)
    if (dt.getMonth() !== view.m || dt.getFullYear() !== view.y) {
      setView({ y: dt.getFullYear(), m: dt.getMonth() })
    }
  }

  function onTriggerKeyDown(e: React.KeyboardEvent) {
    if (disabled) return
    switch (e.key) {
      case 'ArrowDown':
      case 'Enter':
      case ' ':
        e.preventDefault()
        if (!open) setOpen(true)
        break
      case 'Escape':
        if (open) {
          e.preventDefault()
          setOpen(false)
        }
        break
    }
  }

  function onGridKeyDown(e: React.KeyboardEvent) {
    // Nos seletores rápidos de mês/ano, ESC volta p/ a grade de dias; demais
    // teclas de navegação por dia não se aplicam (evita mover destaque oculto).
    if (mode !== 'days') {
      if (e.key === 'Escape') {
        e.preventDefault()
        setMode('days')
      }
      return
    }
    switch (e.key) {
      case 'ArrowLeft':
        e.preventDefault()
        moveActive(-1)
        break
      case 'ArrowRight':
        e.preventDefault()
        moveActive(1)
        break
      case 'ArrowUp':
        e.preventDefault()
        moveActive(-7)
        break
      case 'ArrowDown':
        e.preventDefault()
        moveActive(7)
        break
      case 'PageUp':
        e.preventDefault()
        shiftMonth(-1)
        break
      case 'PageDown':
        e.preventDefault()
        shiftMonth(1)
        break
      case 'Enter':
      case ' ':
        e.preventDefault()
        if (activeISO) selectISO(activeISO)
        break
      case 'Escape':
        e.preventDefault()
        setOpen(false)
        triggerRef.current?.focus()
        break
    }
  }

  const wrapStyle: CSSProperties = {
    position: 'relative',
    width: '100%',
    ...style,
  }

  const display = toBR(value)
  const triggerHover = hoverKey === 'trigger'

  const triggerStyle: CSSProperties = {
    height: 38,
    padding: '0 12px',
    background: 'var(--szv2-surface-alt)',
    border: `1px solid ${
      open || triggerHover ? 'var(--szv2-brand)' : 'var(--szv2-border)'
    }`,
    borderRadius: 10,
    color: display ? 'var(--szv2-text)' : 'var(--szv2-text-faint)',
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
    transition: 'border-color 140ms ease, box-shadow 140ms ease',
  }

  // Caption ("Mês AAAA" / "AAAA" / faixa de anos) — botão que alterna o seletor.
  const captionLabel =
    mode === 'days'
      ? `${MONTHS[view.m]} ${view.y}`
      : mode === 'months'
        ? `${view.y}`
        : `${years[0]} – ${years[years.length - 1]}`

  function onCaptionClick() {
    setMode(m => (m === 'days' ? 'months' : m === 'months' ? 'years' : 'days'))
  }

  // Setas do header: em 'days' navegam mês; em 'months' navegam ano; em 'years'
  // saltam a janela de 12 anos.
  function onPrev() {
    if (mode === 'days') shiftMonth(-1)
    else if (mode === 'months') setView(v => ({ ...v, y: v.y - 1 }))
    else setView(v => ({ ...v, y: v.y - 12 }))
  }
  function onNext() {
    if (mode === 'days') shiftMonth(1)
    else if (mode === 'months') setView(v => ({ ...v, y: v.y + 1 }))
    else setView(v => ({ ...v, y: v.y + 12 }))
  }

  return (
    <div ref={wrapRef} className={className} style={wrapStyle}>
      <button
        ref={triggerRef}
        type="button"
        id={id}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? dialogId : undefined}
        aria-label={ariaLabel}
        aria-disabled={disabled || undefined}
        disabled={disabled}
        style={triggerStyle}
        onMouseEnter={() => !disabled && setHoverKey('trigger')}
        onMouseLeave={() => setHoverKey(k => (k === 'trigger' ? '' : k))}
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
          {display || placeholder}
        </span>
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          aria-hidden="true"
          style={{
            flexShrink: 0,
            color:
              open || triggerHover
                ? 'var(--szv2-brand)'
                : 'var(--szv2-text-muted)',
            transition: 'color 140ms ease',
          }}
        >
          <rect
            x="3"
            y="4"
            width="18"
            height="17"
            rx="2"
            stroke="currentColor"
            strokeWidth="1.8"
          />
          <path
            d="M3 9h18M8 2.5v3M16 2.5v3"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
          />
        </svg>
      </button>

      {open &&
        pos &&
        createPortal(
          <div
            ref={popRef}
            className="sz-dashboard-v2"
            data-theme={portalTheme || undefined}
            style={{
              position: 'fixed',
              left: pos.left,
              top: pos.openUp ? undefined : pos.top,
              bottom: pos.openUp ? window.innerHeight - pos.top : undefined,
              width: POP_WIDTH,
              zIndex: 1000,
              // Override do .sz-dashboard-v2 (min-height:100vh; display:flex) — a classe
              // é reaproveitada AQUI só para herdar as variáveis --szv2-* de tema (o portal
              // escapa a árvore React p/ document.body), mas ela também é a classe-raiz do
              // app inteiro, com layout pensado pra ocupar a tela toda. Sem este override o
              // wrapper vira full-height + flex-stretch, e o dialog (item flex único) estica
              // junto — o popup incha pra ~100vh e, com openUp, cresce pra CIMA a partir da
              // âncora e transborda por cima do viewport (só o rodapé/última fileira ficam
              // visíveis, com um vão enorme entre eles e o conteúdo do drawer abaixo).
              minHeight: 0,
              height: 'auto',
              display: 'block',
            }}
          >
            <div
              ref={dialogRef}
              id={dialogId}
              role="dialog"
              aria-modal="false"
              aria-label="Selecionar data"
              onKeyDown={onGridKeyDown}
              tabIndex={-1}
              style={{
                background: 'var(--szv2-surface)',
                border: '1px solid var(--szv2-border)',
                borderRadius: 14,
                boxShadow:
                  'var(--szv2-shadow-float, 0 12px 30px rgba(15,23,42,.12))',
                padding: 16,
                boxSizing: 'border-box',
                outline: 'none',
                animation: 'szFdpIn 130ms ease',
              }}
            >
              <style>{FDP_KEYFRAMES}</style>

              {/* Cabeçalho: ‹ [Mês AAAA] › */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  marginBottom: 12,
                  gap: 6,
                }}
              >
                <button
                  type="button"
                  aria-label={
                    mode === 'days'
                      ? 'Mês anterior'
                      : mode === 'months'
                        ? 'Ano anterior'
                        : 'Anos anteriores'
                  }
                  onMouseDown={e => e.preventDefault()}
                  onMouseEnter={() => setHoverKey('prev')}
                  onMouseLeave={() => setHoverKey(k => (k === 'prev' ? '' : k))}
                  onClick={onPrev}
                  style={navBtnStyle(hoverKey === 'prev')}
                >
                  <Chevron dir="left" />
                </button>
                <button
                  type="button"
                  aria-label="Escolher mês e ano"
                  onMouseDown={e => e.preventDefault()}
                  onMouseEnter={() => setHoverKey('caption')}
                  onMouseLeave={() =>
                    setHoverKey(k => (k === 'caption' ? '' : k))
                  }
                  onClick={onCaptionClick}
                  style={{
                    flex: 1,
                    height: 30,
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: 5,
                    border: 'none',
                    borderRadius: 9,
                    background:
                      hoverKey === 'caption' ? HOVER_BG : 'transparent',
                    color:
                      hoverKey === 'caption'
                        ? 'var(--szv2-brand)'
                        : 'var(--szv2-text)',
                    fontSize: 14,
                    fontWeight: 700,
                    letterSpacing: '.01em',
                    cursor: 'pointer',
                    outline: 'none',
                    userSelect: 'none',
                    transition: 'background 130ms ease, color 130ms ease',
                  }}
                >
                  {captionLabel}
                  <Caret open={mode !== 'days'} />
                </button>
                <button
                  type="button"
                  aria-label={
                    mode === 'days'
                      ? 'Próximo mês'
                      : mode === 'months'
                        ? 'Próximo ano'
                        : 'Próximos anos'
                  }
                  onMouseDown={e => e.preventDefault()}
                  onMouseEnter={() => setHoverKey('next')}
                  onMouseLeave={() => setHoverKey(k => (k === 'next' ? '' : k))}
                  onClick={onNext}
                  style={navBtnStyle(hoverKey === 'next')}
                >
                  <Chevron dir="right" />
                </button>
              </div>

              {mode === 'days' && (
                <>
                  {/* Linha de dias da semana: D S T Q Q S S */}
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'repeat(7, 1fr)',
                      gap: 4,
                      marginBottom: 6,
                    }}
                  >
                    {WEEKDAYS.map((w, i) => (
                      <div
                        key={i}
                        aria-hidden="true"
                        style={{
                          textAlign: 'center',
                          fontSize: 10.5,
                          fontWeight: 700,
                          letterSpacing: '.06em',
                          textTransform: 'uppercase',
                          color: 'var(--szv2-text-faint)',
                          padding: '2px 0',
                          userSelect: 'none',
                        }}
                      >
                        {w}
                      </div>
                    ))}
                  </div>

                  {/* Grade 7×6 de dias */}
                  <div
                    role="grid"
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'repeat(7, 1fr)',
                      gap: 4,
                    }}
                  >
                    {cells.map(cell => {
                      const isSelected = value === cell.iso
                      const isToday = todayISO === cell.iso
                      const isHover = hoverKey === cell.iso
                      const isDisabled = isDisabledISO(cell.iso)
                      // Fundo: selecionado = brand sólido; hover/active = brand-light.
                      const bg = isSelected
                        ? 'var(--szv2-brand)'
                        : isHover && !isDisabled
                          ? HOVER_BG
                          : 'transparent'
                      const color = isSelected
                        ? 'var(--szv2-on-brand, #fff)'
                        : isDisabled
                          ? 'var(--szv2-text-faint)'
                          : cell.inMonth
                            ? 'var(--szv2-text)'
                            : 'var(--szv2-text-faint)'
                      return (
                        <div
                          key={cell.iso}
                          style={{
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                          }}
                        >
                          <button
                            type="button"
                            role="gridcell"
                            aria-selected={isSelected}
                            aria-current={isToday ? 'date' : undefined}
                            aria-disabled={isDisabled || undefined}
                            disabled={isDisabled}
                            onMouseEnter={() => {
                              if (isDisabled) return
                              setActiveISO(cell.iso)
                              setHoverKey(cell.iso)
                            }}
                            onMouseLeave={() =>
                              setHoverKey(k => (k === cell.iso ? '' : k))
                            }
                            onMouseDown={e => e.preventDefault()}
                            onClick={() => selectISO(cell.iso)}
                            style={{
                              width: 36,
                              height: 36,
                              display: 'inline-flex',
                              alignItems: 'center',
                              justifyContent: 'center',
                              padding: 0,
                              // HOJE não-selecionado: anel fino brand. Selecionado
                              // já tem o disco brand, então sem borda extra.
                              border:
                                isToday && !isSelected
                                  ? '1.5px solid var(--szv2-brand)'
                                  : '1.5px solid transparent',
                              borderRadius: '50%',
                              fontSize: 13.5,
                              fontWeight: isSelected ? 600 : isToday ? 600 : 500,
                              cursor: isDisabled ? 'not-allowed' : 'pointer',
                              background: bg,
                              color,
                              opacity: isDisabled ? 0.45 : cell.inMonth ? 1 : 0.5,
                              boxShadow: isSelected
                                ? '0 2px 8px rgba(30,111,242,.30)'
                                : 'none',
                              outline: 'none',
                              transition:
                                'background 120ms ease, color 120ms ease, box-shadow 120ms ease, border-color 120ms ease',
                            }}
                          >
                            {cell.day}
                          </button>
                        </div>
                      )
                    })}
                  </div>
                </>
              )}

              {mode === 'months' && (
                // Seletor rápido de MÊS: grade 3×4.
                <div
                  role="grid"
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(3, 1fr)',
                    gap: 6,
                    minHeight: 232,
                  }}
                >
                  {MONTHS_SHORT.map((mlabel, mi) => {
                    const isCur = view.m === mi
                    const isHover = hoverKey === `m${mi}`
                    return (
                      <button
                        key={mi}
                        type="button"
                        aria-label={MONTHS[mi]}
                        aria-pressed={isCur}
                        onMouseDown={e => e.preventDefault()}
                        onMouseEnter={() => setHoverKey(`m${mi}`)}
                        onMouseLeave={() =>
                          setHoverKey(k => (k === `m${mi}` ? '' : k))
                        }
                        onClick={() => {
                          setView(v => ({ ...v, m: mi }))
                          setMode('days')
                        }}
                        style={quickCellStyle(isCur, isHover)}
                      >
                        {mlabel}
                      </button>
                    )
                  })}
                </div>
              )}

              {mode === 'years' && (
                // Seletor rápido de ANO: grade 3×4 (janela de 12 anos).
                <div
                  role="grid"
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(3, 1fr)',
                    gap: 6,
                    minHeight: 232,
                  }}
                >
                  {years.map(yr => {
                    const isCur = view.y === yr
                    const isHover = hoverKey === `y${yr}`
                    return (
                      <button
                        key={yr}
                        type="button"
                        aria-label={`Ano ${yr}`}
                        aria-pressed={isCur}
                        onMouseDown={e => e.preventDefault()}
                        onMouseEnter={() => setHoverKey(`y${yr}`)}
                        onMouseLeave={() =>
                          setHoverKey(k => (k === `y${yr}` ? '' : k))
                        }
                        onClick={() => {
                          setView(v => ({ ...v, y: yr }))
                          setMode('months')
                        }}
                        style={quickCellStyle(isCur, isHover)}
                      >
                        {yr}
                      </button>
                    )
                  })}
                </div>
              )}

              {/* Rodapé: Limpar · Hoje (links azul) */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  marginTop: 14,
                  paddingTop: 12,
                  borderTop: '1px solid var(--szv2-border)',
                }}
              >
                <button
                  type="button"
                  onMouseDown={e => e.preventDefault()}
                  onMouseEnter={() => setHoverKey('clear')}
                  onMouseLeave={() =>
                    setHoverKey(k => (k === 'clear' ? '' : k))
                  }
                  onClick={clear}
                  style={footerLinkStyle(hoverKey === 'clear')}
                >
                  Limpar
                </button>
                <button
                  type="button"
                  onMouseDown={e => e.preventDefault()}
                  onMouseEnter={() => setHoverKey('today')}
                  onMouseLeave={() =>
                    setHoverKey(k => (k === 'today' ? '' : k))
                  }
                  onClick={() => {
                    if (!isDisabledISO(todayISO)) selectISO(todayISO)
                    else goToday()
                  }}
                  style={footerLinkStyle(hoverKey === 'today')}
                >
                  Hoje
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}
    </div>
  )
}

// ── Estilos/SVG auxiliares ────────────────────────────────────────────────────

// Botão redondo das setas do header (‹ ›), com estado de hover (brand sutil).
function navBtnStyle(hover: boolean): CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 30,
    height: 30,
    flexShrink: 0,
    border: '1px solid var(--szv2-border)',
    borderRadius: '50%',
    background: hover ? HOVER_BG : 'var(--szv2-surface-alt)',
    color: hover ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
    cursor: 'pointer',
    padding: 0,
    outline: 'none',
    transition: 'background 130ms ease, color 130ms ease, border-color 130ms ease',
  }
}

// Célula dos seletores rápidos de mês/ano (estado atual + hover).
function quickCellStyle(current: boolean, hover: boolean): CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: 48,
    border: current
      ? '1.5px solid var(--szv2-brand)'
      : '1px solid transparent',
    borderRadius: 10,
    background: current
      ? 'var(--szv2-brand)'
      : hover
        ? HOVER_BG
        : 'var(--szv2-surface-alt)',
    color: current
      ? 'var(--szv2-on-brand, #fff)'
      : hover
        ? 'var(--szv2-brand)'
        : 'var(--szv2-text)',
    fontSize: 13.5,
    fontWeight: current ? 600 : 500,
    cursor: 'pointer',
    padding: 0,
    outline: 'none',
    transition: 'background 120ms ease, color 120ms ease, border-color 120ms ease',
  }
}

// Links do rodapé (Limpar / Hoje) — azul, sublinha no hover.
function footerLinkStyle(hover: boolean): CSSProperties {
  return {
    border: 'none',
    background: 'transparent',
    color: 'var(--szv2-brand)',
    fontSize: 12.5,
    fontWeight: 600,
    cursor: 'pointer',
    padding: '2px 4px',
    outline: 'none',
    textDecoration: hover ? 'underline' : 'none',
    textUnderlineOffset: 3,
    transition: 'color 120ms ease',
  }
}

// Keyframes de entrada do popup (fade + leve subida). Injetado inline (sem CSS file).
const FDP_KEYFRAMES = `
@keyframes szFdpIn {
  from { opacity: 0; transform: translateY(-4px) scale(.985); }
  to   { opacity: 1; transform: translateY(0) scale(1); }
}`

function Chevron({ dir }: { dir: 'left' | 'right' }) {
  return (
    <svg
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
      style={{ color: 'currentColor' }}
    >
      <path
        d={dir === 'left' ? 'M15 6l-6 6 6 6' : 'M9 6l6 6-6 6'}
        stroke="currentColor"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

// Pequeno caret ao lado do caption (indica que abre seletor mês/ano).
function Caret({ open }: { open: boolean }) {
  return (
    <svg
      width="11"
      height="11"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
      style={{
        color: 'currentColor',
        transform: open ? 'rotate(180deg)' : 'none',
        transition: 'transform 130ms ease',
      }}
    >
      <path
        d="M6 9l6 6 6-6"
        stroke="currentColor"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
