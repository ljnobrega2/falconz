// AUDIT-2026-06-22 forgot-pw — página pública de redefinição de senha do admin.
// Lê ?token= da URL, pede nova senha + confirmação e chama POST /onboarding/reset.
// Em sucesso, redireciona para /login com mensagem de ok (via state do react-router).
import { FormEvent, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api } from '../api'

const LOGO_LIGHT = `${import.meta.env.BASE_URL}falk-logo-light.png`

export default function ResetPassword() {
  const nav = useNavigate()
  const [params] = useSearchParams()
  const token = (params.get('token') || '').trim()

  const [senha, setSenha] = useState('')
  const [senha2, setSenha2] = useState('')
  const [show, setShow] = useState(false)
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  async function doReset(e: FormEvent) {
    e.preventDefault()
    setErr('')
    if (!token) { setErr('Link inválido — o token não foi encontrado na URL. Solicite uma nova redefinição.'); return }
    if (senha.length < 8) { setErr('A senha deve ter ao menos 8 caracteres.'); return }
    if (senha !== senha2) { setErr('As senhas não conferem.'); return }
    setLoading(true)
    try {
      const r = await api<{ message?: string }>('/onboarding/reset', {
        method: 'POST', body: JSON.stringify({ token, new_password: senha }),
      }, { authRedirect: false })
      // Sucesso → vai para o login com a mensagem de confirmação.
      nav('/login', { replace: true, state: { resetOk: r?.message || 'Senha redefinida com sucesso. Você já pode entrar.' } })
    } catch (e: any) {
      setErr(e.message || 'Não foi possível redefinir a senha. Solicite uma nova redefinição.')
    } finally { setLoading(false) }
  }

  return (
    <div className="flex min-h-screen w-full items-center justify-center bg-slate-50 px-5 py-10 text-slate-900">
      <div className="w-full max-w-[420px]">
        <img src={LOGO_LIGHT} alt="FALK LOG" className="mx-auto mb-7 h-9" />
        <div className="rounded-3xl border border-slate-200 bg-white p-7 shadow-[0_30px_80px_-30px_rgba(15,23,42,.25)] sm:p-8">
          <h1 className="text-[22px] font-bold tracking-tight text-slate-900" style={{ fontFamily: "'Space Grotesk',sans-serif" }}>
            Definir nova senha
          </h1>
          <p className="mt-1 text-[13.5px] text-slate-500">
            Escolha uma nova senha para o seu acesso ao painel FalkLog.
          </p>

          {!token && (
            <div className="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-3.5 py-2.5 text-[13px] text-amber-800">
              Token ausente na URL. Volte e solicite uma nova redefinição de senha.
            </div>
          )}
          {err && (
            <div className="mt-4 rounded-xl border border-red-200 bg-red-50 px-3.5 py-2.5 text-[13px] text-red-700">{err}</div>
          )}

          <form onSubmit={doReset} className="mt-5 space-y-4">
            <div>
              <label className="mb-1.5 block text-[12.5px] font-medium text-slate-600">Nova senha</label>
              <div className="relative">
                <input
                  className="w-full rounded-xl border border-slate-200 bg-slate-50/70 px-3.5 py-3 text-[14.5px] text-slate-900 outline-none transition focus:border-falk-blue focus:bg-white focus:ring-4 focus:ring-falk-blue/10"
                  type={show ? 'text' : 'password'} value={senha} onChange={e => setSenha(e.target.value)}
                  autoComplete="new-password" placeholder="Mínimo 8 caracteres" required
                />
                <button type="button" onClick={() => setShow(v => !v)} tabIndex={-1}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600">
                  {show ? '🙈' : '👁️'}
                </button>
              </div>
            </div>
            <div>
              <label className="mb-1.5 block text-[12.5px] font-medium text-slate-600">Confirmar nova senha</label>
              <input
                className="w-full rounded-xl border border-slate-200 bg-slate-50/70 px-3.5 py-3 text-[14.5px] text-slate-900 outline-none transition focus:border-falk-blue focus:bg-white focus:ring-4 focus:ring-falk-blue/10"
                type={show ? 'text' : 'password'} value={senha2} onChange={e => setSenha2(e.target.value)}
                autoComplete="new-password" placeholder="Repita a senha" required
              />
            </div>
            <button disabled={loading || !token}
              className="w-full rounded-xl bg-falk-blue py-3.5 text-[15px] font-semibold text-white shadow-[0_10px_24px_-8px_rgba(30,111,242,.7)] transition hover:bg-falk-bluedark disabled:opacity-60">
              {loading ? 'Redefinindo…' : 'Redefinir senha'}
            </button>
            <div className="text-center">
              <button type="button" onClick={() => nav('/login')} className="text-[13px] font-medium text-slate-500 hover:text-slate-700">← Voltar para o login</button>
            </div>
          </form>
        </div>
        <p className="mt-6 text-center text-[12px] text-slate-400">
          🦅 FalkLog · Logística de alta performance · Operação nacional
        </p>
      </div>
    </div>
  )
}
