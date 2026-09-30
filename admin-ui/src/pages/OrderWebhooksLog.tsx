// Tela "Expedição › Histórico Webhooks" — dono pediu pra parar de precisar
// perguntar pro produtor se o webhook de status de pedido disparou. Mostra:
//   1. resumo por status (quantos eventos, quantos enviados/pendentes)
//   2. últimas tentativas reais de entrega (produtor, URL, evento, resposta)
//
// Endpoints (handler Go: order_webhooks.go — sistema DIFERENTE do webhook por
// classe de envio em ExpedicaoWebhooks.tsx; esse é o de status de pedido):
//   GET /order-webhooks/outbox-summary
//   GET /order-webhooks/logs?limit=&event_type=&produtor_id=

import { useEffect, useState } from 'react'
import { api } from '../api'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'
import { confirmAsync } from '../components/ConfirmDialog'

type SummaryRow = { event_type: string; total: number; enviados: number; pendentes: number; esgotados: number }
type MismatchRow = {
  wc_order_id: number
  me_shipment_id: string
  label_status: string
  order_status: string
  tracking_code: string
  updated_at: string
}
type LogRow = {
  id: number
  webhook_id: number
  produtor_id: number
  produtor_nome: string
  url: string
  event_type: string
  response_code: number | null
  response_body: string
  created_at: string
}

function fmtDate(s: string) {
  try { return new Date(s).toLocaleString('pt-BR') } catch { return s }
}

