import { useEffect, useRef, useState } from 'react'
import { api } from '../api'

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
}

type ShippingClass = {
  id: number
  name: string
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

const STATUS_LABEL: Record<string, string> = {
  processing: 'Processando',
  pending:    'Pendente',
  completed:  'Concluído',
  cancelled:  'Cancelado',
  'on-hold':  'Aguardando',
}

// Badge de status do pedido WC.
function StatusBadge({ status }: { status: string }) {
  const label = STATUS_LABEL[status] ?? status
  const isProcessing = status === 'processing'
  return (
    <span
      className="sz-badge"
      style={{
        background: isProcessing ? 'rgba(234,88,12,.12)' : 'rgba(100,100,100,.1)',
        color: isProcessing ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
        fontWeight: 600,
        fontSize: 12,
        padding: '2px 8px',
        borderRadius: 99,
        whiteSpace: 'nowrap',
      }}
    >
      {label}
    </span>
  )
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

// ── Componente principal ───────────────────────────────────────────────────────

export default function BulkActions() {
  // Filtros
  const [statusFilter, setStatusFilter] = useState('')
  const [scFilter, setScFilter]         = useState('')
  const [dateFrom, setDateFrom]         = useState('')
  const [dateTo, setDateTo]             = useState('')

  // Dados
  const [orders, setOrders]           = useState<BulkOrder[]>([])
  const [shippingClasses, setSC]      = useState<ShippingClass[]>([])
  const [loading, setLoading]         = useState(true)
  const [err, setErr]                 = useState('')

  // Seleção
  const [selected, setSelected] = useState<Set<number>>(new Set())

  // Ação / resultado
  const [busy, setBusy]                   = useState(false)
  const [result, setResult]               = useState<GenerateResult | null>(null)
  const [queueItems, setQueueItems]       = useState<QueueItem[]>([])
  const [toast, setToast]                 = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

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

  useEffect(() => { loadShippingClasses() }, [])
  useEffect(() => { loadOrders() }, [statusFilter, scFilter, dateFrom, dateTo])

  // ── Toast ────────────────────────────────────────────────────────────────────

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 6000)
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
    const ids = Array.from(selected)
    if (ids.length === 0) return

    const modeLabel: Record<Mode, string> = {
      with_pdf:    'gerar etiquetas (com PDF)',
      no_pdf:      'gerar sem PDF',
      print_batch: 'imprimir lote',
    }
    if (!window.confirm(
      `${modeLabel[mode]}\n\n${ids.length} pedido(s) selecionado(s).\n\nContinuar?`
    )) return

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
      } else if (r.already_queued > 0) {
        showToast('ok', `${r.already_queued} pedido(s) já estavam na fila. Acompanhe o progresso abaixo.`)
        startPolling(ids)
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

  // ── Renderização ─────────────────────────────────────────────────────────────

  const selCount = selected.size
  const allSelected = selCount > 0 && selCount === allIds.length
  const someSelected = selCount > 0 && !allSelected

  // Progresso: itens done ou error dentre os enfileirados.
  const progressDone  = queueItems.filter(qi => qi.status === 'done').length
  const progressError = queueItems.filter(qi => qi.status === 'error').length
  const progressTotal = queueItems.length

  return (
    <div>
      {/* Toast */}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {err && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>
      )}

      {/* ── Barra de filtros ─────────────────────────────────────────────────── */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: 12,
            alignItems: 'center',
          }}
        >
          {/* Chips de status */}
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
            {[
              { value: '',           label: 'Todos' },
              { value: 'processing', label: 'Processando' },
              { value: 'pending',    label: 'Pendente' },
              { value: 'completed',  label: 'Concluído' },
            ].map(opt => (
              <button
                key={opt.value}
                type="button"
                onClick={() => setStatusFilter(opt.value)}
                style={{
                  padding: '4px 14px',
                  borderRadius: 99,
                  border: '1.5px solid',
                  borderColor: statusFilter === opt.value
                    ? 'var(--szv2-brand)'
                    : 'var(--szv2-border)',
                  background: statusFilter === opt.value
                    ? 'rgba(234,88,12,.10)'
                    : 'transparent',
                  color: statusFilter === opt.value
                    ? 'var(--szv2-brand)'
                    : 'var(--szv2-text)',
                  fontWeight: statusFilter === opt.value ? 700 : 400,
                  cursor: 'pointer',
                  fontSize: 13,
                }}
              >
                {opt.label}
              </button>
            ))}
          </div>

          {/* Classe de envio */}
          {shippingClasses.length > 0 && (
            <select
              value={scFilter}
              onChange={e => setScFilter(e.target.value)}
              className="szv2-select"
              style={{ minWidth: 160 }}
            >
              <option value="">Todas as classes</option>
              {shippingClasses.map(sc => (
                <option key={sc.id} value={sc.name}>{sc.name}</option>
              ))}
            </select>
          )}

          {/* Intervalo de data */}
          <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
            <input
              type="date"
              value={dateFrom}
              onChange={e => setDateFrom(e.target.value)}
              className="szv2-input"
              style={{ width: 140 }}
              placeholder="De"
            />
            <span style={{ color: 'var(--szv2-text-muted)' }}>–</span>
            <input
              type="date"
              value={dateTo}
              onChange={e => setDateTo(e.target.value)}
              className="szv2-input"
              style={{ width: 140 }}
              placeholder="Até"
            />
          </div>

          <button
            type="button"
            className="szv2-btn-secondary"
            onClick={loadOrders}
            disabled={loading}
          >
            {loading ? 'Carregando…' : 'Atualizar'}
          </button>
        </div>
      </div>

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

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : orders.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum pedido encontrado com os filtros atuais.
          </div>
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
                  <th>Classe envio</th>
                  <th>Etiqueta</th>
                  <th style={{ textAlign: 'right' }}>Valor</th>
                  <th>Data</th>
                </tr>
              </thead>
              <tbody>
                {orders.map(o => (
                  <tr
                    key={o.order_id}
                    style={{
                      background: selected.has(o.order_id)
                        ? 'rgba(234,88,12,.04)'
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
                    <td><strong>#{o.order_id}</strong></td>
                    <td>{o.customer_name || '—'}</td>
                    <td><StatusBadge status={o.status} /></td>
                    <td>{o.shipping_class || '—'}</td>
                    <td><LabelBadge status={o.label_status} /></td>
                    <td style={{ textAlign: 'right' }}>R$ {fmt(o.total)}</td>
                    <td style={{ color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>
                      {o.created_at ? o.created_at.slice(0, 10) : '—'}
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
                      <a href={qi.print_url!} target="_blank" rel="noreferrer">
                        Pedido #{qi.order_id} — imprimir
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
                      <td><strong>#{qi.order_id}</strong></td>
                      <td><LabelBadge status={qi.status} /></td>
                      <td>
                        {qi.print_url ? (
                          <a
                            href={qi.print_url}
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

      {/* ── Barra de ação sticky (visível quando há seleção) ─────────────────── */}
      {selCount > 0 && (
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
            padding: '12px 20px',
            display: 'flex',
            alignItems: 'center',
            gap: 12,
            zIndex: 1000,
            flexWrap: 'wrap',
          }}
        >
          <span style={{ fontWeight: 700, color: 'var(--szv2-brand)', whiteSpace: 'nowrap' }}>
            {selCount} pedido{selCount !== 1 ? 's' : ''} selecionado{selCount !== 1 ? 's' : ''}
          </span>

          <button
            type="button"
            className="szv2-btn-brand"
            onClick={() => handleGenerate('with_pdf')}
            disabled={busy}
            title="Gerar etiquetas com PDF"
          >
            📦 Gerar etiquetas
          </button>

          <button
            type="button"
            className="szv2-btn-secondary"
            onClick={() => handleGenerate('no_pdf')}
            disabled={busy}
            title="Gerar etiquetas sem PDF (somente dados)"
          >
            📄 Gerar sem PDF
          </button>

          <button
            type="button"
            className="szv2-btn-secondary"
            onClick={() => handleGenerate('print_batch')}
            disabled={busy}
            title="Imprimir etiquetas em lote"
          >
            🖨️ Imprimir lote
          </button>

          <button
            type="button"
            className="szv2-btn-secondary"
            onClick={() => setSelected(new Set())}
            disabled={busy}
            style={{ marginLeft: 4 }}
          >
            Limpar seleção
          </button>
        </div>
      )}
    </div>
  )
}
