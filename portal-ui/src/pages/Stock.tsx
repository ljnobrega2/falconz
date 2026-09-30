// Estoque do produtor — tabela read-only de custódia por CD.
//
// Ligada ao go/portal (leitura de estoque REAL do produtor):
//   GET /portal/producer-stock → { ok, items:[{ product_id, product_name, cd_id,
//                          cd_nome, qty_available, qty_reserved, qty_sellable,
//                          low_stock_threshold }], total }
//
// IMPORTANTE: usar /portal/producer-stock (stock_producer_portal.go), NÃO
// /portal/stock. Aquele último (stock_portal.go) é um espelho placeholder que
// retorna KPIs ZERADOS por design e shape { data:{ kpis, movements, products } }
// — NUNCA carrega estoque real e a página não consegue renderizá-lo. O endpoint
// producer-stock lê a tabela sz_stock real e devolve exatamente o shape abaixo.
//
// A tela é READ-ONLY: lista por (produto × CD) com Disponível, Reservado, Vendável
// e Mínimo. Quando vendável <= mínimo, a linha recebe um badge laranja de "Estoque
// baixo" (var(--szv2-brand)). NUNCA usar verde (#22c55e / var(--szv2-success)) em
// elementos de UI — cor reservada para badges de status externos.
//
// PADRÃO DO APP:
//   - Loading → skeleton de tabela (pulse) no lugar do "Carregando…" textual.
//   - Vazio  → <EmptyState> padronizado.
//   - Erro   → faixa .sz-alert-danger no topo (mesma das demais pages).
//
// CONTRATO: o envelope traz items[] no TOPO (não sob `data`, ao contrário de
// envelopes antigos). normalizeResp() aceita ambos por robustez, mas o caminho
// canônico é r.items.
import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import EmptyState from '../components/EmptyState'
import AlertError from '../components/AlertError'

// Azul da marca FALK LOG (#1E6FF2) — mesma cor que --szv2-brand. Mantido como
// literal para texto+fundo coerentes do badge de estoque (rgba(30,111,242,.10)).
// NUNCA verde, NUNCA laranja.
const SZ_BRAND = '#1E6FF2'
const SZ_BRAND_TINT = 'rgba(30,111,242,.10)'

// ── Shape do endpoint de estoque do produtor ─────────────────────────────────────
type StockRow = {
  product_id: number
  product_name: string
  cd_id: number
  cd_nome: string
  qty_available: number
  qty_reserved: number
  qty_sellable: number
  low_stock_threshold: number
}

type ListResp = {
  ok: boolean
  items: StockRow[]
}

// ── Coerção numérica defensiva ──────────────────────────────────────────────────
// O backend envia inteiros, mas um proxy/shape divergente pode mandar
// null/string/undefined. num() garante número válido — nunca NaN, nunca undefined.
function num(v: unknown): number {
  const n = typeof v === 'number' ? v : Number(v)
  return Number.isFinite(n) ? n : 0
}

// Normalização de busca (lowercase + remove acentos) para o filtro client-side.
function norm(s: string): string {
  return (s || '')
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
}

// normalizeResp — aceita { items } no topo (canônico) ou { data: { items } } /
// { data: [...] } por robustez. Sempre devolve um array (nunca null).
function normalizeResp(r: unknown): StockRow[] {
  if (!r || typeof r !== 'object') return []
  const obj = r as Record<string, unknown>
  if (Array.isArray(obj.items)) return obj.items as StockRow[]
  const data = obj.data as Record<string, unknown> | unknown[] | undefined
  if (Array.isArray(data)) return data as StockRow[]
  if (data && typeof data === 'object' && Array.isArray((data as Record<string, unknown>).items)) {
    return (data as Record<string, unknown>).items as StockRow[]
  }
  return []
}

// ── Skeleton de tabela — padrão do app (pulse), sem dependência externa ──────────
const SKELETON_STYLE = `
@keyframes szStockPulse { 0%,100% { opacity: 1 } 50% { opacity: .45 } }
.sz-stock-skel { animation: szStockPulse 1.2s ease-in-out infinite; }
@media (prefers-reduced-motion: reduce) { .sz-stock-skel { animation: none; opacity: .7 } }
`

