// Página de redefinição de senha — acessada via link do e-mail (?token=xxx).
import { FormEvent, useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import FalkLogo from '../components/FalkLogo'

export default function ResetPassword() {
  const [params] = useSearchParams()
  const nav = useNavigate()
  const token = params.get('token') || ''

  const [senha, setSenha] = useState('')
  const [confirm, setConfirm] = useState('')
  const [showPw, setShowPw] = useState(false)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [done, setDone] = useState(false)

  useEffect(() => {
    if (!token) setErr('Link inválido. Solicite um novo.')
  }, [token])

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (senha !== confirm) { setErr('As senhas não coincidem.'); return }
    if (senha.length < 6) { setErr('A senha deve ter ao menos 6 caracteres.'); return }
    setErr('')
    setLoading(true)
    try {
      await api('/portal/reset-password', {
        method: 'POST',
        body: JSON.stringify({ token, senha }),
      }, { authRedirect: false })
      setDone(true)
    } catch (e: any) {
      setErr(e.message || 'Link inválido ou expirado. Solicite um novo.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: '#F9FAFB', fontFamily: "'Space Grotesk', system-ui, sans-serif" }}>
      <div style={{ width: '100%', maxWidth: 400, background: '#fff', borderRadius: 18, boxShadow: '0 8px 40px rgba(0,0,0,.08)', padding: '40px 36px' }}>
        <div style={{ marginBottom: 28 }}>
          <FalkLogo variant="full" size={36} />
        </div>

        {done ? (
          <div style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 48, marginBottom: 16 }}>✅</div>
            <h1 style={{ fontSize: 22, fontWeight: 800, color: '#111827', margin: '0 0 8px' }}>Senha redefinida!</h1>
            <p style={{ color: '#6B7280', fontSize: 14, marginBottom: 24 }}>Sua senha foi atualizada com sucesso.</p>
            <button
              type="button"
              onClick={() => nav('/login')}
              style={{ width: '100%', height: 49, border: 'none', borderRadius: 11, background: '#1E6FF2', color: '#fff', fontSize: 15.5, fontWeight: 700, cursor: 'pointer' }}
            >
              Ir para o login
            </button>
          </div>
        ) : (
          <form onSubmit={submit}>
            <h1 style={{ fontSize: 22, fontWeight: 800, color: '#111827', margin: '0 0 6px' }}>Nova senha</h1>
            <p style={{ color: '#6B7280', fontSize: 14, margin: '0 0 22px' }}>Escolha uma senha segura para sua conta.</p>

            {err && (
              <div style={{ background: '#FEF2F2', color: '#B91C1C', border: '1px solid #FECACA', borderRadius: 10, padding: '10px 12px', fontSize: 13, marginBottom: 16 }}>
                {err}
              </div>
            )}

            <label style={{ display: 'block', fontSize: 12.5, fontWeight: 600, color: '#374151', margin: '0 0 6px' }}>Nova senha</label>
            <div style={{ position: 'relative', marginBottom: 14 }}>
              <input
                style={{ width: '100%', boxSizing: 'border-box', height: 47, padding: '0 44px 0 14px', borderRadius: 11, border: '1.5px solid #E5E7EB', background: '#F9FAFB', fontSize: 15, color: '#111827', outline: 'none' }}
                type={showPw ? 'text' : 'password'}
                value={senha}
                onChange={(e) => setSenha(e.target.value)}
                placeholder="••••••••"
                minLength={6}
                required
                autoFocus
                disabled={!token}
              />
              <button type="button" onClick={() => setShowPw(v => !v)} style={{ position: 'absolute', right: 8, top: 7, width: 32, height: 32, border: 'none', background: 'transparent', cursor: 'pointer', fontSize: 16 }}>
                {showPw ? '🙈' : '👁️'}
              </button>
            </div>

            <label style={{ display: 'block', fontSize: 12.5, fontWeight: 600, color: '#374151', margin: '0 0 6px' }}>Confirmar senha</label>
            <input
              style={{ width: '100%', boxSizing: 'border-box', height: 47, padding: '0 14px', borderRadius: 11, border: '1.5px solid #E5E7EB', background: '#F9FAFB', fontSize: 15, color: '#111827', outline: 'none', marginBottom: 22 }}
              type={showPw ? 'text' : 'password'}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              placeholder="••••••••"
              minLength={6}
              required
              disabled={!token}
            />

            <button type="submit" style={{ width: '100%', height: 49, border: 'none', borderRadius: 11, background: '#1E6FF2', color: '#fff', fontSize: 15.5, fontWeight: 700, cursor: 'pointer' }} disabled={loading || !token}>
              {loading ? 'Salvando…' : 'Salvar nova senha'}
            </button>
          </form>
        )}
      </div>
    </div>
  )
}
