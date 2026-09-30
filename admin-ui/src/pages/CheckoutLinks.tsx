// Página "Relatório de Checkouts".
// SOMENTE LEITURA — o admin não cria/edita/exclui links de checkout nem links
// de afiliado por aqui. Para análise, baixa um relatório CSV direto do banco
// (GET /checkout-links/export.csv) e consulta uma visão enxuta das ofertas de
// checkout do produtor (que definem o preço de venda).

import { useEffect, useState } from 'react'
import { getToken } from '../api'
import { safeUrl } from '../utils/safeUrl' // AUDIT-2026-06-21 #13
import { api } from '../api'
import CopyButton from '../components/CopyButton'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import FalkSelect from '../components/FalkSelect'

// Base da API (mesma derivação de api.ts) — necessária aqui porque o download
// CSV não passa pelo helper api() (que sempre faz res.json()).
const API_BASE = import.meta.env.VITE_API_BASE || '/wp-json/senderzz/v1/admin'

// ─── Tipos ───────────────────────────────────────────────────────────────────

// Oferta de checkout do PRODUTOR (senderzz_checkout_links) — define o PREÇO de venda.
// Somente leitura.
type CheckoutOffer = {
  id: number
  producer_id: number
  produtor_nome: string
  post_id: number
  token: string
  tipo: string            // correio / motoboy
  url: string             // link público completo (migrado)
  display_value: number   // preço de venda
  price_label: string     // "R$ 349,00"
  name: string            // descritor da oferta
  produto_nome: string    // nome do produto resolvido (best-effort por name; ver handler)
  slug: string
  affiliate_visible: boolean
  conversoes: number      // pedidos atribuídos a este link
  receita_gerada: number
  created_at: string
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

const fmtBRL = (v: number) =>
  'R$ ' + (v ?? 0).toLocaleString('pt-BR', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })

// ─── Componente ──────────────────────────────────────────────────────────────

