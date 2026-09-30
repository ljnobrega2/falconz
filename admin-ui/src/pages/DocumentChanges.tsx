// DocumentChanges — aprovação de troca de documento CPF ⇄ CNPJ (FEAT-DOC-CHANGE-2026-07-03).
//
// O titular (produtor/afiliado/cliente) solicita a troca pelo portal; aqui o admin
// aprova ou rejeita. Ao aprovar, o serviço admin atualiza senderzz_portal_users.nome
// (razão social / nome civil) + document, e o novo nome passa a valer em todo o site.
// O anexo do cartão CNPJ é PII — baixado por endpoint autenticado (Authorization).
import { FormEvent, useEffect, useState } from 'react'
import { useToast } from '../hooks/useToast'
import { api, BASE, getToken } from '../api'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'
import FalkSelect from '../components/FalkSelect'

type Item = {
  id: number
  user_id: number
  user_email: string
  user_role: string
  direction: 'cpf_to_cnpj' | 'cnpj_to_cpf'
  target_nome: string
  target_document: string
  status: 'pending' | 'approved' | 'rejected'
  has_attachment: boolean
  old_nome: string
  old_document: string
  notes: string | null
  created_at: string
  reviewed_at: string | null
}

type StatusFilter = '' | 'pending' | 'approved' | 'rejected'
const STATUS_LABELS: Record<StatusFilter, string> = {
  '': 'Todos', pending: 'Pendentes', approved: 'Aprovados', rejected: 'Rejeitados',
}

function fmtDoc(raw: string | null): string {
  const d = (raw || '').replace(/\D/g, '')
  if (d.length === 11) return d.replace(/(\d{3})(\d{3})(\d{3})(\d{2})/, '$1.$2.$3-$4')
  if (d.length === 14) return d.replace(/(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})/, '$1.$2.$3/$4-$5')
  return raw || '—'
}

function dirLabel(d: string) {
  return d === 'cpf_to_cnpj' ? 'CPF → CNPJ' : 'CNPJ → CPF'
}

function statusBadge(s: string) {
  if (s === 'approved') return <span className="sz-badge szv2-badge-success">Aprovado</span>
  if (s === 'rejected') return <span className="sz-badge szv2-badge-danger">Rejeitado</span>
  return <span className="sz-badge szv2-badge-neutral">Pendente</span>
}

export default function DocumentChanges() {
  const [items, setItems] = useState<Item[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('pending')
  const showToast = useToast()

  const [approveFor, setApproveFor] = useState<Item | null>(null)
  const [approveNotes, setApproveNotes] = useState('')
  const [rejectFor, setRejectFor] = useState<Item | null>(null)
  const [rejectNotes, setRejectNotes] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const p = new URLSearchParams()
      if (statusFilter) p.set('status', statusFilter)
      p.set('limit', '200')
      const r = await api<{ items: Item[]; count: number }>(`/document-changes?${p.toString()}`)
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [statusFilter])

  // Baixa o anexo (PII) com o token no header — depois abre/baixa o blob.
  async function downloadAttachment(it: Item) {
    try {
      const res = await fetch(`${BASE}/document-changes/${it.id}/attachment`, {
        headers: { Authorization: `Bearer ${getToken() || ''}` },
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `cartao-cnpj-${it.id}`
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 5000)
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao baixar o anexo.')
    }
  }

  async function submitApprove(e: FormEvent) {
    e.preventDefault()
    if (!approveFor) return
    setBusy(true)
    try {
      await api(`/document-changes/${approveFor.id}/approve`, {
        method: 'POST',
        body: JSON.stringify({ notes: approveNotes.trim() }),
      })
      setApproveFor(null)
      setApproveNotes('')
      showToast('ok', 'Troca aprovada — o novo documento já vale em todo o site.')
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao aprovar')
    } finally {
      setBusy(false)
    }
  }

  async function submitReject(e: FormEvent) {
    e.preventDefault()
    if (!rejectFor) return
    if (!rejectNotes.trim()) {
      showToast('err', 'Motivo é obrigatório.')
      return
    }
    setBusy(true)
    try {
      await api(`/document-changes/${rejectFor.id}/reject`, {
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
          <h1>Troca de documento (CPF ⇄ CNPJ)</h1>
          <p>{items.length} solicitação(ões){pendingCount > 0 ? ` — ${pendingCount} pendente(s)` : ''}</p>
        </div>
        <div style={{ width: 200 }}>
          <FalkSelect
            aria-label="Status"
            value={statusFilter}
            onChange={v => setStatusFilter(v as StatusFilter)}
            options={(Object.keys(STATUS_LABELS) as StatusFilter[]).map(k => ({ value: k, label: STATUS_LABELS[k] }))}
          />
        </div>
      </div>

      {err && items.length > 0 && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {loading && items.length === 0 ? (
        <TableSkeleton rows={5} cols={7} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={load} />
      ) : !loading && items.length === 0 ? (
        <EmptyState icon="🪪" title="Sem solicitações." description="Nenhuma troca de documento neste filtro." />
      ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Usuário</th>
                <th>Troca</th>
                <th>Novo nome</th>
                <th>Novo documento</th>
                <th>Anexo</th>
                <th>Status</th>
                <th style={{ textAlign: 'right' }}>Ações</th>
              </tr>
            </thead>
            <tbody>
              {items.map(it => (
                <tr key={it.id}>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{it.id}</td>
                  <td>
                    <div style={{ fontSize: 13 }}>{it.user_email || `user #${it.user_id}`}</div>
                    <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{it.user_role}</div>
                  </td>
                  <td style={{ fontSize: 12 }}>{dirLabel(it.direction)}</td>
                  <td style={{ fontWeight: 600 }}>{it.target_nome}</td>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>{fmtDoc(it.target_document)}</td>
                  <td>
                    {it.has_attachment ? (
                      <button className="szv2-btn szv2-btn-sm szv2-btn-secondary" onClick={() => downloadAttachment(it)}>
                        Baixar
                      </button>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>—</span>
                    )}
                  </td>
                  <td>{statusBadge(it.status)}</td>
                  <td style={{ textAlign: 'right' }}>
                    {it.status === 'pending' && (
                      <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-brand"
                          disabled={busy}
                          onClick={() => { setApproveFor(it); setApproveNotes('') }}
                        >
                          Aprovar
                        </button>
                        <button
                          className="szv2-btn szv2-btn-sm szv2-btn-danger"
                          disabled={busy}
                          onClick={() => { setRejectFor(it); setRejectNotes('') }}
                        >
                          Rejeitar
                        </button>
                      </div>
                    )}
                    {it.status === 'rejected' && it.notes && (
                      <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }} title={it.notes}>motivo ⓘ</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Modal: Aprovar */}
      {approveFor && (
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>Aprovar troca #{approveFor.id}</h2>
              <p className="szv2-card-sub">
                {dirLabel(approveFor.direction)} — <strong>{approveFor.target_nome}</strong> ({fmtDoc(approveFor.target_document)}).
                Isto atualiza o nome + documento do usuário em todo o site e reseta a chave PIX (para reconfiguração).
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
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setApproveFor(null)}>
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
              <h2>Rejeitar troca #{rejectFor.id}</h2>
              <p className="szv2-card-sub">Marca como rejeitada — informe o motivo (visível ao usuário).</p>
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
                placeholder="Ex.: cartão CNPJ ilegível, razão social divergente…"
              />
            </div>
            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-danger" disabled={busy}>
                {busy ? 'Rejeitando…' : 'Confirmar rejeição'}
              </button>
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setRejectFor(null)}>
                Cancelar
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
