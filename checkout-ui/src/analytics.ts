// Rastreamento first-party do checkout. Não coleta nome, telefone, CPF ou endereço.

type AnalyticsParams = Record<string, string | number | boolean | undefined>
const CAMPAIGN_KEYS = ['utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term', 'gclid', 'fbclid'] as const
const CAMPAIGN_STORAGE_KEY = 'falk_checkout_campaign'
const GA4_ID = 'G-22GCCY3KMP'

declare global {
  interface Window {
    dataLayer?: Array<Record<string, unknown>>
    gtag?: (...args: unknown[]) => void
  }
}

function readCampaignFromUrl(): Record<string, string> {
  const params = new URLSearchParams(window.location.search)
  return Object.fromEntries(
    CAMPAIGN_KEYS
      .map(key => [key, (params.get(key) || '').trim()] as const)
      .filter(([, value]) => Boolean(value)),
  )
}

export function getCampaignParams(): Record<string, string> {
  const fromUrl = readCampaignFromUrl()
  if (Object.keys(fromUrl).length > 0) {
    try { sessionStorage.setItem(CAMPAIGN_STORAGE_KEY, JSON.stringify(fromUrl)) } catch { /* sem storage */ }
    return fromUrl
  }
  try {
    const saved = JSON.parse(sessionStorage.getItem(CAMPAIGN_STORAGE_KEY) || '{}')
    return saved && typeof saved === 'object' ? saved as Record<string, string> : {}
  } catch {
    return {}
  }
}

export function initAnalytics(): void {
  const id = (import.meta.env.VITE_GA4_MEASUREMENT_ID || GA4_ID).trim()
  if (!id || window.gtag) return
  const script = document.createElement('script')
  script.async = true
  script.src = `https://www.googletagmanager.com/gtag/js?id=${encodeURIComponent(id)}`
  document.head.appendChild(script)
  window.dataLayer = window.dataLayer || []
  window.gtag = (...args: unknown[]) => window.dataLayer?.push({ event: 'gtag', args })
  window.gtag('js', new Date())
  window.gtag('config', id, { send_page_view: false })
}

export function track(name: string, params: AnalyticsParams = {}): void {
  const payload = { ...getCampaignParams(), ...params }
  window.dataLayer = window.dataLayer || []
  window.dataLayer.push({ event: name, ...payload })
  window.gtag?.('event', name, payload)
  sendDirect(name, payload)
}

export function trackPageView(path: string): void {
  track('page_view', { page_path: path, page_location: window.location.href })
}

// Fallback enxuto para navegadores que bloqueiam a fila do gtag, mas permitem
// a coleta HTTPS. O client_id é aleatório e não contém dado pessoal.
function sendDirect(name: string, params: AnalyticsParams): void {
  let clientId = ''
  try {
    clientId = localStorage.getItem('falk_ga_client_id') || ''
    if (!clientId) {
      clientId = `${Date.now()}.${Math.floor(Math.random() * 1_000_000_000)}`
      localStorage.setItem('falk_ga_client_id', clientId)
    }
  } catch {
    clientId = `${Date.now()}.${Math.floor(Math.random() * 1_000_000_000)}`
  }

  const query = new URLSearchParams({
    v: '2',
    tid: GA4_ID,
    cid: clientId,
    en: name,
    dl: window.location.href,
    dt: document.title,
  })
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined) continue
    if (typeof value === 'number') query.set(`epn.${key}`, String(value))
    else query.set(`ep.${key}`, String(value))
  }
  try {
    void fetch(`https://www.google-analytics.com/g/collect?${query.toString()}`, {
      method: 'POST',
      mode: 'no-cors',
      keepalive: true,
    })
  } catch { /* analytics nunca pode interromper o checkout */ }
}
