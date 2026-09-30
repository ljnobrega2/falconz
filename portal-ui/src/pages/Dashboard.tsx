// Dashboard (Visão geral) — port fiel de templates/portal/v2/sections/dashboard.php.
//
// O WP embute um JSON por-pedido e recalcula tudo client-side ao mudar o filtro de
// período. Aqui o backend já agrega: GET /portal/reports devolve COD + Expedição
// (total, receita, ticket, cancelados, by_status, by_product, by_region) e aceita
// recorte por janela ?from&to — então a "recalc por período" vira um re-fetch.
//
// Saldos vêm de endpoints próprios (não reagem ao período, igual ao WP):
//   - Saldo disponível (COD)        → GET /portal/wallet/summary       (.summary.available)
//   - Saldo Expedição               → GET /portal/wallet-expedition/summary (.data.available)
//
// Afiliados ativos no período = CAMPEÕES DE VENDA: o backend agrega by_affiliate
// (produtor-only) — ordenado por faturamento desc, excluindo quem fez R$0 no período.
// Cada linha traz quanto vendeu (R$) e a TAXA DE EFETIVIDADE = entregues/(entregues+
// frustrados)*100. (Antes era um fallback estático de afiliados APROVADOS sem período.)
//
// by_region traz a UF do endereço (billing-first) — o backend deriva de
// sz_order_addresses.uf; a seção é guardada por length > 0.
//
// Cores: SEMPRE var(--szv2-brand) laranja. NUNCA verde #22c55e.
import { useEffect, useState } from 'react'
import { Link, useOutletContext } from 'react-router-dom'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import EmptyState from '../components/EmptyState'
import StatusBadge from '../components/StatusBadge'
import FalkDatePicker from '../components/FalkDatePicker'
import { brl, csvSafe } from '../utils/format'
import type { PortalMe } from '../components/Layout'

// ── Tipos do /portal/reports (idênticos a Reports.tsx) ──────────────────────────
type StatusRow = { status: string; count: number; pct: number }
type ProductRow = { name: string; qty: number; revenue: number }
type RegionRow = { region: string; count: number; pct: number }

type Metrics = {
  total: number
  receita: number
  cancelados: number
  ticket: number
  comissao_liquida?: number
  // liquido_produtor — Σ(total_no_ship − fee4.99% − comissão) só pedidos entregues.
  liquido_produtor?: number
  by_status: StatusRow[]
  by_product: ProductRow[]
  by_region: RegionRow[]
}

// Campeões de venda (produtor-only) — vem do /portal/reports.by_affiliate.
type AffiliateRow = {
  affiliate_id: number
  name: string
  revenue: number // faturamento bruto no período
  // effectiveness: agendado/embalado/em_rota/entregue = efetivo; frustrado/cancelado
  // descontam. = (total - frustrados - cancelados) / total * 100.
  effectiveness: number
  comissao_afiliado: number // Σ comissão líquida distribuída a ESTE afiliado
  comissao_produtor: number // Σ líquido do produtor nos pedidos DESTE afiliado
  comissao_falk: number // Σ take da plataforma nos pedidos DESTE afiliado
}

type ReportsResp = {
  ok: boolean
  cod: Metrics
  exp?: Metrics
  by_affiliate?: AffiliateRow[]
  has_exp: boolean
  total: number
  from: string
  to: string
  role: string
  is_affiliate: boolean
}

type WalletSummaryResp = { ok: boolean; summary?: { available?: number; pending?: number; analysis?: number } }
type WalletExpResp = { ok: boolean; data?: { available?: number; balance?: number; reserved?: number; low_balance?: boolean } }
// /portal/wallet/history?period=mes — extrato do mês (linhas já com net liberado).
type WalletHistResp = { ok: boolean; data?: Array<{ net?: number | string }> }

// ── Conjuntos de status (espelha DONE/ACTIVE/FRUSTR do senderzz-dashboard-v2.js) ──
// AGENDADO ("Agendado") = pedido aceito mas ainda não embalado/em rota. No DB o status
// é `aguardando` (o slug `agendado` não existe na constraint de sz_orders) — então o
// bucket é só ['aguardando']. Mantido DISJUNTO de ACTIVE (que NÃO inclui aguardando)
// p/ não duplicar contagem no card "Eficiência logística".
const DONE = ['entregue', 'completed', 'completo', 'delivered']
const AGENDADO = ['aguardando', 'agendado']
const ACTIVE = ['aprovado', 'on-hold', 'separado', 'embalado', 'coletado', 'acaminho', 'em-rota', 'em_rota', 'emrota', 'emretirada']
const FRUSTR = ['frustrado', 'devolvido']
const CANCEL = ['cancelled', 'cancelado', 'emcancelamento']

