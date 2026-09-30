import { useEffect, useState } from 'react'
import { api } from '../api'
import FalkSelect from '../components/FalkSelect'
import { emitToast } from '../hooks/useToast'

type Settings = {
  me_token: string
  pix_key: string
  pix_key_type: string
  webhook_secret_hint: string
  jwt_secret_hint: string
  motoboy_cc_fee_pct: number
  portal_name: string
  pack_barcode_validation_enabled: boolean
  // FEAT-RBAC-2026-06-21 — recompensa de convite (options sz_invite_reward_*).
  // BACKEND GAP: o handler Go (go/admin/internal/handlers/settings.go) ainda NÃO
  // lê nem grava estas chaves — Get não as devolve e Save ignora campos
  // desconhecidos (PUT retorna 200 sem persistir). Adicionar `sz_invite_reward_type`
  // e `sz_invite_reward_value` ao allowlist de Save e ao Get para o round-trip funcionar.
  invite_reward_type: 'none' | 'fixed' | 'percent'
  invite_reward_value: number
}

// FEAT-RBAC-2026-06-21 — tipos de recompensa de convite.
const INVITE_REWARD_TYPES: { value: Settings['invite_reward_type']; label: string }[] = [
  { value: 'none',    label: 'Desativada' },
  { value: 'fixed',   label: 'Valor fixo (R$)' },
  { value: 'percent', label: 'Percentual (%)' },
]

export default function Settings() {
  const [s, setS] = useState<Partial<Settings>>({})
  const [msg, setMsg] = useState('')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api<Settings>('/settings')
      .then(setS)
      .catch(e => setErr(e.message))
      .finally(() => setLoading(false))
  }, [])

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true); setMsg(''); setErr('')
    try {
      await api('/settings', { method: 'PUT', body: JSON.stringify(s) })
      setMsg('Configurações salvas.')
      emitToast('ok', 'Configurações salvas.')
    } catch (e: any) { setErr(e.message); emitToast('err', e.message || 'Falha ao salvar configurações.') }
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

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando configurações…
        </div>
      ) : (
      <form onSubmit={save}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '20px' }}>
          <div className="szv2-card">
            <div className="szv2-card-head"><div><h2>Melhor Envio</h2></div></div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              {f('me_token', 'Token ME (Bearer)', 'password', 'Bearer ...')}
              {f('portal_name', 'Nome do Portal', 'text', 'FALK LOG')}
            </div>
          </div>

          <div className="szv2-card">
            <div className="szv2-card-head"><div><h2>PIX</h2></div></div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              {f('pix_key', 'Chave PIX', 'text', 'CPF, CNPJ, email, telefone ou aleatória')}
              <div className="szv2-field">
                <label className="szv2-label">Tipo de chave</label>
                <FalkSelect
                  aria-label="Tipo de chave"
                  value={s.pix_key_type ?? 'cpf'}
                  onChange={v => setS(p => ({ ...p, pix_key_type: v }))}
                  options={[
                    { value: 'cpf', label: 'CPF' },
                    { value: 'cnpj', label: 'CNPJ' },
                    { value: 'email', label: 'Email' },
                    { value: 'phone', label: 'Telefone' },
                    { value: 'random', label: 'Aleatória' },
                  ]}
                />
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
              <label style={{ display: 'flex', flexDirection: 'column', gap: 6, cursor: 'pointer', color: 'var(--szv2-text-soft)' }}>
                <span className="szv2-label" style={{ marginBottom: 0 }}>Validar código de barras ao embalar</span>
                <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  Desative para permitir embalar sem leitura do produto. Reative quando quiser voltar a exigir a leitura do código de barras.
                </span>
                <input
                  type="checkbox"
                  checked={!!s.pack_barcode_validation_enabled}
                  onChange={e => setS(p => ({ ...p, pack_barcode_validation_enabled: e.target.checked }))}
                  style={{ width: 18, height: 18 }}
                />
              </label>
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
      )}
    </div>
  )
}
