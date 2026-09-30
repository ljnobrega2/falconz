// CodSaques — saques COD/Produtor + Afiliado numa tabela ÚNICA (AUDIT-2026-07-14,
// pedido do dono) + Regras globais/Overrides por produtor via prop `onlyRules`
// (montada separadamente em Taxas & Config).
// Espelha tab_fin_saques() (Unified_Menu.php :1529) + sz_cod_admin_page() (cod-wallet.php :858).
//
// Ação de marcar pago (produtor) / aprovar (afiliado) / rejeitar abre em
// DetailDrawer (painel lateral direito). IDs de produtor e afiliado são
// id-spaces independentes — a tabela unificada codifica o kind no sinal do id
// (encodeId/decodeId), então seleção em lote pode misturar os 2 tipos.

import { useEffect, useMemo, useRef, useState } from 'react'
import { useToast } from '../hooks/useToast'
import { api, getToken } from '../api'
import { safeUrl } from '../utils/safeUrl' // AUDIT-2026-06-21 #13
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import BulkBar, { useBulkSelection, runBulk } from '../components/BulkBar'
import SzStatusBadge from '../components/StatusBadge'
import DetailDrawer from '../components/DetailDrawer'

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
  proof_url: string | null
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
type Kind = 'producer' | 'affiliate'

// AUDIT-2026-07-14 — tabela ÚNICA de saques Produtor+Afiliado (pedido do
// dono). IDs de produtor e afiliado são id-spaces INDEPENDENTES (podem
// colidir, ex.: #2 produtor E #2 afiliado) — o id "unificado" usado em
// seleção/React key/ação codifica o kind no SINAL: positivo = produtor,
// negativo = afiliado. id=0 nunca é um saque real, então não há ambiguidade.
function encodeId(kind: Kind, id: number): number {
  return kind === 'affiliate' ? -id : id
}
function decodeId(uid: number): { kind: Kind; id: number } {
  return uid < 0 ? { kind: 'affiliate', id: -uid } : { kind: 'producer', id: uid }
}

// ─── Helpers visuais ──────────────────────────────────────────────────────

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

// Rótulo PT-BR por status (cor única vem do StatusBadge central).
const STATUS_LABEL: Record<string, string> = {
  analysis:   'Em análise',
  em_analise: 'Em análise',
  pending:    'Pendente',
  paid:       'Pago',
  approved:   'Aprovado',
  rejected:   'Rejeitado',
}

