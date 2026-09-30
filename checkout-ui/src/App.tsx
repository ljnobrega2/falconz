import { Routes, Route, useParams, useSearchParams, useLocation } from 'react-router-dom'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import Checkout from './Checkout'
import ThankYou from './ThankYou'
import Rastreio from './Rastreio'
import FalkLogo from './components/FalkLogo'
import { resolveBrand, resolveBrandData, accentCssVars, type BrandConfig } from './brand'
import { CheckoutToastHost } from './Toast'
import type { CreateOrderResponse, OfferResponse, ScheduleOption } from './api'
import { initAnalytics, track, trackPageView } from './analytics'

/** Estado entre Checkout -> ThankYou sem precisar de back-end de sessao. */
export interface OrderResult {
  order: CreateOrderResponse
  offer: OfferResponse
  customerName: string
  /** Data de entrega escolhida (com tipo/status) — null em ofertas sem agendamento. */
  selectedDate: ScheduleOption | null
}

// CHECKOUT-BRANDING: cabecalho white-label. Prioridade no cabecalho:
//   1) LOGO do produtor (brand.logoUrl, https) — <img>.
//   2) NOME de marca textual do produtor (brand.hasCustomName) — wordmark colorido.
//   3) LOGO PADRAO FALK (falcao + wordmark "FALK") quando nao ha marca propria —
//      substitui o antigo "nome do produto como logo" (o produto ja aparece no
//      resumo). O selo "Compra segura" permanece (confianca > branding). Se o logo
//      (proprio OU padrao) falhar ao carregar, cai p/ a marca textual (estado React).
function Topbar({ brand }: { brand: BrandConfig }) {
  const [logoFailed, setLogoFailed] = useState(false)
  const [logoPortrait, setLogoPortrait] = useState(false)
  // Troca de marca (novo logoUrl) reseta o estado de falha.
  useEffect(() => {
    setLogoFailed(false)
    setLogoPortrait(false)
  }, [brand.logoUrl])

  const showCustomLogo = Boolean(brand.logoUrl) && !logoFailed

  let mark: ReactNode
  if (showCustomLogo) {
    mark = (
      <div className={`fk-brand-logo-wrap${logoPortrait ? ' is-portrait' : ''}`}>
        <img
          className="fk-brand-logo"
          src={brand.logoUrl}
          alt={brand.name}
          onError={() => setLogoFailed(true)}
          onLoad={event => setLogoPortrait(event.currentTarget.naturalHeight > event.currentTarget.naturalWidth)}
        />
      </div>
    )
  } else if (brand.hasCustomName) {
    // Produtor definiu um nome de marca (sem logo proprio) — marca textual colorida.
    mark = <div className="fk-logo">{brand.name}</div>
  } else {
    // Fallback FALK: mesma marca padrão (águia preta/azul + wordmark "FALK LOG")
    // usada em todas as outras páginas do site (portal/admin) — FalkLogo.tsx.
    mark = <FalkLogo variant="full" size={30} />
  }

  return (
    <div className="fk-topbar">
      {mark}
    </div>
  )
}

function CheckoutBanner({ brand }: { brand: BrandConfig }) {
  const [portrait, setPortrait] = useState(false)
  if (!brand.bannerUrl) return null
  return (
    <div className={`fk-checkout-banner${portrait ? ' is-portrait' : ''}`}>
      <img
        src={brand.bannerUrl}
        alt=""
        loading="eager"
        onLoad={e => setPortrait(e.currentTarget.naturalHeight > e.currentTarget.naturalWidth)}
      />
    </div>
  )
}

