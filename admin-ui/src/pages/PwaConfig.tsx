import { useEffect, useState } from 'react'
import { api } from '../api'

type PwaConfig = {
  app_name: string
  short_name: string
  start_url: string
  display: string
  theme_color: string
  background_color: string
  icon_192: string
  icon_512: string
  sw_cache_version: string
  app_url: string
  sw_url: string
  manifest_url: string
}

type ManifestResult = {
  ok: boolean
  manifest: any
  error: string | null
}

const DISPLAY_OPTIONS = [
  { value: 'standalone', label: 'standalone (padrão — sem barra do browser)' },
  { value: 'browser',    label: 'browser (abre no browser normal)' },
  { value: 'minimal-ui', label: 'minimal-ui (barra mínima do browser)' },
  { value: 'fullscreen', label: 'fullscreen (tela cheia)' },
]

const DEFAULTS: PwaConfig = {
  app_name:         'Senderzz',
  short_name:       'SZ',
  start_url:        '/app/',
  display:          'standalone',
  theme_color:      '#f3f8fb',
  background_color: '#f3f8fb',
  icon_192:         '/wp-content/plugins/senderzz-logistics/assets/pwa-icon-192.png',
  icon_512:         '/wp-content/plugins/senderzz-logistics/assets/pwa-icon-512.png',
  sw_cache_version: 'app250',
  app_url:          '/app/',
  sw_url:           '/app-sw.js',
  manifest_url:     '/app-manifest.json',
}

