import { useEffect, useState } from 'react'
import { api } from '../api'

type MotoboyConfig = {
  geofence_metros: number
  horario_inicio: string
  horario_fim: string
  cc_fee_pct: number
}

type Errors = Partial<Record<keyof MotoboyConfig, string>>

const DEFAULTS: MotoboyConfig = {
  geofence_metros: 500,
  horario_inicio: '08:00',
  horario_fim: '18:00',
  cc_fee_pct: 0,
}

const RE_HORA = /^([01]\d|2[0-3]):[0-5]\d$/

function validate(c: MotoboyConfig): Errors {
  const errs: Errors = {}
  if (!Number.isFinite(c.geofence_metros) || c.geofence_metros <= 0) {
    errs.geofence_metros = 'Geofence deve ser maior que zero'
  } else if (c.geofence_metros < 50 || c.geofence_metros > 5000) {
    errs.geofence_metros = 'Geofence deve estar entre 50 e 5000 metros'
  }
  if (!RE_HORA.test(c.horario_inicio)) {
    errs.horario_inicio = 'Formato inválido (HH:MM)'
  }
  if (!RE_HORA.test(c.horario_fim)) {
    errs.horario_fim = 'Formato inválido (HH:MM)'
  }
  if (!Number.isFinite(c.cc_fee_pct) || c.cc_fee_pct < 0 || c.cc_fee_pct > 30) {
    errs.cc_fee_pct = 'Taxa deve estar entre 0 e 30'
  }
  return errs
}

export default function MotoboyConfig() {
  const [cfg, setCfg] = useState<MotoboyConfig>(DEFAULTS)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [errs, setErrs] = useState<Errors>({})
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const data = await api<MotoboyConfig>('/motoboy-config')
      setCfg({
        geofence_metros: Number(data.geofence_metros) || DEFAULTS.geofence_metros,
        horario_inicio: data.horario_inicio || DEFAULTS.horario_inicio,
        horario_fim: data.horario_fim || DEFAULTS.horario_fim,
        cc_fee_pct: Number(data.cc_fee_pct) || 0,
      })
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar configurações')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  function update<K extends keyof MotoboyConfig>(key: K, value: MotoboyConfig[K]) {
    setCfg(prev => ({ ...prev, [key]: value }))
    setErrs(prev => {
      if (!prev[key]) return prev
      const { [key]: _omit, ...rest } = prev
      return rest
    })
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    const validation = validate(cfg)
    setErrs(validation)
    if (Object.keys(validation).length > 0) {
      showToast('err', 'Corrija os campos destacados antes de salvar')
      return
    }
    setSaving(true)
    try {
      await api<{ ok: boolean; config: MotoboyConfig }>('/motoboy-config', {
        method: 'POST',
        body: JSON.stringify(cfg),
      })
      showToast('ok', 'Configurações salvas com sucesso')
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Configurações operacionais Motoboy</h1>
          <p>Geofence, expediente e taxa de cartão usados pelo módulo de entregas</p>
        </div>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      <form onSubmit={handleSave}>
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div>
              <h2>Parâmetros gerais</h2>
              <p className="szv2-card-sub">
                Valores aplicados ao módulo Motoboy em todo o sistema.
              </p>
            </div>
          </div>

          {loading ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Carregando…
            </div>
          ) : (
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
              {/* Geofence (metros) */}
              <div className="szv2-field">
                <label className="szv2-label">Geofence (metros)</label>
                <input
                  className="szv2-input"
                  type="number"
                  min={50}
                  max={5000}
                  step={50}
                  value={Number.isFinite(cfg.geofence_metros) ? cfg.geofence_metros : ''}
                  onChange={e => update('geofence_metros', Math.round(Number(e.target.value)))}
                  disabled={saving}
                  autoComplete="off"
                />
                <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                  Raio em metros para validar entrada/saída de zona via GPS
                </small>
                {errs.geofence_metros && (
                  <small style={{ color: 'var(--szv2-danger)', fontSize: 12 }}>
                    {errs.geofence_metros}
                  </small>
                )}
              </div>

              {/* Taxa cartão (%) */}
              <div className="szv2-field">
                <label className="szv2-label">Taxa cartão (%)</label>
                <input
                  className="szv2-input"
                  type="number"
                  min={0}
                  max={30}
                  step={0.1}
                  value={Number.isFinite(cfg.cc_fee_pct) ? cfg.cc_fee_pct : ''}
                  onChange={e => update('cc_fee_pct', Number(e.target.value))}
                  disabled={saving}
                  autoComplete="off"
                />
                <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                  Deixe 0 para não exibir segundo valor. Valor cobrado no cartão = valor pedido × (1 + taxa/100)
                </small>
                {errs.cc_fee_pct && (
                  <small style={{ color: 'var(--szv2-danger)', fontSize: 12 }}>
                    {errs.cc_fee_pct}
                  </small>
                )}
              </div>

              {/* Horário de início */}
              <div className="szv2-field">
                <label className="szv2-label">Horário de início</label>
                <input
                  className="szv2-input"
                  type="time"
                  value={cfg.horario_inicio}
                  onChange={e => update('horario_inicio', e.target.value)}
                  disabled={saving}
                  autoComplete="off"
                />
                <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                  Início do expediente operacional
                </small>
                {errs.horario_inicio && (
                  <small style={{ color: 'var(--szv2-danger)', fontSize: 12 }}>
                    {errs.horario_inicio}
                  </small>
                )}
              </div>

              {/* Horário de fim */}
              <div className="szv2-field">
                <label className="szv2-label">Horário de fim</label>
                <input
                  className="szv2-input"
                  type="time"
                  value={cfg.horario_fim}
                  onChange={e => update('horario_fim', e.target.value)}
                  disabled={saving}
                  autoComplete="off"
                />
                <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                  Fim do expediente operacional
                </small>
                {errs.horario_fim && (
                  <small style={{ color: 'var(--szv2-danger)', fontSize: 12 }}>
                    {errs.horario_fim}
                  </small>
                )}
              </div>
            </div>
          )}
        </div>

        <div style={{ marginTop: 20, display: 'flex', gap: 12, alignItems: 'center' }}>
          <button
            type="submit"
            className="szv2-btn szv2-btn-brand"
            disabled={saving || loading}
            style={{ minWidth: 200 }}
          >
            {saving ? 'Salvando…' : 'Salvar configurações'}
          </button>
        </div>
      </form>
    </div>
  )
}