function SkeletonBar({ width = '100%', height = 12 }: { width?: number | string; height?: number }) {
  return (
    <span
      className="sz-stock-skel"
      aria-hidden="true"
      style={{
        display: 'inline-block',
        width,
        height,
        borderRadius: 6,
        background: 'var(--szv2-divider)',
      }}
    />
  )
}

function StockSkeleton() {
  const cols = 6
  return (
    <section id="sec-stock" className="sz-sec" aria-busy="true">
      <style>{SKELETON_STYLE}</style>
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Estoque
        </h2>
      </div>
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div style={{ flex: 1 }}>
            <SkeletonBar width={180} height={16} />
            <div style={{ marginTop: 8 }}>
              <SkeletonBar width={240} height={11} />
            </div>
          </div>
          <SkeletonBar width={220} height={34} />
        </div>
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>Produto</th>
                <th>CD</th>
                <th className="szv2-td-num">Disponível</th>
                <th className="szv2-td-num">Reservado</th>
                <th className="szv2-td-num">Vendável</th>
                <th className="szv2-td-num">Mínimo</th>
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 6 }).map((_, r) => (
                <tr key={r}>
                  {Array.from({ length: cols }).map((__, c) => (
                    <td key={c} style={c >= 2 ? { textAlign: 'right' } : undefined}>
                      <SkeletonBar width={c === 0 ? '70%' : c === 1 ? '55%' : 36} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}

export default function Stock() {
  const [rows, setRows] = useState<StockRow[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Busca client-side por produto/CD.
  const [search, setSearch] = useState('')
  // Filtro por CD (chips).
  const [cdFilter, setCdFilter] = useState<number>(0) // 0 = todos

  const load = useCallback(() => {
    let cancelled = false
    setLoading(true)
    setErr('')
    api<ListResp>('/portal/producer-stock')
      .then(r => {
        if (cancelled) return
        setRows(normalizeResp(r))
        setErr('')
      })
      .catch(e => !cancelled && setErr((e && e.message) || 'Erro ao carregar estoque'))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => load(), [load])

  // Lista de CDs presentes nos dados, para os chips de filtro.
  const cds = useMemo(() => {
    const map = new Map<number, string>()
    rows.forEach(row => {
      const id = num(row?.cd_id)
      if (id && !map.has(id)) map.set(id, (row?.cd_nome || '').trim() || `CD #${id}`)
    })
    return Array.from(map.entries())
      .map(([id, nome]) => ({ id, nome }))
      .sort((a, b) => a.nome.localeCompare(b.nome, 'pt-BR', { sensitivity: 'base' }))
  }, [rows])

  // Linhas visíveis após filtro de CD + busca.
  const visibleRows = useMemo(() => {
    const q = norm(search)
    return rows.filter(row => {
      if (cdFilter && num(row?.cd_id) !== cdFilter) return false
      if (!q) return true
      return norm(`${row?.product_name || ''} ${row?.cd_nome || ''}`).indexOf(q) !== -1
    })
  }, [rows, cdFilter, search])

  // Quantos itens estão em estoque baixo (vendável <= mínimo) entre os visíveis.
  const lowCount = useMemo(
    () => visibleRows.filter(row => num(row?.qty_sellable) <= num(row?.low_stock_threshold)).length,
    [visibleRows],
  )

  // ── Loading → skeleton (padrão do app) ────────────────────────────────────────
  if (loading) return <StockSkeleton />

  return (
    <section id="sec-stock" className="sz-sec">
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Estoque
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Disponibilidade dos seus produtos por centro de distribuição. Somente leitura.
        </p>
      </div>

      {!!err && <AlertError message={err} onRetry={load} />}

      {/* ── Chips de filtro por CD ─────────────────────────────────────────────── */}
      {cds.length >= 1 && (
        <div className="szv2-card" style={{ marginBottom: 'var(--szv2-space-4)' }}>
          <div className="szv2-card-head">
            <div>
              <h2>Centro de Distribuição</h2>
              <p className="szv2-card-sub">Selecione o CD para filtrar o estoque.</p>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 'var(--szv2-space-3)', flexWrap: 'wrap' }}>
            <button
              type="button"
              className={`szv2-btn szv2-btn-sm ${cdFilter === 0 ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
              onClick={() => setCdFilter(0)}
            >
              Todos os CDs
            </button>
            {cds.map(cd => (
              <button
                key={cd.id}
                type="button"
                className={`szv2-btn szv2-btn-sm ${cdFilter === cd.id ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                onClick={() => setCdFilter(cd.id)}
              >
                {cd.nome}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* ── Tabela de estoque (read-only) ─────────────────────────────────────── */}
      {rows.length === 0 ? (
        <div className="szv2-card">
          <EmptyState
            icon="📦"
            title="Nenhum produto com estoque"
            description="O estoque por centro de distribuição dos seus produtos aparecerá aqui assim que houver custódia registrada."
          />
        </div>
      ) : (
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div>
              <h2>Estoque por produto e CD</h2>
              <p className="szv2-card-sub">
                {visibleRows.length} item(ns)
                {lowCount > 0 && (
                  <>
                    {' · '}
                    <span style={{ color: SZ_BRAND, fontWeight: 700 }}>
                      {lowCount} com estoque baixo
                    </span>
                  </>
                )}
              </p>
            </div>
            <input
              type="search"
              className="szv2-input"
              placeholder="Buscar produto ou CD…"
              autoComplete="new-password"
              value={search}
              onChange={e => setSearch(e.target.value)}
              style={{ width: 220, fontSize: 12 }}
            />
          </div>

          {visibleRows.length === 0 ? (
            <EmptyState
              title="Nenhum item para este filtro"
              description="Ajuste a busca ou selecione outro centro de distribuição."
            />
          ) : (
            <div className="szv2-table-wrap">
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Produto</th>
                    <th>CD</th>
                    <th className="szv2-td-num">Disponível</th>
                    <th className="szv2-td-num">Reservado</th>
                    <th className="szv2-td-num">Vendável</th>
                    <th className="szv2-td-num">Mínimo</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleRows.map((row, idx) => {
                    const available = num(row?.qty_available)
                    const reserved = num(row?.qty_reserved)
                    const sellable = num(row?.qty_sellable)
                    const threshold = num(row?.low_stock_threshold)
                    const isLow = sellable <= threshold
                    return (
                      <tr key={`${row?.product_id ?? 'p'}-${row?.cd_id ?? 'c'}-${idx}`}>
                        <td className="szv2-td-main">{row?.product_name || '—'}</td>
                        <td className="szv2-td-sub">{row?.cd_nome || '—'}</td>
                        <td className="szv2-td-num szv2-num">{String(available)}</td>
                        <td className="szv2-td-num szv2-td-sub">
                          {reserved > 0 ? String(reserved) : '—'}
                        </td>
                        <td className="szv2-td-num szv2-num" style={{ fontWeight: 700 }}>
                          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, justifyContent: 'flex-end' }}>
                            {String(sellable)}
                            {isLow && (
                              <span
                                title="Estoque vendável igual ou abaixo do mínimo"
                                style={{
                                  display: 'inline-flex',
                                  alignItems: 'center',
                                  padding: '2px 8px',
                                  borderRadius: 99,
                                  fontSize: 10,
                                  fontWeight: 700,
                                  lineHeight: 1.4,
                                  color: SZ_BRAND,
                                  background: SZ_BRAND_TINT,
                                  whiteSpace: 'nowrap',
                                }}
                              >
                                Estoque baixo
                              </span>
                            )}
                          </span>
                        </td>
                        <td className="szv2-td-num szv2-td-sub">{String(threshold)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}

          <div
            style={{
              padding: '8px 16px',
              borderTop: '1px solid var(--szv2-divider)',
              fontSize: 12,
              color: 'var(--szv2-text-muted)',
            }}
          >
            <span>{visibleRows.length} item(ns) em estoque</span>
          </div>
        </div>
      )}
    </section>
  )
}
