// =============================================================================
// CHECKOUT-PERSONALIZADO (2026-06-19) — resolucao da identidade WHITE-LABEL.
//
// O checkout reflete a MARCA DO PRODUTOR/OFERTA. A marca pode vir de tres fontes,
// em ordem de prioridade:
//   1) Campos da OFERTA (offer.brand) — quando o backend /offer os enviar.
//   2) QUERYSTRING do link (?brand=&cor=&logo=) — permite ao produtor carregar a
//      marca pelo proprio link ANTES do backend persistir os campos.
//   3) FALLBACK FALK — azul #1E6FF2 + marca textual "FALK"/nome da oferta.
//
// SEGURANCA: a cor passa por sanitizeHexColor (so #RGB/#RRGGBB). O logo so e
// aceito se for URL http(s) absoluta — evita data:/javascript: no src da <img>.
// =============================================================================

import { sanitizeHexColor, type OfferResponse } from './api'

/** Paleta FALK padrao (fallback). Espelha as :root --falkz-* do index.css. */
export const FALK_DEFAULT_ACCENT = '#1E6FF2'

/** Identidade resolvida e pronta para renderizar. */
export interface BrandConfig {
  /** Nome a exibir no cabecalho (marca > nome da oferta > "FALK"). */
  name: string
  /** URL do logo (http/https) ou "" para marca textual. */
  logoUrl: string
  /** Cor de acento ja sanitizada (#hex). Sempre presente. */
  accent: string
  bannerUrl: string
  whatsapp: string
  textColor: string
  /** true quando ha uma marca personalizada (nao e o fallback FALK puro). */
  isCustom: boolean
  /**
   * true quando o produtor definiu um NOME DE MARCA explicito (offer.brand.name
   * ou ?brand=) — diferente do nome do PRODUTO (que vira fallback de `name`).
   * O cabecalho usa isto para decidir entre marca textual do produtor e o LOGO
   * PADRAO FALK: sem nome explicito E sem logo proprio => mostra o logo FALK.
   */
  hasCustomName: boolean
}

export function resolveBrandData(data: (Partial<Pick<BrandConfig, 'name' | 'logoUrl' | 'accent' | 'bannerUrl' | 'whatsapp' | 'textColor'>> & { hasCustomName?: boolean }) | null): BrandConfig {
  const q = brandFromQuery()
  const name = (data?.name || q.name || '').trim()
  const logoUrl = safeLogoUrl(data?.logoUrl) || q.logoUrl
  const accent = sanitizeHexColor(data?.accent) || q.accent || FALK_DEFAULT_ACCENT
  const bannerUrl = safeLogoUrl(data?.bannerUrl)
  const whatsapp = (data?.whatsapp || q.whatsapp || '').trim()
  const textColor = sanitizeHexColor(data?.textColor) || '#182235'
  return {
    name: name || 'FALK LOG',
    logoUrl,
    accent,
    bannerUrl,
    whatsapp,
    textColor,
    isCustom: Boolean(data?.hasCustomName || logoUrl || data?.bannerUrl || whatsapp || textColor.toLowerCase() !== '#182235' || accent.toLowerCase() !== FALK_DEFAULT_ACCENT.toLowerCase()),
    hasCustomName: Boolean(data?.hasCustomName),
  }
}

/** Aceita so URL http(s) absoluta como logo (bloqueia data:/javascript:/relativa). */
function safeLogoUrl(raw: string | undefined | null): string {
  const v = (raw || '').trim()
  if (!v) return ''
  return /^https?:\/\//i.test(v) || v.startsWith('/uploads/products/') ? v : ''
}

/** Le a marca da querystring do link de checkout (override do produtor). */
function brandFromQuery(): { name: string; logoUrl: string; accent: string; whatsapp: string } {
  if (typeof window === 'undefined') return { name: '', logoUrl: '', accent: '', whatsapp: '' }
  const p = new URLSearchParams(window.location.search)
  return {
    name: (p.get('brand') || p.get('marca') || '').trim().slice(0, 40),
    logoUrl: safeLogoUrl(p.get('logo') || p.get('logo_url')),
    accent: sanitizeHexColor(p.get('cor') || p.get('accent')),
    whatsapp: (p.get('whatsapp') || '').trim(),
  }
}

