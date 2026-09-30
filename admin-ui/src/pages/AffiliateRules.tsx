import { useEffect, useState } from 'react'
import { useToast } from '../hooks/useToast'
import { api } from '../api'
import FalkSelect from '../components/FalkSelect'

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

// FEAT-RBAC-2026-06-21 — recompensa de convite (programa "indique e ganhe").
// Persistida em senderzz_options sob `sz_invite_reward_type` / `sz_invite_reward_value`,
// gravadas via PUT /settings (mesmo endpoint canônico de options do admin).
//
// BACKEND GAP (DOCUMENTADO, NÃO chutar semântica financeira):
//   - go/admin/internal/handlers/settings.go ainda NÃO conhece estas chaves.
//     `settingsSave` (linhas 82-88) não tem os campos → o PUT as ignora
//     silenciosamente; `settingsResp`/Get (linhas 71-128) não as devolve.
//     Para o round-trip funcionar é preciso adicionar `sz_invite_reward_type`
//     e `sz_invite_reward_value` AOS DOIS: struct `settingsSave` + upsert no
//     Save, e struct `settingsResp` + leitura no Get.
//   - O PUT /settings é merge por-campo (ponteiros), então enviar só estas duas
//     chaves NÃO apaga as demais options — body parcial é seguro.
//   - Esta tela define APENAS tipo + valor da recompensa. QUANDO ela paga, A
//     QUEM e em que evento é regra financeira nova → fica para o backend; aqui
//     não inventamos esses gatilhos.
type InviteReward = {
  type: 'none' | 'fixed' | 'percent'
  value: number
}

// FALK-2026-06-22 — taxa de frustração (configurável, REGRA DO DONO).
// Persistida em senderzz_options sob `sz_frustration_fee_type` / `sz_frustration_fee_value`,
// gravadas via PUT /settings (mesmo endpoint canônico de options do admin).
//
// SEMÂNTICA (não mexer no backend aqui): a config vale para pedidos NOVOS
// frustrados. Pedidos ANTIGOS preservam o que JÁ foi cobrado — o backend
// (order_detail.go) lê o valor armazenado nas metas `_sz_aff_frustration_penalty` /
// `_sz_prod_frustration_penalty` e NÃO recomputa. Esta tela define só tipo + valor.
//
// BACKEND GAP (DOCUMENTADO, mesmo padrão do invite-reward): settings.go ainda NÃO
// conhece estas chaves → o PUT é no-op até o handler ler/gravar `sz_frustration_fee_*`.
// Por isso o toast é honesto (não mente "salvo").
type FrustrationFee = {
  type: 'none' | 'fixed' | 'percent'
  value: number
}

// Resposta parcial de GET /settings — só os campos que esta tela consome.
// (O endpoint devolve mais; deixamos o resto fora do type de propósito.)
type SettingsRewardResp = {
  invite_reward_type?: InviteReward['type']
  invite_reward_value?: number
  // FALK-2026-06-22 — gap-tolerante: GET ainda não devolve estas chaves → cai no default.
  frustration_fee_type?: FrustrationFee['type']
  frustration_fee_value?: number
}

const INVITE_REWARD_TYPES: { value: InviteReward['type']; label: string }[] = [
  { value: 'none',    label: 'Desativada' },
  { value: 'fixed',   label: 'Valor fixo (R$)' },
  { value: 'percent', label: 'Percentual (%)' },
]

// FALK-2026-06-22 — mesmos rótulos da recompensa (Desativada / Valor fixo / Percentual).
const FRUSTRATION_FEE_TYPES: { value: FrustrationFee['type']; label: string }[] = [
  { value: 'none',    label: 'Desativada' },
  { value: 'fixed',   label: 'Valor fixo (R$)' },
  { value: 'percent', label: 'Percentual (%)' },
]

const DEFAULT_INVITE_REWARD: InviteReward = { type: 'none', value: 0 }
const DEFAULT_FRUSTRATION_FEE: FrustrationFee = { type: 'none', value: 0 }

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