// Labels/variantes por status — espelha $sz8rp_st_labels / STATUS_LABELS do WP.
type Variant = 'brand' | 'info' | 'success' | 'danger' | 'neutral' | 'warning'
const ST_LABELS: Record<string, [string, Variant]> = {
  agendado: ['Agendado', 'brand'],
  aguardando: ['Agendado', 'brand'], // DB usa `aguardando`; exibido como "Agendado"
  embalado: ['Embalado', 'brand'],
  acaminho: ['A caminho', 'info'],
  em_rota: ['Em rota', 'info'],
  emrota: ['Em rota', 'info'],
  entregue: ['Entregue', 'success'],
  completed: ['Concluído', 'success'],
  completo: ['Concluído', 'success'],
  frustrado: ['Frustrado', 'danger'],
  cancelado: ['Cancelado', 'neutral'],
  cancelled: ['Cancelado', 'neutral'],
  aprovado: ['Aprovado', 'success'],
  enviado: ['Enviado', 'info'],
  devolvido: ['Devolvido', 'warning'],
  'on-hold': ['Aguardando', 'warning'],
  pending: ['Pendente', 'warning'],
  processing: ['Processando', 'info'],
}
function statusLabel(st: string): [string, Variant] {
  return ST_LABELS[st] ?? [st ? st.charAt(0).toUpperCase() + st.slice(1).replace(/[-_]/g, ' ') : st, 'neutral']
}

function num(v: unknown): number {
  const n = typeof v === 'string' ? parseFloat(v) : (v as number)
  return Number.isFinite(n) ? Number(n) : 0
}