export default function CheckoutLinks() {
  // Ofertas de checkout — somente leitura.
  const [offers, setOffers] = useState<CheckoutOffer[]>([])
  const [offersTotal, setOffersTotal] = useState(0)
  const [offersLoading, setOffersLoading] = useState(false)
  const [offerQ, setOfferQ] = useState('')
  const [offerTipo, setOfferTipo] = useState('')
  const [err, setErr] = useState('')

  // Painel de filtros (FilterTopPanel) + drafts (aplicados só ao confirmar).
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftQ, setDraftQ] = useState('')
  const [draftTipo, setDraftTipo] = useState('')

  // Download do relatório CSV.
  const [downloading, setDownloading] = useState(false)

  // ─── Fetch ofertas (somente leitura) ─────────────────────────────────────────

  async function loadOffers() {
    setOffersLoading(true)
    setErr('')
    try {
      const p = new URLSearchParams()
      if (offerQ.trim()) p.set('q', offerQ.trim())
      if (offerTipo) p.set('tipo', offerTipo)
      p.set('limit', '200')
      const r = await api<{ items: CheckoutOffer[]; total: number }>(
        `/checkout-links/offers?${p.toString()}`
      )
      setOffers(r.items ?? [])
      setOffersTotal(r.total ?? 0)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setOffersLoading(false)
    }
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { loadOffers() }, [offerQ, offerTipo])

  // ─── Download relatório CSV ──────────────────────────────────────────────────

  async function baixarRelatorio() {
    setDownloading(true)
    setErr('')
    try {
      const tok = getToken()
      const res = await fetch(`${API_BASE}/checkout-links/export.csv`, {
        headers: tok ? { Authorization: `Bearer ${tok}` } : {},
      })
      if (!res.ok) {
        // Não baixar JSON de erro como se fosse o CSV.
        const body = await res.json().catch(() => ({}))
        throw new Error(body?.error?.message || `Falha ao gerar relatório (HTTP ${res.status}).`)
      }
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `relatorio-checkouts-${new Date().toISOString().slice(0, 10)}.csv`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setDownloading(false)
    }
  }

  // ─── KPIs (das ofertas carregadas) ───────────────────────────────────────────

  const kpiOfTotal = offers.length
  const kpiOfConv = offers.reduce((s, o) => s + (o.conversoes || 0), 0)
  const kpiOfReceita = offers.reduce((s, o) => s + (o.receita_gerada || 0), 0)

  // ─── Filtros (FilterTopPanel + chips) ────────────────────────────────────────
  function openPanel() {
    setDraftQ(offerQ); setDraftTipo(offerTipo)
    setFilterOpen(true)
  }
  function applyFilters() {
    setOfferQ(draftQ); setOfferTipo(draftTipo)
    setFilterOpen(false)
  }
  function clearFilters() {
    setOfferQ(''); setOfferTipo('')
    setDraftQ(''); setDraftTipo('')
    setFilterOpen(false)
  }

  const TIPO_LABEL: Record<string, string> = { correio: 'Correio', motoboy: 'Motoboy' }
  const chips: ActiveChip[] = []
  if (offerQ) chips.push({ key: 'q', label: `Busca: ${offerQ}`, onRemove: () => setOfferQ('') })
  if (offerTipo) chips.push({ key: 'tipo', label: `Tipo: ${TIPO_LABEL[offerTipo] ?? offerTipo}`, onRemove: () => setOfferTipo('') })

  // ─── Render ────────────────────────────────────────────────────────────────

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Relatório de Checkouts</h1>
          <p>
            {`${offers.length} de ${offersTotal} oferta(s) — visão somente leitura. Para análise completa, baixe o relatório CSV.`}
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={chips.length > 0} count={chips.length} onClick={openPanel} />
          <button
            className="szv2-btn szv2-btn-brand"
            onClick={baixarRelatorio}
            disabled={downloading}
          >
            {downloading ? 'Gerando…' : '⬇ Baixar relatório (CSV)'}
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* KPIs das ofertas */}
      <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 12 }}>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Ofertas migradas</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{kpiOfTotal}</span>
          <span className="szv2-kpi-meta">de {offersTotal} no total</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Conversões</span>
          <span className="szv2-kpi-value">{kpiOfConv.toLocaleString('pt-BR')}</span>
          <span className="szv2-kpi-meta">pedidos atribuídos</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Receita gerada</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-success)' }}>{fmtBRL(kpiOfReceita)}</span>
          <span className="szv2-kpi-meta">via pedidos atribuídos</span>
        </div></div>
      </div>

      {offersLoading && offers.length === 0 ? (
        <TableSkeleton rows={6} cols={10} />
      ) : !offersLoading && offers.length === 0 ? (
        <EmptyState
          icon="🏷️"
          title="Nenhuma oferta de checkout encontrada."
          description={chips.length > 0
            ? 'Ajuste ou remova os filtros aplicados.'
            : 'Quando houver ofertas de checkout migradas, elas aparecem aqui.'}
        />
      ) : (
        <div className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
          <table className="szv2-table" style={{ width: '100%' }}>
            <thead>
              <tr>
                <th>ID</th>
                <th>Oferta</th>
                <th>Produto</th>
                <th>Produtor</th>
                <th>Tipo</th>
                <th className="szv2-td-num">Valor</th>
                <th>Link público</th>
                <th className="szv2-td-num">Conversões</th>
                <th className="szv2-td-num">Receita</th>
                <th>Criado em</th>
              </tr>
            </thead>
            <tbody>
              {offers.map(o => (
                <tr key={o.id}>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>#{o.id}</td>
                  <td style={{ maxWidth: 220 }}>
                    <div style={{ fontWeight: 600 }}>{o.name || '—'}</div>
                    {o.token && (
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {o.token.slice(0, 16)}…
                      </div>
                    )}
                  </td>
                  <td style={{ fontSize: 13, maxWidth: 180 }}>
                    {/* Produto resolvido por NOME no backend (best-effort: não há FK
                        oferta→produto; post_id é canal de envio, não produto). O backend
                        devolve produto_nome NÃO-vazio só quando casou um produto cadastrado;
                        vazio = sem match. Nesse caso exibimos o descritor da oferta (sem o
                        sufixo " — Motoboy") em estilo aproximado. */}
                    {o.produto_nome ? (
                      <span style={{ fontWeight: 600 }}>{o.produto_nome}</span>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-muted)' }} title="Sem produto cadastrado correspondente — exibindo o descritor da oferta.">
                        {o.name.replace(/ — Motoboy$/, '') || '—'}
                      </span>
                    )}
                  </td>
                  <td style={{ fontSize: 13 }}>{o.produtor_nome || `#${o.producer_id}`}</td>
                  <td>
                    <span className={`sz-badge ${o.tipo === 'motoboy' ? 'szv2-badge-brand' : 'szv2-badge-info'}`}>
                      {o.tipo || '—'}
                    </span>
                  </td>
                  <td className="szv2-td-num" style={{ fontWeight: 700, color: 'var(--szv2-brand)', fontFamily: 'var(--szv2-font-mono)' }}>
                    {o.price_label || (o.display_value > 0 ? fmtBRL(o.display_value) : '—')}
                  </td>
                  <td style={{ maxWidth: 300 }}>
                    {o.url ? (
                      <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                        <a
                          href={safeUrl(o.url)}
                          target="_blank"
                          rel="noopener noreferrer"
                          style={{ fontSize: 11, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1, textDecoration: 'none' }}
                          title={o.url}
                        >
                          {o.url}
                        </a>
                        <CopyButton text={o.url} variant="icon" />
                      </div>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                    )}
                  </td>
                  <td className="szv2-td-num" style={{ color: 'var(--szv2-brand)', fontWeight: 600 }}>
                    {(o.conversoes || 0).toLocaleString('pt-BR')}
                  </td>
                  <td className="szv2-td-num" style={{ color: 'var(--szv2-success)', fontWeight: 600 }}>
                    {o.receita_gerada > 0 ? fmtBRL(o.receita_gerada) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                  </td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    {o.created_at ? o.created_at.substring(0, 10) : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Busca (oferta / token)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="Nome da oferta ou token…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
        <FilterField label="Tipo">
          <FalkSelect
            value={draftTipo}
            onChange={v => setDraftTipo(v)}
            options={[
              { value: '', label: 'Todos os tipos' },
              { value: 'correio', label: 'Correio' },
              { value: 'motoboy', label: 'Motoboy' },
            ]}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
