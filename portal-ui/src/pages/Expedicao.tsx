// Expedição — porte fiel de templates/portal/v2/sections/expedicao.php para React.
// Ligada ao go/portal:
//   GET  /portal/expedicao          — pedidos de frete visíveis do produtor (escopo por sessão).
//   POST /portal/expedicao/{id}/approve — aprova (débito TPC + compra de etiqueta).
//   POST /portal/expedicao/{id}/cancel  — cancela (estorno TPC via labels-service).
//
// MIGRAÇÃO 2026-07-28: Cancelar deixou de bater em /wp-admin/admin-ajax.php (WP) —
// esse endpoint NUNCA funcionava no FALK (stack 100% Go, sem WordPress; o nonce
// 'senderzz_portal' nunca era mintado pelo SPA, então toda tentativa falhava com
// "-1"). Reprocessar (retry) AINDA usa o caminho antigo — mesmo gap, ainda não migrado.
//
// Filtros (chips de status, busca, produto/afiliado/oferta/data) são 100% client-side,
// espelhando szV2ExFilter/szV2ExChipFilter do WP.
//
// DESVIOS FIÉIS (contrato Go ≠ shape WP, mas UX idêntica):
//   - VALOR = shipping_total_raw (número → brl()); no WP já vinha "R$ ..." de senderzz_portal_money.
//   - COMISSÃO = affiliate_commission; mostra "—" se <= 0 (ternário, nunca bare 0 &&).
//   - RASTREIO = tracking_codes (0..n) → 2 primeiros, join ', ', trim 20 (title = completo).
//   - Afiliado: bloqueado (mesmo guard da V1). Curto-circuito por role + fallback no 403.
//   - Brand accent SEMPRE via var(--szv2-brand); o #EA580C hardcoded da section WP é descartado.
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { api as adminApi, BASE as ADMIN_BASE, getToken as getPortalToken } from '../admin-orders/api'
import { useToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'
import { brl, dt } from '../utils/format'
import EmptyState from '../components/EmptyState'
import StatusBadge, { statusLabel } from '../components/StatusBadge'
import FalkSelect from '../components/FalkSelect'
import FilterButton from '../admin-orders/components/FilterButton'
import FilterTopPanel, { FilterField, filterInputStyle, ActiveFilterChips, type ActiveChip } from '../admin-orders/components/FilterTopPanel'
import ActionDrawer from '../admin-orders/components/ActionDrawer'
import TableSkeleton from '../admin-orders/components/TableSkeleton'
import ErrorState from '../admin-orders/components/ErrorState'
import type { PortalMe } from '../components/Layout'
import { canPrintExpeditionOrder } from './expedicaoPrint'

// ── Shape (espelha go/portal/internal/handlers/expedicao.go::expedicaoRow) ──────
type ExpedicaoRow = {
  id: number
  wc_order_id: number | null
  number: string
  status: string // cru, sem "wc-"
  financial_status?: string
  scheduled_payment_date?: string | null
  cliente_nome: string
  cliente_telefone: string
  cliente_cpf: string
  checkout_link_id: number | null
  product_name: string
  senderzz_offer_name: string
  affiliate_name: string
  produtor_nome?: string
  shipping_name: string // TRANSPORTADORA / carrier
  tracking_codes: string[]
  tracking_url: string
  shipping_total_raw: number // coluna FRETE
  producer_net: number // coluna LÍQUIDO (o.producer_net, já desconta frete/taxas)
  affiliate_commission: number // coluna COMISSÃO
  has_label: boolean
  label_error: string
  delivery_date: string
  date_machine: string // "Y-m-d H:i:s"
  actions: { can_approve: boolean; can_cancel: boolean; can_retry: boolean }
  dest_cep: string
  dest_logradouro: string
  dest_numero: string
  dest_complemento: string
  dest_bairro: string
  dest_cidade: string
  dest_uf: string
}

type ListResp = { ok: boolean; data: ExpedicaoRow[]; total: number; role: string }

type AdminExpedicaoRow = Omit<ExpedicaoRow, 'tracking_url' | 'actions' | 'producer_net' | 'shipping_total_raw' | 'affiliate_commission' | 'tracking_codes'> & {
  tracking_link: string
  tracking_codes?: string[]
  shipping_total_raw: number
  producer_net: number
  affiliate_commission: number
  produtor_nome?: string
}

// ── Badge de status: componente compartilhado StatusBadge (cor única por status,
//    label sem underscore, sem bolinha) — src/components/StatusBadge.tsx. ─────────

// ── Grupos de filtro — fluxo pedido pelo dono (Pendente → Aprovado → Separado →
// Enviado → Entregue/Alerta). "A retirar" (posto ME aguardando retirada do
// cliente) NÃO existe hoje como estado em sz_orders nem é reportado pelas
// etiquetas ME (wc_me_labels não tem tracking-status granular) — falta fonte
// de dado (webhook de rastreio Melhor Envio) antes de poder exibir esse
// estágio; sinalizado, não fabricado.
const FILTER_GROUPS: Record<string, string[]> = {
  todos: [],
  pendente: ['pending', 'aguardando', 'on-hold'],
  em_andamento: ['em_andamento'],
  aprovado: ['processing'],
  separado: ['em_separacao', 'embalado'],
  enviado: ['enviado'],
  a_caminho: ['a_caminho', 'em_transito', 'saiu_para_entrega'],
  entregue: ['entregue', 'completo'],
  pagamento_agendado: [],
  vencido: [],
  concluido: [],
  alerta: ['cancelled', 'em_cancelamento', 'frustrado', 'reembolsado'],
}
const FILTER_LABELS: Record<string, string> = {
  todos: 'Todos',
  pendente: 'Pendente',
  em_andamento: 'Em Andamento',
  aprovado: 'Aprovado',
  separado: 'Separado',
  enviado: 'Enviado',
  a_caminho: 'A Caminho',
  entregue: 'Entregue',
  pagamento_agendado: 'Pagamento Agendado',
  vencido: 'Vencidos',
  concluido: 'Concluídos',
  alerta: 'Alerta',
}
const GROUP_ORDER = ['todos', 'pendente', 'em_andamento', 'aprovado', 'separado', 'enviado', 'a_caminho', 'entregue', 'pagamento_agendado', 'vencido', 'concluido', 'alerta']

// Slug normalizado (sem "wc-") — igual ao strtolower(str_replace('wc-','',...)) do WP.
function normStatus(s: string): string {
  let slug = (s || '').toLowerCase()
  if (slug.startsWith('wc-')) slug = slug.slice(3)
  return slug
}

function displayStatus(s: string): string {
  const key = (s || '').trim().toLowerCase().replace(/^wc-/, '').replace(/[-_\s]+/g, '')
  return key === 'acaminho' ? 'A Caminho' : statusLabel(s)
}

function financialStatusLabel(s?: string | null): string {
  return s ? statusLabel(s) : 'Sem status financeiro'
}

function isDeliveredStatus(s: string): boolean {
  const slug = normStatus(s)
  return slug === 'entregue' || slug === 'completo' || slug === 'completed'
}

// O estado financeiro vale assim que existe, entregue ou não: quem marcou
// "pago" quer ver "pago", sem depender do status de expedição.
function effectiveStatus(o: ExpedicaoRow): string {
  return o.financial_status ? o.financial_status : o.status
}

function effectiveStatusLabel(o: ExpedicaoRow): string {
  return o.financial_status ? financialStatusLabel(o.financial_status) : displayStatus(o.status)
}

// Grupo de filtro de uma linha (primeiro grupo que casa, com break) — $sz5ex_filter_key.
function filterGroupOf(slug: string): string {
  for (const gk of GROUP_ORDER) {
    if (gk !== 'todos' && FILTER_GROUPS[gk].includes(slug)) return gk
  }
  return 'outros'
}

// produto exibido = product_name (offer_name || 1º item) — já resolvido no backend.
function rowProduct(o: ExpedicaoRow): string {
  return o.product_name || ''
}

// Kit com 2+ produtos vem como "3 Egipzya Sérum + 1 Egipzya Espuma" (join do
// backend). Em vez de truncar com "…" escondendo o 2º item, empilha cada
// produto em sua própria linha — nada de "+N" resumido.
function productLines(product: string): string[] {
  if (!product) return []
  return product.split(' + ').map(s => s.trim()).filter(Boolean)
}

// Rastreio = 2 primeiros códigos, join ', ' (igual ao implode(', ', array_slice(.., 0, 2))).
function rowTracking(o: ExpedicaoRow): string {
  return (o.tracking_codes || []).slice(0, 2).join(', ')
}

// mb_strimwidth seguro — corta em `len` chars adicionando reticências.
function trim(s: string, len: number): string {
  if (!s) return ''
  return s.length > len ? s.slice(0, len - 1) + '…' : s
}

// CPF/CNPJ do cliente com máscara (só dígitos vêm do backend).
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

// Formata telefone BR: (11) 96348-6603 (celular, 9 dígitos) ou (11) 3634-8660 (fixo).
function formatPhone(phone: string): string {
  const d = (phone || '').replace(/\D/g, '')
  if (d.length === 11) return d.replace(/(\d{2})(\d{5})(\d{4})/, '($1) $2-$3')
  if (d.length === 10) return d.replace(/(\d{2})(\d{4})(\d{4})/, '($1) $2-$3')
  return phone
}

function brDateIso(iso: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso || '')
  return m ? `${m[3]}/${m[2]}/${m[1]}` : '—'
}

