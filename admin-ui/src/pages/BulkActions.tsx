import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { safeUrl } from '../utils/safeUrl' // AUDIT-2026-06-21 #13
import { brDate } from '../utils/format'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import { confirmAsync } from '../components/ConfirmDialog'
import DetailDrawer from '../components/DetailDrawer'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import StatusBadge, { statusLabel } from '../components/StatusBadge'
import { emitToast } from '../hooks/useToast'

// ── Tipos ─────────────────────────────────────────────────────────────────────

type LabelStatus = 'none' | 'queued' | 'processing' | 'done' | 'error' | 'cancelled'

type BulkOrder = {
  order_id: number
  customer_name: string
  status: string
  shipping_class: string
  shipping_class_id: number | null
  label_status: LabelStatus
  total: number
  created_at: string
  // Motoboy — pedido vinculado em sz_motoboy_pedidos (pode não existir).
  pedido_id: number | null
  motoboy_id: number | null
  motoboy_nome: string
  mb_status: string // '' | agendado | embalado | em_rota | a_caminho | entregue | frustrado | cancelado
  package_code: string
  dest_nome: string
  dest_endereco: string
  dest_numero: string
  dest_complemento: string
  dest_bairro: string
  dest_cidade: string
  dest_uf: string
  dest_cep: string
  dest_telefone: string
  dest_produto: string // "3x Datalaprox" (qtd + nome) — sz_motoboy_pedidos.dest_produto
}

type ShippingClass = {
  id: number
  name: string
}

type Motoboy = {
  id: number
  nome: string
}

type QueueItem = {
  order_id: number
  status: LabelStatus
  print_url: string | null
  error: string | null
}

type GenerateResult = {
  ok: boolean
  queued: number
  already_queued: number
  errors: string[]
}

type Mode = 'with_pdf' | 'no_pdf' | 'print_batch'

// ── Helpers ───────────────────────────────────────────────────────────────────

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

// Tradução PT-BR dos slugs WC (status do pedido sz_orders) que o StatusBadge
// compartilhado não traduz sozinho (slugs em inglês: pending/on-hold/…).
// Usado só como FALLBACK para pedidos Melhor Envio, que não têm status motoboy.
const WC_STATUS_LABEL: Record<string, string> = {
  pending:   'Pendente',
  processing:'Processando',
  'on-hold': 'Aguardando',
  completed: 'Concluído',
  cancelled: 'Cancelado',
}

// Rótulo PT-BR do status universal — usa a tradução WC quando houver, senão o
// label padronizado do StatusBadge (que cobre o vocabulário motoboy).
function universalStatusLabel(status: string): string {
  return WC_STATUS_LABEL[status] ?? statusLabel(status)
}

// Badge de status da etiqueta.
function LabelBadge({ status }: { status: LabelStatus }) {
  const map: Record<LabelStatus, { label: string; cls: string }> = {
    none:       { label: 'Sem etiqueta',  cls: 'szv2-badge-muted' },
    queued:     { label: 'Em fila…',      cls: 'szv2-badge-warning' },
    processing: { label: 'Em fila…',      cls: 'szv2-badge-warning' },
    done:       { label: '✅ Gerada',      cls: 'szv2-badge-success' },
    error:      { label: '✗ Erro',        cls: 'szv2-badge-danger' },
    cancelled:  { label: 'Cancelada',     cls: 'szv2-badge-muted' },
  }
  const { label, cls } = map[status] ?? map.none
  return <span className={`sz-badge ${cls}`}>{label}</span>
}

// ── Fluxo de etiqueta MOTOBOY ──────────────────────────────────────────────────
// Estado de etiqueta derivado de sz_motoboy_pedidos.status (mb_status):
//   agendado             → pode GERAR etiqueta (transição → embalado)
//   embalado             → pode IMPRIMIR (já gerada)
//   em_rota / posterior  → SEM acesso à etiqueta
//   '' (sem pedido)      → sem etiqueta

// ── Tipo do pedido (COD/motoboy × Melhor Envio) ──────────────────────────────
// Image#38 (2): NUNCA mostrar opção ME para pedido motoboy e vice-versa.
// Discriminador: pedido com linha em sz_motoboy_pedidos (pedido_id != null /
// mb_status != '') é COD/motoboy; o resto segue o fluxo Melhor Envio.
function isMotoboyOrder(o: BulkOrder): boolean {
  return o.pedido_id != null || o.mb_status !== ''
}
function isMeOrder(o: BulkOrder): boolean {
  return !isMotoboyOrder(o)
}

// Motoboy já definido no pedido (não precisa escolher no SELECT para gerar).
function temMotoboyDefinido(o: BulkOrder): boolean {
  return o.motoboy_id != null && o.motoboy_id > 0
}

// Pode gerar etiqueta = pedido motoboy em 'agendado'.
function podeGerarEtiqueta(o: BulkOrder): boolean {
  return o.mb_status === 'agendado'
}
// Pode imprimir = já embalado.
function podeImprimirEtiqueta(o: BulkOrder): boolean {
  return o.mb_status === 'embalado'
}
function podeEntregue(o: BulkOrder): boolean {
  return o.mb_status === 'em_rota'
}
function podeFrustrado(o: BulkOrder): boolean {
  return o.mb_status === 'em_rota'
}

// ── Status universal do pedido ───────────────────────────────────────────────
// UMA coluna de STATUS UNIVERSAL: para pedido motoboy/COD usa o status do fluxo
// motoboy (sz_motoboy_pedidos.status — agendado/embalado/em_rota/entregue/…); p/
// pedido Melhor Envio cai no status do pedido (sz_orders.status). Sem coluna
// "Etiqueta (motoboy)" redundante. Cores/labels vêm do StatusBadge central.
function universalStatus(o: BulkOrder): string {
  return o.mb_status || o.status
}

