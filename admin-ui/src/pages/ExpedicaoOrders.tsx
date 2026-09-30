// Pedidos de Expedição (visão admin, todos os produtores) — porte do padrão
// Cash on Delivery (Orders.tsx) para a listagem de GET /expedicao/orders.
//
// Aprovar/Cancelar/Separar (mark-packed) são rotas nativas Go
// (go/admin/internal/handlers/order_detail.go) — sem hook WC/WordPress, igual
// ao portal-ui/src/pages/Expedicao.tsx. AUDIT-2026-07-28 (dono: "Cancelar não
// aparece no nível admin" + "não dá pra separar pedidos aprovados"): Cancelar
// nunca existiu aqui (só o comentário antigo achando que vinha de fora);
// Separar existia no backend (mark-packed) mas só disparava como efeito
// colateral do botão Imprimir, que só aparecia com status JÁ 'embalado' —
// beco sem saída (não dava pra sair de 'processing' pelo drawer). Corrigido:
// Cancelar chama /orders/{id}/cancel-expedicao; Imprimir/Separar aparece a
// partir de 'processing' com etiqueta emitida (não só depois de já embalado).
import { useEffect, useMemo, useState } from 'react'
import { api, BASE, getToken } from '../api'
import { brl, ymd } from '../utils/format'

