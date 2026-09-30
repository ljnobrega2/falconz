// Login — entrada do Portal FALK LOG.
// Identidade PRÓPRIA (não-clone): painel grafite à esquerda com a marca + proposta
// de valor em features (não cards de notificação), formulário branco à direita.
// Paleta: grafite (#1F2937/#0F172A) + azul FALK (#1E6FF2) + branco. Copy original.
import { FormEvent, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, setToken } from '../api'
import FalkLogo from '../components/FalkLogo'
import DpoFooter from '../components/DpoFooter'

type LoginResp = { requires_2fa: boolean; token?: string; partial_token?: string }

// Diferenciais de MARKETING (pré-login). Os 3 prioritários do dono — Estoque
// rastreado e Multi-transportadora — abrem a grade; COD e Rastreio white-label
// (bons features atuais) seguem. "Frustração 0" é o destaque nº 1 (hero, fora da
// grade). Removidos: "Estoque dinâmico" (substituído por "Estoque rastreado") e
// "Comissões automáticas" (comissão = dado pessoal, não cabe no pré-login).
const FEATURES = [
  { ic: '📍', t: 'Estoque rastreado ponta a ponta', d: 'Do recebimento à entrega — cada item visível em todo o trajeto.' },
  { ic: '🚚', t: 'Expedição multi-transportadora', d: 'Várias transportadoras, cobertura nacional — Brasil inteiro.' },
  { ic: '📦', t: 'Pagamento na entrega', d: 'Cash on Delivery & Pay After Delivery, sem risco de chargeback.' },
  { ic: '🛰️', t: 'Rastreio white-label', d: 'Página de rastreio e etiqueta com a sua marca, do clique à porta.' },
]

// Prova social ILUSTRATIVA — nomes e cidades FICTÍCIOS. NÃO são dados reais de
// cliente (LGPD): jamais exibir nome real de comprador aqui. Rotaciona suave no
// rodapé do painel grafite.
const SOCIAL_PROOF: { name: string; city: string }[] = [
  { name: 'Maria S.', city: 'São Paulo · SP' },
  { name: 'João P.', city: 'Rio de Janeiro · RJ' },
  { name: 'Ana L.', city: 'Belo Horizonte · MG' },
  { name: 'Carlos M.', city: 'Curitiba · PR' },
  { name: 'Fernanda R.', city: 'Porto Alegre · RS' },
  { name: 'Pedro H.', city: 'Salvador · BA' },
]

