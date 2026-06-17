// Tela "Expedição › Webhooks" do painel admin.
// Espelha senderzz_pw_admin_page() em includes/senderzz-producer-webhooks.php
// (referência: AUDIT-ADMIN-WP.md §1.4.3).
//
// Endpoints consumidos (handler Go: expedicao_webhooks.go):
//   GET    /expedicao-webhooks               → listagem com agregados
//   POST   /expedicao-webhooks               → criação (segredo gerado pelo back)
//   PUT    /expedicao-webhooks/{id}          → edição parcial
//   DELETE /expedicao-webhooks/{id}          → soft delete (url='', active=false)
//   POST   /expedicao-webhooks/{id}/test     → dispara payload de teste
//   GET    /expedicao-webhooks/{id}/logs     → histórico de execuções
//   POST   /expedicao-webhooks/{id}/reprocess → reprocessa disparo pelo log_id
//   GET    /expedicao-webhooks/sample-payload     → payload-exemplo
//   GET    /expedicao-webhooks/available-events   → enum de eventos
//   GET    /expedicao-webhooks/classes            → lista de classes de envio (para select)

import { useEffect, useState } from 'react'
import { api } from '../api'

// ----- tipos --------------------------------------------------------------

type WebhookRow = {
  id: number
  class_id: number
  class_name: string
  url: string
  events: string[]
  active: boolean
  last_fired_at: string | null
  last_status: number | null
  last_error: string | null
  fail_count_24h: number
  created_at: string
}

type WebhookLog = {
  id: number
  webhook_id: number
  payload: string
  response_code: number | null
  response_body: string | null
  error: string | null
  fired_at: string
  reprocess_count: number
  last_reprocessed_at: string | null
}

type ShippingClass = {
  id: number
  name: string
  slug: string
}

type TestResult = {
  response_code: number
  response_time_ms: number
  response_body?: string
  error?: string
}

type FormState = {
  id: number          // 0 = criação
  class_id: number
  url: string
  events: string[]
  active: boolean
}

const emptyForm = (): FormState => ({
  id: 0,
  class_id: 0,
  url: '',
  events: ['order_status_enviado'],
  active: true,
})

// URL pública mostrada no topo (read-only). Pode ser sobrescrita por env Vite.
const PUBLIC_INBOUND_URL: string =
  (import.meta.env.VITE_PUBLIC_WEBHOOK_URL as string | undefined) ||
  'https://app.senderzz.com.br/wp-json/senderzz/v1/webhook'

// ----- helpers ------------------------------------------------------------

function fmtDate(s: string | null | undefined): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  return d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
}

function truncate(s: string, n: number): string {
  if (!s) return ''
  return s.length > n ? s.slice(0, n) + '…' : s
}

function statusBadge(code: number | null | undefined) {
  if (code == null) return <span className="sz-badge szv2-badge-neutral">—</span>
  if (code >= 200 && code < 300) return <span className="sz-badge szv2-badge-success">{code}</span>
  if (code >= 300 && code < 400) return <span className="sz-badge szv2-badge-warning">{code}</span>
  return <span className="sz-badge szv2-badge-danger">{code}</span>
}

// ----- componente ---------------------------------------------------------