// Data/hora pt-BR (DD/MM/AAAA HH:mm). sz_orders.created_at é timestamptz — o texto
// já vem com offset; new Date() converte corretamente sem gambiarra de fuso.
function dt(s: string): string {
  if (!s) return '—'
  // BUG-FIX 2026-07-23: timestamptz do Postgres em texto vem como
  // "2026-07-23 15:52:10.450502-03" — offset de 2 dígitos SEM minutos, não é
  // ISO 8601 válido. new Date() direto pode dar Invalid Date silenciosamente
  // (coluna sumia mostrando "—" mesmo com dado correto vindo do backend).
  const normalized = s.replace(' ', 'T').replace(/([+-]\d{2})$/, '$1:00')
  const d = new Date(normalized)
  if (isNaN(d.getTime())) return s
  return d.toLocaleString('pt-BR', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}
import FilterButton from '../components/FilterButton'
import FilterTopPanel, { FilterField, ActiveFilterChips, type ActiveChip } from '../components/FilterTopPanel'
import FalkSelect from '../components/FalkSelect'
import TableSkeleton from '../components/TableSkeleton'
import ErrorState from '../components/ErrorState'
import EmptyState from '../components/EmptyState'
import DetailDrawer from '../components/DetailDrawer'
import FalkDatePicker from '../components/FalkDatePicker'
import { emitToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'

function csvEscape(v: string): string {
  const s = v ?? ''
  return /[";\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}
function csvNum(v: number | null | undefined): string {
  return (Number.isFinite(v as number) ? (v as number) : 0).toFixed(2).replace('.', ',')
}
function defaultRange() {
  const today = new Date()
  const past = new Date(today)
  past.setDate(past.getDate() - 30)
  return { ini: ymd(past), fim: ymd(today) }
}
// Statuses operacionais (ainda em curso) — usados no filtro "Parados 24h+" abaixo.
const OPERATIONAL_STATUSES = new Set(['pending', 'processing', 'aguardando', 'on-hold', 'em_andamento', 'em_separacao', 'embalado', 'coletado', 'enviado'])

// Tradução PT-BR — fluxo pedido pelo dono (Pendente → Aprovado → Separado →
// Enviado → Entregue / Alerta). Slugs reais vêm do CHECK constraint de
// sz_orders (sz_orders_status_check); cada um é mapeado pro estágio visível
// correspondente. "A retirar" (posto ME aguardando o cliente) não existe
// ainda como estado — falta o webhook de rastreio granular da Melhor Envio
// pra alimentar esse estágio; não fabricado aqui (AUDIT 2026-07-23).
const STATUS_PT: Record<string, { label: string; color: string }> = {
  pending: { label: 'Pendente', color: '#6B7280' },
  aguardando: { label: 'Pendente', color: '#6B7280' },
  'on-hold': { label: 'Pendente', color: '#CA8A04' },
  em_andamento: { label: 'Em Andamento', color: '#B45309' },
  processing: { label: 'Aprovado', color: '#7C3AED' },
  em_separacao: { label: 'Separado', color: '#0E7490' },
  embalado: { label: 'Separado', color: '#0891B2' },
  // AUDIT-2026-07-31 (dono): 'coletado' — operador logístico/admin marcou no
  // ponto de coleta. Continua agrupado em "Separado" no filtro rápido (não é
  // etapa nova pro cliente no rastreio público) mas tem cor própria aqui.
  coletado: { label: 'Coletado', color: '#0369A1' },
  enviado: { label: 'Enviado', color: '#14B8A6' },
  a_caminho: { label: 'A Caminho', color: '#6366F1' },
  entregue: { label: 'Entregue', color: '#16A34A' },
  completo: { label: 'Entregue', color: '#15803D' },
  cancelled: { label: 'Alerta', color: '#EF4444' },
  // AUDIT-2026-07-28 (dono: "cancelei e não estornou ainda, sempre que ocorrer
  // isso deve ir pra Em cancelamento") — derivado no backend (não é status real
  // de sz_orders, é 'cancelled' + estorno de etiqueta ainda pendente).
  em_cancelamento: { label: 'Em cancelamento', color: '#A16207' },
  frustrado: { label: 'Alerta', color: '#EF4444' },
  reembolsado: { label: 'Alerta', color: '#A8A29E' },
}
function ExpStatusBadge({ status }: { status: string }) {
  const slug = (status || '').trim().toLowerCase().replace(/^wc-/, '').replace(/[-\s]+/g, '_')
  const meta = STATUS_PT[slug] ?? { label: status || '—', color: '#475569' }
  return (
    <span
      style={{
        display: 'inline-flex', alignItems: 'center', padding: '3px 11px', borderRadius: 999,
        fontSize: 12, fontWeight: 700, lineHeight: 1.5, color: meta.color,
        background: `${meta.color}1A`, border: `1px solid ${meta.color}33`, whiteSpace: 'nowrap',
      }}
    >
      {meta.label}
    </span>
  )
}

type FreightQuote = {
  service_id: number
  nome: string
  transportadora: string
  preco: number
  prazo_dias: number
  bloqueado: boolean
  indisponivel: boolean
  selecionado: boolean
  emitida: boolean
}

type ExpedicaoRow = {
  id: number
  wc_order_id: number | null
  number: string
  status: string
  financial_status?: string
  scheduled_payment_date?: string
  cliente_nome: string
  cliente_telefone: string
  cliente_cpf: string
  checkout_link_id: number | null
  produtor_nome: string
  product_name: string
  senderzz_offer_name: string
  affiliate_name: string
  shipping_name: string
  tracking_codes: string[]
  tracking_link: string
  shipping_total_raw: number
  producer_net: number
  affiliate_commission: number
  has_label: boolean
  label_error: string
  delivery_date: string
  date_machine: string
  updated_at: string
  dest_cep: string
  dest_logradouro: string
  dest_numero: string
  dest_complemento: string
  dest_bairro: string
  dest_cidade: string
  dest_uf: string
}

type ListResp = { items: ExpedicaoRow[]; total: number; has_more?: boolean }

const EXPEDICAO_CSV_COLUMNS: { header: string; get: (o: ExpedicaoRow) => string }[] = [
  { header: 'Pedido', get: o => o.number || String(o.id) },
  { header: 'Data de entrega', get: o => (o.delivery_date || '').slice(0, 10).split('-').reverse().join('/') },
  { header: 'Status', get: o => (STATUS_PT[normStatus(o.status)] ?? { label: o.status }).label },
  { header: 'Financeiro', get: o => FINANCIAL_LABELS[(o.financial_status || '').trim()] ?? '' },
  { header: 'Pagamento em', get: o => o.scheduled_payment_date || '' },
  { header: 'Cliente', get: o => o.cliente_nome || '' },
  { header: 'Produtor', get: o => o.produtor_nome || '' },
  { header: 'Afiliado', get: o => o.affiliate_name || '' },
  { header: 'Produto', get: o => o.product_name || '' },
  { header: 'Transportadora', get: o => o.shipping_name || '' },
  { header: 'Rastreio', get: o => (o.tracking_codes || []).join(', ') },
  { header: 'CEP destino', get: o => o.dest_cep || '' },
  { header: 'Cidade destino', get: o => o.dest_cidade || '' },
  { header: 'UF destino', get: o => o.dest_uf || '' },
  { header: 'Líquido (valor do produto, R$)', get: o => csvNum(o.producer_net) },
  { header: 'Frete (R$)', get: o => csvNum(o.shipping_total_raw) },
  { header: 'Comissão afiliado (R$)', get: o => csvNum(o.affiliate_commission) },
]
function buildExpedicaoCSV(items: ExpedicaoRow[]): string {
  const head = EXPEDICAO_CSV_COLUMNS.map(c => csvEscape(c.header)).join(';')
  const rows = items.map(o => EXPEDICAO_CSV_COLUMNS.map(c => csvEscape(c.get(o))).join(';'))
  return '﻿' + [head, ...rows].join('\r\n') + '\r\n'
}

// Grupos de filtro rápido — cobrem exatamente os 12 status reais de sz_orders
// (nenhum slug inventado). "Em processamento" cobre pending/processing/aguardando/
// on-hold (ainda não despachado); "cancelado" agrupa cancelled+frustrado+reembolsado.
const FILTER_GROUPS: Record<string, string[]> = {
  todos: [],
  pendente: ['pending', 'aguardando', 'on-hold'],
  em_andamento: ['em_andamento'],
  aprovado: ['processing'],
  separado: ['em_separacao', 'embalado', 'coletado'],
  enviado: ['enviado'],
  // Em trânsito tem aba própria: sem isso o pedido sumia de todas as abas e
  // só aparecia em "Todos", sem ser contado em lugar nenhum.
  a_caminho: ['a_caminho', 'em_transito', 'saiu_para_entrega'],
  entregue: ['entregue', 'completo'],
  // Abas financeiras: a linha entra por financial_status, não pelo status de
  // expedição, por isso a lista de slugs fica vazia (ver FINANCIAL_GROUPS).
  pagamento_agendado: [],
  bloqueio: [],
  vencido: [],
  concluido: [],
  alerta: ['cancelled', 'em_cancelamento', 'frustrado', 'reembolsado'],
}
const FINANCIAL_GROUPS = ['pagamento_agendado', 'bloqueio', 'vencido', 'concluido']
const FINANCIAL_LABELS: Record<string, string> = {
  pagamento_agendado: 'Pagamento agendado',
  bloqueio: 'Bloqueio',
  vencido: 'Vencido',
  concluido: 'Pago',
}
/** 2026-10-24 -> 24/10/2026. Vazio vira string vazia, sem quebrar a linha. */
function fmtDateBR(iso?: string): string {
  const d = (iso || '').slice(0, 10)
  return d.includes('-') ? d.split('-').reverse().join('/') : d
}
const FINANCIAL_COLORS: Record<string, string> = {
  pagamento_agendado: '#9333EA',
  bloqueio: '#7C3AED',
  vencido: '#DC2626',
  concluido: '#15803D',
}
const FILTER_LABELS: Record<string, string> = {
  todos: 'Todos',
  pendente: 'Pendente',
  em_andamento: 'Em Andamento',
  aprovado: 'Aprovado',
  separado: 'Separado',
  enviado: 'Enviado',
  entregue: 'Entregue',
  pagamento_agendado: 'Pagamento Agendado',
  bloqueio: 'Bloqueio',
  vencido: 'Vencidos',
  concluido: 'Concluídos',
  alerta: 'Alerta',
}
const GROUP_ORDER = ['todos', 'pendente', 'em_andamento', 'aprovado', 'separado', 'enviado', 'a_caminho', 'entregue', 'pagamento_agendado', 'bloqueio', 'vencido', 'concluido', 'alerta']

function normStatus(s: string): string {
  let slug = (s || '').toLowerCase()
  if (slug.startsWith('wc-')) slug = slug.slice(3)
  return slug.replace(/[-\s]+/g, '_')
}
function filterGroupOf(slug: string): string {
  for (const gk of GROUP_ORDER) {
    if (gk === 'todos' || FINANCIAL_GROUPS.includes(gk)) continue
    if (FILTER_GROUPS[gk].includes(slug)) return gk
  }
  return 'outros'
}
/** Aba a que a linha pertence: a financeira ganha, quando o pedido já tem uma. */
function chipOf(o: ExpedicaoRow): string {
  const fin = (o.financial_status || '').trim()
  if (FINANCIAL_GROUPS.includes(fin)) return fin
  return filterGroupOf(normStatus(o.status))
}
function rowTracking(o: ExpedicaoRow): string {
  return (o.tracking_codes || []).slice(0, 2).join(', ')
}
function trim(s: string, len: number): string {
  if (!s) return ''
  return s.length > len ? s.slice(0, len - 1) + '…' : s
}

// CPF/CNPJ do cliente com máscara (mesma função de portal-ui/src/pages/Expedicao.tsx).
function formatCPF(doc: string): string {
  const d = (doc || '').replace(/\D/g, '')
  if (d.length === 11) return d.replace(/(\d{3})(\d{3})(\d{3})(\d{2})/, '$1.$2.$3-$4')
  if (d.length === 14) return d.replace(/(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})/, '$1.$2.$3/$4-$5')
  return doc || ''
}

// Telefone do cliente sem o DDI +55 (mesmo padrão de Cash on Delivery).
function stripCountryCode(phone: string): string {
  if (!phone) return ''
  const d = phone.replace(/\D/g, '')
  if ((d.length === 12 || d.length === 13) && d.startsWith('55')) {
    return d.slice(2)
  }
  return phone
}

export default function ExpedicaoOrders() {
  const [rows, setRows] = useState<ExpedicaoRow[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [exporting, setExporting] = useState(false)

  const init = useMemo(defaultRange, [])
  const [chip, setChip] = useState('todos')
  const [search, setSearch] = useState('')
  const [fProd, setFProd] = useState('')
  const [fProdutor, setFProdutor] = useState('')
  const [fAff, setFAff] = useState('')
  // Filtro financeiro: combina com os demais (período, produto, produtor) em vez
  // de depender da aba, que só aceita um recorte por vez.
  const [fFin, setFFin] = useState('')
  const [dataIni, setDataIni] = useState(init.ini)
  const [dataFim, setDataFim] = useState(init.fim)
  const [stopped, setStopped] = useState(false)
  const [filterOpen, setFilterOpen] = useState(false)

  function load() {
    setLoading(true)
    setErr('')
    api<ListResp>('/expedicao/orders')
      .then(resp => { setRows(resp.items || []); setHasMore(!!resp.has_more) })
      .catch(e => setErr(e?.message || 'Erro ao carregar Expedição.'))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const [selected, setSelected] = useState<ExpedicaoRow | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  useEffect(() => {
    setQuotes(null)
    setQuotesMeta(null)
    setQuotesErr('')
    setEditOpen(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id])
  const [approveBusy, setApproveBusy] = useState(false)
  const [collectBusy, setCollectBusy] = useState(false)

  // Aprovar (pedido dono 2026-07-28) — mesma regra do backend: só pendente/
  // aguardando/on-hold. Sem campo actions.can_approve no shape (admin lista TODOS
  // os produtores, não escopado como o portal) — deriva do status aqui, servidor
  // reconfirma (fail-closed, 409 se o status já mudou por fora).
  function canApprove(o: ExpedicaoRow): boolean {
    return ['pending', 'aguardando', 'on-hold'].includes(normStatus(o.status))
  }

  // Cancelar (AUDIT-2026-07-28) — mesma janela de status do backend
  // (cancel-expedicao): pendente, aprovado ou separado. Nada além disso.
  const [cancelBusy, setCancelBusy] = useState(false)
  function canCancel(o: ExpedicaoRow): boolean {
    return ['pending', 'aguardando', 'on-hold', 'em_andamento', 'processing', 'em_separacao', 'embalado'].includes(normStatus(o.status))
  }
  function canEmitLabel(o: ExpedicaoRow): boolean {
    return ['pending', 'aguardando', 'on-hold', 'processing', 'em_separacao', 'embalado'].includes(normStatus(o.status)) && !o.has_label
  }
  // Emitir etiqueta (AUDIT-2026-07-28) — reprocessa emissão pra pedido aprovado
  // sem etiqueta (ver achado do pedido 1660/Cleni).
  const [emitLabelBusy, setEmitLabelBusy] = useState(false)
  async function handleEmitLabel(o: ExpedicaoRow) {
    if (emitLabelBusy) return
    setEmitLabelBusy(true)
    try {
      const r = await api<{ ok: boolean; label_id?: number; label_warning?: string }>(
        `/orders/${o.id}/emit-label`,
        { method: 'POST' },
      )
      emitToast('ok', r.label_id ? `Etiqueta emitida (#${r.label_id}).` : (r.label_warning || 'Processado.'))
      setSelected(null)
      load()
    } catch (e: any) {
      emitToast('err', e?.message || 'Não foi possível emitir a etiqueta.')
    } finally {
      setEmitLabelBusy(false)
    }
  }

  // Edição de campos do pedido (AUDIT-2026-07-29, dono: "possibilidade de
  // editar campos do pedido, somente antes de gerar etiqueta") — nome/CPF/
  // telefone/endereço, bloqueado assim que existe etiqueta (backend reforça).
  const [editBusy, setEditBusy] = useState(false)
  const [editForm, setEditForm] = useState<Record<string, string>>({})
  function openEdit(o: ExpedicaoRow) {
    setEditForm({
      cliente_nome: o.cliente_nome || '',
      cliente_telefone: o.cliente_telefone || '',
      cliente_cpf: o.cliente_cpf || '',
      cep: o.dest_cep || '',
      logradouro: o.dest_logradouro || '',
      numero: o.dest_numero || '',
      complemento: o.dest_complemento || '',
      bairro: o.dest_bairro || '',
      cidade: o.dest_cidade || '',
      uf: o.dest_uf || '',
    })
    setEditOpen(true)
  }
  async function saveEdit(o: ExpedicaoRow) {
    if (editBusy) return
    setEditBusy(true)
    try {
      await api(`/orders/${o.id}/fields`, { method: 'PATCH', body: JSON.stringify(editForm) })
      emitToast('ok', 'Pedido atualizado.')
      setEditOpen(false)
      load()
    } catch (e: any) {
      emitToast('err', e?.message || 'Não foi possível salvar as edições.')
    } finally {
      setEditBusy(false)
    }
  }

  // Cotações de frete AO VIVO (AUDIT-2026-07-29, dono: "mostre o custo de
  // todos os fretes cotados e o serviço mais barato selecionado, SOMENTE NO
  // ADMIN") — recotação em tempo real via ME, mesma regra de bloqueio de
  // Correios + escolha da mais barata que a emissão real usa (emit.go).
  const [quotes, setQuotes] = useState<FreightQuote[] | null>(null)
  const [quotesMeta, setQuotesMeta] = useState<{ from_cep: string; to_cep: string; bloqueia_correios: boolean; fonte: string } | null>(null)
  const [quotesBusy, setQuotesBusy] = useState(false)
  // Alteração do estado financeiro pelo painel: o endpoint já existia
  // (POST /orders/{id}/financial-status), faltava a interface.
  const [finAcao, setFinAcao] = useState<'pagamento_agendado' | 'bloqueio' | 'vencido' | 'concluido' | null>(null)
  const [finData, setFinData] = useState('')
  const [finBusy, setFinBusy] = useState(false)
  const [finErr, setFinErr] = useState('')
  const [quotesErr, setQuotesErr] = useState('')
  // AUDIT-2026-07-29 (dono): "botão pra dar andamento em alguma das cotações
  // ali para o admin, em caso do pedido travar em Em Andamento" — força
  // reemissão com o service_id escolhido manualmente (ignora a escolha
  // automática). Só faz sentido pra pedido SEM etiqueta ainda.
  const [forceCarrierBusy, setForceCarrierBusy] = useState<number | null>(null)
  async function handleForceCarrier(o: ExpedicaoRow, serviceId: number) {
    if (forceCarrierBusy !== null) return
    setForceCarrierBusy(serviceId)
    try {
      const r = await api<{ ok: boolean; label_id?: number; label_warning?: string }>(
        `/orders/${o.id}/force-carrier`,
        { method: 'POST', body: JSON.stringify({ service_id: serviceId }) },
      )
      emitToast('ok', r.label_id ? `Etiqueta emitida (#${r.label_id}) com essa transportadora.` : (r.label_warning || 'Processado.'))
      setSelected(null)
      load()
    } catch (e: any) {
      emitToast('err', e?.message || 'Não foi possível emitir com essa transportadora.')
    } finally {
      setForceCarrierBusy(null)
    }
  }

  async function salvarFinanceiro(o: ExpedicaoRow) {
    if (!finAcao) return
    if (finAcao === 'pagamento_agendado' && !finData) {
      setFinErr('Informe a data do pagamento.')
      return
    }
    setFinBusy(true)
    setFinErr('')
    try {
      await api(`/orders/${o.id}/financial-status`, {
        method: 'POST',
        body: JSON.stringify({
          financial_status: finAcao,
          scheduled_payment_date: finAcao === 'pagamento_agendado' ? finData : null,
        }),
      })
      emitToast('ok', `Pedido ${o.number || o.id}: ${FINANCIAL_LABELS[finAcao].toLowerCase()}.`)
      setFinAcao(null)
      setFinData('')
      await load()
      setSelected(null)
    } catch (e: any) {
      setFinErr(e?.message || 'Não foi possível salvar.')
    } finally {
      setFinBusy(false)
    }
  }

  async function loadQuotes(o: ExpedicaoRow) {
    if (quotesBusy) return
    setQuotesBusy(true)
    setQuotesErr('')
    setQuotes(null)
    try {
      const r = await api<{ ok: boolean; fonte: string; from_cep: string; to_cep: string; bloqueia_correios?: boolean; cotacoes: FreightQuote[] }>(
        `/orders/${o.id}/freight-quotes`,
      )
      setQuotes(r.cotacoes)
      setQuotesMeta({ from_cep: r.from_cep, to_cep: r.to_cep, bloqueia_correios: !!r.bloqueia_correios, fonte: r.fonte })
    } catch (e: any) {
      setQuotesErr(e?.message || 'Não foi possível cotar frete.')
    } finally {
      setQuotesBusy(false)
    }
  }

  async function handleCancel(o: ExpedicaoRow, backToPending = false) {
    if (cancelBusy) return
    const ok = await confirmAsync({
      title: backToPending ? 'Cancelar etiqueta' : 'Cancelar pedido',
      message: backToPending
        ? `Cancelar a etiqueta atual do pedido ${o.number || o.id} e voltar o pedido para pendente?`
        : `Tem certeza que deseja cancelar o pedido ${o.number || o.id}? Esta ação não pode ser desfeita.`,
      confirmLabel: backToPending ? 'Voltar para pendente' : 'Cancelar pedido',
      cancelLabel: 'Voltar',
      danger: !backToPending,
      variant: backToPending ? 'warning' : 'danger',
    })
    if (!ok) return
    setCancelBusy(true)
    try {
      const r = await api<{ ok: boolean; status: string; label_warning?: string; estorno_mensagem?: string }>(
        `/orders/${o.id}/cancel-expedicao`,
        { method: 'POST', body: backToPending ? JSON.stringify({ target_status: 'pending' }) : undefined },
      )
      const extra = r.label_warning || r.estorno_mensagem
      emitToast('ok', backToPending
        ? `Pedido ${o.number || o.id} voltou para pendente.` + (extra ? ` ${extra}` : '')
        : `Pedido ${o.number || o.id} cancelado.` + (extra ? ` ${extra}` : ''))
      setSelected(null)
      load()
    } catch (e: any) {
      emitToast('err', e?.message || 'Não foi possível cancelar o pedido.')
    } finally {
      setCancelBusy(false)
    }
  }

  // Imprimir etiqueta (PDF real, baixado — nunca a página interativa da ME) e
  // Declaração de Conteúdo, direto no drawer, individual OU em lote (mesmos 2
  // botões, a única diferença é a lista de ids) — pedido dono 2026-07-28: "só
  // era os botão de imprimir etiqueta e declaração além de poder selecionar".
  const [printBusy, setPrintBusy] = useState(false)
  // Seleção em lote (pedido dono 2026-07-28: "poder selecionar" pra imprimir/
  // declarar vários de uma vez, sem sair da lista).
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  function toggleSelect(id: number) {
    setSelectedIds(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  // AUDIT-2026-07-28: sem endpoint público da API ME que devolva bytes de PDF
  // puro (3 testes: JSON completo, content-negotiation, curl direto na URL do
  // label já emitido — todos HTML de login/SPA sem cookie de sessão). Mesmo
  // fluxo do legado WP: quem opera fica logado em melhorenvio.com.br no
  // próprio navegador — a URL abre a etiqueta já renderizada (Ctrl+P > salvar
  // PDF), igual sempre funcionou. Sem sessão ativa lá, cai em tela de login.
  // AUDIT-2026-07-28: /bulk-actions/print-batch devolve BYTES reais agora
  // (PDF puro se 1 pedido, ZIP se vários) — não mais JSON {label_url}. Usa
  // fetch cru (não o helper api(), que sempre tenta JSON.parse) e dispara o
  // download via blob URL, sem abrir nenhum site.
  async function downloadPrintBatch(ids: number[], wantLabels: boolean, wantDeclaration: boolean, fallbackName: string) {
    const res = await fetch(`${BASE}/bulk-actions/print-batch`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${getToken() ?? ''}`,
      },
      body: JSON.stringify({ order_ids: ids, want_labels: wantLabels, want_declaration: wantDeclaration }),
    })
    if (!res.ok) {
      const text = await res.text().catch(() => '')
      throw new Error(text || `HTTP ${res.status}`)
    }
    const blob = await res.blob()
    const cd = res.headers.get('Content-Disposition') || ''
    const match = /filename="([^"]+)"/.exec(cd)
    const filename = match?.[1] || fallbackName
    const blobUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = blobUrl
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(blobUrl), 30000)
  }

  async function printLabelsPDF(ids: number[]) {
    if (printBusy || ids.length === 0) return
    setPrintBusy(true)
    try {
      await downloadPrintBatch(ids, true, false, ids.length > 1 ? 'etiquetas.pdf' : `etiqueta-${ids[0]}.pdf`)
      await Promise.all(ids.map(id => api(`/orders/${id}/mark-packed`, { method: 'POST' }).catch(() => {})))
      load()
    } catch (e: any) {
      emitToast('err', e.message || 'Falha ao baixar etiqueta(s).')
    } finally {
      setPrintBusy(false)
    }
  }

  async function printDeclaration(ids: number[]) {
    if (printBusy || ids.length === 0) return
    setPrintBusy(true)
    try {
      await downloadPrintBatch(ids, false, true, ids.length > 1 ? 'declaracoes.pdf' : `dace-${ids[0]}.pdf`)
    } catch (e: any) {
      emitToast('err', e.message || 'Falha ao baixar declaração de conteúdo.')
    } finally {
      setPrintBusy(false)
    }
  }
  async function handleApprove(o: ExpedicaoRow) {
    if (approveBusy) return
    setApproveBusy(true)
    try {
      const r = await api<{ ok: boolean; status: string; label_id?: number; label_warning?: string }>(
        `/orders/${o.id}/approve`,
        { method: 'POST' },
      )
      emitToast('ok', `Pedido ${o.number || o.id} aprovado.` + (r.label_warning ? ` ⚠️ ${r.label_warning}` : r.label_id ? ' Etiqueta emitida.' : ''))
      setSelected(null)
      load()
    } catch (e: any) {
      emitToast('err', e?.message || 'Não foi possível aprovar o pedido.')
    } finally {
      setApproveBusy(false)
    }
  }

  // AUDIT-2026-07-31 (dono): operador logístico/admin marca 'coletado' ao
  // colocar o pacote no ponto de coleta. 'enviado' sempre sobrepõe (a ME
  // confirmando postagem vence — ver label_jobs.go ProcessSyncTracking).
  async function handleMarkCollected(o: ExpedicaoRow) {
    if (collectBusy) return
    setCollectBusy(true)
    try {
      const r = await api<{ ok: boolean; status: string; changed: boolean }>(
        `/orders/${o.id}/mark-collected`,
        { method: 'POST' },
      )
      emitToast('ok', r.changed ? `Pedido ${o.number || o.id} marcado como coletado.` : 'Pedido já estava além de "embalado".')
      setSelected(null)
      load()
    } catch (e: any) {
      emitToast('err', e?.message || 'Não foi possível marcar como coletado.')
    } finally {
      setCollectBusy(false)
    }
  }

  async function exportCSV() {
    if (exporting) return
    setExporting(true)
    try {
      if (visible.length === 0) {
        emitToast('info', 'Nenhum pedido para exportar com esses filtros.')
        return
      }
      const blob = new Blob([buildExpedicaoCSV(visible)], { type: 'text/csv;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `expedicao-${ymd(new Date())}.csv`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      emitToast('ok', `Relatório exportado (${visible.length} pedido${visible.length > 1 ? 's' : ''}).`)
    } catch (e) {
      emitToast('err', e instanceof Error ? e.message : 'Falha ao exportar o relatório.')
    } finally {
      setExporting(false)
    }
  }

  const products = useMemo(() => {
    const set = new Set<string>()
    rows.forEach(o => { if (o.product_name) set.add(o.product_name) })
    return Array.from(set).sort((a, b) => a.localeCompare(b, 'pt-BR'))
  }, [rows])
  const produtores = useMemo(() => {
    const set = new Set<string>()
    rows.forEach(o => { if (o.produtor_nome) set.add(o.produtor_nome) })
    return Array.from(set).sort((a, b) => a.localeCompare(b, 'pt-BR'))
  }, [rows])
  const affiliates = useMemo(() => {
    const set = new Set<string>()
    rows.forEach(o => { if (o.affiliate_name) set.add(o.affiliate_name) })
    return Array.from(set).sort((a, b) => a.localeCompare(b, 'pt-BR'))
  }, [rows])

  // Base comum dos badges e da lista: tudo o que está filtrado MENOS o status.
  // Contar sobre `rows` fazia o badge dizer um número e a lista mostrar outro.
  const baseRows = useMemo(() => {
    const q = search.trim().toLowerCase()
    const now = Date.now()

    return rows.filter(o => {
      const slug = normStatus(o.status)
      const dateIso = (o.delivery_date || o.date_machine || '').slice(0, 10)

      if (stopped) {
        if (!OPERATIONAL_STATUSES.has(slug)) return false
        const upd = Date.parse(o.updated_at)
        if (!upd || now - upd < 24 * 60 * 60 * 1000) return false
      } else {
        if (dataIni && dateIso && dateIso < dataIni) return false
        if (dataFim && dateIso && dateIso > dataFim) return false
      }
      if (fProd && o.product_name !== fProd) return false
      if (fProdutor && o.produtor_nome !== fProdutor) return false
      if (fAff && o.affiliate_name !== fAff) return false
      if (fFin) {
        const fin = (o.financial_status || '').trim()
        if (fFin === 'sem_status' ? FINANCIAL_GROUPS.includes(fin) : fin !== fFin) return false
      }
      if (q) {
        const hay = `${o.number} ${o.cliente_nome} ${o.produtor_nome} ${o.product_name} ${rowTracking(o)} ${o.affiliate_name}`.toLowerCase()
        if (hay.indexOf(q) === -1) return false
      }
      return true
    })
  }, [rows, search, fProd, fProdutor, fAff, fFin, dataIni, dataFim, stopped])

  const groupCounts = useMemo(() => {
    const counts: Record<string, number> = {}
    GROUP_ORDER.forEach(gk => { counts[gk] = 0 })
    baseRows.forEach(o => {
      counts.todos++
      const gk = chipOf(o)
      if (gk !== 'outros') counts[gk] = (counts[gk] || 0) + 1
    })
    return counts
  }, [baseRows])
  const chips = useMemo(
    () => GROUP_ORDER.filter(gk => gk === 'todos' || (groupCounts[gk] || 0) > 0),
    [groupCounts],
  )

  // A lista é a mesma base dos badges, com o status escolhido aplicado por
  // último: o número do badge é exatamente quantas linhas aparecem ao clicar.
  const visible = useMemo(() => {
    if (stopped || chip === 'todos') return baseRows
    return baseRows.filter(o => chipOf(o) === chip)
  }, [baseRows, chip, stopped])

  const activeChips: ActiveChip[] = []
  if (stopped) activeChips.push({ key: 'stopped', label: 'Parados 24h+', onRemove: () => setStopped(false) })
  if (chip !== 'todos') activeChips.push({ key: 'chip', label: `Status: ${FILTER_LABELS[chip] || chip}`, onRemove: () => setChip('todos') })
  if (fProd) activeChips.push({ key: 'prod', label: `Produto: ${fProd}`, onRemove: () => setFProd('') })
  if (fProdutor) activeChips.push({ key: 'produtor', label: `Produtor: ${fProdutor}`, onRemove: () => setFProdutor('') })
  if (fAff) activeChips.push({ key: 'aff', label: `Afiliado: ${fAff}`, onRemove: () => setFAff('') })
  if (fFin) activeChips.push({
    key: 'fin',
    label: `Pagamento: ${fFin === 'sem_status' ? 'sem estado' : FINANCIAL_LABELS[fFin]}`,
    onRemove: () => setFFin(''),
  })
  if (dataIni !== init.ini) activeChips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni(init.ini) })
  if (dataFim !== init.fim) activeChips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim(init.fim) })
  if (search) activeChips.push({ key: 'search', label: `Busca: ${search}`, onRemove: () => setSearch('') })
  const activeFilterCount = activeChips.length
  function clearFilters() {
    setChip('todos'); setFProd(''); setFProdutor(''); setFAff(''); setFFin('')
    setDataIni(init.ini); setDataFim(init.fim); setStopped(false); setSearch('')
  }

  // AUDIT-2026-07-31 (dono): copia o LINK público de rastreio FALK
  // (/checkout/rastreio/{code}), não o código bruto da transportadora —
  // é o que o cliente recebe, o admin/produtor deve mandar a mesma coisa.
  // Fallback pro código bruto só se o link não vier (SALT ausente/order_number
  // vazio — não deve acontecer em produção, mas não trava o botão).
  async function copyTracking(link: string, fallbackCode: string) {
    const text = link || fallbackCode
    try {
      await navigator.clipboard?.writeText(text)
      emitToast('ok', link ? 'Link de rastreio copiado!' : 'Rastreio copiado! (sem link — só o código)')
    } catch {
      emitToast('err', 'Não foi possível copiar o rastreio.')
    }
  }

  return (
    <div>
      <div className="szv2-section-head" style={{ flexWrap: 'wrap', gap: 8 }}>
        <div>
          <h1>Pedidos de Expedição</h1>
          <p>
            {loading ? 'Carregando…' : `${visible.length} pedido(s) encontrado(s)`}
            {stopped ? ' — parados 24h+' : ''}
            {activeFilterCount > 0 ? ` · ${activeFilterCount} filtro(s) ativo(s)` : ''}
          </p>
        </div>
        <div style={{ display: 'flex', flexDirection: 'row', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary szv2-btn-sm"
            onClick={exportCSV}
            disabled={exporting || visible.length === 0}
            title="Exportar os pedidos filtrados em CSV"
          >
            {exporting ? 'Exportando…' : '↓ Exportar relatórios'}
          </button>
          <FilterButton active={activeFilterCount > 0} count={activeFilterCount} onClick={() => setFilterOpen(true)} />
        </div>
      </div>

      <ActiveFilterChips chips={activeChips} onClearAll={clearFilters} />

      {err && rows.length > 0 && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}
      {!loading && !err && hasMore && (
        <div className="sz-alert-danger" style={{ marginBottom: 12, background: 'var(--szv2-warning-bg, #FEF3C7)', color: 'var(--szv2-warning, #92400E)', borderColor: 'var(--szv2-warning, #92400E)' }}>
          Lista truncada em 500 pedidos — use os filtros pra restringir o período.
        </div>
      )}

      {!loading && (
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center', marginBottom: 12 }}>
          {chips.map(gk => {
            const active = chip === gk
            const n = groupCounts[gk] || 0
            return (
              <button
                key={gk}
                type="button"
                className={`szv2-chip szv2-filter-chip${active ? ' szv2-chip-active' : ''}`}
                onClick={() => setChip(gk)}
                style={{
                  display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 13px', borderRadius: 99,
                  border: `1.5px solid ${active ? 'var(--szv2-brand)' : 'var(--szv2-border)'}`,
                  background: active ? 'var(--szv2-brand)' : 'var(--szv2-surface)',
                  color: active ? 'var(--szv2-on-brand)' : 'var(--szv2-text-soft)',
                  fontSize: 13, fontWeight: 600, cursor: 'pointer', whiteSpace: 'nowrap', transition: 'all .12s',
                }}
              >
                {FILTER_LABELS[gk]}
                <span style={{
                  display: 'inline-flex', alignItems: 'center', justifyContent: 'center', minWidth: 20, height: 20,
                  padding: '0 5px', fontSize: 11, fontWeight: 700, borderRadius: 99,
                  background: active ? 'rgba(255,255,255,.25)' : 'rgba(0,0,0,.08)',
                }}>
                  {String(n)}
                </span>
              </button>
            )
          })}
          <button
            type="button"
            className={`szv2-chip szv2-filter-chip${stopped ? ' szv2-chip-active' : ''}`}
            onClick={() => setStopped(v => !v)}
            title="Pedidos operacionais sem atualização há 24h ou mais"
            style={{
              display: 'inline-flex', alignItems: 'center', gap: 6, padding: '5px 13px', borderRadius: 99,
              border: `1.5px solid ${stopped ? 'var(--szv2-danger)' : 'var(--szv2-border)'}`,
              background: stopped ? 'var(--szv2-danger)' : 'var(--szv2-surface)',
              color: stopped ? '#fff' : 'var(--szv2-text-soft)',
              fontSize: 13, fontWeight: 600, cursor: 'pointer', whiteSpace: 'nowrap',
            }}
          >
            ⏱ Parados 24h+
          </button>
          <input
            type="search"
            className="szv2-input"
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Buscar pedido, cliente, produtor ou rastreio…"
            autoComplete="new-password"
            spellCheck={false}
            style={{ flex: 1, minWidth: 200, height: 38, fontSize: 13 }}
          />
        </div>
      )}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={() => setFilterOpen(false)}
        onClear={clearFilters}
        title="Filtros de Expedição"
      >
          <FilterField label="Pagamento">
            <FalkSelect
              aria-label="Filtrar por estado do pagamento"
              value={fFin}
              onChange={v => setFFin(v)}
              placeholder="Qualquer pagamento"
              options={[
                { value: '', label: 'Qualquer pagamento' },
                { value: 'pagamento_agendado', label: 'Pagamento agendado' },
                { value: 'vencido', label: 'Vencido' },
                { value: 'concluido', label: 'Pago' },
                { value: 'sem_status', label: 'Sem estado financeiro' },
              ]}
            />
          </FilterField>
        {products.length > 0 && (
          <FilterField label="Produto">
            <FalkSelect
              aria-label="Filtrar por produto"
              value={fProd}
              onChange={v => setFProd(v)}
              placeholder="Todos os produtos"
              options={[{ value: '', label: 'Todos os produtos' }, ...products.map(v => ({ value: v, label: v }))]}
            />
          </FilterField>
        )}
        {produtores.length > 0 && (
          <FilterField label="Produtor">
            <FalkSelect
              aria-label="Filtrar por produtor"
              value={fProdutor}
              onChange={v => setFProdutor(v)}
              placeholder="Todos os produtores"
              options={[{ value: '', label: 'Todos os produtores' }, ...produtores.map(v => ({ value: v, label: v }))]}
            />
          </FilterField>
        )}
        {affiliates.length > 0 && (
          <FilterField label="Afiliado">
            <FalkSelect
              aria-label="Filtrar por afiliado"
              value={fAff}
              onChange={v => setFAff(v)}
              placeholder="Todos os afiliados"
              options={[{ value: '', label: 'Todos os afiliados' }, ...affiliates.map(v => ({ value: v, label: v }))]}
            />
          </FilterField>
        )}
        <FilterField label="De">
          <FalkDatePicker value={dataIni} onChange={setDataIni} max={dataFim} aria-label="Data inicial" />
        </FilterField>
        <FilterField label="Até">
          <FalkDatePicker value={dataFim} onChange={setDataFim} min={dataIni} aria-label="Data final" />
        </FilterField>
      </FilterTopPanel>

      {loading ? (
        <TableSkeleton rows={6} cols={8} />
      ) : err && rows.length === 0 ? (
        <ErrorState message={err} onRetry={load} />
      ) : rows.length === 0 ? (
        <EmptyState
          icon="📦"
          title="Nenhum pedido de Expedição"
          description="Os pedidos com frete aparecem aqui assim que chegarem."
        />
      ) : (
      <div className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
        <div className="szv2-table-wrap">
          <table className="szv2-table" style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={{ width: 32 }}></th>
                <th style={{ whiteSpace: 'nowrap' }}>PEDIDO</th>
                <th>CLIENTE</th>
                <th>STATUS</th>
                <th>PRODUTOR</th>
                <th>PRODUTO</th>
                <th>TRANSPORTADORA</th>
                <th>RASTREIO</th>
                <th style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>LÍQUIDO</th>
                <th style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>FRETE</th>
              </tr>
            </thead>
            <tbody>
              {visible.map(o => {
                const num = o.number || String(o.id)
                const clienteTel = stripCountryCode(o.cliente_telefone || '')
                const tracking = rowTracking(o)
                const dateFmt = o.delivery_date ? o.delivery_date.slice(0, 10).split('-').reverse().join('/') : (o.date_machine ? dt(o.date_machine) : '—')
                return (
                  <tr
                    key={o.id}
                    data-status={normStatus(o.status)}
                    onClick={() => setSelected(o)}
                    style={{ cursor: 'pointer' }}
                    title="Clique para ver detalhes"
                  >
                    <td onClick={e => e.stopPropagation()}>
                      {/* AUDIT-2026-07-29 (dono): seleção pra impressão em lote agora cobre
                          Aprovado (processing) e Separado (em_separacao) além de Embalado —
                          antes só embalado selecionava; etiqueta só existe (has_label) a partir
                          de Aprovado mesmo, então nada impede imprimir mais cedo no fluxo. */}
                      {['processing', 'em_separacao', 'embalado'].includes(normStatus(o.status)) && o.has_label && (
                        <input type="checkbox" checked={selectedIds.has(o.id)} onChange={() => toggleSelect(o.id)} />
                      )}
                    </td>
                    <td style={{ whiteSpace: 'nowrap' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                        <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--szv2-text)' }}>{num}</span>
                        <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{dateFmt}</span>
                      </div>
                    </td>
                    <td>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                        <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--szv2-text)' }}>{o.cliente_nome || '—'}</span>
                        {!!clienteTel && (
                          <span
                            role="button"
                            title="Copiar telefone"
                            onClick={async e => {
                              e.stopPropagation()
                              try {
                                await navigator.clipboard.writeText(clienteTel)
                                emitToast('ok', 'Telefone copiado!')
                              } catch {
                                emitToast('err', 'Não foi possível copiar.')
                              }
                            }}
                            style={{ fontSize: 11, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', cursor: 'pointer' }}
                          >
                            {clienteTel} 📋
                          </span>
                        )}
                      </div>
                      {!!o.affiliate_name && (
                        <div style={{ fontSize: 11, marginTop: 2 }}>
                          <span style={{ padding: '1px 6px', background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', borderRadius: 99, fontWeight: 600 }}>
                            {o.affiliate_name}
                          </span>
                        </div>
                      )}
                    </td>
                    <td style={{ padding: '12px 14px' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 4 }}>
                        <ExpStatusBadge status={o.status} />
                        {/* Estado financeiro: fica abaixo do status de expedição, em
                            peso menor. Duas pílulas lado a lado brigavam entre si; um
                            ponto colorido + texto deixa claro quem é o principal. */}
                        {FINANCIAL_GROUPS.includes((o.financial_status || '').trim()) && (() => {
                          const fin = (o.financial_status || '').trim()
                          const cor = FINANCIAL_COLORS[fin]
                          const data = fmtDateBR(o.scheduled_payment_date)
                          return (
                            <span
                              title={
                                fin === 'pagamento_agendado' && data
                                  ? `Pagamento agendado para ${data}`
                                  : FINANCIAL_LABELS[fin]
                              }
                              style={{
                                display: 'inline-flex',
                                alignItems: 'center',
                                gap: 5,
                                paddingLeft: 2,
                                fontSize: 11,
                                lineHeight: 1.2,
                                whiteSpace: 'nowrap',
                                color: fin === 'vencido' ? cor : 'var(--szv2-text-muted)',
                                fontWeight: fin === 'vencido' ? 600 : 500,
                              }}
                            >
                              <span
                                aria-hidden
                                style={{
                                  width: 6,
                                  height: 6,
                                  borderRadius: '50%',
                                  background: cor,
                                  flex: '0 0 auto',
                                }}
                              />
                              {fin === 'pagamento_agendado' && data
                                ? `Pagamento ${data.slice(0, 5)}`
                                : FINANCIAL_LABELS[fin]}
                            </span>
                          )
                        })()}
                      </div>
                    </td>
                    <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)' }}>{o.produtor_nome || '—'}</td>
                    <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)', maxWidth: 180 }}>
                      <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={o.product_name}>
                        {trim(o.product_name, 32) || '—'}
                      </div>
                    </td>
                    <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)' }}>{o.shipping_name || '—'}</td>
                    <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)', whiteSpace: 'nowrap' }}>
                      {tracking !== '' ? (
                        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                          {/* AUDIT-2026-07-31 (dono: "clicar no rastreio não redireciona
                              pro rastreio") — o código em si agora é um link clicável
                              (abre a página pública de rastreio numa aba nova); o botão
                              ao lado continua só copiando o link, ações separadas. */}
                          {o.tracking_link ? (
                            <a
                              href={o.tracking_link}
                              target="_blank"
                              rel="noreferrer"
                              onClick={e => e.stopPropagation()}
                              title={`Abrir rastreio — ${tracking}`}
                              style={{ color: 'var(--szv2-brand)', textDecoration: 'none' }}
                            >
                              {trim(tracking, 20)}
                            </a>
                          ) : (
                            <span title={tracking}>{trim(tracking, 20)}</span>
                          )}
                          <button
                            type="button"
                            onClick={e => { e.stopPropagation(); copyTracking(o.tracking_link, tracking) }}
                            title="Copiar link de rastreio"
                            style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--szv2-text-muted)', padding: 0 }}
                          >
                            ⧉
                          </button>
                        </span>
                      ) : (
                        <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                      )}
                    </td>
                    <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                      <span style={{ fontWeight: 700, fontSize: 13, color: 'var(--szv2-text)', fontFamily: 'var(--szv2-font-mono)' }}>{brl(o.producer_net)}</span>
                    </td>
                    <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                      <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--szv2-text)', fontFamily: 'var(--szv2-font-mono)' }}>{brl(o.shipping_total_raw)}</span>
                    </td>
                  </tr>
                )
              })}
              {visible.length === 0 && (
                <tr>
                  <td colSpan={10} style={{ textAlign: 'center', padding: '40px 16px', color: 'var(--szv2-text-muted)', fontSize: 13 }}>
                    Nenhum pedido para esse filtro. Ajuste a busca ou limpe os filtros.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <div style={{ padding: '10px 16px', borderTop: '1px solid var(--szv2-border)', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          <span>Mostrando {visible.length} pedido{visible.length !== 1 ? 's' : ''}</span>
        </div>
      </div>
      )}

      {/* Barra flutuante de ações em lote — pedido dono 2026-07-28. */}
      {selectedIds.size > 0 && (
        <div
          style={{
            position: 'fixed', bottom: 24, left: '50%', transform: 'translateX(-50%)',
            background: 'var(--szv2-card-bg, #fff)', border: '1.5px solid var(--szv2-border)',
            borderRadius: 12, boxShadow: '0 8px 32px rgba(0,0,0,.18)',
            padding: '10px 14px', display: 'flex', alignItems: 'center', gap: 10, zIndex: 1000,
          }}
        >
          <span style={{ fontWeight: 700, color: 'var(--szv2-brand)', whiteSpace: 'nowrap' }}>
            {selectedIds.size} selecionado{selectedIds.size !== 1 ? 's' : ''}
          </span>
          <button
            type="button"
            className="szv2-btn szv2-btn-brand"
            disabled={printBusy}
            onClick={() => printLabelsPDF(Array.from(selectedIds))}
          >
            🖨️ Imprimir etiquetas
          </button>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={printBusy}
            onClick={() => printDeclaration(Array.from(selectedIds))}
          >
            📄 Imprimir declaração
          </button>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={printBusy}
            onClick={() => { printLabelsPDF(Array.from(selectedIds)); printDeclaration(Array.from(selectedIds)) }}
          >
            🖨️📄 Imprimir ambos
          </button>
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setSelectedIds(new Set())}>
            Limpar
          </button>
        </div>
      )}

      {selected && (
        <DetailDrawer
          open
          onClose={() => setSelected(null)}
          title={`Pedido ${selected.wc_order_id ?? selected.id} — ${(STATUS_PT[normStatus(selected.status)] ?? { label: selected.status }).label}`}
        >
          <>
            {/* AUDIT-2026-07-28 (dono: "deixa o drawer no admin igual ao de
                produtor, mantendo as funcionalidades exclusivas de admin") —
                mesma estrutura de seções (Endereço/Envio/Financeiro/Ações) e
                estilo do drawer do portal-ui (Expedicao.tsx), com os campos e
                botões exclusivos de admin (Produtor, Emitir etiqueta,
                Imprimir/Separar, Declaração) preservados dentro dela. */}
            <div style={{ paddingTop: 4 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>
                  Endereço de destino
                </span>
                {/* AUDIT-2026-07-29: edição só antes de gerar etiqueta (backend reforça). */}
                {!selected.has_label && !editOpen && (
                  <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={() => openEdit(selected)}>
                    Editar
                  </button>
                )}
              </div>
              {editOpen ? (
                <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {([
                    ['cliente_nome', 'Nome do cliente'], ['cliente_telefone', 'Telefone'], ['cliente_cpf', 'CPF'],
                    ['cep', 'CEP'], ['logradouro', 'Logradouro'], ['numero', 'Número'],
                    ['complemento', 'Complemento'], ['bairro', 'Bairro'], ['cidade', 'Cidade'], ['uf', 'UF'],
                  ] as [string, string][]).map(([key, label]) => (
                    <label key={key} style={{ display: 'flex', flexDirection: 'column', gap: 2, fontSize: 12 }}>
                      <span style={{ color: 'var(--szv2-text-muted)' }}>{label}</span>
                      <input
                        className="szv2-input"
                        value={editForm[key] ?? ''}
                        onChange={e => setEditForm(f => ({ ...f, [key]: e.target.value }))}
                      />
                    </label>
                  ))}
                  <div style={{ display: 'flex', gap: 8, marginTop: 4 }}>
                    <button type="button" className="szv2-btn szv2-btn-brand szv2-btn-sm" disabled={editBusy} onClick={() => saveEdit(selected)}>
                      {editBusy ? 'Salvando…' : 'Salvar'}
                    </button>
                    <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" disabled={editBusy} onClick={() => setEditOpen(false)}>
                      Cancelar
                    </button>
                  </div>
                </div>
              ) : (
                <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.6 }}>
                  {selected.dest_logradouro ? (
                    <>
                      <div>
                        {selected.dest_logradouro}
                        {selected.dest_numero ? `, ${selected.dest_numero}` : ''}
                      </div>
                      {selected.dest_complemento && <div>{selected.dest_complemento}</div>}
                      {selected.dest_bairro && <div>{selected.dest_bairro}</div>}
                      <div>
                        {selected.dest_cidade || '—'}
                        {selected.dest_uf ? `/${selected.dest_uf}` : ''}
                      </div>
                      {selected.dest_cep && (
                        <div style={{ color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                          CEP {selected.dest_cep}
                        </div>
                      )}
                    </>
                  ) : (
                    <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                  )}
                </div>
              )}
            </div>

            <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>
                Envio
              </span>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Cliente</span>
                  <span style={{ fontWeight: 600 }}>{selected.cliente_nome || '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>CPF</span>
                  <span style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)' }}>{formatCPF(selected.cliente_cpf) || '—'}</span>
                </div>
                {/* Produtor — exclusivo admin (visão global, produtor não precisa ver o próprio nome). */}
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Produtor</span>
                  <span style={{ fontWeight: 600 }}>{selected.produtor_nome || '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Produto</span>
                  <span style={{ fontWeight: 600 }}>{selected.product_name || '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>ID do checkout</span>
                  <span style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)' }}>{selected.checkout_link_id ?? '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Transportadora</span>
                  <span style={{ fontWeight: 600 }}>{selected.shipping_name || '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Rastreio</span>
                  <span style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)', display: 'flex', flexWrap: 'wrap', gap: 6, justifyContent: 'flex-end' }}>
                    {selected.tracking_codes.length > 0
                      ? selected.tracking_codes.map(c => <span key={c}>{c}</span>)
                      : '—'}
                  </span>
                </div>
                {!!selected.label_error && (
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                    <span style={{ color: '#B91C1C' }}>⚠️ Falha na última emissão</span>
                    <span style={{ fontWeight: 600, color: '#B91C1C', textAlign: 'right' }}>{selected.label_error}</span>
                  </div>
                )}
                {!!selected.affiliate_name && (
                  <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                    <span style={{ color: 'var(--szv2-text-muted)' }}>Afiliado</span>
                    <span style={{ fontWeight: 600 }}>{selected.affiliate_name}</span>
                  </div>
                )}
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Data</span>
                  <span style={{ fontWeight: 600 }}>{selected.date_machine ? dt(selected.date_machine) : '—'}</span>
                </div>
              </div>
            </div>

            <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>
                Financeiro
              </span>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Valor (produto)</span>
                  <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)' }}>{brl(selected.producer_net)}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Frete</span>
                  <span style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-text)' }}>{brl(selected.shipping_total_raw)}</span>
                </div>
                {FINANCIAL_GROUPS.includes((selected.financial_status || '').trim()) && (
                  <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                    <span style={{ color: 'var(--szv2-text-muted)' }}>Situação</span>
                    <span style={{ fontWeight: 600, color: FINANCIAL_COLORS[(selected.financial_status || '').trim()] }}>
                      {FINANCIAL_LABELS[(selected.financial_status || '').trim()]}
                      {selected.scheduled_payment_date ? ` · ${fmtDateBR(selected.scheduled_payment_date)}` : ''}
                    </span>
                  </div>
                )}
              </div>
            </div>

            {/* Estado do pagamento, alterável aqui. O backend já expunha
                POST /orders/{id}/financial-status; só faltava a interface. */}
            <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16, display: 'flex', flexDirection: 'column', gap: 10 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>
                Pagamento
              </span>
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                {(['pagamento_agendado', 'bloqueio', 'concluido'] as const).map(acao => (
                  <button
                    key={acao}
                    type="button"
                    className={`szv2-btn szv2-btn-sm ${finAcao === acao ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                    onClick={() => {
                      setFinErr('')
                      setFinAcao(finAcao === acao ? null : acao)
                      setFinData(selected.scheduled_payment_date?.slice(0, 10) || '')
                    }}
                  >
                    {FINANCIAL_LABELS[acao]}
                  </button>
                ))}
              </div>
              {finAcao && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: '12px 14px', background: 'var(--szv2-surface-alt)', borderRadius: 8 }}>
                  {finAcao === 'pagamento_agendado' && (
                    <label style={{ display: 'flex', flexDirection: 'column', gap: 6, fontSize: 12, fontWeight: 600 }}>
                      Data do pagamento
                      <input
                        type="date"
                        className="szv2-input"
                        value={finData}
                        onChange={e => { setFinData(e.target.value); setFinErr('') }}
                      />
                    </label>
                  )}
                  {finErr && <span style={{ fontSize: 12, color: 'var(--szv2-danger)' }}>{finErr}</span>}
                  <div style={{ display: 'flex', gap: 8 }}>
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-brand szv2-btn-sm"
                      disabled={finBusy}
                      onClick={() => salvarFinanceiro(selected)}
                    >
                      {finBusy ? 'Salvando…' : 'Salvar'}
                    </button>
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                      disabled={finBusy}
                      onClick={() => { setFinAcao(null); setFinErr('') }}
                    >
                      Cancelar
                    </button>
                  </div>
                </div>
              )}
            </div>

            {/* AUDIT-2026-07-29 (dono): "mostre o custo de todos os fretes cotados
                e o serviço mais barato selecionado, SOMENTE NO ADMIN" — recotação
                ao vivo na ME (mesma regra de bloqueio de Correios + mais barata
                que a emissão real usa), pra auditar se o sistema escolheria certo. */}
            <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>
                  Cotações de frete (admin)
                </span>
                <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" disabled={quotesBusy} onClick={() => loadQuotes(selected)}>
                  {quotesBusy ? 'Cotando…' : quotes ? 'Recotar' : 'Ver cotações'}
                </button>
              </div>
              {quotesErr && <div style={{ marginTop: 8, fontSize: 13, color: '#B91C1C' }}>{quotesErr}</div>}
              {quotesMeta && (
                <div style={{ marginTop: 8, fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  {quotesMeta.from_cep} → {quotesMeta.to_cep}
                  {quotesMeta.bloqueia_correios ? ' · Correios bloqueado no perfil' : ''}
                  {quotesMeta.fonte === 'historico'
                    ? ' · Cotação do momento da emissão (não recotado)'
                    : ' · Cotação ao vivo (pedido ainda sem etiqueta)'}
                </div>
              )}
              {quotes && (
                <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
                  {quotes.map(q => {
                    const destacada = quotesMeta?.fonte === 'historico' ? q.emitida : q.selecionado
                    return (
                    <div
                      key={q.service_id}
                      style={{
                        display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8,
                        padding: '4px 8px', borderRadius: 6,
                        background: destacada ? 'rgba(16,185,129,0.12)' : 'transparent',
                        opacity: q.bloqueado || q.indisponivel ? 0.55 : 1,
                      }}
                    >
                      <span>
                        {destacada ? (quotesMeta?.fonte === 'historico' ? '📦 ' : '✅ ') : ''}
                        <strong>{q.transportadora || '—'}</strong> — {q.nome}
                        {q.bloqueado ? ' (bloqueado no perfil)' : ''}
                        {q.indisponivel ? ' (indisponível)' : ''}
                        {q.emitida && quotesMeta?.fonte === 'historico' ? ' — emitida' : ''}
                      </span>
                      <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <span style={{ fontFamily: 'var(--szv2-font-mono)', fontWeight: destacada ? 700 : 500, whiteSpace: 'nowrap' }}>
                          {brl(q.preco)}
                        </span>
                        {!q.bloqueado && !q.indisponivel && canEmitLabel(selected) && (
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                            disabled={forceCarrierBusy !== null}
                            onClick={() => handleForceCarrier(selected, q.service_id)}
                            title="Emitir etiqueta forçando esta transportadora"
                          >
                            {forceCarrierBusy === q.service_id ? 'Emitindo…' : 'Usar esta'}
                          </button>
                        )}
                      </span>
                    </div>
                  )})}
                  {quotes.length === 0 && <span style={{ color: 'var(--szv2-text-faint)' }}>Nenhuma cotação retornada.</span>}
                </div>
              )}
            </div>

            {/* Ações — exclusivas de admin (Aprovar/Emitir etiqueta/Imprimir/
                Separar/Declaração/Cancelar), agrupadas igual ao portal (flex-wrap
                em vez de botões full-width empilhados). */}
            <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              {canApprove(selected) && (
                <button type="button" className="szv2-btn szv2-btn-brand szv2-btn-sm" disabled={approveBusy} onClick={() => handleApprove(selected)}>
                  {approveBusy ? 'Aprovando…' : 'Aprovar'}
                </button>
              )}
              {/* AUDIT-2026-07-28: achado ao vivo — pedido 1660 (Cleni) ficou
                  'processing' sem NENHUMA etiqueta na ME e sem histórico de status
                  (aprovado por fora do fluxo normal, autoEmitLabel nunca rodou).
                  Botão de emergência: reprocessa a emissão pra pedido aprovado
                  sem etiqueta ainda (idempotente do lado do labels-service). */}
              {canEmitLabel(selected) && (
                <button type="button" className="szv2-btn szv2-btn-brand szv2-btn-sm" disabled={emitLabelBusy} onClick={() => handleEmitLabel(selected)}>
                  {emitLabelBusy ? 'Emitindo…' : '🏷️ Emitir etiqueta'}
                </button>
              )}
              {/* AUDIT-2026-07-28: antes só aparecia com status JÁ 'embalado' — beco
                  sem saída, pois é o PRÓPRIO Imprimir que separa (mark-packed
                  depois do print). Agora aparece a partir de 'processing' (aprovado,
                  etiqueta emitida) OU já separado — cobre o caminho inteiro. */}
              {['processing', 'em_separacao', 'embalado'].includes(normStatus(selected.status)) && selected.has_label && (
                <>
                  <button type="button" className="szv2-btn szv2-btn-brand szv2-btn-sm" disabled={printBusy} onClick={() => printLabelsPDF([selected.id])}>
                    🖨️ Imprimir etiqueta{normStatus(selected.status) === 'processing' ? ' (separa)' : ''}
                  </button>
                  <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" disabled={printBusy} onClick={() => printDeclaration([selected.id])}>
                    📄 Declaração
                  </button>
                  <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" disabled={printBusy} onClick={() => { printLabelsPDF([selected.id]); printDeclaration([selected.id]) }}>
                    🖨️📄 Ambos
                  </button>
                </>
              )}
              {/* AUDIT-2026-07-31 (dono): "coletado" = pacote levado ao ponto de coleta,
                  entre Separado (embalado) e Enviado. Só faz sentido a partir de
                  'embalado' (já separado/etiqueta pronta). */}
              {normStatus(selected.status) === 'embalado' && (
                <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" disabled={collectBusy} onClick={() => handleMarkCollected(selected)}>
                  {collectBusy ? 'Marcando…' : '📍 Marcar coletado'}
                </button>
              )}
              {canCancel(selected) && (
                <>
                  {selected.has_label && (
                    <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" disabled={cancelBusy} onClick={() => handleCancel(selected, true)}>
                      {cancelBusy ? 'Cancelando…' : 'Cancelar etiqueta e voltar para pendente'}
                    </button>
                  )}
                  <button type="button" className="szv2-btn szv2-btn-danger szv2-btn-sm" disabled={cancelBusy} onClick={() => handleCancel(selected)}>
                    {cancelBusy ? 'Cancelando…' : 'Cancelar pedido'}
                  </button>
                </>
              )}
            </div>
          </>
        </DetailDrawer>
      )}
    </div>
  )
}