/**
 * Resolve a identidade final. `offer` pode ser null (carregando/erro) — nesse
 * caso so a querystring/fallback valem (cabecalho ja pinta a marca cedo).
 */
export function resolveBrand(offer: OfferResponse | null): BrandConfig {
  const q = brandFromQuery()
  const ob = offer?.brand

  // NOME DE MARCA explicito (NAO o nome do produto) — produtor via offer.brand.name
  // ou ?brand=. Distingue "marca textual do produtor" do fallback FALK no cabecalho.
  const explicitName = (ob?.name || q.name || '').trim()

  // Prioridade: oferta > querystring > fallback.
  const name = (explicitName || offer?.name || '').trim()
  const logoUrl = safeLogoUrl(ob?.logo_url) || q.logoUrl
  const accent = sanitizeHexColor(ob?.accent) || q.accent || FALK_DEFAULT_ACCENT
  const bannerUrl = safeLogoUrl(ob?.banner_url)
  const whatsapp = (ob?.whatsapp || q.whatsapp || '').trim()
  const textColor = sanitizeHexColor(ob?.text_color)

  const isCustom = Boolean(
    explicitName || logoUrl || (accent && accent.toLowerCase() !== FALK_DEFAULT_ACCENT.toLowerCase())
  )

  return resolveBrandData({ name: name || 'FALK LOG', logoUrl, accent, bannerUrl, whatsapp, textColor, hasCustomName: explicitName.length > 0 })
}

// --- Tonalizacao da cor de acento -----------------------------------------
// A partir do accent (1 cor), derivamos as variantes que o CSS espera (600/700/
// soft/cta-shadow) escurecendo/clareando o hex. Mantem o checkout coerente com
// QUALQUER cor do produtor sem exigir que ele informe a paleta inteira.

function hexToRgb(hex: string): [number, number, number] {
  let h = hex.replace('#', '')
  if (h.length === 3) h = h.split('').map((c) => c + c).join('')
  const n = parseInt(h, 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

function clamp(n: number): number {
  return Math.max(0, Math.min(255, Math.round(n)))
}

function rgbToHex(r: number, g: number, b: number): string {
  return '#' + [r, g, b].map((c) => clamp(c).toString(16).padStart(2, '0')).join('')
}

/** Escurece (factor<1) ou clareia (factor>1) uma cor hex. */
function shade(hex: string, factor: number): string {
  const [r, g, b] = hexToRgb(hex)
  return rgbToHex(r * factor, g * factor, b * factor)
}

/** rgba(...) a partir do hex + alpha — usado nos tons "soft" de fundo/anel. */
function rgba(hex: string, alpha: number): string {
  const [r, g, b] = hexToRgb(hex)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

/** Escolhe texto preto para acentos claros (ex.: verde), branco para escuros. */
function onBrandColor(hex: string): string {
  const [r, g, b] = hexToRgb(hex)
  const yiq = (r * 299 + g * 587 + b * 114) / 1000
  return yiq >= 155 ? '#000' : '#fff'
}

/**
 * Gera as overrides das variaveis CSS --falkz-* a partir do accent. Aplicadas
 * inline no container do checkout (escopo local) — NAO mexe no :root global nem
 * no portal. Quando accent e o azul FALK, o resultado e ~identico ao tema padrao.
 */
export function accentCssVars(accent: string, textColor = '#182235'): React.CSSProperties {
  const brand = accent
  const brand600 = shade(accent, 0.85)
  const brand700 = shade(accent, 0.72)
  const soft = rgba(accent, 0.1)
  return {
    // cast: chaves custom (--falkz-*) nao existem no tipo CSSProperties.
    ['--falkz-brand' as string]: brand,
    ['--falkz-brand-600' as string]: brand600,
    ['--falkz-brand-700' as string]: brand700,
    ['--falkz-brand-soft' as string]: soft,
    ['--falkz-ok' as string]: brand600,
    ['--falkz-ok-soft' as string]: soft,
    ['--falkz-on-brand' as string]: onBrandColor(accent),
    ['--falkz-ink' as string]: textColor,
    ['--falkz-shadow-cta' as string]: `0 6px 18px ${rgba(accent, 0.32)}`,
  } as React.CSSProperties
}
