// Tela "Faturamento FALKZ" — receita consolidada da plataforma.
// Contrato (backend será ligado depois — trata 404/erro graciosamente):
//   GET /revenue/summary → { total, meta_ano, pendente?, pendente_pedidos?, by_component:[{component,lancamentos,receita}], by_month:[{mes,receita}] }
//   GET /revenue?from=&to=&produtor= → lista de lançamentos
// Estilo szv2 espelhado de Commissions.tsx / Dashboard.tsx. Brand: var(--szv2-brand).
import { useEffect, useState } from 'react'
import { api } from '../api'
import { brDate } from '../utils/format'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import CardKpiSkeleton from '../components/CardKpiSkeleton'
import FalkDatePicker from '../components/FalkDatePicker'
import FalkSelect from '../components/FalkSelect'

type ByComponent = { component: string; lancamentos: number; receita: number }
type ByMonth = { mes: string; receita: number }

type Summary = {
  total: number
  meta_ano: number
  by_component: ByComponent[]
  by_month: ByMonth[]
  // Previsão de receita de pedidos em andamento (ainda não realizada).
  pendente?: number
  pendente_pedidos?: number
}

// Espelha revenue.go List → lanc struct (backend já envia order_id/base_amount/
// ref — só não estava exposto no front, daí "sem referência de pedido/clareza").
type Lancamento = {
  id: number | string
  data?: string
  created_at?: string // backend envia created_at
  order_id?: number | null
  component: string
  produtor: string
  base_amount?: number
  receita: number
  ref?: string
}

const META_DEFAULT = 1_000_000

// Produtor para o filtro select (regra do dono: filtros de texto viram SELECT).
// Fonte: GET /producers → { items: [{ user_id, nome, email }] }. O backend casa
// o filtro `produtor` por ILIKE no nome, então value=nome funciona (casa exato).
type ProdutorOption = { user_id: number; nome: string; email?: string }

// Rótulos amigáveis dos componentes de receita conhecidos.
const COMPONENT_LABELS: Record<string, string> = {
  taxa_afiliado_4_99: 'Taxa de Afiliado (4,99%)',
  taxa_transacao_produtor: 'Taxa de Transação (Produtor)',
  taxa_entrega: 'Taxa de Entrega',
  taxa_frustrado: 'Taxa de Frustrado',
  taxa_frustrado_produtor: 'Taxa de Frustrado (Produtor)',
  taxa_saque: 'Taxa de Saque',
  taxa_antecipacao: 'Taxa de Antecipação',
}

function componentLabel(c: string): string {
  return COMPONENT_LABELS[c] || c
}

