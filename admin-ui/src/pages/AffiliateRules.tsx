import { useEffect, useState } from 'react'
import { api } from '../api'

// Tela "Regras de Afiliados" — defaults globais do programa de afiliados.
// Espelha AUDIT-ADMIN-WP.md §4.7 + §26.4 e bate com handlers/affiliate_rules.go.
// Stats no topo + 2 cards de configuração (Regras + Penalidades) + botão Salvar.

type AffiliateRules = {
  default_commission_pct: number
  default_retention_days: number
  default_withdraw_fee: number
  default_penalty_value: number
  first_frustration_penalty: number
  producer_frustration_penalty: number
  auto_approve: boolean
  min_withdraw_amount: number
  max_withdraw_per_month: number
}

type AffiliateRulesStats = {
  total_affiliates: number
  total_approved: number
  total_pending_approval: number
  avg_commission_pct: number
  total_balance_available: number
  total_balance_pending: number
}

// Defaults batem com o handler Go (fonte da verdade) — usados antes do GET.
const DEFAULT_RULES: AffiliateRules = {
  default_commission_pct: 10.0,
  default_retention_days: 7,
  default_withdraw_fee: 2.0,
  default_penalty_value: 5.0,
  first_frustration_penalty: 5.0,
  producer_frustration_penalty: 8.0,
  auto_approve: false,
  min_withdraw_amount: 50.0,
  max_withdraw_per_month: 0,
}

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

