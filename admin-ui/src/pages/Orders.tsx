import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, BASE, getToken } from '../api'
import { brl, ymd, brDate } from '../utils/format'
import { safeUrl } from '../utils/safeUrl' // AUDIT-2026-06-21 #13
import FilterDrawer from '../components/FilterDrawer'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import CopyButton from '../components/CopyButton'
import StatusBadge, { statusLabel } from '../components/StatusBadge'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'
import { confirmAsync } from '../components/ConfirmDialog'
import { emitToast } from '../hooks/useToast'

// BUILD-STATUS-LABEL-2026-08-05: invalida o bundle do painel após a padronização
// visual de a_caminho para A Caminho.
import BulkBar, { useBulkSelection } from '../components/BulkBar'

// ── Tipos do detalhe enriquecido (GET /orders/{id}) ──────────────────────────
// Mantém em sync com order_detail.go — só os campos exibidos no drawer.
type DetailMarketing = {
  utm_source: string; utm_medium: string; utm_campaign: string
  utm_term: string; utm_content: string; referrer: string; landing_page: string
}
type DetailFiscal = {
  nfe_chave: string; nfe_numero: string; nfe_serie: string
  nfe_url: string; nfe_status: string
}
type DetailTracking = { code: string; url: string; carrier: string }
type DetailMotoboyInfo = {
  motoboy_nome?: string
  motoboy_telefone?: string
  motoboy_placa?: string
}
type DetailAudit = {
  id: number
  actor_tipo: string
  acao: string
  de_status?: string | null
  para_status?: string | null
  created_at: string
}
type DetailFullAddress = {
  exists: boolean
  nome: string
  telefone: string  // telefone do cliente (order_detail.go → orderFullAddress.Telefone)
  cep: string
  logradouro: string
  numero: string
  complemento: string
  bairro: string
  cidade: string
  uf: string
}
// Breakdown financeiro discriminado — backend passa a expor em GET /orders/{id}.
// Sync com order_detail.go → payload.financeiro (todos os valores JÁ calculados no
// backend; NÃO recalcular aqui — a regra de comissão é oficial e mora no Go).
// Fórmula (referência, não reimplementar):
//   comissao_afiliado_bruta   = valor_total × comissao_pct
//   taxa_transacao_afiliado   = bruta × 0,0499      (fatia da plataforma)
//   comissao_afiliado_liquida = bruta × (1 − 0,0499)
//   liquido_produtor          = valor_total − bruta − taxa_entrega − taxa_transacao_produtor
//   Frustrado: comissão substituída pela taxa de frustrado.
type DetailFinanceiro = {
  valor_pedido: number
  comissao_pct?: number
  comissao_afiliado_bruta: number
  taxa_transacao_afiliado: number   // 4,99%
  comissao_afiliado_liquida: number
  taxa_entrega: number
  taxa_transacao_produtor: number
  liquido_produtor: number
  // Frustrado — backend marca frustrado=true (cobre 'frustrado' e 'reembolsado').
  // PREJUÍZO de cada parte (REGRA DO DONO 2026-06-22): valores POSITIVOS, exibidos
  // como despesa negativa/danger. prejuizo_afiliado = bruta + taxa_frustracao_afiliado;
  // prejuizo_produtor = taxa_entrega + taxa_frustracao_produtor. Opcionais p/ compat.
  frustrado?: boolean
  taxa_frustracao_afiliado?: number
  taxa_frustracao_produtor?: number
  prejuizo_afiliado?: number
  prejuizo_produtor?: number
}
type OrderDetailLite = {
  // SHAPE: o backend aninha o breakdown em order.financeiro (não no topo).
  order?: {
    endereco_envio?: DetailFullAddress
    endereco_cobranca?: DetailFullAddress
    financeiro?: DetailFinanceiro
  }
  marketing?: DetailMarketing
  fiscal?: DetailFiscal
  tracking?: DetailTracking
  motoboy?: DetailMotoboyInfo & { audit?: DetailAudit[] }
  // financeiro: bloco novo. Opcional — se o backend ainda não expõe, o drawer
  // cai no resumo financeiro simples (valor + taxa + comissão líquida).
  // Mantido no topo como fallback de compat. com versões antigas do backend.
  financeiro?: DetailFinanceiro
}

type P = {
  id: number
  wc_order_id?: number | null
  sz_order_id?: number | null
  motoboy_id?: number | null
  status: string
  financial_status?: string
  scheduled_payment_date?: string | null
  valor: number
  taxa_motoboy: number
  taxa_frustrado: number
  dest_nome: string
  dest_cep: string
  dest_cidade: string
  dest_uf: string
  cliente_nome: string
  // cliente_telefone — telefone do cliente (sync com orders.go → cliente_telefone).
  // Opcional: lista funciona mesmo se o backend ainda não enviar.
  cliente_telefone?: string
  produto: string
  // produto_qtd — quantidade FÍSICA real (coluna sz_order_items.quantidade ×
  // multiplicador do nome, mesma fórmula do trigger de estoque). Fonte de
  // verdade — NUNCA re-derivar de oferta_nome/produto no front (marketing
  // pode divergir da coluna real, ex.: "3 potes + 1 brinde" = 4 unidades).
  produto_qtd?: number
  afiliado_nome: string
  oferta_link: string
  // comissao = sz_orders.affiliate_amount → LÍQUIDA do afiliado (já tem o 4,99%
  // de taxa de transação descontado). Nunca exibir comissão bruta.
  // Oferta de checkout = link do produtor que define o PREÇO de venda (fonte fiel migrada).
  oferta_nome: string   // _senderzz_offer_name
  oferta_valor: number  // _senderzz_offer_value (== display_value do link)
  oferta_url: string    // senderzz_checkout_links.url
  // variacao — variação REAL do item do pedido (atributo do produto), vinda do
  // backend (orders.go → variacao). NÃO tem relação com o link de checkout. Hoje
  // sempre "" (fonte real inexistente no banco — ver nota no backend) → exibe "—".
  variacao?: string
  comissao: number
  comissao_afiliado_bruta?: number
  comissao_afiliado_liquida?: number
  comissao_produtor?: number
  taxa_falk?: number
  taxa_frustracao_afiliado?: number
  taxa_frustracao_produtor?: number
  created_at: string
  // delivery_date — data de entrega (YYYY-MM-DD), enviada por orders.go (delivery_date).
  // Opcional/nullable: usada na exportação CSV; a tabela ignora se ausente.
  delivery_date?: string | null
  motoboy_nome: string
  dest_endereco: string
  dest_numero: string
  dest_complemento: string
  dest_bairro: string
  dest_produto: string
  package_code: string
}

// ZonaSchedule — regra de agendamento da zona do pedido (sync com zona_schedule.go →
// zonaScheduleResponse). dias = DOW permitidos (0=domingo..6=sábado, convenção PHP
// date('w')/Postgres EXTRACT(DOW)); cutoffs = "HH:MM" por DOW (índice 0..6). Cutoff =
// para entregar no dia D, agendar até cutoffs[DOW(D)] do dia anterior. has_schedule=false
// → zona ausente/sem regra (fail-open: não desabilita por zona, só min/max).
type ZonaSchedule = {
  has_schedule: boolean
  dias: number[]
  cutoffs: string[]
}

// stripCountryCode — remove o código do país (+55) do telefone do CLIENTE antes de
// exibir. Regra: olha só os dígitos; se tiver 12–13 dígitos e começar com "55"
// (DDI Brasil + DDD + número), corta os 2 primeiros. Senão devolve como veio.
// Espelha o $sz4mb_fmt_phone do portal V1 (CLAUDE.md → "Telefone — strip +55").
function stripCountryCode(phone: string): string {
  if (!phone) return ''
  const d = phone.replace(/\D/g, '')
  if ((d.length === 12 || d.length === 13) && d.startsWith('55')) {
    return d.slice(2)
  }
  return phone
}

const STATUS_OPTIONS = [
  { value: '__logistico', label: 'Em expedição' },
  { value: '__entregues', label: 'Entregues' },
  { value: 'pre_agendado', label: 'Pré-agendado' },
  { value: 'agendado', label: 'Agendado' },
  { value: 'embalado', label: 'Embalado' },
  { value: 'em_rota', label: 'A caminho' },
  { value: 'frustrado', label: 'Frustrado' },
  { value: 'cancelado', label: 'Cancelado' },
]

// Rótulo do filtro: 'em_rota' aparece como "A caminho" (pedido do dono). Apenas
// label — value permanece 'em_rota'. NÃO alterar o statusLabel global (fonte única
// de cor/label do admin); badges das linhas seguem exibindo "Em rota".
const filterStatusLabel = (s: string): string =>
  STATUS_OPTIONS.find(opt => opt.value === s)?.label ?? (s === 'em_rota' ? 'A caminho' : statusLabel(s))

function displayStatus(s: string): string {
  const key = (s || '').trim().toLowerCase().replace(/^wc-/, '').replace(/[-_\s]+/g, '')
  return key === 'acaminho' ? ['A', 'Caminho'].join(' ') : statusLabel(s)
}

function financialStatusLabel(s?: string | null): string {
  return s ? statusLabel(s) : 'Sem status financeiro'
}

