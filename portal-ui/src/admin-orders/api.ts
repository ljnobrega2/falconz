// AUDIT-2026-06-18 Onda3 — 401 usa SPA navigate + toast (sem window.location.href)
export const BASE = import.meta.env.VITE_API_BASE || '/wp-json/senderzz/v1/admin'

// LOGIN-UNICO (FEAT-RBAC) — namespace do PORTAL, derivado do BASE do admin removendo
// o sufixo "/admin" (mesma origem, mesmo namespace senderzz/v1). Usado SÓ no fallback
// 2FA da página única de login: o /admin/login já faz o handoff admin→portal server-side
// para contas sem 2FA, mas não consegue completar o desafio 2FA (não tem o endpoint).
export const PORTAL_BASE =
  import.meta.env.VITE_PORTAL_API_BASE || BASE.replace(/\/admin\/?$/, '')

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
 * AUDIT-2026-06-18 Onda3
 */
let _navigate401: ((path: string) => void) | null = null
let _toast401: ((kind: 'ok' | 'err' | 'warn' | 'info', msg: string) => void) | null = null

// FE-401-REDIRECT-CONSISTENT — fila de 401 pendente (espelha portal-ui/src/api.ts).
// Race no mount: os useEffect dos filhos (páginas) rodam ANTES do useEffect do
// Layout que chama registerApi401Handlers (efeitos passivos correm bottom-up).
// Se o 1º fetch já volta 401, os handlers ainda são null. Antes, o fallback fazia
// um window.location.href forçado (hard reload + caminho hardcoded /admin/login),
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
 * AUDIT FE-401-REDIRECT-CONSISTENT — paridade com portal-ui/src/api.ts.
 * Opções extras do cliente.
 * authRedirect=false → não dispara o fluxo global de "sessão expirada" no 401
 * (usado nas chamadas de login: 401 ali = credenciais/2FA inválidos; deve ser
 *  mostrado inline na tela de login, não limpar token nem redirecionar/recarregar).
 * Sem isso, um login com 401 antes do Layout montar entraria no fluxo global e a
 * mensagem real do servidor (mostrada inline) seria perdida.
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
  headers.set('Content-Type', 'application/json')
  const tok = getToken()
  if (tok) headers.set('Authorization', `Bearer ${tok}`)

  // Havia token nesta request? Só enfileiramos redirect de 401 quando havia — um
  // 401 sem token autenticado (ex.: namespace público) não deve forçar logout.
  const hadToken = !!tok

  const res = await fetch(`${BASE}${path}`, { ...init, headers })
  if (res.status === 401) {
    const body = await res.json().catch(() => ({}))
    // go/admin usa envelope {error:{message}} (≠ go/portal {erro}); fallback p/ outras formas.
    const msg = body?.error?.message || 'Sessão expirada. Faça login novamente.'
    // AUDIT FE-401-REDIRECT-CONSISTENT — só dispara o fluxo global se authRedirect !== false.
    if (opts.authRedirect !== false) {
      setToken(null)
      // AUDIT-2026-06-18 Onda3 + FE-401-REDIRECT-CONSISTENT — caminho único: SPA
      // navigate '/login'. Se os handlers já estão registrados, redireciona agora;
      // senão enfileira p/ flush em registerApi401Handlers (sem window.location
      // forçado, sem hard reload, sem caminho hardcoded /admin/login).
      // Toast de fim de sessão fica sempre amigável (a 401 do middleware retorna
      // "token inválido"/"admin inválido", jargão demais para o usuário). A mensagem
      // real do servidor segue no throw abaixo, para callers que mostram erro inline.
      if (_navigate401) {
        if (_toast401) _toast401('warn', 'Sessão expirada. Faça login novamente.')
        _navigate401('/login')
      } else if (hadToken) {
        _pending401 = { path: '/login', msg: 'Sessão expirada. Faça login novamente.' }
      }
    }
    throw new Error(msg)
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body?.error?.message || `HTTP ${res.status}`)
  }
  return res.json()
}