// `embedded` esconde o cabeçalho interno, a faixa de 5 KPIs e o card de
// "Recompensa de indicação" — usados quando a tela única (AfiliadosFinHub)
// promove esses elementos. Os formulários (Regras Padrão + Penalidades +
// Taxa de frustração) e seus botões de salvar permanecem 100% funcionais.
export default function AffiliateRules({
  embedded = false,
}: {
  embedded?: boolean
} = {}) {
  const [rules, setRules] = useState<AffiliateRules>(DEFAULT_RULES)
  const [stats, setStats] = useState<AffiliateRulesStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const showToast = useToast() // AUDIT-2026-06-18 Onda3

  // FEAT-RBAC-2026-06-21 — recompensa de convite: estado e salvar independentes
  // (round-trip próprio em /settings, fora do form de regras de afiliado).
  const [reward, setReward] = useState<InviteReward>(DEFAULT_INVITE_REWARD)
  const [rewardSaving, setRewardSaving] = useState(false)

  // FALK-2026-06-22 — taxa de frustração: estado e salvar independentes
  // (round-trip próprio em /settings, fora do form de regras de afiliado).
  const [frustFee, setFrustFee] = useState<FrustrationFee>(DEFAULT_FRUSTRATION_FEE)
  const [frustFeeSaving, setFrustFeeSaving] = useState(false)


  async function load() {
    setLoading(true)
    setErr('')
    try {
      // Carrega rules + stats + recompensa em paralelo (corte de latência).
      // O GET /settings é tolerante: hoje não devolve as chaves de recompensa
      // (BACKEND GAP) → cai no default. `.catch(()=>null)` evita derrubar a tela.
      const [r, s, rw] = await Promise.all([
        api<AffiliateRules>('/affiliate-rules'),
        api<AffiliateRulesStats>('/affiliate-rules/stats').catch(() => null),
        api<SettingsRewardResp>('/settings').catch(() => null), // FEAT-RBAC-2026-06-21
      ])
      // Mescla com defaults caso o backend devolva campos faltando.
      setRules({ ...DEFAULT_RULES, ...r })
      setStats(s)
      if (rw) {
        setReward({
          type: rw.invite_reward_type ?? DEFAULT_INVITE_REWARD.type,
          value: typeof rw.invite_reward_value === 'number' ? rw.invite_reward_value : DEFAULT_INVITE_REWARD.value,
        })
        // FALK-2026-06-22 — taxa de frustração (gap-tolerante → cai no default).
        setFrustFee({
          type: rw.frustration_fee_type ?? DEFAULT_FRUSTRATION_FEE.type,
          value: typeof rw.frustration_fee_value === 'number' ? rw.frustration_fee_value : DEFAULT_FRUSTRATION_FEE.value,
        })
      }
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

  // FEAT-RBAC-2026-06-21 — salva SÓ a recompensa de convite, em /settings.
  // Round-trip independente do form de regras (chaves sz_invite_* ≠ sz_aff_*).
  // PUT /settings é merge por-campo → body parcial não apaga outras options.
  // NOTA: enquanto settings.go não conhecer estas chaves, o PUT é no-op no
  // backend (não persiste) — por isso o toast é neutro, não um "salvo" mentiroso.
  async function handleSaveReward() {
    const v = reward.type === 'none' ? 0 : Math.max(0, reward.value)
    if (reward.type === 'percent' && v > 100) {
      showToast('err', 'Percentual de recompensa deve estar entre 0 e 100%.')
      return
    }
    setRewardSaving(true)
    try {
      await api('/settings', {
        method: 'PUT',
        body: JSON.stringify({
          invite_reward_type: reward.type,
          invite_reward_value: v,
        }),
      })
      // Mensagem honesta: o backend ainda pode não persistir (gap documentado).
      showToast('info', 'Recompensa de convite enviada. Persistência depende de suporte no backend /settings (sz_invite_reward_*).')
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao salvar recompensa de convite')
    } finally {
      setRewardSaving(false)
    }
  }

  // FALK-2026-06-22 — salva SÓ a taxa de frustração, em /settings.
  // Round-trip independente do form de regras (chaves sz_frustration_fee_* ≠ sz_aff_*).
  // PUT /settings é merge por-campo → body parcial não apaga outras options.
  // NOTA: enquanto settings.go não conhecer estas chaves, o PUT é no-op no backend
  // (não persiste) — por isso o toast é neutro, não um "salvo" mentiroso.
  async function handleSaveFrustFee() {
    const v = frustFee.type === 'none' ? 0 : Math.max(0, frustFee.value)
    if (frustFee.type === 'percent' && v > 100) {
      showToast('err', 'Percentual da taxa de frustração deve estar entre 0 e 100%.')
      return
    }
    setFrustFeeSaving(true)
    try {
      await api('/settings', {
        method: 'PUT',
        body: JSON.stringify({
          frustration_fee_type: frustFee.type,
          frustration_fee_value: v,
        }),
      })
      // Mensagem honesta: o backend ainda pode não persistir (gap documentado).
      showToast('info', 'Taxa de frustração enviada. Persistência depende de suporte no backend /settings (sz_frustration_fee_*).')
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao salvar taxa de frustração')
    } finally {
      setFrustFeeSaving(false)
    }
  }

  return (
    <div>
      {!embedded && (
        <div className="szv2-section-head">
          <div>
            <h1>Regras de Afiliados</h1>
            <p>Defaults globais aplicados a novos afiliados, retenção de saldo e penalidades de frustração</p>
          </div>
        </div>
      )}

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* ---------- KPIs — ocultos em embedded (promovidos para a faixa do pai) ---------- */}
      {!embedded && stats && (
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
        <>
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
                    background: 'rgba(30, 111, 242,.06)',
                    borderRadius: 8,
                    border: '1px solid rgba(30, 111, 242,.20)',
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
              type="submit"
              className="szv2-btn szv2-btn-brand"
              disabled={saving || loading}
              style={{ minWidth: 180 }}
            >
              {saving ? 'Salvando…' : 'Salvar regras'}
            </button>
          </div>
        </form>

        {/* ---------- FEAT-RBAC-2026-06-21 — Recompensa de convite ---------- */}
        {/* Card autônomo: GET/PUT /settings próprio, botão de salvar próprio.
            Não compartilha o form de regras (chaves sz_invite_* ≠ sz_aff_*).
            Em embedded esconde-se — a tela única (AfiliadosFinHub) mostra uma
            versão compacta da mesma regra fixa no corpo principal. */}
        {!embedded && (
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Recompensa de indicação (indique e ganhe)</h2>
              <p className="szv2-card-sub">
                Regra FIXA, igual para qualquer usuário — não é configurável. Cada
                usuário tem um link individual de indicação no próprio perfil
                (<span style={{ fontFamily: 'var(--szv2-font-mono)' }}>falklog.com.br/r/&#123;código&#125;</span>);
                quem se cadastra por ele fica associado ao indicador, que recebe abaixo.
              </p>
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14, alignItems: 'stretch' }}>
            <div style={{ padding: 16, background: 'var(--szv2-brand-light)', borderRadius: 10 }}>
              <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Pedido COD (pago na entrega) concluído</div>
              <div style={{ fontSize: 30, fontWeight: 800, color: 'var(--szv2-brand)', lineHeight: 1.1 }}>2,5%</div>
              <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>do valor do pedido, por indicação</div>
            </div>
            <div style={{ padding: 16, background: 'var(--szv2-bg-soft, #f8fafc)', borderRadius: 10, border: '1px solid var(--szv2-divider)' }}>
              <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Pedido por Expedição (frete)</div>
              <div style={{ fontSize: 30, fontWeight: 800, color: 'var(--szv2-text)', lineHeight: 1.1 }}>1%</div>
              <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>do valor do pedido, por indicação</div>
            </div>
          </div>

          <div
            style={{
              padding: 12,
              background: 'rgba(30, 111, 242,.06)',
              borderRadius: 8,
              border: '1px solid rgba(30, 111, 242,.20)',
              marginTop: 12,
            }}
          >
            <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)', lineHeight: 1.5 }}>
              Recompensa paga ao INDICADOR quando o usuário indicado gera pedido: 2,5% em COD
              concluído, 1% em expedição. O crédito automático na carteira é processado no
              backend (cron de indicação) — a tela não tem mais configuração de tipo/valor.
            </span>
          </div>
        </div>
        )}

        {/* ---------- FALK-2026-06-22 — Taxa de frustração ---------- */}
        {/* Card autônomo: GET/PUT /settings próprio, botão de salvar próprio.
            Não compartilha o form de regras (chaves sz_frustration_fee_* ≠ sz_aff_*). */}
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Taxa de frustração</h2>
              <p className="szv2-card-sub">
                Define o tipo e o valor cobrado quando um pedido é marcado como frustrado.
              </p>
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14, alignItems: 'flex-start' }}>
            <div className="szv2-field">
              <label className="szv2-label">Tipo de taxa</label>
              <FalkSelect
                value={frustFee.type}
                onChange={v => setFrustFee(prev => ({ ...prev, type: v as FrustrationFee['type'] }))}
                options={FRUSTRATION_FEE_TYPES.map(t => ({ value: t.value, label: t.label }))}
                aria-label="Tipo de taxa"
              />
              <span
                className="szv2-help"
                style={{ display: 'block', marginTop: 4, fontSize: 12, color: 'var(--szv2-text-muted)' }}
              >
                "Desativada" zera o valor — nenhuma taxa de frustração é cobrada.
              </span>
            </div>

            {frustFee.type !== 'none' && (
              <NumberField
                label="Valor da taxa"
                value={frustFee.value}
                onChange={v => setFrustFee(prev => ({
                  ...prev,
                  value: frustFee.type === 'percent' ? Math.min(100, Math.max(0, v)) : Math.max(0, v),
                }))}
                step="0.01"
                min={0}
                max={frustFee.type === 'percent' ? 100 : undefined}
                suffix={frustFee.type === 'percent' ? '%' : 'R$'}
                help={frustFee.type === 'percent'
                  ? 'Percentual aplicado sobre o pedido frustrado.'
                  : 'Valor fixo em R$ por pedido frustrado.'}
              />
            )}
          </div>

          {/* Nota da REGRA DO DONO: vale só para pedidos NOVOS; antigos preservam o cobrado. */}
          <div
            style={{
              padding: 12,
              background: 'rgba(30, 111, 242,.06)',
              borderRadius: 8,
              border: '1px solid rgba(30, 111, 242,.20)',
              marginTop: 6,
            }}
          >
            <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)', lineHeight: 1.5 }}>
              Aplica-se a pedidos <strong>NOVOS</strong> frustrados; pedidos antigos mantêm o que já
              foi cobrado. Salva em{' '}
              <span style={{ fontFamily: 'var(--szv2-font-mono)' }}>sz_frustration_fee_type</span> /
              {' '}<span style={{ fontFamily: 'var(--szv2-font-mono)' }}>sz_frustration_fee_value</span> via
              {' '}<span style={{ fontFamily: 'var(--szv2-font-mono)' }}>/settings</span>. A persistência
              depende de suporte no backend (essas chaves ainda não são lidas/gravadas pelo handler
              de configurações).
            </span>
          </div>

          <div style={{ marginTop: 20, display: 'flex', justifyContent: 'flex-end' }}>
            <button
              type="button"
              className="szv2-btn szv2-btn-brand"
              disabled={frustFeeSaving || loading}
              onClick={handleSaveFrustFee}
              style={{ minWidth: 180 }}
            >
              {frustFeeSaving ? 'Salvando…' : 'Salvar taxa de frustração'}
            </button>
          </div>
        </div>
        </>
      )}
    </div>
  )
}
