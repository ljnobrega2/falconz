import { FormEvent, useEffect, useState } from 'react'
import { api } from '../api'

type Request = {
  id: number
  nome: string
  email: string
  document: string | null
  telefone: string | null
  empresa: string | null
  status: 'pending' | 'approved' | 'rejected'
  token: string
  created_at: string
  approved_at: string | null
  notes: string | null
}

type StatusFilter = '' | 'pending' | 'approved' | 'rejected'

const STATUS_LABELS: Record<StatusFilter, string> = {
  '':         'Todos',
  pending:    'Pendentes',
  approved:   'Aprovados',
  rejected:   'Rejeitados',
}

// Formata CPF como XXX.XXX.XXX-XX (espelha sz_onboarding_format_cpf).
function fmtCPF(raw: string | null): string {
  if (!raw) return '—'
  const d = raw.replace(/\D+/g, '')
  if (d.length !== 11) return raw
  return `${d.slice(0, 3)}.${d.slice(3, 6)}.${d.slice(6, 9)}-${d.slice(9, 11)}`
}

function fmtDate(raw: string | null): string {
  if (!raw) return '—'
  // YYYY-MM-DD HH:MM:SS… → DD/MM/YYYY
  const d = raw.slice(0, 10)
  const [y, m, day] = d.split('-')
  return y && m && day ? `${day}/${m}/${y}` : raw
}

function statusBadge(status: string) {
  if (status === 'approved') return <span className="sz-badge szv2-badge-success">Aprovado</span>
  if (status === 'rejected') return <span className="sz-badge szv2-badge-danger">Rejeitado</span>
  return <span className="sz-badge szv2-badge-neutral">Pendente</span>
}

const emptyCreate = () => ({
  nome:     '',
  email:    '',
  document: '',
  telefone: '',
  empresa:  '',
})

