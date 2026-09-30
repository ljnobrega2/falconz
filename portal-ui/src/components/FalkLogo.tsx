// FalkLogo — marca FALK LOG reutilizável (logo em TODAS as páginas).
//
// Usa as LOGOS REAIS (PNG em public/):
//   - falk-logo-dark.png  → arte sobre fundo escuro (navy)  → tema DARK
//   - falk-logo-light.png → arte sobre fundo claro (branco) → tema LIGHT
// Cada PNG é "águia + divisor + FALK LOG" na horizontal.
//
// Variantes:
//   full        — imagem horizontal completa (águia + FALK LOG) — header, login.
//   icon / mark — SÓ a ÁGUIA (recorte do lado esquerdo via object-fit:cover).
//
// Seleção de tema (resolvida 1x → escolhe src E recorte da águia juntos, p/ não
// divergir):
//   1. prop `tone` explícita vence: 'light' = sobre fundo escuro → PNG dark;
//      'brand' (default visual) = sobre fundo claro → PNG light.
//   2. sem `tone`: detecta o ancestral `[data-theme]` (.sz-dashboard-v2) e
//      reage ao toggle de tema via MutationObserver.
// API/props mantidas (variant/tone/size) p/ não quebrar usos existentes.
import { useEffect, useRef, useState, type CSSProperties } from 'react'

type Props = {
  variant?: 'full' | 'mark' | 'icon'
  tone?: 'brand' | 'light'
  size?: number
  className?: string
  style?: CSSProperties
}

// Caminhos servidos de public/ — BASE_URL cobre montagem em '/' OU '/admin/'.
const SRC_DARK = `${import.meta.env.BASE_URL}falk-logo-dark.png`
const SRC_LIGHT = `${import.meta.env.BASE_URL}falk-logo-light.png`

// Proporção (largura/altura) de cada PNG e fração da largura ocupada pela águia
// (lado esquerdo). Usadas no recorte do ícone (object-fit:cover, ancorado à
// esquerda). Valores medidos das artes reais.
const RATIO_DARK = 1672 / 941 // ≈ 1.777
const RATIO_LIGHT = 678 / 274 // ≈ 2.474
const EAGLE_FRACTION_DARK = 0.34 // águia ocupa ~34% da largura no PNG dark
const EAGLE_FRACTION_LIGHT = 0.38 // ~38% no PNG light (menos respiro lateral)

// CONTENT_FRACTION_* — fração VERTICAL do canvas realmente ocupada pela arte
// (águia + texto). O PNG dark tem margem navy grande acima/abaixo (arte ocupa
// ~62% da altura do canvas); o PNG light é justo, quase sem respiro (~93%).
// Renderizar os dois na MESMA altura de <img> faz a arte do dark parecer bem
// menor que a do light (mesmo container, conteúdo visível bem menor) — bug
// reportado 2026-07-14 ("logo no tema claro é maior que no escuro"). Corrige
// escalando a altura do <img> pelo inverso da fração de conteúdo, igualando o
// tamanho VISÍVEL da arte entre os dois temas para o mesmo `size`.
const CONTENT_FRACTION_DARK = 0.62
const CONTENT_FRACTION_LIGHT = 0.93

export default function FalkLogo({
  variant = 'full',
  tone,
  size = 34,
  className,
  style,
}: Props) {
  const ref = useRef<HTMLSpanElement>(null)
  // tema detectado do ancestral (só usado quando `tone` não é passado).
  const [detectedDark, setDetectedDark] = useState(false)

  useEffect(() => {
    if (tone) return // prop explícita vence — não observa o DOM.
    const host = ref.current?.closest('[data-theme]') as HTMLElement | null
    if (!host) return
    const read = () => setDetectedDark(host.getAttribute('data-theme') === 'dark')
    read()
    const obs = new MutationObserver(read)
    obs.observe(host, { attributes: true, attributeFilter: ['data-theme'] })
    return () => obs.disconnect()
  }, [tone])

  // Resolve UMA vez: tema → src + métricas de recorte (não podem divergir).
  const isDark = tone ? tone === 'light' : detectedDark
  const src = isDark ? SRC_DARK : SRC_LIGHT
  const ratio = isDark ? RATIO_DARK : RATIO_LIGHT
  const eagleFraction = isDark ? EAGLE_FRACTION_DARK : EAGLE_FRACTION_LIGHT

  // icon/mark = só a águia: recorta o lado esquerdo da imagem.
  if (variant === 'icon' || variant === 'mark') {
    const fullWidth = size * ratio // largura natural do PNG nesta altura
    const boxWidth = Math.round(fullWidth * eagleFraction)
    return (
      <span
        ref={ref}
        className={className}
        style={{
          display: 'inline-flex',
          width: boxWidth,
          height: size,
          overflow: 'hidden',
          flex: 'none',
          ...style,
        }}
      >
        <img
          src={src}
          alt="FALK LOG"
          style={{
            height: size,
            width: fullWidth,
            maxWidth: 'none',
            objectFit: 'cover',
            objectPosition: 'left center',
            display: 'block',
          }}
        />
      </span>
    )
  }

  // full = imagem horizontal completa. `size` é a altura VISÍVEL desejada da
  // arte (águia+texto) — a altura real do <img> é maior no dark pra compensar
  // a margem vazia do PNG (ver CONTENT_FRACTION_*), assim a arte fica do MESMO
  // tamanho visual nos dois temas.
  const contentFraction = isDark ? CONTENT_FRACTION_DARK : CONTENT_FRACTION_LIGHT
  const imgHeight = size / contentFraction
  return (
    <span
      ref={ref}
      className={className}
      style={{ display: 'inline-flex', alignItems: 'center', height: size, overflow: 'hidden', ...style }}
    >
      <img
        src={src}
        alt="FALK LOG"
        style={{ height: imgHeight, width: 'auto', display: 'block' }}
      />
    </span>
  )
}
