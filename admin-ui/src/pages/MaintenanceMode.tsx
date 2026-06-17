// MaintenanceMode — tela de configuração do modo manutenção do Senderzz.
// Paridade com includes/senderzz-maintenance.php. Visual: layout em duas colunas
// (formulário à esquerda, preview ao vivo à direita) seguindo o padrão AuditEngine.
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'

type Settings = {
  enabled: boolean
  return_date: string // YYYY-MM-DD
  return_time: string // HH:MM
  title: string
  message: string
}

// Defaults espelham senderzz_maintenance_defaults() do PHP.
const DEFAULTS: Settings = {
  enabled: false,
  return_date: '',
  return_time: '',
  title: 'Estamos ajustando a operação',
  message:
    'A plataforma Senderzz está temporariamente em manutenção para melhorias operacionais. Voltaremos em breve.',
}

const TITLE_MAX = 90

// formatReturnLabel — espelha senderzz_maintenance_return_label() (PHP).
function formatReturnLabel(date: string, time: string): string {
  const d = (date || '').trim()
  const t = (time || '').trim()
  if (!d && !t) return 'Retorno previsto em breve'

  let label = ''
  if (d) {
    // Aceita YYYY-MM-DD; converte para dd/mm/yyyy. Em caso de string inválida usa original.
    const m = d.match(/^(\d{4})-(\d{2})-(\d{2})$/)
    label += m ? `${m[3]}/${m[2]}/${m[1]}` : d
  }
  if (t) {
    label += (label ? ' às ' : '') + t
  }
  return `Retorno previsto: ${label}`
}

