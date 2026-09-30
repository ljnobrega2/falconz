// FalkLogo — marca FALK LOG padrão (mesma arte de portal-ui/admin-ui). Checkout
// só roda sobre fundo claro (sem toggle de tema), então usa só falk-logo-light.png
// (águia preta/azul + wordmark "FALK LOG"), sem a lógica de detecção de tema dos
// outros apps.
type Props = {
  variant?: 'full' | 'mark' | 'icon'
  size?: number
  className?: string
}

const SRC_LIGHT = `${import.meta.env.BASE_URL}falk-logo-light.png`
const RATIO_LIGHT = 678 / 274 // ≈ 2.474 — proporção da arte real
const EAGLE_FRACTION_LIGHT = 0.38 // águia ocupa ~38% da largura no PNG
const CONTENT_FRACTION_LIGHT = 0.93 // arte é justa no canvas, quase sem respiro

export default function FalkLogo({ variant = 'full', size = 30, className }: Props) {
  // icon/mark = só a águia: recorta o lado esquerdo da imagem.
  if (variant === 'icon' || variant === 'mark') {
    const fullWidth = size * RATIO_LIGHT
    const boxWidth = Math.round(fullWidth * EAGLE_FRACTION_LIGHT)
    return (
      <span
        className={className}
        style={{ display: 'inline-flex', width: boxWidth, height: size, overflow: 'hidden', flex: 'none' }}
      >
        <img
          src={SRC_LIGHT}
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

  const imgHeight = size / CONTENT_FRACTION_LIGHT
  return (
    <span className={className} style={{ display: 'inline-flex', alignItems: 'center', height: size, overflow: 'hidden' }}>
      <img src={SRC_LIGHT} alt="FALK LOG" style={{ height: imgHeight, width: 'auto', display: 'block' }} />
    </span>
  )
}