function WhatsAppContactButton({ brand }: { brand: BrandConfig }) {
  if (!brand.whatsapp) return null
  const digits = brand.whatsapp.replace(/\D/g, '')
  const phone = digits.length === 10 || digits.length === 11 ? `55${digits}` : digits
  const href = `https://wa.me/${phone}?text=Ol%C3%A1%2C%20preciso%20de%20ajuda%20com%20meu%20pedido.`
  return (
    <a
      className="fk-whatsapp-contact"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      aria-label="Fale no WhatsApp"
      onClick={() => track('click_whatsapp', { placement: 'checkout_floating_button' })}
    >
      <svg className="fk-whatsapp-icon" viewBox="0 0 24 24" aria-hidden="true" fill="currentColor">
        <path d="M20.5 3.5A11.8 11.8 0 0 0 12.08 0C5.55 0 .24 5.31.24 11.84c0 2.09.55 4.13 1.6 5.93L.13 24l6.38-1.67a11.82 11.82 0 0 0 5.57 1.42h.01c6.53 0 11.84-5.31 11.84-11.84 0-3.17-1.23-6.15-3.43-8.41ZM12.09 21.7h-.01a9.82 9.82 0 0 1-5-1.37l-.36-.21-3.79.99 1.01-3.69-.23-.38a9.82 9.82 0 0 1-1.5-5.2C2.21 6.42 6.64 2 12.08 2a9.79 9.79 0 0 1 6.97 2.9 9.82 9.82 0 0 1 2.89 7.01c0 5.43-4.42 9.79-9.85 9.79Zm5.38-7.35c-.29-.15-1.71-.84-1.98-.94-.27-.1-.47-.15-.67.15-.2.3-.77.94-.94 1.13-.17.2-.35.22-.64.07-.29-.15-1.2-.44-2.29-1.41-.85-.76-1.42-1.7-1.59-1.99-.17-.3-.02-.46.13-.61.13-.13.29-.35.44-.52.15-.17.2-.3.3-.49.1-.2.05-.37-.02-.52-.07-.15-.67-1.61-.91-2.2-.24-.58-.48-.5-.67-.51h-.57c-.2 0-.52.07-.79.37-.27.3-1.03 1.01-1.03 2.47s1.06 2.87 1.21 3.07c.15.2 2.08 3.18 5.04 4.46.7.3 1.25.49 1.68.62.71.23 1.36.2 1.87.12.57-.08 1.71-.7 1.95-1.38.24-.68.24-1.26.17-1.38-.08-.12-.27-.2-.56-.34Z" />
      </svg>
      <span>Fale no WhatsApp</span>
    </a>
  )
}

/** Resolve o token de ?sz= OU do path /checkout/:token. */
function useCheckoutToken(): string {
  const { token } = useParams()
  const [params] = useSearchParams()
  return (params.get('sz') || token || '').trim()
}

/**
 * FEAT-AFF-ATTRIBUTION (2026-06-24): resolve o token de rastreio do afiliado (?r=).
 * Best-practice de mercado: last-click com PERSISTÊNCIA — o ?r= é capturado no load
 * e gravado em sessionStorage, sobrevivendo a refresh/navegação dentro do checkout
 * (sem ele, recarregar a página perderia o crédito do afiliado). A URL sempre vence
 * (clique mais recente); na ausência, usa o último salvo. O token é OPACO no front —
 * o servidor decodifica → senderzz_affiliates.id, valida o afiliado ativo e credita.
 */
function useAffRef(): string {
  const [params] = useSearchParams()
  const fromUrl = (params.get('r') || '').trim()
  useEffect(() => {
    if (fromUrl) {
      try { sessionStorage.setItem('falk_aff_ref', fromUrl) } catch { /* sessionStorage indisponível — degrada */ }
    }
  }, [fromUrl])
  return useMemo(() => {
    if (fromUrl) return fromUrl
    try { return (sessionStorage.getItem('falk_aff_ref') || '').trim() } catch { return '' }
  }, [fromUrl])
}

