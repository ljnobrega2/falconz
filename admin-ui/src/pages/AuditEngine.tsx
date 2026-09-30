import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { confirmAsync } from '../components/ConfirmDialog'
import { useToast } from '../hooks/useToast'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'

type Counts = {
  split: number
  aff_bad: number
  aff_missing: number
  wallet: number
  total: number
}

type Problem = {
  order_id: number
  type_key: 'split' | 'aff_bad' | 'aff_missing' | 'wallet'
  type_label: string
  expected: number
  actual: number
  affiliate_id?: number | null
  producer_id?: number | null
}

type FixAllResult = {
  ok: boolean
  result: {
    missing_inserted: number
    bad_transactions_updated: number
    producer_wallet_updated: number
    wallet_summary_synced: number
  }
}

type TypeFilter = '' | 'split' | 'aff_bad' | 'aff_missing' | 'wallet'

const TYPE_LABELS: Record<TypeFilter, string> = {
  '':            'Todos',
  split:         'Total divergente',
  aff_bad:       'Comissão afiliado divergente',
  aff_missing:   'Afiliado sem transação',
  wallet:        'Produtor COD divergente',
}

function KpiCard({ label, value, sub, danger }: { label: string; value: number | string; sub?: string; danger?: boolean }) {
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span
          className="szv2-kpi-value"
          style={danger && Number(value) > 0
            ? { color: 'var(--szv2-danger)' }
            : { color: 'var(--szv2-brand)' }}
        >
          {value}
        </span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

const fmt = (v: number) => v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

export default function AuditEngine() {
  const [counts, setCounts] = useState<Counts | null>(null)
  const [problems, setProblems] = useState<Problem[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const showToast = useToast() // AUDIT-2026-06-18 Onda3

  // Drill-down: a Dashboard deep-linka ?audit_type=split (pré-seleciona o filtro
  // de divergência). Param distinto de ?tab= (que o SistemaHub usa).
  const [searchParams] = useSearchParams()
  const initialType = ((): TypeFilter => {
    const t = searchParams.get('audit_type')
    return (t && t in TYPE_LABELS) ? (t as TypeFilter) : ''
  })()

  // Filtros aplicados.
  const [typeFilter, setTypeFilter] = useState<TypeFilter>(initialType)
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftType, setDraftType] = useState<TypeFilter>('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const c = await api<Counts>('/audit/counts')
      setCounts(c)
      const p = new URLSearchParams()
      if (typeFilter) p.set('type', typeFilter)
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      p.set('limit', '200')
      const r = await api<{ items: Problem[]; count: number }>(`/audit/problems?${p.toString()}`)
      setProblems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [typeFilter, dataIni, dataFim])

  function openPanel() {
    setDraftType(typeFilter); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setTypeFilter(draftType); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setTypeFilter(''); setDataIni(''); setDataFim('')
    setDraftType(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }


  async function handleFixAll() {
    if (!await confirmAsync({ message: 'Corrigir TODAS as divergências em batch?\n\nEssa operação:\n- Insere comissões ausentes\n- Atualiza valores divergentes\n- Sincroniza carteiras\n\nContinuar?' })) return
    setBusy(true)
    try {
      const r = await api<FixAllResult>('/audit/fix-all', { method: 'POST' })
      const x = r.result
      showToast('ok',
        `Correção concluída: ${x.missing_inserted} comissões inseridas, ${x.bad_transactions_updated} transações atualizadas, ${x.producer_wallet_updated} carteiras corrigidas, ${x.wallet_summary_synced} resumos sincronizados.`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao corrigir')
    } finally {
      setBusy(false)
    }
  }

  async function handleFixOrder(orderID: number) {
    if (!await confirmAsync({ message: `Corrigir pedido ${orderID}?` })) return
    setBusy(true)
    try {
      await api(`/audit/fix-order/${orderID}`, { method: 'POST' })
      showToast('ok', `Pedido ${orderID} corrigido`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha')
    } finally {
      setBusy(false)
    }
  }

  async function handleFixAffiliateWallet(affiliateID: number) {
    if (!await confirmAsync({ message: `Sincronizar carteira do afiliado #${affiliateID}?\n\nRecalcula balance e pending_balance a partir das transações.` })) return
    setBusy(true)
    try {
      await api(`/affiliates/${affiliateID}/wallet-fix`, { method: 'POST' })
      showToast('ok', `Carteira do afiliado #${affiliateID} sincronizada`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao sincronizar carteira')
    } finally {
      setBusy(false)
    }
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (typeFilter) chips.push({ key: 'type', label: `Tipo: ${TYPE_LABELS[typeFilter]}`, onRemove: () => setTypeFilter('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      <div className="szv2-section-head">
        <div>
          <h1>Auditoria financeira</h1>
          <p>Divergências entre pedidos, comissões e carteiras.</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {/* KPIs */}
      {counts && (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))' }}>
          <KpiCard label="Divergências totais" value={counts.total} sub="itens para revisão" danger />
          <KpiCard label="Total divergente"    value={counts.split}      sub="valores fora do esperado" danger />
          <KpiCard label="Repasse pendente"    value={counts.aff_missing} sub="comissão sem transação" danger />
          <KpiCard label="Carteira divergente" value={counts.aff_bad + counts.wallet} sub="carteira precisa revisão" danger />
        </div>
      )}

      {/* Ação batch */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Correção em lote</h2>
            <p className="szv2-card-sub">
              Corrige apenas registros com divergência objetiva. Não altera a regra financeira do pedido.
            </p>
          </div>
          <button
            type="button"
            className="szv2-btn-brand"
            onClick={handleFixAll}
            disabled={busy || !counts || counts.total === 0}
          >
            {busy ? 'Corrigindo…' : 'Corrigir tudo'}
          </button>
        </div>
      </div>

      {/* Lista */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Pedidos com divergência</h2>
            <p className="szv2-card-sub">{problems.length} registro(s)</p>
          </div>
        </div>

        {loading && problems.length === 0 ? (
          <TableSkeleton rows={5} cols={8} />
        ) : !loading && problems.length === 0 ? (
          <EmptyState
            icon="✅"
            title="Sem pedidos com divergência."
            description="Quando houver divergências entre carteiras e pedidos, elas aparecem aqui."
          />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Pedido</th>
                  <th>Tipo</th>
                  <th>Afiliado</th>
                  <th>Produtor</th>
                  <th style={{ textAlign: 'right' }}>Esperado</th>
                  <th style={{ textAlign: 'right' }}>Atual</th>
                  <th style={{ textAlign: 'right' }}>Diferença</th>
                  <th style={{ width: 140 }}>Ação</th>
                </tr>
              </thead>
              <tbody>
                {problems.map(p => {
                  const diff = p.actual - p.expected
                  return (
                    <tr key={`${p.type_key}-${p.order_id}`}>
                      <td><strong>{p.order_id}</strong></td>
                      <td>
                        <span className="sz-badge szv2-badge-danger">{p.type_label}</span>
                      </td>
                      <td>{p.affiliate_id ?? '—'}</td>
                      <td>{p.producer_id ?? '—'}</td>
                      <td style={{ textAlign: 'right' }}>R$ {fmt(p.expected)}</td>
                      <td style={{ textAlign: 'right' }}>R$ {fmt(p.actual)}</td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-danger)' }}>
                        R$ {fmt(Math.abs(diff))}
                      </td>
                      <td style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                        <button
                          type="button"
                          className="szv2-btn-secondary"
                          onClick={() => handleFixOrder(p.order_id)}
                          disabled={busy}
                        >
                          Corrigir
                        </button>
                        {p.affiliate_id && (
                          <button
                            type="button"
                            className="szv2-btn-secondary"
                            onClick={() => handleFixAffiliateWallet(p.affiliate_id!)}
                            disabled={busy}
                            title={`Sincronizar carteira do afiliado #${p.affiliate_id}`}
                          >
                            Sincronizar carteira
                          </button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftIni}
            max={draftFim || undefined}
            onChange={v => setDraftIni(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftFim}
            min={draftIni || undefined}
            onChange={v => setDraftFim(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Tipo">
          <FalkSelect
            aria-label="Tipo"
            value={draftType}
            onChange={v => setDraftType(v as TypeFilter)}
            options={(Object.keys(TYPE_LABELS) as TypeFilter[]).map(k => ({
              value: k,
              label: TYPE_LABELS[k],
            }))}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
