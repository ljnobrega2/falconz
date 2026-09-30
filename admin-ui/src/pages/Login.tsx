import { FormEvent, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import {
  motion,
  AnimatePresence,
  useMotionValue,
  useSpring,
  useTransform,
} from 'framer-motion'
import { api, setToken, BASE, PORTAL_BASE } from '../api'

const LOGO_DARK = `${import.meta.env.BASE_URL}falk-logo-dark.png`   // arte clara p/ fundo escuro
const LOGO_LIGHT = `${import.meta.env.BASE_URL}falk-logo-light.png` // arte escura p/ fundo claro
const FALCON = `${import.meta.env.BASE_URL}falk-falcon-dark.png`   // falcão original preto+azul (ícone notif em fundo branco)

// SIGNUP→CLIENTE: o cadastro nasce ATIVO como role='cliente' e usa o PORTAL (não o
// admin). O signup retorna um token de PORTAL (iss=senderzz-portal). Handoff cross-app:
// gravamos esse token na chave que o portal-ui lê ('sz_portal_token' — NÃO usar o
// setToken importado, que escreve 'sz_admin_token') e navegamos para /portal/.
// Admin (/admin/) e portal (/portal/) são servidos na MESMA ORIGEM em produção, então
// localStorage é compartilhado e o portal-ui já entra logado. Em dev (Vite 5173/5174 =
// origens distintas) o handoff não é exercitável — comportamento production-correct.
const PORTAL_TOKEN_KEY = 'sz_portal_token'
const PORTAL_URL = import.meta.env.VITE_PORTAL_URL || '/portal/'

// ────────────────────────────────────────────────────────────────────────────
// Card institucional com efeito 3D que acompanha o cursor (Framer Motion)
// ────────────────────────────────────────────────────────────────────────────
function TiltCard({
  icon, title, text, foot, delay = 0,
}: { icon: string; title: string; text: string; foot?: string; delay?: number }) {
  const ref = useRef<HTMLDivElement>(null)
  const mx = useMotionValue(0)
  const my = useMotionValue(0)
  const rX = useSpring(useTransform(my, [-0.5, 0.5], [8, -8]), { stiffness: 220, damping: 18 })
  const rY = useSpring(useTransform(mx, [-0.5, 0.5], [-10, 10]), { stiffness: 220, damping: 18 })
  const glowX = useTransform(mx, [-0.5, 0.5], ['0%', '100%'])

  function onMove(e: React.MouseEvent) {
    const r = ref.current?.getBoundingClientRect()
    if (!r) return
    mx.set((e.clientX - r.left) / r.width - 0.5)
    my.set((e.clientY - r.top) / r.height - 0.5)
  }
  function onLeave() { mx.set(0); my.set(0) }

  return (
    <motion.div
      ref={ref}
      onMouseMove={onMove}
      onMouseLeave={onLeave}
      initial={{ opacity: 0, y: 22 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ delay, duration: 0.55, ease: [0.22, 1, 0.36, 1] }}
      style={{ rotateX: rX, rotateY: rY, transformPerspective: 900 }}
      className="group relative rounded-2xl border border-falk-line/70 bg-white/[0.04] p-5 backdrop-blur-sm
                 transition-colors hover:border-falk-blue/50 will-change-transform"
    >
      {/* brilho sutil que segue o cursor */}
      <motion.div
        style={{ left: glowX }}
        className="pointer-events-none absolute -top-px h-px w-24 -translate-x-1/2 bg-gradient-to-r from-transparent via-falk-blue to-transparent opacity-0 group-hover:opacity-90 transition-opacity"
      />
      <div className="absolute inset-0 rounded-2xl bg-gradient-to-br from-falk-blue/[0.07] to-transparent opacity-0 group-hover:opacity-100 transition-opacity" />
      <div className="relative" style={{ transform: 'translateZ(40px)' }}>
        <div className="mb-3 flex h-11 w-11 items-center justify-center rounded-xl bg-falk-blue/15 text-lg ring-1 ring-falk-blue/25">
          {icon}
        </div>
        <h3 className="text-[15px] font-semibold text-white">{title}</h3>
        <p className="mt-1.5 text-[13px] leading-relaxed text-falk-steel">{text}</p>
        {foot && <p className="mt-2 text-[11px] text-falk-steel/70">{foot}</p>}
      </div>
    </motion.div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Notificações flutuantes de autoridade (volume operacional)
// ────────────────────────────────────────────────────────────────────────────
const NOTIFS = [
  // Formato padronizado: título · valor (R$) · cidade · UF. Mistura COD, expedição e afiliados.
  { t: 'Nova venda aprovada', v: 'R$ 297,00', l: 'São Paulo · SP' },
  { t: 'Pagamento na entrega', v: 'R$ 754,90', l: 'Rio de Janeiro · RJ' },
  { t: 'Venda por afiliado', v: 'R$ 489,00', l: 'Campinas · SP' },
  { t: 'Pagamento liberado', v: 'R$ 1.842,00', l: 'Belo Horizonte · MG' },
  { t: 'Pedido expedido', v: 'R$ 312,00', l: 'Curitiba · PR' },
  { t: 'Comissão de afiliado', v: 'R$ 73,00', l: 'Salvador · BA' },
]

function FloatingNotifs() {
  const [i, setI] = useState(0)
  useEffect(() => {
    const id = setInterval(() => setI(v => (v + 1) % NOTIFS.length), 2600)
    return () => clearInterval(id)
  }, [])
  // mostra 3 notificações em pilha, cíclicas
  const shown = [0, 1, 2].map(k => NOTIFS[(i + k) % NOTIFS.length])
  return (
    <div className="relative h-full min-h-[360px] w-full">
      <AnimatePresence initial={false}>
        {shown.map((n, k) => (
          <motion.div
            key={`${i}-${k}`}
            initial={{ opacity: 0, y: 30, scale: 0.96 }}
            animate={{ opacity: 1 - k * 0.18, y: k * 132, scale: 1 - k * 0.03 }}
            exit={{ opacity: 0, y: -24, scale: 0.96 }}
            transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}
            className="absolute left-0 right-0 flex items-center gap-3 rounded-2xl border border-falk-line/70
                       bg-falk-graphite2/80 p-4 backdrop-blur-md shadow-[0_18px_50px_rgba(0,0,0,.45)]"
            style={{ zIndex: 10 - k }}
          >
            <div className="flex h-11 w-11 flex-none items-center justify-center rounded-xl bg-white ring-1 ring-falk-blue/30">
              <img src={FALCON} alt="FALK" className="h-8 w-8 object-contain" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center justify-between">
                <span className="text-[13px] font-semibold text-white">FalkLog</span>
                <span className="text-[10px] uppercase tracking-wide text-falk-steel/70">agora</span>
              </div>
              <div className="truncate text-[13px] text-falk-steel">{n.t}</div>
              {n.v && <div className="text-[14px] font-semibold text-falk-sky">{n.v}</div>}
              <div className="text-[11px] text-falk-steel/70">{n.l}</div>
            </div>
          </motion.div>
        ))}
      </AnimatePresence>
    </div>
  )
}

// ────────────────────────────────────────────────────────────────────────────
// Máscaras de exibição (BR). O STATE guarda o texto formatado, mas o payload do
// cadastro envia SEMPRE só dígitos (whats/doc → onlyDigits). Editar/apagar no
// meio nunca trava: a máscara é recalculada a partir dos dígitos puros.
// ────────────────────────────────────────────────────────────────────────────
function onlyDigits(s: string): string {
  return (s || '').replace(/\D/g, '')
}

// Telefone BR: 11 díg → (00) 00000-0000 (celular) · 10 díg → (00) 0000-0000 (fixo).
// Formata progressivamente enquanto digita; limita a 11 dígitos.
function maskPhoneBR(input: string): string {
  const d = onlyDigits(input).slice(0, 11)
  if (d.length === 0) return ''
  if (d.length <= 2) return `(${d}`
  if (d.length <= 6) return `(${d.slice(0, 2)}) ${d.slice(2)}`
  if (d.length <= 10) return `(${d.slice(0, 2)}) ${d.slice(2, 6)}-${d.slice(6)}` // fixo
  return `(${d.slice(0, 2)}) ${d.slice(2, 7)}-${d.slice(7)}` // celular (11 díg)
}

// CPF/CNPJ: 11 díg → 000.000.000-00 · 14 díg → 00.000.000/0000-00.
// Abaixo de 12 dígitos formata como CPF; a partir de 12, como CNPJ. Limita a 14.
function maskCpfCnpj(input: string): string {
  const d = onlyDigits(input).slice(0, 14)
  if (d.length === 0) return ''
  if (d.length <= 11) {
    // CPF: 000.000.000-00
    let out = d.slice(0, 3)
    if (d.length > 3) out += '.' + d.slice(3, 6)
    if (d.length > 6) out += '.' + d.slice(6, 9)
    if (d.length > 9) out += '-' + d.slice(9, 11)
    return out
  }
  // CNPJ: 00.000.000/0000-00
  let out = d.slice(0, 2)
  out += '.' + d.slice(2, 5)
  out += '.' + d.slice(5, 8)
  out += '/' + d.slice(8, 12)
  if (d.length > 12) out += '-' + d.slice(12, 14)
  return out
}

// ────────────────────────────────────────────────────────────────────────────
// ────────────────────────────────────────────────────────────────────────────
// Força de senha (client-side) — 3 níveis: Fácil / Médio / Difícil.
// Regra (pontos): comprimento>=10, minúscula, maiúscula, número, símbolo.
// Mapa: score<=2 → Fácil (vermelho) · score 3-4 → Médio (amarelo) · score 5 → Difícil (verde).
// O submit do cadastro BLOQUEIA "Fácil" (exige no mínimo "Médio").
type PwLevel = 'facil' | 'medio' | 'dificil'
function passwordStrength(pw: string): { level: PwLevel; score: number } {
  let score = 0
  if (pw.length >= 10) score++
  if (/[a-z]/.test(pw)) score++
  if (/[A-Z]/.test(pw)) score++
  if (/[0-9]/.test(pw)) score++
  if (/[^A-Za-z0-9]/.test(pw)) score++
  const level: PwLevel = score <= 2 ? 'facil' : score <= 4 ? 'medio' : 'dificil'
  return { level, score }
}
const PW_META: Record<PwLevel, { label: string; bar: string; text: string; fill: number }> = {
  facil:   { label: 'Fácil',   bar: 'bg-red-500',    text: 'text-red-600',     fill: 33 },
  medio:   { label: 'Médio',   bar: 'bg-amber-400',  text: 'text-amber-600',   fill: 66 },
  dificil: { label: 'Difícil', bar: 'bg-emerald-500', text: 'text-emerald-600', fill: 100 },
}

// Medidor visual de força — barra colorida + label. Só renderiza quando há senha.
function PasswordStrength({ value }: { value: string }) {
  if (!value) return null
  const { level } = passwordStrength(value)
  const meta = PW_META[level]
  return (
    <div className="mt-2">
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-200">
        <div className={`h-full rounded-full transition-all duration-300 ${meta.bar}`} style={{ width: `${meta.fill}%` }} />
      </div>
      <div className="mt-1 flex items-center justify-between">
        <span className={`text-[11.5px] font-semibold ${meta.text}`}>Força: {meta.label}</span>
        {level === 'facil' && (
          <span className="text-[11px] text-slate-400">Use letras, números e símbolos</span>
        )}
      </div>
    </div>
  )
}

function Field({
  label, type = 'text', value, onChange, autoComplete, placeholder, reveal, onReveal,
}: any) {
  return (
    <div>
      <label className="mb-1.5 block text-[12.5px] font-medium text-slate-600">{label}</label>
      <div className="relative">
        <input
          className="w-full rounded-xl border border-slate-200 bg-slate-50/70 px-3.5 py-3 text-[14.5px] text-slate-900
                     outline-none transition focus:border-falk-blue focus:bg-white focus:ring-4 focus:ring-falk-blue/10"
          type={type} value={value} onChange={onChange} autoComplete={autoComplete} placeholder={placeholder} required
        />
        {onReveal && (
          <button type="button" onClick={onReveal} tabIndex={-1}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600">
            {reveal ? '🙈' : '👁️'}
          </button>
        )}
      </div>
    </div>
  )
}

export default function Login() {
  const nav = useNavigate()
  const [mode, setMode] = useState<'login' | 'signup' | 'forgot'>('login')
  const [email, setEmail] = useState('')
  const [senha, setSenha] = useState('')
  const [showPwd, setShowPwd] = useState(false)
  // LOGIN-UNICO 2FA — etapa de código 2FA do PORTAL. O /admin/login não completa 2FA
  // (não tem o endpoint), então quando uma conta de portal exige 2FA caímos no
  // /portal/login (que emite partial_token) e completamos via /portal/login/2fa aqui.
  const [twoFA, setTwoFA] = useState(false)
  const [codigo, setCodigo] = useState('')
  const [partialToken, setPartialToken] = useState('')
  // cadastro
  const [nome, setNome] = useState('')
  const [doc, setDoc] = useState('') // CPF ou CNPJ (obrigatório no cadastro)
  const [cEmail, setCEmail] = useState('')
  const [whats, setWhats] = useState('')
  const [cSenha, setCSenha] = useState('')
  const [cSenha2, setCSenha2] = useState('')
  const [showCPwd, setShowCPwd] = useState(false)
  const [acceptTerms, setAcceptTerms] = useState(false) // aceite de termos/privacidade (obrigatório)

  // recuperação de senha (forgot)
  const [fEmail, setFEmail] = useState('')
  const [devResetUrl, setDevResetUrl] = useState('')

  // REF-PAYOUT 463 — indicação (link /r/{code} → ?ref=CODE). Capturada da URL e
  // enviada no cadastro; resolvemos o nome do indicador só para feedback visual.
  const [ref, setRef] = useState('')
  const [refName, setRefName] = useState('')

  const [err, setErr] = useState('')
  const [ok, setOk] = useState('')
  const [loading, setLoading] = useState(false)
  // FALLBACK signup sem token: conta criada mas sem auto-login → mostra CTA p/ o portal.
  const [signupNeedsLogin, setSignupNeedsLogin] = useState(false)

  // AUDIT-2026-06-22 forgot-pw — mensagem de sucesso vinda do /reset (location state).
  const loc = useLocation()
  useEffect(() => {
    const st = loc.state as { resetOk?: string } | null
    if (st?.resetOk) {
      setOk(st.resetOk)
      // limpa o state para não reaparecer em refresh/navegação.
      window.history.replaceState({}, '')
    }
  }, [loc.state])

  // REF-PAYOUT 463 — captura ?ref=CODE da URL (link de indicação /r/{code}). Se houver
  // código, abre o cadastro e resolve o nome do indicador (best-effort — falha = ignora).
  useEffect(() => {
    const code = (new URLSearchParams(loc.search).get('ref') || '').trim()
    if (!code) return
    setRef(code)
    setMode('signup')
    api<{ ok: boolean; nome?: string }>(
      `/onboarding/referral/${encodeURIComponent(code)}`, {}, { authRedirect: false },
    )
      .then(rr => { if (rr?.ok && rr.nome) setRefName(rr.nome) })
      .catch(() => {})
  }, [loc.search])

  // primeiro acesso: sem admin → cria o 1º acesso
  useEffect(() => {
    api<{ admin_users_count: number }>('/onboarding/setup-status', {}, { authRedirect: false })
      .then(s => { if (s && s.admin_users_count === 0) nav('/setup') })
      .catch(() => {})
  }, [])

  // PÁGINA ÚNICA DE LOGIN (FEAT-RBAC / LOGIN-UNICO). Esta tela (raiz /) é o ÚNICO
  // ponto de entrada para TODOS os papéis. O backend /admin/login já é RBAC: tenta
  // admin_users e, se não casar, portal_users (emitindo um token de PORTAL já com
  // sessão válida). Aqui só falta ROTEAR pelo papel e fazer o handoff cross-SPA.
  //
  // Fluxo:
  //  1. normaliza email (trim+lowercase — corrige o bug case-sensitive do portal).
  //  2. POST /admin/login (raw fetch — precisamos do CODE do erro, não só da message).
  //  3. 200 + resposta tem `admin` (sinal explícito do backend) → ADMIN: grava
  //     sz_admin_token e nav('/') no SPA admin.
  //  4. 200 sem `admin` (papel de portal: produtor/afiliado/cliente/operator) →
  //     grava o JWT de portal em sz_portal_token (chave que o portal-ui lê; MESMA
  //     ORIGEM /admin/ + /portal/ ⇒ localStorage compartilhado) e
  //     window.location.assign('/portal/') — portal abre JÁ LOGADO.
  //  5. 403 requires_2fa → conta de portal com 2FA: o /admin/login não completa o
  //     desafio, então caímos no /portal/login (emite partial_token) e mostramos a
  //     etapa de código, completada por doLogin2fa via /portal/login/2fa.
  //  6. 401 → credenciais inválidas (em AMBAS as tabelas; não adianta tentar portal).
  async function doLogin(e: FormEvent) {
    e.preventDefault(); setErr(''); setOk(''); setLoading(true)
    const normEmail = email.trim().toLowerCase()
    try {
      const res = await fetch(`${BASE}/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: normEmail, senha }),
      })
      const body = await res.json().catch(() => ({} as any))

      if (res.ok) {
        const token: string = body?.token || ''
        if (!token) { setErr('Resposta inesperada do servidor'); return }
        // Discriminador backend-explícito: só ADMIN traz o campo `admin`.
        if (body?.admin) {
          setToken(token)        // grava em sz_admin_token
          nav('/')
        } else {
          // Papel de PORTAL — handoff cross-SPA (mesmo padrão do signup→cliente).
          try { localStorage.setItem(PORTAL_TOKEN_KEY, token) } catch { /* storage off */ }
          window.location.assign(PORTAL_URL)
        }
        return
      }

      // 403 requires_2fa → conta de portal com 2FA: completa o desafio no portal.
      if (res.status === 403 && body?.error?.code === 'requires_2fa') {
        await startPortal2fa(normEmail)
        return
      }

      // 401 (ou qualquer outro) → mostra a mensagem do servidor inline.
      setErr(body?.error?.message || 'Credenciais inválidas')
    } catch (e: any) {
      setErr(e?.message || 'Não foi possível conectar ao servidor.')
    } finally { setLoading(false) }
  }

  // Inicia o fluxo 2FA do PORTAL: o /admin/login não emite partial_token, então
  // re-autenticamos no /portal/login (mesmas credenciais) só para obter o
  // partial_token e disparar o envio do código. Mantém o loading do submit pai.
  async function startPortal2fa(normEmail: string) {
    try {
      const res = await fetch(`${PORTAL_BASE}/portal/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: normEmail, senha }),
      })
      const body = await res.json().catch(() => ({} as any))
      if (res.ok && body?.requires_2fa && body?.partial_token) {
        setPartialToken(body.partial_token)
        setTwoFA(true)
        setOk('Enviamos um código de verificação para o seu e-mail.')
        return
      }
      // Não pediu 2FA (ex.: estado mudou) mas trouxe token → entra direto no portal.
      if (res.ok && body?.token) {
        try { localStorage.setItem(PORTAL_TOKEN_KEY, body.token) } catch { /* off */ }
        window.location.assign(PORTAL_URL)
        return
      }
      setErr(body?.erro || body?.error?.message || 'Não foi possível iniciar a verificação 2FA.')
    } catch (e: any) {
      setErr(e?.message || 'Não foi possível conectar ao servidor.')
    }
  }

  // Completa o 2FA do portal: troca partial_token + código pelo JWT final e faz o
  // handoff cross-SPA para /portal/.
  async function doLogin2fa(e: FormEvent) {
    e.preventDefault(); setErr(''); setLoading(true)
    try {
      const res = await fetch(`${PORTAL_BASE}/portal/login/2fa`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ partial_token: partialToken, codigo }),
      })
      const body = await res.json().catch(() => ({} as any))
      if (res.ok && body?.token) {
        try { localStorage.setItem(PORTAL_TOKEN_KEY, body.token) } catch { /* off */ }
        window.location.assign(PORTAL_URL)
        return
      }
      setErr(body?.erro || body?.error?.message || 'Código inválido')
    } catch (e: any) {
      setErr(e?.message || 'Não foi possível conectar ao servidor.')
    } finally { setLoading(false) }
  }

  async function doSignup(e: FormEvent) {
    e.preventDefault(); setErr(''); setOk('')
    const docDigits = doc.replace(/\D/g, '')
    if (docDigits.length !== 11 && docDigits.length !== 14) { setErr('Informe um CPF (11 dígitos) ou CNPJ (14 dígitos) válido.'); return }
    if (cSenha.length < 8) { setErr('A senha deve ter ao menos 8 caracteres.'); return }
    // BLOQUEIO de força: senha "Fácil" é recusada (mínimo exigido = "Médio").
    if (passwordStrength(cSenha).level === 'facil') {
      setErr('Senha muito fraca. Use ao menos 10 caracteres combinando maiúsculas, minúsculas, números e símbolos (força mínima: Médio).')
      return
    }
    if (cSenha !== cSenha2) { setErr('As senhas não conferem.'); return }
    if (!acceptTerms) { setErr('Você precisa aceitar os Termos de Uso e a Política de Privacidade.'); return }
    setLoading(true)
    try {
      // SIGNUP→CLIENTE: a conta nasce ATIVA como role='cliente'. O backend retorna
      // { id, ok, role:'cliente', token?, user? }. token/user só vêm se a emissão da
      // sessão portal teve sucesso (best-effort) — por isso são opcionais aqui.
      const r = await api<{ id: number; ok: boolean; role?: string; token?: string }>(
        '/onboarding/signup',
        {
          method: 'POST',
          body: JSON.stringify({ nome, email: cEmail, whatsapp: onlyDigits(whats), documento: docDigits, senha: cSenha, ref }),
        },
        { authRedirect: false },
      )
      setNome(''); setCEmail(''); setWhats(''); setDoc(''); setCSenha(''); setCSenha2(''); setAcceptTerms(false)
      // Conta nasce ATIVA como 'cliente' (sem aprovação). AUTO-LOGIN: grava o token de
      // PORTAL na chave que o portal-ui lê ('sz_portal_token', MESMA ORIGEM /portal/ + /admin/)
      // e navega pro portal JÁ LOGADO. window.location (não nav()) p/ sair do basename /admin/.
      if (r?.token) {
        setOk('Conta criada! Entrando no portal…')
        try { localStorage.setItem(PORTAL_TOKEN_KEY, r.token) } catch { /* storage indisponível */ }
        window.location.assign(PORTAL_URL)
        return
      }
      // Fallback (token ausente — emissão best-effort falhou): conta criada, sem auto-login.
      setOk('Conta criada! Sua conta de cliente já está ativa — acesse pelo portal.')
    } catch (e: any) { setErr(e.message || 'Não foi possível concluir o cadastro.') }
    finally { setLoading(false) }
  }

  async function doForgot(e: FormEvent) {
    e.preventDefault(); setErr(''); setOk(''); setDevResetUrl(''); setLoading(true)
    try {
      // O backend SEMPRE responde 200 genérico (anti-enumeração). Em DEV pode vir
      // dev_reset_url — mostramos como link clicável para teste sem SMTP.
      const r = await api<{ message?: string; dev_reset_url?: string }>('/onboarding/forgot', {
        method: 'POST', body: JSON.stringify({ email: fEmail }),
      }, { authRedirect: false })
      setOk(r?.message || 'Se o e-mail existir, enviamos as instruções.')
      if (r?.dev_reset_url) setDevResetUrl(r.dev_reset_url)
    } catch (e: any) {
      setErr(e.message || 'Não foi possível processar a solicitação.')
    } finally { setLoading(false) }
  }

  // Troca de modo zerando mensagens/estado transitório.
  function switchMode(m: 'login' | 'signup' | 'forgot') {
    setMode(m); setErr(''); setOk(''); setDevResetUrl(''); setSignupNeedsLogin(false)
    setTwoFA(false); setCodigo(''); setPartialToken(''); setAcceptTerms(false)
  }

  return (
    <div className="flex min-h-screen w-full bg-slate-50 text-slate-900">
      {/* ───────────── ÁREA ESQUERDA — institucional ───────────── */}
      <div className="relative hidden w-[56%] flex-col justify-center overflow-hidden bg-falk-ink px-10 py-9 lg:flex xl:px-14">
        {/* fundo: gradiente + grade + brilho azul */}
        <div className="pointer-events-none absolute inset-0 bg-gradient-to-br from-falk-graphite via-falk-ink to-black" />
        <div className="pointer-events-none absolute inset-0 opacity-[0.5]"
          style={{ background: 'radial-gradient(900px 500px at 85% -10%, rgba(30,111,242,.22), transparent 60%)' }} />
        <div className="pointer-events-none absolute inset-0 opacity-[0.06]"
          style={{ backgroundImage: 'linear-gradient(#fff 1px,transparent 1px),linear-gradient(90deg,#fff 1px,transparent 1px)', backgroundSize: '46px 46px' }} />
        {/* falcão estratégico (watermark) */}
        <img src={LOGO_DARK} alt="" className="pointer-events-none absolute -right-10 bottom-8 h-44 select-none opacity-[0.06]" />

        <div className="relative w-full">
          <motion.h1 initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.6 }}
            className="max-w-2xl text-[34px] font-bold leading-[1.08] tracking-tight text-white xl:text-[40px]"
            style={{ fontFamily: "'Space Grotesk','Inter',sans-serif" }}>
            <span className="text-falk-sky">Fulfillment em todo Brasil</span> e Cash On Delivery.
          </motion.h1>
          <motion.p initial={{ opacity: 0, y: 16 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.6, delay: 0.1 }}
            className="mt-4 max-w-md text-[15px] leading-relaxed text-falk-steel">
            Venda pro Brasil inteiro por transportadora ou motoboy. Sua marca, seus afiliados, numa plataforma só.
          </motion.p>

          {/* Faixa do programa de afiliados — gancho comercial */}
          <motion.a href="https://app.falklog.com.br"
            initial={{ opacity: 0, y: 14 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.6, delay: 0.18 }}
            className="group mt-4 inline-flex items-center gap-3 rounded-full border border-falk-blue/30 bg-falk-blue/10 py-2 pl-2.5 pr-4 backdrop-blur-sm transition hover:border-falk-blue/60 hover:bg-falk-blue/[0.16]">
            <span className="flex h-7 w-7 flex-none items-center justify-center rounded-full bg-falk-blue/20 text-[13px] ring-1 ring-falk-blue/30">🤝</span>
            <span className="text-[13px] font-medium text-white">
              Programa de afiliados — <span className="text-falk-sky">ganhe comissão indicando</span>, sem estoque e sem risco
            </span>
            <span className="text-falk-sky transition group-hover:translate-x-0.5">→</span>
          </motion.a>

          {/* cards + notificações lado a lado */}
          <div className="mt-7 flex items-stretch gap-6">
            <div className="grid grid-cols-2 gap-4 flex-1 max-w-[560px]">
              <TiltCard icon="🔄" title="Sistema Boomerang" text="O menor índice de pedidos frustrados: tentativa, retorno e reentrega sem dor de cabeça." delay={0.15} />
              <TiltCard icon="🎁" title="Frustração Gratuita*" text="Sem cobrança de taxa de frustração para o produtor." foot="*Válido para a primeira entrega." delay={0.22} />
              <TiltCard icon="⚡" title="Saque em até 7 dias**" text="Mais previsibilidade para o seu fluxo de caixa." foot="**A partir de 300 pedidos mensais." delay={0.29} />
              <TiltCard icon="🇧🇷" title="Fulfillment Nacional" text="Centros de distribuição e múltiplas transportadoras, de Norte a Sul do Brasil." delay={0.36} />
            </div>
            <div className="hidden w-[360px] flex-none items-stretch xl:flex">
              <FloatingNotifs />
            </div>
          </div>
        </div>
      </div>

      {/* ───────────── ÁREA DIREITA — login / cadastro ───────────── */}
      <div className="flex w-full flex-1 items-center justify-center px-5 py-10 sm:px-8">
        <motion.div initial={{ opacity: 0, y: 18 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.5 }}
          className="w-full max-w-[420px]">
          <img src={LOGO_LIGHT} alt="FALK LOG" className="mb-7 h-9 lg:hidden" />

          <div className="rounded-3xl border border-slate-200 bg-white p-7 shadow-[0_30px_80px_-30px_rgba(15,23,42,.25)] sm:p-8">
            <div className="mb-6 flex items-center gap-2">
              <img src={LOGO_LIGHT} alt="" className="hidden h-8 lg:block" />
            </div>

            {/* Toggle — escondido no modo "forgot" (fluxo dedicado de recuperação). */}
            {mode !== 'forgot' && (
              <div className="mb-6 grid grid-cols-2 rounded-xl bg-slate-100 p-1">
                {(['login', 'signup'] as const).map(m => (
                  <button key={m} onClick={() => switchMode(m)}
                    className={`relative rounded-lg py-2 text-[14px] font-semibold transition ${mode === m ? 'text-white' : 'text-slate-500 hover:text-slate-700'}`}>
                    {mode === m && (
                      <motion.span layoutId="falk-toggle" className="absolute inset-0 rounded-lg bg-falk-blue shadow-sm"
                        transition={{ type: 'spring', stiffness: 380, damping: 30 }} />
                    )}
                    <span className="relative">{m === 'login' ? 'Entrar' : 'Criar conta'}</span>
                  </button>
                ))}
              </div>
            )}

            <h1 className="text-[22px] font-bold tracking-tight text-slate-900" style={{ fontFamily: "'Space Grotesk',sans-serif" }}>
              {mode === 'login' ? 'Acesse sua conta' : mode === 'signup' ? 'Crie sua conta' : 'Recuperar senha'}
            </h1>
            <p className="mt-1 text-[13.5px] text-slate-500">
              {mode === 'login' ? 'Entre para acessar sua operação FalkLog.'
                : mode === 'signup' ? 'Cadastre-se e acesse na hora — sem espera de aprovação.'
                : 'Informe o e-mail da sua conta e enviaremos as instruções de redefinição.'}
            </p>

            <AnimatePresence>
              {err && (
                <motion.div initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} exit={{ opacity: 0, height: 0 }}
                  className="mt-4 rounded-xl border border-red-200 bg-red-50 px-3.5 py-2.5 text-[13px] text-red-700">{err}</motion.div>
              )}
              {ok && (
                <motion.div initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} exit={{ opacity: 0, height: 0 }}
                  className="mt-4 rounded-xl border border-emerald-200 bg-emerald-50 px-3.5 py-2.5 text-[13px] text-emerald-700">
                  {ok}
                  {/* FALLBACK signup sem token: leva o usuário ao login do portal (não o deixa travado). */}
                  {signupNeedsLogin && (
                    <a href={PORTAL_URL}
                      className="mt-2 block rounded-lg bg-falk-blue px-3 py-2 text-center text-[13px] font-semibold text-white transition hover:bg-falk-bluedark">
                      Acessar o portal
                    </a>
                  )}
                </motion.div>
              )}
            </AnimatePresence>

            <AnimatePresence mode="wait">
              {mode === 'login' && twoFA ? (
                /* LOGIN-UNICO — etapa 2FA do portal (conta de portal com 2FA habilitado). */
                <motion.form key="2fa" onSubmit={doLogin2fa} initial={{ opacity: 0, x: -12 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 12 }}
                  transition={{ duration: 0.25 }} className="mt-5 space-y-4">
                  <Field label="Código de verificação" type="text" value={codigo} onChange={(e: any) => setCodigo(e.target.value.replace(/\D/g, '').slice(0, 6))} autoComplete="one-time-code" placeholder="000000" />
                  <button disabled={loading} className="w-full rounded-xl bg-falk-blue py-3.5 text-[15px] font-semibold text-white shadow-[0_10px_24px_-8px_rgba(30,111,242,.7)] transition hover:bg-falk-bluedark disabled:opacity-60">
                    {loading ? 'Verificando…' : 'Verificar e entrar'}
                  </button>
                  <div className="text-center">
                    <button type="button" onClick={() => { setTwoFA(false); setCodigo(''); setPartialToken(''); setErr(''); setOk('') }} className="text-[13px] font-medium text-slate-500 hover:text-slate-700">← Voltar para o login</button>
                  </div>
                </motion.form>
              ) : mode === 'login' ? (
                <motion.form key="login" onSubmit={doLogin} initial={{ opacity: 0, x: -12 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 12 }}
                  transition={{ duration: 0.25 }} className="mt-5 space-y-4">
                  <Field label="E-mail" type="email" value={email} onChange={(e: any) => setEmail(e.target.value)} autoComplete="email" placeholder="voce@empresa.com.br" />
                  <Field label="Senha" type={showPwd ? 'text' : 'password'} value={senha} onChange={(e: any) => setSenha(e.target.value)} autoComplete="current-password" placeholder="••••••••" reveal={showPwd} onReveal={() => setShowPwd(v => !v)} />
                  <div className="flex justify-end">
                    <button type="button" onClick={() => { setFEmail(email); switchMode('forgot') }} className="text-[13px] font-medium text-falk-blue hover:text-falk-bluedark">Esqueci minha senha</button>
                  </div>
                  <button disabled={loading} className="w-full rounded-xl bg-falk-blue py-3.5 text-[15px] font-semibold text-white shadow-[0_10px_24px_-8px_rgba(30,111,242,.7)] transition hover:bg-falk-bluedark disabled:opacity-60">
                    {loading ? 'Entrando…' : 'Entrar'}
                  </button>
                </motion.form>
              ) : mode === 'signup' ? (
                <motion.form key="signup" onSubmit={doSignup} initial={{ opacity: 0, x: 12 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -12 }}
                  transition={{ duration: 0.25 }} className="mt-5 space-y-4">
                  {/* REF-PAYOUT 463 — indicação capturada do link /r/{code}. */}
                  {refName && (
                    <div className="rounded-xl border border-falk-blue/20 bg-falk-blue/[0.06] px-3.5 py-2.5 text-[13px] text-slate-700">
                      🤝 Indicado por <span className="font-semibold text-slate-900">{refName}</span>
                    </div>
                  )}
                  <Field label="Nome" value={nome} onChange={(e: any) => setNome(e.target.value)} autoComplete="name" placeholder="Seu nome completo" />
                  <Field label="E-mail" type="email" value={cEmail} onChange={(e: any) => setCEmail(e.target.value)} autoComplete="email" placeholder="voce@empresa.com.br" />
                  <Field label="WhatsApp" value={whats} onChange={(e: any) => setWhats(maskPhoneBR(e.target.value))} autoComplete="tel" placeholder="(11) 90000-0000" />
                  <Field label="CPF ou CNPJ" value={doc} onChange={(e: any) => setDoc(maskCpfCnpj(e.target.value))} autoComplete="off" placeholder="CPF (pessoa física) ou CNPJ (empresa)" />
                  <div>
                    <Field label="Senha" type={showCPwd ? 'text' : 'password'} value={cSenha} onChange={(e: any) => setCSenha(e.target.value)} autoComplete="new-password" placeholder="Mínimo 8 caracteres" reveal={showCPwd} onReveal={() => setShowCPwd(v => !v)} />
                    {/* Medidor de força (client-side) — Fácil/Médio/Difícil. Submit bloqueia "Fácil". */}
                    <PasswordStrength value={cSenha} />
                  </div>
                  <Field label="Confirmar senha" type={showCPwd ? 'text' : 'password'} value={cSenha2} onChange={(e: any) => setCSenha2(e.target.value)} autoComplete="new-password" placeholder="Repita a senha" />
                  {/* Aceite de termos — logo após os campos, ANTES do botão (não isolado no rodapé). */}
                  <label className="flex cursor-pointer items-start gap-2.5 text-[12.5px] leading-snug text-slate-600">
                    <input
                      type="checkbox"
                      checked={acceptTerms}
                      onChange={(e) => setAcceptTerms(e.target.checked)}
                      className="mt-0.5 h-4 w-4 flex-none cursor-pointer rounded border-slate-300 text-falk-blue accent-falk-blue focus:ring-falk-blue/30"
                    />
                    <span>
                      Li e aceito os{' '}
                      <a href="/termos.html" target="_blank" rel="noopener noreferrer" className="font-semibold text-falk-blue hover:text-falk-bluedark">Termos de Uso</a>
                      {' '}e a{' '}
                      <a href="/privacidade.html" target="_blank" rel="noopener noreferrer" className="font-semibold text-falk-blue hover:text-falk-bluedark">Política de Privacidade</a>.
                    </span>
                  </label>
                  <button disabled={loading} className="w-full rounded-xl bg-falk-blue py-3.5 text-[15px] font-semibold text-white shadow-[0_10px_24px_-8px_rgba(30,111,242,.7)] transition hover:bg-falk-bluedark disabled:opacity-60">
                    {loading ? 'Enviando…' : 'Criar conta'}
                  </button>
                </motion.form>
              ) : (
                <motion.form key="forgot" onSubmit={doForgot} initial={{ opacity: 0, x: -12 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 12 }}
                  transition={{ duration: 0.25 }} className="mt-5 space-y-4">
                  <Field label="E-mail" type="email" value={fEmail} onChange={(e: any) => setFEmail(e.target.value)} autoComplete="email" placeholder="voce@empresa.com.br" />
                  {devResetUrl && (
                    <div className="rounded-xl border border-amber-200 bg-amber-50 px-3.5 py-2.5 text-[12.5px] text-amber-800">
                      <span className="font-semibold">Modo desenvolvimento</span> — link de redefinição:
                      <a href={devResetUrl} className="mt-1 block break-all font-medium text-falk-blue hover:text-falk-bluedark">{devResetUrl}</a>
                    </div>
                  )}
                  <button disabled={loading} className="w-full rounded-xl bg-falk-blue py-3.5 text-[15px] font-semibold text-white shadow-[0_10px_24px_-8px_rgba(30,111,242,.7)] transition hover:bg-falk-bluedark disabled:opacity-60">
                    {loading ? 'Enviando…' : 'Enviar instruções'}
                  </button>
                  <div className="text-center">
                    <button type="button" onClick={() => switchMode('login')} className="text-[13px] font-medium text-slate-500 hover:text-slate-700">← Voltar para o login</button>
                  </div>
                </motion.form>
              )}
            </AnimatePresence>
          </div>

          <p className="mt-6 text-center text-[12px] text-slate-400">
            🦅 FalkLog · Logística de alta performance · Operação nacional
          </p>
        </motion.div>
      </div>
    </div>
  )
}