function CheckoutRoute({
  onDone,
  result,
}: {
  onDone: (r: OrderResult) => void
  result: OrderResult | null
}) {
  const token = useCheckoutToken()
  const affRef = useAffRef()
  const location = useLocation()

  // CHECKOUT-PERSONALIZADO: a oferta carregada (reportada pelo Checkout) eleva a
  // marca. Antes de carregar, a marca vem so da querystring/fallback (header ja
  // pinta cedo, sem flash do azul generico quando o link traz ?brand/?cor).
  const [offer, setOffer] = useState<OfferResponse | null>(result?.offer ?? null)
  const brand = useMemo(() => resolveBrand(offer), [offer])
  const accentStyle = useMemo(() => accentCssVars(brand.accent), [brand.accent])
  // Se ja houver resultado para este caminho, mostra a confirmacao (coluna estreita).
  // Marca derivada da oferta do pedido — mantem o branding na tela de obrigado.
  if (result) {
    const resultBrand = resolveBrand(result.offer)
    return (
      <div className="fk-shell" style={accentCssVars(resultBrand.accent, resultBrand.textColor)}>
        <Topbar brand={resultBrand} />
        <ThankYou result={result} />
        <WhatsAppContactButton brand={resultBrand} />
        <div className="fk-foot">{resultBrand.name} · Acompanhe seu pedido</div>
      </div>
    )
  }

  // Checkout: shell largo (2 colunas no desktop, empilha no mobile).
  return (
    <div className="fk-shell fk-shell-wide" style={{ ...accentStyle, ['--falkz-ink' as string]: brand.textColor }}>
      <CheckoutBanner brand={brand} />
      <Checkout key={location.key} token={token} affRef={affRef} onDone={onDone} onOffer={setOffer} />
      <WhatsAppContactButton brand={brand} />
    </div>
  )
}

/** Wrapper do rastreio: mantem Topbar/foot, container mais largo (2 colunas). */
function RastreioRoute() {
  const [brand, setBrand] = useState(() => resolveBrand(null))
  const handleBrand = useCallback((data: { name: string; logo_url: string; accent: string; banner_url: string; whatsapp: string; text_color: string }) => {
    setBrand(resolveBrandData({
      name: data.name,
      logoUrl: '',
      accent: data.accent,
      bannerUrl: '',
        whatsapp: data.whatsapp,
        textColor: data.text_color,
    }))
  }, [])
  return (
    <div className="fk-shell fk-shell-rastreio" style={accentCssVars(brand.accent, brand.textColor)}>
      <Topbar brand={brand} />
      <Rastreio onBrand={handleBrand} hideGenericCopy={brand.isCustom} />
      <WhatsAppContactButton brand={brand} />
      {!brand.isCustom && <div className="fk-foot">{brand.name} · Pagamento na entrega</div>}
    </div>
  )
}

export default function App() {
  // Resultado do pedido mantido em memoria (SPA). Sem token = tela de erro.
  const [result, setResult] = useState<OrderResult | null>(null)

  // Titulo da aba por rota: rastreio vs finalizar pedido (pedido do dono).
  const location = useLocation()
  useEffect(() => {
    initAnalytics()
    trackPageView(location.pathname)
  }, [location.pathname])
  useEffect(() => {
    const isTrack = location.pathname.startsWith('/rastreio/') || location.pathname.startsWith('/pedido/')
    document.title = isTrack ? 'Acompanhar pedido' : 'Finalizar pedido'
  }, [location.pathname])

  return (
    <>
      <Routes>
        {/* Rastreio publico — basename "/checkout" -> /checkout/rastreio/:code */}
        <Route path="/rastreio/:code" element={<RastreioRoute />} />
        <Route path="/pedido/:code" element={<RastreioRoute />} />

        <Route path="/" element={<CheckoutRoute onDone={setResult} result={result} />} />
        <Route path="/:token" element={<CheckoutRoute onDone={setResult} result={result} />} />
        <Route
          path="*"
          element={<CheckoutRoute onDone={setResult} result={result} />}
        />
      </Routes>
      {/* Toast lateral (tema FALK do checkout) — feedback de erro ao finalizar o pedido. */}
      <CheckoutToastHost />
    </>
  )
}