export default function PwaConfig() {
  const [cfg, setCfg] = useState<PwaConfig>(DEFAULTS)
  const [form, setForm] = useState<PwaConfig>(DEFAULTS)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [err, setErr] = useState('')

  // Estado de verificação de manifest.
  const [manifBusy, setManifBusy] = useState(false)
  const [manifResult, setManifResult] = useState<ManifestResult | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 6000)
  }

  function field(key: keyof PwaConfig) {
    return (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
      setForm(f => ({ ...f, [key]: e.target.value }))
    }
  }

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const c = await api<PwaConfig>('/pwa-config')
      setCfg(c)
      setForm(c)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar configuração PWA')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; config: PwaConfig }>('/pwa-config', {
        method: 'POST',
        body: JSON.stringify({
          app_name:         form.app_name,
          short_name:       form.short_name,
          display:          form.display,
          theme_color:      form.theme_color,
          background_color: form.background_color,
          icon_192:         form.icon_192,
          icon_512:         form.icon_512,
          sw_cache_version: form.sw_cache_version,
        }),
      })
      setCfg(r.config)
      setForm(r.config)
      showToast('ok', 'Configuração do PWA salva com sucesso.')
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar configuração')
    } finally {
      setBusy(false)
    }
  }

  async function handleTestManifest() {
    setManifBusy(true)
    setManifResult(null)
    try {
      const r = await api<ManifestResult>('/pwa-config/test-manifest')
      setManifResult(r)
    } catch (e: any) {
      setManifResult({ ok: false, manifest: null, error: e.message || 'Erro ao verificar manifest' })
    } finally {
      setManifBusy(false)
    }
  }

  if (loading) {
    return (
      <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
        Carregando…
      </div>
    )
  }

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

      {/* Card: URLs do PWA */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>URLs do PWA</h2>
            <p className="szv2-card-sub">
              Endereços gerados automaticamente — não editáveis por aqui.
            </p>
          </div>
          <button
            type="button"
            className="szv2-btn-secondary"
            onClick={handleTestManifest}
            disabled={manifBusy}
          >
            {manifBusy ? 'Verificando…' : 'Verificar manifest'}
          </button>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12, marginTop: 12 }}>
          {[
            { label: 'App URL', value: cfg.app_url },
            { label: 'Service Worker', value: cfg.sw_url },
            { label: 'Manifest', value: cfg.manifest_url },
          ].map(({ label, value }) => (
            <div key={label} style={{
              padding: '10px 12px',
              background: 'var(--szv2-bg, #f9fafb)',
              borderRadius: 6,
              border: '1px solid var(--szv2-border, #e5e7eb)',
              fontSize: 12,
            }}>
              <div style={{ color: 'var(--szv2-text-muted)', marginBottom: 4 }}>{label}</div>
              <code style={{ fontSize: 12, wordBreak: 'break-all' }}>{value}</code>
            </div>
          ))}
        </div>

        {/* Resultado da verificação do manifest */}
        {manifResult && (
          <div style={{ marginTop: 16 }}>
            {manifResult.ok ? (
              <>
                <div className="sz-alert-success" style={{ marginBottom: 8 }}>
                  Manifest encontrado e válido.
                </div>
                <pre style={{
                  background: 'var(--szv2-bg, #f9fafb)',
                  border: '1px solid var(--szv2-border, #e5e7eb)',
                  borderRadius: 6,
                  padding: 12,
                  fontSize: 11,
                  overflowX: 'auto',
                  maxHeight: 300,
                }}>
                  {JSON.stringify(manifResult.manifest, null, 2)}
                </pre>
              </>
            ) : (
              <div className="sz-alert-danger">
                Erro ao verificar manifest: {manifResult.error}
              </div>
            )}
          </div>
        )}
      </div>

      <form onSubmit={handleSave}>
        {/* Card: Configurações */}
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Configurações do PWA</h2>
              <p className="szv2-card-sub">
                Manifest, ícones e aparência do app instalável.
              </p>
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginTop: 16 }}>
            {/* App name */}
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Nome do app
              <input
                type="text"
                className="szv2-input"
                value={form.app_name}
                onChange={field('app_name')}
                disabled={busy}
                placeholder="Senderzz"
              />
            </label>

            {/* Short name */}
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Nome curto <span style={{ color: 'var(--szv2-text-muted)', fontWeight: 400 }}>(máx. 12 chars)</span>
              <input
                type="text"
                className="szv2-input"
                value={form.short_name}
                onChange={field('short_name')}
                maxLength={12}
                disabled={busy}
                placeholder="SZ"
              />
            </label>

            {/* Display mode */}
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Modo de exibição
              <select
                className="szv2-select"
                value={form.display}
                onChange={field('display')}
                disabled={busy}
              >
                {DISPLAY_OPTIONS.map(o => (
                  <option key={o.value} value={o.value}>{o.label}</option>
                ))}
              </select>
            </label>

            {/* SW cache version */}
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Versão do cache (SW)
              <input
                type="text"
                className="szv2-input"
                value={form.sw_cache_version}
                onChange={field('sw_cache_version')}
                disabled={busy}
                placeholder="app250"
              />
            </label>

            {/* Theme color */}
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Cor do tema
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input
                  type="color"
                  value={form.theme_color}
                  onChange={field('theme_color')}
                  disabled={busy}
                  style={{ width: 40, height: 36, padding: 2, borderRadius: 4, border: '1px solid var(--szv2-border, #e5e7eb)', cursor: 'pointer' }}
                />
                <input
                  type="text"
                  className="szv2-input"
                  value={form.theme_color}
                  onChange={field('theme_color')}
                  disabled={busy}
                  placeholder="#f3f8fb"
                  style={{ flex: 1 }}
                />
              </div>
            </label>

            {/* Background color */}
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Cor de fundo
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input
                  type="color"
                  value={form.background_color}
                  onChange={field('background_color')}
                  disabled={busy}
                  style={{ width: 40, height: 36, padding: 2, borderRadius: 4, border: '1px solid var(--szv2-border, #e5e7eb)', cursor: 'pointer' }}
                />
                <input
                  type="text"
                  className="szv2-input"
                  value={form.background_color}
                  onChange={field('background_color')}
                  disabled={busy}
                  placeholder="#f3f8fb"
                  style={{ flex: 1 }}
                />
              </div>
            </label>

            {/* Icon 192 */}
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13, gridColumn: '1 / -1' }}>
              URL do ícone 192×192
              <input
                type="url"
                className="szv2-input"
                value={form.icon_192}
                onChange={field('icon_192')}
                disabled={busy}
                placeholder="/wp-content/plugins/senderzz-logistics/assets/pwa-icon-192.png"
              />
            </label>

            {/* Icon 512 */}
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13, gridColumn: '1 / -1' }}>
              URL do ícone 512×512
              <input
                type="url"
                className="szv2-input"
                value={form.icon_512}
                onChange={field('icon_512')}
                disabled={busy}
                placeholder="/wp-content/plugins/senderzz-logistics/assets/pwa-icon-512.png"
              />
            </label>
          </div>
        </div>

        {/* Card: Live preview */}
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Preview de instalação</h2>
              <p className="szv2-card-sub">
                Simulação do prompt "Adicionar à tela inicial" com os valores atuais do formulário.
              </p>
            </div>
          </div>

          <div style={{
            marginTop: 16,
            display: 'inline-flex',
            alignItems: 'center',
            gap: 14,
            padding: '14px 18px',
            borderRadius: 12,
            border: '1px solid var(--szv2-border, #e5e7eb)',
            background: form.background_color || '#f3f8fb',
            boxShadow: '0 2px 12px rgba(0,0,0,.08)',
            maxWidth: 360,
          }}>
            {/* Ícone */}
            <div style={{
              width: 56,
              height: 56,
              borderRadius: 12,
              overflow: 'hidden',
              flexShrink: 0,
              background: form.theme_color || '#f3f8fb',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              border: '1px solid rgba(0,0,0,.06)',
            }}>
              {form.icon_192 ? (
                <img
                  src={form.icon_192}
                  alt="ícone"
                  style={{ width: '100%', height: '100%', objectFit: 'cover' }}
                  onError={e => { (e.target as HTMLImageElement).style.display = 'none' }}
                />
              ) : (
                <span style={{ fontSize: 22 }}>📦</span>
              )}
            </div>

            {/* Texto */}
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontWeight: 700, fontSize: 15, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {form.app_name || 'Senderzz'}
              </div>
              <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 2 }}>
                {form.short_name || 'SZ'}
              </div>
            </div>

            {/* Badge instalar */}
            <div style={{
              background: form.theme_color || '#f3f8fb',
              border: `1.5px solid ${form.theme_color || 'var(--szv2-brand)'}`,
              borderRadius: 20,
              padding: '5px 14px',
              fontSize: 12,
              fontWeight: 600,
              color: 'var(--szv2-text, #1f2937)',
              whiteSpace: 'nowrap',
              flexShrink: 0,
            }}>
              Instalar
            </div>
          </div>
        </div>

        {/* Footer — botão salvar */}
        <div style={{ marginTop: 24, display: 'flex', justifyContent: 'flex-end' }}>
          <button
            type="submit"
            className="szv2-btn-brand"
            disabled={busy}
          >
            {busy ? 'Salvando…' : 'Salvar configuração'}
          </button>
        </div>
      </form>
    </div>
  )
}