export default function Expedicao() {
  const toast = useToast()
  const [rows, setRows] = useState<ExpedicaoRow[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [forbidden, setForbidden] = useState(false) // afiliado / 403
  const [isOperator, setIsOperator] = useState(false)
  const [actionBusy, setActionBusy] = useState(false) // trava duplo-clique em Aprovar/Cancelar/Reprocessar
  const [printBusy, setPrintBusy] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [financialAction, setFinancialAction] = useState<'pagamento_agendado' | 'vencido' | 'concluido' | null>(null)
  const [financialDate, setFinancialDate] = useState('')
  const [financialBusy, setFinancialBusy] = useState(false)
  const [financialErr, setFinancialErr] = useState('')

  // Filtros (todos client-side — espelham szV2ExFilter).
  const [chip, setChip] = useState('todos')
  const [search, setSearch] = useState('')
  const [fProd, setFProd] = useState('')
  const [fAff, setFAff] = useState('')
  const [fOffer, setFOffer] = useState('')
  const [fDate, setFDate] = useState('') // '' | hoje | semana | mes

  // Painel de filtros lateral (padrão Cash on Delivery) — os campos que já
  // vivem inline (busca + selects) continuam visíveis; o drawer agrupa todos
  // pra manter o cabeçalho enxuto em telas menores.
  const [filterOpen, setFilterOpen] = useState(false)

  function load() {
    let cancelled = false
    setLoading(true)
    setErr('')
    // Curto-circuito por role: afiliado nunca vê Expedição (mesmo guard da V1).
    api<PortalMe>('/portal/me')
      .then(me => {
        if (cancelled) return
        const r = (me?.role || 'client').toLowerCase()
        const operator = r === 'operator' || r === 'operador'
        setIsOperator(operator)
        // Role do banco pode vir em PT-BR (afiliado/afiliada) — espelha Layout.tsx.
        // O 403 do Go (msg "não está disponível no seu perfil") continua como fallback.
        if (r === 'affiliate' || r === 'afiliado' || r === 'afiliada') {
          setForbidden(true)
          setLoading(false)
          return
        }
        if (r === 'operator' || r === 'operador') {
          return adminApi<{ items: AdminExpedicaoRow[] }>('/operator/expedicao/orders').then(resp => {
            if (cancelled) return
            setRows((resp.items || []).map(o => ({
              ...o,
              tracking_url: o.tracking_link || '',
              tracking_codes: o.tracking_codes || [],
              actions: {
                can_approve: ['pending', 'aguardando', 'on-hold'].includes(normStatus(o.status)),
                can_cancel: ['pending', 'aguardando', 'on-hold', 'em_andamento', 'processing', 'em_separacao', 'embalado'].includes(normStatus(o.status)),
                can_retry: false,
              },
            })))
            setLoading(false)
          })
        }
        return api<ListResp>('/portal/expedicao').then(resp => {
          if (cancelled) return
          setRows(resp.data || [])
          setLoading(false)
        })
      })
      .catch(e => {
        if (cancelled) return
        const msg = e?.message || ''
        // Fallback do 403: api.ts perde o status HTTP, casamos pela mensagem.
        if (/não está disponível no seu perfil|não disponível/i.test(msg)) {
          setForbidden(true)
        } else {
          setErr(msg || 'Erro ao carregar a Expedição')
        }
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }

  useEffect(() => {
    const cancel = load()
    return cancel
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ── Opções derivadas do conjunto (no WP eram coletadas em PHP) ────────────────
  const products = useMemo(() => {
    const set = new Set<string>()
    rows.forEach(o => {
      const p = rowProduct(o)
      if (p) set.add(p)
    })
    return Array.from(set).sort((a, b) => a.localeCompare(b, 'pt-BR'))
  }, [rows])

  const affiliates = useMemo(() => {
    const set = new Set<string>()
    rows.forEach(o => {
      if (o.affiliate_name) set.add(o.affiliate_name)
    })
    return Array.from(set).sort((a, b) => a.localeCompare(b, 'pt-BR'))
  }, [rows])

  const offers = useMemo(() => {
    const set = new Set<string>()
    rows.forEach(o => {
      if (o.senderzz_offer_name) set.add(o.senderzz_offer_name)
    })
    return Array.from(set).sort((a, b) => a.localeCompare(b, 'pt-BR'))
  }, [rows])

  // ── Filtro client-side (espelha szV2ExFilter linha-a-linha) ───────────────────
  const visible = useMemo(() => {
    const q = search.trim().toLowerCase()

    const today = new Date()
    today.setHours(0, 0, 0, 0)
    const weekStart = new Date(today)
    weekStart.setDate(today.getDate() - today.getDay())
    const monthStart = new Date(today.getFullYear(), today.getMonth(), 1)

    return rows.filter(o => {
      const slug = normStatus(o.status)
      const fg = filterGroupOf(slug)
      const prod = rowProduct(o)
      const aff = o.affiliate_name || ''
      const offer = o.senderzz_offer_name || ''
      const dateIso = (o.delivery_date || o.date_machine || '').slice(0, 10)

      // Chip de status
      const financialChip = chip === 'pagamento_agendado' || chip === 'vencido' || chip === 'concluido'
      if (financialChip) {
        if (o.financial_status !== chip) return false
      } else if (chip && chip !== 'todos' && fg !== chip) {
        return false
      }
      // Produto / afiliado / oferta
      if (fProd && prod !== fProd) return false
      if (fAff && aff !== fAff) return false
      if (fOffer && offer !== fOffer) return false
      // Data
      if (fDate) {
        if (!dateIso) return false
        const d = new Date(dateIso + 'T00:00:00')
        if (fDate === 'hoje' && d.getTime() !== today.getTime()) return false
        if (fDate === 'semana' && d < weekStart) return false
        if (fDate === 'mes' && d < monthStart) return false
      }
      // Busca texto (número, cliente, produto, rastreio, afiliado)
      if (q) {
        const hay = `${o.number} ${o.cliente_nome} ${prod} ${rowTracking(o)} ${aff}`.toLowerCase()
        if (hay.indexOf(q) === -1) return false
      }
      return true
    })
  }, [rows, chip, search, fProd, fAff, fOffer, fDate])

  const footCount = `Mostrando ${visible.length} pedido${visible.length !== 1 ? 's' : ''}`

  // Chips de filtros ativos (padrão Cash on Delivery) — cada um some ao clicar no X.
  const activeChips: ActiveChip[] = []
  if (chip !== 'todos') activeChips.push({ key: 'chip', label: `Status: ${FILTER_LABELS[chip] || chip}`, onRemove: () => setChip('todos') })
  if (fProd) activeChips.push({ key: 'prod', label: `Produto: ${fProd}`, onRemove: () => setFProd('') })
  if (fAff) activeChips.push({ key: 'aff', label: `Afiliado: ${fAff}`, onRemove: () => setFAff('') })
  if (fOffer) activeChips.push({ key: 'offer', label: `Oferta: ${fOffer}`, onRemove: () => setFOffer('') })
  if (fDate) activeChips.push({ key: 'date', label: `Data: ${fDate === 'hoje' ? 'Hoje' : fDate === 'semana' ? 'Esta semana' : 'Este mês'}`, onRemove: () => setFDate('') })
  if (search) activeChips.push({ key: 'search', label: `Busca: ${search}`, onRemove: () => setSearch('') })
  const activeFilterCount = activeChips.length
  function clearFilters() {
    setChip('todos'); setFProd(''); setFAff(''); setFOffer(''); setFDate(''); setSearch('')
  }

  // ── Ações de mutação (nonce gap: batem no WP admin-ajax durante a migração) ───
  async function copyTracking(tracking: string) {
    try {
      await navigator.clipboard?.writeText(tracking)
      toast('ok', 'Rastreio copiado!')
    } catch {
      toast('err', 'Não foi possível copiar o rastreio.')
    }
  }

  async function copyPhone(phone: string) {
    try {
      await navigator.clipboard?.writeText(phone.replace(/\D/g, ''))
      toast('ok', 'Telefone copiado!')
    } catch {
      toast('err', 'Não foi possível copiar o telefone.')
    }
  }

  function togglePrintSelection(id: number) {
    setSelectedIds(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  async function downloadPrintBatch(
    ids: number[],
    wantLabels: boolean,
    wantDeclaration: boolean,
    fallbackName: string,
  ) {
    const res = await fetch(`${ADMIN_BASE}/operator/expedicao/print-batch`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${getPortalToken() ?? ''}`,
      },
      body: JSON.stringify({
        order_ids: ids,
        want_labels: wantLabels,
        want_declaration: wantDeclaration,
      }),
    })
    if (!res.ok) {
      const text = await res.text().catch(() => '')
      let message = text
      try {
        const parsed = JSON.parse(text)
        message = parsed?.error?.message || parsed?.erro || text
      } catch {
        // O labels-service também pode devolver erro em texto puro.
      }
      throw new Error(message || `HTTP ${res.status}`)
    }

    const blob = await res.blob()
    const disposition = res.headers.get('Content-Disposition') || ''
    const filename = /filename="([^"]+)"/.exec(disposition)?.[1] || fallbackName
    const blobURL = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = blobURL
    anchor.download = filename
    document.body.appendChild(anchor)
    anchor.click()
    anchor.remove()
    setTimeout(() => URL.revokeObjectURL(blobURL), 30_000)
  }

  async function printExpedition(
    ids: number[],
    wantLabels: boolean,
    wantDeclaration: boolean,
  ) {
    if (!isOperator || printBusy || ids.length === 0) return
    setPrintBusy(true)
    try {
      // O labels-service devolve uma categoria por resposta. "Ambos" mantém a
      // mesma semântica do admin: dois downloads, um PDF de etiquetas e outro de
      // declarações, sem misturar tipos de documento no mesmo arquivo.
      const downloads: Promise<void>[] = []
      if (wantLabels) {
        downloads.push(downloadPrintBatch(
          ids,
          true,
          false,
          ids.length > 1 ? 'etiquetas.pdf' : `etiqueta-${ids[0]}.pdf`,
        ))
      }
      if (wantDeclaration) {
        downloads.push(downloadPrintBatch(
          ids,
          false,
          true,
          ids.length > 1 ? 'declaracoes.pdf' : `declaracao-${ids[0]}.pdf`,
        ))
      }
      await Promise.all(downloads)

      // Imprimir etiqueta significa que o pacote entrou em separação. A rota é
      // idempotente e já possui gate admin/operador no backend.
      if (wantLabels) {
        await Promise.all(ids.map(id => adminApi(`/orders/${id}/mark-packed`, { method: 'POST' })))
        setSelectedIds(new Set())
        setSelectedOrder(null)
        load()
      }
      toast('ok', ids.length > 1 ? 'Arquivos de impressão baixados.' : 'Arquivo de impressão baixado.')
    } catch (e: any) {
      toast('err', e?.message || 'Não foi possível baixar os arquivos de impressão.')
    } finally {
      setPrintBusy(false)
    }
  }

  async function runAction(o: ExpedicaoRow, action: 'approve' | 'cancel' | 'retry') {
    if (actionBusy) return // duplo-clique — 1ª chamada já em voo
    if (action === 'cancel') {
      const ok = await confirmAsync({
        title: 'Cancelar pedido',
        message: o.has_label
          ? `Cancelar o pedido ${o.number}? A etiqueta já foi comprada e o cancelamento será processado.`
          : `Tem certeza que deseja cancelar o pedido ${o.number}?`,
        confirmLabel: 'Cancelar pedido',
        cancelLabel: 'Voltar',
        danger: true,
      })
      if (!ok) return
    }
    // Aprovar/Cancelar = rotas nativas Go (débito/estorno TPC do lado do servidor).
    // Reprocessar ainda depende do hook WC antigo (admin-ajax.php) — nonce
    // 'senderzz_portal' nunca mintado pelo SPA (gap reconhecido, não migrado ainda).
    if (action === 'approve' || action === 'cancel') {
      setActionBusy(true)
      try {
        const request = isOperator ? adminApi : api
        const r = await request<{ ok: boolean; label_warning?: string; estorno_mensagem?: string }>(
          (isOperator
            ? `/orders/${o.id}/${action === 'cancel' ? 'cancel-expedicao' : 'approve'}`
            : `/portal/expedicao/${o.id}/${action}`),
          { method: 'POST' },
        )
        const okMsg = action === 'approve'
          ? `Pedido ${o.number || o.id} aprovado — foi para "Aprovado".`
          : `Pedido ${o.number || o.id} cancelado.`
        const extra = r.label_warning || r.estorno_mensagem
        toast('ok', extra ? `${okMsg} ${extra}` : okMsg)
        setSelectedOrder(null)
        load()
      } catch (e: any) {
        const msg = action === 'approve' ? 'Não foi possível aprovar o pedido.' : 'Não foi possível cancelar o pedido.'
        toast('err', e?.message || msg)
      } finally {
        setActionBusy(false)
      }
      return
    }
    setActionBusy(true)
    try {
      const body = new URLSearchParams()
      body.set('action', 'senderzz_portal')
      body.set('szaction', action)
      // NOTE p/ o dono da rota wallet-aware de mutação: aqui enviamos sz_orders.id.
      // O handler senderzz_portal histórico chaveava pelo WC order id (o.wc_order_id) —
      // confirmar a semântica ao mintar o nonce. Hoje a chamada é barrada pelo nonce gap.
      body.set('order_id', String(o.id))
      const res = await fetch('/wp-admin/admin-ajax.php', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        credentials: 'same-origin',
        body: body.toString(),
      })
      const j = await res.json().catch(() => ({}))
      // Sucesso só se wp_send_json_success (success === true). check_ajax_referer falhando
      // devolve HTTP 200 com body "-1" → NÃO tratar como sucesso (evita toast falso-positivo).
      // É exatamente esse bloqueio de nonce que garante zero mutação financeira acidental.
      if (!res.ok || !j || j.success !== true) {
        throw new Error((j && (j.data?.message || j.data)) || 'Falha ao processar a ação.')
      }
      // Só 'retry' chega aqui — approve/cancel retornam cedo acima (rotas nativas).
      toast('ok', 'Etiqueta reprocessada.')
    } catch (e: any) {
      toast('err', e?.message || 'Não foi possível concluir a ação.')
    } finally {
      setActionBusy(false)
    }
  }

  // Drawer de detalhe (clique na linha) — mesma UX do Cash on Delivery.
  const [selectedOrder, setSelectedOrder] = useState<ExpedicaoRow | null>(null)

  function resetFinancialAction() {
    setFinancialAction(null)
    setFinancialDate('')
    setFinancialErr('')
  }

  async function saveFinancialStatus(o: ExpedicaoRow) {
    if (!financialAction) return
    if (financialAction === 'pagamento_agendado' && !financialDate) {
      setFinancialErr('Informe a data agendada de pagamento.')
      return
    }
    setFinancialBusy(true)
    setFinancialErr('')
    try {
      await adminApi(`/orders/${o.id}/financial-status`, {
        method: 'POST',
        body: JSON.stringify({
          financial_status: financialAction,
          scheduled_payment_date: financialDate || undefined,
        }),
      })
      toast('ok', `Pedido ${o.number || o.id} → ${financialStatusLabel(financialAction)}.`)
      resetFinancialAction()
      setSelectedOrder(null)
      load()
    } catch (e: any) {
      setFinancialErr(e?.message || 'Falha ao alterar status financeiro.')
    } finally {
      setFinancialBusy(false)
    }
  }

  // ── Estados de saída ──────────────────────────────────────────────────────────
  // Afiliado / 403 — mesmo guard da V1.
  if (forbidden) {
    return (
      <section id="sec-expedicao" className="sz-sec">
        <EmptyState
          title="Expedição não disponível"
          description="Expedição não está disponível no seu perfil."
        />
      </section>
    )
  }

  return (
    <section id="sec-expedicao" className="sz-sec">
      {!!err && rows.length > 0 && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}

      {/* EmptyState SÓ quando o backend devolve conjunto vazio (não em filtro 0), e
          só depois do carregamento inicial resolver sem erro (padrão Cash on Delivery). */}
      {!loading && !err && rows.length === 0 ? (
        <EmptyState
          title="Nenhum pedido de Expedição"
          description="Os pedidos com frete aparecem aqui assim que chegarem."
        />
      ) : (
        <>
          {/* Cabeçalho: contagem + botão de filtros (padrão Cash on Delivery) */}
          <div className="szv2-section-head" style={{ flexWrap: 'wrap', gap: 8, marginBottom: 12 }}>
            <div>
              <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', margin: 0 }}>
                {loading ? 'Carregando…' : `${visible.length} pedido(s) encontrado(s)`}
                {activeFilterCount > 0 ? ` · ${activeFilterCount} filtro(s) ativo(s)` : ''}
              </p>
            </div>
            <FilterButton active={activeFilterCount > 0} count={activeFilterCount} onClick={() => setFilterOpen(true)} />
          </div>

          {/* Chips de filtros ativos */}
          <ActiveFilterChips chips={activeChips} onClearAll={clearFilters} />

          {/* Painel lateral de filtros (idêntico ao Cash on Delivery: nenhum filtro
              fica solto no corpo da tela, tudo vive no FilterButton/drawer). */}
          <FilterTopPanel
            open={filterOpen}
            onClose={() => setFilterOpen(false)}
            onApply={() => setFilterOpen(false)}
            onClear={clearFilters}
            title="Filtros de Expedição"
          >
            <FilterField label="Status">
              <FalkSelect
                aria-label="Filtrar por status"
                value={chip}
                onChange={v => setChip(v)}
                options={GROUP_ORDER.map(gk => ({ value: gk, label: FILTER_LABELS[gk] }))}
              />
            </FilterField>
            {products.length > 0 && (
              <FilterField label="Produto">
                <FalkSelect
                  aria-label="Filtrar por produto"
                  value={fProd}
                  onChange={v => setFProd(v)}
                  placeholder="Todos os produtos"
                  options={[
                    { value: '', label: 'Todos os produtos' },
                    ...products.map(v => ({ value: v, label: v })),
                  ]}
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
                  options={[
                    { value: '', label: 'Todos os afiliados' },
                    ...affiliates.map(v => ({ value: v, label: v })),
                  ]}
                />
              </FilterField>
            )}
            {offers.length > 0 && (
              <FilterField label="Oferta">
                <FalkSelect
                  aria-label="Filtrar por oferta"
                  value={fOffer}
                  onChange={v => setFOffer(v)}
                  placeholder="Todas as ofertas"
                  options={[
                    { value: '', label: 'Todas as ofertas' },
                    ...offers.map(v => ({ value: v, label: v })),
                  ]}
                />
              </FilterField>
            )}
            <FilterField label="Data">
              <FalkSelect
                aria-label="Filtrar por data"
                value={fDate}
                onChange={v => setFDate(v)}
                placeholder="Qualquer data"
                options={[
                  { value: '', label: 'Qualquer data' },
                  { value: 'hoje', label: 'Hoje' },
                  { value: 'semana', label: 'Esta semana' },
                  { value: 'mes', label: 'Este mês' },
                ]}
              />
            </FilterField>
            <FilterField label="Busca">
              <input
                type="search"
                style={filterInputStyle}
                placeholder="Pedido / cliente / rastreio"
                value={search}
                onChange={e => setSearch(e.target.value)}
              />
            </FilterField>
          </FilterTopPanel>

          {/* Tabela */}
          {loading ? (
            <TableSkeleton rows={6} cols={isOperator ? 9 : 8} />
          ) : err ? (
            <ErrorState message={err} onRetry={load} />
          ) : (
          <div className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
            <div className="szv2-table-wrap">
              <table className="szv2-table" style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    {isOperator && <th aria-label="Selecionar para impressão" />}
                    <th style={{ whiteSpace: 'nowrap' }}>PEDIDO</th>
                    <th>CLIENTE</th>
                    <th>STATUS</th>
                    <th>PRODUTO</th>
                    <th>TRANSPORTADORA</th>
                    <th>RASTREIO</th>
                    <th style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>VALOR</th>
                    <th style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>FRETE</th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map(o => {
                    const num = o.number || String(o.id)
                    const client = o.cliente_nome || '—'
                    const clienteTel = stripCountryCode(o.cliente_telefone || '')
                    const product = rowProduct(o) || '—'
                    const carrier = o.shipping_name || '—'
                    const tracking = rowTracking(o)
                    const dateFmt = o.delivery_date
                      ? o.delivery_date.slice(0, 10).split('-').reverse().join('/')
                      : (o.date_machine ? dt(o.date_machine) : '—')
                    return (
                      <tr
                        key={o.id}
                        data-status={normStatus(o.status)}
                        onClick={() => { resetFinancialAction(); setSelectedOrder(o) }}
                        style={{ cursor: 'pointer' }}
                        title="Clique para ver detalhes"
                      >
                        {isOperator && (
                          <td onClick={e => e.stopPropagation()}>
                            {canPrintExpeditionOrder(isOperator, o.status, o.has_label) && (
                              <input
                                type="checkbox"
                                aria-label={`Selecionar pedido ${num} para impressão`}
                                checked={selectedIds.has(o.id)}
                                onChange={() => togglePrintSelection(o.id)}
                              />
                            )}
                          </td>
                        )}
                        <td style={{ whiteSpace: 'nowrap' }}>
                          <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                            <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--szv2-text)' }}>{num}</span>
                            <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{dateFmt}</span>
                          </div>
                        </td>
                        <td>
                          <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                            <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--szv2-text)' }}>{client}</span>
                            {!!clienteTel && (
                              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 11, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)' }}>
                                {formatPhone(clienteTel)}
                                <button
                                  type="button"
                                  className="szv2-link-btn"
                                  onClick={e => { e.stopPropagation(); copyPhone(clienteTel) }}
                                  title="Copiar telefone"
                                  aria-label="Copiar telefone"
                                  style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--szv2-text-muted)', padding: 0 }}
                                >
                                  ⧉
                                </button>
                              </span>
                            )}
                          </div>
                          {!!o.affiliate_name && (
                            <div style={{ fontSize: 11, marginTop: 2 }}>
                              <span
                                style={{
                                  padding: '1px 6px',
                                  background: 'var(--szv2-brand-light)',
                                  color: 'var(--szv2-brand)',
                                  borderRadius: 99,
                                  fontWeight: 600,
                                }}
                              >
                                {o.affiliate_name}
                              </span>
                            </div>
                          )}
                        </td>
                        <td style={{ padding: '12px 14px' }}>
                          <div style={{ display: 'flex', flexDirection: 'column', gap: 4, alignItems: 'flex-start' }}>
                            <StatusBadge status={effectiveStatus(o)} label={effectiveStatusLabel(o)} />
                            {o.financial_status === 'pagamento_agendado' && o.scheduled_payment_date && (
                              <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                                {brDateIso(o.scheduled_payment_date)}
                              </span>
                            )}
                          </div>
                        </td>
                        <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)', maxWidth: 220 }}>
                          {productLines(product).length > 1 ? (
                            <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                              {productLines(product).map((line, i) => (
                                <div key={i} style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={line}>
                                  {trim(line, 32)}
                                </div>
                              ))}
                            </div>
                          ) : (
                            <div
                              style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                              title={product}
                            >
                              {trim(product, 32)}
                            </div>
                          )}
                        </td>
                        <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)' }}>{carrier}</td>
                        <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)', whiteSpace: 'nowrap' }}>
                          {tracking !== '' ? (
                            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                              {o.tracking_url ? (
                                <a
                                  href={o.tracking_url}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="szv2-link-btn"
                                  onClick={e => e.stopPropagation()}
                                  title={`Abrir rastreio ${tracking}`}
                                  aria-label={`Abrir rastreio ${tracking}`}
                                  style={{ fontFamily: 'var(--szv2-font-mono)' }}
                                >
                                  {trim(tracking, 20)}
                                </a>
                              ) : (
                                <span title={tracking}>{trim(tracking, 20)}</span>
                              )}
                              {/* O botão de abrir e o de copiar são ações independentes:
                                  quem opera pode conferir o pedido ou compartilhar o link
                                  público de rastreio sem precisar abrir uma nova aba. */}
                              <button
                                type="button"
                                className="szv2-link-btn"
                                onClick={e => {
                                  e.stopPropagation()
                                  if (o.tracking_url) window.open(o.tracking_url, '_blank', 'noopener')
                                  else copyTracking(tracking)
                                }}
                                title="Abrir rastreio"
                                aria-label="Abrir página de rastreio"
                                style={{
                                  background: 'none',
                                  border: 'none',
                                  cursor: 'pointer',
                                  color: 'var(--szv2-text-muted)',
                                  padding: 0,
                                }}
                              >
                                ↗
                              </button>
                              <button
                                type="button"
                                className="szv2-link-btn"
                                onClick={e => {
                                  e.stopPropagation()
                                  copyTracking(o.tracking_url || tracking)
                                }}
                                title="Copiar link de rastreio"
                                aria-label="Copiar link de rastreio"
                                style={{
                                  background: 'none',
                                  border: 'none',
                                  cursor: 'pointer',
                                  color: 'var(--szv2-text-muted)',
                                  padding: 0,
                                }}
                              >
                                ⧉
                              </button>
                            </span>
                          ) : (
                            <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                          )}
                        </td>
                        <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                          <span style={{ fontWeight: 700, fontSize: 13, color: 'var(--szv2-text)', fontFamily: 'var(--szv2-font-mono)' }}>
                            {brl(o.producer_net)}
                          </span>
                        </td>
                        <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                          <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--szv2-text)', fontFamily: 'var(--szv2-font-mono)' }}>
                            {brl(o.shipping_total_raw)}
                          </span>
                        </td>
                      </tr>
                    )
                  })}
                  {visible.length === 0 && (
                    <tr>
                      <td
                        colSpan={isOperator ? 9 : 8}
                        style={{
                          textAlign: 'center',
                          padding: '40px 16px',
                          color: 'var(--szv2-text-muted)',
                          fontSize: 13,
                        }}
                      >
                        Nenhum pedido para esse filtro. Ajuste a busca ou limpe os filtros.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
            <div
              style={{
                padding: '10px 16px',
                borderTop: '1px solid var(--szv2-border)',
                fontSize: 13,
                color: 'var(--szv2-text-muted)',
              }}
            >
              <span>{footCount}</span>
            </div>
          </div>
          )}
        </>
      )}

      {/* Ações em lote são exclusivas do operador logístico. Produtores nunca
          recebem o endpoint global nem os controles de seleção. */}
      {isOperator && selectedIds.size > 0 && (
        <div
          style={{
            position: 'fixed',
            bottom: 24,
            left: '50%',
            transform: 'translateX(-50%)',
            background: 'var(--szv2-card-bg, #fff)',
            border: '1.5px solid var(--szv2-border)',
            borderRadius: 12,
            boxShadow: '0 8px 32px rgba(0,0,0,.18)',
            padding: '10px 14px',
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            zIndex: 1000,
            flexWrap: 'wrap',
          }}
        >
          <span style={{ fontWeight: 700, color: 'var(--szv2-brand)', whiteSpace: 'nowrap' }}>
            {selectedIds.size} selecionado{selectedIds.size !== 1 ? 's' : ''}
          </span>
          <button
            type="button"
            className="szv2-btn szv2-btn-brand"
            disabled={printBusy}
            onClick={() => printExpedition(Array.from(selectedIds), true, false)}
          >
            🖨️ Imprimir etiquetas
          </button>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={printBusy}
            onClick={() => printExpedition(Array.from(selectedIds), false, true)}
          >
            📄 Imprimir declarações
          </button>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={printBusy}
            onClick={() => printExpedition(Array.from(selectedIds), true, true)}
          >
            🖨️📄 Imprimir ambos
          </button>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={printBusy}
            onClick={() => setSelectedIds(new Set())}
          >
            Limpar
          </button>
        </div>
      )}

      {/* Drawer de detalhe (mesmo componente/UX do Cash on Delivery) */}
      <ActionDrawer
        open={selectedOrder !== null}
        onClose={() => setSelectedOrder(null)}
        onApply={() => setSelectedOrder(null)}
        applyLabel="Fechar"
        title={selectedOrder ? `${selectedOrder.number || selectedOrder.id} — ${effectiveStatusLabel(selectedOrder)}` : 'Detalhes'}
      >
        {selectedOrder && (
          <>
            <div style={{ paddingTop: 4 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>
                Endereço de destino
              </span>
              <div style={{ marginTop: 8, fontSize: 13, lineHeight: 1.6 }}>
                {selectedOrder.dest_logradouro ? (
                  <>
                    <div>
                      {selectedOrder.dest_logradouro}
                      {selectedOrder.dest_numero ? `, ${selectedOrder.dest_numero}` : ''}
                    </div>
                    {selectedOrder.dest_complemento && <div>{selectedOrder.dest_complemento}</div>}
                    {selectedOrder.dest_bairro && <div>{selectedOrder.dest_bairro}</div>}
                    <div>
                      {selectedOrder.dest_cidade || '—'}
                      {selectedOrder.dest_uf ? `/${selectedOrder.dest_uf}` : ''}
                    </div>
                    {selectedOrder.dest_cep && (
                      <div style={{ color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                        CEP {selectedOrder.dest_cep}
                      </div>
                    )}
                  </>
                ) : (
                  <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                )}
              </div>
            </div>

            <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16 }}>
              <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>
                Envio
              </span>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Cliente</span>
                  <span style={{ fontWeight: 600 }}>{selectedOrder.cliente_nome || '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>CPF</span>
                  <span style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)' }}>
                    {formatCPF(selectedOrder.cliente_cpf) || '—'}
                  </span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Produto</span>
                  <span style={{ fontWeight: 600 }}>{rowProduct(selectedOrder) || '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>ID do checkout</span>
                  <span style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)' }}>{selectedOrder.checkout_link_id ?? '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Transportadora</span>
                  <span style={{ fontWeight: 600 }}>{selectedOrder.shipping_name || '—'}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Rastreio</span>
                  {rowTracking(selectedOrder) && selectedOrder.tracking_url ? (
                    <a
                      href={selectedOrder.tracking_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="szv2-link-btn"
                      title={`Abrir rastreio ${rowTracking(selectedOrder)}`}
                      aria-label={`Abrir rastreio ${rowTracking(selectedOrder)}`}
                      style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)' }}
                    >
                      {rowTracking(selectedOrder)}
                    </a>
                  ) : (
                    <span style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)' }}>
                      {rowTracking(selectedOrder) || '—'}
                    </span>
                  )}
                </div>
                {!!selectedOrder.label_error && (
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                    <span style={{ color: '#B91C1C' }}>⚠️ Falha na última emissão</span>
                    <span style={{ fontWeight: 600, color: '#B91C1C', textAlign: 'right' }}>{selectedOrder.label_error}</span>
                  </div>
                )}
                {!!selectedOrder.affiliate_name && (
                  <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                    <span style={{ color: 'var(--szv2-text-muted)' }}>Afiliado</span>
                    <span style={{ fontWeight: 600 }}>{selectedOrder.affiliate_name}</span>
                  </div>
                )}
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Data</span>
                  <span style={{ fontWeight: 600 }}>{selectedOrder.date_machine ? dt(selectedOrder.date_machine) : '—'}</span>
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
                  <span style={{ fontWeight: 700, fontFamily: 'var(--szv2-font-mono)' }}>{brl(selectedOrder.producer_net)}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ color: 'var(--szv2-text-muted)' }}>Frete</span>
                  <span style={{ fontWeight: 600, fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-text)' }}>{brl(selectedOrder.shipping_total_raw)}</span>
                </div>
              </div>
            </div>

            {/* Antes só aparecia em pedido entregue, e o produtor não achava o
                controle em mais lugar nenhum. O estado financeiro é decisão de
                quem opera, não consequência do status de expedição. */}
            {(
              <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16, display: 'flex', flexDirection: 'column', gap: 10 }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
                  <span style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--szv2-text-muted)', letterSpacing: '0.05em' }}>
                    Financeiro pós-entrega
                  </span>
                  {selectedOrder.financial_status ? (
                    <StatusBadge status={selectedOrder.financial_status} label={financialStatusLabel(selectedOrder.financial_status)} />
                  ) : (
                    <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Sem status financeiro</span>
                  )}
                </div>
                {selectedOrder.scheduled_payment_date && (
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, fontSize: 13 }}>
                    <span style={{ color: 'var(--szv2-text-muted)' }}>Data agendada</span>
                    <span style={{ fontFamily: 'var(--szv2-font-mono)', fontWeight: 600 }}>{brDateIso(selectedOrder.scheduled_payment_date)}</span>
                  </div>
                )}
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  <button
                    type="button"
                    className={`szv2-btn szv2-btn-sm ${financialAction === 'pagamento_agendado' ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                    onClick={() => {
                      setFinancialErr('')
                      setFinancialAction(financialAction === 'pagamento_agendado' ? null : 'pagamento_agendado')
                      setFinancialDate(selectedOrder.scheduled_payment_date || '')
                    }}
                  >
                    Pagamento Agendado
                  </button>
                  <button
                    type="button"
                    className={`szv2-btn szv2-btn-sm ${financialAction === 'vencido' ? 'szv2-btn-danger' : 'szv2-btn-secondary'}`}
                    onClick={() => { setFinancialErr(''); setFinancialAction(financialAction === 'vencido' ? null : 'vencido') }}
                  >
                    Vencido
                  </button>
                  <button
                    type="button"
                    className={`szv2-btn szv2-btn-sm ${financialAction === 'concluido' ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                    onClick={() => { setFinancialErr(''); setFinancialAction(financialAction === 'concluido' ? null : 'concluido') }}
                  >
                    Concluído
                  </button>
                </div>
                {financialAction && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: '12px 14px', background: 'var(--szv2-surface-alt)', borderRadius: 8 }}>
                    {financialAction === 'pagamento_agendado' && (
                      <label style={{ display: 'flex', flexDirection: 'column', gap: 6, fontSize: 12, fontWeight: 600 }}>
                        Data agendada de pagamento
                        <input
                          type="date"
                          className="szv2-input"
                          value={financialDate}
                          onChange={e => { setFinancialDate(e.target.value); setFinancialErr('') }}
                        />
                      </label>
                    )}
                    {financialErr && (
                      <div style={{ fontSize: 12, color: 'var(--szv2-danger)', background: 'var(--szv2-danger-bg,#FEF2F2)', borderRadius: 6, padding: '6px 10px' }}>
                        {financialErr}
                      </div>
                    )}
                    <div style={{ display: 'flex', gap: 8 }}>
                      <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={resetFinancialAction} disabled={financialBusy}>Cancelar</button>
                      <button
                        type="button"
                        className={`szv2-btn szv2-btn-sm ${financialAction === 'vencido' ? 'szv2-btn-danger' : 'szv2-btn-brand'}`}
                        style={{ flex: 1, justifyContent: 'center' }}
                        disabled={financialBusy || (financialAction === 'pagamento_agendado' && !financialDate)}
                        onClick={() => saveFinancialStatus(selectedOrder)}
                      >
                        {financialBusy ? 'Salvando…' : `Confirmar ${financialStatusLabel(financialAction)}`}
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )}

            {canPrintExpeditionOrder(isOperator, selectedOrder.status, selectedOrder.has_label) && (
              <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-brand szv2-btn-sm"
                  disabled={printBusy}
                  onClick={() => printExpedition([selectedOrder.id], true, false)}
                >
                  🖨️ Imprimir etiqueta{normStatus(selectedOrder.status) === 'processing' ? ' (separa)' : ''}
                </button>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                  disabled={printBusy}
                  onClick={() => printExpedition([selectedOrder.id], false, true)}
                >
                  📄 Declaração
                </button>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                  disabled={printBusy}
                  onClick={() => printExpedition([selectedOrder.id], true, true)}
                >
                  🖨️📄 Ambos
                </button>
              </div>
            )}

            {/* Ações — movidas pra dentro do drawer (não ficam soltas na linha da tabela). */}
            {/* Emitir Etiqueta NUNCA aparece pro produtor — ele só aprova/cancela.
                A emissão em si (débito do saldo + compra da etiqueta) é ação de
                operação (admin), não do produtor. */}
            {(() => {
              const canApprove = !!selectedOrder.actions?.can_approve
              const canCancel = !!selectedOrder.actions?.can_cancel
              const canRetry = !!selectedOrder.actions?.can_retry
              if (!canApprove && !canCancel && !canRetry) return null
              return (
                <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 16, paddingTop: 16, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  {canApprove && (
                    <button type="button" className="szv2-btn szv2-btn-brand szv2-btn-sm" disabled={actionBusy} onClick={() => runAction(selectedOrder, 'approve')}>
                      {actionBusy ? 'Aprovando…' : 'Aprovar'}
                    </button>
                  )}
                  {canCancel && (
                    <button type="button" className="szv2-btn szv2-btn-danger szv2-btn-sm" disabled={actionBusy} onClick={() => runAction(selectedOrder, 'cancel')}>
                      Cancelar
                    </button>
                  )}
                  {canRetry && (
                    <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" disabled={actionBusy} onClick={() => runAction(selectedOrder, 'retry')}>
                      Reprocessar
                    </button>
                  )}
                </div>
              )
            })()}
          </>
        )}
      </ActionDrawer>
    </section>
  )
}
