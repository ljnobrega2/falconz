// CodSaques — saques COD/Produtor + Afiliado + Regras globais + Overrides por produtor.
// Espelha tab_fin_saques() (Unified_Menu.php :1529) + sz_cod_admin_page() (cod-wallet.php :858).
//
// Três abas: Produtor / Afiliado / Regras Globais.
// Modais inline (szv2-modal-overlay + .szv2-open) para marcar pago / rejeitar.

import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// ─── Tipos ────────────────────────────────────────────────────────────────

type ProducerWithdrawal = {
  id: number
  user_id: number
  user_email: string
  amount: number
  fee: number
  net: number
  pix_key: string
  pix_type: string
  holder_name: string
  holder_cpf: string
  status: string
  admin_note: string | null
  proof_url: string | null
  completed_at: string | null
  created_at: string
}

type AffiliateWithdrawal = {
  id: number
  affiliate_id: number
  affiliate_name: string
  amount: number
  fee: number
  net_amount: number
  pix_key: string
  bank_info: string
  status: string
  admin_note: string | null
  decided_at: string | null
  decided_by: number | null
  created_at: string
}

type GlobalRules = {
  retention_days: number
  withdraw_fee: number
  anticipation_fee_pct: number
  motoboy_fee: number
  operational_fund_fee: number
}

type ProducerOverrideItem = {
  user_id: number
  nome: string
  email: string
  retention_days: number | null
  withdraw_fee: number | null
  anticipation_fee: number | null
  eff_retention_days: number
  eff_withdraw_fee: number
  eff_anticipation_fee: number
}

type Tab = 'producer' | 'affiliate' | 'rules'
type ModalKind = 'pay' | 'reject' | null

// ─── Helpers visuais ──────────────────────────────────────────────────────

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

// Map de status (DB) → classe de badge e rótulo PT-BR.
const STATUS_BADGE: Record<string, { cls: string; label: string }> = {
  analysis:   { cls: 'szv2-badge-warning', label: 'Em análise' },
  em_analise: { cls: 'szv2-badge-warning', label: 'Em análise' },
  pending:    { cls: 'szv2-badge-warning', label: 'Pendente'   },
  paid:       { cls: 'szv2-badge-success', label: 'Pago'       },
  approved:   { cls: 'szv2-badge-success', label: 'Aprovado'   },
  rejected:   { cls: 'szv2-badge-danger',  label: 'Rejeitado'  },
}

function StatusBadge({ status }: { status: string }) {
  const m = STATUS_BADGE[status] || { cls: 'szv2-badge-neutral', label: status || '—' }
  return <span className={`sz-badge ${m.cls}`}>{m.label}</span>
}

// Filtros por aba — chave passada ao backend (?status=). 'paid' inclui approved (mapeado por aba).
type FilterKey = '' | 'analysis' | 'pending' | 'paid' | 'rejected'
const FILTER_LABEL: Record<FilterKey, string> = {
  '':         'Todos',
  analysis:   'Em análise',
  pending:    'Pendente',
  paid:       'Pago',
  rejected:   'Rejeitado',
}
const FILTER_ORDER: FilterKey[] = ['', 'analysis', 'pending', 'paid', 'rejected']

// Para o afiliado, "Pago" no chip = "approved" no DB.
const filterToBackend = (tab: Tab, f: FilterKey): string => {
  if (!f) return ''
  if (tab === 'affiliate' && f === 'paid') return 'approved'
  return f
}

// Decide se uma linha ainda admite ação (não finalizada).
const isOpen = (status: string) =>
  status === 'analysis' || status === 'pending' || status === 'em_analise'

// ─── Modais ───────────────────────────────────────────────────────────────

type ActionTarget = { id: number; kind: ModalKind; tab?: Tab }

