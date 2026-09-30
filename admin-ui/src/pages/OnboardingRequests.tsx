import { FormEvent, useEffect, useState } from 'react'
import { useToast } from '../hooks/useToast'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'
import BulkBar, { useBulkSelection, runBulk } from '../components/BulkBar'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'

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

// FEAT-RBAC-2026-06-21 — níveis (papéis) canônicos atribuíveis na aprovação.
// O admin escolhe o NÍVEL do usuário ao aprovar; o handler Signup já documenta
// "O admin aprova e DEFINE O NÍVEL (RBAC) do usuário". Default 'produtor' para
// preservar o comportamento atual (approveOne hardcoda role='produtor').
// FEAT-RBAC-2026-06-21 — níveis aprováveis por este fluxo (portal_users + admin).
// motoboy NÃO entra aqui: vive em sz_motoboys (cadastro próprio), não em portal_users.
type Nivel = 'admin' | 'operator' | 'produtor' | 'afiliado' | 'cliente'

const NIVEL_LABELS: Record<Nivel, string> = {
  admin:    'Administrador',
  operator: 'Operador logístico (OL)',
  produtor: 'Produtor',
  afiliado: 'Afiliado',
  cliente:  'Cliente',
}

const NIVEL_DEFAULT: Nivel = 'produtor'

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
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const showToast = useToast() // AUDIT-2026-06-18 Onda3

  // Filtros aplicados.
  const [q, setQ] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftStatus, setDraftStatus] = useState<StatusFilter>('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')

  const [filterOpen, setFilterOpen] = useState(false)

  // Modais.
  const [showCreate, setShowCreate] = useState(false)
  const [createForm, setCreateForm] = useState(emptyCreate())
  const [createErr, setCreateErr] = useState('')

  const [detail, setDetail] = useState<Request | null>(null)

  const [rejectFor, setRejectFor] = useState<Request | null>(null)
  const [rejectNotes, setRejectNotes] = useState('')

  const [approveFor, setApproveFor] = useState<Request | null>(null)
  const [approveNotes, setApproveNotes] = useState('')
  const [approveNivel, setApproveNivel] = useState<Nivel>(NIVEL_DEFAULT) // FEAT-RBAC-2026-06-21

  // Ação em lote (aprovar/rejeitar a seleção com uma única nota/motivo).
  const [bulkKind, setBulkKind] = useState<'approve' | 'reject' | null>(null)
  const [bulkNotes, setBulkNotes] = useState('')
  // FEAT-RBAC-2026-06-21 — nível (papel) aplicado a TODAS as solicitações
  // aprovadas em lote. Sem isto, o lote enviaria só `notes` e cada conta cairia
  // no default do backend (approveOne hardcoda 'produtor'); o select deixa a
  // escolha explícita e consistente com a aprovação individual.
  const [bulkNivel, setBulkNivel] = useState<Nivel>(NIVEL_DEFAULT)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const p = new URLSearchParams()
      if (statusFilter) p.set('status', statusFilter)
      if (q.trim()) p.set('q', q.trim())
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      p.set('limit', '200')
      const r = await api<{ items: Request[]; count: number }>(`/onboarding/requests?${p.toString()}`)
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [q, statusFilter, dataIni, dataFim])


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
        // FEAT-RBAC-2026-06-21 — envia o NÍVEL (papel) na chave `role` (lida por
        // approveOne; whitelist produtor|afiliado|operator|cliente; admin exige senha do cadastro).
        { method: 'POST', body: JSON.stringify({ notes: approveNotes.trim(), role: approveNivel }) }
      )
      setApproveFor(null)
      setApproveNotes('')
      setApproveNivel(NIVEL_DEFAULT)
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

  // Seleção em lote — só solicitações pendentes são elegíveis.
  const selectableIds = items.filter(x => x.status === 'pending').map(x => x.id)
  const bulk = useBulkSelection(selectableIds)

  // Executa aprovar/rejeitar em lote (loop sobre endpoints por-ID).
  async function runBulkAction() {
    if (!bulkKind || bulk.size === 0) return
    if (bulkKind === 'reject' && !bulkNotes.trim()) {
      showToast('err', 'Motivo é obrigatório para rejeitar.')
      return
    }
    setBusy(true)
    try {
      const notes = bulkNotes.trim()
      const { ok, fail, errors } = await runBulk(bulk.ids, async (id) => {
        const path = bulkKind === 'approve'
          ? `/onboarding/requests/${id}/approve`
          : `/onboarding/requests/${id}/reject`
        // FEAT-RBAC-2026-06-21 — aprovação em lote envia o `role` escolhido p/ todas.
        const body = bulkKind === 'approve' ? { notes, role: bulkNivel } : { notes }
        await api(path, { method: 'POST', body: JSON.stringify(body) })
      })
      if (ok > 0) showToast('ok', `${ok} solicitação(ões) ${bulkKind === 'approve' ? 'aprovada(s)' : 'rejeitada(s)'}.`)
      if (fail > 0) showToast('err', `${fail} falha(s): ${errors.slice(0, 3).join(' · ')}`)
      setBulkKind(null)
      setBulkNotes('')
      setBulkNivel(NIVEL_DEFAULT) // FEAT-RBAC-2026-06-21
      bulk.clear()
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha na operação em lote')
    } finally {
      setBusy(false)
    }
  }

  function openPanel() {
    setDraftQ(q); setDraftStatus(statusFilter); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setStatusFilter(draftStatus); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setStatusFilter(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftStatus(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (statusFilter) chips.push({ key: 'status', label: `Status: ${STATUS_LABELS[statusFilter]}`, onRemove: () => setStatusFilter('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Onboarding</h1>
          <p>{items.length} solicitação(ões){pendingCount > 0 ? ` — ${pendingCount} pendente(s)` : ''}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          <button
            className="szv2-btn szv2-btn-brand"
            onClick={() => { setCreateForm(emptyCreate()); setCreateErr(''); setShowCreate(true) }}
          >
            + Novo cadastro
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {/* Banner só com dados na tela (erro de refresh/ação). Falha de
          carregamento inicial vira ErrorState abaixo. */}
      {err && items.length > 0 && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {loading && items.length === 0 ? (
        <TableSkeleton rows={5} cols={9} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={load} />
      ) : !loading && items.length === 0 ? (
        <EmptyState
          icon="📋"
          title="Sem solicitações pendentes."
          description='Use "+ Novo cadastro" para criar manualmente, ou aguarde solicitações públicas.'
        />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th style={{ width: 36 }}>
                <input
                  type="checkbox"
                  checked={bulk.allSelected}
                  ref={el => { if (el) el.indeterminate = bulk.someSelected }}
                  onChange={bulk.toggleAll}
                  disabled={selectableIds.length === 0}
                  title="Selecionar todas as solicitações pendentes"
                  aria-label="Selecionar todas as solicitações pendentes"
                />
              </th>
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
            {items.map(req => (
              <tr key={req.id}>
                <td>
                  {req.status === 'pending' ? (
                    <input
                      type="checkbox"
                      checked={bulk.has(req.id)}
                      onChange={() => bulk.toggle(req.id)}
                      aria-label={`Selecionar solicitação #${req.id}`}
                    />
                  ) : null}
                </td>
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
                          onClick={() => { setApproveFor(req); setApproveNotes(''); setApproveNivel(NIVEL_DEFAULT) }}
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
      )}

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
              <div style={{ padding: '8px 0', fontSize: 13 }}>{fmtDate(detail.created_at)}</div>
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
            {/* FEAT-RBAC-2026-06-21 — admin define o NÍVEL (papel) do usuário aprovado. */}
            <div className="szv2-field" style={{ marginBottom: 16 }}>
              <label className="szv2-label">Nível do usuário *</label>
              <FalkSelect
                aria-label="Nível do usuário"
                value={approveNivel}
                onChange={v => setApproveNivel(v as Nivel)}
                options={(Object.keys(NIVEL_LABELS) as Nivel[]).map(k => ({ value: k, label: NIVEL_LABELS[k] }))}
              />
              <span
                className="szv2-help"
                style={{ display: 'block', marginTop: 4, fontSize: 12, color: 'var(--szv2-text-muted)' }}
              >
                Papel atribuído à conta criada. Padrão: <strong>Produtor</strong>.
              </span>
            </div>
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

      {/* Modal: ação em lote (aprovar/rejeitar com nota/motivo único) */}
      {bulkKind && (
        <div className="szv2-card" style={{ marginTop: 24 }}>
          <div className="szv2-card-head">
            <div>
              <h2>
                {bulkKind === 'approve' ? 'Aprovar' : 'Rejeitar'} {bulk.size} solicitação(ões)
              </h2>
              <p className="szv2-card-sub">
                {bulkKind === 'approve'
                  ? 'Cria portal_user + classe de frete para cada solicitação selecionada.'
                  : 'Marca como rejeitada — o motivo é aplicado a todas as selecionadas.'}
              </p>
            </div>
            <button className="szv2-modal-x" onClick={() => { setBulkKind(null); setBulkNotes('') }}>✕</button>
          </div>
          {/* FEAT-RBAC-2026-06-21 — nível aplicado a todas as selecionadas (só na aprovação). */}
          {bulkKind === 'approve' && (
            <div className="szv2-field" style={{ marginBottom: 16 }}>
              <label className="szv2-label">Nível do usuário *</label>
              <FalkSelect
                aria-label="Nível do usuário"
                value={bulkNivel}
                onChange={v => setBulkNivel(v as Nivel)}
                options={(Object.keys(NIVEL_LABELS) as Nivel[]).map(k => ({ value: k, label: NIVEL_LABELS[k] }))}
              />
              <span
                className="szv2-help"
                style={{ display: 'block', marginTop: 4, fontSize: 12, color: 'var(--szv2-text-muted)' }}
              >
                Aplicado a todas as solicitações selecionadas. Padrão: <strong>Produtor</strong>.
              </span>
            </div>
          )}
          <div className="szv2-field" style={{ marginBottom: 16 }}>
            <label className="szv2-label">{bulkKind === 'reject' ? 'Motivo *' : 'Notas (opcional)'}</label>
            <textarea
              className="szv2-input"
              rows={3}
              required={bulkKind === 'reject'}
              value={bulkNotes}
              onChange={e => setBulkNotes(e.target.value)}
              placeholder={bulkKind === 'reject' ? 'Ex.: dados inválidos…' : 'Observações…'}
            />
          </div>
          <div className="sz-form-actions">
            <button
              type="button"
              className={`szv2-btn ${bulkKind === 'approve' ? 'szv2-btn-brand' : 'szv2-btn-danger'}`}
              disabled={busy}
              onClick={runBulkAction}
            >
              {busy ? 'Processando…' : (bulkKind === 'approve' ? 'Confirmar aprovação' : 'Confirmar rejeição')}
            </button>
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary"
              onClick={() => { setBulkKind(null); setBulkNotes('') }}
            >
              Cancelar
            </button>
          </div>
        </div>
      )}

      <BulkBar
        count={bulk.size}
        onClear={bulk.clear}
        busy={busy}
        noun="solicitação selecionada"
        nounPlural="solicitações selecionadas"
        actions={[
          { label: 'Aprovar selecionadas', variant: 'brand', onClick: () => { setBulkNotes(''); setBulkNivel(NIVEL_DEFAULT); setBulkKind('approve') } },
          { label: 'Rejeitar selecionadas', variant: 'danger', onClick: () => { setBulkNotes(''); setBulkKind('reject') } },
        ]}
      />

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftIni}
            max={draftFim || undefined}
            onChange={v => setDraftIni(v)}
            placeholder="dd/mm/aaaa"
            aria-label="Data inicial"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftFim}
            min={draftIni || undefined}
            onChange={v => setDraftFim(v)}
            placeholder="dd/mm/aaaa"
            aria-label="Data final"
          />
        </FilterField>
        <FilterField label="Status">
          <FalkSelect
            aria-label="Status"
            value={draftStatus}
            onChange={v => setDraftStatus(v as StatusFilter)}
            options={(Object.keys(STATUS_LABELS) as StatusFilter[]).map(k => ({ value: k, label: STATUS_LABELS[k] }))}
          />
        </FilterField>
        <FilterField label="Busca (nome / e-mail)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: joao@…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
