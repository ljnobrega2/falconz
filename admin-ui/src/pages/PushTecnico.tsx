import { useEffect, useState } from 'react'
import { api } from '../api'

// ----- tipos ---------------------------------------------------------------

type PushStatus = {
  vapid_public: string
  vapid_configured: boolean
  device_count: number
  log_count: number
  env_managed: boolean
}

type NotifLogItem = {
  id: number
  user_id: number
  display_name: string // nome do portal_user ou "user #ID"
  event: string
  recipient_type: string
  order_id: number | null
  status: string
  http_code: number
  error_msg: string
  created_at: string
}

// ----- helpers UI ----------------------------------------------------------

function KpiCard({ label, value, sub, ok }: { label: string; value: number | string; sub?: string; ok?: boolean }) {
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span
          className="szv2-kpi-value"
          style={{ color: ok === false ? 'var(--szv2-danger)' : 'var(--szv2-brand)' }}
        >
          {value}
        </span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const danger = ['err', 'error', 'failed'].includes(status)
  const warn = ['queued', 'manual_test'].includes(status)
  const cls = danger
    ? 'sz-badge szv2-badge-danger'
    : warn
    ? 'sz-badge'
    : 'sz-badge szv2-badge-ok'
  return <span className={cls}>{status}</span>
}

function fmtDate(iso: string) {
  try {
    return new Date(iso).toLocaleString('pt-BR')
  } catch {
    return iso
  }
}

// ----- componente principal ------------------------------------------------

