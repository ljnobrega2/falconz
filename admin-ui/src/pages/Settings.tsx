import { useEffect, useState } from 'react'
import { api } from '../api'

type Settings = {
  me_token: string
  pix_key: string
  pix_key_type: string
  webhook_secret_hint: string
  jwt_secret_hint: string
  motoboy_cc_fee_pct: number
  portal_name: string
}

export default function Settings() {
  const [s, setS] = useState<Partial<Settings>>({})
  const [msg, setMsg] = useState('')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    api<Settings>('/settings').then(setS).catch(e => setErr(e.message))
  }, [])

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true); setMsg(''); setErr('')
    try {
      await api('/settings', { method: 'PUT', body: JSON.stringify(s) })
      setMsg('Configurações salvas.')
    } catch (e: any) { setErr(e.message) }
    finally { setSaving(false) }
  }

  const f = (key: keyof Settings, label: string, type = 'text', placeholder = '') => (
    <div className="szv2-field">
      <label className="szv2-label">{label}</label>
      <input
        className="szv2-input"
        type={type}
        placeholder={placeholder}
        value={(s[key] as string) ?? ''}
        onChange={e => setS(p => ({ ...p, [key]: type === 'number' ? +e.target.value : e.target.value }))}
        autoComplete="off"
      />
    </div>
  )

  return (
    <div>
      <div className="szv2-section-head">
        <div><h1>Configurações do Sistema</h1><p>Tokens, chaves e parâmetros globais</p></div>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      <form onSubmit={save}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '20px' }}>
          <div className="szv2-card">
            <div className="szv2-card-head"><div><h2>Melhor Envio</h2></div></div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              {f('me_token', 'Token ME (Bearer)', 'password', 'Bearer ...')}
              {f('portal_name', 'Nome do Portal', 'text', 'Senderzz')}
            </div>
          </div>

          <div className="szv2-card">
            <div className="szv2-card-head"><div><h2>PIX</h2></div></div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              {f('pix_key', 'Chave PIX', 'text', 'CPF, CNPJ, email, telefone ou aleatória')}
              <div className="szv2-field">
                <label className="szv2-label">Tipo de chave</label>
                <select className="szv2-select" value={s.pix_key_type ?? 'cpf'} onChange={e => setS(p => ({ ...p, pix_key_type: e.target.value }))}>
                  <option value="cpf">CPF</option>
                  <option value="cnpj">CNPJ</option>
                  <option value="email">Email</option>
                  <option value="phone">Telefone</option>
                  <option value="random">Aleatória</option>
                </select>
              </div>
            </div>
          </div>

          <div className="szv2-card">
            <div className="szv2-card-head"><div><h2>Motoboy</h2></div></div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              <div className="szv2-field">
                <label className="szv2-label">Taxa cartão motoboy (%)</label>
                <input className="szv2-input" type="number" step="0.1" min="0" max="30"
                  value={s.motoboy_cc_fee_pct ?? 0}
                  onChange={e => setS(p => ({ ...p, motoboy_cc_fee_pct: +e.target.value }))} />
              </div>
            </div>
          </div>

          <div className="szv2-card">
            <div className="szv2-card-head"><div><h2>Segurança</h2></div></div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              <div className="szv2-field">
                <label className="szv2-label">Webhook Secret (hint)</label>
                <input className="szv2-input" type="text" readOnly value={s.webhook_secret_hint ?? '****'} style={{ color: 'var(--szv2-text-muted)', cursor: 'default' }} />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">JWT Secret (hint)</label>
                <input className="szv2-input" type="text" readOnly value={s.jwt_secret_hint ?? '****'} style={{ color: 'var(--szv2-text-muted)', cursor: 'default' }} />
              </div>
            </div>
          </div>
        </div>

        <div style={{ marginTop: '20px', display: 'flex', gap: '12px', alignItems: 'center' }}>
          <button className="szv2-btn szv2-btn-brand" type="submit" disabled={saving} style={{ minWidth: '160px' }}>
            {saving ? 'Salvando…' : 'Salvar configurações'}
          </button>
          {msg && <span style={{ color: 'var(--szv2-success)', fontSize: '14px' }}>✓ {msg}</span>}
        </div>
      </form>
    </div>
  )
}
