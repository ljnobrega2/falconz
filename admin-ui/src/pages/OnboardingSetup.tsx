import { FormEvent, useEffect, useState } from 'react'
import { api, setToken } from '../api'

type SetupStatus = {
  admin_users_count: number
  me_token_configured: boolean
  webhook_secret_configured: boolean
  jwt_secret_configured: boolean
  schemas_applied: string[]
  ready: boolean
  pending_steps: string[]
}

type CreateAdminResp = {
  ok: boolean
  id: number
  token: string
  admin?: { id: number; email: string; nome: string }
}

type Step = 'check' | 'create_admin' | 'done'

function CheckRow({ ok, label, sub }: { ok: boolean; label: string; sub?: string }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '12px 0',
        borderBottom: '1px solid var(--szv2-border, rgba(0,0,0,0.06))',
      }}
    >
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 28,
          height: 28,
          borderRadius: 999,
          background: ok ? 'rgba(34,197,94,.12)' : 'rgba(220,38,38,.12)',
          color: ok ? '#16a34a' : '#dc2626',
          fontWeight: 700,
          fontSize: 16,
          flexShrink: 0,
        }}
      >
        {ok ? '✓' : '✕'}
      </span>
      <div style={{ flex: 1 }}>
        <div style={{ fontWeight: 600, fontSize: 14 }}>{label}</div>
        {sub && (
          <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{sub}</div>
        )}
      </div>
      <span
        className={`sz-badge ${ok ? 'szv2-badge-success' : 'szv2-badge-danger'}`}
      >
        {ok ? 'OK' : 'Pendente'}
      </span>
    </div>
  )
}

