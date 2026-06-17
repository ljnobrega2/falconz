import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, setToken } from '../api'

export default function Login() {
  const nav = useNavigate()
  const [email, setEmail] = useState('')
  const [senha, setSenha] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setLoading(true)
    try {
      const r = await api<{ token: string }>('/login', {
        method: 'POST',
        body: JSON.stringify({ email, senha }),
      })
      setToken(r.token)
      nav('/')
    } catch (e: any) {
      setErr(e.message || 'Credenciais inválidas')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="sz-login-page">
      <form onSubmit={submit} className="sz-login-box">
        <div className="sz-login-logo">S</div>
        <h1 className="sz-login-title">Senderzz Admin</h1>
        <p className="sz-login-sub">Entrar no painel administrativo</p>

        {err && <div className="sz-login-err">{err}</div>}

        <label className="sz-login-label">Email</label>
        <input
          className="sz-login-input"
          type="email"
          value={email}
          onChange={e => setEmail(e.target.value)}
          autoComplete="email"
          required
        />

        <label className="sz-login-label">Senha</label>
        <input
          className="sz-login-input"
          type="password"
          value={senha}
          onChange={e => setSenha(e.target.value)}
          autoComplete="current-password"
          required
        />

        <button className="sz-login-btn" type="submit" disabled={loading}>
          {loading ? 'Entrando…' : 'Entrar'}
        </button>
      </form>
    </div>
  )
}