export default function Login() {
  const nav = useNavigate()
  const [phase, setPhase] = useState<'creds' | '2fa' | 'forgot' | 'forgot-sent'>('creds')
  const [email, setEmail] = useState('')
  const [senha, setSenha] = useState('')
  const [showPw, setShowPw] = useState(false)
  const [codigo, setCodigo] = useState('')
  const [partialToken, setPartialToken] = useState('')
  const [forgotEmail, setForgotEmail] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  // Feed de prova social (ilustrativo) — índice rotaciona a cada 3.2s; cleanup no unmount.
  const [feedIdx, setFeedIdx] = useState(0)
  useEffect(() => {
    const id = setInterval(() => setFeedIdx((i) => (i + 1) % SOCIAL_PROOF.length), 3200)
    return () => clearInterval(id)
  }, [])
  const proof = SOCIAL_PROOF[feedIdx]

  async function submitCreds(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setLoading(true)
    try {
      const r = await api<LoginResp>(
        '/portal/login',
        { method: 'POST', body: JSON.stringify({ email, senha }) },
        { authRedirect: false },
      )
      if (r.requires_2fa) {
        setPartialToken(r.partial_token || '')
        setPhase('2fa')
      } else if (r.token) {
        setToken(r.token)
        nav('/')
      } else setErr('Resposta inesperada do servidor')
    } catch (e: any) {
      setErr(e.message || 'Credenciais inválidas')
    } finally {
      setLoading(false)
    }
  }

  async function submitForgot(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setLoading(true)
    try {
      await api('/portal/forgot-password', {
        method: 'POST',
        body: JSON.stringify({ email: forgotEmail }),
      }, { authRedirect: false })
      setPhase('forgot-sent')
    } catch (e: any) {
      setErr(e.message || 'Erro ao enviar. Tente novamente.')
    } finally {
      setLoading(false)
    }
  }

  async function submit2fa(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setLoading(true)
    try {
      const r = await api<{ token: string }>(
        '/portal/login/2fa',
        { method: 'POST', body: JSON.stringify({ partial_token: partialToken, codigo }) },
        { authRedirect: false },
      )
      setToken(r.token)
      nav('/')
    } catch (e: any) {
      setErr(e.message || 'Código inválido')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={S.shell}>
      {/* ── Painel grafite (marca + valor) ─────────────────────────────────── */}
      <aside style={S.brand}>
        <div style={S.grid} aria-hidden />
        <div style={S.glow} aria-hidden />
        <div style={S.brandInner}>
          <FalkLogo variant="full" size={38} tone="light" />
          <div style={{ marginTop: 'auto' }}>
            <h2 style={S.bH2}>
              Receba na entrega.
              <br />
              <span style={{ color: '#5B97F9' }}>Cresça sem risco.</span>
            </h2>
            <p style={S.bP}>
              A plataforma white-label de logística e Cash on Delivery — do checkout à
              comissão, com a sua marca em cada etapa.
            </p>

            {/* Diferencial nº 1 — "Frustração 0" (destaque, largura total) */}
            <div style={S.hero}>
              <span style={S.heroBadge}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M9 12l2 2 4-4" />
                  <path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z" />
                </svg>
                Frustração 0
              </span>
              <div style={S.heroTitle}>A 1ª frustração de cada cliente é por nossa conta.</div>
              <div style={S.heroSub}>Na primeira entrega de cada novo cliente, se algo der errado, a frustração é por nossa conta. É o nosso diferencial nº 1.</div>
            </div>

            <div style={S.feats}>
              {FEATURES.map((f, i) => (
                <div key={i} style={S.feat}>
                  <span style={S.featIc}>{f.ic}</span>
                  <div>
                    <div style={S.featT}>{f.t}</div>
                    <div style={S.featD}>{f.d}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Prova social ILUSTRATIVA — nomes fictícios (LGPD), rotaciona suave. */}
          <div style={S.feed} aria-live="polite">
            <span style={S.feedDot} aria-hidden />
            <span style={S.feedTxt}>
              Entregue agora — <b style={{ color: '#fff', fontWeight: 700 }}>{proof.name}</b>
              <span style={{ color: 'rgba(255,255,255,.42)' }}> · {proof.city}</span>
            </span>
          </div>
          <p style={S.bFoot}>Exemplos ilustrativos · FALK LOG · Logística white-label · © 2026</p>
        </div>
      </aside>

      {/* ── Formulário (branco) ────────────────────────────────────────────── */}
      <main style={S.formSide}>
        <div style={S.formWrap}>
          <div style={S.mobLogo}>
            <FalkLogo variant="full" size={36} />
          </div>

          {phase === 'creds' ? (
            <form onSubmit={submitCreds}>
              <h1 style={S.h1}>Acesse sua conta</h1>
              <p style={S.sub}>Entre para gerenciar entregas, estoque e comissões.</p>

              {err && <div style={S.err}>{err}</div>}

              <label style={S.label}>E-mail</label>
              <input
                style={S.input}
                type="email"
                autoComplete="username"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="seu@email.com"
                required
              />

              <label style={S.label}>Senha</label>
              <div style={{ position: 'relative' }}>
                <input
                  style={{ ...S.input, paddingRight: 44 }}
                  type={showPw ? 'text' : 'password'}
                  autoComplete="current-password"
                  value={senha}
                  onChange={(e) => setSenha(e.target.value)}
                  placeholder="••••••••"
                  required
                />
                <button type="button" onClick={() => setShowPw((v) => !v)} style={S.eye} aria-label="Mostrar senha">
                  {showPw ? '🙈' : '👁️'}
                </button>
              </div>

              <div style={{ textAlign: 'right', margin: '8px 0 22px' }}>
                <a href="#" style={S.link} onClick={(e) => { e.preventDefault(); setForgotEmail(email); setErr(''); setPhase('forgot') }}>
                  Esqueci minha senha
                </a>
              </div>

              <button type="submit" style={S.btn} disabled={loading}>
                {loading ? 'Entrando…' : 'Entrar na plataforma'}
              </button>
            </form>
          ) : (
            <form onSubmit={submit2fa}>
              <h1 style={S.h1}>Verificação em 2 etapas</h1>
              <p style={S.sub}>Digite o código de 6 dígitos enviado a você.</p>
              {err && <div style={S.err}>{err}</div>}
              <label style={S.label}>Código</label>
              <input
                style={{ ...S.input, letterSpacing: 8, textAlign: 'center', fontSize: 22 }}
                inputMode="numeric"
                maxLength={6}
                value={codigo}
                onChange={(e) => setCodigo(e.target.value.replace(/\D/g, ''))}
                placeholder="000000"
                required
                autoFocus
              />
              <button type="submit" style={{ ...S.btn, marginTop: 22 }} disabled={loading}>
                {loading ? 'Validando…' : 'Confirmar'}
              </button>
              <button
                type="button"
                style={S.btnGhost}
                onClick={() => {
                  setPhase('creds')
                  setCodigo('')
                  setPartialToken('')
                  setErr('')
                }}
              >
                Voltar
              </button>
            </form>
          )}

          {phase === 'forgot' && (
            <form onSubmit={submitForgot}>
              <h1 style={S.h1}>Redefinir senha</h1>
              <p style={S.sub}>Informe seu e-mail e enviaremos um link para criar uma nova senha.</p>
              {err && <div style={S.err}>{err}</div>}
              <label style={S.label}>E-mail</label>
              <input
                style={S.input}
                type="email"
                value={forgotEmail}
                onChange={(e) => setForgotEmail(e.target.value)}
                placeholder="seu@email.com"
                required
                autoFocus
              />
              <button type="submit" style={{ ...S.btn, marginTop: 22 }} disabled={loading}>
                {loading ? 'Enviando…' : 'Enviar link de redefinição'}
              </button>
              <button type="button" style={S.btnGhost} onClick={() => { setPhase('creds'); setErr('') }}>
                Voltar para o login
              </button>
            </form>
          )}

          {phase === 'forgot-sent' && (
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 48, marginBottom: 16 }}>📧</div>
              <h1 style={{ ...S.h1, textAlign: 'center' }}>Verifique seu e-mail</h1>
              <p style={{ ...S.sub, textAlign: 'center' }}>
                Se o endereço <b>{forgotEmail}</b> estiver cadastrado, você receberá um link em instantes.
              </p>
              <p style={{ fontSize: 13, color: '#9CA3AF', marginTop: 8 }}>O link expira em 30 minutos.</p>
              <button type="button" style={{ ...S.btnGhost, marginTop: 24 }} onClick={() => { setPhase('creds'); setErr('') }}>
                Voltar para o login
              </button>
            </div>
          )}

          {/* Canal do Encarregado de Dados (DPO) — LGPD Art. 41. */}
          <DpoFooter variant="login" />
        </div>
      </main>
    </div>
  )
}

const GRAPH = '#1F2937'
const GRAPH_DARK = '#0F172A'
const BLUE = '#1E6FF2'

const S: Record<string, React.CSSProperties> = {
  shell: { display: 'flex', minHeight: '100vh', fontFamily: "'Space Grotesk', system-ui, sans-serif", background: '#fff' },

  // painel grafite
  brand: { position: 'relative', flex: '1 1 46%', overflow: 'hidden', background: `linear-gradient(165deg, ${GRAPH} 0%, ${GRAPH_DARK} 70%, #0A0F1A 100%)`, display: 'flex' },
  grid: { position: 'absolute', inset: 0, backgroundImage: 'linear-gradient(rgba(255,255,255,.04) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,.04) 1px, transparent 1px)', backgroundSize: '44px 44px', maskImage: 'radial-gradient(120% 100% at 30% 20%, #000 40%, transparent 100%)' },
  glow: { position: 'absolute', width: 480, height: 480, left: -140, bottom: -160, borderRadius: '50%', background: `radial-gradient(circle, ${BLUE}40 0%, transparent 65%)`, filter: 'blur(10px)' },
  brandInner: { position: 'relative', display: 'flex', flexDirection: 'column', padding: '48px 52px', width: '100%', maxWidth: 540 },
  bH2: { fontSize: 42, fontWeight: 800, color: '#fff', lineHeight: 1.05, letterSpacing: '-1px', margin: '0 0 14px' },
  bP: { fontSize: 15.5, lineHeight: 1.55, color: 'rgba(255,255,255,.62)', margin: '0 0 28px', maxWidth: 420 },
  // destaque "Frustração 0" (diferencial nº 1) — largura total acima da grade
  hero: { borderRadius: 16, border: `1px solid ${BLUE}`, background: `${BLUE}1A`, padding: '16px 18px', margin: '0 0 16px' },
  heroBadge: { display: 'inline-flex', alignItems: 'center', gap: 7, padding: '5px 11px', borderRadius: 999, background: BLUE, color: '#fff', fontWeight: 800, fontSize: 12.5, letterSpacing: '.2px' },
  heroTitle: { marginTop: 10, fontSize: 16, fontWeight: 800, color: '#fff', lineHeight: 1.25 },
  heroSub: { marginTop: 4, fontSize: 12.5, color: 'rgba(255,255,255,.6)', lineHeight: 1.45 },

  feats: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 },
  feat: { display: 'flex', gap: 11, alignItems: 'flex-start', background: 'rgba(255,255,255,.04)', border: '1px solid rgba(255,255,255,.08)', borderRadius: 14, padding: '14px' },
  featIc: { fontSize: 20, lineHeight: 1, flex: 'none' },
  featT: { fontSize: 13.5, fontWeight: 700, color: '#fff' },
  featD: { fontSize: 12, color: 'rgba(255,255,255,.5)', marginTop: 3, lineHeight: 1.4 },

  // mini-feed de prova social (rodapé do painel grafite)
  feed: { display: 'flex', alignItems: 'center', gap: 9, marginTop: 28, padding: '10px 12px', borderRadius: 12, background: 'rgba(255,255,255,.04)', border: '1px solid rgba(255,255,255,.08)' },
  feedDot: { flex: 'none', width: 8, height: 8, borderRadius: '50%', background: '#3DDC84', boxShadow: '0 0 0 3px rgba(61,220,132,.18)' },
  feedTxt: { fontSize: 12.5, color: 'rgba(255,255,255,.62)', lineHeight: 1.35 },

  bFoot: { marginTop: 14, fontSize: 11.5, color: 'rgba(255,255,255,.38)' },

  // formulário branco
  formSide: { flex: '1 1 54%', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '40px 24px' },
  formWrap: { width: '100%', maxWidth: 388 },
  mobLogo: { marginBottom: 34 },
  h1: { fontSize: 27, fontWeight: 800, color: '#111827', margin: '0 0 6px', letterSpacing: '-0.5px' },
  sub: { fontSize: 14, color: '#6B7280', margin: '0 0 26px' },
  label: { display: 'block', fontSize: 12.5, fontWeight: 600, color: '#374151', margin: '14px 0 6px' },
  input: { width: '100%', boxSizing: 'border-box', height: 47, padding: '0 14px', borderRadius: 11, border: '1.5px solid #E5E7EB', background: '#F9FAFB', fontSize: 15, color: '#111827', outline: 'none' },
  eye: { position: 'absolute', right: 8, top: 7, width: 32, height: 32, border: 'none', background: 'transparent', cursor: 'pointer', fontSize: 16 },
  link: { color: BLUE, fontSize: 13, fontWeight: 600, textDecoration: 'none' },
  btn: { width: '100%', height: 49, border: 'none', borderRadius: 11, background: BLUE, color: '#fff', fontSize: 15.5, fontWeight: 700, cursor: 'pointer', boxShadow: '0 10px 24px rgba(30,111,242,.26)' },
  btnGhost: { width: '100%', height: 44, marginTop: 10, border: '1.5px solid #E5E7EB', borderRadius: 11, background: 'transparent', color: '#6B7280', fontSize: 14, fontWeight: 600, cursor: 'pointer' },
  err: { background: '#FEF2F2', color: '#B91C1C', border: '1px solid #FECACA', borderRadius: 10, padding: '10px 12px', fontSize: 13, marginBottom: 16 },
}