function ActionModal(props: {
  open: ActionTarget
  busy: boolean
  onClose: () => void
  onConfirm: (proofURL: string, note: string, file: File | null) => void
}) {
  const { open, busy, onClose, onConfirm } = props
  const [proof, setProof] = useState('')
  const [note, setNote] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  // Reset ao abrir/trocar alvo.
  useEffect(() => {
    setProof('')
    setNote('')
    setFile(null)
    if (fileRef.current) fileRef.current.value = ''
  }, [open.id, open.kind])

  if (!open.kind) return null
  const isPay = open.kind === 'pay'

  return (
    <div
      className="szv2-modal-overlay szv2-open"
      onClick={(e) => { if (e.target === e.currentTarget && !busy) onClose() }}
    >
      <div className="szv2-modal">
        <div className="szv2-modal-head">
          <h3>{isPay ? 'Marcar como pago' : 'Rejeitar saque'} #{open.id}</h3>
          <button className="szv2-modal-x" onClick={onClose} disabled={busy} aria-label="Fechar">✕</button>
        </div>
        <div className="szv2-modal-body" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {isPay && (
            <>
              <div className="szv2-field">
                <label className="szv2-label">Comprovante — arquivo (opcional)</label>
                <input
                  ref={fileRef}
                  type="file"
                  className="szv2-input"
                  accept="image/*,application/pdf"
                  disabled={busy}
                  onChange={(e) => {
                    const f = e.target.files?.[0] ?? null
                    setFile(f)
                    if (f) setProof('') // limpa URL se arquivo selecionado
                  }}
                />
                <span className="szv2-text-xs szv2-text-muted">
                  Imagem (JPG/PNG/etc.) ou PDF. Máx 16 MB.
                </span>
              </div>
              <div className="szv2-field">
                <label className="szv2-label">ou URL do comprovante</label>
                <input
                  type="url"
                  className="szv2-input"
                  placeholder="https://…"
                  value={proof}
                  onChange={(e) => {
                    setProof(e.target.value)
                    if (e.target.value && fileRef.current) {
                      fileRef.current.value = '' // limpa arquivo se URL digitada
                      setFile(null)
                    }
                  }}
                  disabled={busy || !!file}
                />
              </div>
            </>
          )}
          <div className="szv2-field">
            <label className="szv2-label">Observação interna</label>
            <textarea
              className="szv2-input"
              rows={3}
              style={{ height: 'auto', padding: '8px 12px', resize: 'vertical' }}
              placeholder={isPay ? 'Ex.: pago via PIX manual em…' : 'Motivo da recusa…'}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              disabled={busy}
            />
          </div>
        </div>
        <div className="szv2-modal-foot">
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={onClose}
            disabled={busy}
          >
            Cancelar
          </button>
          <button
            type="button"
            className={`szv2-btn ${isPay ? 'szv2-btn-brand' : 'szv2-btn-danger'}`}
            onClick={() => onConfirm(proof, note, file)}
            disabled={busy}
          >
            {busy ? 'Enviando…' : (isPay ? 'Marcar pago' : 'Rejeitar')}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─── Página ───────────────────────────────────────────────────────────────

export default function CodSaques() {
  const [tab, setTab] = useState<Tab>('producer')
  const [filter, setFilter] = useState<FilterKey>('')
  const [prodItems, setProdItems] = useState<ProducerWithdrawal[]>([])
  const [affItems, setAffItems] = useState<AffiliateWithdrawal[]>([])
  const [rules, setRules] = useState<GlobalRules | null>(null)
  const [rulesTableReady, setRulesTableReady] = useState(true)
  const [overrides, setOverrides] = useState<ProducerOverrideItem[]>([])
  const [overridesReady, setOverridesReady] = useState(true)
  const [overridesBusy, setOverridesBusy] = useState(false)
  // edits[user_id] = campos que o admin editou localmente (antes de salvar)
  const [overrideEdits, setOverrideEdits] = useState<Record<number, Partial<ProducerOverrideItem>>>({})
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [modal, setModal] = useState<ActionTarget>({ id: 0, kind: null })

  // Filtros adicionais (data + busca) — aplicados client-side sobre prodItems/affItems.
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState('')
  const [q, setQ] = useState('')

  // Painel
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftFilter, setDraftFilter] = useState<FilterKey>('')
  const [draftFrom, setDraftFrom] = useState('')
  const [draftTo, setDraftTo] = useState('')
  const [draftQ, setDraftQ] = useState('')

  // Toast com timeout único.
  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  // Carrega a lista correspondente à aba ativa.
  async function loadList() {
    setLoading(true)
    setErr('')
    try {
      const qsStatus = filterToBackend(tab, filter)
      const qs = qsStatus ? `?status=${qsStatus}&limit=120` : '?limit=120'
      if (tab === 'producer') {
        const r = await api<{ items: ProducerWithdrawal[] }>(`/cod-saques/producer${qs}`)
        setProdItems(r.items || [])
      } else if (tab === 'affiliate') {
        const r = await api<{ items: AffiliateWithdrawal[] }>(`/cod-saques/affiliate${qs}`)
        setAffItems(r.items || [])
      }
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  async function loadRules() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<{ rules: GlobalRules; table_ready: boolean }>('/cod-saques/global-rules')
      setRules(r.rules)
      setRulesTableReady(r.table_ready)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar regras')
    } finally {
      setLoading(false)
    }
  }

  async function loadOverrides() {
    setOverridesBusy(true)
    try {
      const r = await api<{ items: ProducerOverrideItem[]; table_ready: boolean }>('/cod-saques/producer/overrides')
      setOverrides(r.items || [])
      setOverridesReady(r.table_ready ?? true)
      setOverrideEdits({})
    } catch {
      // silencioso — não bloqueia a aba de regras globais
      setOverrides([])
    } finally {
      setOverridesBusy(false)
    }
  }

  // Carga inicial / quando muda aba ou filtro.
  useEffect(() => {
    if (tab === 'rules') {
      loadRules()
    } else {
      loadList()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, filter])

  // switchTab reseta o filtro ANTES de mudar a aba para evitar reload duplo.
  function switchTab(next: Tab) {
    if (next === tab) return
    setFilter('')
    setTab(next)
  }

  // ── Ações ────────────────────────────────────────────────────────────────

  function openModal(id: number, kind: Exclude<ModalKind, null>) {
    setModal({ id, kind })
  }
  function closeModal() {
    setModal({ id: 0, kind: null })
  }

  async function confirmAction(proofURL: string, note: string) {
    if (!modal.kind) return
    setBusy(true)
    try {
      const id = modal.id
      let url = ''
      let payload: Record<string, string> = {}
      if (tab === 'producer') {
        url = modal.kind === 'pay'
          ? `/cod-saques/producer/${id}/mark-paid`
          : `/cod-saques/producer/${id}/reject`
        payload = modal.kind === 'pay'
          ? { proof_url: proofURL, admin_note: note }
          : { admin_note: note }
      } else {
        url = modal.kind === 'pay'
          ? `/cod-saques/affiliate/${id}/approve`
          : `/cod-saques/affiliate/${id}/reject`
        payload = { admin_note: note }
      }
      await api(url, { method: 'POST', body: JSON.stringify(payload) })
      showToast('ok', modal.kind === 'pay'
        ? `Saque #${id} marcado como ${tab === 'affiliate' ? 'aprovado' : 'pago'}.`
        : `Saque #${id} rejeitado.`)
      closeModal()
      await loadList()
    } catch (e: any) {
      showToast('err', e.message || 'Falha na operação')
    } finally {
      setBusy(false)
    }
  }

  async function saveRules() {
    if (!rules) return
    setBusy(true)
    try {
      await api('/cod-saques/global-rules', {
        method: 'POST',
        body: JSON.stringify(rules),
      })
      showToast('ok', 'Regras salvas.')
      await loadRules()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar')
    } finally {
      setBusy(false)
    }
  }

  // ── KPIs derivados (cabeçalho) ───────────────────────────────────────────
  const kpis = useMemo(() => {
    if (tab === 'producer') {
      const open = prodItems.filter(p => isOpen(p.status)).length
      const totalNet = prodItems.reduce((s, p) => s + p.net, 0)
      return { open, total: prodItems.length, totalNet }
    }
    if (tab === 'affiliate') {
      const open = affItems.filter(p => isOpen(p.status)).length
      const totalNet = affItems.reduce((s, p) => s + p.net_amount, 0)
      return { open, total: affItems.length, totalNet }
    }
    return null
  }, [tab, prodItems, affItems])

  // Aplica filtros client-side de data + busca por nome/email.
  function inRange(created: string): boolean {
    if (!dateFrom && !dateTo) return true
    const d = created.slice(0, 10)
    if (dateFrom && d < dateFrom) return false
    if (dateTo && d > dateTo) return false
    return true
  }
  function matchProd(p: ProducerWithdrawal): boolean {
    if (!inRange(p.created_at)) return false
    if (!q) return true
    const needle = q.toLowerCase()
    return [p.user_email, p.holder_name, p.holder_cpf].some(s => (s || '').toLowerCase().includes(needle))
  }
  function matchAff(a: AffiliateWithdrawal): boolean {
    if (!inRange(a.created_at)) return false
    if (!q) return true
    const needle = q.toLowerCase()
    return (a.affiliate_name || '').toLowerCase().includes(needle)
  }
  const visibleProd = prodItems.filter(matchProd)
  const visibleAff = affItems.filter(matchAff)

  function openPanel() {
    setDraftFilter(filter); setDraftFrom(dateFrom); setDraftTo(dateTo); setDraftQ(q)
    setFilterOpen(true)
  }
  function applyFilters() {
    setFilter(draftFilter); setDateFrom(draftFrom); setDateTo(draftTo); setQ(draftQ)
    setFilterOpen(false)
  }
  function clearFilters() {
    setFilter(''); setDateFrom(''); setDateTo(''); setQ('')
    setDraftFilter(''); setDraftFrom(''); setDraftTo(''); setDraftQ('')
    setFilterOpen(false)
  }

  // Chips ativos (só aparecem nas abas que filtram).
  const chips: ActiveChip[] = []
  if (tab !== 'rules') {
    if (filter) chips.push({ key: 'status', label: `Status: ${FILTER_LABEL[filter]}`, onRemove: () => setFilter('') })
    if (dateFrom) chips.push({ key: 'from', label: `De: ${dateFrom}`, onRemove: () => setDateFrom('') })
    if (dateTo) chips.push({ key: 'to', label: `Até: ${dateTo}`, onRemove: () => setDateTo('') })
    if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  }

  // ─── Render ──────────────────────────────────────────────────────────────

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

      {/* Tabs */}
      <div className="szv2-tabs">
        <button
          type="button"
          className="szv2-tab"
          aria-selected={tab === 'producer'}
          onClick={() => switchTab('producer')}
        >
          Saques Produtor
        </button>
        <button
          type="button"
          className="szv2-tab"
          aria-selected={tab === 'affiliate'}
          onClick={() => switchTab('affiliate')}
        >
          Saques Afiliado
        </button>
        <button
          type="button"
          className="szv2-tab"
          aria-selected={tab === 'rules'}
          onClick={() => switchTab('rules')}
        >
          Regras Globais
        </button>
      </div>

      {/* KPI mini-bar (somente nas abas de lista) */}
      {kpis && (
        <div
          className="szv2-kpi-grid"
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(3, minmax(0,1fr))',
            gap: 16,
            marginBottom: 16,
          }}
        >
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Em aberto</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-warning)' }}>{kpis.open}</span>
              <span className="szv2-kpi-meta">solicitações aguardando decisão</span>
            </div>
          </div>
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Total exibido</span>
              <span className="szv2-kpi-value">{kpis.total}</span>
              <span className="szv2-kpi-meta">linhas (limite 120)</span>
            </div>
          </div>
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Líquido somado</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>R$ {fmt(kpis.totalNet)}</span>
              <span className="szv2-kpi-meta">soma do líquido das linhas</span>
            </div>
          </div>
        </div>
      )}

      {/* Filtros (somente nas abas de lista) */}
      {tab !== 'rules' && (
        <>
          <ActiveFilterChips chips={chips} onClearAll={clearFilters} />
        </>
      )}

      {tab !== 'rules' && (
        <div className="szv2-card" style={{ marginBottom: 16 }}>
          <div className="szv2-card-head">
            <div>
              <h2>{tab === 'producer' ? 'Saques COD / Produtor' : 'Saques de Afiliados'}</h2>
              <p className="szv2-card-sub">
                {tab === 'producer'
                  ? 'Análise → Pendente → Pago. Marque como pago após confirmar o repasse.'
                  : 'Aprovar debita a carteira do afiliado e cria uma transação de saída.'}
              </p>
            </div>
            <FilterButton
              active={chips.length > 0}
              count={chips.length}
              onClick={openPanel}
            />
          </div>

          {loading ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Carregando…
            </div>
          ) : (tab === 'producer' ? visibleProd.length : visibleAff.length) === 0 ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Nenhum saque encontrado para este filtro.
            </div>
          ) : tab === 'producer' ? (
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>Usuário</th>
                    <th style={{ textAlign: 'right' }}>Valor</th>
                    <th style={{ textAlign: 'right' }}>Taxa</th>
                    <th style={{ textAlign: 'right' }}>Líquido</th>
                    <th>PIX</th>
                    <th>Status</th>
                    <th>Data</th>
                    <th style={{ width: 180 }}>Ações</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleProd.map(p => (
                    <tr key={p.id}>
                      <td><strong>#{p.id}</strong></td>
                      <td>
                        <div style={{ fontSize: 13 }}>{p.user_email || '—'}</div>
                        {p.holder_name && (
                          <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{p.holder_name}</div>
                        )}
                      </td>
                      <td style={{ textAlign: 'right' }}>R$ {fmt(p.amount)}</td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>R$ {fmt(p.fee)}</td>
                      <td style={{ textAlign: 'right', fontWeight: 700, color: 'var(--szv2-brand)' }}>
                        R$ {fmt(p.net)}
                      </td>
                      <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 11 }}>
                        {p.pix_key
                          ? <span title={p.pix_key}>{p.pix_type ? `${p.pix_type.toUpperCase()} · ` : ''}{p.pix_key.length > 24 ? p.pix_key.slice(0, 24) + '…' : p.pix_key}</span>
                          : '—'}
                      </td>
                      <td><StatusBadge status={p.status} /></td>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                        {p.created_at?.slice(0, 16).replace('T', ' ') ?? '—'}
                      </td>
                      <td>
                        {isOpen(p.status) ? (
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-brand szv2-btn-sm"
                              onClick={() => openModal(p.id, 'pay')}
                              disabled={busy}
                            >
                              Marcar pago
                            </button>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-danger szv2-btn-sm"
                              onClick={() => openModal(p.id, 'reject')}
                              disabled={busy}
                            >
                              Rejeitar
                            </button>
                          </div>
                        ) : p.proof_url ? (
                          <a
                            href={p.proof_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            style={{ color: 'var(--szv2-brand)', fontSize: 12 }}
                          >
                            Comprovante
                          </a>
                        ) : (
                          <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>Afiliado</th>
                    <th style={{ textAlign: 'right' }}>Valor</th>
                    <th style={{ textAlign: 'right' }}>Taxa</th>
                    <th style={{ textAlign: 'right' }}>Líquido</th>
                    <th>Conta</th>
                    <th>Status</th>
                    <th>Data</th>
                    <th style={{ width: 180 }}>Ações</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleAff.map(a => (
                    <tr key={a.id}>
                      <td><strong>#{a.id}</strong></td>
                      <td style={{ fontWeight: 500 }}>{a.affiliate_name || `#${a.affiliate_id}`}</td>
                      <td style={{ textAlign: 'right' }}>R$ {fmt(a.amount)}</td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>R$ {fmt(a.fee)}</td>
                      <td style={{ textAlign: 'right', fontWeight: 700, color: 'var(--szv2-brand)' }}>
                        R$ {fmt(a.net_amount)}
                      </td>
                      <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 11 }}>
                        {[a.pix_key, a.bank_info].filter(Boolean).join(' · ') || '—'}
                      </td>
                      <td><StatusBadge status={a.status} /></td>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                        {(a.decided_at || a.created_at)?.slice(0, 16).replace('T', ' ') ?? '—'}
                      </td>
                      <td>
                        {isOpen(a.status) ? (
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-brand szv2-btn-sm"
                              onClick={() => openModal(a.id, 'pay')}
                              disabled={busy}
                            >
                              Aprovar
                            </button>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-danger szv2-btn-sm"
                              onClick={() => openModal(a.id, 'reject')}
                              disabled={busy}
                            >
                              Rejeitar
                            </button>
                          </div>
                        ) : (
                          <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Aba Regras Globais */}
      {tab === 'rules' && (
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div>
              <h2>Regras globais de saque</h2>
              <p className="szv2-card-sub">
                Fallback aplicado a todo produtor/afiliado quando não há override individual.
              </p>
            </div>
          </div>

          {!rulesTableReady && (
            <div className="sz-alert-danger" style={{ marginBottom: 16 }}>
              Tabela <code>senderzz_options</code> ainda não foi migrada. A leitura usa defaults, e
              o salvamento será habilitado quando a migração rodar.
            </div>
          )}

          {loading || !rules ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Carregando…
            </div>
          ) : (
            <form
              onSubmit={(e) => { e.preventDefault(); saveRules() }}
              className="sz-form-grid sz-form-grid-2"
              style={{ maxWidth: 720 }}
            >
              <div className="szv2-field">
                <label className="szv2-label">Retenção após entrega (dias)</label>
                <input
                  type="number"
                  min={0}
                  className="szv2-input"
                  value={rules.retention_days}
                  onChange={(e) => setRules({ ...rules, retention_days: Math.max(0, parseInt(e.target.value || '0', 10)) })}
                  disabled={busy}
                />
                <span className="szv2-text-xs szv2-text-muted">
                  Quantos dias após a entrega o repasse fica disponível para saque.
                </span>
              </div>

              <div className="szv2-field">
                <label className="szv2-label">Taxa de saque (R$)</label>
                <input
                  type="number"
                  min={0}
                  step="0.01"
                  className="szv2-input"
                  value={rules.withdraw_fee}
                  onChange={(e) => setRules({ ...rules, withdraw_fee: Math.max(0, parseFloat(e.target.value || '0')) })}
                  disabled={busy}
                />
                <span className="szv2-text-xs szv2-text-muted">
                  Valor fixo cobrado a cada saque solicitado.
                </span>
              </div>

              <div className="szv2-field">
                <label className="szv2-label">Taxa de antecipação (%)</label>
                <input
                  type="number"
                  min={0}
                  step="0.01"
                  className="szv2-input"
                  value={rules.anticipation_fee_pct}
                  onChange={(e) => setRules({ ...rules, anticipation_fee_pct: Math.max(0, parseFloat(e.target.value || '0')) })}
                  disabled={busy}
                />
                <span className="szv2-text-xs szv2-text-muted">
                  Percentual cobrado quando o saque é antecipado antes do prazo de retenção.
                </span>
              </div>

              <div className="szv2-field">
                <label className="szv2-label">Taxa motoboy admin (R$)</label>
                <input
                  type="number"
                  min={0}
                  step="0.01"
                  className="szv2-input"
                  value={rules.motoboy_fee}
                  onChange={(e) => setRules({ ...rules, motoboy_fee: Math.max(0, parseFloat(e.target.value || '0')) })}
                  disabled={busy}
                />
                <span className="szv2-text-xs szv2-text-muted">
                  Custo padrão de motoboy aplicado em <code>{'{{comissao_admin_liquida}}'}</code>.
                </span>
              </div>

              <div className="szv2-field">
                <label className="szv2-label">Fundo operacional (R$)</label>
                <input
                  type="number"
                  min={0}
                  step="0.01"
                  className="szv2-input"
                  value={rules.operational_fund_fee}
                  onChange={(e) => setRules({ ...rules, operational_fund_fee: Math.max(0, parseFloat(e.target.value || '0')) })}
                  disabled={busy}
                />
                <span className="szv2-text-xs szv2-text-muted">
                  Reserva operacional descontada antes do repasse ao produtor.
                </span>
              </div>

              <div className="sz-form-actions" style={{ gridColumn: '1 / -1' }}>
                <button
                  type="submit"
                  className="szv2-btn szv2-btn-brand"
                  disabled={busy || !rulesTableReady}
                >
                  {busy ? 'Salvando…' : 'Salvar regras'}
                </button>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-secondary"
                  onClick={loadRules}
                  disabled={busy}
                >
                  Recarregar
                </button>
              </div>
            </form>
          )}
        </div>
      )}

      <ActionModal
        open={modal}
        busy={busy}
        onClose={closeModal}
        onConfirm={confirmAction}
      />

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFrom}
            onChange={e => setDraftFrom(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftTo}
            onChange={e => setDraftTo(e.target.value)}
          />
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftFilter}
            onChange={e => setDraftFilter(e.target.value as FilterKey)}
          >
            {FILTER_ORDER.map(k => <option key={k} value={k}>{FILTER_LABEL[k]}</option>)}
          </select>
        </FilterField>
        <FilterField label="Busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="afiliado / produtor / e-mail"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
