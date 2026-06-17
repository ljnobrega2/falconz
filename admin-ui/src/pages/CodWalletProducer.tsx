import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'

// ---------------------------------------------------------------------------
// Tipos espelhados de internal/handlers/cod_wallet_producer.go
// ---------------------------------------------------------------------------

type Summary = {
  total_pending: number
  total_available: number
  total_paid_30d: number
  producers_count: number
}

type PixDefault = {
  holder: string
  key: string
  type: string // cpf | cnpj | email | telefone | aleatoria
}

type Row = {
  user_id: number
  nome: string
  email: string
  saldo_pending: number
  saldo_available: number
  saldo_paid_30d: number
  pix_default: PixDefault | null
  ultima_movimentacao: string | null
}

type Account = {
  id: number
  holder_name: string
  holder_cpf: string
  pix_type: string
  pix_key: string
  is_default: boolean
}

// ---------------------------------------------------------------------------
// Tipos do P&L financeiro (cod_livro.go)
// ---------------------------------------------------------------------------

type FinSummary = {
  bruto_cod: number
  afiliados: number
  taxas_senderzz: number
  liquido_produtor: number
  previsto_produtor: number
}

type FinProducerRow = {
  producer_id: number
  producer_email: string
  pedidos: number
  recebidos: number
  frustrados: number
  previstos: number
  bruto: number
  bruto_previsto: number
  taxas_senderzz: number
  afiliado: number
  liquido_produtor: number
  frustrado_produtor: number
  frustrado_afiliados: number
  frustrado_valor: number
}

// ---------------------------------------------------------------------------
// Tipos de regras globais e overrides (cod_saques.go)
// ---------------------------------------------------------------------------

type GlobalRules = {
  retention_days: number
  withdraw_fee: number
  anticipation_fee_pct: number
  motoboy_fee: number
  operational_fund_fee: number
}

type ProducerOverride = {
  user_id: number
  nome: string
  email: string
  retention_days: number | null
  withdraw_fee: number | null
  anticipation_fee_pct: number | null
  eff_retention_days: number
  eff_withdraw_fee: number
  eff_anticipation_fee: number
}

// ---------------------------------------------------------------------------
// Helpers de formatação
// ---------------------------------------------------------------------------

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const money = (v: number) => 'R$ ' + fmt(v)

// Máscara da key PIX para a coluna "PIX padrão".
// CPF/CNPJ: pega últimos 4 dígitos e prefixa com '•••'.
// Email/telefone/aleatoria: trunca em 22 chars com reticências.
function maskPixKey(type: string, key: string): string {
  if (!key) return '—'
  const t = (type || '').toLowerCase()
  if (t === 'cpf' || t === 'cnpj') {
    const digits = key.replace(/\D/g, '')
    if (digits.length <= 4) return '•••' + digits
    return '•••' + digits.slice(-4)
  }
  if (key.length > 22) return key.slice(0, 22) + '…'
  return key
}

// Badge color por tipo de chave PIX.
const PIX_TYPE_BADGE: Record<string, string> = {
  cpf:        'szv2-badge-info',
  cnpj:       'szv2-badge-info',
  email:      'szv2-badge-neutral',
  telefone:   'szv2-badge-neutral',
  aleatoria:  'szv2-badge-warning',
}

function fmtDateBR(iso: string | null | undefined): string {
  if (!iso) return '—'
  // O backend devolve algo como "2026-06-16 14:32:11" ou "2026-06-16T14:32:11Z".
  const safe = iso.replace('T', ' ').slice(0, 16)
  return safe
}

// Data padrão: últimos 7 dias
function defaultDateRange(): { from: string; to: string } {
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - 7)
  const fmt = (d: Date) => d.toISOString().slice(0, 10)
  return { from: fmt(from), to: fmt(to) }
}

// ---------------------------------------------------------------------------
// KPI card (mesma estética de AuditEngine / AffiliateWallet)
// ---------------------------------------------------------------------------