// KPI card simples (label + value + sub). Cor opcional via `tone`.
function KpiCard({
  label,
  value,
  sub,
  tone,
}: {
  label: string
  value: number | string
  sub?: string
  tone?: 'brand' | 'success' | 'warning' | 'muted'
}) {
  const color =
    tone === 'success' ? 'var(--szv2-success)'
    : tone === 'warning' ? 'var(--szv2-warning, #d97706)'
    : tone === 'muted'   ? 'var(--szv2-text-muted)'
                         : 'var(--szv2-brand)'
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span className="szv2-kpi-value" style={{ color }}>{value}</span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

// Field genérico com label + input + helper text.
function NumberField({
  label,
  help,
  value,
  onChange,
  step = '0.01',
  min = 0,
  max,
  suffix,
}: {
  label: string
  help?: string
  value: number
  onChange: (v: number) => void
  step?: string
  min?: number
  max?: number
  suffix?: string
}) {
  return (
    <div className="szv2-field">
      <label className="szv2-label">{label}</label>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <input
          className="szv2-input"
          type="number"
          step={step}
          min={min}
          max={max}
          value={isFinite(value) ? value : 0}
          onChange={e => {
            // Mantém vazio como 0 — campo numérico sempre serializa float.
            const n = parseFloat(e.target.value.replace(',', '.'))
            onChange(isFinite(n) ? n : 0)
          }}
          style={{ flex: 1 }}
        />
        {suffix && (
          <span style={{ color: 'var(--szv2-text-muted)', fontSize: 14, minWidth: 24 }}>
            {suffix}
          </span>
        )}
      </div>
      {help && (
        <span
          className="szv2-help"
          style={{ display: 'block', marginTop: 4, fontSize: 12, color: 'var(--szv2-text-muted)' }}
        >
          {help}
        </span>
      )}
    </div>
  )
}

export default function AffiliateRules() {
  const [rules, setRules] = useState<AffiliateRules>(DEFAULT_RULES)
  const [stats, setStats] = useState<AffiliateRulesStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  async function load() {
    setLoading(true)
    setErr('')
    try {
      // Carrega rules + stats em paralelo (Promise.all para corte de latência).
      const [r, s] = await Promise.all([
        api<AffiliateRules>('/affiliate-rules'),
        api<AffiliateRulesStats>('/affiliate-rules/stats').catch(() => null),
      ])
      // Mescla com defaults caso o backend devolva campos faltando.
      setRules({ ...DEFAULT_RULES, ...r })
      setStats(s)
    } catch (e: any) {
      setErr(e?.message || 'Erro ao carregar regras de afiliados')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  // Validação client-side antes do POST. Devolve mensagem ou '' se ok.
  function validate(): string {
    if (rules.default_commission_pct < 0 || rules.default_commission_pct > 100) {
      return 'Comissão padrão deve estar entre 0 e 100%.'
    }
    if (rules.default_retention_days < 0 || !Number.isInteger(rules.default_retention_days)) {
      return 'Dias de retenção deve ser inteiro >= 0.'
    }
    if (rules.default_withdraw_fee < 0) return 'Taxa de saque não pode ser negativa.'
    if (rules.default_penalty_value < 0) return 'Penalidade padrão não pode ser negativa.'
    if (rules.first_frustration_penalty < 0) return 'Penalidade da 1ª frustração não pode ser negativa.'
    if (rules.producer_frustration_penalty < 0) return 'Penalidade do produtor não pode ser negativa.'
    if (rules.min_withdraw_amount < 0) return 'Valor mínimo de saque não pode ser negativo.'
    if (rules.max_withdraw_per_month < 0) return 'Teto mensal de saque não pode ser negativo.'
    return ''
  }

  async function handleSave(e?: React.FormEvent) {
    e?.preventDefault()
    const v = validate()
    if (v) {
      showToast('err', v)
      return
    }
    setSaving(true)
    try {
      await api('/affiliate-rules', {
        method: 'POST',
        body: JSON.stringify(rules),
      })
      showToast('ok', 'Regras de afiliados salvas com sucesso.')
      // Recarrega stats — saldos podem ter mudado se houver job lateral.
      const s = await api<AffiliateRulesStats>('/affiliate-rules/stats').catch(() => null)
      if (s) setStats(s)
    } catch (err: any) {
      showToast('err', err?.message || 'Falha ao salvar regras')
    } finally {
      setSaving(false)
    }
  }

  // Setter helper — preserva tipos enquanto atualiza uma chave.
  function up<K extends keyof AffiliateRules>(key: K, value: AffiliateRules[K]) {
    setRules(prev => ({ ...prev, [key]: value }))
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Regras de Afiliados</h1>
          <p>Defaults globais aplicados a novos afiliados, retenção de saldo e penalidades de frustração</p>
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

      {/* ---------- KPIs ---------- */}
      {stats && (
        <div
          className="szv2-kpi-grid"
          style={{ gridTemplateColumns: 'repeat(5, minmax(0,1fr))', gap: 16, marginBottom: 24 }}
        >
          <KpiCard
            label="Total afiliados"
            value={stats.total_affiliates.toLocaleString('pt-BR')}
            sub="cadastrados na plataforma"
          />
          <KpiCard
            label="Aprovados"
            value={stats.total_approved.toLocaleString('pt-BR')}
            sub="ativos e operando"
            tone="success"
          />
          <KpiCard
            label="Pendentes aprovação"
            value={stats.total_pending_approval.toLocaleString('pt-BR')}
            sub="aguardando produtor"
            tone="warning"
          />
          <KpiCard
            label="Comissão média"
            value={`${fmt(stats.avg_commission_pct)}%`}
            sub="vínculos com override"
          />
          <KpiCard
            label="Saldo total a pagar"
            value={`R$ ${fmt(stats.total_balance_available + stats.total_balance_pending)}`}
            sub={`R$ ${fmt(stats.total_balance_available)} disponível · R$ ${fmt(stats.total_balance_pending)} pendente`}
          />
        </div>
      )}

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando regras…
        </div>
      ) : (
        <form onSubmit={handleSave}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
            {/* ---------- Card 1: Regras Padrão ---------- */}
            <div className="szv2-card">
              <div className="szv2-card-head">
                <div>
                  <h2>Regras Padrão</h2>
                  <p className="szv2-card-sub">
                    Aplicadas a afiliados sem override específico
                  </p>
                </div>
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
                <NumberField
                  label="Comissão padrão"
                  value={rules.default_commission_pct}
                  onChange={v => up('default_commission_pct', Math.min(100, Math.max(0, v)))}
                  step="0.1"
                  min={0}
                  max={100}
                  suffix="%"
                  help="Percentual de comissão padrão para novos vínculos afiliado→produto"
                />

                <NumberField
                  label="Dias de retenção"
                  value={rules.default_retention_days}
                  onChange={v => up('default_retention_days', Math.max(0, Math.round(v)))}
                  step="1"
                  min={0}
                  suffix="dias"
                  help="Período em que a comissão fica 'pendente' antes de virar saldo disponível"
                />

                <NumberField
                  label="Taxa de saque"
                  value={rules.default_withdraw_fee}
                  onChange={v => up('default_withdraw_fee', Math.max(0, v))}
                  step="0.01"
                  min={0}
                  suffix="R$"
                  help="Desconto fixo aplicado a cada solicitação de saque do afiliado"
                />

                <NumberField
                  label="Valor mínimo de saque"
                  value={rules.min_withdraw_amount}
                  onChange={v => up('min_withdraw_amount', Math.max(0, v))}
                  step="0.01"
                  min={0}
                  suffix="R$"
                  help="Saldo mínimo que o afiliado precisa ter para solicitar saque"
                />

                <NumberField
                  label="Teto mensal de saque"
                  value={rules.max_withdraw_per_month}
                  onChange={v => up('max_withdraw_per_month', Math.max(0, v))}
                  step="0.01"
                  min={0}
                  suffix="R$"
                  help="Soma máxima de saques aprovados por mês · 0 = ilimitado"
                />

                {/* Toggle auto-approve */}
                <div className="szv2-field">
                  <label
                    className="szv2-label"
                    style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: 'pointer' }}
                  >
                    <input
                      type="checkbox"
                      checked={rules.auto_approve}
                      onChange={e => up('auto_approve', e.target.checked)}
                      style={{ width: 18, height: 18, cursor: 'pointer' }}
                    />
                    <span>Aprovar afiliados automaticamente</span>
                  </label>
                  <span
                    className="szv2-help"
                    style={{ display: 'block', marginTop: 4, fontSize: 12, color: 'var(--szv2-text-muted)' }}
                  >
                    Quando ativo, novos pedidos de afiliação caem direto em <strong>aprovado</strong>.
                    Quando inativo, ficam em <strong>pendente</strong> aguardando o produtor.
                  </span>
                </div>
              </div>
            </div>

            {/* ---------- Card 2: Penalidades Padrão ---------- */}
            <div className="szv2-card">
              <div className="szv2-card-head">
                <div>
                  <h2>Penalidades Padrão</h2>
                  <p className="szv2-card-sub">
                    Aplicado quando pedido marcado como frustrado
                  </p>
                </div>
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
                <NumberField
                  label="Penalidade padrão (reincidente)"
                  value={rules.default_penalty_value}
                  onChange={v => up('default_penalty_value', Math.max(0, v))}
                  step="0.01"
                  min={0}
                  suffix="R$"
                  help="Cobrada do afiliado em frustrações a partir da 2ª no mesmo período"
                />

                <NumberField
                  label="Penalidade da 1ª frustração"
                  value={rules.first_frustration_penalty}
                  onChange={v => up('first_frustration_penalty', Math.max(0, v))}
                  step="0.01"
                  min={0}
                  suffix="R$"
                  help="Cobrada do afiliado na primeira frustração do período (geralmente menor)"
                />

                <NumberField
                  label="Penalidade do produtor"
                  value={rules.producer_frustration_penalty}
                  onChange={v => up('producer_frustration_penalty', Math.max(0, v))}
                  step="0.01"
                  min={0}
                  suffix="R$"
                  help="Valor cobrado do produtor (separado do afiliado) em pedidos frustrados"
                />

                <div
                  style={{
                    padding: 12,
                    background: 'rgba(234,88,12,.06)',
                    borderRadius: 8,
                    border: '1px solid rgba(234,88,12,.20)',
                    marginTop: 6,
                  }}
                >
                  <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)', lineHeight: 1.5 }}>
                    Estes valores são <strong>defaults globais</strong>. Overrides por afiliado ou
                    produtor podem ser configurados na tela <em>COD · Taxas de Entrega</em>.
                  </span>
                </div>
              </div>
            </div>
          </div>

          {/* ---------- Botão salvar ---------- */}
          <div
            style={{
              marginTop: 24,
              display: 'flex',
              gap: 12,
              alignItems: 'center',
              justifyContent: 'flex-end',
            }}
          >
            <button
              type="button"
              className="szv2-btn-secondary"
              onClick={() => load()}
              disabled={saving || loading}
            >
              Restaurar do servidor
            </button>
            <button
              type="submit"
              className="szv2-btn-brand"
              disabled={saving || loading}
              style={{ minWidth: 180 }}
            >
              {saving ? 'Salvando…' : 'Salvar regras'}
            </button>
          </div>
        </form>
      )}
    </div>
  )
}