function StatusBadge({ status }: { status: string }) {
  return <SzStatusBadge status={status} label={STATUS_LABEL[status] || status || '—'} />
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

// id=0 + bulkIds preenchido → ação em lote (a barra sticky abre o modal com a
// seleção inteira; um único motivo/observação se aplica a todos). `id`/
// `bulkIds` são ids UNIFICADOS (encodeId) — cada um carrega seu próprio kind.
type ActionTarget = { id: number; kind: ModalKind; bulkIds?: number[] }

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

  const bulkCount = open.bulkIds?.length ?? 0
  const isBulk = bulkCount > 0

  // Reset ao abrir/trocar alvo.
  useEffect(() => {
    setProof('')
    setNote('')
    setFile(null)
    if (fileRef.current) fileRef.current.value = ''
  }, [open.id, open.kind, bulkCount])

  if (!open.kind) return null
  const isPay = open.kind === 'pay'

  return (
    <DetailDrawer
      open
      onClose={() => { if (!busy) onClose() }}
      title={
        <>
          {isPay ? 'Marcar como pago' : 'Rejeitar saque'}{' '}
          {isBulk ? `— ${bulkCount} saque(s) selecionado(s)` : `#${open.id}`}
        </>
      }
      footer={
        <>
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
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {isBulk && (
            <div className="sz-alert-warning" style={{ fontSize: 13 }}>
              {isPay
                ? `A observação/comprovante será aplicada a todos os ${bulkCount} saques confirmados (pode incluir produtor e afiliado).`
                : `O motivo será aplicado a todos os ${bulkCount} saques rejeitados.`}
            </div>
          )}
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
    </DetailDrawer>
  )
}

// ─── Página ───────────────────────────────────────────────────────────────

// AUDIT-2026-07-14 — `onlyRules` permite montar SÓ a aba "Regras Globais"
// (regras de saque + repasse ao produtor COD), sem as filas de aprovação —
// usado em Taxas & Config (pedido do dono: regras de saque/repasse são
// config, não fila operacional). Mesmo componente, mesma lógica/estado —
// só esconde o seletor de aba e as 2 outras abas.
export default function CodSaques({ onlyRules = false }: { onlyRules?: boolean } = {}) {
  const [tab, setTab] = useState<Tab>(onlyRules ? 'rules' : 'producer')
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
  const showToast = useToast() // AUDIT-2026-06-18 Onda3
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

  // Carrega produtor + afiliado JUNTOS (tela única, AUDIT-2026-07-14). O filtro de
  // status usa o mapeamento próprio de cada lado ('paid' vira 'approved' do lado
  // afiliado — filterToBackend já faz essa tradução por tab).
  async function loadList() {
    setLoading(true)
    setErr('')
    try {
      const qsProdStatus = filterToBackend('producer', filter)
      const qsAffStatus = filterToBackend('affiliate', filter)
      const qsProd = qsProdStatus ? `?status=${qsProdStatus}&limit=120` : '?limit=120'
      const qsAff = qsAffStatus ? `?status=${qsAffStatus}&limit=120` : '?limit=120'
      const [rp, ra] = await Promise.all([
        api<{ items: ProducerWithdrawal[] }>(`/cod-saques/producer${qsProd}`),
        api<{ items: AffiliateWithdrawal[] }>(`/cod-saques/affiliate${qsAff}`),
      ])
      setProdItems(rp.items || [])
      setAffItems(ra.items || [])
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

  // ── Ações ────────────────────────────────────────────────────────────────

  function openModal(id: number, kind: Exclude<ModalKind, null>) {
    setModal({ id, kind })
  }
  function closeModal() {
    setModal({ id: 0, kind: null })
  }

  // Monta URL + payload de UM saque para a ação corrente (pay/reject). `uid` é
  // o id UNIFICADO (encodeId) — decodifica o kind (produtor/afiliado) do sinal.
  function buildAction(uid: number, actionKind: Exclude<ModalKind, null>, proofURL: string, note: string) {
    const { kind, id } = decodeId(uid)
    if (kind === 'producer') {
      return {
        url: actionKind === 'pay'
          ? `/cod-saques/producer/${id}/mark-paid`
          : `/cod-saques/producer/${id}/reject`,
        payload: actionKind === 'pay'
          ? { proof_url: proofURL, admin_note: note }
          : { admin_note: note },
      }
    }
    return {
      url: actionKind === 'pay'
        ? `/cod-saques/affiliate/${id}/approve`
        : `/cod-saques/affiliate/${id}/reject`,
      payload: actionKind === 'pay'
        ? { proof_url: proofURL, admin_note: note }
        : { admin_note: note },
    }
  }

  // Upload multipart do comprovante — o helper api() força Content-Type JSON e
  // não serve para FormData; usa fetch direto como em MotoboyCustodia. Devolve
  // a proof_url gerada pelo backend. `uid` = id unificado (decodifica o kind).
  async function uploadProof(uid: number, file: File): Promise<string> {
    const { kind, id } = decodeId(uid)
    const fd = new FormData()
    fd.append('proof_file', file)
    const apiBase = import.meta.env.VITE_API_BASE || '/wp-json/senderzz/v1/admin'
    const tok = getToken()
    const headers: Record<string, string> = {}
    if (tok) headers['Authorization'] = `Bearer ${tok}`
    const endpoint = kind === 'affiliate'
      ? `/cod-saques/affiliate/${id}/upload-proof`
      : `/cod-saques/producer/${id}/upload-proof`
    const res = await fetch(`${apiBase}${endpoint}`, {
      method: 'POST',
      headers,
      body: fd,
    })
    if (!res.ok) {
      const body = await res.json().catch(() => ({}))
      throw new Error(body?.error?.message || `Falha no upload do comprovante (HTTP ${res.status})`)
    }
    const body = await res.json().catch(() => ({})) as { proof_url?: string }
    return body.proof_url || ''
  }

  async function confirmAction(proofURL: string, note: string, file: File | null) {
    if (!modal.kind) return
    const actionKind = modal.kind
    const verbDone = actionKind === 'pay' ? 'confirmado(s)' : 'rejeitado(s)'
    setBusy(true)
    try {
      // Caminho em lote — loop sobre os ids selecionados (sem endpoint de lote
      // no backend). Ids UNIFICADOS: cada um pode ser produtor OU afiliado —
      // buildAction decodifica o kind por id, então o lote aceita mistura dos 2.
      // Comprovante (URL) não é repassado em lote: o ImageUpload de arquivo é
      // por-saque; no lote só observação/URL textual se aplica.
      if (modal.bulkIds && modal.bulkIds.length > 0) {
        const { ok, fail, errors } = await runBulk(modal.bulkIds, async (uid) => {
          const { url, payload } = buildAction(uid, actionKind, proofURL, note)
          await api(url, { method: 'POST', body: JSON.stringify(payload) })
        })
        if (ok > 0) showToast('ok', `${ok} saque(s) ${verbDone}.`)
        if (fail > 0) showToast('err', `${fail} falha(s): ${errors.slice(0, 3).join(' · ')}`)
        bulk.clear()
        closeModal()
        await loadList()
        return
      }

      // Caminho individual.
      const uid = modal.id
      const { kind, id } = decodeId(uid)
      // Comprovante por arquivo: faz upload multipart antes de confirmar.
      let effProofURL = proofURL
      if (actionKind === 'pay' && file) {
        effProofURL = await uploadProof(uid, file)
      }
      const { url, payload } = buildAction(uid, actionKind, effProofURL, note)
      await api(url, { method: 'POST', body: JSON.stringify(payload) })
      showToast('ok', actionKind === 'pay'
        ? `Saque #${id} marcado como ${kind === 'affiliate' ? 'aprovado' : 'pago'}.`
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

  // ── KPIs derivados (cabeçalho) — combinam produtor + afiliado ───────────
  const kpis = useMemo(() => {
    if (tab === 'rules') return null
    const openProd = prodItems.filter(p => isOpen(p.status)).length
    const openAff = affItems.filter(p => isOpen(p.status)).length
    const totalNet = prodItems.reduce((s, p) => s + p.net, 0) + affItems.reduce((s, p) => s + p.net_amount, 0)
    return { open: openProd + openAff, total: prodItems.length + affItems.length, totalNet }
  }, [tab, prodItems, affItems])

  // Aplica filtros client-side de data + busca por nome/telefone/CPF (sem e-mail).
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
    // Regra do dono: busca só por nome, telefone e CPF (sem e-mail). Aqui = nome do
    // titular (holder_name) + CPF do titular (holder_cpf); não há telefone nesta linha.
    return [p.holder_name, p.holder_cpf].some(s => (s || '').toLowerCase().includes(needle))
  }
  function matchAff(a: AffiliateWithdrawal): boolean {
    if (!inRange(a.created_at)) return false
    if (!q) return true
    const needle = q.toLowerCase()
    return (a.affiliate_name || '').toLowerCase().includes(needle)
  }
  const visibleProd = prodItems.filter(matchProd)
  const visibleAff = affItems.filter(matchAff)

  // Linha unificada — produtor + afiliado na MESMA tabela (pedido do dono
  // 2026-07-14). uid = id codificado (encodeId) — chave de seleção/React key.
  type UnifiedRow = {
    uid: number
    kind: Kind
    nome: string
    sub: string | null
    amount: number
    fee: number
    net: number
    conta: string
    status: string
    dataRef: string
    proof_url: string | null
  }
  const unifiedRows: UnifiedRow[] = useMemo(() => [
    ...visibleProd.map((p): UnifiedRow => ({
      uid: encodeId('producer', p.id), kind: 'producer',
      nome: p.user_email || `#${p.user_id}`, sub: p.holder_name || null,
      amount: p.amount, fee: p.fee, net: p.net,
      conta: p.pix_key ? `${p.pix_type ? p.pix_type.toUpperCase() + ' · ' : ''}${p.pix_key}` : '—',
      status: p.status, dataRef: p.created_at, proof_url: p.proof_url,
    })),
    ...visibleAff.map((a): UnifiedRow => ({
      uid: encodeId('affiliate', a.id), kind: 'affiliate',
      nome: a.affiliate_name || `#${a.affiliate_id}`, sub: null,
      amount: a.amount, fee: a.fee, net: a.net_amount,
      conta: [a.pix_key, a.bank_info].filter(Boolean).join(' · ') || '—',
      status: a.status, dataRef: a.decided_at || a.created_at, proof_url: a.proof_url,
    })),
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ].sort((r1, r2) => (r2.dataRef || '').localeCompare(r1.dataRef || '')), [visibleProd, visibleAff])

  // Seleção em lote — só linhas acionáveis (status aberto) são elegíveis.
  const selectableIds = useMemo(
    () => unifiedRows.filter(r => isOpen(r.status)).map(r => r.uid),
    [unifiedRows],
  )
  const bulk = useBulkSelection(selectableIds)

  // Troca de aba/filtro limpa a seleção (ids não cruzam abas).
  useEffect(() => { bulk.clear() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [tab, filter])

  function openBulkModal(kind: Exclude<ModalKind, null>) {
    if (bulk.size === 0) return
    setModal({ id: 0, kind, bulkIds: bulk.ids })
  }

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

      {/* KPI mini-bar (somente na tela de lista, não em onlyRules) */}
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
              <h2>Saques COD (Produtor / Afiliado)</h2>
              <p className="szv2-card-sub">
                Análise → Pendente → Pago/Aprovado. Marcar pago (produtor) debita repasse; aprovar
                (afiliado) debita a carteira e cria transação de saída.
              </p>
            </div>
            <FilterButton
              active={chips.length > 0}
              count={chips.length}
              onClick={openPanel}
            />
          </div>

          {loading && unifiedRows.length === 0 ? (
            <TableSkeleton rows={5} cols={9} />
          ) : !loading && unifiedRows.length === 0 ? (
            <EmptyState
              icon="💸"
              title="Nenhum saque encontrado para este filtro."
              description="Solicitações de saque de produtores e afiliados aparecem aqui."
            />
          ) : (
            <div style={{ overflowX: 'auto' }}>
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
                        title="Selecionar todos os saques em aberto"
                        aria-label="Selecionar todos os saques em aberto"
                      />
                    </th>
                    <th>ID</th>
                    <th>Tipo</th>
                    <th>Usuário</th>
                    <th style={{ textAlign: 'right' }}>Valor</th>
                    <th style={{ textAlign: 'right' }}>Taxa</th>
                    <th style={{ textAlign: 'right' }}>Líquido</th>
                    <th>PIX / Conta</th>
                    <th>Status</th>
                    <th>Data</th>
                    <th style={{ width: 180 }}>Ações</th>
                  </tr>
                </thead>
                <tbody>
                  {unifiedRows.map(r => (
                    <tr key={r.uid}>
                      <td>
                        {isOpen(r.status) ? (
                          <input
                            type="checkbox"
                            checked={bulk.has(r.uid)}
                            onChange={() => bulk.toggle(r.uid)}
                            aria-label={`Selecionar saque #${Math.abs(r.uid)}`}
                          />
                        ) : null}
                      </td>
                      <td><strong>#{Math.abs(r.uid)}</strong></td>
                      <td>
                        <span className={`sz-badge ${r.kind === 'producer' ? 'szv2-badge-brand' : 'szv2-badge-neutral'}`}>
                          {r.kind === 'producer' ? 'Produtor' : 'Afiliado'}
                        </span>
                      </td>
                      <td>
                        <div style={{ fontSize: 13 }}>{r.nome}</div>
                        {r.sub && (
                          <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{r.sub}</div>
                        )}
                      </td>
                      <td style={{ textAlign: 'right' }}>R$ {fmt(r.amount)}</td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>R$ {fmt(r.fee)}</td>
                      <td style={{ textAlign: 'right', fontWeight: 700, color: 'var(--szv2-brand)' }}>
                        R$ {fmt(r.net)}
                      </td>
                      <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 11 }}>
                        <span title={r.conta}>{r.conta.length > 28 ? r.conta.slice(0, 28) + '…' : r.conta}</span>
                      </td>
                      <td><StatusBadge status={r.status} /></td>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                        {r.dataRef?.slice(0, 16).replace('T', ' ') ?? '—'}
                      </td>
                      <td>
                        {isOpen(r.status) ? (
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-brand szv2-btn-sm"
                              onClick={() => openModal(r.uid, 'pay')}
                              disabled={busy}
                            >
                              {r.kind === 'producer' ? 'Marcar pago' : 'Aprovar'}
                            </button>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-danger szv2-btn-sm"
                              onClick={() => openModal(r.uid, 'reject')}
                              disabled={busy}
                            >
                              Rejeitar
                            </button>
                          </div>
                        ) : r.proof_url ? (
                          <a
                            href={safeUrl(r.proof_url)}
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
          <FalkDatePicker
            value={draftFrom}
            onChange={v => setDraftFrom(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftTo}
            onChange={v => setDraftTo(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Status">
          <FalkSelect
            value={draftFilter}
            onChange={v => setDraftFilter(v as FilterKey)}
            options={FILTER_ORDER.map(k => ({ value: k, label: FILTER_LABEL[k] }))}
            aria-label="Status"
          />
        </FilterField>
        <FilterField label="Busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="Buscar por nome, telefone ou CPF"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>

      {/* Barra de ações em lote — só nas abas de lista, sobre saques em aberto. */}
      {tab !== 'rules' && (
        <BulkBar
          count={bulk.size}
          onClear={bulk.clear}
          busy={busy}
          noun="saque selecionado"
          nounPlural="saques selecionados"
          actions={[
            {
              label: 'Confirmar selecionados',
              variant: 'brand',
              onClick: () => openBulkModal('pay'),
            },
            {
              label: 'Rejeitar selecionados',
              variant: 'danger',
              onClick: () => openBulkModal('reject'),
            },
          ]}
        />
      )}
    </div>
  )
}