export default function OnboardingRequests() {
  const [items, setItems] = useState<Request[]>([])
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('')
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  // Modais.
  const [showCreate, setShowCreate] = useState(false)
  const [createForm, setCreateForm] = useState(emptyCreate())
  const [createErr, setCreateErr] = useState('')

  const [detail, setDetail] = useState<Request | null>(null)

  const [rejectFor, setRejectFor] = useState<Request | null>(null)
  const [rejectNotes, setRejectNotes] = useState('')

  const [approveFor, setApproveFor] = useState<Request | null>(null)
  const [approveNotes, setApproveNotes] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = statusFilter ? `?status=${statusFilter}&limit=200` : '?limit=200'
      const r = await api<{ items: Request[]; count: number }>(`/onboarding/requests${qs}`)
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [statusFilter])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  // ── Criar ────────────────────────────────────────────────────────────────
  async function submitCreate(e: FormEvent) {
    e.preventDefault()
    setCreateErr('')
    setBusy(true)
    try {
      await api('/onboarding/requests', {
        method: 'POST',
        body: JSON.stringify({
          nome:     createForm.nome.trim(),
          email:    createForm.email.trim().toLowerCase(),
          document: createForm.document.replace(/\D+/g, ''),
          telefone: createForm.telefone.trim(),
          empresa:  createForm.empresa.trim(),
        }),
      })
      setShowCreate(false)
      setCreateForm(emptyCreate())
      showToast('ok', 'Solicitação criada com sucesso.')
      await load()
    } catch (e: any) {
      setCreateErr(e.message || 'Erro ao criar')
    } finally {
      setBusy(false)
    }
  }

  // ── Aprovar ──────────────────────────────────────────────────────────────
  async function submitApprove(e: FormEvent) {
    e.preventDefault()
    if (!approveFor) return
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; portal_user_id: number; class_id: number; email_pending?: boolean }>(
        `/onboarding/requests/${approveFor.id}/approve`,
        { method: 'POST', body: JSON.stringify({ notes: approveNotes.trim() }) }
      )
      setApproveFor(null)
      setApproveNotes('')
      const classInfo = r.class_id ? ` Classe de frete #${r.class_id} criada.` : ' (classe de frete não criada — tabela ausente).'
      const emailInfo = r.email_pending ? ' E-mail de boas-vindas pendente — enviar manualmente ou via WordPress.' : ''
      showToast('ok', `Solicitação aprovada. Portal user #${r.portal_user_id} criado.${classInfo}${emailInfo}`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao aprovar')
    } finally {
      setBusy(false)
    }
  }

  // ── Rejeitar ─────────────────────────────────────────────────────────────
  async function submitReject(e: FormEvent) {
    e.preventDefault()
    if (!rejectFor) return
    if (!rejectNotes.trim()) {
      showToast('err', 'Motivo é obrigatório.')
      return
    }
    setBusy(true)
    try {
      await api(`/onboarding/requests/${rejectFor.id}/reject`, {
        method: 'POST',
        body: JSON.stringify({ notes: rejectNotes.trim() }),
      })
      setRejectFor(null)
      setRejectNotes('')
      showToast('ok', 'Solicitação rejeitada.')
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao rejeitar')
    } finally {
      setBusy(false)
    }
  }

  const pendingCount = items.filter(x => x.status === 'pending').length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Onboarding</h1>
          <p>{items.length} solicitação(ões){pendingCount > 0 ? ` — ${pendingCount} pendente(s)` : ''}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <select
            value={statusFilter}
            onChange={e => setStatusFilter(e.target.value as StatusFilter)}
            className="szv2-select"
            disabled={busy || loading}
          >
            {(Object.keys(STATUS_LABELS) as StatusFilter[]).map(k => (
              <option key={k} value={k}>{STATUS_LABELS[k]}</option>
            ))}
          </select>
          <button
            className="szv2-btn szv2-btn-brand"
            onClick={() => { setCreateForm(emptyCreate()); setCreateErr(''); setShowCreate(true) }}
          >
            + Novo cadastro
          </button>
        </div>
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

      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Nome</th>
              <th>E-mail</th>
              <th>CPF</th>
              <th>Telefone</th>
              <th>Empresa</th>
              <th>Status</th>
              <th>Data</th>
              <th style={{ textAlign: 'right' }}>Ações</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={9}>
                <div style={{ padding: 32, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
                  Carregando…
                </div>
              </td></tr>
            ) : items.length === 0 ? (
              <tr><td colSpan={9}>
                <div className="szv2-empty">
                  <h3>Nenhum cadastro pendente.</h3>
                  <p>Use "+ Novo cadastro" para criar manualmente, ou aguarde solicitações públicas.</p>
                </div>
              </td></tr>
            ) : items.map(req => (
              <tr key={req.id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{req.id}</td>
                <td style={{ fontWeight: 600 }}>{req.nome}</td>
                <td style={{ fontSize: 13 }}>{req.email}</td>
                <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>{fmtCPF(req.document)}</td>
                <td style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>{req.telefone || '—'}</td>
                <td style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>{req.empresa || '—'}</td>
                <td>{statusBadge(req.status)}</td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>{fmtDate(req.created_at)}</td>
                <td style={{ textAlign: 'right' }}>
                  <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                    <button
                      className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                      onClick={() => setDetail(req)}
                    >
                      Detalhes
                    </button>
                    {req.status === 'pending' && (
                      <>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-brand"
                          disabled={busy}
                          onClick={() => { setApproveFor(req); setApproveNotes('') }}
                        >
                          Aprovar
                        </button>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-danger"
                          disabled={busy}
                          onClick={() => { setRejectFor(req); setRejectNotes('') }}
                        >
                          Rejeitar
                        </button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Modal: Novo cadastro */}
      {showCreate && (
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div><h2>Novo cadastro</h2></div>
            <button className="szv2-modal-x" onClick={() => setShowCreate(false)}>✕</button>
          </div>
          {createErr && (
            <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{createErr}</div>
          )}
          <form onSubmit={submitCreate}>
            <div className="sz-form-grid sz-form-grid-2" style={{ marginBottom: 16 }}>
              <div className="szv2-field">
                <label className="szv2-label">Nome *</label>
                <input
                  className="szv2-input"
                  required
                  value={createForm.nome}
                  onChange={e => setCreateForm({ ...createForm, nome: e.target.value })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">E-mail *</label>
                <input
                  className="szv2-input"
                  type="email"
                  required
                  value={createForm.email}
                  onChange={e => setCreateForm({ ...createForm, email: e.target.value })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">CPF *</label>
                <input
                  className="szv2-input"
                  required
                  placeholder="000.000.000-00"
                  value={createForm.document}
                  onChange={e => setCreateForm({ ...createForm, document: e.target.value })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Telefone</label>
                <input
                  className="szv2-input"
                  placeholder="(11) 99999-9999"
                  value={createForm.telefone}
                  onChange={e => setCreateForm({ ...createForm, telefone: e.target.value })}
                />
              </div>
              <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
                <label className="szv2-label">Empresa</label>
                <input
                  className="szv2-input"
                  value={createForm.empresa}
                  onChange={e => setCreateForm({ ...createForm, empresa: e.target.value })}
                />
              </div>
            </div>
            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={busy}>
                {busy ? 'Salvando…' : 'Criar solicitação'}
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                onClick={() => setShowCreate(false)}
              >
                Cancelar
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Modal: Detalhes */}
      {detail && (
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Solicitação #{detail.id}</h2>
              <p className="szv2-card-sub">{statusBadge(detail.status)}</p>
            </div>
            <button className="szv2-modal-x" onClick={() => setDetail(null)}>✕</button>
          </div>
          <div className="sz-form-grid sz-form-grid-2" style={{ marginBottom: 16 }}>
            <div className="szv2-field">
              <label className="szv2-label">Nome</label>
              <div style={{ padding: '8px 0', fontWeight: 600 }}>{detail.nome}</div>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">E-mail</label>
              <div style={{ padding: '8px 0' }}>{detail.email}</div>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">CPF</label>
              <div style={{ padding: '8px 0', fontFamily: 'var(--szv2-font-mono)' }}>{fmtCPF(detail.document)}</div>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Telefone</label>
              <div style={{ padding: '8px 0' }}>{detail.telefone || '—'}</div>
            </div>
            <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
              <label className="szv2-label">Empresa</label>
              <div style={{ padding: '8px 0' }}>{detail.empresa || '—'}</div>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Criado em</label>
              <div style={{ padding: '8px 0', fontSize: 13 }}>{detail.created_at}</div>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Aprovado em</label>
              <div style={{ padding: '8px 0', fontSize: 13 }}>{detail.approved_at || '—'}</div>
            </div>
            {detail.notes && (
              <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
                <label className="szv2-label">Notas</label>
                <div style={{ padding: '8px 0', fontSize: 13, whiteSpace: 'pre-wrap' }}>{detail.notes}</div>
              </div>
            )}
          </div>
          <div className="sz-form-actions">
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary"
              onClick={() => setDetail(null)}
            >
              Fechar
            </button>
          </div>
        </div>
      )}

      {/* Modal: Aprovar */}
      {approveFor && (
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Aprovar solicitação #{approveFor.id}</h2>
              <p className="szv2-card-sub">
                Cria portal_user e classe de frete para <strong>{approveFor.email}</strong>. Aplica markup padrão.
                E-mail de boas-vindas deve ser enviado manualmente.
              </p>
            </div>
            <button className="szv2-modal-x" onClick={() => setApproveFor(null)}>✕</button>
          </div>
          <form onSubmit={submitApprove}>
            <div className="szv2-field" style={{ marginBottom: 16 }}>
              <label className="szv2-label">Notas (opcional)</label>
              <textarea
                className="szv2-input"
                rows={3}
                value={approveNotes}
                onChange={e => setApproveNotes(e.target.value)}
                placeholder="Observações sobre a aprovação…"
              />
            </div>
            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={busy}>
                {busy ? 'Aprovando…' : 'Confirmar aprovação'}
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                onClick={() => setApproveFor(null)}
              >
                Cancelar
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Modal: Rejeitar */}
      {rejectFor && (
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Rejeitar solicitação #{rejectFor.id}</h2>
              <p className="szv2-card-sub">
                Marca como rejeitada — informe o motivo.
              </p>
            </div>
            <button className="szv2-modal-x" onClick={() => setRejectFor(null)}>✕</button>
          </div>
          <form onSubmit={submitReject}>
            <div className="szv2-field" style={{ marginBottom: 16 }}>
              <label className="szv2-label">Motivo *</label>
              <textarea
                className="szv2-input"
                required
                rows={3}
                value={rejectNotes}
                onChange={e => setRejectNotes(e.target.value)}
                placeholder="Ex.: dados inválidos, e-mail corporativo não verificado…"
              />
            </div>
            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-danger" disabled={busy}>
                {busy ? 'Rejeitando…' : 'Confirmar rejeição'}
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                onClick={() => setRejectFor(null)}
              >
                Cancelar
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