export default function OrderWebhooksLog() {
  const [summary, setSummary] = useState<SummaryRow[]>([])
  const [dispatchEnabled, setDispatchEnabled] = useState(false)
  const [logs, setLogs] = useState<LogRow[]>([])
  const [mismatches, setMismatches] = useState<MismatchRow[]>([])
  const [syncingID, setSyncingID] = useState<number | null>(null)
  const [resending, setResending] = useState(false)
  const [resendingExhausted, setResendingExhausted] = useState(false)
  const [busy, setBusy] = useState(true)
  const [err, setErr] = useState<string | null>(null)
  const [eventFilter, setEventFilter] = useState('')

  async function load() {
    setBusy(true)
    setErr(null)
    try {
      const [s, l, m] = await Promise.all([
        api<{ items: SummaryRow[]; dispatch_enabled: boolean }>('/order-webhooks/outbox-summary'),
        api<{ items: LogRow[] }>(`/order-webhooks/logs?limit=100${eventFilter ? `&event_type=${encodeURIComponent(eventFilter)}` : ''}`),
        api<{ mismatches: MismatchRow[] }>('/order-status-sync/mismatches'),
      ])
      setSummary(s.items || [])
      setDispatchEnabled(!!s.dispatch_enabled)
      setLogs(l.items || [])
      setMismatches(m.mismatches || [])
    } catch (e: any) {
      setErr(e?.message || 'erro ao carregar histórico de webhooks')
    } finally {
      setBusy(false)
    }
  }

  async function syncOrder(orderID: number) {
    setSyncingID(orderID)
    try {
      await api(`/order-status-sync/${orderID}/sync`, { method: 'POST' })
      await load()
    } catch (e: any) {
      setErr(e?.message || `erro ao sincronizar pedido #${orderID}`)
    } finally {
      setSyncingID(null)
    }
  }

  async function resendStale() {
    const totalPendentes = summary.reduce((acc, r) => acc + r.pendentes, 0)
    const ok = await confirmAsync({
      title: 'Reenviar webhooks pendentes travados',
      message: `Isso vai disparar de verdade até ${totalPendentes} webhook(s) travados (>2h) pros produtores cadastrados, no próximo minuto. Confirma?`,
      confirmLabel: 'Reenviar agora',
      cancelLabel: 'Cancelar',
      danger: true,
    })
    if (!ok) return
    setResending(true)
    try {
      const r = await api<{ ok: boolean; renovados: number }>('/order-webhooks/resend-stale', { method: 'POST' })
      await load()
      setErr(null)
      alert(`${r.renovados} evento(s) renovado(s) — serão entregues no próximo ciclo do dispatcher (até 1 min).`)
    } catch (e: any) {
      setErr(e?.message || 'erro ao reenviar pendentes travados')
    } finally {
      setResending(false)
    }
  }

  // AUDIT-2026-07-31: esgotado (6 tentativas) nunca mais é pego por resendStale
  // (esse só renova quem ainda tem attempts < max_attempts) — ficava travado
  // pra sempre, sem alerta. Ação separada zera attempts também.
  async function resendExhausted() {
    const totalEsgotados = summary.reduce((acc, r) => acc + r.esgotados, 0)
    const ok = await confirmAsync({
      title: 'Reenviar webhooks ESGOTADOS (desistiram após 6 tentativas)',
      message: `Isso vai reiniciar do zero até ${totalEsgotados} webhook(s) que já esgotaram todas as tentativas e nunca mais seriam entregues sozinhos. Confirma?`,
      confirmLabel: 'Reenviar agora',
      cancelLabel: 'Cancelar',
      danger: true,
    })
    if (!ok) return
    setResendingExhausted(true)
    try {
      const r = await api<{ ok: boolean; renovados: number }>('/order-webhooks/resend-exhausted', { method: 'POST' })
      await load()
      setErr(null)
      alert(`${r.renovados} evento(s) esgotado(s) reiniciado(s) — serão entregues no próximo ciclo do dispatcher (até 1 min).`)
    } catch (e: any) {
      setErr(e?.message || 'erro ao reenviar esgotados')
    } finally {
      setResendingExhausted(false)
    }
  }

  useEffect(() => { load() }, [eventFilter])

  return (
    <div>
      <h2 style={{ marginBottom: 4 }}>Histórico de Webhooks (status de pedido)</h2>
      <p style={{ color: 'var(--szv2-text-muted)', marginBottom: 16, display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
        <span>
          Dispatch geral:{' '}
          <strong style={{ color: dispatchEnabled ? 'var(--szv2-success)' : 'var(--szv2-danger)' }}>
            {dispatchEnabled ? 'LIGADO' : 'DESLIGADO'}
          </strong>
        </span>
        <button className="szv2-btn szv2-btn-secondary szv2-btn-sm" disabled={resending} onClick={resendStale}>
          {resending ? 'Reenviando...' : 'Reenviar pendentes travados (>2h)'}
        </button>
        {summary.some((r) => r.esgotados > 0) && (
          <button
            className="szv2-btn szv2-btn-sm"
            style={{ background: 'var(--szv2-danger)', color: '#fff' }}
            disabled={resendingExhausted}
            onClick={resendExhausted}
          >
            {resendingExhausted ? 'Reenviando...' : `⚠ Reenviar ESGOTADOS (${summary.reduce((a, r) => a + r.esgotados, 0)})`}
          </button>
        )}
      </p>

      <h3 style={{ marginBottom: 8 }}>Resumo por status</h3>
      {busy && summary.length === 0 ? (
        <TableSkeleton rows={4} cols={4} />
      ) : summary.length === 0 ? (
        <EmptyState title="Nenhum evento de webhook registrado ainda." />
      ) : (
        <table className="szv2-table" style={{ marginBottom: 24 }}>
          <thead>
            <tr>
              <th>Status</th>
              <th>Total de eventos</th>
              <th>Enviados</th>
              <th>Pendentes</th>
              <th>Esgotados</th>
            </tr>
          </thead>
          <tbody>
            {summary.map((r) => (
              <tr key={r.event_type}>
                <td>{r.event_type.replace('order_status_', '')}</td>
                <td>{r.total}</td>
                <td>{r.enviados}</td>
                <td style={{ color: r.pendentes > 0 ? 'var(--szv2-warning)' : undefined }}>{r.pendentes}</td>
                <td style={{ color: r.esgotados > 0 ? 'var(--szv2-danger)' : undefined, fontWeight: r.esgotados > 0 ? 600 : undefined }}>
                  {r.esgotados}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h3 style={{ marginBottom: 8 }}>Pedidos dessincronizados (Melhor Envio)</h3>
      <p style={{ color: 'var(--szv2-text-muted)', marginBottom: 12 }}>
        Etiqueta já atualizou na ME (rastreio/entrega) mas o status do pedido ainda não acompanhou —
        normalmente webhook da ME não chegou. Clique em Sincronizar pra forçar consulta direta na ME.
      </p>
      {mismatches.length === 0 && !busy ? (
        <EmptyState title="Nenhum pedido dessincronizado." description="Sync automático via webhook está em dia." />
      ) : (
        <table className="szv2-table" style={{ marginBottom: 24 }}>
          <thead>
            <tr>
              <th>Pedido</th>
              <th>Status etiqueta (ME)</th>
              <th>Status pedido</th>
              <th>Rastreio</th>
              <th>Atualizado</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {mismatches.map((m) => (
              <tr key={m.wc_order_id}>
                <td>#{m.wc_order_id}</td>
                <td>{m.label_status}</td>
                <td style={{ color: 'var(--szv2-warning)' }}>{m.order_status}</td>
                <td>{m.tracking_code || '—'}</td>
                <td>{fmtDate(m.updated_at)}</td>
                <td>
                  <button
                    className="szv2-btn szv2-btn-sm"
                    disabled={syncingID === m.wc_order_id}
                    onClick={() => syncOrder(m.wc_order_id)}
                  >
                    {syncingID === m.wc_order_id ? 'Sincronizando...' : 'Sincronizar'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h3 style={{ marginBottom: 8 }}>Últimas entregas</h3>
      <div style={{ marginBottom: 12 }}>
        <input
          className="szv2-input"
          placeholder="Filtrar por event_type (ex: order_status_enviado)"
          value={eventFilter}
          onChange={(e) => setEventFilter(e.target.value)}
          style={{ maxWidth: 320 }}
        />
      </div>

      {err && <ErrorState message={err} onRetry={load} />}
      {busy ? (
        <TableSkeleton rows={8} cols={6} />
      ) : logs.length === 0 ? (
        <EmptyState title="Nenhuma entrega registrada ainda" description="Dispatch pode estar desligado ou sem integrações ativas." />
      ) : (
        <table className="szv2-table">
          <thead>
            <tr>
              <th>Data</th>
              <th>Produtor</th>
              <th>URL</th>
              <th>Evento</th>
              <th>Resposta</th>
              <th>Corpo</th>
            </tr>
          </thead>
          <tbody>
            {logs.map((l) => (
              <tr key={l.id}>
                <td>{fmtDate(l.created_at)}</td>
                <td>{l.produtor_nome || `#${l.produtor_id}`}</td>
                <td style={{ maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{l.url}</td>
                <td>{l.event_type.replace('order_status_', '')}</td>
                <td style={{ color: l.response_code && l.response_code < 300 ? 'var(--szv2-success)' : 'var(--szv2-danger)' }}>
                  {l.response_code ?? 'erro'}
                </td>
                <td style={{ maxWidth: 280, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={l.response_body}>
                  {l.response_body}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