// R$ 1.234,56 — formato brasileiro.
function brl(v: number): string {
  return 'R$ ' + Number(v || 0).toLocaleString('pt-BR', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

export default function Faturamento() {
  // --- Resumo ---
  const [summary, setSummary] = useState<Summary | null>(null)
  const [summaryLoading, setSummaryLoading] = useState(true)
  const [summaryPending, setSummaryPending] = useState(false) // endpoint 404/erro → aviso suave

  // --- Lançamentos ---
  const [items, setItems] = useState<Lancamento[]>([])
  const [itemsLoading, setItemsLoading] = useState(true)
  const [itemsPending, setItemsPending] = useState(false)

  // --- Filtros aplicados ---
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [produtor, setProdutor] = useState('')

  // --- Painel de filtros + drafts (aplicados só ao confirmar) ---
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [draftFrom, setDraftFrom] = useState('')
  const [draftTo, setDraftTo] = useState('')
  const [draftProdutor, setDraftProdutor] = useState('')

  // Opções do filtro "Produtor" (carregadas no mount via GET /producers).
  const [produtores, setProdutores] = useState<ProdutorOption[]>([])
  useEffect(() => {
    api<{ items?: ProdutorOption[] }>('/producers?limit=300')
      .then(r => setProdutores(r.items ?? []))
      .catch(() => setProdutores([]))
  }, [])

  // Options do FalkSelect — 1ª opção vazia = sem filtro. value=nome (ILIKE no backend).
  const produtorOptions = [
    { value: '', label: 'Todos' },
    ...produtores.map(p => ({ value: p.nome, label: p.nome })),
  ]

  // MED43 — Carrega resumo respeitando os filtros (período + produtor),
  // recarregando sempre que eles mudam (mesma querystring da lista).
  useEffect(() => {
    const p = new URLSearchParams()
    if (from) p.set('from', from)
    if (to) p.set('to', to)
    if (produtor.trim()) p.set('produtor', produtor.trim())
    const qs = p.toString()
    setSummaryLoading(true)
    api<Summary>(`/revenue/summary${qs ? `?${qs}` : ''}`)
      .then(r => { setSummary(r); setSummaryPending(false) })
      .catch(() => { setSummary(null); setSummaryPending(true) })
      .finally(() => setSummaryLoading(false))
  }, [from, to, produtor])

  // Carrega lançamentos sempre que filtros mudam.
  useEffect(() => {
    const p = new URLSearchParams()
    if (from) p.set('from', from)
    if (to) p.set('to', to)
    if (produtor.trim()) p.set('produtor', produtor.trim())
    const qs = p.toString()
    setItemsLoading(true)
    api<{ items?: Lancamento[] } | Lancamento[]>(`/revenue${qs ? `?${qs}` : ''}`)
      .then(r => {
        // Aceita tanto { items: [...] } quanto array direto.
        const list = Array.isArray(r) ? r : (r.items ?? [])
        setItems(list)
        setItemsPending(false)
      })
      .catch(() => { setItems([]); setItemsPending(true) })
      .finally(() => setItemsLoading(false))
  }, [from, to, produtor])

  // ── Helpers do painel de filtros ───────────────────────────────────
  function openFilters() {
    setDraftFrom(from); setDraftTo(to); setDraftProdutor(produtor)
    setFiltersOpen(true)
  }
  function applyFilters() {
    setFrom(draftFrom); setTo(draftTo); setProdutor(draftProdutor)
    setFiltersOpen(false)
  }
  function clearFilters() {
    setFrom(''); setTo(''); setProdutor('')
    setDraftFrom(''); setDraftTo(''); setDraftProdutor('')
    setFiltersOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (from) chips.push({ key: 'from', label: `De: ${from}`, onRemove: () => setFrom('') })
  if (to) chips.push({ key: 'to', label: `Até: ${to}`, onRemove: () => setTo('') })
  if (produtor) chips.push({ key: 'produtor', label: `Produtor: ${produtor}`, onRemove: () => setProdutor('') })

  // Métricas derivadas (defensivas — endpoint pode estar pendente).
  const total = summary?.total ?? 0
  const meta = summary?.meta_ano && summary.meta_ano > 0 ? summary.meta_ano : META_DEFAULT
  const pct = meta > 0 ? Math.min(100, (total / meta) * 100) : 0
  // Previsão: receita estimada de pedidos em andamento (não realizada).
  const pendente = summary?.pendente ?? 0
  const pendentePedidos = summary?.pendente_pedidos ?? 0
  const byComponent = summary?.by_component ?? []

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Faturamento FALKZ</h1>
          <p>Receita consolidada da plataforma · meta {brl(meta)} / ano</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={chips.length > 0} count={chips.length} onClick={openFilters} />
        </div>
      </div>

      {/* Aviso de endpoint pendente (esperado enquanto backend não liga). */}
      {!!summaryPending && (
        <div
          className="sz-alert-warning"
          style={{
            marginBottom: 16,
            padding: '12px 16px',
            borderRadius: 10,
            background: 'rgba(234,88,12,.08)',
            border: '1px solid var(--szv2-brand)',
            color: 'var(--szv2-text)',
            fontSize: 13,
          }}
        >
          <strong style={{ color: 'var(--szv2-brand)' }}>Endpoint pendente</strong> — o backend de
          faturamento (<code>/revenue/summary</code>) será conectado em breve. Exibindo valores zerados.
        </div>
      )}

      {/* ── KPIs grandes: Total Faturamento (realizado) + Pendente (previsão) ── */}
      {summaryLoading ? (
        <CardKpiSkeleton count={2} />
      ) : (
        <div
          className="szv2-kpi-grid"
          style={{ gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 1fr)', marginBottom: 24 }}
        >
          {/* Card 1: Total Faturamento (realizado) + progresso vs meta */}
          <div className="szv2-card">
            <div className="szv2-kpi" style={{ marginBottom: 16 }}>
              <span className="szv2-kpi-label">Total Faturamento</span>
              <span
                className="szv2-kpi-value"
                style={{ color: 'var(--szv2-brand)', fontSize: 40, fontWeight: 800, lineHeight: 1.1 }}
              >
                {brl(total)}
              </span>
              <span className="szv2-kpi-meta">
                {pct.toFixed(1)}% da meta anual de {brl(meta)}
              </span>
            </div>

            {/* Barra de progresso vs meta */}
            <div
              style={{
                position: 'relative',
                height: 14,
                borderRadius: 999,
                background: 'var(--szv2-surface-alt)',
                overflow: 'hidden',
                border: '1px solid var(--szv2-border)',
              }}
              role="progressbar"
              aria-valuenow={Math.round(pct)}
              aria-valuemin={0}
              aria-valuemax={100}
            >
              <div
                style={{
                  position: 'absolute',
                  inset: 0,
                  width: `${pct}%`,
                  borderRadius: 999,
                  background: 'var(--szv2-brand)',
                  transition: 'width .4s ease',
                }}
              />
            </div>
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                marginTop: 6,
                fontSize: 12,
                color: 'var(--szv2-text-muted)',
              }}
            >
              <span>R$ 0</span>
              <span style={{ fontWeight: 600, color: 'var(--szv2-brand)' }}>{pct.toFixed(1)}%</span>
              <span>{brl(meta)}</span>
            </div>
          </div>

          {/* Card 2: Pendente (previsão) — receita estimada, ainda não realizada.
              Borda/acento na cor da marca (var(--szv2-brand)) para amarrar à marca,
              mas valor em cor NEUTRA (não brand) para não confundir com o realizado. */}
          <div
            className="szv2-card"
            style={{
              borderLeft: '3px solid var(--szv2-brand)',
              background: 'rgba(30,111,242,.04)',
            }}
          >
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Pendente (previsão)</span>
              <span
                className="szv2-kpi-value"
                style={{ color: 'var(--szv2-text)', fontSize: 32, fontWeight: 800, lineHeight: 1.1 }}
              >
                {brl(pendente)}
              </span>
              <span className="szv2-kpi-meta">
                {Number(pendentePedidos || 0).toLocaleString('pt-BR')} pedido(s) em andamento
              </span>
              <span
                style={{
                  marginTop: 8,
                  fontSize: 11,
                  lineHeight: 1.4,
                  color: 'var(--szv2-text-muted)',
                }}
              >
                previsão de pedidos em andamento (agendamento/embalado/em rota)
              </span>
            </div>
          </div>
        </div>
      )}

      {/* ── Cards por componente de receita ──────────────────────────── */}
      <div className="szv2-card-head" style={{ marginBottom: 8 }}>
        <h2>Receita por componente</h2>
      </div>
      {summaryLoading ? (
        <CardKpiSkeleton count={2} />
      ) : !!byComponent.length ? (
        <div
          style={{
            display: 'flex',
            flexWrap: 'nowrap',
            overflowX: 'auto',
            gap: 12,
            marginBottom: 24,
          }}
        >
          {byComponent.map(c => (
            <div className="szv2-card" style={{ flex: '1 0 200px', minWidth: 200 }} key={c.component}>
              <div className="szv2-kpi">
                <span className="szv2-kpi-label">{componentLabel(c.component)}</span>
                <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>
                  {brl(c.receita)}
                </span>
                <span className="szv2-kpi-meta">
                  {Number(c.lancamentos || 0).toLocaleString('pt-BR')} lançamento(s)
                </span>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div style={{ marginBottom: 24 }}>
          <EmptyState
            icon="📊"
            title="Sem componentes de receita."
            description="Quando houver receita registrada por componente, os cards aparecem aqui."
          />
        </div>
      )}

      {/* ── Tabela de lançamentos ────────────────────────────────────── */}
      <div className="szv2-card-head" style={{ marginBottom: 8 }}>
        <h2>Lançamentos</h2>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {!!itemsPending && (
        <div
          className="sz-alert-warning"
          style={{
            marginBottom: 12,
            padding: '12px 16px',
            borderRadius: 10,
            background: 'rgba(234,88,12,.08)',
            border: '1px solid var(--szv2-brand)',
            color: 'var(--szv2-text)',
            fontSize: 13,
          }}
        >
          <strong style={{ color: 'var(--szv2-brand)' }}>Endpoint pendente</strong> — a lista de
          lançamentos (<code>/revenue</code>) será conectada em breve.
        </div>
      )}

      {itemsLoading && items.length === 0 ? (
        <TableSkeleton rows={6} cols={6} />
      ) : !itemsLoading && items.length === 0 ? (
        <EmptyState
          icon="🧾"
          title="Sem lançamentos no período."
          description="Quando houver receita lançada, os registros aparecem aqui."
        />
      ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>Data</th>
                <th>Pedido</th>
                <th>Componente</th>
                <th>Nome</th>
                <th className="szv2-td-num">Base</th>
                <th className="szv2-td-num">Receita</th>
              </tr>
            </thead>
            <tbody>
              {items.map((l, i) => (
                <tr key={l.id ?? i}>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    {(l.created_at || l.data) ? brDate(String(l.created_at || l.data)) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                  </td>
                  <td style={{ fontSize: 13 }}>
                    {l.order_id ? (
                      <a href={`/orders/${l.order_id}`} style={{ color: 'var(--szv2-brand)', fontWeight: 600 }}>
                        #{l.order_id}
                      </a>
                    ) : (
                      <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }} title={l.ref || ''}>
                        {l.ref || '—'}
                      </span>
                    )}
                  </td>
                  <td style={{ fontSize: 13 }}>
                    {l.component ? componentLabel(l.component) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                  </td>
                  <td style={{ fontSize: 13, fontWeight: 600 }}>
                    {l.produtor || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                  </td>
                  <td className="szv2-td-num" style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                    {l.base_amount !== undefined ? brl(l.base_amount) : '—'}
                  </td>
                  <td
                    className="szv2-td-num"
                    style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)', fontWeight: 700 }}
                  >
                    {brl(l.receita)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* ── Painel de filtros (período + produtor) ───────────────────── */}
      <FilterTopPanel
        open={filtersOpen}
        onClose={() => setFiltersOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros — Faturamento"
      >
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftFrom}
            onChange={v => setDraftFrom(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftTo}
            onChange={v => setDraftTo(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Produtor">
          <FalkSelect
            value={draftProdutor}
            onChange={v => setDraftProdutor(v)}
            options={produtorOptions}
            placeholder="Todos"
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