function normalizeProductName(name: string): string {
  const clean = (name || '')
    .trim()
    .replace(/\s+/g, ' ')
    .replace(/[`\u2018\u2019]+$/g, '')
    .trim()
  if (!clean) return ''
  if (/^(datalaprox|pote|potes|remarketing|downsell|padrão|padrao)(\b|\s|$)/i.test(clean)) {
    return 'Datalaprox'
  }
  return clean
}

// normalizeMetrics — blinda qualquer shape do backend (idêntico a Reports.tsx).
function normalizeMetrics(raw: Partial<Metrics> | null | undefined): Metrics {
  const m = raw ?? {}
  return {
    total: num(m.total),
    receita: num(m.receita),
    cancelados: num(m.cancelados),
    ticket: num(m.ticket),
    comissao_liquida: num(m.comissao_liquida),
    liquido_produtor: num(m.liquido_produtor),
    by_status: Array.isArray(m.by_status)
      ? m.by_status
          .map(s => ({ status: String(s?.status ?? ''), count: num(s?.count), pct: num(s?.pct) }))
          .filter(s => s.status !== 'pending' && s.status !== 'processing')
      : [],
    by_product: Array.isArray(m.by_product)
      ? m.by_product.map(p => ({ name: normalizeProductName(String(p?.name ?? '')), qty: num(p?.qty), revenue: num(p?.revenue) }))
      : [],
    by_region: Array.isArray(m.by_region)
      ? m.by_region.map(r => ({ region: String(r?.region ?? ''), count: num(r?.count), pct: num(r?.pct) }))
      : [],
  }
}

// Conta pedidos por conjunto de status (eficiência logística — grid do WP).
function countIn(byStatus: StatusRow[], set: string[]): number {
  return byStatus.reduce((acc, s) => (set.includes(s.status) ? acc + s.count : acc), 0)
}

// Datas-default (últimos 30 dias) — espelha as datas iniciais dos inputs do WP.
// Data LOCAL (não UTC) p/ casar com current_time('Y-m-d') do servidor.
function fmtDate(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
function windowForDays(days: number): { from: string; to: string } {
  const to = new Date()
  const from = new Date()
  // days=1 (Hoje) → from = hoje; senão volta days-1 dias (inclui o dia atual), igual ao WP.
  from.setDate(from.getDate() - (days - 1))
  return { from: fmtDate(from), to: fmtDate(to) }
}

// ── CSV client-side (espelha szV2DashExportXlsx — exporta os agregados do grupo) ──
// Excel-friendly: separador ';' (padrão pt-BR), aspas com escape, valores monetários
// com vírgula decimal. O BOM UTF-8 é prefixado no Blob (onExport) p/ acentos no Excel.
function csvField(v: string | number): string {
  // AUDIT-2026-06-21 #15: csvSafe neutraliza fórmula (= + - @ TAB CR) ANTES do quote-escape.
  const s = csvSafe(v)
  return /[",\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}
// Moeda com vírgula decimal p/ o Excel pt-BR interpretar como número (não texto).
function money(v: number): string {
  return num(v).toFixed(2).replace('.', ',')
}

// Contexto completo da tela — espelha o que está visível no painel ativo, por papel.
type CsvCtx = {
  m: Metrics
  label: string // 'COD' | 'Expedicao'
  from: string
  to: string
  isAff: boolean
  done: number
  agendado: number
  active: number
  frustr: number
  canc: number
  effPct: number
  codAvail: number
  codPending: number
  recebidoMes: number
  expAvail: number
  affiliates: AffiliateRow[]
}

function buildCsv(ctx: CsvCtx): string {
  const { m, label, from, to, isAff, done, agendado, active, frustr, canc, effPct } = ctx
  const frustrPct = m.total > 0 ? Math.round((frustr / m.total) * 100) : 0
  const lines: string[] = []
  lines.push(`Painel;${csvField(isAff ? 'Afiliado' : label)}`)
  lines.push(`Periodo;${csvField(from || '-')} a ${csvField(to || '-')}`)
  lines.push('')

  // ── Indicadores (KPIs visíveis na tela, por papel) ──
  lines.push('Indicador;Valor')
  if (isAff) {
    // Painel AFILIADO — eixo comissão (espelha AffiliateDashboard).
    lines.push(`Comissao a receber;${money(ctx.codAvail)}`)
    lines.push(`A liberar;${money(ctx.codPending)}`)
    lines.push(`Recebido no mes;${money(ctx.recebidoMes)}`)
    lines.push(`Vendas no periodo;${num(m.total)}`)
    lines.push(`Conversao (entregues);${effPct}%`)
    lines.push(`Taxa de frustracao;${frustrPct}%`)
  } else if (label === 'COD') {
    // Painel PRODUTOR COD (espelha CodPanel).
    lines.push(`Saldo disponivel;${money(ctx.codAvail)}`)
    lines.push(`Faturamento;${money(m.receita)}`)
    lines.push(`Pedidos no periodo;${num(m.total)}`)
    lines.push(`Ticket medio;${money(m.ticket)}`)
    lines.push(`Taxa de entrega COD;${effPct}%`)
  } else {
    // Painel PRODUTOR Expedição (espelha ExpPanel).
    lines.push(`Saldo Expedicao;${money(ctx.expAvail)}`)
    lines.push(`Total cobrado;${money(m.receita)}`)
    lines.push(`Pedidos Expedicao;${num(m.total)}`)
    lines.push(`Ticket medio;${money(m.ticket)}`)
    lines.push(`Cancelados;${num(canc)}`)
    lines.push(`Taxa de entrega;${effPct}%`)
  }
  lines.push('')

  // ── Situação consolidada (agendado/em rota/entregues/frustrados/cancelados) ──
  lines.push('Situacao;Quantidade')
  if (!isAff && label === 'COD') lines.push(`Agendado;${num(agendado)}`)
  lines.push(`Entregues;${num(done)}`)
  if (!isAff && label === 'COD') lines.push(`Em rota;${num(active)}`)
  lines.push(`Frustrados;${num(frustr)}`)
  lines.push(`Cancelados;${num(canc)}`)
  lines.push('')

  // ── Pedidos por status ──
  lines.push('Status;Pedidos;%')
  m.by_status.forEach(s => lines.push(`${csvField(statusLabel(s.status)[0])};${num(s.count)};${num(s.pct)}`))
  lines.push('')

  // ── Produtos mais vendidos ──
  lines.push('Produto;Pedidos;Faturamento')
  m.by_product.forEach(p => lines.push(`${csvField(p.name)};${num(p.qty)};${money(p.revenue)}`))

  // ── Pedidos por região (quando o backend popula) ──
  if (m.by_region.length) {
    lines.push('')
    lines.push('Regiao;Pedidos;%')
    m.by_region.forEach(r => lines.push(`${csvField(r.region)};${num(r.count)};${num(r.pct)}`))
  }

  // ── Afiliados ativos no período = campeões de venda (somente produtor) ──
  if (!isAff && ctx.affiliates.length) {
    lines.push('')
    lines.push('Afiliado;Vendeu R$;Efetividade %')
    ctx.affiliates.forEach(a =>
      lines.push(`${csvField(a.name || '-')};${money(a.revenue)};${num(a.effectiveness)}`),
    )
  }

  return lines.join('\r\n')
}

const PERIODS: { days: number; label: string }[] = [
  { days: 1, label: 'Hoje' },
  { days: 7, label: '7 dias' },
  { days: 30, label: '30 dias' },
  { days: 90, label: '90 dias' },
]

export default function Dashboard() {
  const toast = useToast()
  // `me` vem do Outlet (Layout.tsx só monta a página DEPOIS que `me` carrega) — mesma
  // fonte que OrdersHub usa pro gate de expedição, disponível já no 1º render. Antes
  // este componente buscava /portal/me e /portal/reports por conta própria e a aba
  // Expedição só aparecia depois que `data.has_exp` chegava — piscava (some → aparece),
  // diferente da tela "Pedidos" (OrdersHub), onde a aba já nasce certa.
  const { me } = useOutletContext<{ me: PortalMe | null }>() ?? { me: null }

  // Modo COD × Expedição (espelha o szv2-dash-switcher).
  const [mode, setMode] = useState<'cod' | 'exp'>('cod')

  // Relatórios (re-fetch por período) + saldos (estáveis).
  const [data, setData] = useState<ReportsResp | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [codAvail, setCodAvail] = useState(0)
  const [codPending, setCodPending] = useState(0) // afiliado: comissão a liberar
  const [recebidoMes, setRecebidoMes] = useState(0) // afiliado: comissão creditada lifetime
  const [expAvail, setExpAvail] = useState(0)

  // Filtro de período: botão ativo (days) OU range custom (from/to).
  const [activeDays, setActiveDays] = useState<number>(7) // WP COD default = 7 dias
  const init = windowForDays(7)
  const [from, setFrom] = useState(init.from)
  const [to, setTo] = useState(init.to)

  function loadReports(w: { from: string; to: string }) {
    setLoading(true)
    const qs = w.from && w.to ? `?from=${encodeURIComponent(w.from)}&to=${encodeURIComponent(w.to)}` : ''
    api<ReportsResp>(`/portal/reports${qs}`)
      .then(r => {
        setData(r)
        if (!r.has_exp) setMode('cod')
        // "Total recebido" afiliado = comissão creditada NO PERÍODO (comissao_liquida do COD)
        if (r.is_affiliate && r.cod?.comissao_liquida != null) {
          setRecebidoMes(Math.round(num(r.cod.comissao_liquida) * 100) / 100)
        }
        setErr('')
      })
      .catch(e => setErr(e.message || 'Erro ao carregar a visão geral'))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    // Saldos — leitura única (não reagem ao período).
    api<WalletSummaryResp>('/portal/wallet/summary')
      .then(r => {
        setCodAvail(num(r?.summary?.available))
        setCodPending(num(r?.summary?.pending))
      })
      .catch(() => {})
    // recebidoMes é atualizado via loadReports (comissao_liquida do período) — sem fetch separado aqui.
    api<WalletExpResp>('/portal/wallet-expedition/summary').then(r => setExpAvail(num(r?.data?.available))).catch(() => {})
    // Afiliados ativos (campeões de venda) vêm do /portal/reports.by_affiliate — não há
    // fetch separado; a lista reage ao período junto com os demais agregados.
    loadReports(init)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Clique nos botões de atalho (Hoje/7/30/90) — recalcula a janela e re-busca.
  function selectDays(days: number) {
    const w = windowForDays(days)
    setActiveDays(days)
    setFrom(w.from)
    setTo(w.to)
    loadReports(w)
  }

  // Range custom (De/até) — desativa o atalho e re-busca (espelha szV2DashCustomRange).
  function applyCustomRange(nf: string, nt: string) {
    setActiveDays(0) // 0 = nenhum atalho ativo
    if (nf && nt) loadReports({ from: nf, to: nt })
  }

  const isAff = !!data?.is_affiliate
  // Mesma fórmula de OrdersHub/Layout.tsx (produtor + settings.expedicao_ativa) —
  // não depende do fetch de /portal/reports, então não pisca no 1º render.
  const role = (me?.role || 'cliente').toLowerCase()
  const isProducerMe = role === 'produtor' || role === 'producer'
  const expFlag = me?.settings?.expedicao_ativa
  const hasExp = isProducerMe && (expFlag === true || expFlag === 'true')
  const cod = normalizeMetrics(data?.cod)
  const exp = normalizeMetrics(data?.exp)
  const m = mode === 'cod' ? cod : exp

  // Campeões de venda (produtor-only) — já ordenados/filtrados pelo backend.
  const affiliates: AffiliateRow[] = Array.isArray(data?.by_affiliate)
    ? data!.by_affiliate.map(a => ({
        affiliate_id: num(a?.affiliate_id),
        name: String(a?.name ?? ''),
        revenue: num(a?.revenue),
        effectiveness: num(a?.effectiveness),
        comissao_afiliado: num(a?.comissao_afiliado),
        comissao_produtor: num(a?.comissao_produtor),
        comissao_falk: num(a?.comissao_falk),
      }))
    : []

  // Eficiência (espelha recalcCod/recalcExp): contagens por conjunto de status.
  const done = countIn(m.by_status, DONE)
  const agendado = countIn(m.by_status, AGENDADO)
  const active = countIn(m.by_status, ACTIVE)
  const frustr = countIn(m.by_status, FRUSTR)
  const canc = countIn(m.by_status, CANCEL)
  // AUDIT-2026-07-11 (pedido do dono): agendado/embalado/em_rota (+ entregue) contam
  // como pedido EFETIVO; frustrado/cancelado são DESCONTO na eficiência. Antes só
  // "entregue" contava (penalizava pedidos ainda em andamento sem terem falhado).
  const effPct = m.total > 0 ? Math.round(((m.total - frustr - canc) / m.total) * 100) : 0

  function onExport() {
    // Painel ativo: afiliado sempre COD; produtor segue o switcher COD/Expedição.
    const label = mode === 'cod' ? 'COD' : 'Expedicao'
    // Sempre exporta — zeros são dados válidos (KPIs/saldos aparecem mesmo sem pedidos).
    const csv = buildCsv({
      m, label, from, to, isAff,
      done, agendado, active, frustr, canc, effPct,
      codAvail, codPending, recebidoMes, expAvail, affiliates,
    })
    // BOM UTF-8 (﻿) → Excel reconhece acentos; type text/csv p/ download direto.
    const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    const today = fmtDate(new Date())
    a.download = `falk-painel-${isAff ? 'afiliado' : mode}-${from || today}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    const pLabel = isAff ? 'Afiliado' : label
    toast('ok', `Painel ${pLabel} exportado.`)
  }

  return (
    <section id="sec-dashboard" className="sz-sec">
      {/* Ação de exportação. A saudação ("Olá, …" + "logado como …") foi movida para
          a topbar OPACA (sticky) em Layout.tsx — antes ela ficava aqui no fluxo de
          rolagem e VAZAVA sob a barra de topo ao rolar ("override"). Mantemos só o
          botão Exportar, alinhado à direita. */}
      <div
        className="szv2-page-head"
        style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}
      >
        {/* #78 — título da tela ACIMA do seletor de período (a tela nascia sem heading,
            só com o seletor Hoje/7/30/90). Mesma convenção das outras páginas
            (h2.szv2-page-title). O nome do usuário fica no topbar, não aqui. */}
        <div>
          <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
            Visão geral
          </h2>
          <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>Seus números de vendas e entregas no período.</p>
        </div>
        <button
          type="button"
          className="szv2-btn szv2-btn-secondary szv2-btn-sm"
          onClick={onExport}
          disabled={loading}
          style={{ display: 'flex', alignItems: 'center', gap: 6 }}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
            <polyline points="7 10 12 15 17 10" />
            <line x1="12" y1="15" x2="12" y2="3" />
          </svg>
          Exportar Excel
        </button>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}

      {/* Seletor COD / Expedição. O rótulo "Cash on Delivery" fica SEMPRE à mostra
          e ESTÁTICO: antes o bloco era gateado por `!isAff`, e como `isAff` deriva de
          `data` (null no 1º render → true só depois do fetch p/ afiliado/cliente), o
          switcher aparecia e SUMIA a cada carga/troca de período — efeito de "piscar".
          Sem o gate, o DOM não muda no load → nada pisca (sem !important, sem animação).
          A aba Expedição segue gateada por `hasExp`; afiliado/cliente (sem expedição)
          vê só a aba COD, e o painel do corpo é escolhido por `isAff` (não por `mode`,
          travado em 'cod' p/ quem não tem expedição), então a aba COD solitária é inócua. */}
      <div className="szv2-dash-switcher" role="tablist" aria-label="Modo de visualização">
          <button
            type="button"
            className={`szv2-dash-tab${mode === 'cod' ? ' szv2-dash-tab--active' : ''}`}
            role="tab"
            aria-selected={mode === 'cod'}
            onClick={() => setMode('cod')}
          >
            <svg viewBox="0 0 20 20" aria-hidden="true">
              <path d="M5 14a2.5 2.5 0 1 0 0 .01zM15 14a2.5 2.5 0 1 0 0 .01zM11 5h3l3 4v4h-2a3 3 0 0 0-6 0H8a3 3 0 0 0-5.4-1.8L2 9l4-1 2-3h3z" />
            </svg>
            Cash on Delivery
          </button>
          {hasExp && (
            <button
              type="button"
              className={`szv2-dash-tab${mode === 'exp' ? ' szv2-dash-tab--active' : ''}`}
              role="tab"
              aria-selected={mode === 'exp'}
              onClick={() => setMode('exp')}
            >
              <svg viewBox="0 0 20 20" aria-hidden="true">
                <path d="M10 2 3 5.5v9L10 18l7-3.5v-9L10 2zm0 2.2 4.6 2.3L10 8.8 5.4 6.5 10 4.2zM5 8.1l4 2v5.3l-4-2V8.1zm10 0v5.3l-4 2v-5.3l4-2z" />
              </svg>
              Expedição
            </button>
          )}
      </div>

      {/* Barra de período (espelha szv2-period-bar) — comum aos dois painéis */}
      <div className="szv2-period-bar" role="group" aria-label="Período">
        <span className="szv2-period-label">Período:</span>
        {PERIODS.map(p => (
          <button
            key={p.days}
            type="button"
            className={`szv2-period-btn${activeDays === p.days ? ' szv2-period-btn--active' : ''}`}
            onClick={() => selectDays(p.days)}
            disabled={loading}
          >
            {p.label}
          </button>
        ))}
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, marginLeft: 6 }}>
          <label style={{ fontSize: 12, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>De</label>
          <FalkDatePicker
            value={from}
            style={{ width: 138 }}
            aria-label="Data inicial"
            onChange={v => { setFrom(v); applyCustomRange(v, to) }}
          />
          <label style={{ fontSize: 12, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>até</label>
          <FalkDatePicker
            value={to}
            style={{ width: 138 }}
            aria-label="Data final"
            onChange={v => { setTo(v); applyCustomRange(from, v) }}
          />
        </span>
      </div>

      {loading && !data ? (
        <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
      ) : isAff ? (
        // AFILIADO — painel de COMISSÃO (sem blocos logísticos Em rota/Transportadora/
        // Eficiência). KPIs vêm do que a API serve hoje: /portal/wallet/summary
        // (a receber/a liberar/em análise) + /portal/reports (vendas/conversão).
        <AffiliateDashboard
          m={cod}
          avail={codAvail}
          pending={codPending}
          recebidoMes={recebidoMes}
          done={done}
          frustr={frustr}
          canc={canc}
          effPct={effPct}
        />
      ) : mode === 'cod' ? (
        <CodPanel
          m={cod}
          codAvail={codAvail}
          isAff={isAff}
          done={done}
          agendado={agendado}
          active={active}
          frustr={frustr}
          canc={canc}
          effPct={effPct}
          affiliates={affiliates}
        />
      ) : (
        <ExpPanel m={exp} expAvail={expAvail} done={done} canc={canc} effPct={effPct} />
      )}
    </section>
  )
}

// ── Painel AFILIADO ───────────────────────────────────────────────────────────────
// Reorienta o dashboard p/ COMISSÃO (UX-AUDIT §2.3). Sem "Em rota"/"Transportadora
// destaque"/"Eficiência logística"/"Custo médio de frete"/"Pedidos por região" — métricas
// de produtor/OL. Eixo: a receber, a liberar, em análise, vendas válidas, conversão,
// frustração. Saldos de /portal/wallet/summary; vendas/status de /portal/reports.
function AffiliateDashboard({
  m, avail, pending, recebidoMes, done, frustr, canc, effPct,
}: {
  m: Metrics
  avail: number
  pending: number
  recebidoMes: number
  done: number
  frustr: number
  canc: number
  effPct: number
}) {
  // Conversão = entregues / pedidos no período (proxy da taxa de sucesso da comissão).
  const frustrPct = m.total > 0 ? Math.round((frustr / m.total) * 100) : 0
  return (
    <>
      {/* KPIs de comissão — a receber/a liberar/em análise + vendas e conversão */}
      <div className="szv2-kpi-grid">
        <Kpi label="Comissão no período" value={brl(recebidoMes)} meta="comissão creditada no período" />
        <Kpi label="Vendas no período" value={String(m.total)} meta="pedidos atribuídos a você" />
        <Kpi label="Conversão (entregues)" value={`${effPct}%`} meta={`${done} entregue(s)`} />
        <Kpi label="Taxa de frustração" value={`${frustrPct}%`} meta={`${frustr} frustrado(s)`} />
      </div>

      {/* Pedidos por status — distribuição das suas vendas */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <h2>Suas vendas por status</h2>
          <span className="szv2-card-sub">{m.total} no período</span>
        </div>
        <StatusBars rows={m.by_status} total={m.total} />
      </div>

      {/* Situação consolidada (entregues/frustrados/cancelados) — sem termos logísticos de produtor */}
      <div className="szv2-card">
        <div className="szv2-card-head"><h2>Situação das vendas</h2></div>
        <div className="szv2-opcard-grid szv2-opcard-grid-3">
          <div className="szv2-opcard-item"><span className="szv2-opcard-val">{done}</span><span className="szv2-opcard-label">Entregues</span></div>
          <div className="szv2-opcard-item"><span className="szv2-opcard-val" style={{ color: 'var(--szv2-danger)' }}>{frustr}</span><span className="szv2-opcard-label">Frustrados</span></div>
          <div className="szv2-opcard-item szv2-opcard-item--muted"><span className="szv2-opcard-val">{canc}</span><span className="szv2-opcard-label">Cancelados</span></div>
        </div>
      </div>

      {/* Produtos que você mais vendeu */}
      <div className="szv2-card">
        <div className="szv2-card-head"><h2>Produtos que você mais vendeu</h2></div>
        <div className="szv2-table-wrap szv2-table-flush">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>Produto</th>
                <th className="szv2-td-num">Vendas</th>
                <th className="szv2-td-num">Faturamento gerado</th>
              </tr>
            </thead>
            <tbody>
              {m.by_product.length === 0 ? (
                <tr><td colSpan={3} className="szv2-empty-cell" style={{ color: 'var(--szv2-text-muted)' }}>Sem vendas no período</td></tr>
              ) : (
                m.by_product.map((p, i) => (
                  <tr key={`${p.name}-${i}`}>
                    <td>{p.name}</td>
                    <td className="szv2-td-num szv2-num">{p.qty}</td>
                    <td className="szv2-td-num szv2-num">{brl(p.revenue)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <p className="szv2-dash-nav-link" style={{ marginTop: 12 }}>
        <Link to="/wallet" className="szv2-link-btn" style={{ color: 'var(--szv2-brand)', fontWeight: 600, textDecoration: 'none' }}>
          Ver carteira de comissões →
        </Link>
      </p>
    </>
  )
}

// ── Barras de status (espelha szV2RenderStatusBars) ──────────────────────────────
function StatusBars({ rows, total }: { rows: StatusRow[]; total: number }) {
  if (total === 0 || rows.length === 0) {
    return <div style={{ fontSize: 12, color: 'var(--szv2-text-faint)' }}>Sem dados no período.</div>
  }
  const sorted = [...rows].sort((a, b) => b.count - a.count)
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 4 }}>
      {sorted.map((s, i) => {
        // variante (cor da BARRA) vem de statusLabel; o badge usa o componente
        // compartilhado StatusBadge (cor única por status, sem bolinha).
        const [, variant] = statusLabel(s.status)
        return (
          <div key={`${s.status}-${i}`} className="szv2-report-status-row">
            <StatusBadge status={s.status} className="szv2-report-status-badge" />
            <div className="szv2-report-bar-track">
              <div className={`szv2-report-bar szv2-report-bar--${variant}`} style={{ width: `${s.pct}%` }} />
            </div>
            <span className="szv2-report-count szv2-num">{`${s.count} (${s.pct}%)`}</span>
          </div>
        )
      })}
    </div>
  )
}

// ── Status em linha única (mesmo padrão visual do mini-grid do card Eficiência
// logística — pedido do dono 2026-07-11, substitui as barras verticais). ──────────
function StatusLine({ rows, effPct }: { rows: StatusRow[]; effPct: number }) {
  if (rows.length === 0) {
    return <div style={{ fontSize: 12, color: 'var(--szv2-text-faint)' }}>Sem dados no período.</div>
  }
  const sorted = [...rows].sort((a, b) => b.count - a.count)
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 12 }}>
        <span style={{ fontSize: 24, fontWeight: 600, color: 'var(--szv2-brand)' }}>{effPct}%</span>
        <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>eficiência (agendado/embalado/em rota/entregue vs. frustrado/cancelado)</span>
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {sorted.map((s, i) => {
          const [label] = statusLabel(s.status)
          return (
            <div
              key={`${s.status}-${i}`}
              style={{ flex: '1 1 90px', textAlign: 'center', padding: 8, background: 'var(--szv2-surface-alt)', borderRadius: 'var(--szv2-radius-md)' }}
            >
              <div style={{ fontSize: 18, fontWeight: 600, color: 'var(--szv2-text)' }}>{s.count}</div>
              <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{label} ({s.pct}%)</div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ── Barras de região (espelha barRow) ────────────────────────────────────────────
function RegionBars({ rows }: { rows: RegionRow[] }) {
  if (rows.length === 0) {
    return <p className="szv2-empty-inline" style={{ fontSize: 13, color: 'var(--szv2-text-muted)', margin: 0 }}>Sem dados no período</p>
  }
  return (
    <>
      {rows.map((r, i) => (
        <div className="szv2-region-row" key={`${r.region}-${i}`}>
          <span className="szv2-region-name">{r.region}</span>
          <div className="szv2-region-bar-wrap">
            <div className="szv2-region-bar-fill" style={{ width: `${r.pct}%` }} />
          </div>
          <span className="szv2-region-meta szv2-num">
            {r.count} <small>({r.pct}%)</small>
          </span>
        </div>
      ))}
    </>
  )
}

// ── KPI card simples (espelha sz_v2_kpi_card) ────────────────────────────────────
function Kpi({ label, value, meta }: { label: string; value: string; meta?: string }) {
  return (
    <div className="szv2-card szv2-kpi">
      <span className="szv2-kpi-label">{label}</span>
      <span className="szv2-kpi-value szv2-num">{value}</span>
      {meta && <span className="szv2-kpi-meta" style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{meta}</span>}
    </div>
  )
}

// ── Painel COD ───────────────────────────────────────────────────────────────────
function CodPanel({
  m, codAvail, isAff, done, agendado, active, frustr, canc, effPct, affiliates,
}: {
  m: Metrics
  codAvail: number
  isAff: boolean
  done: number
  agendado: number
  active: number
  frustr: number
  canc: number
  effPct: number
  affiliates: AffiliateRow[]
}) {
  return (
    <>
      {/* KPIs COD — "Saldo disponível" e "Taxa de entrega COD" removidos (pedido do dono).
          "Comissão líquida" (comissão de afiliados já net, regra #1587) ao lado de Faturamento. */}
      <div className="szv2-kpi-grid">
        <Kpi label="Faturamento" value={brl(m.receita)} />
        {isAff
          ? <Kpi label="Minha comissão" value={brl(m.comissao_liquida ?? 0)} meta="líquida no período" />
          : <Kpi label="Comissão" value={brl(m.liquido_produtor ?? 0)} />
        }
        <Kpi label="Pedidos no período" value={String(m.total)} />
        <Kpi label="Ticket médio" value={brl(m.ticket)} />
      </div>

      {/* Pedidos por status (linha única, mesmo padrão do mini-grid de Eficiência
          logística — pedido do dono 2026-07-11) + Produtos mais vendidos (por oferta) */}
      <div className="szv2-dash-row">
        <div className="szv2-card szv2-dash-opcard" style={{ flex: 1.4 }}>
          <div className="szv2-card-head">
            <h2>Pedidos por status</h2>
            <span className="szv2-card-sub">{m.total} no período</span>
          </div>
          <StatusLine rows={m.by_status} effPct={effPct} />
        </div>

        <div className="szv2-card" style={{ flex: 1 }}>
          <div className="szv2-card-head">
            <h2>Produtos mais vendidos</h2>
            <span className="szv2-card-sub">Por nome de oferta</span>
          </div>
          <div className="szv2-table-wrap szv2-table-flush">
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Oferta</th>
                  <th className="szv2-td-num">Pedidos</th>
                  <th className="szv2-td-num">Faturamento</th>
                </tr>
              </thead>
              <tbody>
                {m.by_product.length === 0 ? (
                  <tr><td colSpan={3} className="szv2-empty-cell" style={{ color: 'var(--szv2-text-muted)' }}>Sem produtos no período. Ajuste a data ou aguarde novos pedidos.</td></tr>
                ) : (
                  m.by_product.map((p, i) => (
                    <tr key={`${p.name}-${i}`}>
                      <td>{p.name}</td>
                      <td className="szv2-td-num szv2-num">{p.qty}</td>
                      <td className="szv2-td-num szv2-num">{brl(p.revenue)}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      {/* Pedidos por região — comentado a pedido do dono (2026-07-11). Deixa o JSX
          aqui (não deletado) pra religar rápido se quiser de volta depois.
      <div className="szv2-card">
        <div className="szv2-card-head">
          <h2>Pedidos por região</h2>
          <span className="szv2-card-sub">{m.total}</span>
        </div>
        <div className="szv2-region-list">
          <RegionBars rows={m.by_region} />
        </div>
      </div>
      */}

      {/* Afiliados ativos no período = CAMPEÕES DE VENDA (somente produtor — espelha
          !is_aff). Ordenado por faturamento desc (backend), excluindo quem fez R$0.
          Colunas: Afiliado | Vendeu | Comissão afiliado | Comissão produtor | Comissão
          Falk | Taxa de sucesso (agendado/embalado/em_rota/entregue = efetivo;
          frustrado/cancelado descontam — pedido do dono 2026-07-11). */}
      {!isAff && (
        <div className="szv2-card">
          <div className="szv2-card-head">
            <h2>Afiliados ativos no período</h2>
            <span className="szv2-card-sub">campeões de venda</span>
          </div>
          {affiliates.length === 0 ? (
            <EmptyState
              icon="🏆"
              title="Sem vendas de afiliados no período"
              description="Quando seus afiliados venderem neste período, o ranking aparecerá aqui."
            />
          ) : (
            <div className="szv2-table-wrap szv2-table-flush">
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Afiliado</th>
                    <th className="szv2-td-num">Vendeu</th>
                    <th className="szv2-td-num">Comissão afiliado</th>
                    <th className="szv2-td-num">Comissão produtor</th>
                    <th className="szv2-td-num">Taxa de sucesso</th>
                  </tr>
                </thead>
                <tbody>
                  {affiliates.map(a => (
                    <tr key={a.affiliate_id}>
                      <td>{a.name || '—'}</td>
                      <td className="szv2-td-num szv2-num">{brl(a.revenue)}</td>
                      <td className="szv2-td-num szv2-num">{brl(a.comissao_afiliado)}</td>
                      <td className="szv2-td-num szv2-num">{brl(a.comissao_produtor)}</td>
                      <td className="szv2-td-num szv2-num">{num(a.effectiveness)}%</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      <p className="szv2-dash-nav-link" style={{ marginTop: 12 }}>
        <Link to="/motoboy" className="szv2-link-btn" style={{ color: 'var(--szv2-brand)', fontWeight: 600, textDecoration: 'none' }}>
          Ver pedidos COD →
        </Link>
      </p>
    </>
  )
}

// ── Painel Expedição ─────────────────────────────────────────────────────────────
function ExpPanel({
  m, expAvail, done, canc, effPct,
}: {
  m: Metrics
  expAvail: number
  done: number
  canc: number
  effPct: number
}) {
  return (
    <>
      {/* KPIs Expedição */}
      <div className="szv2-kpi-grid">
        <Kpi label="Saldo Expedição" value={brl(expAvail)} />
        <Kpi label="Total cobrado" value={brl(m.receita)} />
        <Kpi label="Pedidos Expedição" value={String(m.total)} meta="pedidos no período" />
        <Kpi label="Ticket médio" value={brl(m.ticket)} />
        <Kpi label="Cancelados" value={String(canc)} />
        <Kpi label="Taxa de entrega" value={`${effPct}%`} meta="entregas concluídas" />
      </div>

      {/* Situação dos pedidos */}
      <div className="szv2-card">
        <div className="szv2-card-head"><h2>Situação dos pedidos</h2></div>
        <div className="szv2-opcard-grid szv2-opcard-grid-3">
          <div className="szv2-opcard-item"><span className="szv2-opcard-val">{done}</span><span className="szv2-opcard-label">Entregues</span></div>
          <div className="szv2-opcard-item szv2-opcard-item--muted"><span className="szv2-opcard-val">{canc}</span><span className="szv2-opcard-label">Cancelados</span></div>
          <div className="szv2-opcard-item"><span className="szv2-opcard-val">{effPct}%</span><span className="szv2-opcard-label">Taxa de entrega</span></div>
        </div>
      </div>

      {/* Pedidos por status */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <h2>Pedidos por status</h2>
          <span className="szv2-card-sub">{m.total} no período</span>
        </div>
        <StatusBars rows={m.by_status} total={m.total} />
      </div>

      {/* Produtos mais expedidos */}
      <div className="szv2-card">
        <div className="szv2-card-head"><h2>Produtos mais expedidos</h2></div>
        <div className="szv2-table-wrap szv2-table-flush">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>Produto</th>
                <th className="szv2-td-num">Expedições</th>
                <th className="szv2-td-num">Faturamento</th>
              </tr>
            </thead>
            <tbody>
              {m.by_product.length === 0 ? (
                <tr><td colSpan={3} className="szv2-empty-cell" style={{ color: 'var(--szv2-text-muted)' }}>Sem produtos no período. Ajuste a data ou aguarde novos pedidos.</td></tr>
              ) : (
                m.by_product.map((p, i) => (
                  <tr key={`${p.name}-${i}`}>
                    <td>{p.name}</td>
                    <td className="szv2-td-num szv2-num">{p.qty}</td>
                    <td className="szv2-td-num szv2-num">{brl(p.revenue)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Expedições por região */}
      <div className="szv2-card">
        <div className="szv2-card-head"><h2>Expedições por região</h2><span className="szv2-card-sub">No período</span></div>
        <div className="szv2-region-list">
          <RegionBars rows={m.by_region} />
        </div>
      </div>

      <p className="szv2-dash-nav-link" style={{ marginTop: 12 }}>
        <Link to="/expedicao" className="szv2-link-btn" style={{ color: 'var(--szv2-brand)', fontWeight: 600, textDecoration: 'none' }}>
          Ver expedições →
        </Link>
      </p>
    </>
  )
}