export default function OnboardingSetup() {
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [step, setStep] = useState<Step>('check')
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Form state.
  const [nome, setNome] = useState('')
  const [email, setEmail] = useState('')
  const [senha, setSenha] = useState('')
  const [senha2, setSenha2] = useState('')
  const [saving, setSaving] = useState(false)
  const [formErr, setFormErr] = useState('')

  async function loadStatus() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<SetupStatus>('/onboarding/setup-status')
      setStatus(r)
    } catch (e: any) {
      setErr(e.message || 'Erro ao verificar status')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadStatus()
  }, [])

  async function submitCreate(e: FormEvent) {
    e.preventDefault()
    setFormErr('')
    if (senha !== senha2) {
      setFormErr('As senhas não coincidem.')
      return
    }
    if (senha.length < 8) {
      setFormErr('A senha deve ter ao menos 8 caracteres.')
      return
    }
    setSaving(true)
    try {
      const r = await api<CreateAdminResp>('/onboarding/setup/create-admin', {
        method: 'POST',
        body: JSON.stringify({ nome, email, senha }),
      })
      if (r.token) {
        setToken(r.token)
      }
      setStep('done')
    } catch (e: any) {
      setFormErr(e.message || 'Erro ao criar admin')
    } finally {
      setSaving(false)
    }
  }

  // Estado de carregamento inicial.
  if (loading) {
    return (
      <div className="sz-login-page">
        <div className="szv2-card" style={{ maxWidth: 560, width: '100%' }}>
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando status do sistema…
          </div>
        </div>
      </div>
    )
  }

  // Já configurado: avisa e redireciona para login.
  if (status && status.admin_users_count > 0 && step !== 'done') {
    return (
      <div className="sz-login-page">
        <div className="szv2-card" style={{ maxWidth: 560, width: '100%' }}>
          <div className="szv2-card-head">
            <div>
              <h2>Sistema já configurado</h2>
              <p className="szv2-card-sub">
                Um administrador já foi criado anteriormente.
              </p>
            </div>
          </div>
          <div style={{ padding: '16px 0' }}>
            <a className="szv2-btn szv2-btn-brand" href="/admin/login">
              Ir para o login
            </a>
          </div>
        </div>
      </div>
    )
  }

  // Sucesso após criar o admin.
  if (step === 'done') {
    return (
      <div className="sz-login-page">
        <div className="szv2-card" style={{ maxWidth: 560, width: '100%' }}>
          <div className="szv2-card-head">
            <div>
              <h2>Admin criado com sucesso</h2>
              <p className="szv2-card-sub">
                Você já pode acessar o painel administrativo.
              </p>
            </div>
          </div>
          <div style={{ padding: '24px 0', textAlign: 'center' }}>
            <div
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                width: 64,
                height: 64,
                borderRadius: 999,
                background: 'rgba(34,197,94,.12)',
                color: '#16a34a',
                fontSize: 32,
                fontWeight: 700,
                marginBottom: 16,
              }}
            >
              ✓
            </div>
            <p style={{ marginBottom: 24 }}>
              Admin <strong>{email}</strong> registrado como{' '}
              <code>super_admin</code>.
            </p>
            <a className="szv2-btn szv2-btn-brand" href="/admin/">
              Ir para o dashboard
            </a>
          </div>
        </div>
      </div>
    )
  }

  // Passo 2: formulário de criação do primeiro admin.
  if (step === 'create_admin') {
    return (
      <div className="sz-login-page">
        <form onSubmit={submitCreate} className="szv2-card" style={{ maxWidth: 560, width: '100%' }}>
          <div className="szv2-card-head">
            <div>
              <h2>Criar primeiro admin</h2>
              <p className="szv2-card-sub">
                Este será o usuário <code>super_admin</code> do painel.
              </p>
            </div>
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary"
              onClick={() => setStep('check')}
            >
              ← Voltar
            </button>
          </div>

          {formErr && (
            <div className="sz-alert-danger" style={{ marginBottom: 16 }}>
              {formErr}
            </div>
          )}

          <div className="sz-form-grid" style={{ marginBottom: 16 }}>
            <div className="szv2-field">
              <label className="szv2-label">Nome *</label>
              <input
                className="szv2-input"
                required
                value={nome}
                onChange={e => setNome(e.target.value)}
                autoComplete="name"
              />
            </div>
            <div className="szv2-field">
              <label className="szv2-label">E-mail *</label>
              <input
                className="szv2-input"
                type="email"
                required
                value={email}
                onChange={e => setEmail(e.target.value)}
                autoComplete="email"
              />
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Senha *</label>
              <input
                className="szv2-input"
                type="password"
                required
                minLength={8}
                value={senha}
                onChange={e => setSenha(e.target.value)}
                autoComplete="new-password"
              />
              <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 4 }}>
                Mínimo 8 caracteres.
              </div>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Confirmar senha *</label>
              <input
                className="szv2-input"
                type="password"
                required
                minLength={8}
                value={senha2}
                onChange={e => setSenha2(e.target.value)}
                autoComplete="new-password"
              />
            </div>
          </div>

          <div className="sz-form-actions">
            <button
              type="submit"
              className="szv2-btn szv2-btn-brand"
              disabled={saving}
            >
              {saving ? 'Criando…' : 'Criar admin'}
            </button>
          </div>
        </form>
      </div>
    )
  }

  // Passo 1: check inicial.
  const s = status as SetupStatus
  const schemasOK = s.schemas_applied.length > 0
  const allReady = s.ready

  return (
    <div className="sz-login-page">
      <div className="szv2-card" style={{ maxWidth: 640, width: '100%' }}>
        <div className="szv2-card-head">
          <div>
            <h2>Bem-vindo ao Senderzz Admin</h2>
            <p className="szv2-card-sub">
              Verificação do ambiente antes do primeiro acesso.
            </p>
          </div>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={loadStatus}
          >
            ↻ Recarregar
          </button>
        </div>

        {err && (
          <div className="sz-alert-danger" style={{ marginBottom: 16 }}>
            {err}
          </div>
        )}

        <div style={{ padding: '8px 0 16px' }}>
          <CheckRow
            ok={s.admin_users_count > 0}
            label="Administradores cadastrados"
            sub={`${s.admin_users_count} admin(s) ativo(s) em senderzz_admin_users`}
          />
          <CheckRow
            ok={s.me_token_configured}
            label="Token Melhor Envio"
            sub="env SENDERZZ_ME_TOKEN / TPC_ME_TOKEN"
          />
          <CheckRow
            ok={s.webhook_secret_configured}
            label="Secret de webhooks"
            sub="env MOTOBOY_INTERNAL_SECRET / TPC_WEBHOOK_SECRET"
          />
          <CheckRow
            ok={s.jwt_secret_configured}
            label="Secret JWT"
            sub="env ADMIN_JWT_SECRET / JWT_SECRET"
          />
          <CheckRow
            ok={schemasOK}
            label="Schemas do banco aplicados"
            sub={
              schemasOK
                ? `Subsistemas: ${s.schemas_applied.join(', ')}`
                : 'Nenhum schema aplicado'
            }
          />
        </div>

        {allReady ? (
          <div
            className="sz-alert-success"
            style={{
              marginBottom: 16,
              display: 'flex',
              alignItems: 'center',
              gap: 12,
            }}
          >
            <strong>Sistema pronto.</strong>
            <a className="szv2-btn szv2-btn-brand" href="/admin/" style={{ marginLeft: 'auto' }}>
              Ir para o dashboard
            </a>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            {s.pending_steps.length > 0 && (
              <div className="sz-alert-danger" style={{ fontSize: 13 }}>
                <strong>Etapas pendentes:</strong>{' '}
                {s.pending_steps.join(', ')}
              </div>
            )}

            {s.admin_users_count === 0 ? (
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                onClick={() => setStep('create_admin')}
              >
                Criar primeiro admin →
              </button>
            ) : (
              <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
                Configure as etapas pendentes via variáveis de ambiente / schema migrations
                e recarregue.
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
