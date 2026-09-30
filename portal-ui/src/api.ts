// Portal user React — cliente REST contra go/portal (/wp-json/senderzz/v1).
// Envelope go/portal: {"ok":true,...} em sucesso, {"ok":false,"erro":"msg"} em erro.
// 401 → SPA navigate /login + toast (sem window.location.href forçado). Se o 401
// chega antes do Layout registrar os handlers (race de mount), enfileira e dá flush.
const BASE = import.meta.env.VITE_API_BASE || '/wp-json/senderzz/v1'

// Base do labels-service (go/labels, namespace wc-melhor-envio/v1).
// Em prod/test, nginx roteia /wp-json/wc-melhor-envio/v1/ → falk-labels:8084.
export const LABELS_BASE = import.meta.env.VITE_LABELS_API_BASE || '/wp-json/wc-melhor-envio/v1'

export function getToken(): string | null {
  return localStorage.getItem('sz_portal_token')
}

export function setToken(t: string | null) {
  if (t) localStorage.setItem('sz_portal_token', t)
  else localStorage.removeItem('sz_portal_token')
}

export function clearToken() {
  localStorage.removeItem('sz_portal_token')
}

/**
 * Callbacks registradas pelo Layout para navegar e toastar sem depender do React tree.
 */
let _navigate401: ((path: string) => void) | null = null
let _toast401: ((kind: 'ok' | 'err' | 'warn' | 'info', msg: string) => void) | null = null

// FE-401-REDIRECT-CONSISTENT — fila de 401 pendente.
// Race no mount: os useEffect dos filhos (páginas) rodam ANTES do useEffect do
// Layout que chama registerApi401Handlers (efeitos passivos correm bottom-up).
// Se o 1º fetch já volta 401, os handlers ainda são null. Antes, o fallback fazia
// um window.location.href forçado (hard reload + caminho hardcoded /portal/login),
// inconsistente com o SPA navigate '/login' do caso normal. Agora enfileiramos o
// redirect e damos flush assim que os handlers chegam — sempre SPA, sem reload.
let _pending401: { path: string; msg: string } | null = null

export function registerApi401Handlers(
  navigate: (path: string) => void,
  toast: (kind: 'ok' | 'err' | 'warn' | 'info', msg: string) => void,
) {
  _navigate401 = navigate
  _toast401 = toast
  // Flush one-shot: se um 401 chegou antes do registro, redireciona agora (SPA).
  if (_pending401) {
    const p = _pending401
    _pending401 = null
    _toast401('warn', p.msg)
    _navigate401(p.path)
  }
}

/**
 * Opções extras do cliente.
 * authRedirect=false → não dispara o fluxo global de "sessão expirada" no 401
 * (usado nas chamadas de login: 401 ali = credenciais/código inválidos, deve
 *  ser mostrado inline, não redirecionar nem limpar token).
 */
export interface ApiOpts {
  authRedirect?: boolean
}

export async function api<T = any>(
  path: string,
  init: RequestInit = {},
  opts: ApiOpts = {}
): Promise<T> {
  const headers = new Headers(init.headers)
  // FormData → deixa o browser definir Content-Type (com o boundary do multipart).
  // Forçar application/json aqui quebraria o upload de arquivo (ex.: cartão CNPJ).
  if (!(init.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  const tok = getToken()
  if (tok) headers.set('Authorization', `Bearer ${tok}`)

  // Havia token nesta request? Só enfileiramos redirect de 401 quando havia — um
  // 401 sem token autenticado (ex.: namespace público) não deve forçar logout.
  const hadToken = !!tok

  const res = await fetch(`${BASE}${path}`, { ...init, headers })
  if (res.status === 401) {
    const body = await res.json().catch(() => ({}))
    // go/portal usa {ok:false, erro:"..."}; fallback p/ outras formas.
    const msg = body?.erro || body?.error?.message || 'Sessão expirada. Faça login novamente.'
    if (opts.authRedirect !== false) {
      setToken(null)
      // Caminho único: SPA navigate '/login'. Se os handlers já estão registrados,
      // redireciona agora; senão enfileira p/ flush em registerApi401Handlers.
      // Sem window.location forçado (sem hard reload, sem caminho hardcoded).
      if (_navigate401) {
        if (_toast401) _toast401('warn', msg)
        _navigate401('/login')
      } else if (hadToken) {
        _pending401 = { path: '/login', msg }
      }
    }
    throw new Error(msg)
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    // go/portal usa {ok:false, erro:"..."}; fallback p/ outras formas.
    throw new Error(body?.erro || body?.error?.message || `HTTP ${res.status}`)
  }
  return res.json()
}

/** labelsApi — mesmo padrão de api() mas aponta para go/labels (LABELS_BASE). */
export async function labelsApi<T = any>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const tok = getToken()
  if (tok) headers.set('Authorization', `Bearer ${tok}`)
  const res = await fetch(`${LABELS_BASE}${path}`, { ...init, headers })
  if (res.status === 401) {
    const body = await res.json().catch(() => ({}))
    const msg = body?.erro || 'Sessão expirada. Faça login novamente.'
    if (_navigate401) {
      setToken(null)
      if (_toast401) _toast401('warn', msg)
      _navigate401('/login')
    }
    throw new Error(msg)
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body?.erro || body?.error?.message || `HTTP ${res.status}`)
  }
  return res.json()
}