// ── Exportar relatório de pedidos (CSV conforme filtros) ────────────────────────
// CSV pt-BR de boa legibilidade: BOM UTF-8 (Excel/Sheets reconhecem acentos),
// delimitador ';' (padrão pt-BR), números com vírgula decimal (Excel soma direto),
// datas DD/MM/AAAA. Colunas fixas e ordenadas; valores escapados (aspas duplicadas)
// p/ não quebrar quando o conteúdo tem ';', '"' ou quebra de linha.
function csvEscape(v: string): string {
  const s = v ?? ''
  return /[";\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}
function csvNum(v: number | null | undefined): string {
  return (Number.isFinite(v as number) ? (v as number) : 0).toFixed(2).replace('.', ',')
}
const ORDERS_CSV_COLUMNS: { header: string; get: (p: P) => string }[] = [
  { header: 'Pedido', get: p => String(p.wc_order_id ?? p.id) },
  { header: 'Data', get: p => brDate(p.created_at) },
  { header: 'Status logístico', get: p => statusLabel(p.status) },
  { header: 'Status financeiro', get: p => p.financial_status ? financialStatusLabel(p.financial_status) : '' },
  { header: 'Data pagamento agendado', get: p => (p.scheduled_payment_date ? brDate(p.scheduled_payment_date) : '') },
  { header: 'Cliente', get: p => p.cliente_nome || p.dest_nome || '' },
  { header: 'Telefone', get: p => p.cliente_telefone || '' },
  { header: 'CEP', get: p => p.dest_cep || '' },
  { header: 'Cidade', get: p => p.dest_cidade || '' },
  { header: 'UF', get: p => p.dest_uf || '' },
  { header: 'Produto', get: p => normalizeProductLabel(p.oferta_nome, p.produto, p.produto_qtd) },
  { header: 'Variação', get: p => normalizeProductName(p.variacao || '') },
  { header: 'Oferta', get: p => p.oferta_nome || '' },
  { header: 'Valor da oferta (R$)', get: p => csvNum(p.oferta_valor) },
  { header: 'Valor do pedido (R$)', get: p => csvNum(p.valor) },
  // comissao = LÍQUIDA do afiliado (já com o 4,99% descontado — regra #1587).
  { header: 'Comissão líquida (R$)', get: p => csvNum(p.comissao) },
  { header: 'Taxa motoboy (R$)', get: p => csvNum(p.taxa_motoboy) },
  { header: 'Afiliado', get: p => p.afiliado_nome || '' },
  { header: 'Data de entrega', get: p => (p.delivery_date ? brDate(p.delivery_date) : '') },
]
function buildOrdersCSV(items: P[]): string {
  const head = ORDERS_CSV_COLUMNS.map(c => csvEscape(c.header)).join(';')
  const rows = items.map(p => ORDERS_CSV_COLUMNS.map(c => csvEscape(c.get(p))).join(';'))
  return '﻿' + [head, ...rows].join('\r\n') + '\r\n'
}

// Unidades federativas (UFs) para o filtro de Estado. Enviadas como `uf` na querystring.
const UF_OPTIONS = ['AC', 'AL', 'AP', 'AM', 'BA', 'CE', 'DF', 'ES', 'GO', 'MA', 'MT', 'MS', 'MG', 'PA', 'PB', 'PR', 'PE', 'PI', 'RJ', 'RN', 'RS', 'RO', 'RR', 'SC', 'SP', 'SE', 'TO']

// Status onde a ação Cancelar fica disponível (admin-ui apenas; produtor/afiliado
// no portal segue restrito a agendado/embalado — ver motoboy_portal.go).
const RESCHEDULABLE = new Set(['pre_agendado', 'agendado', 'embalado', 'em_rota'])
// Status onde Reagendar fica disponível.
const REAGENDAVEL = new Set(['agendado', 'embalado', 'em_rota'])

// orderLabel — número exibível do pedido (wc_order_id, ou id como fallback). Pedido
// reagendado por cópia é um pedido normal como outro qualquer — SEM marcador (REGRA
// DO DONO 2026-07-21: idêntico ao original, só muda a data, nada de diferenciar).
function orderLabel(p: P): string {
  return String(p.wc_order_id ?? p.id)
}

// Data/hora em pt-BR (DD/MM/AAAA HH:mm) no fuso de Brasília. BUG-FIX 2026-07-21:
// nem toda coluna do banco é timestamptz — `mp.created_at`/`sz_motoboy_audit` são
// `timestamp` SEM fuso (naive) e a sessão do Postgres é America/Sao_Paulo, então o
// texto que chega ("2026-07-21 22:02:14", sem offset) JÁ é a hora de Brasília. A
// versão antiga assumia "sem offset = UTC" e appendava 'Z' cegamente, subtraindo 3h
// à toa (22:02 virava 19:02). Agora: sem offset → usa os campos direto, SEM conversão;
// com offset explícito (timestamptz, ex. sz_orders) → converte de verdade pra SP.
function fmtDateTime(s: string): string {
  if (!s) return '—'
  const m = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:?\d{2})?$/.exec(s.trim())
  if (!m) return s
  const [, y, mo, da, hh, mi, ss, off] = m
  if (!off) {
    return `${da}/${mo}/${y}, ${hh}:${mi}`
  }
  const offNorm = off === 'Z' ? 'Z' : off.length === 5 ? off : `${off.slice(0, 3)}:${off.slice(3)}`
  const d = new Date(`${y}-${mo}-${da}T${hh}:${mi}:${ss}${offNorm}`)
  if (isNaN(d.getTime())) return s
  return d.toLocaleString('pt-BR', { timeZone: 'America/Sao_Paulo', day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function stripLeadingQty(value: string): string {
  return (value || '').trim().replace(/^\d+\s+/, '').trim()
}

function extractLeadingQty(...values: string[]): string {
  for (const value of values) {
    const m = (value || '').trim().match(/^(\d+)\s+/)
    if (m) return m[1]
  }
  return '—'
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

function formatCommissionPct(value?: number): string {
  const raw = Number.isFinite(value as number) ? (value as number) : 0
  const pct = raw <= 1 ? raw * 100 : raw
  const rounded = Math.round(pct * 100) / 100
  return `${Number.isInteger(rounded) ? rounded.toFixed(0) : rounded.toFixed(2)}%`
}

// Normaliza o nome do produto para exibição na lista:
// "3 Datalaprox" -> "3 Datalaprox"
// "Datalaprox"   -> "3 Datalaprox" quando a quantidade vem da oferta.
// qtdReal — produto_qtd do backend (fonte de verdade: coluna sz_order_items.
// quantidade). Quando presente, SUBSTITUI o número adivinhado do texto —
// AUDIT-2026-07-11: oferta_nome pode ser só marketing (ex.: "3 potes + 1
// brinde" tem qtd real 4, não 3) e pedidos antigos guardam a qtd só na
// coluna, não no texto. Sem qtdReal (chamadas legadas), cai no texto-guess.
function normalizeProductLabel(ofertaNome: string, produto: string, qtdReal?: number): string {
  const clean = stripLeadingQty(produto) || stripLeadingQty(ofertaNome)
  const baseName = normalizeProductName(clean)
  if (!baseName) return '—'
  if (qtdReal !== undefined) {
    return `${qtdReal} ${baseName}`
  }
  const qtd = extractLeadingQty(ofertaNome, produto)
  return qtd === '—' ? baseName : `${qtd} ${baseName}`
}

function defaultRange() {
  const today = new Date()
  const past = new Date(today)
  past.setDate(past.getDate() - 30)
  return { ini: ymd(past), fim: ymd(today) }
}

// next30Days — lista das próximas 30 datas a partir de hoje (inclusive).
// HORÁRIO LIMITE: para entregar numa data, o agendamento tem de ser feito até
// 21h do dia ANTERIOR. Logo "Hoje" fica sempre bloqueado (o cutoff de ontem já
// passou). Cutoff por região/zona já existe (zona_schedule.go → GET .../zona-schedule,
// consumido pelo FalkDatePicker) — 21h aqui é só o fallback global usado quando a
// zona do pedido não tem regra própria (fail-open, ver linha ~1091).
const CUTOFF_HOUR = 21
function next30Days(): { iso: string; dow: string; label: string; preAgendado: boolean; bloqueado: boolean }[] {
  const dows = ['Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb']
  const out: { iso: string; dow: string; label: string; preAgendado: boolean; bloqueado: boolean }[] = []
  const now = new Date()
  const base = new Date()
  base.setHours(0, 0, 0, 0)
  let biz = 0 // dias úteis acumulados a partir de hoje
  for (let i = 0; i < 30; i++) {
    const d = new Date(base)
    d.setDate(base.getDate() + i)
    const wd = d.getDay()
    if (i > 0 && wd !== 0 && wd !== 6) biz++ // conta dia útil (seg–sex), exclui hoje
    const cutoff = new Date(d)
    cutoff.setDate(d.getDate() - 1)
    cutoff.setHours(CUTOFF_HOUR, 0, 0, 0)
    out.push({
      iso: ymd(d),
      dow: dows[wd],
      label: i === 0 ? 'Hoje' : i === 1 ? 'Amanhã' : brDate(ymd(d)),
      // >5 dias úteis de distância = PRÉ-AGENDADO; até 5 = AGENDADO.
      preAgendado: biz > 5,
      // bloqueado se já passou o cutoff (21h do dia anterior).
      bloqueado: now > cutoff,
    })
  }
  return out
}

// zonaDiasLabel — rótulo PT-BR dos DOW da zona (0=dom..6=sáb), p/ a dica do reagendamento.
// Ex.: [1,2,3,4,5,6] → "Seg, Ter, Qua, Qui, Sex, Sáb".
function zonaDiasLabel(dias: number[]): string {
  const nomes = ['Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb']
  return [...dias].sort((a, b) => a - b).map(d => nomes[d] ?? String(d)).join(', ')
}

// makeZoneDateDisabled — fábrica do predicado de desabilitação POR ZONA, usada pelo picker
// de REAGENDAR. Espelha EXATO o gate do backend (zona_schedule.go → dateAllowed):
//   data permitida ⇔ DOW(data) ∈ zona.dias  E  agora ≤ (data − 1 dia) às cutoffs[DOW(data)].
// DOW: 0=domingo..6=sábado (getDay() local, mesma convenção do banco PHP date('w')).
// Fail-open: sem schedule (null / has_schedule=false) → undefined (não desabilita por zona).
function makeZoneDateDisabled(zona: ZonaSchedule | null): ((iso: string) => boolean) | undefined {
  if (!zona || !zona.has_schedule) return undefined
  const dias = zona.dias
  const cutoffs = zona.cutoffs
  return (iso: string) => {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso)
    if (!m) return false
    const y = Number(m[1]); const mo = Number(m[2]) - 1; const da = Number(m[3])
    // Data em hora local (construtor numérico — sem pitfall UTC).
    const d = new Date(y, mo, da)
    const dow = d.getDay() // 0=domingo..6=sábado
    if (!dias.includes(dow)) return true // zona não entrega neste dia da semana
    // Cutoff: deadline = (data − 1 dia) às cutoffs[dow]. Bloqueia se agora já passou.
    const hhmm = (cutoffs[dow] || '21:00').split(':')
    const hh = Number(hhmm[0]) || 21
    const mm = Number(hhmm[1]) || 0
    const deadline = new Date(y, mo, da - 1, hh, mm, 0, 0)
    return new Date() > deadline
  }
}

// fmtCutoffLabel — "21:00" → "21h", "12:30" → "12:30". Limite legível por dia.
function fmtCutoffLabel(hhmm: string): string {
  const m = /^(\d{1,2}):(\d{2})$/.exec((hhmm || '').trim())
  if (!m) return hhmm || '21h'
  return m[2] === '00' ? `${Number(m[1])}h` : `${Number(m[1])}:${m[2]}`
}

// cutoffForIso — horário limite (cutoffs[DOW]) da zona p/ a data iso; null se a zona
// não tem schedule. Espelha o índice DOW de makeZoneDateDisabled (getDay local).
function cutoffForIso(zona: ZonaSchedule | null, iso: string): string | null {
  if (!zona || !zona.has_schedule) return null
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso)
  if (!m) return null
  const dow = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).getDay()
  return fmtCutoffLabel(zona.cutoffs[dow] || '21:00')
}

export default function Orders() {
  const init = useMemo(defaultRange, [])
  const days30 = useMemo(next30Days, [])

  // ?produtor_id=N na URL — chegando do botão "Ver pedidos" da tela Carteiras >
  // Clientes (TpcClientes.tsx). Filtra a lista pra só os pedidos daquele produtor.
  const [searchParams] = useSearchParams()
  const produtorIdFiltro = searchParams.get('produtor_id') || ''

  // Dados
  const [items, setItems] = useState<P[]>([])

  // Unidades físicas vendidas na lista atual — usa produto_qtd (fonte real do
  // backend: sz_order_items.quantidade, já com o multiplicador de kit
  // aplicado). NÃO adivinha pelo texto do nome/oferta (AUDIT-2026-07-11:
  // pedidos antigos guardam a qtd real só na coluna, não no texto — e o texto
  // da oferta pode ser só marketing, ex. "3 potes + 1 brinde" = 4 unidades).
  const totalUnidadesVendidas = useMemo(() => {
    return items.reduce((sum, p) => sum + (p.produto_qtd ?? 1), 0)
  }, [items])

  const [loading, setLoading] = useState(true)
  const [err, setErr]     = useState('')
  const [busyId, setBusyId] = useState<number | null>(null)
  const [exporting, setExporting] = useState(false)

  // Opções dos filtros "Produto" e "Afiliado" — carregadas uma vez no mount.
  // Regra do dono: filtros de texto viram SELECT (sem digitação livre). O value
  // enviado ao backend continua sendo o NOME (casa exato com a busca ILIKE atual).
  // Shapes REAIS confirmados: /products → { items: [{ id, nome }], total }
  // (ver Stock.tsx); /affiliates → { items: [{ nome, email, ... }], total }
  // (ver Affiliates.tsx). Erro é silenciado → o filtro fica só com "Todos".
  const [produtoOpts, setProdutoOpts] = useState<{ id: number; nome: string }[]>([])
  const [afiliadoOpts, setAfiliadoOpts] = useState<{ nome: string; email: string }[]>([])

  useEffect(() => {
    let active = true
    api<{ items: { id: number; nome: string }[] }>('/products?limit=300')
      .then(r => { if (active) setProdutoOpts(r.items ?? []) })
      .catch(() => { /* degrada para só "Todos" — não quebra a página */ })
    api<{ items: { nome: string; email: string }[] }>('/affiliates?limit=300')
      .then(r => { if (active) setAfiliadoOpts(r.items ?? []) })
      .catch(() => { /* degrada para só "Todos" — não quebra a página */ })
    return () => { active = false }
  }, [])

  // Estado dos filtros (rascunho dentro do drawer)
  const [draftStatus,  setDraftStatus]  = useState('')
  const [draftDataIni, setDraftDataIni] = useState(init.ini)
  const [draftDataFim, setDraftDataFim] = useState(init.fim)
  const [draftCidade,  setDraftCidade]  = useState('')
  const [draftUf,      setDraftUf]      = useState('')
  const [draftProduto, setDraftProduto] = useState('')
  const [draftAfiliado, setDraftAfiliado] = useState('')
  const [draftSearch,  setDraftSearch]  = useState('')
  const [draftStopped, setDraftStopped] = useState(false)

  // Filtros aplicados (disparam fetch)
  const [status,  setStatus]  = useState('')
  const [dataIni, setDataIni] = useState(init.ini)
  const [dataFim, setDataFim] = useState(init.fim)
  const [cidade,  setCidade]  = useState('')
  const [uf,      setUf]      = useState('')
  const [produto, setProduto] = useState('')
  const [afiliado, setAfiliado] = useState('')
  const [search,  setSearch]  = useState('')
  const [stopped, setStopped] = useState(false)

  // UI
  const [filterOpen,    setFilterOpen]    = useState(false)
  const [selectedOrder, setSelectedOrder] = useState<P | null>(null)
  const [detailTab, setDetailTab] = useState<'resumo' | 'utm' | 'historico' | 'nfe'>('resumo')

  // ── Bulk confirm panel (status via seleção) ────────────────────────
  type BulkStatusAction = 'entregue' | 'frustrado' | 'em_rota'
  const [bulkConfirm,  setBulkConfirm]  = useState<BulkStatusAction | null>(null)
  const [bulkJustif,   setBulkJustif]   = useState('')
  const [bulkMotivo,   setBulkMotivo]   = useState('')
  const [bulkForceBusy, setBulkForceBusy] = useState(false)
  const [hasPrinted,   setHasPrinted]   = useState(false)
  // Imprimir por data de entrega (header da página)
  const [printDate,    setPrintDate]    = useState('')
  const [printBusy,    setPrintBusy]    = useState(false)

  // ── Drawer: mudar status do pedido (seção na base do Resumo) ────────────────
  type DrawerAction = 'entregue' | 'frustrado' | 'em_rota'
  const [drawerAction, setDrawerAction]     = useState<DrawerAction | null>(null)
  const [drawerJustif, setDrawerJustif]     = useState('')
  const [drawerMotivo, setDrawerMotivo]     = useState('')
  const [drawerFile,   setDrawerFile]       = useState<File | null>(null)
  const [drawerEvidURL, setDrawerEvidURL]   = useState('')
  const [drawerUploading, setDrawerUploading] = useState(false)
  const [drawerBusy,  setDrawerBusy]        = useState(false)
  const [drawerErr,   setDrawerErr]         = useState('')

  function resetDrawerAction() {
    setDrawerAction(null); setDrawerJustif(''); setDrawerMotivo('')
    setDrawerFile(null); setDrawerEvidURL(''); setDrawerErr('')
  }

  async function drawerUploadEvidence(orderId: number) {
    if (!drawerFile) return
    setDrawerUploading(true)
    setDrawerErr('')
    try {
      const form = new FormData()
      form.append('evidence_file', drawerFile)
      const tok = getToken()
      const res = await fetch(`${BASE}/orders/${orderId}/upload-evidence`, {
        method: 'POST',
        headers: tok ? { Authorization: `Bearer ${tok}` } : {},
        body: form,
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data?.error?.message || 'Falha no upload')
      setDrawerEvidURL(data.url)
    } catch (e: unknown) {
      setDrawerErr((e as Error).message || 'Falha no upload')
    } finally {
      setDrawerUploading(false)
    }
  }

  async function drawerForceStatus(p: P) {
    if (!drawerAction) return
    if (!drawerJustif.trim()) { setDrawerErr('Justificativa obrigatória.'); return }
    if (drawerAction === 'frustrado' && !drawerMotivo) { setDrawerErr('Motivo obrigatório.'); return }
    if ((drawerAction === 'entregue' || drawerAction === 'frustrado') && !drawerEvidURL) {
      setDrawerErr('Faça o upload do comprovante antes de confirmar.'); return
    }
    setDrawerBusy(true); setDrawerErr('')
    try {
      await api(`/orders/${p.sz_order_id}/force-motoboy-status`, {
        method: 'POST',
        body: JSON.stringify({
          target_status: drawerAction,
          motivo: drawerMotivo,
          observacao: drawerJustif,
          comprov_url: drawerEvidURL,
        }),
      })
      const labels: Record<DrawerAction, string> = { entregue: 'Entregue', frustrado: 'Frustrado', em_rota: 'Em rota' }
      emitToast('ok', `Pedido ${p.wc_order_id ?? p.id} → ${labels[drawerAction]}.`)
      resetDrawerAction()
      setSelectedOrder(null)
      load()
    } catch (e: unknown) {
      setDrawerErr((e as Error).message || 'Falha ao alterar status.')
    } finally {
      setDrawerBusy(false)
    }
  }

  async function drawerChangeMotoboy(p: P) {
    const motoboyId = drawerMotoboyId ? Number(drawerMotoboyId) : null
    if (!motoboyId) {
      setDrawerErr('Selecione um motoboy.')
      return
    }
    if (!p.sz_order_id) {
      setDrawerErr('Pedido sem vínculo para troca de motoboy.')
      return
    }
    setDrawerBusy(true)
    setDrawerErr('')
    try {
      await api(`/orders/${p.sz_order_id}/change-motoboy`, {
        method: 'POST',
        body: JSON.stringify({ motoboy_id: motoboyId }),
      })
      emitToast('ok', `Motoboy atualizado para ${motoboys.find(m => m.id === motoboyId)?.nome ?? `#${motoboyId}`}.`)
      setSelectedOrder(prev => prev ? { ...prev, motoboy_id: motoboyId } : prev)
      load()
    } catch (e: unknown) {
      setDrawerErr((e as Error).message || 'Falha ao trocar motoboy.')
    } finally {
      setDrawerBusy(false)
    }
  }

  // Reagendar — drawer lateral com calendário de 30 dias.
  const [rescheduleOrder, setRescheduleOrder] = useState<P | null>(null)
  const [rescheduleDate, setRescheduleDate]   = useState('')

  // Reagendar FRUSTRADO via CLONE — data escolhida inline no Resumo do drawer.
  // Não apaga o original; o backend cria uma CÓPIA com a nova data de entrega.
  const [cloneDate, setCloneDate] = useState('')

  // Schedule da ZONA do pedido (dias de funcionamento + cutoff por dia) — REGRA DO DONO
  // 2026-06-23: a elegibilidade da data de reagendamento passa a ser pela zona, não pela
  // regra global (CUTOFF_HOUR=21 + dias úteis). Carregado de GET /orders/motoboy/{id}/
  // zona-schedule ({id} = pedido motoboy). has_schedule=false → fail-open (cai no min-only).
  // Este é keyed por selectedOrder (picker de clone do FRUSTRADO no drawer de DETALHE).
  const [zonaSchedule, setZonaSchedule] = useState<ZonaSchedule | null>(null)

  // Schedule da ZONA do pedido de REAGENDAR (drawer próprio, keyed por rescheduleOrder, NÃO
  // por selectedOrder — openReschedule faz setSelectedOrder(null), então o zonaSchedule acima
  // não serve). Carregado de GET /orders/motoboy/{id}/zona-schedule. has_schedule=false →
  // fail-open (cai no comportamento atual: dias úteis, sem desabilitar por zona).
  const [reschedZona, setReschedZona] = useState<ZonaSchedule | null>(null)

  // Detalhe enriquecido (lazy fetch GET /orders/{sz_order_id}).
  // sz_order_id é a PK em sz_orders — NÃO confundir com selectedOrder.id (pedido motoboy).
  const [detail, setDetail]               = useState<OrderDetailLite | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  // ── Bulk (ações em lote) ─────────────────────────────────────────────────────
  type MotoboyOpt = { id: number; nome: string }
  const [motoboys, setMotoboys]   = useState<MotoboyOpt[]>([])
  const [selMotoboy, setSelMotoboy] = useState('')
  const [drawerMotoboyId, setDrawerMotoboyId] = useState('')
  const [bulkBusy, setBulkBusy]   = useState(false)
  const bulk = useBulkSelection(
    items
      .filter(p => p.status === 'agendado' || p.status === 'embalado' || p.status === 'em_rota' || p.status === 'a_caminho')
      .map(p => p.id)
  )
  const _bulkLoadedMb = useRef(false)

  function motoboyChangeableStatus(status: string) {
    return status === 'embalado' || status === 'em_rota' || status === 'a_caminho'
  }

  useEffect(() => {
    if (!selectedOrder) { setDetail(null); setZonaSchedule(null); return }
    setDetailTab('resumo')
    setCloneDate('')
    resetDrawerAction()

    // Schedule da ZONA — keyed pelo id do pedido MOTOBOY (selectedOrder.id), não pelo
    // sz_order_id. Vale p/ originais E clones órfãos (clone não tem sz_order_id). Só
    // carrega p/ frustrado (única tela que usa o picker de clone). Fail-soft: erro → null
    // (o FalkDatePicker cai no min-only enquanto carrega / se a zona não tiver regra).
    setZonaSchedule(null)
    if (selectedOrder.status === 'frustrado' || selectedOrder.status === 'cancelado') {
      api<ZonaSchedule>(`/orders/motoboy/${selectedOrder.id}/zona-schedule`)
        .then(r => setZonaSchedule(r))
        .catch(() => setZonaSchedule(null))
    }
    setDrawerMotoboyId(selectedOrder.motoboy_id ? String(selectedOrder.motoboy_id) : '')

    const szId = selectedOrder.sz_order_id
    if (!szId) { setDetail(null); return }
    setDetailLoading(true)
    setDetail(null)
    api<OrderDetailLite>(`/orders/${szId}`)
      .then(r => setDetail(r))
      .catch(() => setDetail(null))
      .finally(() => setDetailLoading(false))
  }, [selectedOrder])

  // Schedule da ZONA p/ o drawer de REAGENDAR — keyed pelo id do pedido motoboy do
  // rescheduleOrder (independente do selectedOrder, que openReschedule zera). Carrega p/
  // QUALQUER status (reagendar comum vale p/ agendado/em rota). Fail-soft: erro/404 →
  // null → o picker cai no comportamento atual (dias úteis, sem desabilitar por zona).
  // authRedirect:false p/ não derrubar a sessão num 401/404.
  useEffect(() => {
    if (!rescheduleOrder) { setReschedZona(null); return }
    let active = true
    setReschedZona(null)
    api<ZonaSchedule>(`/orders/motoboy/${rescheduleOrder.id}/zona-schedule`, {}, { authRedirect: false })
      .then(r => { if (active) setReschedZona(r) })
      .catch(() => { if (active) setReschedZona(null) })
    return () => { active = false }
  }, [rescheduleOrder])

  function showToast(kind: 'ok' | 'err', msg: string) {
    emitToast(kind, msg)
  }

  function buildQS() {
    const p = new URLSearchParams()
    if (status) p.set('status', status)
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    if (cidade) p.set('cidade', cidade)
    if (uf) p.set('uf', uf)
    if (produto) p.set('produto', produto)
    if (afiliado) p.set('afiliado', afiliado)
    if (search) p.set('s', search)
    if (stopped) p.set('stopped', '1')
    if (produtorIdFiltro) p.set('produtor_id', produtorIdFiltro)
    p.set('limit', '100')
    return p.toString()
  }

  function load() {
    setErr('')
    setLoading(true)
    api<{ items: P[] }>(`/orders/motoboy?${buildQS()}`)
      .then(r => setItems(r.items || []))
      .catch(e => setErr(e.message))
      .finally(() => setLoading(false))
    // Load motoboys once per session
    if (!_bulkLoadedMb.current) {
      _bulkLoadedMb.current = true
      api<{ items: MotoboyOpt[] }>('/motoboys')
        .then(r => setMotoboys(r.items || []))
        .catch(() => {})
    }
  }

  // ── Ações em lote ───────────────────────────────────────────────────────────

  async function handleBulkEmbalar() {
    const targetIds = bulk.ids
      .map(id => items.find(p => p.id === id))
      .filter((p): p is P => !!p && p.status === 'agendado' && !!p.sz_order_id)
      .map(p => p.sz_order_id!)
    if (targetIds.length === 0) {
      emitToast('err', 'Nenhum pedido em "agendado" selecionado.')
      return
    }
    const motoboyId = selMotoboy ? Number(selMotoboy) : null
    const semMotoboy = targetIds.filter(szOrderId => {
      const p = items.find(x => x.sz_order_id === szOrderId)
      return p ? !p.motoboy_id : false
    })
    if (motoboyId == null && semMotoboy.length > 0) {
      emitToast('err', `Selecione um motoboy — ${semMotoboy.length} pedido(s) sem motoboy definido.`)
      return
    }
    const nomeMb = motoboyId ? (motoboys.find(m => m.id === motoboyId)?.nome ?? '') : ''
    const ok = await confirmAsync({
      variant: 'warning',
      title: 'Embalar pedidos',
      message: `Embalar ${targetIds.length} pedido(s) e gerar etiqueta?` +
        (nomeMb ? `\n\nMotoboy: ${nomeMb}` : '') +
        '\n\nStatus passará para "embalado".',
      confirmLabel: 'Embalar',
    })
    if (!ok) return
    setBulkBusy(true)
    try {
      const r = await api<{ ok: boolean; embalado: number; skipped: number }>(
        '/bulk-actions/motoboy-generate-labels',
        { method: 'POST', body: JSON.stringify({ order_ids: targetIds, motoboy_id: motoboyId }) }
      )
      emitToast('ok', `${r.embalado} embalado(s)${r.skipped ? ` · ${r.skipped} ignorado(s)` : ''}`)
      bulk.clear()
      load()
    } catch (e: any) {
      emitToast('err', e.message || 'Falha ao embalar')
    } finally {
      setBulkBusy(false)
    }
  }

  async function handleBulkTrocarMotoboy() {
    const motoboyId = selMotoboy ? Number(selMotoboy) : null
    if (motoboyId == null) {
      emitToast('err', 'Selecione um motoboy.')
      return
    }
    const targetIds = bulk.ids.filter(id => {
      const p = items.find(x => x.id === id)
      return !!p && motoboyChangeableStatus(p.status)
    })
    if (targetIds.length === 0) {
      emitToast('err', 'Nenhum pedido elegível para trocar motoboy.')
      return
    }
    const nomeMb = motoboys.find(m => m.id === motoboyId)?.nome ?? ''
    const ok = await confirmAsync({
      variant: 'warning',
      title: 'Trocar motoboy',
      message: `Trocar o motoboy de ${targetIds.length} pedido(s)?` +
        (nomeMb ? `\n\nNovo motoboy: ${nomeMb}` : '') +
        '\n\nOs status atuais serão preservados.',
      confirmLabel: 'Trocar',
    })
    if (!ok) return
    setBulkBusy(true)
    try {
      let okCount = 0
      for (const id of targetIds) {
        const p = items.find(x => x.id === id)
        if (!p?.sz_order_id) continue
        await api(`/orders/${p.sz_order_id}/change-motoboy`, {
          method: 'POST',
          body: JSON.stringify({ motoboy_id: motoboyId }),
        })
        okCount++
      }
      emitToast('ok', `${okCount} pedido(s) com motoboy atualizado(s).`)
      bulk.clear()
      load()
    } catch (e: any) {
      emitToast('err', e.message || 'Falha ao trocar motoboy')
    } finally {
      setBulkBusy(false)
    }
  }

  function handleBulkImprimir() {
    const toprint = bulk.ids
      .map(id => items.find(p => p.id === id))
      .filter((p): p is P => !!p && p.status === 'embalado' && !!p.package_code)
    if (toprint.length === 0) {
      emitToast('err', 'Nenhum pedido "embalado" selecionado.')
      return
    }
    const esc = (s: string) => String(s ?? '').replace(/[&<>"]/g, c =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] as string))
    const fmtR = (v: number) => v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    const pages = toprint.map(p => {
      const qrSrc = 'https://api.qrserver.com/v1/create-qr-code/?size=150x150&data=' + encodeURIComponent(p.package_code)
      const endereco = [p.dest_endereco, p.dest_numero].filter(Boolean).join(', ')
      const telD = (p.cliente_telefone || '').replace(/\D+/g, '')
      const telFmt = (telD.length === 12 || telD.length === 13) && telD.startsWith('55') ? telD.slice(2) : telD
      return `<div class="etq">
        <p class="ped">Pedido ${esc(String(p.wc_order_id ?? p.id))}</p>
        <p class="nome">${esc(p.dest_nome || p.cliente_nome)}</p>
        ${p.dest_produto ? `<p class="lin"><strong>${esc(p.dest_produto)}</strong></p>` : ''}
        <p class="lin">${esc(endereco)}</p>
        ${p.dest_complemento ? `<p class="lin">Compl.: ${esc(p.dest_complemento)}</p>` : ''}
        <p class="lin">${esc([p.dest_bairro, p.dest_cidade, p.dest_uf].filter(Boolean).join(' · '))}</p>
        <p class="lin">CEP: ${esc(p.dest_cep)}</p>
        ${telFmt ? `<p class="lin">Tel.: ${esc(telFmt)}</p>` : ''}
        <p class="lin"><strong>Valor: R$ ${esc(fmtR(p.valor))}</strong></p>
        <div class="qr"><img src="${qrSrc}" alt="QR" width="150" height="150"><div class="code">${esc(p.package_code)}</div></div>
      </div>`
    }).join('<div style="page-break-after:always"></div>')
    const win = window.open('', '_blank', 'width=500,height=700')
    if (!win) { emitToast('err', 'Bloqueado pelo navegador — permita pop-ups.'); return }
    win.document.write(`<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><title>Etiquetas</title>
<style>*{box-sizing:border-box;font-family:Arial,Helvetica,sans-serif}body{margin:0;padding:16px;color:#111}
.etq{border:2px solid #111;border-radius:8px;padding:14px;max-width:360px;margin-bottom:24px}
.ped{font-size:20px;font-weight:800;margin:0 0 4px}
.nome{font-size:16px;font-weight:700;margin:0 0 2px}.lin{font-size:13px;margin:1px 0}.code{font-size:11px;word-break:break-all;margin-top:4px}
.qr{text-align:center;margin-top:12px}@media print{.etq{page-break-inside:avoid}}</style></head>
<body>${pages}<div style="text-align:center;margin-top:20px"><button onclick="window.print()">Imprimir tudo</button></div></body></html>`)
    win.document.close()
    setHasPrinted(true)
  }

  // ── Bulk force-status (Entregue / Frustrado) ────────────────────────────────

  async function handleBulkForceStatus(target: BulkStatusAction) {
    if (!bulkJustif.trim()) { setBulkJustif(''); return }
    if (target === 'frustrado' && !bulkMotivo) return
    const elegivel = bulk.ids
      .map(id => items.find(p => p.id === id))
      .filter((p): p is P => {
        if (!p || !p.sz_order_id) return false
        if (target === 'em_rota') return p.status === 'embalado'
        if (target === 'entregue') return p.status === 'em_rota'
        if (target === 'frustrado') return p.status === 'em_rota'
        return false
      })
    if (elegivel.length === 0) { emitToast('err', 'Nenhum pedido elegível.'); return }
    setBulkForceBusy(true)
    let ok = 0; let fail = 0
    for (const p of elegivel) {
      try {
        await api(`/orders/${p.sz_order_id}/force-motoboy-status`, {
          method: 'POST',
          body: JSON.stringify({ target_status: target, motivo: bulkMotivo, observacao: bulkJustif }),
        })
        ok++
      } catch { fail++ }
    }
    setBulkForceBusy(false)
    setBulkConfirm(null); setBulkJustif(''); setBulkMotivo('')
    bulk.clear(); setHasPrinted(false)
    emitToast(fail === 0 ? 'ok' : 'err',
      fail === 0 ? `${ok} pedido(s) → ${target === 'entregue' ? 'Entregue' : target === 'frustrado' ? 'Frustrado' : 'Em rota'}.`
        : `${ok} ok · ${fail} falha(s).`)
    load()
  }

  // ── Imprimir por data de entrega (header) ────────────────────────────────────
  async function handlePrintByDate() {
    if (!printDate) return
    setPrintBusy(true)
    try {
      const r = await api<{ items: Array<{
        pedido_id: number; order_id: number; dest_nome: string; customer_name?: string
        dest_produto?: string; dest_endereco?: string; dest_numero?: string
        dest_complemento?: string; dest_bairro?: string; dest_cidade?: string
        dest_uf?: string; dest_cep?: string; dest_telefone?: string
        package_code?: string; total: number
      }> }>(`/motoboy-etiquetas?date=${printDate}&status=embalado`)
      const rows = r.items ?? []
      if (rows.length === 0) { emitToast('info', `Nenhuma etiqueta embalada para ${brDate(printDate)}.`); return }
      const esc = (s: string) => String(s ?? '').replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c] as string))
      const fmt2 = (v: number) => v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
      const pages = rows.map(o => {
        const qrSrc = o.package_code ? 'https://api.qrserver.com/v1/create-qr-code/?size=150x150&data=' + encodeURIComponent(o.package_code) : ''
        const telD = (o.dest_telefone || '').replace(/\D+/g, '')
        const tel = (telD.length === 12 || telD.length === 13) && telD.startsWith('55') ? telD.slice(2) : telD
        const end = [o.dest_endereco, o.dest_numero].filter(Boolean).join(', ')
        return `<div class="etq"><p class="ped">Pedido ${esc(String(o.order_id))}</p>
<p class="nome">${esc(o.dest_nome || o.customer_name || '')}</p>
${o.dest_produto ? `<p class="lin"><strong>${esc(o.dest_produto)}</strong></p>` : ''}
${end ? `<p class="lin">${esc(end)}</p>` : ''}
${o.dest_complemento ? `<p class="lin">Compl.: ${esc(o.dest_complemento)}</p>` : ''}
<p class="lin">${esc([o.dest_bairro,o.dest_cidade,o.dest_uf].filter(Boolean).join(' · '))}</p>
<p class="lin">CEP: ${esc(o.dest_cep||'')}</p>
${tel?`<p class="lin">Tel.: ${esc(tel)}</p>`:''}
<p class="lin"><strong>Valor: R$ ${esc(fmt2(o.total))}</strong></p>
${qrSrc?`<div class="qr"><img src="${qrSrc}" alt="QR" width="150" height="150"><div class="code">${esc(o.package_code!)}</div></div>`:''}</div>`
      }).join('<div style="page-break-after:always"></div>')
      const w = window.open('', '_blank')
      if (w) {
        w.document.write(`<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><title>Etiquetas ${printDate}</title>
<style>*{box-sizing:border-box;font-family:Arial,Helvetica,sans-serif}body{margin:0;padding:16px;color:#111}
.etq{border:2px solid #111;border-radius:8px;padding:14px;max-width:360px;margin:0 auto 24px}
.ped{font-size:20px;font-weight:800;margin:0 0 4px}.nome{font-size:16px;font-weight:700;margin:0 0 2px}
.lin{font-size:13px;margin:1px 0}.qr{text-align:center;margin-top:12px}.code{font-size:11px;margin-top:4px;word-break:break-all}
@media print{button{display:none}.etq{page-break-after:always;border:none}}</style></head>
<body>${pages}<div style="text-align:center;margin-top:12px"><button onclick="window.print()">Imprimir todas</button></div></body></html>`)
        w.document.close(); w.focus(); setTimeout(() => w.print(), 600)
      }
    } catch (e) {
      emitToast('err', (e as Error).message || 'Falha ao carregar etiquetas.')
    } finally {
      setPrintBusy(false)
    }
  }

  // Exporta os pedidos que casam com os filtros ATUAIS em CSV. Refaz o fetch com
  // limit alto (200 = teto do backend) p/ pegar além da página exibida; monta o CSV
  // client-side (controle total do layout) e dispara o download. Não altera a tela.
  async function exportCSV() {
    if (exporting) return
    setExporting(true)
    try {
      const p = new URLSearchParams(buildQS())
      p.set('limit', '200') // teto atual do backend; cobre o dataset com folga
      const r = await api<{ items: P[] }>(`/orders/motoboy?${p.toString()}`)
      const rows = r.items || []
      if (rows.length === 0) {
        emitToast('info', 'Nenhum pedido para exportar com esses filtros.')
        return
      }
      const blob = new Blob([buildOrdersCSV(rows)], { type: 'text/csv;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `pedidos-${ymd(new Date())}.csv`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      emitToast('ok', `Relatório exportado (${rows.length} pedido${rows.length > 1 ? 's' : ''}).`)
    } catch (e) {
      emitToast('err', e instanceof Error ? e.message : 'Falha ao exportar o relatório.')
    } finally {
      setExporting(false)
    }
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load() }, [status, dataIni, dataFim, cidade, uf, produto, afiliado, search, stopped, produtorIdFiltro])

  // Sincroniza rascunho ao abrir o drawer
  function openFilterDrawer() {
    setDraftStatus(status)
    setDraftDataIni(dataIni)
    setDraftDataFim(dataFim)
    setDraftCidade(cidade)
    setDraftUf(uf)
    setDraftProduto(produto)
    setDraftAfiliado(afiliado)
    setDraftSearch(search)
    setDraftStopped(stopped)
    setFilterOpen(true)
  }

  function applyFilters() {
    setStatus(draftStatus)
    setDataIni(draftDataIni)
    setDataFim(draftDataFim)
    setCidade(draftCidade)
    setUf(draftUf)
    setProduto(draftProduto)
    setAfiliado(draftAfiliado)
    setSearch(draftSearch)
    setStopped(draftStopped)
    setFilterOpen(false)
  }

  function clearFilters() {
    setDraftStatus('')
    setDraftDataIni(init.ini)
    setDraftDataFim(init.fim)
    setDraftCidade('')
    setDraftUf('')
    setDraftProduto('')
    setDraftAfiliado('')
    setDraftSearch('')
    setDraftStopped(false)
    // Aplica imediatamente
    setStatus('')
    setDataIni(init.ini)
    setDataFim(init.fim)
    setCidade('')
    setUf('')
    setProduto('')
    setAfiliado('')
    setSearch('')
    setStopped(false)
    setFilterOpen(false)
  }

  // Cidades para o filtro <FalkSelect> — derivadas dos pedidos já carregados
  // (regra do dono: filtro de cidade via SELECT, sem digitação livre). Distintas,
  // não vazias, ordenadas. Garante que a cidade já APLICADA / no rascunho continue
  // na lista mesmo que o resultado atual não a contenha (senão o select não
  // conseguiria exibir a opção selecionada).
  const cidadeOpts = useMemo(() => {
    const set = new Set<string>()
    for (const o of items) {
      const c = (o.dest_cidade || '').trim()
      if (c) set.add(c)
    }
    if (cidade.trim()) set.add(cidade.trim())
    if (draftCidade.trim()) set.add(draftCidade.trim())
    return [...set].sort((a, b) => a.localeCompare(b, 'pt-BR'))
  }, [items, cidade, draftCidade])

  // Conta filtros ativos (exclui range padrão)
  const activeFilterCount = [
    status !== '',
    cidade !== '',
    uf !== '',
    produto !== '',
    afiliado !== '',
    search !== '',
    stopped,
    dataIni !== init.ini,
    dataFim !== init.fim,
  ].filter(Boolean).length

  // ── Ações: Reagendar e Cancelar ──────────────────────────────────────────
  // Disparadas a partir da aba "Ações" do drawer de detalhe (única superfície
  // dessas ações — saíram da lista). Fecha o detalhe antes de abrir o drawer de
  // reagendamento para não empilhar dois overlays.
  function openReschedule(p: P) {
    setSelectedOrder(null)
    setRescheduleOrder(p)
    setRescheduleDate('')
  }

  async function confirmReschedule() {
    if (!rescheduleOrder || !rescheduleDate) return
    const p = rescheduleOrder
    setBusyId(p.id)
    try {
      await api(`/orders/motoboy/${p.id}/reagendar`, {
        method: 'POST',
        body: JSON.stringify({ data: rescheduleDate }),
      })
      showToast('ok', `Pedido ${p.wc_order_id ?? p.id} reagendado para ${brDate(rescheduleDate)}.`)
      setRescheduleOrder(null)
      setRescheduleDate('')
      load()
    } catch (e: unknown) {
      showToast('err', (e as Error).message || 'Falha ao reagendar')
    } finally {
      setBusyId(null)
    }
  }

  // cloneFrustrated — REAGENDA um pedido FRUSTRADO criando uma CÓPIA (não apaga o
  // original). O backend copia todos os dados e aplica a nova data de entrega.
  async function cloneFrustrated(p: P) {
    if (!cloneDate) return
    setBusyId(p.id)
    try {
      await api(`/orders/motoboy/${p.id}/reagendar-clone`, {
        method: 'POST',
        body: JSON.stringify({ data: cloneDate }),
      })
      showToast('ok', `Pedido ${p.wc_order_id ?? p.id} reagendado para ${brDate(cloneDate)}.`)
      setSelectedOrder(null)
      setCloneDate('')
      load()
    } catch (e: unknown) {
      showToast('err', (e as Error).message || 'Falha ao reagendar (clone)')
    } finally {
      setBusyId(null)
    }
  }

  // cloneDateDisabled — predicado de desabilitação POR ZONA p/ o FalkDatePicker do clone.
  // Espelha EXATO o gate do backend (zona_schedule.go → dateAllowed):
  //   data permitida ⇔ DOW(data) ∈ zona.dias  E  agora ≤ (data − 1 dia) às cutoffs[DOW(data)].
  // DOW: 0=domingo..6=sábado (getDay() local, mesma convenção do banco PHP date('w')).
  // Fail-open: sem schedule (null / has_schedule=false) → não desabilita por zona (só o
  // min do picker vale). NÃO reimplementa parse de formato — o backend já normaliza
  // dias:number[] + cutoffs:string[7]. Memoizado por zonaSchedule (estável p/ o picker).
  const cloneDateDisabled = useMemo<((iso: string) => boolean) | undefined>(() => {
    if (!zonaSchedule || !zonaSchedule.has_schedule) return undefined
    const dias = zonaSchedule.dias
    const cutoffs = zonaSchedule.cutoffs
    return (iso: string) => {
      const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso)
      if (!m) return false
      const y = Number(m[1]); const mo = Number(m[2]) - 1; const da = Number(m[3])
      // Data em hora local (construtor numérico — sem pitfall UTC).
      const d = new Date(y, mo, da)
      const dow = d.getDay() // 0=domingo..6=sábado
      if (!dias.includes(dow)) return true // zona não entrega neste dia da semana
      // Cutoff: deadline = (data − 1 dia) às cutoffs[dow]. Bloqueia se agora já passou.
      const hhmm = (cutoffs[dow] || '21:00').split(':')
      const hh = Number(hhmm[0]) || 21
      const mm = Number(hhmm[1]) || 0
      const deadline = new Date(y, mo, da - 1, hh, mm, 0, 0)
      return new Date() > deadline
    }
  }, [zonaSchedule])

  // rescheduleDateDisabled — predicado POR ZONA p/ o picker de REAGENDAR (lista days30).
  // REGRA DO DONO 2026-06-23: o reagendar oferecia TODO dia (até domingo) ignorando a agenda
  // da zona; agora esconde os DOW fora de dias_funcionamento (e respeita o cutoff por dia).
  // Keyed por reschedZona (zona do rescheduleOrder). Lógica em makeZoneDateDisabled (espelha
  // o backend). Fail-open: sem schedule → undefined → cai no cutoff global de next30Days.
  const rescheduleDateDisabled = useMemo<((iso: string) => boolean) | undefined>(
    () => makeZoneDateDisabled(reschedZona), [reschedZona])

  async function cancelOrder(p: P) {
    const ok = await confirmAsync({
      variant: 'danger',
      title: 'Cancelar pedido',
      message: `Tem certeza que deseja cancelar o pedido ${p.wc_order_id ?? p.id}${p.cliente_nome ? ` de ${p.cliente_nome}` : ''}? Esta ação não pode ser desfeita.`,
      confirmLabel: 'Cancelar pedido',
    })
    if (!ok) return
    setBusyId(p.id)
    try {
      await api(`/orders/motoboy/${p.id}/cancelar`, { method: 'POST' })
      showToast('ok', `Pedido ${p.wc_order_id ?? p.id} cancelado.`)
      setSelectedOrder(null)
      load()
    } catch (err: unknown) {
      showToast('err', (err as Error).message || 'Falha ao cancelar')
    } finally {
      setBusyId(null)
    }
  }


  // Constrói chips ativos para exibir abaixo do header.
  const activeChips: ActiveChip[] = []
  if (status) activeChips.push({ key: 'status', label: `Status: ${filterStatusLabel(status)}`, onRemove: () => setStatus('') })
  if (cidade) activeChips.push({ key: 'cidade', label: `Cidade: ${cidade}`, onRemove: () => setCidade('') })
  if (uf) activeChips.push({ key: 'uf', label: `Estado: ${uf}`, onRemove: () => setUf('') })
  if (produto) activeChips.push({ key: 'produto', label: `Produto: ${produto}`, onRemove: () => setProduto('') })
  if (afiliado) activeChips.push({ key: 'afiliado', label: `Afiliado: ${afiliado}`, onRemove: () => setAfiliado('') })
  if (search) activeChips.push({ key: 'search', label: `Busca: ${search}`, onRemove: () => setSearch('') })
  if (stopped) activeChips.push({ key: 'stopped', label: 'Parados 24h+', onRemove: () => setStopped(false) })
  if (dataIni !== init.ini) activeChips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni(init.ini) })
  if (dataFim !== init.fim) activeChips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim(init.fim) })

  // Endereço completo do cliente vindo do detalhe (endereco_envio → fallback cobrança).
  const endereco = detail?.order?.endereco_envio?.exists
    ? detail.order.endereco_envio
    : detail?.order?.endereco_cobranca?.exists
    ? detail.order.endereco_cobranca
    : null

  // Breakdown financeiro discriminado (admin vê tudo). SHAPE: vem aninhado em
  // order.financeiro; mantém fallback ao topo p/ compat. Quando ausente, o drawer
  // cai no resumo simples (valor + taxa + comissão líquida).
  const financeiro = detail?.order?.financeiro ?? detail?.financeiro ?? null

  const utm = detail?.marketing
  // UTM "real" = tem campanha de verdade. source vazio ou '(direct)'/'direct' com
  // só landing/referrer NÃO conta (é tráfego direto) → aba UTM oculta.
  const utmSrc = (utm?.utm_source || '').trim().toLowerCase()
  const hasUtm = !!utm && !!(
    (utmSrc && utmSrc !== 'direct' && utmSrc !== '(direct)') ||
    utm.utm_medium || utm.utm_campaign || utm.utm_term || utm.utm_content
  )
  const audit = detail?.motoboy?.audit ?? []
  const hasHist = audit.length > 0
  const fiscal = detail?.fiscal
  const hasNfe = !!fiscal && (fiscal.nfe_chave || fiscal.nfe_numero || fiscal.nfe_url)

  // Abas do drawer de detalhe. Resumo sempre; UTM/Histórico/Nota Fiscal só
  // aparecem quando há conteúdo (detecção pela resposta da API — campos
  // ausentes / array vazio). Enquanto o detalhe carrega, não escondemos as
  // abas opcionais para evitar "piscar" (mostra todas até o fetch resolver).
  const detailIsLoading = detailLoading && !!selectedOrder?.sz_order_id
  const visibleTabs = ([
    ['resumo', 'Resumo', true],
    ['utm', 'UTM', !detailIsLoading && hasUtm],
    ['historico', 'Histórico', !detailIsLoading && hasHist],
    ['nfe', 'Nota Fiscal', !detailIsLoading && hasNfe],
  ] as const).filter(([, , show]) => show)

  // Se a aba ativa deixou de existir (ex.: detalhe carregou e veio sem UTM),
  // volta para Resumo — evita um corpo de aba em branco sem cabeçalho.
  useEffect(() => {
    if (!visibleTabs.some(([key]) => key === detailTab)) {
      setDetailTab('resumo')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detailIsLoading, hasUtm, hasHist, hasNfe])

  return (
    <div>
      {/* ── Section head ──────────────────────────────────────── */}
      <div className="szv2-section-head" style={{ flexWrap: 'wrap', gap: 8 }}>
        <div>
          <h1>Pedidos Motoboy</h1>
          <p>
            {items.length} pedido(s) encontrado(s)
            {' · '}{totalUnidadesVendidas} unidade(s) vendida(s)
            {stopped ? ' — parados 24h+' : ''}
            {activeFilterCount > 0 ? ` · ${activeFilterCount} filtro(s) ativo(s)` : ''}
          </p>
        </div>
        <div style={{ display: 'flex', flexDirection: 'row', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          {/* Imprimir etiquetas por data de entrega */}
          <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
            <input
              type="date"
              value={printDate}
              onChange={e => setPrintDate(e.target.value)}
              className="szv2-input szv2-input-sm"
              style={{ width: 140 }}
              title="Data de entrega"
            />
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary szv2-btn-sm"
              onClick={handlePrintByDate}
              disabled={!printDate || printBusy}
              title="Imprimir etiquetas por data de entrega"
            >
              {printBusy ? '…' : '🖨 Por data'}
            </button>
          </div>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary szv2-btn-sm"
            onClick={exportCSV}
            disabled={exporting || items.length === 0}
            title="Exportar os pedidos filtrados em CSV"
          >
            {exporting ? 'Exportando…' : '↓ Exportar relatórios'}
          </button>
          <FilterButton
            active={activeFilterCount > 0}
            count={activeFilterCount}
            onClick={openFilterDrawer}
          />
        </div>
      </div>

      {/* Filtro por produtor vindo da tela Carteiras > Clientes ("Ver pedidos").
          Não é um filtro do drawer — vive só na URL — por isso tem chip próprio. */}
      {produtorIdFiltro && (
        <div className="szv2-alert-info" style={{ marginBottom: 12, display: 'flex', alignItems: 'center', gap: 8 }}>
          <span>Mostrando só pedidos do produtor #{produtorIdFiltro}</span>
          <a href="/orders" className="szv2-btn szv2-btn-sm szv2-btn-secondary">Limpar</a>
        </div>
      )}

      {/* ── Chips de filtros ativos ──────────────────────────── */}
      <ActiveFilterChips chips={activeChips} onClearAll={clearFilters} />

      {/* ── Alertas ──────────────────────────────────────────── */}
      {/* Banner só quando há dados na tela (erro de refresh/ação). Falha de
          carregamento inicial vira ErrorState na área da tabela. */}
      {err && items.length > 0 && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}

      {/* ── Tabela ───────────────────────────────────────────── */}
      {loading && items.length === 0 ? (
        <TableSkeleton rows={6} cols={13} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={load} />
      ) : !loading && items.length === 0 ? (
        <EmptyState
          icon="📦"
          title="Nenhum pedido encontrado com esses filtros."
          description="Ajuste o período ou remova os filtros aplicados."
        />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th style={{ width: 36, paddingRight: 0 }}>
                <input
                  type="checkbox"
                  checked={bulk.allSelected}
                  ref={el => { if (el) el.indeterminate = bulk.someSelected }}
                  onChange={bulk.toggleAll}
                  title="Selecionar todos elegíveis"
                />
              </th>
              <th>Pedido</th>
              <th>Cliente</th>
              <th>Status</th>
              <th>Motoboy</th>
              <th>Produto</th>
              <th>Afiliado</th>
              <th>Data de entrega</th>
              <th className="szv2-td-num">Bruto</th>
              <th className="szv2-td-num">Comissão afiliado</th>
              <th className="szv2-td-num">Comissão produtor</th>
              <th className="szv2-td-num">Taxa Falk</th>
            </tr>
          </thead>
          <tbody>
            {items.map(p => {
              const produtoLabel = normalizeProductLabel(p.oferta_nome, p.produto, p.produto_qtd)
              // Telefone do cliente sem o DDI +55 (coluna Cliente).
              const clienteTel = stripCountryCode(p.cliente_telefone || '')
              const isSelectable = p.status === 'agendado' || p.status === 'embalado' || p.status === 'em_rota' || p.status === 'a_caminho'
              return (
              <tr
                key={p.id}
                onClick={() => setSelectedOrder(p)}
                style={{ cursor: 'pointer' }}
                title="Clique para ver detalhes"
              >
                {/* Checkbox */}
                <td style={{ paddingRight: 0 }} onClick={e => e.stopPropagation()}>
                  {isSelectable && (
                    <input
                      type="checkbox"
                      checked={bulk.has(p.id)}
                      onChange={() => bulk.toggle(p.id)}
                    />
                  )}
                </td>
                {/* Pedido — número + criado (unificados): número em cima, data embaixo */}
                <td>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                    <span style={{ fontWeight: 600, fontSize: 13 }}>{orderLabel(p)}</span>
                    <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{fmtDateTime(p.created_at)}</span>
                  </div>
                </td>
                {/* Cliente — nome em cima, telefone abaixo (menor/cinza) */}
                <td>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                    <span style={{ fontWeight: 500 }}>
                      {p.cliente_nome || p.dest_nome || '—'}
                    </span>
                    {!!clienteTel && (
                      <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)' }}>
                        {clienteTel}
                      </span>
                    )}
                  </div>
                </td>
                {/* Status */}
                <td>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 4, alignItems: 'flex-start' }}>
                    <StatusBadge status={p.status} label={displayStatus(p.status)} />
                  </div>
                </td>
                {/* Motoboy */}
                <td style={{ fontSize: 13, color: p.motoboy_nome ? 'var(--szv2-text)' : 'var(--szv2-text-faint)' }}>
                  {p.motoboy_nome || '—'}
                </td>
                <td style={{ fontSize: 13, color: 'var(--szv2-text)', whiteSpace: 'nowrap' }}>{produtoLabel}</td>
                {/* Afiliado */}
                <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)' }}>{p.afiliado_nome || '—'}</td>
                {/* Data de entrega */}
                <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)', fontFamily: 'var(--szv2-font-mono)' }}>
                  {p.delivery_date ? brDate(p.delivery_date) : '—'}
                </td>
                <td className="szv2-td-num" style={{ fontWeight: 700 }}>
                  {p.valor > 0 ? (
                    <span style={{ color: 'var(--szv2-text)', fontFamily: 'var(--szv2-font-mono)' }}>{brl(p.valor)}</span>
                  ) : (
                    <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                  )}
                </td>
                <td className="szv2-td-num" style={{ fontWeight: 700 }}>
                  {(p.status === 'cancelado' || p.status === 'cancelled') ? (
                    <span style={{ color: 'var(--szv2-text-faint)', fontFamily: 'var(--szv2-font-mono)' }}>R$ 0,00</span>
                  ) : (p.status === 'frustrado' || p.status === 'reembolsado') ? (
                    (p.taxa_frustracao_afiliado ?? p.taxa_frustrado) > 0 ? (
                      <span style={{ color: 'var(--szv2-danger)', fontFamily: 'var(--szv2-font-mono)' }}>{brl(p.taxa_frustracao_afiliado ?? p.taxa_frustrado)}</span>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-faint)', fontFamily: 'var(--szv2-font-mono)' }}>R$ 0,00</span>
                    )
                  ) : (p.comissao_afiliado_liquida ?? p.comissao) > 0 ? (
                    <span style={{ color: 'var(--szv2-brand)', fontFamily: 'var(--szv2-font-mono)' }}>{brl(p.comissao_afiliado_liquida ?? p.comissao)}</span>
                  ) : (
                    <span style={{ color: 'var(--szv2-text-faint)', fontFamily: 'var(--szv2-font-mono)' }}>R$ 0,00</span>
                  )}
                </td>
                <td className="szv2-td-num" style={{ fontWeight: 700 }}>
                  {(p.status === 'cancelado' || p.status === 'cancelled') ? (
                    <span style={{ color: 'var(--szv2-text-faint)', fontFamily: 'var(--szv2-font-mono)' }}>R$ 0,00</span>
                  ) : (p.status === 'frustrado' || p.status === 'reembolsado') ? (
                    (p.taxa_frustracao_produtor ?? 0) > 0 ? (
                      <span style={{ color: 'var(--szv2-danger)', fontFamily: 'var(--szv2-font-mono)' }}>{brl(p.taxa_frustracao_produtor ?? 0)}</span>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-faint)', fontFamily: 'var(--szv2-font-mono)' }}>R$ 0,00</span>
                    )
                  ) : (
                    <span
                      style={{
                        color: (p.comissao_produtor ?? 0) > 0 ? 'var(--szv2-brand)' : 'var(--szv2-text-faint)',
                        fontFamily: 'var(--szv2-font-mono)'
                      }}
                    >
                      {brl(p.comissao_produtor ?? 0)}
                    </span>
                  )}
                </td>
                <td className="szv2-td-num" style={{ fontWeight: 700 }}>
                  {(p.status === 'cancelado' || p.status === 'cancelled') ? (
                    <span style={{ color: 'var(--szv2-text-faint)', fontFamily: 'var(--szv2-font-mono)' }}>R$ 0,00</span>
                  ) : (p.status === 'frustrado' || p.status === 'reembolsado') ? (
                    // Taxa Falk em pedido frustrado = penalidade total (afiliado + produtor),
                    // não a taxa Falk normal de pedido concluído.
                    ((p.taxa_frustracao_afiliado ?? p.taxa_frustrado ?? 0) + (p.taxa_frustracao_produtor ?? 0)) > 0 ? (
                      <span style={{ color: 'var(--szv2-warning)', fontFamily: 'var(--szv2-font-mono)' }}>
                        {brl((p.taxa_frustracao_afiliado ?? p.taxa_frustrado ?? 0) + (p.taxa_frustracao_produtor ?? 0))}
                      </span>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                    )
                  ) : (p.taxa_falk ?? 0) > 0 ? (
                    <span style={{ color: 'var(--szv2-warning)', fontFamily: 'var(--szv2-font-mono)' }}>{brl(p.taxa_falk ?? 0)}</span>
                  ) : (
                    <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                  )}
                </td>
              </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      )}

      {/* ── Bulk confirm panel (Entregue / Frustrado) ─────────── */}
      {bulkConfirm && (
        <div style={{ position: 'fixed', bottom: 80, left: '50%', transform: 'translateX(-50%)', zIndex: 300, background: 'var(--szv2-surface)', border: '1px solid var(--szv2-border)', borderRadius: 12, padding: '16px 20px', boxShadow: '0 8px 32px rgba(0,0,0,.18)', width: 'min(420px, 94vw)', display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div style={{ fontWeight: 700, fontSize: 14 }}>
            {bulkConfirm === 'entregue' && '✅ Marcar Entregue'}
            {bulkConfirm === 'frustrado' && '✗ Registrar Frustrado'}
            {' '}— {bulk.size} pedido(s)
          </div>
          {bulkConfirm === 'frustrado' && (
            <select
              value={bulkMotivo}
              onChange={e => setBulkMotivo(e.target.value)}
              style={{ padding: '8px 10px', borderRadius: 6, border: '1px solid var(--szv2-border)', fontSize: 13 }}
            >
              <option value="">— Motivo (obrigatório) —</option>
              <option value="cliente_ausente">Cliente ausente</option>
              <option value="endereco_incorreto">Endereço incorreto</option>
              <option value="cliente_recusou">Cliente recusou</option>
              <option value="produto_danificado">Produto danificado</option>
              <option value="tentativa_sem_sucesso">Tentativa sem sucesso</option>
              <option value="outro">Outro</option>
            </select>
          )}
          <textarea
            rows={2}
            placeholder="Justificativa obrigatória"
            value={bulkJustif}
            onChange={e => setBulkJustif(e.target.value)}
            style={{ width: '100%', padding: '8px 10px', borderRadius: 6, border: '1px solid var(--szv2-border)', fontSize: 13, resize: 'vertical', boxSizing: 'border-box' }}
          />
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={() => { setBulkConfirm(null); setBulkJustif(''); setBulkMotivo('') }} disabled={bulkForceBusy}>Cancelar</button>
            <button
              type="button"
              className={`szv2-btn szv2-btn-sm ${bulkConfirm === 'frustrado' ? 'szv2-btn-danger' : 'szv2-btn-brand'}`}
              disabled={bulkForceBusy || !bulkJustif.trim() || (bulkConfirm === 'frustrado' && !bulkMotivo)}
              onClick={() => handleBulkForceStatus(bulkConfirm!)}
            >
              {bulkForceBusy ? 'Salvando…' : 'Confirmar'}
            </button>
          </div>
        </div>
      )}

      {/* ── BulkBar ─────────────────────────────────────────────────────── */}
      {(() => {
        const selectedItems = bulk.ids
          .map(id => items.find(p => p.id === id))
          .filter((p): p is P => !!p)
        const hasAgendado = selectedItems.length > 0 && selectedItems.some(p => p.status === 'agendado')
        const allEmbalado = selectedItems.length > 0 && selectedItems.every(p => p.status === 'embalado')
        const hasReassignable = selectedItems.length > 0 && selectedItems.some(p => motoboyChangeableStatus(p.status))
        const allEmRota = selectedItems.length > 0 && selectedItems.every(p => p.status === 'em_rota')
        const actions = [
          hasAgendado ? {
            label: '📦 Embalar',
            variant: 'brand' as const,
            onClick: handleBulkEmbalar,
            disabled: !bulk.ids.some(id => items.find(p => p.id === id)?.status === 'agendado'),
            title: 'Embalar pedidos "agendado" e gerar etiqueta (itens com outro status na seleção são ignorados)',
          } : null,
          hasReassignable ? {
            label: '↔ Trocar motoboy',
            variant: 'secondary' as const,
            onClick: handleBulkTrocarMotoboy,
            disabled: !selMotoboy,
            title: 'Trocar o motoboy dos pedidos embalados/em rota/a caminho selecionados',
          } : null,
          allEmbalado ? {
            label: '🖨️ Imprimir etiquetas',
            variant: 'secondary' as const,
            onClick: handleBulkImprimir,
            disabled: !bulk.ids.some(id => items.find(p => p.id === id)?.status === 'embalado' && !!items.find(p => p.id === id)?.package_code),
            title: 'Imprimir etiquetas dos pedidos embalados selecionados',
          } : null,
          allEmbalado ? {
            label: '🛵 Em rota',
            variant: 'brand' as const,
            onClick: () => { setBulkConfirm('em_rota'); setBulkJustif(''); setBulkMotivo('') },
            disabled: false,
            title: 'Marcar pedidos embalados como Em rota',
          } : null,
          allEmRota ? {
            label: '✅ Entregue',
            variant: 'secondary' as const,
            onClick: () => { setBulkConfirm('entregue'); setBulkJustif(''); setBulkMotivo('') },
            disabled: false,
            title: 'Marcar pedidos Em Rota como Entregue',
          } : null,
          allEmRota ? {
            label: '✗ Frustrado',
            variant: 'danger' as const,
            onClick: () => { setBulkConfirm('frustrado'); setBulkJustif(''); setBulkMotivo('') },
            disabled: false,
            title: 'Registrar entrega frustrada',
          } : null,
        ].filter(Boolean) as {
          label: string
          variant: 'brand' | 'secondary' | 'danger'
          onClick: () => void
          disabled?: boolean
          title?: string
        }[]
        return (
      <BulkBar
        count={bulk.size}
        onClear={bulk.clear}
        busy={bulkBusy || bulkForceBusy}
        noun="pedido selecionado"
        nounPlural="pedidos selecionados"
        children={bulk.size > 0 && bulk.ids.some(id => {
          const st = items.find(p => p.id === id)?.status
          return st === 'agendado' || motoboyChangeableStatus(st || '')
        }) ? (
          <>
            <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)', fontWeight: 600 }}>Motoboy:</span>
            <FalkSelect
              value={selMotoboy}
              onChange={setSelMotoboy}
              placeholder="Selecione um motoboy"
              options={motoboys.map(m => ({ value: String(m.id), label: m.nome }))}
              style={{ minWidth: 220 }}
            />
          </>
        ) : null}
        actions={actions}
      />
        )
      })()}

      {/* ── Painel de Filtros (topo) ────────────────────────── */}
      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftDataIni}
            max={draftDataFim}
            onChange={v => setDraftDataIni(v)}
            placeholder="dd/mm/aaaa"
            aria-label="Data inicial"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftDataFim}
            min={draftDataIni}
            onChange={v => setDraftDataFim(v)}
            placeholder="dd/mm/aaaa"
            aria-label="Data final"
          />
        </FilterField>
        <FilterField label="Status">
          <FalkSelect
            aria-label="Status"
            value={draftStatus}
            onChange={v => setDraftStatus(v)}
            placeholder="Todos status"
            options={[
              { value: '', label: 'Todos status' },
              ...STATUS_OPTIONS.map(s => ({ value: s.value, label: s.label })),
            ]}
          />
        </FilterField>
        <FilterField label="Cidade">
          <FalkSelect
            aria-label="Cidade"
            value={draftCidade}
            onChange={v => setDraftCidade(v)}
            placeholder="Todas as cidades"
            options={[
              { value: '', label: 'Todas as cidades' },
              ...cidadeOpts.map(c => ({ value: c, label: c })),
            ]}
          />
        </FilterField>
        <FilterField label="Estado (UF)">
          <FalkSelect
            aria-label="Estado (UF)"
            value={draftUf}
            onChange={v => setDraftUf(v)}
            placeholder="Todos os estados"
            options={[
              { value: '', label: 'Todos os estados' },
              ...UF_OPTIONS.map(u => ({ value: u, label: u })),
            ]}
          />
        </FilterField>
        <FilterField label="Produto">
          <FalkSelect
            aria-label="Produto"
            value={draftProduto}
            onChange={v => setDraftProduto(v)}
            placeholder="Todos os produtos"
            options={[
              { value: '', label: 'Todos os produtos' },
              ...produtoOpts.map(p => ({ value: p.nome, label: p.nome })),
            ]}
          />
        </FilterField>
        <FilterField label="Afiliado">
          <FalkSelect
            aria-label="Afiliado"
            value={draftAfiliado}
            onChange={v => setDraftAfiliado(v)}
            placeholder="Todos os afiliados"
            options={[
              { value: '', label: 'Todos os afiliados' },
              ...afiliadoOpts.map(a => ({ value: a.nome, label: a.nome })),
            ]}
          />
        </FilterField>
        <FilterField label="Busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="Pedido / produto / afiliado / nome"
            value={draftSearch}
            onChange={e => setDraftSearch(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>

      {/* ── Drawer: Reagendar (próximos 30 dias) ─────────────── */}
      <FilterDrawer
        open={rescheduleOrder !== null}
        onClose={() => setRescheduleOrder(null)}
        onApply={confirmReschedule}
        onClear={() => setRescheduleOrder(null)}
        applyLabel={busyId === rescheduleOrder?.id ? 'Reagendando…' : 'Confirmar reagendamento'}
        clearLabel="Cancelar"
        title={rescheduleOrder ? `Reagendar ${rescheduleOrder.wc_order_id ?? rescheduleOrder.id}` : 'Reagendar'}
      >
        {rescheduleOrder && (
          <>
            <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', lineHeight: 1.5 }}>
              Escolha a nova data de entrega. Datas com mais de 5 dias úteis ficam como
              <strong style={{ color: 'var(--szv2-brand)' }}> pré-agendado</strong> até a confirmação do produtor/afiliado.
            </div>
            {reschedZona?.has_schedule && (
              <div style={{ fontSize: 11, color: 'var(--szv2-text-faint)', lineHeight: 1.5 }}>
                Só é possível agendar nos dias de funcionamento desta zona ({zonaDiasLabel(reschedZona.dias)}), respeitando o horário limite de cada dia.
              </div>
            )}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {/* DONO: dias indisponíveis (cutoff global OU fora da agenda da zona) NÃO
                  aparecem na lista — filtra antes do map, em vez de mostrar desabilitado. */}
              {days30.filter(d => !(d.bloqueado || (rescheduleDateDisabled?.(d.iso) ?? false))).map(d => {
                const selected = rescheduleDate === d.iso
                const bloqueado = false
                return (
                  <button
                    key={d.iso}
                    type="button"
                    disabled={bloqueado}
                    onClick={() => !bloqueado && setRescheduleDate(d.iso)}
                    style={{
                      opacity: bloqueado ? 0.45 : 1,
                      cursor: bloqueado ? 'not-allowed' : 'pointer',
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      padding: '9px 12px',
                      borderRadius: 8,
                      border: selected ? '1px solid var(--szv2-brand)' : '1px solid var(--szv2-border)',
                      background: selected ? 'rgba(30, 111, 242,.10)' : 'var(--szv2-surface)',
                      color: selected ? 'var(--szv2-brand)' : 'var(--szv2-text)',
                      fontSize: 13,
                      fontWeight: selected ? 700 : 500,
                      textAlign: 'left',
                    }}
                  >
                    <span>{d.label} <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontWeight: 400 }}>· {d.dow}{cutoffForIso(reschedZona, d.iso) ? ` · até ${cutoffForIso(reschedZona, d.iso)}` : ''}</span></span>
                    <span style={{
                      fontSize: 10.5, fontWeight: 700, letterSpacing: '.04em', textTransform: 'uppercase',
                      padding: '3px 9px', borderRadius: 999,
                      color: d.preAgendado ? '#5b6472' : 'var(--szv2-brand)',
                      background: d.preAgendado ? 'rgba(91,100,114,.14)' : 'rgba(30,111,242,.12)',
                    }}>{d.preAgendado ? 'Pré-agendado' : 'Agendado'}</span>
                  </button>
                )
              })}
            </div>
          </>
        )}
      </FilterDrawer>

      {/* ── Drawer de Detalhe do Pedido ──────────────────────── */}
      <FilterDrawer
        open={selectedOrder !== null}
        onClose={() => setSelectedOrder(null)}
        onApply={() => setSelectedOrder(null)}
        applyLabel="Fechar"
        title={selectedOrder ? `${orderLabel(selectedOrder)} — ${displayStatus(selectedOrder.status)}` : 'Detalhes'}
      >
        {selectedOrder && (
          <>
            {/* Abas — só renderiza as que têm conteúdo (Resumo sempre). */}
            <div style={{ display: 'flex', gap: 4, borderBottom: '1px solid var(--szv2-divider)' }}>
              {visibleTabs.map(([key, label]) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => setDetailTab(key)}
                  style={{
                    background: 'transparent',
                    border: 0,
                    borderBottom: detailTab === key ? '2px solid var(--szv2-brand)' : '2px solid transparent',
                    color: detailTab === key ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
                    fontWeight: detailTab === key ? 700 : 500,
                    fontSize: 13,
                    padding: '8px 10px',
                    cursor: 'pointer',
                  }}
                >
                  {label}
                </button>
              ))}
            </div>

            {/* ── Aba: Resumo ─────────────────────────────────── */}
            {detailTab === 'resumo' && (
              <>
                {/* QR Code da etiqueta — só quando embalado e tem package_code */}
                {selectedOrder.package_code && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em', alignSelf: 'flex-start' }}>QR Code da etiqueta</span>
                    <img
                      src={`https://api.qrserver.com/v1/create-qr-code/?size=180x180&data=${encodeURIComponent(selectedOrder.package_code)}`}
                      alt="QR Code"
                      width={180} height={180}
                      style={{ borderRadius: 8, border: '1px solid var(--szv2-border)' }}
                    />
                    <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontFamily: 'monospace', wordBreak: 'break-all', textAlign: 'center' }}>{selectedOrder.package_code}</div>
                  </div>
                )}

                {/* Endereço completo do cliente */}
                <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                  <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Endereço do cliente</span>
                  <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.6 }}>
                    {detailLoading && selectedOrder.sz_order_id ? (
                      <span style={{ color: 'var(--szv2-text-muted)' }}>Carregando…</span>
                    ) : endereco ? (
                      <>
                        {endereco.nome && <div style={{ fontWeight: 600 }}>{endereco.nome}</div>}
                        <div>
                          {endereco.logradouro || '—'}
                          {endereco.numero ? `, ${endereco.numero}` : ''}
                        </div>
                        {endereco.complemento && <div>{endereco.complemento}</div>}
                        {endereco.bairro && <div>{endereco.bairro}</div>}
                        <div>
                          {endereco.cidade || selectedOrder.dest_cidade || '—'}
                          {(endereco.uf || selectedOrder.dest_uf) ? `/${endereco.uf || selectedOrder.dest_uf}` : ''}
                        </div>
                        {(endereco.cep || selectedOrder.dest_cep) && (
                          <div style={{ color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                            CEP {endereco.cep || selectedOrder.dest_cep}
                          </div>
                        )}
                        {/* Telefone do cliente sem o DDI +55 (fallback p/ o da lista). */}
                        {!!stripCountryCode(endereco.telefone || selectedOrder.cliente_telefone || '') && (
                          <div style={{ color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                            {stripCountryCode(endereco.telefone || selectedOrder.cliente_telefone || '')}
                          </div>
                        )}
                      </>
                    ) : (
                      <>
                        <div style={{ fontWeight: 600 }}>{selectedOrder.dest_nome || '—'}</div>
                        {selectedOrder.dest_cidade && (
                          <div>{selectedOrder.dest_cidade}{selectedOrder.dest_uf ? `/${selectedOrder.dest_uf}` : ''}</div>
                        )}
                        {selectedOrder.dest_cep && (
                          <div style={{ color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                            CEP {selectedOrder.dest_cep}
                          </div>
                        )}
                        {/* Telefone do cliente sem o DDI +55. */}
                        {!!stripCountryCode(selectedOrder.cliente_telefone || '') && (
                          <div style={{ color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                            {stripCountryCode(selectedOrder.cliente_telefone || '')}
                          </div>
                        )}
                      </>
                    )}
                  </div>
                </div>
                {/* Financeiro — breakdown discriminado (admin vê tudo). */}
                <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                  <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Financeiro</span>
                  {detailLoading && selectedOrder.sz_order_id ? (
                    <div style={{ marginTop: 8, fontSize: 12, color: 'var(--szv2-text-muted)' }}>Carregando…</div>
                  ) : financeiro ? (
                    // Breakdown completo do backend. Valores já calculados no Go —
                    // NÃO recalcular aqui. brl() exibe 0 com segurança (sem 0 && JSX).
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>Valor do pedido</span>
                        <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)' }}>
                          {brl(financeiro.valor_pedido)}
                        </span>
                      </div>

                      {/* Bloco afiliado — SÓ aparece quando existe afiliado de verdade
                          (comissao_pct > 0). Sem afiliado, essas 3 linhas eram só ruído
                          (tudo R$ 0,00) escondendo a taxa de entrega real. */}
                      {!!financeiro.comissao_pct && (
                        <>
                          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                            <span style={{ color: 'var(--szv2-text-muted)' }}>Comissão afiliado (bruta)</span>
                            <span style={{ fontFamily: 'var(--szv2-font-mono)' }}>
                              {brl(financeiro.comissao_afiliado_bruta)}
                            </span>
                          </div>
                          {/* Frustrado: taxa de transação do afiliado é 0 (sem fatia da plataforma);
                              a regra do dono manda mostrar o PREJUÍZO, não a dedução zerada. */}
                          {!financeiro.frustrado && (
                            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                              <span style={{ color: 'var(--szv2-text-muted)' }}>Taxa transação afiliado (4,99%)</span>
                              <span style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-warning)' }}>
                                − {brl(financeiro.taxa_transacao_afiliado)}
                              </span>
                            </div>
                          )}
                          {financeiro.frustrado ? (
                            // TAXA DE FRUSTRAÇÃO (afiliado) — valor REAL cobrado do afiliado na
                            // frustração (linha penalty do razão; congelado na baixa). REGRA DO
                            // DONO 2026-06-23: mostrar a taxa cobrada de cada parte, explícita,
                            // mesmo quando 0. Linha de despesa vermelha (danger).
                            <div style={{ display: 'flex', justifyContent: 'space-between', borderTop: '1px dashed var(--szv2-divider)', paddingTop: 6 }}>
                              <span style={{ color: 'var(--szv2-danger)', fontWeight: 600 }}>Taxa de frustração (afiliado)</span>
                              <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-danger)' }}>
                                − {brl(financeiro.taxa_frustracao_afiliado ?? 0)}
                              </span>
                            </div>
                          ) : (
                            <div style={{ display: 'flex', justifyContent: 'space-between', borderTop: '1px dashed var(--szv2-divider)', paddingTop: 6 }}>
                              <span style={{ color: 'var(--szv2-text-muted)', fontWeight: 600 }}>Comissão afiliado (líquida) ({formatCommissionPct(financeiro.comissao_pct)})</span>
                              <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)' }}>
                                {brl(financeiro.comissao_afiliado_liquida)}
                              </span>
                            </div>
                          )}
                        </>
                      )}

                      {/* Bloco produtor. Taxa de entrega em destaque (fontWeight 600) —
                          pedido dono: deixar bem transparente o que é cobrado, sem
                          afiliado essa é a primeira dedução real que o produtor vê. */}
                      <div style={{ display: 'flex', justifyContent: 'space-between', borderTop: '1px solid var(--szv2-divider)', paddingTop: 8, marginTop: 2 }}>
                        <span style={{ color: 'var(--szv2-text)', fontWeight: 600 }}>Taxa de entrega (abatida do produtor)</span>
                        <span style={{ fontFamily: 'var(--szv2-font-mono)', fontWeight: 600, color: 'var(--szv2-warning)' }}>
                          − {brl(financeiro.taxa_entrega)}
                        </span>
                      </div>
                      {/* Frustrado: mostra a TAXA DE FRUSTRAÇÃO cobrada do produtor, explícita
                          (mesmo quando 0 — REGRA DO DONO 2026-06-23). Linha de despesa vermelha. */}
                      {financeiro.frustrado ? (
                        <div style={{ display: 'flex', justifyContent: 'space-between', borderTop: '1px dashed var(--szv2-divider)', paddingTop: 6 }}>
                          <span style={{ color: 'var(--szv2-danger)', fontWeight: 600 }}>Taxa de frustração (produtor)</span>
                          <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-danger)' }}>
                            − {brl(financeiro.taxa_frustracao_produtor ?? 0)}
                          </span>
                        </div>
                      ) : (
                        <>
                          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                            <span style={{ color: 'var(--szv2-text-muted)' }}>Taxa transação produtor</span>
                            <span style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-warning)' }}>
                              − {brl(financeiro.taxa_transacao_produtor)}
                            </span>
                          </div>
                          <div style={{ display: 'flex', justifyContent: 'space-between', borderTop: '1px dashed var(--szv2-divider)', paddingTop: 6 }}>
                            {/* "Líquido produtor" (não "Comissão") — sem afiliado no pedido,
                                chamar de comissão confunde (não tem quem receber comissão). */}
                            <span style={{ color: 'var(--szv2-text-muted)', fontWeight: 600 }}>
                              {!!financeiro.comissao_pct ? 'Comissão' : 'Líquido produtor'}
                            </span>
                            <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)' }}>
                              {brl(financeiro.liquido_produtor)}
                            </span>
                          </div>
                        </>
                      )}
                    </div>
                  ) : (
                    // Fallback: backend ainda não expõe o breakdown — resumo simples.
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>Valor do pedido</span>
                        <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)' }}>
                          {selectedOrder.valor > 0 ? brl(selectedOrder.valor) : '—'}
                        </span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>
                          {selectedOrder.status === 'frustrado' ? 'Taxa frustrado' : 'Taxa motoboy'}
                        </span>
                        <span style={{ fontFamily: 'var(--szv2-font-mono)', color: selectedOrder.status === 'frustrado' ? 'var(--szv2-warning)' : undefined }}>
                          {selectedOrder.status === 'frustrado' && selectedOrder.taxa_frustrado > 0
                            ? brl(selectedOrder.taxa_frustrado)
                            : selectedOrder.taxa_motoboy > 0 ? brl(selectedOrder.taxa_motoboy) : '—'}
                        </span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>Comissão afiliado (líquida)</span>
                        <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)' }}>
                          {selectedOrder.comissao > 0 ? brl(selectedOrder.comissao) : '—'}
                        </span>
                      </div>
                    </div>
                  )}
                </div>

                {/* Produto + Afiliado */}
                <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                  <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Produto / Afiliado</span>
                  <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                      <span style={{ color: 'var(--szv2-text-muted)' }}>Produto</span>
                      <span style={{ fontWeight: 500, whiteSpace: 'nowrap' }}>{normalizeProductLabel(selectedOrder.oferta_nome, selectedOrder.produto, selectedOrder.produto_qtd)}</span>
                    </div>
                    {/* Variação — só aqui no detalhe (regra do dono: fora da tabela). */}
                    <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                      <span style={{ color: 'var(--szv2-text-muted)' }}>Variação</span>
                      <span style={{ fontWeight: 500 }}>{normalizeProductName(selectedOrder.variacao || '') || '—'}</span>
                    </div>
                    <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                      <span style={{ color: 'var(--szv2-text-muted)' }}>Afiliado</span>
                      <span>{selectedOrder.afiliado_nome || '—'}</span>
                    </div>
                    {(selectedOrder.motoboy_nome || detail?.motoboy?.motoboy_nome) && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>Motoboy</span>
                        <span style={{ fontWeight: 600 }}>{selectedOrder.motoboy_nome || detail?.motoboy?.motoboy_nome || '—'}</span>
                      </div>
                    )}
                  </div>
                </div>

                {/* Oferta de checkout — link do produtor que define o preço de venda (fonte fiel migrada) */}
                {(selectedOrder.oferta_nome || selectedOrder.oferta_valor > 0 || selectedOrder.oferta_url || selectedOrder.oferta_link) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Oferta de checkout</span>
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                      {selectedOrder.oferta_nome && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Oferta</span>
                          <span style={{ fontWeight: 500, textAlign: 'right' }}>{selectedOrder.oferta_nome}</span>
                        </div>
                      )}
                      {selectedOrder.oferta_valor > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Valor da oferta</span>
                          <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-brand)' }}>
                            {brl(selectedOrder.oferta_valor)}
                          </span>
                        </div>
                      )}
                      {selectedOrder.oferta_url && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                          <span style={{ color: 'var(--szv2-text-muted)', flexShrink: 0 }}>Link</span>
                          <a
                            href={safeUrl(selectedOrder.oferta_url)}
                            target="_blank"
                            rel="noopener noreferrer"
                            style={{ color: 'var(--szv2-brand)', fontFamily: 'var(--szv2-font-mono)', fontSize: 11, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', textAlign: 'right' }}
                            title={selectedOrder.oferta_url}
                            onClick={e => e.stopPropagation()}
                          >
                            {selectedOrder.oferta_url}
                          </a>
                        </div>
                      )}
                      {!selectedOrder.oferta_nome && !selectedOrder.oferta_valor && selectedOrder.oferta_link && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Token (afiliado)</span>
                          <span style={{ fontFamily: 'var(--szv2-font-mono)', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', padding: '2px 8px', borderRadius: 4, fontSize: 11 }}>
                            {selectedOrder.oferta_link}
                          </span>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {/* Motoboy / entregador (do detalhe) */}
                {detail?.motoboy && (detail.motoboy.motoboy_nome || detail.motoboy.motoboy_telefone || detail.motoboy.motoboy_placa) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>🛵 Motoboy</span>
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                      {detail.motoboy.motoboy_nome && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Nome</span>
                          <span style={{ fontWeight: 600 }}>{detail.motoboy.motoboy_nome}</span>
                        </div>
                      )}
                      {detail.motoboy.motoboy_telefone && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Telefone</span>
                          <a
                            href={`tel:${detail.motoboy.motoboy_telefone.replace(/\D/g, '')}`}
                            style={{ color: 'var(--szv2-brand)', fontFamily: 'var(--szv2-font-mono)', textDecoration: 'none' }}
                          >
                            {detail.motoboy.motoboy_telefone}
                          </a>
                        </div>
                      )}
                      {detail.motoboy.motoboy_placa && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Placa</span>
                          <span style={{ fontFamily: 'var(--szv2-font-mono)', fontWeight: 700, letterSpacing: '.05em' }}>{detail.motoboy.motoboy_placa}</span>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {/* Rastreio */}
                {detail?.tracking && (detail.tracking.code || detail.tracking.url || detail.tracking.carrier) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>📦 Rastreio</span>
                    <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                      {detail.tracking.code && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Código</span>
                          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                            <span style={{ fontFamily: 'var(--szv2-font-mono)', fontWeight: 700 }}>{detail.tracking.code}</span>
                            <CopyButton text={detail.tracking.code} variant="icon" />
                          </span>
                        </div>
                      )}
                      {detail.tracking.carrier && (
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span style={{ color: 'var(--szv2-text-muted)' }}>Transportadora</span>
                          <span>{detail.tracking.carrier}</span>
                        </div>
                      )}
                      {detail.tracking.url && (
                        <a
                          href={safeUrl(detail.tracking.url)}
                          target="_blank"
                          rel="noreferrer"
                          className="szv2-btn szv2-btn-brand"
                          style={{ marginTop: 4, textAlign: 'center', fontSize: 12 }}
                        >
                          Abrir rastreio
                        </a>
                      )}
                    </div>
                  </div>
                )}

                {/* Reagendar / Cancelar — no lugar da Identificação. Reagendar libera até
                    em_rota; Cancelar só agendado/embalado (RESCHEDULABLE). */}
                {(REAGENDAVEL.has(selectedOrder.status) || RESCHEDULABLE.has(selectedOrder.status)) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16, display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {REAGENDAVEL.has(selectedOrder.status) && (
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-secondary"
                        style={{ width: '100%', justifyContent: 'center' }}
                        disabled={busyId === selectedOrder.id}
                        onClick={() => openReschedule(selectedOrder)}
                      >
                        Reagendar
                      </button>
                    )}
                    {RESCHEDULABLE.has(selectedOrder.status) && (
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-danger"
                        style={{ width: '100%', justifyContent: 'center' }}
                        disabled={busyId === selectedOrder.id}
                        onClick={() => cancelOrder(selectedOrder)}
                      >
                        {busyId === selectedOrder.id ? 'Cancelando…' : 'Cancelar pedido'}
                      </button>
                    )}
                  </div>
                )}

                {/* Reagendar FRUSTRADO/CANCELADO = CÓPIA (REGRA DO DONO). Escolhe a nova data; o
                    backend clona TODOS os dados num novo pedido e preserva o original. */}
                {(selectedOrder.status === 'frustrado' || selectedOrder.status === 'cancelado') && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16, display: 'flex', flexDirection: 'column', gap: 10 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Reagendar</span>
                    <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', lineHeight: 1.5 }}>
                      Escolha a nova data de entrega para este pedido.
                    </div>
                    {zonaSchedule?.has_schedule && (
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-faint)', lineHeight: 1.5 }}>
                        Só é possível agendar nos dias de funcionamento desta zona ({zonaDiasLabel(zonaSchedule.dias)}), respeitando o horário limite de cada dia.
                      </div>
                    )}
                    <FalkDatePicker
                      value={cloneDate}
                      onChange={setCloneDate}
                      min={ymd(new Date())}
                      isDateDisabled={cloneDateDisabled}
                      placeholder="Nova data de entrega"
                      aria-label="Nova data de entrega"
                    />
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-brand"
                      style={{ width: '100%', justifyContent: 'center' }}
                      disabled={!cloneDate || busyId === selectedOrder.id}
                      onClick={() => cloneFrustrated(selectedOrder)}
                    >
                      {busyId === selectedOrder.id ? 'Reagendando…' : 'Reagendar'}
                    </button>
                  </div>
                )}

                {/* Trocar motoboy — embalado / em rota / a caminho */}
                {selectedOrder.sz_order_id && motoboyChangeableStatus(selectedOrder.status) && (
                  <div style={{ borderTop: '1px solid var(--szv2-divider)', paddingTop: 16, display: 'flex', flexDirection: 'column', gap: 10 }}>
                    <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>Trocar motoboy</span>
                    <FalkSelect
                      value={drawerMotoboyId}
                      onChange={v => setDrawerMotoboyId(v)}
                      placeholder="Selecione um motoboy"
                      options={motoboys.map(m => ({ value: String(m.id), label: m.nome }))}
                    />
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-brand"
                      style={{ width: '100%', justifyContent: 'center' }}
                      disabled={drawerBusy || !drawerMotoboyId}
                      onClick={() => drawerChangeMotoboy(selectedOrder)}
                    >
                      {drawerBusy ? 'Salvando…' : 'Trocar motoboy'}
                    </button>
                  </div>
                )}

                {/* ── Mudar status (base do drawer, visível no Resumo) ── */}
                {selectedOrder.status === 'em_rota' && selectedOrder.sz_order_id && (
                  <div style={{ borderTop: '1px solid var(--szv2-border)', marginTop: 16, paddingTop: 14 }}>
                    <div style={{ fontWeight: 700, fontSize: 12, color: 'var(--szv2-text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 10 }}>Mudar status</div>
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: drawerAction ? 12 : 0 }}>
                      <button type="button" className={`szv2-btn szv2-btn-sm ${drawerAction === 'entregue' ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                        onClick={() => { resetDrawerAction(); setDrawerAction(drawerAction === 'entregue' ? null : 'entregue') }}>
                        ✅ Entregue
                      </button>
                      <button type="button" className={`szv2-btn szv2-btn-sm ${drawerAction === 'frustrado' ? 'szv2-btn-danger' : 'szv2-btn-secondary'}`}
                        onClick={() => { resetDrawerAction(); setDrawerAction(drawerAction === 'frustrado' ? null : 'frustrado') }}>
                        ✗ Frustrado
                      </button>
                    </div>

                    {drawerAction && (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: '12px 14px', background: 'var(--szv2-surface-alt)', borderRadius: 8 }}>
                        {/* Comprovante — obrigatório para entregue/frustrado */}
                        {(drawerAction === 'entregue' || drawerAction === 'frustrado') && (
                          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                            <label style={{ fontSize: 12, fontWeight: 600 }}>Comprovante (foto/PDF) — obrigatório</label>
                            {drawerEvidURL ? (
                              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                <span style={{ fontSize: 12, color: 'var(--szv2-success,#16a34a)' }}>✓ arquivo enviado</span>
                                <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={() => { setDrawerFile(null); setDrawerEvidURL('') }}>Trocar</button>
                              </div>
                            ) : (
                              <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                                <input type="file" accept="image/jpeg,image/png,application/pdf"
                                  onChange={e => { setDrawerFile(e.target.files?.[0] ?? null); setDrawerEvidURL('') }}
                                  style={{ fontSize: 12 }} />
                                {drawerFile && !drawerEvidURL && (
                                  <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                                    disabled={drawerUploading}
                                    onClick={() => drawerUploadEvidence(selectedOrder.sz_order_id!)}>
                                    {drawerUploading ? 'Enviando…' : '↑ Enviar arquivo'}
                                  </button>
                                )}
                              </div>
                            )}
                          </div>
                        )}

                        {/* Motivo — só para frustrado */}
                        {drawerAction === 'frustrado' && (
                          <select value={drawerMotivo} onChange={e => { setDrawerMotivo(e.target.value); setDrawerErr('') }}
                            style={{ padding: '8px 10px', borderRadius: 6, border: '1px solid var(--szv2-border)', fontSize: 13 }}>
                            <option value="">— Motivo (obrigatório) —</option>
                            <option value="cliente_ausente">Cliente ausente</option>
                            <option value="endereco_incorreto">Endereço incorreto</option>
                            <option value="cliente_recusou">Cliente recusou</option>
                            <option value="produto_danificado">Produto danificado</option>
                            <option value="tentativa_sem_sucesso">Tentativa sem sucesso</option>
                            <option value="outro">Outro</option>
                          </select>
                        )}

                        <textarea rows={2} placeholder="Justificativa obrigatória"
                          value={drawerJustif} onChange={e => { setDrawerJustif(e.target.value); setDrawerErr('') }}
                          style={{ width: '100%', padding: '8px 10px', borderRadius: 6, border: '1px solid var(--szv2-border)', fontSize: 13, resize: 'vertical', boxSizing: 'border-box' }} />

                        {drawerErr && <div style={{ fontSize: 12, color: 'var(--szv2-danger)', background: 'var(--szv2-danger-bg,#FEF2F2)', borderRadius: 6, padding: '6px 10px' }}>{drawerErr}</div>}

                        <div style={{ display: 'flex', gap: 8 }}>
                          <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={resetDrawerAction} disabled={drawerBusy}>Cancelar</button>
                          <button type="button"
                            className={`szv2-btn szv2-btn-sm ${drawerAction === 'frustrado' ? 'szv2-btn-danger' : 'szv2-btn-brand'}`}
                            style={{ flex: 1, justifyContent: 'center' }}
                            disabled={drawerBusy || !drawerJustif.trim()
                              || (drawerAction === 'frustrado' && !drawerMotivo)
                              || ((drawerAction === 'entregue' || drawerAction === 'frustrado') && !drawerEvidURL)}
                            onClick={() => drawerForceStatus(selectedOrder)}>
                            {drawerBusy ? 'Salvando…' : `Confirmar ${drawerAction === 'entregue' ? 'Entregue' : 'Frustrado'}`}
                          </button>
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </>
            )}

            {/* ── Aba: UTM ────────────────────────────────────── */}
            {detailTab === 'utm' && (
              <div style={{ paddingTop: 4 }}>
                {detailLoading && selectedOrder.sz_order_id ? (
                  <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Carregando…</div>
                ) : hasUtm && utm ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8, fontSize: 12, fontFamily: 'var(--szv2-font-mono)' }}>
                    {utm.utm_source && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>source</span><span>{utm.utm_source}</span>
                      </div>
                    )}
                    {utm.utm_medium && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>medium</span><span>{utm.utm_medium}</span>
                      </div>
                    )}
                    {utm.utm_campaign && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>campaign</span><span>{utm.utm_campaign}</span>
                      </div>
                    )}
                    {utm.utm_term && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>term</span><span>{utm.utm_term}</span>
                      </div>
                    )}
                    {utm.utm_content && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>content</span><span>{utm.utm_content}</span>
                      </div>
                    )}
                    {utm.referrer && (
                      <div>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>referrer: </span>
                        <a href={safeUrl(utm.referrer)} target="_blank" rel="noreferrer" style={{ color: 'var(--szv2-brand)', wordBreak: 'break-all' }}>{utm.referrer}</a>
                      </div>
                    )}
                    {utm.landing_page && (
                      <div>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>landing: </span>
                        <a href={safeUrl(utm.landing_page)} target="_blank" rel="noreferrer" style={{ color: 'var(--szv2-brand)', wordBreak: 'break-all' }}>{utm.landing_page}</a>
                      </div>
                    )}
                  </div>
                ) : (
                  <EmptyState icon="🎯" title="Sem dados de UTM" description="Este pedido não tem parâmetros de campanha registrados." />
                )}
              </div>
            )}

            {/* ── Aba: Histórico ──────────────────────────────── */}
            {detailTab === 'historico' && (
              <div style={{ paddingTop: 4 }}>
                {detailLoading && selectedOrder.sz_order_id ? (
                  <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Carregando…</div>
                ) : audit.length > 0 ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
                    {audit.map(a => (
                      <div
                        key={a.id}
                        style={{
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 2,
                          padding: '10px 0',
                          borderBottom: '1px solid var(--szv2-divider)',
                          fontSize: 13,
                        }}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                          <span style={{ fontWeight: 600 }}>
                            {a.de_status ? `${displayStatus(a.de_status)} → ` : ''}
                            <span style={{ color: 'var(--szv2-brand)' }}>{a.para_status ? displayStatus(a.para_status) : (a.acao || '—')}</span>
                          </span>
                          <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', whiteSpace: 'nowrap' }}>
                            {fmtDateTime(a.created_at)}
                          </span>
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                          {a.acao || 'ação'}
                          {a.actor_tipo ? ` · ${a.actor_tipo}` : ''}
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <EmptyState icon="🕓" title="Sem histórico" description="Nenhuma mudança de status registrada para este pedido." />
                )}
              </div>
            )}

            {/* ── Aba: Nota Fiscal ────────────────────────────── */}
            {detailTab === 'nfe' && (
              <div style={{ paddingTop: 4 }}>
                {detailLoading && selectedOrder.sz_order_id ? (
                  <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Carregando…</div>
                ) : hasNfe && fiscal ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                    {fiscal.nfe_numero && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>Número</span>
                        <span style={{ fontFamily: 'var(--szv2-font-mono)' }}>{fiscal.nfe_numero}</span>
                      </div>
                    )}
                    {fiscal.nfe_serie && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>Série</span>
                        <span style={{ fontFamily: 'var(--szv2-font-mono)' }}>{fiscal.nfe_serie}</span>
                      </div>
                    )}
                    {fiscal.nfe_status && (
                      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                        <span style={{ color: 'var(--szv2-text-muted)' }}>Status</span>
                        <span>{fiscal.nfe_status}</span>
                      </div>
                    )}
                    {fiscal.nfe_chave && (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                        <span style={{ color: 'var(--szv2-text-muted)', fontSize: 11 }}>Chave (44 dígitos)</span>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 4, padding: 6, background: 'var(--szv2-surface-alt)', borderRadius: 6 }}>
                          <code style={{ flex: 1, fontFamily: 'var(--szv2-font-mono)', fontSize: 11, wordBreak: 'break-all' }}>{fiscal.nfe_chave}</code>
                          <CopyButton text={fiscal.nfe_chave} variant="icon" />
                        </div>
                      </div>
                    )}
                    {fiscal.nfe_url && (
                      <a
                        href={safeUrl(fiscal.nfe_url)}
                        target="_blank"
                        rel="noreferrer"
                        className="szv2-btn szv2-btn-secondary"
                        style={{ marginTop: 4, textAlign: 'center', fontSize: 12 }}
                      >
                        Abrir XML
                      </a>
                    )}
                  </div>
                ) : (
                  <EmptyState icon="📄" title="Sem nota fiscal" description="Nenhuma NF-e emitida para este pedido." />
                )}
              </div>
            )}

          </>
        )}
      </FilterDrawer>
    </div>
  )
}