export default function MaintenanceMode() {
  const [settings, setSettings] = useState<Settings>(DEFAULTS)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const s = await api<Settings>('/maintenance')
      // Garante shape mesmo se backend devolver campos faltando.
      setSettings({
        enabled: !!s?.enabled,
        return_date: s?.return_date ?? '',
        return_time: s?.return_time ?? '',
        title: s?.title ?? DEFAULTS.title,
        message: s?.message ?? DEFAULTS.message,
      })
    } catch (e: any) {
      setErr(e?.message || 'Erro ao carregar configurações')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  // Preview live: usa título/mensagem digitados, com fallback para defaults se vazios.
  const previewTitle   = settings.title.trim()   || DEFAULTS.title
  const previewMessage = settings.message.trim() || DEFAULTS.message
  const returnLabel    = useMemo(
    () => formatReturnLabel(settings.return_date, settings.return_time),
    [settings.return_date, settings.return_time]
  )

  function update<K extends keyof Settings>(key: K, value: Settings[K]) {
    setSettings(prev => ({ ...prev, [key]: value }))
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    if (settings.title.length > TITLE_MAX) {
      showToast('err', `Título excede ${TITLE_MAX} caracteres.`)
      return
    }
    setSaving(true)
    try {
      await api<{ ok: boolean; settings: Settings }>('/maintenance', {
        method: 'POST',
        body: JSON.stringify(settings),
      })
      showToast('ok', 'Modo manutenção atualizado.')
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao salvar')
    } finally {
      setSaving(false)
    }
  }

  const titleRemaining = TITLE_MAX - settings.title.length
  const counterColor = titleRemaining < 0
    ? 'var(--szv2-danger)'
    : titleRemaining < 10
      ? 'var(--szv2-brand)'
      : 'var(--szv2-text-muted)'

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {loading ? (
        <div className="szv2-card">
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        </div>
      ) : (
        <form onSubmit={handleSave}>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)',
              gap: 24,
              alignItems: 'start',
            }}
          >
            {/* ---------- Coluna esquerda: formulário ---------- */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
              {/* Toggle Card */}
              <div className="szv2-card">
                <div className="szv2-card-head">
                  <div>
                    <h2>Modo manutenção</h2>
                    <p className="szv2-card-sub">
                      Quando ativado, clientes, produtores, afiliados e motoboys veem a tela de manutenção.
                    </p>
                  </div>
                </div>

                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 16,
                    padding: '16px 0 4px',
                  }}
                >
                  <label
                    htmlFor="sz-maint-enabled"
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 12,
                      cursor: 'pointer',
                      fontWeight: 600,
                    }}
                  >
                    {/* Switch custom */}
                    <span
                      style={{
                        position: 'relative',
                        display: 'inline-block',
                        width: 52,
                        height: 28,
                        borderRadius: 999,
                        background: settings.enabled ? '#EA580C' : '#cbd5e1',
                        transition: 'background 0.2s',
                        flexShrink: 0,
                      }}
                    >
                      <span
                        style={{
                          position: 'absolute',
                          top: 3,
                          left: settings.enabled ? 27 : 3,
                          width: 22,
                          height: 22,
                          borderRadius: '50%',
                          background: '#fff',
                          transition: 'left 0.2s',
                          boxShadow: '0 2px 4px rgba(0,0,0,.2)',
                        }}
                      />
                    </span>
                    <input
                      id="sz-maint-enabled"
                      type="checkbox"
                      checked={settings.enabled}
                      onChange={e => update('enabled', e.target.checked)}
                      style={{ position: 'absolute', opacity: 0, width: 0, height: 0 }}
                    />
                    <span>Modo manutenção</span>
                  </label>

                  <span
                    style={{
                      fontWeight: 700,
                      fontSize: 13,
                      padding: '4px 12px',
                      borderRadius: 999,
                      background: settings.enabled
                        ? 'rgba(34,197,94,0.12)'
                        : 'rgba(148,163,184,0.18)',
                      color: settings.enabled ? '#16a34a' : '#64748b',
                    }}
                  >
                    {settings.enabled ? 'Ativado' : 'Desativado'}
                  </span>
                </div>
              </div>

              {/* Form Card */}
              <div className="szv2-card">
                <div className="szv2-card-head">
                  <div>
                    <h2>Configurações</h2>
                    <p className="szv2-card-sub">
                      Personalize o título, mensagem e horário previsto de retorno.
                    </p>
                  </div>
                </div>

                <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
                  {/* Título */}
                  <div>
                    <label
                      htmlFor="sz-maint-title"
                      style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}
                    >
                      Título da tela
                    </label>
                    <input
                      id="sz-maint-title"
                      type="text"
                      className="szv2-input"
                      value={settings.title}
                      maxLength={TITLE_MAX}
                      onChange={e => update('title', e.target.value)}
                      placeholder={DEFAULTS.title}
                      style={{ width: '100%' }}
                    />
                    <div
                      style={{
                        marginTop: 4,
                        fontSize: 12,
                        color: counterColor,
                        textAlign: 'right',
                      }}
                    >
                      {settings.title.length} / {TITLE_MAX}
                    </div>
                  </div>

                  {/* Mensagem */}
                  <div>
                    <label
                      htmlFor="sz-maint-message"
                      style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}
                    >
                      Mensagem
                    </label>
                    <textarea
                      id="sz-maint-message"
                      className="szv2-input"
                      rows={5}
                      value={settings.message}
                      onChange={e => update('message', e.target.value)}
                      placeholder={DEFAULTS.message}
                      style={{ width: '100%', resize: 'vertical', fontFamily: 'inherit' }}
                    />
                  </div>

                  {/* Data + Hora lado a lado */}
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                    <div>
                      <label
                        htmlFor="sz-maint-date"
                        style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}
                      >
                        Data prevista de retorno
                      </label>
                      <input
                        id="sz-maint-date"
                        type="date"
                        className="szv2-input"
                        value={settings.return_date}
                        onChange={e => update('return_date', e.target.value)}
                        style={{ width: '100%' }}
                      />
                    </div>
                    <div>
                      <label
                        htmlFor="sz-maint-time"
                        style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}
                      >
                        Horário previsto
                      </label>
                      <input
                        id="sz-maint-time"
                        type="time"
                        className="szv2-input"
                        value={settings.return_time}
                        onChange={e => update('return_time', e.target.value)}
                        style={{ width: '100%' }}
                      />
                    </div>
                  </div>

                  {/* Help text */}
                  <p
                    style={{
                      fontSize: 13,
                      color: 'var(--szv2-text-muted)',
                      background: 'rgba(234,88,12,0.06)',
                      border: '1px solid rgba(234,88,12,0.18)',
                      borderRadius: 10,
                      padding: '10px 12px',
                      margin: 0,
                    }}
                  >
                    <strong style={{ color: '#EA580C' }}>Bypass:</strong> admins (manage_options) e
                    operadores logísticos continuam acessando normalmente.
                  </p>
                </div>
              </div>

              {/* Botão Salvar */}
              <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                <button
                  type="submit"
                  className="szv2-btn-brand"
                  disabled={saving || loading}
                >
                  {saving ? 'Salvando…' : 'Salvar modo manutenção'}
                </button>
              </div>
            </div>

            {/* ---------- Coluna direita: preview ao vivo ---------- */}
            <div className="szv2-card" style={{ position: 'sticky', top: 16 }}>
              <div className="szv2-card-head">
                <div>
                  <h2>Pré-visualização</h2>
                  <p className="szv2-card-sub">
                    Como os clientes verão a tela quando o modo manutenção estiver ativo.
                  </p>
                </div>
              </div>

              <div
                role="img"
                aria-label="Pré-visualização da tela de manutenção"
                style={{
                  position: 'relative',
                  background: 'linear-gradient(135deg, #0f172a, #1e293b)',
                  color: 'white',
                  padding: 60,
                  textAlign: 'center',
                  borderRadius: 16,
                  overflow: 'hidden',
                  boxShadow: '0 18px 40px rgba(0,0,0,.22)',
                }}
              >
                {/* Barra laranja topo */}
                <div
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    right: 0,
                    height: 5,
                    background: 'linear-gradient(90deg, #E8650A, #ff7a1a)',
                  }}
                />

                {/* Logo "S" mark */}
                <div
                  style={{
                    width: 56,
                    height: 56,
                    borderRadius: 16,
                    background: 'linear-gradient(135deg, #E8650A, #ff7a1a)',
                    display: 'grid',
                    placeItems: 'center',
                    fontWeight: 700,
                    color: '#fff',
                    fontSize: 22,
                    margin: '0 auto 18px',
                    boxShadow: '0 14px 34px rgba(232,101,10,.32)',
                  }}
                >
                  S
                </div>

                {/* Pill manutenção programada */}
                <div
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 8,
                    border: '1px solid rgba(232,101,10,.32)',
                    background: 'rgba(232,101,10,.12)',
                    color: '#fed7aa',
                    borderRadius: 999,
                    padding: '6px 12px',
                    fontSize: 12,
                    fontWeight: 700,
                    marginBottom: 18,
                  }}
                >
                  ● Manutenção programada
                </div>

                {/* Título */}
                <h1
                  style={{
                    fontSize: 28,
                    lineHeight: 1.15,
                    margin: '0 0 14px',
                    letterSpacing: '-.02em',
                    color: '#f8fafc',
                  }}
                >
                  {previewTitle}
                </h1>

                {/* Mensagem (respeita quebras de linha) */}
                <p
                  style={{
                    fontSize: 15,
                    lineHeight: 1.6,
                    color: '#cbd5e1',
                    margin: '0 auto 22px',
                    maxWidth: 480,
                    whiteSpace: 'pre-wrap',
                  }}
                >
                  {previewMessage}
                </p>

                {/* Bloco "Voltamos em" só se houver data ou hora preenchida */}
                {(settings.return_date || settings.return_time) && (
                  <div
                    style={{
                      display: 'inline-flex',
                      gap: 10,
                      alignItems: 'center',
                      background: 'rgba(255,255,255,.06)',
                      border: '1px solid rgba(255,255,255,.10)',
                      borderRadius: 16,
                      padding: '12px 18px',
                      color: '#fff',
                      fontWeight: 700,
                      fontSize: 14,
                    }}
                  >
                    <span
                      style={{
                        width: 32,
                        height: 32,
                        borderRadius: 10,
                        background: 'rgba(232,101,10,.18)',
                        display: 'grid',
                        placeItems: 'center',
                        color: '#fdba74',
                      }}
                    >
                      ⏱
                    </span>
                    <span>{returnLabel}</span>
                  </div>
                )}

                {/* Footer */}
                <div
                  style={{
                    marginTop: 28,
                    paddingTop: 18,
                    borderTop: '1px solid rgba(255,255,255,.08)',
                    fontSize: 12,
                    color: '#94a3b8',
                  }}
                >
                  Senderzz Logística · acesso liberado para admins e operação
                </div>
              </div>

              {/* Aviso quando desativado */}
              {!settings.enabled && (
                <p
                  style={{
                    marginTop: 14,
                    fontSize: 12,
                    color: 'var(--szv2-text-muted)',
                    textAlign: 'center',
                  }}
                >
                  Esta tela só será exibida quando o modo manutenção estiver ativado.
                </p>
              )}
            </div>
          </div>
        </form>
      )}
    </div>
  )
}