export default function PushTecnico() {
  const [status, setStatus] = useState<PushStatus | null>(null)
  const [logs, setLogs] = useState<NotifLogItem[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [busy, setBusy] = useState(false)

  // --- VAPID regenerar
  const [confirmInput, setConfirmInput] = useState('')
  const [showConfirm, setShowConfirm] = useState(false)

  // --- Teste de envio
  const [testUserId, setTestUserId] = useState('')
  const [testTitle, setTestTitle] = useState('')
  const [testBody, setTestBody] = useState('')

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const s = await api<PushStatus>('/push-tecnico/status')
      setStatus(s)
      const l = await api<{ items: NotifLogItem[] }>('/push-tecnico/logs?limit=25')
      setLogs(l.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function handleRegenerate() {
    if (confirmInput !== 'REGENERAR') {
      showToast('err', 'Digite exatamente "REGENERAR" para confirmar.')
      return
    }
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; vapid_public_masked: string }>(
        '/push-tecnico/regenerate-vapid',
        { method: 'POST', body: JSON.stringify({ confirm: confirmInput }) },
      )
      showToast('ok', `VAPID regenerado. Chave pública: ${r.vapid_public_masked}`)
      setConfirmInput('')
      setShowConfirm(false)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao regenerar VAPID')
    } finally {
      setBusy(false)
    }
  }

  async function handleTestSend() {
    const uid = parseInt(testUserId, 10)
    if (!uid || uid <= 0) { showToast('err', 'Informe um user_id válido'); return }
    if (!testTitle.trim()) { showToast('err', 'Informe um título'); return }
    setBusy(true)
    try {
      await api('/push-tecnico/test-send', {
        method: 'POST',
        body: JSON.stringify({ user_id: uid, title: testTitle, body: testBody }),
      })
      showToast('ok', 'Notificação de teste enfileirada.')
      setTestTitle('')
      setTestBody('')
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao enviar teste')
    } finally {
      setBusy(false)
    }
  }

  async function handleReprocess(id: number) {
    setBusy(true)
    try {
      await api(`/push-tecnico/logs/${id}/reprocess`, { method: 'POST' })
      showToast('ok', `Log #${id} marcado para reprocessamento.`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao reprocessar')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {/* Banner env_managed */}
      {status?.env_managed && (
        <div
          className="sz-alert-info"
          style={{ marginBottom: 16, background: 'var(--szv2-info-bg, #eff6ff)', border: '1px solid #bfdbfe', borderRadius: 8, padding: '12px 16px', color: '#1e40af' }}
        >
          VAPID gerenciado via variável de ambiente — chaves VAPID_PUBLIC / VAPID_PRIVATE definidas no servidor. Alterações pelo painel serão ignoradas enquanto as env vars estiverem ativas.
        </div>
      )}

      {/* KPIs */}
      {status && (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))' }}>
          <KpiCard label="Dispositivos subscritos" value={status.device_count} sub="sz_push_subscriptions" />
          <KpiCard label="Notificações enviadas" value={status.log_count} sub="sz_notif_log" />
          <KpiCard
            label="VAPID configurado"
            value={status.vapid_configured ? 'Sim' : 'Não'}
            sub={status.vapid_configured ? status.vapid_public : 'chave não configurada'}
            ok={status.vapid_configured ? undefined : false}
          />
        </div>
      )}

      {loading && (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando…
        </div>
      )}

      {/* Card VAPID */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Chaves VAPID</h2>
            <p className="szv2-card-sub">
              Chave pública usada para assinar notificações Web Push (RFC 8292 / prime256v1).
            </p>
          </div>
          {!showConfirm && (
            <button
              type="button"
              className="szv2-btn-danger"
              onClick={() => setShowConfirm(true)}
              disabled={busy || status?.env_managed}
            >
              Regenerar VAPID
            </button>
          )}
        </div>

        {status && (
          <div style={{ padding: '0 0 8px' }}>
            <label className="szv2-field-label" style={{ display: 'block', marginBottom: 4 }}>
              Chave pública atual (truncada)
            </label>
            <input
              readOnly
              value={status.vapid_public || '(não configurada)'}
              className="szv2-input"
              style={{ fontFamily: 'monospace', width: '100%' }}
            />
          </div>
        )}

        {showConfirm && (
          <div style={{ marginTop: 16, padding: 16, background: 'var(--szv2-bg-alt, #fef2f2)', borderRadius: 8, border: '1px solid #fca5a5' }}>
            <p style={{ marginBottom: 8, color: 'var(--szv2-danger)' }}>
              <strong>Atenção:</strong> Regenerar o par VAPID invalida <em>todas</em> as assinaturas push existentes. Dispositivos subscritos precisarão re-assinar.
            </p>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input
                type="text"
                className="szv2-input"
                placeholder='Digite "REGENERAR" para confirmar'
                value={confirmInput}
                onChange={e => setConfirmInput(e.target.value)}
                style={{ flex: 1 }}
                autoComplete="off"
              />
              <button
                type="button"
                className="szv2-btn-danger"
                onClick={handleRegenerate}
                disabled={busy || confirmInput !== 'REGENERAR'}
              >
                {busy ? 'Gerando…' : 'Confirmar'}
              </button>
              <button
                type="button"
                className="szv2-btn-secondary"
                onClick={() => { setShowConfirm(false); setConfirmInput('') }}
                disabled={busy}
              >
                Cancelar
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Card teste de envio */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Enviar notificação de teste</h2>
            <p className="szv2-card-sub">
              Enfileira um push manual para um usuário específico. Registrado em sz_notif_log.
            </p>
          </div>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '160px 1fr 1fr', gap: 12, alignItems: 'end' }}>
          <div>
            <label className="szv2-field-label" style={{ display: 'block', marginBottom: 4 }}>
              User ID
            </label>
            <input
              type="number"
              className="szv2-input"
              value={testUserId}
              onChange={e => setTestUserId(e.target.value)}
              placeholder="ex: 42"
              min={1}
            />
          </div>
          <div>
            <label className="szv2-field-label" style={{ display: 'block', marginBottom: 4 }}>
              Título
            </label>
            <input
              type="text"
              className="szv2-input"
              value={testTitle}
              onChange={e => setTestTitle(e.target.value)}
              placeholder="Título da notificação"
            />
          </div>
          <div>
            <label className="szv2-field-label" style={{ display: 'block', marginBottom: 4 }}>
              Mensagem
            </label>
            <input
              type="text"
              className="szv2-input"
              value={testBody}
              onChange={e => setTestBody(e.target.value)}
              placeholder="Corpo da notificação"
            />
          </div>
        </div>
        <div style={{ marginTop: 12 }}>
          <button
            type="button"
            className="szv2-btn-brand"
            onClick={handleTestSend}
            disabled={busy}
          >
            {busy ? 'Enviando…' : 'Enviar teste'}
          </button>
        </div>
      </div>

      {/* Tabela de log */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Log de notificações</h2>
            <p className="szv2-card-sub">Últimas 25 entradas de sz_notif_log</p>
          </div>
          <button
            type="button"
            className="szv2-btn-secondary"
            onClick={load}
            disabled={loading || busy}
          >
            Atualizar
          </button>
        </div>

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : logs.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhuma entrada no log.
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Data</th>
                  <th>User</th>
                  <th>Evento</th>
                  <th>Destinatário</th>
                  <th>Pedido</th>
                  <th>Status</th>
                  <th>HTTP</th>
                  <th>Erro</th>
                  <th style={{ width: 120 }}>Ação</th>
                </tr>
              </thead>
              <tbody>
                {logs.map(item => (
                  <tr key={item.id}>
                    <td style={{ whiteSpace: 'nowrap' }}>{fmtDate(item.created_at)}</td>
                    <td title={`user_id: ${item.user_id}`}>{item.display_name || `user #${item.user_id}`}</td>
                    <td>{item.event}</td>
                    <td>{item.recipient_type}</td>
                    <td>{item.order_id ?? '—'}</td>
                    <td><StatusBadge status={item.status} /></td>
                    <td>{item.http_code > 0 ? item.http_code : '—'}</td>
                    <td style={{ color: 'var(--szv2-danger)', maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {item.error_msg || '—'}
                    </td>
                    <td>
                      <button
                        type="button"
                        className="szv2-btn-secondary"
                        onClick={() => handleReprocess(item.id)}
                        disabled={busy}
                      >
                        Reprocessar
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