function KpiCard({
  label, value, sub, tone,
}: {
  label: string
  value: string | number
  sub?: string
  tone?: 'brand' | 'success' | 'warning' | 'info'
}) {
  const color = (() => {
    switch (tone) {
      case 'success': return 'var(--szv2-success)'
      case 'warning': return 'var(--szv2-warning)'
      case 'info':    return 'var(--szv2-info)'
      default:        return 'var(--szv2-brand)'
    }
  })()
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

// ---------------------------------------------------------------------------
// Drawer: contas PIX + atalho para a tela de transações
// ---------------------------------------------------------------------------

function AccountsDrawer({
  row, onClose,
}: {
  row: Row
  onClose: () => void
}) {
  const [items, setItems] = useState<Account[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  useEffect(() => {
    let active = true
    setLoading(true); setErr('')
    api<{ items: Account[] }>(`/cod-wallet-producer/${row.user_id}/accounts`)
      .then(r => { if (active) setItems(r.items || []) })
      .catch(e => { if (active) setErr(e.message || 'Erro ao carregar contas') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [row.user_id])

  return (
    <div
      className="szv2-modal-overlay szv2-open"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="szv2-modal szv2-modal-lg" style={{ maxWidth: 760 }}>
        <div className="szv2-modal-head">
          <h3>Carteira COD — {row.nome || row.email || `#${row.user_id}`}</h3>
          <button className="szv2-modal-x" onClick={onClose} aria-label="Fechar">✕</button>
        </div>

        <div className="szv2-modal-body">
          {/* Mini-resumo do produtor */}
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(3, minmax(0,1fr))',
              gap: 12,
              marginBottom: 16,
            }}
          >
            <div style={{ padding: 12, background: 'var(--szv2-warning-bg)', borderRadius: 8 }}>
              <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Pendente</div>
              <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-warning)' }}>
                {money(row.saldo_pending)}
              </div>
            </div>
            <div style={{ padding: 12, background: 'var(--szv2-success-bg)', borderRadius: 8 }}>
              <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Disponível</div>
              <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-success)' }}>
                {money(row.saldo_available)}
              </div>
            </div>
            <div style={{ padding: 12, background: 'var(--szv2-info-bg)', borderRadius: 8 }}>
              <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Pago (30d)</div>
              <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-info)' }}>
                {money(row.saldo_paid_30d)}
              </div>
            </div>
          </div>

          <h4 style={{ margin: '8px 0 12px' }}>Contas PIX cadastradas</h4>

          {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}

          {loading ? (
            <div style={{ padding: 40, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Carregando…
            </div>
          ) : items.length === 0 ? (
            <div className="szv2-empty">
              <h3>Nenhuma conta PIX</h3>
              <p>O produtor ainda não cadastrou contas para receber saques COD.</p>
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Titular</th>
                    <th>CPF</th>
                    <th>Tipo</th>
                    <th>Chave</th>
                    <th>Padrão</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map(a => (
                    <tr key={a.id}>
                      <td style={{ fontWeight: 600 }}>{a.holder_name || '—'}</td>
                      <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                        {a.holder_cpf || '—'}
                      </td>
                      <td>
                        <span
                          className={`sz-badge ${PIX_TYPE_BADGE[a.pix_type] || 'szv2-badge-neutral'}`}
                        >
                          {a.pix_type || '—'}
                        </span>
                      </td>
                      <td
                        style={{
                          fontFamily: 'var(--szv2-font-mono)',
                          fontSize: 12,
                          maxWidth: 220,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                        title={a.pix_key}
                      >
                        {a.pix_key || '—'}
                      </td>
                      <td>
                        {a.is_default
                          ? <span className="sz-badge szv2-badge-success">padrão</span>
                          : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="szv2-modal-foot">
          <a
            className="szv2-btn szv2-btn-secondary"
            href={`/cod-wallet-transactions?user_id=${row.user_id}`}
          >
            Ver todas as transações
          </a>
          <button className="szv2-btn szv2-btn-secondary" onClick={onClose}>Fechar</button>
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Seção Financeiro / P&L (espelha tab_fin_produtores do WP)
// ---------------------------------------------------------------------------

function FinancialSection() {
  const def = defaultDateRange()
  const [from, setFrom] = useState(def.from)
  const [to, setTo] = useState(def.to)
  const [draftFrom, setDraftFrom] = useState(def.from)
  const [draftTo, setDraftTo] = useState(def.to)
  const [filterOpen, setFilterOpen] = useState(false)
  const [finSummary, setFinSummary] = useState<FinSummary | null>(null)
  const [finRows, setFinRows] = useState<FinProducerRow[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')

  async function load() {
    setLoading(true); setErr('')
    try {
      const [s, r] = await Promise.all([
        api<FinSummary>(`/cod-livro/summary?from=${from}&to=${to}`),
        api<{ items: FinProducerRow[] }>(`/cod-livro/producers-summary?from=${from}&to=${to}`),
      ])
      setFinSummary(s)
      setFinRows(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar financeiro')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [from, to]) // eslint-disable-line react-hooks/exhaustive-deps

  function openPanel()    { setDraftFrom(from); setDraftTo(to); setFilterOpen(true) }
  function applyFilters() { setFrom(draftFrom); setTo(draftTo); setFilterOpen(false) }
  function clearFilters() {
    setDraftFrom(def.from); setDraftTo(def.to)
    setFrom(def.from); setTo(def.to); setFilterOpen(false)
  }

  const chips: ActiveChip[] = []
  if (from !== def.from) chips.push({ key: 'from', label: `De: ${from}`, onRemove: () => setFrom(def.from) })
  if (to   !== def.to)   chips.push({ key: 'to',   label: `Até: ${to}`,   onRemove: () => setTo(def.to) })

  return (
    <div className="szv2-card" style={{ marginBottom: 24 }}>
      <div className="szv2-card-head">
        <div>
          <h2>Financeiro / P&amp;L por Produtor</h2>
          <p className="szv2-card-sub">
            Agrega pedidos do período por produtor (bruto, afiliados, taxas, líquido).
            Espelha tab_fin_produtores do WP.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <FilterButton active={chips.length > 0} count={chips.length} onClick={openPanel} />
          <button
            className="szv2-btn szv2-btn-secondary"
            onClick={load}
            disabled={loading}
          >
            {loading ? 'Buscando…' : 'Atualizar'}
          </button>
        </div>
      </div>

      <div style={{ padding: '0 16px' }}>
        <ActiveFilterChips chips={chips} onClearAll={clearFilters} />
      </div>

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros — Financeiro"
      >
        <FilterField label="Data inicial">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFrom}
            onChange={e => setDraftFrom(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftTo}
            onChange={e => setDraftTo(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>

      {err && <div className="sz-alert-danger" style={{ margin: '12px 0' }}>{err}</div>}

      {/* 5 KPIs do WP (linhas 792–796 Unified_Menu.php) */}
      {finSummary && (
        <div
          className="szv2-kpi-grid"
          style={{ gridTemplateColumns: 'repeat(5, minmax(0,1fr))', margin: '16px 0' }}
        >
          <KpiCard
            label="Bruto COD"
            value={money(finSummary.bruto_cod)}
            sub="sem frustrados"
          />
          <KpiCard
            label="Afiliados"
            value={money(finSummary.afiliados)}
            sub="repasse"
            tone="warning"
          />
          <KpiCard
            label="Taxas Senderzz"
            value={money(finSummary.taxas_senderzz)}
            sub="operação"
          />
          <KpiCard
            label="Líquido produtor"
            value={money(finSummary.liquido_produtor)}
            sub="líquido"
            tone="success"
          />
          <KpiCard
            label="Previsto produtor"
            value={money(finSummary.previsto_produtor)}
            sub="agendados/em aberto"
            tone="warning"
          />
        </div>
      )}

      {/* Tabela de produtores — 11 colunas do WP (linha 798) */}
      {loading && finRows.length === 0 ? (
        <TableSkeleton rows={5} cols={11} />
      ) : !loading && finRows.length === 0 ? (
        <EmptyState
          icon="📊"
          title="Nenhum produtor no período."
          description="Tente ajustar o intervalo de datas."
        />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>Produtor</th>
              <th style={{ textAlign: 'right' }}>Bruto COD</th>
              <th style={{ textAlign: 'right' }}>Afiliados</th>
              <th style={{ textAlign: 'right' }}>Taxas Senderzz</th>
              <th style={{ textAlign: 'right' }}>Líquido produtor</th>
              <th style={{ textAlign: 'right' }}>Previsto</th>
              <th style={{ textAlign: 'right' }}>Potencial frustrado</th>
              <th style={{ textAlign: 'right' }}>Pedidos</th>
              <th style={{ textAlign: 'right' }}>Entregues</th>
              <th style={{ textAlign: 'right' }}>Previstos</th>
              <th style={{ textAlign: 'right' }}>Frustrados</th>
            </tr>
          </thead>
          <tbody>
            {finRows.map(r => (
              <tr key={r.producer_id}>
                <td>
                  <div style={{ fontWeight: 600 }}>
                    {r.producer_email || `#${r.producer_id}`}
                  </div>
                  <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                    ID {r.producer_id}
                  </div>
                </td>
                <td style={{ textAlign: 'right' }}>{money(r.bruto)}</td>
                <td style={{ textAlign: 'right', color: 'var(--szv2-warning)' }}>
                  {money(r.afiliado)}
                </td>
                <td style={{ textAlign: 'right' }}>{money(r.taxas_senderzz)}</td>
                <td style={{ textAlign: 'right', color: 'var(--szv2-success)', fontWeight: 700 }}>
                  {money(r.liquido_produtor)}
                </td>
                <td style={{ textAlign: 'right', color: 'var(--szv2-warning)' }}>
                  {money(r.bruto_previsto)}
                </td>
                <td style={{ textAlign: 'right', color: 'var(--szv2-danger)' }}>
                  {money(r.frustrado_valor)}
                </td>
                <td style={{ textAlign: 'right' }}>{r.pedidos}</td>
                <td style={{ textAlign: 'right', color: 'var(--szv2-success)' }}>{r.recebidos}</td>
                <td style={{ textAlign: 'right', color: 'var(--szv2-warning)' }}>{r.previstos}</td>
                <td style={{ textAlign: 'right', color: 'var(--szv2-danger)' }}>{r.frustrados}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Seção Regras de repasse (espelha sz_cod_admin_page do WP)
// ---------------------------------------------------------------------------

function RulesSection() {
  const [rules, setRules] = useState<GlobalRules>({
    retention_days: 7,
    withdraw_fee: 2.99,
    anticipation_fee_pct: 4.99,
    motoboy_fee: 18,
    operational_fund_fee: 2,
  })
  const [overrides, setOverrides] = useState<ProducerOverride[]>([])
  const [overrideEdits, setOverrideEdits] = useState<Record<number, Partial<ProducerOverride>>>({})
  const [loading, setLoading] = useState(true)
  const [savingRules, setSavingRules] = useState(false)
  const [savingOverrides, setSavingOverrides] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  async function loadAll() {
    setLoading(true); setErr('')
    try {
      const [gr, ov] = await Promise.all([
        api<{ rules: GlobalRules }>('/cod-saques/global-rules'),
        api<{ items: ProducerOverride[] }>('/cod-saques/producer/overrides'),
      ])
      setRules(gr.rules)
      setOverrides(ov.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar regras')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { loadAll() }, [])

  async function handleSaveRules(e: React.FormEvent) {
    e.preventDefault()
    setSavingRules(true)
    try {
      await api('/cod-saques/global-rules', { method: 'POST', body: JSON.stringify(rules) })
      showToast('ok', 'Regras globais salvas.')
    } catch (e: any) {
      showToast('err', e.message || 'Erro ao salvar regras')
    } finally {
      setSavingRules(false)
    }
  }

  async function handleSaveOverrides(e: React.FormEvent) {
    e.preventDefault()
    setSavingOverrides(true)
    // Monta apenas os produtores que tiveram campos editados
    const items = overrides.map(o => {
      const edit = overrideEdits[o.user_id] || {}
      return {
        user_id: o.user_id,
        retention_days: edit.retention_days !== undefined ? edit.retention_days : o.retention_days,
        withdraw_fee: edit.withdraw_fee !== undefined ? edit.withdraw_fee : o.withdraw_fee,
        anticipation_fee_pct:
          edit.anticipation_fee_pct !== undefined
            ? edit.anticipation_fee_pct
            : o.anticipation_fee_pct,
      }
    })
    try {
      await api('/cod-saques/producer/overrides', {
        method: 'POST',
        body: JSON.stringify({ items }),
      })
      showToast('ok', 'Overrides por produtor salvos.')
      await loadAll()
    } catch (e: any) {
      showToast('err', e.message || 'Erro ao salvar overrides')
    } finally {
      setSavingOverrides(false)
    }
  }

  function setOverrideField(
    userId: number,
    field: keyof ProducerOverride,
    value: string,
  ) {
    setOverrideEdits(prev => ({
      ...prev,
      [userId]: {
        ...prev[userId],
        [field]: value === '' ? null : field === 'retention_days' ? parseInt(value, 10) : parseFloat(value.replace(',', '.')),
      },
    }))
  }

  if (loading) {
    return (
      <div className="szv2-card" style={{ marginBottom: 24 }}>
        <div style={{ padding: 32, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando regras…
        </div>
      </div>
    )
  }

  return (
    <div style={{ marginBottom: 24 }}>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 12 }}
        >
          {toast.msg}
        </div>
      )}

      {/* Formulário de regras globais + taxas motoboy */}
      <form onSubmit={handleSaveRules}>
        <div className="szv2-card" style={{ marginBottom: 16 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Regras padrão de repasse</h2>
              <p className="szv2-card-sub">
                Fallback global. Campos vazios no produtor usam estes valores automaticamente.
              </p>
            </div>
          </div>

          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))',
              gap: 16,
              marginBottom: 20,
            }}
          >
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Retenção após entrega (dias)
              <input
                type="number"
                min="0"
                className="szv2-input"
                value={rules.retention_days}
                onChange={e =>
                  setRules(r => ({ ...r, retention_days: Math.max(0, parseInt(e.target.value, 10) || 0) }))
                }
              />
            </label>
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Taxa de saque padrão (R$)
              <input
                type="number"
                min="0"
                step="0.01"
                className="szv2-input"
                value={rules.withdraw_fee}
                onChange={e =>
                  setRules(r => ({ ...r, withdraw_fee: Math.max(0, parseFloat(e.target.value) || 0) }))
                }
              />
            </label>
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Taxa de antecipação padrão (%)
              <input
                type="number"
                min="0"
                step="0.01"
                className="szv2-input"
                value={rules.anticipation_fee_pct}
                onChange={e =>
                  setRules(r => ({
                    ...r,
                    anticipation_fee_pct: Math.max(0, parseFloat(e.target.value) || 0),
                  }))
                }
              />
            </label>
          </div>

          <h3 style={{ fontSize: 14, fontWeight: 600, marginBottom: 8 }}>
            Taxas administrativas Motoboy
          </h3>
          <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 12 }}>
            Usadas nas variáveis de comissão administrativa do push:{' '}
            <code>{'{{comissao_admin_liquida}}'}</code> e{' '}
            <code>{'{{comissao_admin_liquida_total}}'}</code>.
          </p>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))',
              gap: 16,
              marginBottom: 20,
            }}
          >
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Taxa motoboy admin (R$)
              <input
                type="number"
                min="0"
                step="0.01"
                className="szv2-input"
                value={rules.motoboy_fee}
                onChange={e =>
                  setRules(r => ({ ...r, motoboy_fee: Math.max(0, parseFloat(e.target.value) || 0) }))
                }
              />
            </label>
            <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
              Fundo operacional (R$)
              <input
                type="number"
                min="0"
                step="0.01"
                className="szv2-input"
                value={rules.operational_fund_fee}
                onChange={e =>
                  setRules(r => ({
                    ...r,
                    operational_fund_fee: Math.max(0, parseFloat(e.target.value) || 0),
                  }))
                }
              />
            </label>
          </div>

          <button
            type="submit"
            className="szv2-btn szv2-btn-brand"
            disabled={savingRules}
          >
            {savingRules ? 'Salvando…' : 'Salvar regras'}
          </button>
        </div>
      </form>

      {/* Tabela de overrides por produtor */}
      {overrides.length > 0 && (
        <form onSubmit={handleSaveOverrides}>
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Overrides por produtor</h2>
                <p className="szv2-card-sub">
                  Campos vazios herdam os valores globais acima.
                </p>
              </div>
            </div>

            <div className="szv2-table-wrap">
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Produtor</th>
                    <th>Retenção (dias)</th>
                    <th>Taxa saque (R$)</th>
                    <th>Taxa antecipação (%)</th>
                    <th>Efetivo</th>
                  </tr>
                </thead>
                <tbody>
                  {overrides.map(o => {
                    const edit = overrideEdits[o.user_id] || {}
                    return (
                      <tr key={o.user_id}>
                        <td>
                          <div style={{ fontWeight: 600 }}>{o.nome || o.email || `#${o.user_id}`}</div>
                          <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                            {o.email} · ID {o.user_id}
                          </div>
                        </td>
                        <td>
                          <input
                            type="number"
                            min="0"
                            className="szv2-input"
                            style={{ width: 90 }}
                            placeholder={String(rules.retention_days)}
                            value={
                              edit.retention_days !== undefined
                                ? (edit.retention_days ?? '')
                                : (o.retention_days ?? '')
                            }
                            onChange={e => setOverrideField(o.user_id, 'retention_days', e.target.value)}
                          />
                        </td>
                        <td>
                          <input
                            type="number"
                            min="0"
                            step="0.01"
                            className="szv2-input"
                            style={{ width: 90 }}
                            placeholder={fmt(rules.withdraw_fee)}
                            value={
                              edit.withdraw_fee !== undefined
                                ? (edit.withdraw_fee ?? '')
                                : (o.withdraw_fee ?? '')
                            }
                            onChange={e => setOverrideField(o.user_id, 'withdraw_fee', e.target.value)}
                          />
                        </td>
                        <td>
                          <input
                            type="number"
                            min="0"
                            step="0.01"
                            className="szv2-input"
                            style={{ width: 90 }}
                            placeholder={fmt(rules.anticipation_fee_pct)}
                            value={
                              edit.anticipation_fee_pct !== undefined
                                ? (edit.anticipation_fee_pct ?? '')
                                : (o.anticipation_fee_pct ?? '')
                            }
                            onChange={e =>
                              setOverrideField(o.user_id, 'anticipation_fee_pct', e.target.value)
                            }
                          />
                        </td>
                        <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                          {o.eff_retention_days} dias · R${' '}
                          {fmt(o.eff_withdraw_fee)} · {fmt(o.eff_anticipation_fee)}%
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            <div style={{ marginTop: 16 }}>
              <button
                type="submit"
                className="szv2-btn szv2-btn-brand"
                disabled={savingOverrides}
              >
                {savingOverrides ? 'Salvando…' : 'Salvar overrides'}
              </button>
            </div>
          </div>
        </form>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Página principal
// ---------------------------------------------------------------------------

export default function CodWalletProducer() {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [rows, setRows] = useState<Row[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState<number | null>(null)
  const [drawer, setDrawer] = useState<Row | null>(null)
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  // Aba ativa: 'wallet' | 'financeiro' | 'regras'
  const [tab, setTab] = useState<'wallet' | 'financeiro' | 'regras'>('wallet')

  // Filtros aplicados (aba Wallet).
  const [q, setQ] = useState('')
  const [userIDFilter, setUserIDFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftUserID, setDraftUserID] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  function openPanel() {
    setDraftQ(q); setDraftUserID(userIDFilter); setDraftStatus(statusFilter); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setUserIDFilter(draftUserID); setStatusFilter(draftStatus); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setUserIDFilter(''); setStatusFilter(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftUserID(''); setDraftStatus(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (userIDFilter) chips.push({ key: 'uid', label: `User #${userIDFilter}`, onRemove: () => setUserIDFilter('') })
  if (statusFilter) chips.push({ key: 'status', label: `Status: ${statusFilter}`, onRemove: () => setStatusFilter('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  async function loadSummary() {
    try {
      const s = await api<Summary>('/cod-wallet-producer/summary')
      setSummary(s)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar resumo')
    }
  }

  async function loadList() {
    setLoading(true); setErr('')
    try {
      const p = new URLSearchParams()
      p.set('limit', '300')
      if (q) p.set('q', q)
      if (userIDFilter) p.set('user_id', userIDFilter)
      if (statusFilter) p.set('status', statusFilter)
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      const r = await api<{ items: Row[] }>(`/cod-wallet-producer?${p.toString()}`)
      setRows(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar lista')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadSummary()
    loadList()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, userIDFilter, statusFilter, dataIni, dataFim])

  async function handleRelease(r: Row) {
    if (!window.confirm(
      `Liberar transações pendentes vencidas do produtor #${r.user_id}?\n\n` +
      'Promove status pending → available para registros cujo release_at já venceu.',
    )) return
    setBusy(r.user_id)
    try {
      const resp = await api<{ ok: boolean; released_count: number }>(
        `/cod-wallet-producer/${r.user_id}/release-pending`,
        { method: 'POST' },
      )
      showToast('ok',
        `${resp.released_count ?? 0} transação(ões) liberada(s) para o produtor #${r.user_id}.`)
      await Promise.all([loadSummary(), loadList()])
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao liberar')
    } finally {
      setBusy(null)
    }
  }

  // Lista filtrada client-side por nome/email enquanto digita (server já
  // suporta o mesmo filtro via ?q=, usado apenas no Enter / botão).
  const filteredRows = useMemo(() => {
    const term = q.trim().toLowerCase()
    if (!term) return rows
    return rows.filter(r =>
      (r.nome || '').toLowerCase().includes(term) ||
      (r.email || '').toLowerCase().includes(term))
  }, [rows, q])

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Carteira COD — Produtores</h1>
          <p>Saldos, P&amp;L financeiro e regras de repasse por produtor</p>
        </div>
        {tab === 'wallet' && (
          <div style={{ display: 'flex', gap: 8 }}>
            <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          </div>
        )}
      </div>

      {tab === 'wallet' && <ActiveFilterChips chips={chips} onClearAll={clearFilters} />}

      {/* Tabs de navegação */}
      <div style={{ display: 'flex', gap: 4, marginBottom: 20, borderBottom: '1px solid var(--szv2-border)' }}>
        {([
          { key: 'wallet',     label: 'Carteira / Saldos' },
          { key: 'financeiro', label: 'Financeiro / P&L' },
          { key: 'regras',     label: 'Regras de repasse' },
        ] as const).map(t => (
          <button
            key={t.key}
            type="button"
            onClick={() => setTab(t.key)}
            style={{
              padding: '8px 16px',
              border: 'none',
              background: 'transparent',
              cursor: 'pointer',
              fontWeight: tab === t.key ? 700 : 400,
              color: tab === t.key ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
              borderBottom: tab === t.key ? '2px solid var(--szv2-brand)' : '2px solid transparent',
              fontSize: 14,
            }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* ── Aba: Carteira / Saldos ── */}
      {tab === 'wallet' && (
        <>
          {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

          {toast && (
            <div
              className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
              style={{ marginBottom: 16 }}
            >
              {toast.msg}
            </div>
          )}

          {/* KPIs carteira */}
          {summary && (
            <div
              className="szv2-kpi-grid"
              style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))', marginBottom: 16 }}
            >
              <KpiCard
                label="Pendente"
                value={money(summary.total_pending)}
                sub="aguardando release_at"
                tone="warning"
              />
              <KpiCard
                label="Disponível"
                value={money(summary.total_available)}
                sub="pronto para saque"
                tone="success"
              />
              <KpiCard
                label="Pago (30d)"
                value={money(summary.total_paid_30d)}
                sub="saques concluídos"
                tone="info"
              />
              <KpiCard
                label="Produtores"
                value={summary.producers_count.toLocaleString('pt-BR')}
                sub="com movimentação COD"
                tone="brand"
              />
            </div>
          )}

          {/* Resumo da lista */}
          <div className="szv2-card" style={{ marginBottom: 16 }}>
            <div className="szv2-card-head">
              <div>
                <h2>Produtores</h2>
                <p className="szv2-card-sub">
                  {filteredRows.length} produtor(es)
                  {summary ? ` de ${summary.producers_count}` : ''}
                </p>
              </div>
            </div>
          </div>

          {/* Tabela de carteira */}
          {loading && rows.length === 0 ? (
            <TableSkeleton rows={6} cols={7} />
          ) : !loading && filteredRows.length === 0 ? (
            <EmptyState
              icon="💼"
              title="Nenhum produtor com carteira COD encontrado."
              description="Ajuste o filtro de busca ou aguarde a primeira movimentação."
            />
          ) : (
          <div className="szv2-table-wrap">
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Produtor</th>
                  <th style={{ textAlign: 'right' }}>Pending</th>
                  <th style={{ textAlign: 'right' }}>Available</th>
                  <th style={{ textAlign: 'right' }}>Pago (30d)</th>
                  <th>PIX padrão</th>
                  <th>Última mov.</th>
                  <th style={{ width: 260 }}>Ações</th>
                </tr>
              </thead>
              <tbody>
                {filteredRows.map(r => (
                  <tr key={r.user_id}>
                    <td>
                      <div style={{ fontWeight: 600 }}>{r.nome || '—'}</div>
                      <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                        {r.email || `wp_user #${r.user_id}`}
                      </div>
                    </td>
                    <td style={{ textAlign: 'right', color: 'var(--szv2-warning)', fontWeight: 700 }}>
                      {money(r.saldo_pending)}
                    </td>
                    <td style={{ textAlign: 'right', color: 'var(--szv2-success)', fontWeight: 700 }}>
                      {money(r.saldo_available)}
                    </td>
                    <td style={{ textAlign: 'right', color: 'var(--szv2-info)' }}>
                      {money(r.saldo_paid_30d)}
                    </td>
                    <td>
                      {r.pix_default ? (
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <span
                            className={`sz-badge ${PIX_TYPE_BADGE[r.pix_default.type] || 'szv2-badge-neutral'}`}
                          >
                            {r.pix_default.type || '—'}
                          </span>
                          <span
                            style={{
                              fontFamily: 'var(--szv2-font-mono)',
                              fontSize: 12,
                              color: 'var(--szv2-text-soft)',
                            }}
                            title={r.pix_default.key}
                          >
                            {maskPixKey(r.pix_default.type, r.pix_default.key)}
                          </span>
                        </div>
                      ) : (
                        <span style={{ color: 'var(--szv2-text-faint)' }}>sem conta</span>
                      )}
                    </td>
                    <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                      {fmtDateBR(r.ultima_movimentacao)}
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                        <button
                          type="button"
                          className="szv2-btn szv2-btn-sm szv2-btn-brand"
                          onClick={() => handleRelease(r)}
                          disabled={busy !== null}
                          title="Promove pending → available para tx cujo release_at já venceu"
                        >
                          {busy === r.user_id ? '…' : 'Liberar vencidos'}
                        </button>
                        <button
                          type="button"
                          className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                          onClick={() => setDrawer(r)}
                          disabled={busy !== null}
                        >
                          Ver tx
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          )}
        </>
      )}

      {/* ── Aba: Financeiro / P&L ── */}
      {tab === 'financeiro' && <FinancialSection />}

      {/* ── Aba: Regras de repasse ── */}
      {tab === 'regras' && <RulesSection />}

      {drawer && (
        <AccountsDrawer row={drawer} onClose={() => setDrawer(null)} />
      )}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <input
            type="date"
            style={filterInputStyle}
            value={draftIni}
            max={draftFim || undefined}
            onChange={e => setDraftIni(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFim}
            min={draftIni || undefined}
            onChange={e => setDraftFim(e.target.value)}
          />
        </FilterField>
        <FilterField label="User ID">
          <input
            type="number"
            style={filterInputStyle}
            placeholder="ex.: 42"
            value={draftUserID}
            onChange={e => setDraftUserID(e.target.value)}
          />
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value)}
          >
            <option value="">Todos</option>
            <option value="pending">Pendente</option>
            <option value="available">Disponível</option>
            <option value="paid">Pago</option>
          </select>
        </FilterField>
        <FilterField label="Busca (email / nome)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: joao@…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
