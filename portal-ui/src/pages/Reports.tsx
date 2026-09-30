// Relatórios — ligada ao go/portal (GET /portal/reports). Port fiel de
// templates/portal/v2/sections/reports.php.
//
// Tela READ-ONLY. As métricas (COD/motoboy + Expedição) já vêm agregadas e
// recortadas pela sessão no backend (reports_portal.go espelha sz9rp_metrics).
//
// Janela de datas (paridade c/ WP):
//   - carregamento inicial → NENHUM from/to (set recente completo, cap 500);
//   - clique "Filtrar"     → re-fetch com ?from&to (o Go realmente recorta).
// (No WP o szV2RpFilter é só cosmético; aqui o backend filtra de verdade.)
//
// Afiliado: has_exp=false e exp ausente → sem aba Expedição (espelha $sz8rp_has_exp).
// Exportar CSV é gerado client-side a partir dos agregados (sem rota de mutação;
// o handler documenta CSV como client-side). Botão oculto p/ afiliado (WP: !is_aff).
//
// by_region vem sempre vazio por design do backend (region='' — paridade com WP,
// onde format_order não popula $o['region']); a seção é guardada por length > 0.
import { useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import EmptyState from '../components/EmptyState'
import InlineLoading from '../components/InlineLoading'
import AlertError from '../components/AlertError'
import StatusBadge from '../components/StatusBadge'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import { brl, csvSafe } from '../utils/format'

type StatusRow = { status: string; count: number; pct: number }
type ProductRow = { name: string; qty: number; revenue: number }
type RegionRow = { region: string; count: number; pct: number }

type Metrics = {
  total: number
  receita: number
  cancelados: number
  ticket: number
  by_status: StatusRow[]
  by_product: ProductRow[]
  by_region: RegionRow[]
}

type ReportsResp = {
  ok: boolean
  cod: Metrics
  exp?: Metrics
  has_exp: boolean
  total: number
  from: string
  to: string
  role: string
  is_affiliate: boolean
}

type Variant = 'brand' | 'info' | 'success' | 'danger' | 'neutral' | 'warning'

// $sz8rp_st_labels — label + variante por status. Status cru vem do backend; a
// cor/rótulo é resolvida aqui (igual ao WP). Fallback: [ucfirst(status), 'neutral'].
const ST_LABELS: Record<string, [string, Variant]> = {
  agendado: ['Agendado', 'brand'],
  embalado: ['Embalado', 'info'],
  acaminho: ['Em rota', 'info'],
  entregue: ['Entregue', 'success'],
  frustrado: ['Frustrado', 'danger'],
  cancelado: ['Cancelado', 'neutral'],
  cancelled: ['Cancelado', 'neutral'],
  aprovado: ['Aprovado', 'success'],
  enviado: ['Enviado', 'info'],
  extravio: ['Extravio', 'danger'],
  devolvido: ['Devolvido', 'warning'],
}

function statusLabel(st: string): [string, Variant] {
  return ST_LABELS[st] ?? [st ? st.charAt(0).toUpperCase() + st.slice(1) : st, 'neutral']
}

// Datas-default (últimos 30 dias) — só valores iniciais dos inputs; aplicadas no
// clique "Filtrar" (espelha $sz8rp_to / $sz8rp_from).
// Usa data LOCAL (não toISOString/UTC) para casar com current_time('Y-m-d') do WP
// e com o pin America/Sao_Paulo de format.ts (evita off-by-one após ~21h BRT).
function defaultWindow(): { from: string; to: string } {
  const fmt = (d: Date) =>
    `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - 30)
  return { from: fmt(from), to: fmt(to) }
}

// num — coerção defensiva: qualquer valor do backend (null/undefined/string) vira
// número finito. Evita NaN e null-deref nos KPIs / barras / CSV.
function num(v: unknown): number {
  const n = typeof v === 'string' ? parseFloat(v) : (v as number)
  return Number.isFinite(n) ? Number(n) : 0
}

// normalizeMetrics — blinda QUALQUER shape de métricas vindo do backend (null,
// objeto parcial sem os arrays, numéricos undefined) num Metrics 100% válido.
// O Go (reports_portal.go) já devolve by_* como [], mas um deploy antigo / conta
// nova / 200-vazio pode mandar cod=null ou sem os campos — sem isto o ReportPanel
// faria .map() sobre undefined e a tela ficaria BRANCA (ErrorBoundary). Resiliência:
// TODO acesso a array passa por Array.isArray; todo acesso a row é coagido.
function normalizeMetrics(raw: Partial<Metrics> | null | undefined): Metrics {
  const m = raw ?? {}
  return {
    total: num(m.total),
    receita: num(m.receita),
    cancelados: num(m.cancelados),
    ticket: num(m.ticket),
    by_status: Array.isArray(m.by_status)
      ? m.by_status.map(s => ({
          status: String(s?.status ?? ''),
          count: num(s?.count),
          pct: num(s?.pct),
        }))
      : [],
    by_product: Array.isArray(m.by_product)
      ? m.by_product.map(p => ({
          name: String(p?.name ?? ''),
          qty: num(p?.qty),
          revenue: num(p?.revenue),
        }))
      : [],
    by_region: Array.isArray(m.by_region)
      ? m.by_region.map(r => ({
          region: String(r?.region ?? ''),
          count: num(r?.count),
          pct: num(r?.pct),
        }))
      : [],
  }
}

// CSV client-side a partir dos agregados do grupo (não é o CSV por-pedido do WP —
// não há dados por-pedido nesta rota; consistente com o design client-side).
function csvField(v: string | number): string {
  // AUDIT-2026-06-21 #15: csvSafe neutraliza fórmula (= + - @ TAB CR) ANTES do quote-escape.
  const s = csvSafe(v)
  return /[",\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

function buildCsv(m: Metrics): string {
  // m já vem de normalizeMetrics (arrays garantidos), mas mantemos ?? [] por defesa.
  const byStatus = Array.isArray(m.by_status) ? m.by_status : []
  const byProduct = Array.isArray(m.by_product) ? m.by_product : []
  const byRegion = Array.isArray(m.by_region) ? m.by_region : []
  const lines: string[] = []
  lines.push('Indicador;Valor')
  lines.push(`Total de pedidos;${num(m.total)}`)
  lines.push(`Faturamento total;${num(m.receita).toFixed(2)}`)
  lines.push(`Cancelados;${num(m.cancelados)}`)
  lines.push(`Ticket medio;${num(m.ticket).toFixed(2)}`)
  lines.push('')
  lines.push('Status;Pedidos;%')
  byStatus.forEach(s => lines.push(`${csvField(statusLabel(s.status)[0])};${num(s.count)};${num(s.pct)}`))
  lines.push('')
  lines.push('Produto;Pedidos;Faturamento')
  byProduct.forEach(p => lines.push(`${csvField(p.name)};${num(p.qty)};${num(p.revenue).toFixed(2)}`))
  if (byRegion.length) {
    lines.push('')
    lines.push('Regiao;Pedidos;%')
    byRegion.forEach(r => lines.push(`${csvField(r.region)};${num(r.count)};${num(r.pct)}`))
  }
  return lines.join('\n')
}

export default function Reports() {
  const toast = useToast()
  const [data, setData] = useState<ReportsResp | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Aba ativa (COD × Expedição) + filtros (espelha os inputs do WP).
  const [tab, setTab] = useState<'cod' | 'exp'>('cod')
  const dw = defaultWindow()
  const [from, setFrom] = useState(dw.from)
  const [to, setTo] = useState(dw.to)
  const [status, setStatus] = useState('')
  const [note, setNote] = useState('')

  // load(window?) — sem janela no mount (set recente completo); com janela no
  // clique "Filtrar". Paridade com o handler (só recorta quando from E to vão juntos).
  function load(window?: { from: string; to: string }) {
    setLoading(true)
    const qs = window ? `?from=${encodeURIComponent(window.from)}&to=${encodeURIComponent(window.to)}` : ''
    api<ReportsResp>(`/portal/reports${qs}`)
      .then(r => {
        setData(r)
        // Afiliado não tem aba Expedição → força COD.
        if (!r.has_exp) setTab('cod')
        setErr('')
      })
      .catch(e => setErr(e.message || 'Erro ao carregar relatórios'))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  function onFilter() {
    setNote(`Filtro aplicado: ${from || '—'} a ${to || '—'}.`)
    load({ from, to })
  }

  function onExport() {
    if (!data) return
    const isCod = tab === 'cod'
    // normalizeMetrics garante shape válido mesmo se cod/exp vier null/parcial.
    const m = normalizeMetrics(isCod ? data.cod : data.exp)
    if (m.total === 0) return
    const label = isCod ? 'COD' : 'Expedicao'
    const mode = isCod ? 'motoboy' : 'expedicao'
    const csv = buildCsv(m)
    const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `falk-${mode}-${from || 'all'}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    toast('ok', `Relatório ${label} exportado.`)
  }

  const isAff = !!data?.is_affiliate
  const hasExp = !!data?.has_exp
  // normalizeMetrics blinda contra cod/exp null, parcial ou com numéricos undefined
  // (200-vazio / conta nova). Nunca chega array undefined no ReportPanel → nunca branco.
  const activeMetrics: Metrics = normalizeMetrics(tab === 'cod' ? data?.cod : data?.exp)

  return (
    <section id="sec-reports" className="sz-sec" aria-busy={loading || undefined}>
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Relatórios
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>Acompanhe pedidos, faturamento e status por período.</p>
      </div>

      {err && <AlertError message={err} onRetry={() => load()} />}

      {/* Filtros + seletor de modalidade + exportar */}
      <div className="szv2-card szv2-report-filter-bar">
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--szv2-space-3)', flexWrap: 'wrap' }}>
          {hasExp && (
            <div
              style={{
                display: 'flex',
                gap: 0,
                background: 'var(--szv2-surface-alt)',
                border: '1px solid var(--szv2-border)',
                borderRadius: 'var(--szv2-radius-md)',
                padding: 3,
              }}
            >
              <button
                type="button"
                className={`szv2-btn szv2-btn-sm ${tab === 'cod' ? 'szv2-btn-brand' : 'szv2-btn-ghost'}`}
                style={{
                  borderRadius: 'calc(var(--szv2-radius-md) - 3px)',
                  minWidth: 130,
                  ...(tab === 'cod' ? {} : { border: 'none', background: 'transparent' }),
                }}
                onClick={() => setTab('cod')}
              >
                Motoboy / COD
              </button>
              <button
                type="button"
                className={`szv2-btn szv2-btn-sm ${tab === 'exp' ? 'szv2-btn-brand' : 'szv2-btn-ghost'}`}
                style={{
                  borderRadius: 'calc(var(--szv2-radius-md) - 3px)',
                  minWidth: 130,
                  ...(tab === 'exp' ? {} : { border: 'none', background: 'transparent' }),
                }}
                onClick={() => setTab('exp')}
              >
                Expedição
              </button>
            </div>
          )}
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <label className="szv2-label" style={{ whiteSpace: 'nowrap', margin: 0 }}>De</label>
            <FalkDatePicker value={from} onChange={v => setFrom(v)} aria-label="Data inicial" style={{ width: 150 }} />
            <label className="szv2-label" style={{ whiteSpace: 'nowrap', margin: 0 }}>Até</label>
            <FalkDatePicker value={to} onChange={v => setTo(v)} aria-label="Data final" style={{ width: 150 }} />
          </div>
          {/* Filtro de status: parit cosmética com o WP. No WP só alimentava o
              export_csv server-side (agora client-side por agregado); o GET
              /portal/reports não recebe status. Mantido para UX idêntica. */}
          <FalkSelect
            value={status}
            onChange={v => setStatus(v)}
            placeholder="Todos os status"
            style={{ minWidth: 160 }}
            options={[
              { value: '', label: 'Todos os status' },
              { value: 'entregue', label: 'Entregue' },
              { value: 'frustrado', label: 'Frustrado' },
              { value: 'cancelado', label: 'Cancelado' },
              { value: 'acaminho', label: 'Em rota' },
            ]}
          />
          <button type="button" className="szv2-btn szv2-btn-brand szv2-btn-sm" onClick={onFilter} disabled={loading}>
            Filtrar
          </button>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          {!isAff && (
            <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={onExport} disabled={loading || !data}>
              ↓ Exportar
            </button>
          )}
        </div>
      </div>

      {/* Painel ativo (COD ou Expedição) */}
      {loading ? (
        <div className="szv2-conn-panel" aria-busy="true">
          <InlineLoading label="Carregando relatório…" />
        </div>
      ) : (
        <div className="szv2-conn-panel">
          <ReportPanel metrics={activeMetrics} />
        </div>
      )}

      <p id="szv2-rp-note" style={{ fontSize: 12, color: 'var(--szv2-text-faint)', marginTop: 8 }}>
        {note || `Mostrando ${data?.total ?? 0} pedido(s) visíveis dos últimos 30 dias.`}
      </p>
    </section>
  )
}