// ── Componente principal ───────────────────────────────────────────────────────

export default function BulkActions() {
  // Filtros
  const [statusFilter, setStatusFilter] = useState('')
  const [scFilter, setScFilter]         = useState('')
  const [dateFrom, setDateFrom]         = useState('')
  const [dateTo, setDateTo]             = useState('')
  const [q, setQ]                       = useState('')

  // Painel
  const [filterOpen, setFilterOpen]     = useState(false)
  const [draftStatus, setDraftStatus]   = useState('')
  const [draftSc, setDraftSc]           = useState('')
  const [draftIni, setDraftIni]         = useState('')
  const [draftFim, setDraftFim]         = useState('')
  const [draftQ, setDraftQ]             = useState('')

  // Dados
  const [orders, setOrders]           = useState<BulkOrder[]>([])
  const [shippingClasses, setSC]      = useState<ShippingClass[]>([])
  const [motoboys, setMotoboys]       = useState<Motoboy[]>([])
  const [loading, setLoading]         = useState(true)
  const [err, setErr]                 = useState('')

  // Motoboy selecionado p/ atribuir ao gerar etiqueta.
  const [selMotoboy, setSelMotoboy]   = useState('')

  // Menu lateral de ações em lote (Image#38: barra flutuante → drawer).
  const [actionsOpen, setActionsOpen] = useState(false)

  // Seleção
  const [selected, setSelected] = useState<Set<number>>(new Set())

  // Ação / resultado
  const [busy, setBusy]                   = useState(false)
  const [result, setResult]               = useState<GenerateResult | null>(null)
  const [queueItems, setQueueItems]       = useState<QueueItem[]>([])

  // Modal de mudança de status individual
  const [statusModal, setStatusModal] = useState<{
    order: BulkOrder
    targetStatus: 'entregue' | 'frustrado' | 'cancelado'
  } | null>(null)
  const [smMotivo, setSmMotivo] = useState('')
  const [smObs, setSmObs] = useState('')
  const [smBusy, setSmBusy] = useState(false)

  // Polling ref
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // ── Carregamento ─────────────────────────────────────────────────────────────

  async function loadShippingClasses() {
    try {
      const r = await api<{ items: ShippingClass[] }>('/bulk-actions/shipping-classes')
      setSC(r.items || [])
    } catch {
      // Ignora — dropdown simplesmente fica vazio.
    }
  }

  async function loadMotoboys() {
    try {
      // Endpoint já existente — lista motoboys ativos (ativo=true).
      const r = await api<{ items: Motoboy[] }>('/motoboys')
      setMotoboys(r.items || [])
    } catch {
      // Ignora — dropdown de motoboy fica vazio.
    }
  }

  async function loadOrders() {
    setLoading(true)
    setErr('')
    setSelected(new Set())
    setResult(null)
    stopPolling()
    try {
      const qs = new URLSearchParams()
      if (statusFilter) qs.set('status', statusFilter)
      if (scFilter)     qs.set('shipping_class', scFilter)
      if (dateFrom)     qs.set('date_from', dateFrom)
      if (dateTo)       qs.set('date_to', dateTo)
      if (q.trim())     qs.set('q', q.trim())
      qs.set('limit', '200')
      const r = await api<{ items: BulkOrder[]; count: number }>(
        `/bulk-actions/orders?${qs}`
      )
      setOrders(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar pedidos')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { loadShippingClasses(); loadMotoboys() }, [])
  useEffect(() => { loadOrders() }, [statusFilter, scFilter, dateFrom, dateTo, q])

  function openPanel() {
    setDraftStatus(statusFilter); setDraftSc(scFilter); setDraftIni(dateFrom); setDraftFim(dateTo); setDraftQ(q)
    setFilterOpen(true)
  }
  function applyFilters() {
    setStatusFilter(draftStatus); setScFilter(draftSc); setDateFrom(draftIni); setDateTo(draftFim); setQ(draftQ)
    setFilterOpen(false)
  }
  function clearFilters() {
    setStatusFilter(''); setScFilter(''); setDateFrom(''); setDateTo(''); setQ('')
    setDraftStatus(''); setDraftSc(''); setDraftIni(''); setDraftFim(''); setDraftQ('')
    setFilterOpen(false)
  }

  // ── Toast ────────────────────────────────────────────────────────────────────

  function showToast(kind: 'ok' | 'err', msg: string) {
    emitToast(kind, msg)
  }

  // ── Seleção ──────────────────────────────────────────────────────────────────

  const allIds = orders.map(o => o.order_id)

  function toggleAll() {
    if (selected.size === allIds.length) {
      setSelected(new Set())
    } else {
      setSelected(new Set(allIds))
    }
  }

  function toggleOne(id: number) {
    const s = new Set(selected)
    s.has(id) ? s.delete(id) : s.add(id)
    setSelected(s)
  }

  // ── Polling de status ────────────────────────────────────────────────────────

  function stopPolling() {
    if (pollRef.current) {
      clearInterval(pollRef.current)
      pollRef.current = null
    }
  }

  async function pollStatus(ids: number[]) {
    try {
      const r = await api<{ items: QueueItem[] }>(
        `/bulk-actions/queue-status?order_ids=${ids.join(',')}`
      )
      const items = r.items || []
      setQueueItems(items)

      // Atualiza label_status na tabela em memória.
      setOrders(prev => {
        const statusMap = new Map(items.map(qi => [qi.order_id, qi.status]))
        return prev.map(o =>
          statusMap.has(o.order_id)
            ? { ...o, label_status: statusMap.get(o.order_id)! }
            : o
        )
      })

      // Para polling quando todos prontos.
      const done = items.every(qi => qi.status === 'done' || qi.status === 'error')
      if (done) stopPolling()
    } catch {
      // Silencia erros de polling.
    }
  }

  function startPolling(ids: number[]) {
    stopPolling()
    pollRef.current = setInterval(() => pollStatus(ids), 5000)
  }

  useEffect(() => () => stopPolling(), [])

  // ── Ação: gerar etiquetas ────────────────────────────────────────────────────

  async function handleGenerate(mode: Mode) {
    // Image#38 (2): apenas pedidos Melhor Envio entram no fluxo ME — nunca
    // enfileira etiqueta ME para pedido COD/motoboy mesmo em seleção mista.
    const ids = Array.from(selected).filter(id => {
      const o = orders.find(x => x.order_id === id)
      return o ? isMeOrder(o) : false
    })
    if (ids.length === 0) {
      showToast('err', 'Nenhum pedido Melhor Envio selecionado.')
      return
    }

    const modeLabel: Record<Mode, string> = {
      with_pdf:    'gerar etiquetas (com PDF)',
      no_pdf:      'gerar sem PDF',
      print_batch: 'imprimir lote',
    }
    const ok = await confirmAsync({
      variant: 'confirm',
      title: 'Confirmar ação em lote',
      message: `Deseja ${modeLabel[mode]} para os ${ids.length} pedido(s) selecionado(s)?`,
      confirmLabel: 'Gerar',
    })
    if (!ok) return

    setBusy(true)
    setResult(null)
    setQueueItems([])
    try {
      const r = await api<GenerateResult>('/bulk-actions/generate-labels', {
        method: 'POST',
        body: JSON.stringify({ order_ids: ids, mode }),
      })
      setResult(r)
      if (r.queued > 0) {
        showToast('ok', `${r.queued} pedido(s) enfileirado(s)${r.already_queued ? ` (${r.already_queued} já estavam na fila)` : ''}.`)
        startPolling(ids)
        setActionsOpen(false) // libera o painel de resultado (progresso + links).
      } else if (r.already_queued > 0) {
        showToast('ok', `${r.already_queued} pedido(s) já estavam na fila. Acompanhe o progresso abaixo.`)
        startPolling(ids)
        setActionsOpen(false)
      }
      if (r.errors?.length) {
        showToast('err', `${r.errors.length} erro(s) ao enfileirar. Veja o painel de resultado.`)
      }
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao enfileirar etiquetas')
    } finally {
      setBusy(false)
    }
  }

  // ── Ação: gerar etiqueta MOTOBOY (agendado → embalado) ───────────────────────
  // Aceita uma lista explícita de order_ids (botão por linha) ou usa a seleção.
  async function handleMotoboyGenerate(ids?: number[]) {
    const targetIds = (ids && ids.length ? ids : Array.from(selected))
      // Só pedidos elegíveis (agendado) entram — espelha o guard do servidor.
      .filter(id => {
        const o = orders.find(x => x.order_id === id)
        return o ? podeGerarEtiqueta(o) : false
      })

    if (targetIds.length === 0) {
      showToast('err', 'Nenhum pedido elegível (status “agendado”) selecionado.')
      return
    }

    const motoboyId = selMotoboy ? Number(selMotoboy) : null

    // Image#38 (3): NÃO embalar/gerar etiqueta sem motoboy definido. Em vez do
    // popup "vai ficar indefinido", exige o SELECT antes de gerar. Um pedido
    // está OK se já tem motoboy_id OU se um motoboy foi escolhido no SELECT.
    const semMotoboy = targetIds.filter(id => {
      const o = orders.find(x => x.order_id === id)
      return o ? !temMotoboyDefinido(o) : false
    })
    if (motoboyId == null && semMotoboy.length > 0) {
      showToast(
        'err',
        `Selecione um motoboy antes de gerar — ${semMotoboy.length} pedido(s) ainda sem motoboy definido.`,
      )
      // Abre o menu lateral para o usuário escolher no SELECT.
      setActionsOpen(true)
      return
    }

    const nomeMb = motoboyId ? (motoboys.find(m => m.id === motoboyId)?.nome ?? '') : ''
    const msg = `Gerar etiqueta de ${targetIds.length} pedido(s)?` +
      (nomeMb ? `\n\nMotoboy: ${nomeMb}` : '\n\n(mantém o motoboy já atribuído a cada pedido)') +
      `\n\nO status passará para “embalado” e a etiqueta não poderá ser gerada novamente.`
    const ok = await confirmAsync({
      variant: 'warning',
      title: 'Gerar etiqueta motoboy',
      message: msg,
      confirmLabel: 'Gerar etiqueta',
    })
    if (!ok) return

    setBusy(true)
    try {
      const r = await api<{ ok: boolean; embalado: number; skipped: number; embalado_ids: number[] }>(
        '/bulk-actions/motoboy-generate-labels',
        {
          method: 'POST',
          body: JSON.stringify({ order_ids: targetIds, motoboy_id: motoboyId }),
        },
      )
      if (r.embalado > 0) {
        showToast('ok', `${r.embalado} etiqueta(s) gerada(s) (status → embalado)` +
          (r.skipped > 0 ? ` · ${r.skipped} ignorado(s).` : '.'))
        setActionsOpen(false)
      } else {
        showToast('err', 'Nenhuma etiqueta gerada — pedidos não estavam em “agendado”.')
      }
      // Recarrega para refletir o novo status (sem polling — fluxo é síncrono).
      await loadOrders()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao gerar etiqueta motoboy')
    } finally {
      setBusy(false)
    }
  }

  // ── Ação: imprimir etiqueta MOTOBOY (somente embalado) ───────────────────────
  function handlePrintEtiqueta(o: BulkOrder) {
    if (!podeImprimirEtiqueta(o)) return
    const win = window.open('', '_blank', 'width=420,height=620')
    if (!win) {
      showToast('err', 'Bloqueado pelo navegador — permita pop-ups para imprimir.')
      return
    }
    const esc = (s: string) =>
      String(s ?? '').replace(/[&<>"]/g, c =>
        ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] as string))
    const qrSrc =
      'https://api.qrserver.com/v1/create-qr-code/?size=150x150&data=' +
      encodeURIComponent(o.package_code)
    const endereco = [o.dest_endereco, o.dest_numero].filter(Boolean).join(', ')
    // Telefone sem +55 (DDI) — paridade com a etiqueta motoboy.
    const telD = (o.dest_telefone || '').replace(/\D+/g, '')
    const telFmt = (telD.length === 12 || telD.length === 13) && telD.startsWith('55') ? telD.slice(2) : telD
    const html = `<!doctype html><html lang="pt-BR"><head><meta charset="utf-8">
<title>Etiqueta ${o.order_id}</title>
<style>
  *{box-sizing:border-box;font-family:Arial,Helvetica,sans-serif}
  body{margin:0;padding:16px;color:#111}
  .etq{border:2px solid #111;border-radius:8px;padding:14px;max-width:360px}
  .ped{font-size:20px;font-weight:800;margin:0 0 4px}
  .mb{font-size:12px;color:#1E6FF2;font-weight:700;margin:0 0 10px}
  .nome{font-size:16px;font-weight:700;margin:0 0 2px}
  .lin{font-size:13px;margin:1px 0}
  .qr{text-align:center;margin-top:12px}
  .code{font-size:11px;letter-spacing:.5px;margin-top:4px;color:#333}
  @media print{button{display:none}body{padding:0}}
</style></head><body>
  <div class="etq">
    <p class="ped">Pedido ${esc(String(o.order_id))}</p>
    ${o.motoboy_nome ? `<p class="mb">Motoboy: ${esc(o.motoboy_nome)}</p>` : ''}
    <p class="nome">${esc(o.dest_nome || o.customer_name)}</p>
    ${o.dest_produto ? `<p class="lin"><strong>${esc(o.dest_produto)}</strong></p>` : ''}
    <p class="lin">${esc(endereco)}</p>
    ${o.dest_complemento ? `<p class="lin">Compl.: ${esc(o.dest_complemento)}</p>` : ''}
    <p class="lin">${esc([o.dest_bairro, o.dest_cidade, o.dest_uf].filter(Boolean).join(' · '))}</p>
    <p class="lin">CEP: ${esc(o.dest_cep)}</p>
    ${telFmt ? `<p class="lin">Tel.: ${esc(telFmt)}</p>` : ''}
    <p class="lin"><strong>Valor: R$ ${esc(fmt(o.total))}</strong></p>
    <div class="qr">
      <img src="${qrSrc}" alt="QR" width="150" height="150">
      <div class="code">${esc(o.package_code)}</div>
    </div>
  </div>
  <div style="text-align:center;margin-top:12px">
    <button onclick="window.print()">Imprimir</button>
  </div>
</body></html>`
    win.document.write(html)
    win.document.close()
  }

  // ── Ação: etiqueta(s)/declaração — já emitidas (pedido dono 2026-07-28) ──────
  // Selecionado (1 ou vários) + status elegível (label_status='done' — etiqueta
  // JÁ EMITIDA) → agrupa e devolve documento(s) num PDF só (ME combina do lado
  // dela quando manda vários shipment_ids). Individual usa o MESMO endpoint com
  // 1 order_id só. NUNCA gera etiqueta nova nem muda status — Imprimir lote
  // (acima) já cobre gerar+imprimir do zero; isto aqui é reimpressão/documento
  // extra depois que já foi separado, sem re-disparar nada.
  async function handlePrintBatch(wantLabels: boolean, wantDeclaration: boolean) {
    const ids = selMeOrders.filter(o => o.label_status === 'done').map(o => o.order_id)
    if (ids.length === 0) {
      showToast('err', 'Nenhum pedido selecionado com etiqueta já emitida.')
      return
    }
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; label_url?: string; declaration_url?: string; missing_order_ids?: number[] }>(
        '/bulk-actions/print-batch',
        { method: 'POST', body: JSON.stringify({ order_ids: ids, want_labels: wantLabels, want_declaration: wantDeclaration }) },
      )
      if (r.label_url) window.open(safeUrl(r.label_url), '_blank', 'noopener,noreferrer')
      if (r.declaration_url) window.open(safeUrl(r.declaration_url), '_blank', 'noopener,noreferrer')
      if (!r.label_url && !r.declaration_url) {
        showToast('err', 'Nada retornado pela ME.')
        return
      }
      if (wantLabels) await Promise.all(ids.map(id => api(`/orders/${id}/mark-packed`, { method: 'POST' }).catch(() => {})))
      showToast('ok', `${ids.length} pedido(s) processado(s)${r.missing_order_ids?.length ? ` · ${r.missing_order_ids.length} sem etiqueta (ignorado)` : ''}.`)
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao recuperar documento(s) na ME')
    } finally {
      setBusy(false)
    }
  }

  // ── Ação: forçar status motoboy individual ───────────────────────────────────
  async function handleForceStatus() {
    if (!statusModal) return
    setSmBusy(true)
    try {
      await api(`/orders/${statusModal.order.order_id}/force-motoboy-status`, {
        method: 'POST',
        body: JSON.stringify({
          target_status: statusModal.targetStatus,
          motivo: smMotivo,
          observacao: smObs,
        }),
      })
      showToast('ok', `Status → ${statusModal.targetStatus}`)
      setStatusModal(null)
      setSmMotivo('')
      setSmObs('')
      await loadOrders()
    } catch (e: any) {
      showToast('err', e?.message || 'Erro ao mudar status')
    } finally {
      setSmBusy(false)
    }
  }

  // ── Renderização ─────────────────────────────────────────────────────────────

  const selCount = selected.size
  const allSelected = selCount > 0 && selCount === allIds.length
  const someSelected = selCount > 0 && !allSelected

  // Partição da seleção por tipo de pedido (Image#38: filtra ações por tipo).
  const selOrders   = orders.filter(o => selected.has(o.order_id))
  const selMeOrders = selOrders.filter(isMeOrder)
  const selMbOrders = selOrders.filter(isMotoboyOrder)
  const hasMeSel    = selMeOrders.length > 0
  const hasMbSel    = selMbOrders.length > 0

  // Pedidos selecionados elegíveis a gerar etiqueta motoboy (status 'agendado').
  const selEligibleMbOrders = selMbOrders.filter(podeGerarEtiqueta)
  const selEligibleMb = selEligibleMbOrders.length
  // Pedidos elegíveis que ainda NÃO têm motoboy definido (exigem o SELECT).
  const selMbSemMotoboy = selEligibleMbOrders.filter(o => !temMotoboyDefinido(o)).length
  // Precisa escolher motoboy no SELECT? (há elegível sem motoboy e nada escolhido)
  const precisaEscolherMb = selMbSemMotoboy > 0 && !selMotoboy

  // Progresso: itens done ou error dentre os enfileirados.
  const progressDone  = queueItems.filter(qi => qi.status === 'done').length
  const progressError = queueItems.filter(qi => qi.status === 'error').length
  const progressTotal = queueItems.length

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (statusFilter) chips.push({ key: 'status', label: `Status: ${statusFilter}`, onRemove: () => setStatusFilter('') })
  if (scFilter) chips.push({ key: 'sc', label: `Classe: ${scFilter}`, onRemove: () => setScFilter('') })
  if (dateFrom) chips.push({ key: 'ini', label: `De: ${dateFrom}`, onRemove: () => setDateFrom('') })
  if (dateTo) chips.push({ key: 'fim', label: `Até: ${dateTo}`, onRemove: () => setDateTo('') })
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })

  return (
    <div>
      {err && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>
      )}

      <div className="szv2-section-head">
        <div>
          <h1>Ações em lote — Etiquetas</h1>
          <p>{orders.length} pedido(s) com filtros aplicados.</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton
            active={chips.length > 0}
            count={chips.length}
            onClick={openPanel}
          />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <button
          type="button"
          className="szv2-btn szv2-btn-secondary"
          onClick={() => { loadOrders(); setFilterOpen(false) }}
          disabled={loading}
          style={{ width: '100%', marginBottom: 12 }}
        >
          {loading ? 'Carregando…' : '🔄 Atualizar lista'}
        </button>
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftIni}
            onChange={v => setDraftIni(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftFim}
            onChange={v => setDraftFim(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Status">
          <FalkSelect
            aria-label="Status"
            value={draftStatus}
            onChange={v => setDraftStatus(v)}
            options={[
              { value: '', label: 'Todos' },
              { value: 'processing', label: 'Processando' },
              { value: 'pending', label: 'Pendente' },
              { value: 'completed', label: 'Concluído' },
              { value: 'on-hold', label: 'Aguardando' },
              { value: 'cancelled', label: 'Cancelado' },
            ]}
          />
        </FilterField>
        {shippingClasses.length > 0 && (
          <FilterField label="Classe de envio">
            <FalkSelect
              aria-label="Classe de envio"
              value={draftSc}
              onChange={v => setDraftSc(v)}
              options={[
                { value: '', label: 'Todas as classes' },
                ...shippingClasses.map(sc => ({ value: sc.name, label: sc.name })),
              ]}
            />
          </FilterField>
        )}
        <FilterField label="Busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="Pedido / cliente"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>

      {/* ── Tabela de seleção ────────────────────────────────────────────────── */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Pedidos elegíveis</h2>
            <p className="szv2-card-sub">
              {orders.length} pedido(s)
              {selCount > 0 ? ` — ${selCount} selecionado(s)` : ''}
            </p>
          </div>
        </div>

        {loading && orders.length === 0 ? (
          <TableSkeleton rows={6} cols={8} />
        ) : !loading && orders.length === 0 ? (
          <EmptyState
            icon="📦"
            title="Nenhum pedido elegível para ação em lote."
            description="Ajuste os filtros acima para encontrar pedidos."
          />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th style={{ width: 36 }}>
                    <input
                      type="checkbox"
                      checked={allSelected}
                      ref={el => {
                        if (el) el.indeterminate = someSelected
                      }}
                      onChange={toggleAll}
                      title="Selecionar todos"
                    />
                  </th>
                  <th>Pedido</th>
                  <th>Cliente</th>
                  <th>Status</th>
                  <th>Motoboy</th>
                  <th style={{ textAlign: 'right' }}>Valor</th>
                  <th>Data</th>
                  <th style={{ textAlign: 'center' }}>Ações</th>
                </tr>
              </thead>
              <tbody>
                {orders.map(o => (
                  <tr
                    key={o.order_id}
                    style={{
                      background: selected.has(o.order_id)
                        ? 'rgba(30, 111, 242,.04)'
                        : undefined,
                      cursor: 'pointer',
                    }}
                    onClick={() => toggleOne(o.order_id)}
                  >
                    <td onClick={e => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        checked={selected.has(o.order_id)}
                        onChange={() => toggleOne(o.order_id)}
                      />
                    </td>
                    <td><strong>{o.order_id}</strong></td>
                    <td>{o.customer_name || '—'}</td>
                    <td>
                      <StatusBadge
                        status={universalStatus(o)}
                        label={universalStatusLabel(universalStatus(o))}
                      />
                    </td>
                    <td>
                      {o.motoboy_nome
                        ? o.motoboy_nome
                        : <span style={{ color: 'var(--szv2-text-muted)' }}>Não atribuído</span>}
                    </td>
                    <td style={{ textAlign: 'right' }}>R$ {fmt(o.total)}</td>
                    <td style={{ color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>
                      {o.created_at ? brDate(o.created_at) : '—'}
                    </td>
                    <td
                      style={{ textAlign: 'center', whiteSpace: 'nowrap' }}
                      onClick={e => e.stopPropagation()}
                    >
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 4, alignItems: 'center' }}>
                        {podeGerarEtiqueta(o) ? (
                          temMotoboyDefinido(o) ? (
                            // 'agendado' + motoboy já definido → gera direto.
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-brand"
                              style={{ fontSize: 12, padding: '4px 10px' }}
                              disabled={busy}
                              onClick={() => handleMotoboyGenerate([o.order_id])}
                              title="Gerar etiqueta (status → embalado)"
                            >
                              📦 Gerar
                            </button>
                          ) : (
                            // 'agendado' SEM motoboy → não gera direto (Image#38 (3)).
                            // Encaminha para o menu lateral escolher o motoboy no SELECT.
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-secondary"
                              style={{ fontSize: 12, padding: '4px 10px' }}
                              disabled={busy}
                              onClick={() => {
                                // Garante a linha na seleção (sem TOGGLE — não desmarcar
                                // se já estava marcada) e abre o menu p/ escolher motoboy.
                                setSelected(prev => new Set(prev).add(o.order_id))
                                setActionsOpen(true)
                              }}
                              title="Defina o motoboy no menu de ações antes de gerar"
                            >
                              👤 Definir motoboy
                            </button>
                          )
                        ) : podeImprimirEtiqueta(o) ? (
                          // Status 'embalado' → permite imprimir.
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-secondary"
                            style={{ fontSize: 12, padding: '4px 10px' }}
                            onClick={() => handlePrintEtiqueta(o)}
                            title="Imprimir etiqueta"
                          >
                            🖨️ Imprimir
                          </button>
                        ) : (
                          // em_rota ou posterior / sem pedido → sem acesso à etiqueta.
                          <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>—</span>
                        )}
                        {isMotoboyOrder(o) && (
                          <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', justifyContent: 'center' }}>
                            {podeEntregue(o) && (
                              <button type="button" className="szv2-btn szv2-btn-secondary" style={{ fontSize: 11, padding: '3px 8px' }}
                                onClick={() => { setStatusModal({ order: o, targetStatus: 'entregue' }); setSmMotivo(''); setSmObs('') }}>
                                ✅ Entregue
                              </button>
                            )}
                            {podeFrustrado(o) && (
                              <button type="button" className="szv2-btn szv2-btn-danger" style={{ fontSize: 11, padding: '3px 8px' }}
                                onClick={() => { setStatusModal({ order: o, targetStatus: 'frustrado' }); setSmMotivo(''); setSmObs('') }}>
                                ✗ Frustrado
                              </button>
                            )}
                          </div>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* ── Painel de resultado ──────────────────────────────────────────────── */}
      {result && (
        <div className="szv2-card" style={{ marginTop: 16 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Resultado do envio</h2>
              <p className="szv2-card-sub">
                {result.queued} enfileirado(s) · {result.already_queued} já na fila ·{' '}
                {result.errors.length} erro(s)
              </p>
            </div>
          </div>

          {/* Barra de progresso */}
          {progressTotal > 0 && (
            <div style={{ marginBottom: 16 }}>
              <div style={{
                display: 'flex',
                justifyContent: 'space-between',
                marginBottom: 4,
                fontSize: 13,
                color: 'var(--szv2-text-muted)',
              }}>
                <span>Progresso</span>
                <span>
                  {progressDone} / {progressTotal} gerado(s)
                  {progressError > 0 ? ` · ${progressError} erro(s)` : ''}
                </span>
              </div>
              <div style={{
                height: 8,
                borderRadius: 99,
                background: 'var(--szv2-border)',
                overflow: 'hidden',
              }}>
                <div style={{
                  height: '100%',
                  borderRadius: 99,
                  background: progressError > 0 ? 'var(--szv2-danger)' : 'var(--szv2-brand)',
                  width: `${progressTotal > 0 ? Math.round(((progressDone + progressError) / progressTotal) * 100) : 0}%`,
                  transition: 'width .4s ease',
                }} />
              </div>
            </div>
          )}

          {/* Links de impressão prontos */}
          {queueItems.filter(qi => qi.status === 'done' && qi.print_url).length > 0 && (
            <div style={{ marginBottom: 16 }}>
              <p style={{ fontWeight: 600, marginBottom: 8 }}>Etiquetas prontas:</p>
              <ul style={{ margin: 0, padding: '0 0 0 20px' }}>
                {queueItems
                  .filter(qi => qi.status === 'done' && qi.print_url)
                  .map(qi => (
                    <li key={qi.order_id}>
                      <a href={safeUrl(qi.print_url)} target="_blank" rel="noreferrer">
                        Pedido {qi.order_id} — imprimir
                      </a>
                    </li>
                  ))}
              </ul>
            </div>
          )}

          {/* Erros de enfileiramento */}
          {result.errors.length > 0 && (
            <div>
              <p style={{ fontWeight: 600, color: 'var(--szv2-danger)', marginBottom: 8 }}>
                Erros ao enfileirar:
              </p>
              <ul style={{ margin: 0, padding: '0 0 0 20px', color: 'var(--szv2-danger)', fontSize: 13 }}>
                {result.errors.map((e, i) => <li key={i}>{e}</li>)}
              </ul>
            </div>
          )}

          {/* Status individual */}
          {queueItems.length > 0 && (
            <div style={{ overflowX: 'auto', marginTop: 16 }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Pedido</th>
                    <th>Status fila</th>
                    <th>Ação</th>
                  </tr>
                </thead>
                <tbody>
                  {queueItems.map(qi => (
                    <tr key={qi.order_id}>
                      <td><strong>{qi.order_id}</strong></td>
                      <td><LabelBadge status={qi.status} /></td>
                      <td>
                        {qi.print_url ? (
                          <a
                            href={safeUrl(qi.print_url)}
                            target="_blank"
                            rel="noreferrer"
                            className="szv2-btn-secondary"
                            style={{ fontSize: 12 }}
                          >
                            Imprimir
                          </a>
                        ) : qi.error ? (
                          <span style={{ color: 'var(--szv2-danger)', fontSize: 12 }}>
                            {qi.error}
                          </span>
                        ) : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* ── Gatilho sticky → abre menu lateral de ações (Image#38 (1)) ────────── */}
      {/* A barra flutuante virou um botão compacto que abre o drawer; assim o
          overlay não bloqueia a re-seleção de linhas na tabela. */}
      {selCount > 0 && !actionsOpen && (
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
            padding: '10px 14px 10px 18px',
            display: 'flex',
            alignItems: 'center',
            gap: 12,
            zIndex: 1000,
          }}
        >
          <span style={{ fontWeight: 700, color: 'var(--szv2-brand)', whiteSpace: 'nowrap' }}>
            {selCount} pedido{selCount !== 1 ? 's' : ''} selecionado{selCount !== 1 ? 's' : ''}
          </span>
          <button
            type="button"
            className="szv2-btn szv2-btn-brand"
            onClick={() => setActionsOpen(true)}
            disabled={busy}
            title="Abrir menu de ações em lote"
          >
            ⚙️ Ações
          </button>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={() => setSelected(new Set())}
            disabled={busy}
          >
            Limpar
          </button>
        </div>
      )}

      {/* ── Modal de mudança de status ──────────────────────────────────────── */}
      {statusModal && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0,0,0,.5)',
          display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 2000,
        }} onClick={() => !smBusy && setStatusModal(null)}>
          <div style={{
            background: 'var(--szv2-card-bg, #fff)', borderRadius: 12, padding: 24,
            minWidth: 340, maxWidth: 480, width: '90%',
          }} onClick={e => e.stopPropagation()}>
            <h3 style={{ margin: '0 0 16px', fontSize: 16, fontWeight: 700 }}>
              {statusModal.targetStatus === 'entregue' && '✅ Confirmar Entrega'}
              {statusModal.targetStatus === 'frustrado' && '✗ Registrar Frustrado'}
              {statusModal.targetStatus === 'cancelado' && '⊘ Cancelar Pedido'}
              {' — Pedido #'}{statusModal.order.order_id}
            </h3>

            {statusModal.targetStatus === 'frustrado' && (
              <>
                <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--szv2-text-muted)', display: 'block', marginBottom: 4 }}>
                  Motivo do frustrado
                </label>
                <select
                  className="sz-login-input"
                  value={smMotivo}
                  onChange={e => setSmMotivo(e.target.value)}
                  style={{ marginBottom: 12 }}
                  disabled={smBusy}
                >
                  <option value="">Selecione…</option>
                  <option value="cliente_ausente">Cliente ausente</option>
                  <option value="endereco_incorreto">Endereço incorreto</option>
                  <option value="cliente_recusou">Cliente recusou</option>
                  <option value="nao_encontrado">Endereço não encontrado</option>
                  <option value="outro">Outro</option>
                </select>

                <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--szv2-text-muted)', display: 'block', marginBottom: 4 }}>
                  Observação (opcional)
                </label>
                <textarea
                  className="sz-login-input"
                  value={smObs}
                  onChange={e => setSmObs(e.target.value)}
                  rows={3}
                  placeholder="Detalhes adicionais…"
                  style={{ marginBottom: 16, resize: 'vertical' }}
                  disabled={smBusy}
                />
              </>
            )}

            {statusModal.targetStatus === 'entregue' && (
              <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', marginBottom: 16 }}>
                Confirma a entrega do pedido #{statusModal.order.order_id} para {statusModal.order.dest_nome || statusModal.order.customer_name}?
              </p>
            )}

            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button type="button" className="szv2-btn szv2-btn-secondary"
                onClick={() => setStatusModal(null)} disabled={smBusy}>
                Cancelar
              </button>
              <button
                type="button"
                className={`szv2-btn ${statusModal.targetStatus === 'frustrado' ? 'szv2-btn-danger' : 'szv2-btn-brand'}`}
                onClick={handleForceStatus}
                disabled={smBusy || (statusModal.targetStatus === 'frustrado' && !smMotivo)}
              >
                {smBusy ? 'Salvando…' : 'Confirmar'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Menu lateral de ações em lote ────────────────────────────────────── */}
      <DetailDrawer
        open={actionsOpen && selCount > 0}
        onClose={() => setActionsOpen(false)}
        title={`Ações em lote — ${selCount} pedido${selCount !== 1 ? 's' : ''}`}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
          {/* Resumo da seleção por tipo */}
          <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)', lineHeight: 1.6 }}>
            {hasMbSel && (
              <div>
                🛵 <strong style={{ color: 'var(--szv2-text)' }}>{selMbOrders.length}</strong> pedido(s)
                COD/Motoboy{selEligibleMb > 0 ? ` · ${selEligibleMb} pronto(s) p/ etiqueta` : ''}
              </div>
            )}
            {hasMeSel && (
              <div>
                🏷️ <strong style={{ color: 'var(--szv2-text)' }}>{selMeOrders.length}</strong> pedido(s)
                Melhor Envio
              </div>
            )}
          </div>

          {/* ── Bloco MOTOBOY — só aparece se há pedido COD/motoboy na seleção ──
              Image#38 (2): nunca mostra ação motoboy para pedido ME. */}
          {hasMbSel && (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 10,
                paddingBottom: 16,
                borderBottom: hasMeSel ? '1px solid var(--szv2-divider)' : undefined,
              }}
            >
              <span style={{ fontWeight: 700, fontSize: 13, color: 'var(--szv2-text)' }}>
                Etiqueta Motoboy (COD)
              </span>

              {/* SELECT de motoboy — tema do site (Image#38 (3)). Obrigatório
                  quando há pedido elegível sem motoboy definido. */}
              <label style={{ fontSize: 12, color: 'var(--szv2-text-muted)', fontWeight: 600 }}>
                Motoboy
                {precisaEscolherMb && (
                  <span style={{ color: 'var(--szv2-danger)' }}> *obrigatório</span>
                )}
              </label>
              <FalkSelect
                aria-label="Motoboy"
                value={selMotoboy}
                onChange={v => setSelMotoboy(v)}
                disabled={busy}
                options={[
                  {
                    value: '',
                    label: selMbSemMotoboy > 0 ? 'Selecione um motoboy…' : 'Manter motoboy atual',
                  },
                  ...motoboys.map(m => ({ value: String(m.id), label: m.nome })),
                ]}
              />
              {precisaEscolherMb && (
                <span style={{ fontSize: 12, color: 'var(--szv2-danger)' }}>
                  {selMbSemMotoboy} pedido(s) ainda sem motoboy — escolha um acima para gerar.
                </span>
              )}

              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                style={{ width: '100%' }}
                onClick={() => handleMotoboyGenerate()}
                disabled={busy || selEligibleMb === 0 || precisaEscolherMb}
                title={
                  selEligibleMb === 0
                    ? 'Nenhum pedido selecionado em status “agendado”'
                    : precisaEscolherMb
                      ? 'Escolha um motoboy antes de gerar'
                      : 'Gerar etiqueta motoboy (status → embalado)'
                }
              >
                📦 Gerar etiqueta{selEligibleMb > 0 ? ` (${selEligibleMb})` : ''}
              </button>
            </div>
          )}

          {/* ── Bloco MELHOR ENVIO — só aparece se há pedido ME na seleção ──────
              Image#38 (2): nunca mostra "Etiqueta ME (PDF)" para pedido motoboy. */}
          {hasMeSel && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              <span style={{ fontWeight: 700, fontSize: 13, color: 'var(--szv2-text)' }}>
                Etiqueta Melhor Envio
              </span>
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                style={{ width: '100%' }}
                onClick={() => handleGenerate('with_pdf')}
                disabled={busy}
                title="Gerar etiquetas Melhor Envio com PDF"
              >
                🏷️ Etiqueta ME (PDF)
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                style={{ width: '100%' }}
                onClick={() => handleGenerate('no_pdf')}
                disabled={busy}
                title="Gerar etiquetas sem PDF (somente dados)"
              >
                📄 Gerar sem PDF
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                style={{ width: '100%' }}
                onClick={() => handleGenerate('print_batch')}
                disabled={busy}
                title="Imprimir etiquetas em lote"
              >
                🖨️ Imprimir lote
              </button>

              {/* Reimpressão/documentos extra — só pedidos JÁ com etiqueta emitida
                  (label_status='done'), sem gerar nada novo nem mudar status. */}
              <span style={{ fontWeight: 700, fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 8 }}>
                Já emitidas (reimprimir / documentos)
              </span>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                style={{ width: '100%' }}
                onClick={() => handlePrintBatch(true, false)}
                disabled={busy}
                title="Imprimir etiqueta(s) já emitida(s)"
              >
                🏷️ Imprimir etiqueta
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                style={{ width: '100%' }}
                onClick={() => handlePrintBatch(false, true)}
                disabled={busy}
                title="Declaração de Conteúdo (DACE simplificado)"
              >
                📄 Declaração de Conteúdo
              </button>
            </div>
          )}

          {/* Limpar seleção */}
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            style={{ width: '100%' }}
            onClick={() => { setSelected(new Set()); setActionsOpen(false) }}
            disabled={busy}
          >
            Limpar seleção
          </button>
        </div>
      </DetailDrawer>
    </div>
  )
}
