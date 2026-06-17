const BASE = import.meta.env.VITE_API_BASE || '/wp-json/senderzz/v1/admin'

export function getToken(): string | null {
  return localStorage.getItem('sz_admin_token')
}

export function setToken(t: string | null) {
  if (t) localStorage.setItem('sz_admin_token', t)
  else localStorage.removeItem('sz_admin_token')
}

export function clearToken() {
  localStorage.removeItem('sz_admin_token')
}

export async function api<T = any>(
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const tok = getToken()
  if (tok) headers.set('Authorization', `Bearer ${tok}`)

  const res = await fetch(`${BASE}${path}`, { ...init, headers })
  if (res.status === 401) {
    setToken(null)
    window.location.href = '/admin/login'
    throw new Error('unauthorized')
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body?.error?.message || `HTTP ${res.status}`)
  }
  return res.json()
}