export default function ExpedicaoWebhooks() {
  const [items, setItems] = useState<WebhookRow[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  // form (modal inline)
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState<FormState>(emptyForm())
  const [saving, setSaving] = useState(false)

  // sample payload (collapsed details)
  const [sample, setSample] = useState<string>('')

  // available events (enum vindo do back)
  const [availEvents, setAvailEvents] = useState<string[]>([])

  // classes de envio para o select do formulário
  const [shippingClasses, setShippingClasses] = useState<ShippingClass[]>([])

  // logs drawer
  const [logsFor, setLogsFor] = useState<WebhookRow | null>(null)
  const [logs, setLogs] = useState<WebhookLog[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const [reprocessingLogId, setReprocessingLogId] = useState<number | null>(null)

  // ----- carregamento inicial --------------------------------------------

  async function load() {
    setLoading(true); setErr('')
    try {
      const r = await api<{ items: WebhookRow[] }>('/expedicao-webhooks')
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  async function loadMeta() {
    try {
      const sp = await api<any>('/expedicao-webhooks/sample-payload')
      setSample(JSON.stringify(sp, null, 2))
    } catch {/* graceful */}
    try {
      const ev = await api<{ items: string[] }>('/expedicao-webhooks/available-events')
      setAvailEvents(ev.items || [])
    } catch {/* graceful */}
    try {
      const cl = await api<{ classes: ShippingClass[] }>('/expedicao-webhooks/classes')
      setShippingClasses(cl.classes || [])
    } catch {/* graceful */}
  }

  useEffect(() => { load(); loadMeta() }, [])

  // ----- toast helper ----------------------------------------------------

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  // ----- ações sobre items -----------------------------------------------

  function openCreate() {
    setForm(emptyForm())
    setShowForm(true)
  }

  function openEdit(row: WebhookRow) {
    setForm({
      id: row.id,
      class_id: row.class_id,
      url: row.url,
      events: [...row.events],
      active: row.active,
    })
    setShowForm(true)
  }

  function toggleEvent(name: string) {
    setForm(f => ({
      ...f,
      events: f.events.includes(name)
        ? f.events.filter(e => e !== name)
        : [...f.events, name],
    }))
  }

  async function save(e: React.FormEvent) {
    e.preventDefault()
    if (!/^https?:\/\//i.test(form.url)) {
      showToast('err', 'URL precisa começar com http:// ou https://')
      return
    }
    if (form.events.length === 0) {
      showToast('err', 'Selecione pelo menos um evento')
      return
    }
    setSaving(true)
    try {
      const body = JSON.stringify({
        class_id: form.class_id,
        url: form.url,
        events: form.events,
        active: form.active,
      })
      if (form.id > 0) {
        await api(`/expedicao-webhooks/${form.id}`, { method: 'PUT', body })
        showToast('ok', `Webhook #${form.id} atualizado`)
      } else {
        await api('/expedicao-webhooks', { method: 'POST', body })
        showToast('ok', 'Webhook criado (segredo HMAC gerado)')
      }
      setShowForm(false)
      setForm(emptyForm())
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar')
    } finally {
      setSaving(false)
    }
  }

  async function toggleActive(row: WebhookRow) {
    try {
      await api(`/expedicao-webhooks/${row.id}`, {
        method: 'PUT',
        body: JSON.stringify({ active: !row.active }),
      })
      showToast('ok', `Webhook #${row.id} ${row.active ? 'pausado' : 'ativado'}`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao alternar status')
    }
  }

  async function del(row: WebhookRow) {
    if (!window.confirm(
      `Excluir webhook #${row.id}?\n\nURL: ${row.url}\n\nA exclusão é soft (limpa URL e desativa).`,
    )) return
    try {
      await api(`/expedicao-webhooks/${row.id}`, { method: 'DELETE' })
      showToast('ok', `Webhook #${row.id} excluído`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao excluir')
    }
  }

  async function test(row: WebhookRow) {
    try {
      const r = await api<TestResult>(`/expedicao-webhooks/${row.id}/test`, { method: 'POST' })
      if (r.error) {
        showToast('err', `Teste falhou: ${r.error} (${r.response_time_ms}ms)`)
      } else {
        const ok = r.response_code >= 200 && r.response_code < 300
        showToast(ok ? 'ok' : 'err',
          `HTTP ${r.response_code} em ${r.response_time_ms}ms${ok ? '' : ' — verifique o destino'}`)
      }
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao testar')
    }
  }

  async function reprocessLog(webhookId: number, logId: number) {
    setReprocessingLogId(logId)
    try {
      const r = await api<{ ok: boolean; response_code: number; error?: string }>(
        `/expedicao-webhooks/${webhookId}/reprocess`,
        { method: 'POST', body: JSON.stringify({ log_id: logId }) },
      )
      if (r.ok) {
        showToast('ok', `Reprocessado com sucesso (HTTP ${r.response_code})`)
      } else {
        showToast('err', `Reprocessamento falhou: ${r.error || `HTTP ${r.response_code}`}`)
      }
      // Recarrega logs para atualizar reprocess_count e last_reprocessed_at.
      if (logsFor) {
        const updated = await api<{ items: WebhookLog[] }>(`/expedicao-webhooks/${logsFor.id}/logs?limit=50`)
        setLogs(updated.items || [])
      }
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao reprocessar')
    } finally {
      setReprocessingLogId(null)
    }
  }

  async function openLogs(row: WebhookRow) {
    setLogsFor(row)
    setLogs([])
    setLogsLoading(true)
    try {
      const r = await api<{ items: WebhookLog[] }>(`/expedicao-webhooks/${row.id}/logs?limit=50`)
      setLogs(r.items || [])
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao carregar logs')
    } finally {
      setLogsLoading(false)
    }
  }

  function copyUrl() {
    try {
      navigator.clipboard.writeText(PUBLIC_INBOUND_URL)
      showToast('ok', 'URL copiada para a área de transferência')
    } catch {
      showToast('err', 'Falha ao copiar — copie manualmente')
    }
  }

  // ----- render ----------------------------------------------------------

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Webhooks de Expedição</h1>
          <p>Notifica produtores quando pedidos mudam de status. Configurável por classe de envio.</p>
        </div>
        <button className="szv2-btn szv2-btn-brand" onClick={openCreate}>
          + Adicionar webhook
        </button>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {/* Top bar: URL pública + copiar */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>URL de recebimento</h2>
            <p className="szv2-card-sub">Endpoint público que recebe webhooks externos.</p>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <input
            className="szv2-input"
            value={PUBLIC_INBOUND_URL}
            readOnly
            onFocus={e => e.currentTarget.select()}
            style={{ flex: 1, minWidth: 280, fontFamily: 'var(--szv2-font-mono)', fontSize: 13 }}
          />
          <button className="szv2-btn szv2-btn-secondary" type="button" onClick={copyUrl}>
            Copiar URL
          </button>
        </div>
      </div>

      {/* Payload exemplo */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <details>
          <summary style={{ cursor: 'pointer', fontWeight: 600 }}>
            Payload exemplo (clique para expandir)
          </summary>
          <pre
            style={{
              marginTop: 12,
              padding: 12,
              background: 'var(--szv2-bg-soft, #0e1117)',
              color: 'var(--szv2-text, #c9d1d9)',
              borderRadius: 6,
              fontFamily: 'var(--szv2-font-mono)',
              fontSize: 12,
              overflowX: 'auto',
              maxHeight: 360,
            }}
          >
            {sample || 'Carregando…'}
          </pre>
        </details>
      </div>

      {/* Form inline (criar/editar) */}
      {showForm && (
        <div className="szv2-card" style={{ marginBottom: 24 }}>
          <div className="szv2-card-head">
            <div><h2>{form.id ? `Editar webhook #${form.id}` : 'Novo webhook'}</h2></div>
            <button className="szv2-modal-x" onClick={() => setShowForm(false)}>✕</button>
          </div>
          <form onSubmit={save}>
            <div className="sz-form-grid sz-form-grid-2" style={{ marginBottom: 16 }}>
              <div className="szv2-field">
                <label className="szv2-label">Classe de envio *</label>
                {shippingClasses.length > 0 ? (
                  <select
                    className="szv2-input"
                    required
                    value={form.class_id}
                    onChange={e => setForm({ ...form, class_id: Number(e.target.value) })}
                  >
                    {shippingClasses.map(c => (
                      <option key={c.id} value={c.id}>{c.name}</option>
                    ))}
                  </select>
                ) : (
                  <input
                    className="szv2-input"
                    type="number"
                    min={0}
                    required
                    placeholder="ID da classe (0 = universal)"
                    value={form.class_id}
                    onChange={e => setForm({ ...form, class_id: Number(e.target.value) || 0 })}
                  />
                )}
                <small style={{ color: 'var(--szv2-text-muted)' }}>
                  "Produtos sem classe" (ID 0) = universal.
                </small>
              </div>
              <div className="szv2-field">
                <label className="szv2-label">URL *</label>
                <input
                  className="szv2-input"
                  type="url"
                  required
                  placeholder="https://seu-servico.com/webhook"
                  value={form.url}
                  onChange={e => setForm({ ...form, url: e.target.value })}
                />
              </div>
            </div>

            <div className="szv2-field" style={{ marginBottom: 16 }}>
              <label className="szv2-label">Eventos *</label>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12 }}>
                {(availEvents.length > 0 ? availEvents : form.events).map(ev => (
                  <label
                    key={ev}
                    style={{
                      display: 'flex', alignItems: 'center', gap: 6,
                      padding: '6px 10px',
                      border: '1px solid var(--szv2-border)',
                      borderRadius: 6,
                      cursor: 'pointer',
                      fontSize: 13,
                      background: form.events.includes(ev) ? 'rgba(234,88,12,.10)' : 'transparent',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={form.events.includes(ev)}
                      onChange={() => toggleEvent(ev)}
                    />
                    <code style={{ fontSize: 12 }}>{ev}</code>
                  </label>
                ))}
              </div>
            </div>

            <div style={{ marginBottom: 16 }}>
              <label style={{
                display: 'flex', alignItems: 'center', gap: 8,
                fontSize: 14, cursor: 'pointer', color: 'var(--szv2-text-soft)',
              }}>
                <input
                  type="checkbox"
                  checked={form.active}
                  onChange={e => setForm({ ...form, active: e.target.checked })}
                />
                Ativo (desmarque para pausar sem excluir)
              </label>
            </div>

            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={saving}>
                {saving ? 'Salvando…' : form.id ? 'Atualizar webhook' : 'Criar webhook'}
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                onClick={() => setShowForm(false)}
              >
                Cancelar
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Tabela */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Webhooks configurados</h2>
            <p className="szv2-card-sub">{items.length} webhook(s)</p>
          </div>
        </div>

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : items.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum webhook configurado ainda. Clique em "Adicionar webhook".
          </div>
        ) : (
          <div className="szv2-table-wrap" style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Classe</th>
                  <th>URL</th>
                  <th>Eventos</th>
                  <th>Status</th>
                  <th>Última execução</th>
                  <th>Último erro</th>
                  <th style={{ textAlign: 'right' }}>Falhas 24h</th>
                  <th style={{ textAlign: 'right', width: 280 }}>Ações</th>
                </tr>
              </thead>
              <tbody>
                {items.map(row => (
                  <tr key={row.id}>
                    <td>
                      <div style={{ fontWeight: 600 }}>
                        {row.class_name || `Classe #${row.class_id}`}
                      </div>
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                        ID {row.class_id}
                      </div>
                    </td>
                    <td>
                      <code
                        title={row.url}
                        style={{
                          fontSize: 12,
                          fontFamily: 'var(--szv2-font-mono)',
                          color: 'var(--szv2-text-soft)',
                        }}
                      >
                        {truncate(row.url, 48)}
                      </code>
                    </td>
                    <td>
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                        {row.events.length === 0
                          ? <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>—</span>
                          : row.events.map(ev => (
                              <span key={ev} className="sz-badge szv2-badge-neutral" style={{ fontSize: 11 }}>
                                {ev}
                              </span>
                            ))}
                      </div>
                    </td>
                    <td>
                      {row.active
                        ? <span className="sz-badge szv2-badge-success">Ativo</span>
                        : <span className="sz-badge szv2-badge-warning">Pausado</span>}
                    </td>
                    <td>
                      <div style={{ fontSize: 13 }}>{fmtDate(row.last_fired_at)}</div>
                      <div style={{ marginTop: 2 }}>{statusBadge(row.last_status)}</div>
                    </td>
                    <td>
                      {row.last_error
                        ? (
                          <span
                            title={row.last_error}
                            style={{
                              color: 'var(--szv2-danger)',
                              fontSize: 12,
                              fontFamily: 'var(--szv2-font-mono)',
                            }}
                          >
                            {truncate(row.last_error, 60)}
                          </span>
                        )
                        : <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>—</span>}
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      {row.fail_count_24h > 0
                        ? <span style={{ color: 'var(--szv2-danger)', fontWeight: 600 }}>
                            {row.fail_count_24h}
                          </span>
                        : <span style={{ color: 'var(--szv2-text-muted)' }}>0</span>}
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                          onClick={() => test(row)}
                          title="Dispara payload de teste com HMAC"
                        >
                          Testar
                        </button>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                          onClick={() => openEdit(row)}
                        >
                          Editar
                        </button>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                          onClick={() => openLogs(row)}
                        >
                          Ver logs
                        </button>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                          onClick={() => toggleActive(row)}
                        >
                          {row.active ? 'Pausar' : 'Ativar'}
                        </button>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-danger"
                          onClick={() => del(row)}
                        >
                          Excluir
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Drawer de logs (overlay) */}
      {logsFor && (
        <div
          role="dialog"
          aria-modal="true"
          onClick={() => setLogsFor(null)}
          style={{
            position: 'fixed', inset: 0,
            background: 'rgba(0,0,0,.5)',
            zIndex: 9999,
            display: 'flex', justifyContent: 'flex-end',
          }}
        >
          <div
            onClick={e => e.stopPropagation()}
            style={{
              width: 'min(960px, 100vw)',
              height: '100vh',
              background: 'var(--szv2-bg, #fff)',
              color: 'var(--szv2-text)',
              boxShadow: '-12px 0 24px rgba(0,0,0,.2)',
              overflowY: 'auto',
              padding: 24,
            }}
          >
            <div style={{
              display: 'flex', alignItems: 'center', justifyContent: 'space-between',
              marginBottom: 16,
            }}>
              <div>
                <h2 style={{ margin: 0 }}>
                  Logs do webhook #{logsFor.id}
                </h2>
                <p className="szv2-card-sub" style={{ marginTop: 4 }}>
                  {logsFor.url}
                </p>
              </div>
              <button className="szv2-modal-x" onClick={() => setLogsFor(null)}>✕</button>
            </div>

            {logsLoading ? (
              <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
                Carregando logs…
              </div>
            ) : logs.length === 0 ? (
              <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
                Nenhum disparo registrado ainda.
              </div>
            ) : (
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Data</th>
                    <th>Status</th>
                    <th>Payload (preview)</th>
                    <th>Erro</th>
                    <th style={{ whiteSpace: 'nowrap' }}>Reprocessado</th>
                    <th>Ações</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.map(l => (
                    <tr key={l.id}>
                      <td style={{ whiteSpace: 'nowrap', fontSize: 13 }}>{fmtDate(l.fired_at)}</td>
                      <td>{statusBadge(l.response_code)}</td>
                      <td>
                        <code style={{
                          fontFamily: 'var(--szv2-font-mono)',
                          fontSize: 11,
                          color: 'var(--szv2-text-soft)',
                          whiteSpace: 'pre-wrap',
                          wordBreak: 'break-all',
                        }}>
                          {truncate(l.payload || '', 280)}
                        </code>
                      </td>
                      <td style={{ color: 'var(--szv2-danger)', fontSize: 12 }}>
                        {l.error ? truncate(l.error, 160) : '—'}
                      </td>
                      <td style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
                        {l.reprocess_count > 0 ? (
                          <div>
                            <span style={{ fontWeight: 600 }}>{l.reprocess_count}×</span>
                            {l.last_reprocessed_at && (
                              <div style={{ color: 'var(--szv2-text-muted)', fontSize: 11 }}>
                                {fmtDate(l.last_reprocessed_at)}
                              </div>
                            )}
                          </div>
                        ) : (
                          <span style={{ color: 'var(--szv2-text-muted)' }}>—</span>
                        )}
                      </td>
                      <td>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                          disabled={reprocessingLogId === l.id}
                          onClick={() => logsFor && reprocessLog(logsFor.id, l.id)}
                          title="Reenviar este disparo para o endpoint configurado"
                        >
                          {reprocessingLogId === l.id ? 'Enviando…' : 'Reprocessar'}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