// ── Painel de métricas de um grupo — espelha sz9rp_render_panel() ───────────────
// metrics chega SEMPRE normalizado (normalizeMetrics), mas o painel é defensivo por
// conta própria: re-coage numéricos e re-garante os arrays (?? [] / Array.isArray).
// Nenhum acesso pode lançar → nunca tela branca.
function ReportPanel({ metrics }: { metrics: Metrics }) {
  const m = normalizeMetrics(metrics)
  const byStatus = Array.isArray(m.by_status) ? m.by_status : []
  const byProduct = Array.isArray(m.by_product) ? m.by_product : []
  const byRegion = Array.isArray(m.by_region) ? m.by_region : []

  if (m.total === 0) {
    return (
      <EmptyState
        icon="📊"
        title="Sem pedidos no período"
        description="Os relatórios aparecem assim que houver pedidos visíveis."
      />
    )
  }

  return (
    <>
      {/* KPIs */}
      <div className="szv2-kpi-grid">
        <div className="szv2-card szv2-kpi">
          <span className="szv2-kpi-label">Total de pedidos</span>
          <span className="szv2-kpi-value szv2-num">{m.total}</span>
        </div>
        <div className="szv2-card szv2-kpi">
          <span className="szv2-kpi-label">Faturamento total</span>
          <span className="szv2-kpi-value szv2-num">{brl(m.receita)}</span>
        </div>
        <div className="szv2-card szv2-kpi">
          <span className="szv2-kpi-label">Cancelados</span>
          <span className={`szv2-kpi-value szv2-num${m.cancelados > 0 ? ' szv2-kpi-danger' : ''}`}>{m.cancelados}</span>
        </div>
        <div className="szv2-card szv2-kpi">
          <span className="szv2-kpi-label">Ticket médio</span>
          <span className="szv2-kpi-value szv2-num">{brl(m.ticket)}</span>
        </div>
      </div>

      {/* Pedidos por status */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <h2>Pedidos por status</h2>
          <span className="szv2-card-sub">{m.total} total</span>
        </div>
        {byStatus.length === 0 ? (
          <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13, margin: 0 }}>
            Nenhum status para exibir.
          </p>
        ) : (
          byStatus.map((s, i) => {
            // variante (cor da BARRA) vem de statusLabel; o badge usa o componente
            // compartilhado StatusBadge (cor única por status, sem bolinha).
            const [, variant] = statusLabel(s.status)
            return (
              <div className="szv2-report-status-row" key={`${s.status}-${i}`}>
                <StatusBadge status={s.status} className="szv2-report-status-badge" />
                <div className="szv2-report-bar-track">
                  <div className={`szv2-report-bar szv2-report-bar--${variant}`} style={{ width: `${s.pct}%` }} />
                </div>
                <span className="szv2-report-count szv2-num">{`${s.count} (${s.pct}%)`}</span>
              </div>
            )
          })
        )}
      </div>

      {/* Produtos mais vendidos */}
      {byProduct.length > 0 && (
        <div className="szv2-card">
          <div className="szv2-card-head"><h2>Produtos mais vendidos</h2></div>
          <div className="szv2-table-wrap">
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Produto</th>
                  <th className="szv2-td-num">Pedidos</th>
                  <th className="szv2-td-num">Faturamento</th>
                </tr>
              </thead>
              <tbody>
                {byProduct.map((p, i) => (
                  <tr key={`${p.name}-${i}`}>
                    <td>{p.name}</td>
                    <td className="szv2-td-num szv2-num">{p.qty}</td>
                    <td className="szv2-td-num szv2-num">{brl(p.revenue)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Pedidos por região (vazio por design do backend → guardado por length) */}
      {byRegion.length > 0 && (
        <div className="szv2-card">
          <div className="szv2-card-head">
            <h2>Pedidos por região</h2>
            <span className="szv2-card-sub">{m.total} total</span>
          </div>
          {byRegion.map((r, i) => (
            <div className="szv2-report-status-row" key={`${r.region}-${i}`}>
              <span className="szv2-report-region">{r.region}</span>
              <div className="szv2-report-bar-track">
                <div className="szv2-report-bar szv2-report-bar--brand" style={{ width: `${r.pct}%` }} />
              </div>
              <span className="szv2-report-count szv2-num">{`${r.count} (${r.pct}%)`}</span>
            </div>
          ))}
        </div>
      )}
    </>
  )
}
