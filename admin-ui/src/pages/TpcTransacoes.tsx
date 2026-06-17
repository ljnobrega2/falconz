import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// ----- Tipos retornados pelo handler Go ---------------------------------------

type TransacaoRow = {
  id: number
  user_id: number
  nome: string
  email: string
  tipo: string
  valor: number
  saldo_apos: number
  descricao: string
  referencia: string | null
  order_id: number | null
  me_order_id: string | null
  status: string
  created_at: string
}

type ListResponse = {
  items: TransacaoRow[]
  total: number
  page: number
  per_page: number
}

type DeleteResponse = {
  ok: boolean
  id: number
  user_id: number
  novo_saldo: number
}

type VerificarPixResponse = {
  ok: boolean
  queued: number
  job_queued: boolean
  requested_at: string
  stub?: boolean
}

// ----- Filtros ---------------------------------------------------------------

type TipoFilter = '' | 'credito' | 'debito'
type StatusFilter = '' | 'pendente' | 'analise' | 'confirmado' | 'cancelado'
type CarteiraFilter = 'expedicao' | 'cod' | 'todas'

const TIPO_CHIPS: { key: TipoFilter; label: string }[] = [
  { key: '',        label: 'Todos' },
  { key: 'credito', label: 'Crédito' },
  { key: 'debito',  label: 'Débito' },
]

const STATUS_CHIPS: { key: StatusFilter; label: string }[] = [
  { key: '',           label: 'Todos' },
  { key: 'pendente',   label: 'Pendente' },
  { key: 'analise',    label: 'Análise' },
  { key: 'confirmado', label: 'Confirmado' },
  { key: 'cancelado',  label: 'Cancelado' },
]

// ----- Helpers ---------------------------------------------------------------

const fmt = (v: number) =>
  'R$ ' + v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtDate = (s: string | null | undefined) =>
  s ? s.slice(0, 16).replace('T', ' ') : '—'

// Mapeamento de classes para o badge de status — segue a paleta padrão Senderzz V2.
const STATUS_BADGE_CLS: Record<string, string> = {
  pendente:   'szv2-badge-warning',
  analise:    'szv2-badge-warning',
  confirmado: 'szv2-badge-success',
  cancelado:  'szv2-badge-danger',
}

const STATUS_LABEL: Record<string, string> = {
  pendente:   'Pendente',
  analise:    'Em análise',
  confirmado: 'Confirmado',
  cancelado:  'Cancelado',
}

// Chip component — espelha o padrão szv2-tab usado em outros painéis.
function Chip({
  active,
  onClick,
  children,
  disabled,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      className="szv2-tab"
      aria-selected={active}
      disabled={disabled}
      onClick={onClick}
      style={{ minHeight: 32, padding: '6px 14px', fontSize: 13 }}
    >
      {children}
    </button>
  )
}

// ----- Página ----------------------------------------------------------------

export default function TpcTransacoes() {
  const PER_PAGE = 20

  const [items, setItems] = useState<TransacaoRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)

  // Filtros
  const [tipo, setTipo] = useState<TipoFilter>('')
  const [status, setStatus] = useState<StatusFilter>('')
  // Filtro client-side para separar transações da Carteira Expedição (frete) vs COD
  const [carteira, setCarteira] = useState<CarteiraFilter>('expedicao')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')
  const [userIdInput, setUserIdInput] = useState('')
  const [userIdApplied, setUserIdApplied] = useState('')

  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [deletingId, setDeletingId] = useState<number | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = new URLSearchParams()
      qs.set('page', String(page))
      qs.set('per_page', String(PER_PAGE))
      if (tipo)          qs.set('tipo', tipo)
      if (status)        qs.set('status', status)
      if (dataIni)       qs.set('data_ini', dataIni)
      if (dataFim)       qs.set('data_fim', dataFim)
      if (userIdApplied) qs.set('user_id', userIdApplied)

      const r = await api<ListResponse>(`/tpc-transacoes?${qs.toString()}`)
      setItems(r.items || [])
      setTotal(r.total || 0)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar transações')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [page, tipo, status, dataIni, dataFim, userIdApplied])

  // Reseta para página 1 sempre que um filtro mudar.
  function applyTipo(t: TipoFilter)     { setPage(1); setTipo(t) }
  function applyStatus(s: StatusFilter) { setPage(1); setStatus(s) }
  function applyDataIni(d: string)      { setPage(1); setDataIni(d) }
  function applyDataFim(d: string)      { setPage(1); setDataFim(d) }

  function applyUserId(e: React.FormEvent) {
    e.preventDefault()
    setPage(1)
    setUserIdApplied(userIdInput.trim())
  }

  function clearAllFilters() {
    setTipo('')
    setStatus('')
    setDataIni('')
    setDataFim('')
    setUserIdInput('')
    setUserIdApplied('')
    setPage(1)
    setDraftTipo(''); setDraftStatus('')
    setDraftIni(''); setDraftFim(''); setDraftUser('')
    setFilterOpen(false)
  }

  // Painel
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftTipo, setDraftTipo] = useState<TipoFilter>('')
  const [draftStatus, setDraftStatus] = useState<StatusFilter>('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [draftUser, setDraftUser] = useState('')

  function openPanel() {
    setDraftTipo(tipo); setDraftStatus(status)
    setDraftIni(dataIni); setDraftFim(dataFim); setDraftUser(userIdInput)
    setFilterOpen(true)
  }
  function applyFilters() {
    setTipo(draftTipo); setStatus(draftStatus)
    setDataIni(draftIni); setDataFim(draftFim)
    setUserIdInput(draftUser); setUserIdApplied(draftUser.trim())
    setPage(1); setFilterOpen(false)
  }

  async function handleDelete(id: number) {
    if (!window.confirm(`Excluir transação #${id}?\n\nO saldo do cliente será recalculado automaticamente a partir das transações confirmadas restantes.\n\nEsta ação é IRREVERSÍVEL.`)) {
      return
    }
    setDeletingId(id)
    try {
      const r = await api<DeleteResponse>(`/tpc-transacoes/${id}`, { method: 'DELETE' })
      showToast('ok',
        `Transação #${id} excluída. Novo saldo do user_id ${r.user_id}: ${fmt(r.novo_saldo)}`)
      // Se essa era a última linha da página atual e há mais páginas, recua uma página.
      if (items.length === 1 && page > 1) {
        setPage(p => Math.max(1, p - 1))
      } else {
        await load()
      }
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao excluir transação')
    } finally {
      setDeletingId(null)
    }
  }

  async function handleVerificarPix() {
    if (!window.confirm('Disparar verificação de PIX pendentes agora?')) return
    setBusy(true)
    try {
      const r = await api<VerificarPixResponse>('/tpc-transacoes/verificar-pix', { method: 'POST' })
      const stubNote = r.stub ? ' (modo dev — wallet-service ainda não integrado)' : ''
      showToast('ok', `${r.queued} PIX pendente(s) enfileirado(s) para verificação${stubNote}.`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao verificar PIX')
    } finally {
      setBusy(false)
    }
  }

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil(total / PER_PAGE)),
    [total],
  )

  const hasAnyFilter =
    tipo !== '' || status !== '' || dataIni !== '' || dataFim !== '' || userIdApplied !== ''

  // Filtra client-side por carteira (descrição contém "COD" = transação COD)
  const visibleItems = useMemo(() => {
    if (carteira === 'todas') return items
    if (carteira === 'cod') {
      return items.filter(t => /cod/i.test(t.descricao || ''))
    }
    // 'expedicao' (default): oculta transações COD
    return items.filter(t => !/cod/i.test(t.descricao || ''))
  }, [items, carteira])

  // Chips
  const chips: ActiveChip[] = []
  if (tipo) chips.push({ key: 'tipo', label: `Tipo: ${tipo}`, onRemove: () => applyTipo('') })
  if (status) chips.push({ key: 'status', label: `Status: ${status}`, onRemove: () => applyStatus('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => applyDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => applyDataFim('') })
  if (userIdApplied) chips.push({ key: 'user', label: `User: #${userIdApplied}`, onRemove: () => { setUserIdInput(''); setUserIdApplied(''); setPage(1) } })

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Transações Expedição</h1>
          <p>{total} transação(ões){hasAnyFilter ? ' filtrada(s)' : ' no total'}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton
            active={chips.length > 0}
            count={chips.length}
            onClick={openPanel}
          />
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={handleVerificarPix}
            disabled={busy}
          >
            {busy ? 'Verificando…' : 'Verificar PIX agora'}
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearAllFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      <div className="szv2-alert" style={{ marginBottom: 16 }}>
        Estas são transações da carteira de Expedição (frete pré-pago).
        Para transações COD veja: Transações COD.
      </div>

      {/* Carteira (client-side) — mantido fora do painel pois é toggle visual */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: 0.5 }}>
          Carteira
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          <Chip active={carteira === 'expedicao'} onClick={() => setCarteira('expedicao')} disabled={loading}>
            Expedição (frete)
          </Chip>
          <Chip active={carteira === 'cod'} onClick={() => setCarteira('cod')} disabled={loading}>
            COD
          </Chip>
          <Chip active={carteira === 'todas'} onClick={() => setCarteira('todas')} disabled={loading}>
            Todas
          </Chip>
        </div>
      </div>

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearAllFilters}
        title="Filtros"
      >
        <FilterField label="Data inicial">
          <input
            type="date"
            style={filterInputStyle}
            value={draftIni}
            onChange={e => setDraftIni(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFim}
            onChange={e => setDraftFim(e.target.value)}
          />
        </FilterField>
        <FilterField label="Tipo">
          <select
            style={filterInputStyle}
            value={draftTipo}
            onChange={e => setDraftTipo(e.target.value as TipoFilter)}
          >
            {TIPO_CHIPS.map(c => <option key={c.key || 'all-tipo'} value={c.key}>{c.label}</option>)}
          </select>
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value as StatusFilter)}
          >
            {STATUS_CHIPS.map(c => <option key={c.key || 'all-status'} value={c.key}>{c.label}</option>)}
          </select>
        </FilterField>
        <FilterField label="Busca (user_id)">
          <input
            type="search"
            inputMode="numeric"
            style={filterInputStyle}
            placeholder="ID do WordPress"
            value={draftUser}
            onChange={e => setDraftUser(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>

      {/* Tabela */}
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Usuário</th>
              <th>Tipo</th>
              <th>Status</th>
              <th className="szv2-td-num">Valor</th>
              <th className="szv2-td-num">Saldo após</th>
              <th>Descrição</th>
              <th>Data</th>
              <th style={{ width: 120 }}>Ações</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={9}>
                  <div className="szv2-empty"><h3>Carregando…</h3></div>
                </td>
              </tr>
            ) : visibleItems.length === 0 ? (
              <tr>
                <td colSpan={9}>
                  <div className="szv2-empty">
                    <h3>Nenhuma transação encontrada</h3>
                    {(hasAnyFilter || carteira !== 'todas') && <p>Tente ajustar os filtros acima.</p>}
                  </div>
                </td>
              </tr>
            ) : visibleItems.map(t => {
              const isCredito = t.tipo === 'credito'
              const isDebito  = t.tipo === 'debito'
              const tipoBadgeCls =
                isCredito ? 'szv2-badge-success'
                : isDebito ? 'szv2-badge-danger'
                : 'szv2-badge-neutral'
              const tipoLabel =
                isCredito ? '+ Crédito'
                : isDebito ? '− Débito'
                : t.tipo
              const valorColor =
                isCredito ? 'var(--szv2-success)'
                : isDebito ? 'var(--szv2-danger)'
                : 'inherit'
              const statusBadgeCls = STATUS_BADGE_CLS[t.status] || 'szv2-badge-neutral'
              const statusLabel = STATUS_LABEL[t.status] || t.status

              return (
                <tr key={t.id}>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                    #{t.id}
                  </td>
                  <td style={{ fontSize: 13 }}>
                    {t.nome || t.email ? (
                      <div>
                        <div style={{ fontWeight: 600 }}>
                          {t.nome || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                        </div>
                        {t.email && (
                          <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                            {t.email}
                          </div>
                        )}
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-faint)' }}>
                          ID #{t.user_id}
                        </div>
                      </div>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-faint)' }}>ID #{t.user_id}</span>
                    )}
                  </td>
                  <td>
                    <span className={`sz-badge ${tipoBadgeCls}`}>{tipoLabel}</span>
                  </td>
                  <td>
                    <span className={`sz-badge ${statusBadgeCls}`}>{statusLabel}</span>
                  </td>
                  <td
                    className="szv2-td-num"
                    style={{ fontWeight: 700, color: valorColor }}
                  >
                    {fmt(t.valor)}
                  </td>
                  <td className="szv2-td-num" style={{ color: 'var(--szv2-text-muted)' }}>
                    {fmt(t.saldo_apos)}
                  </td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-soft)', maxWidth: 320 }}>
                    {t.descricao || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    {t.order_id && (
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginTop: 2 }}>
                        Pedido #{t.order_id}
                      </div>
                    )}
                  </td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>
                    {fmtDate(t.created_at)}
                  </td>
                  <td>
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-sm szv2-btn-danger"
                      disabled={deletingId === t.id}
                      onClick={() => handleDelete(t.id)}
                    >
                      {deletingId === t.id ? '…' : 'Excluir'}
                    </button>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Paginação */}
      {total > PER_PAGE && (
        <div
          className="szv2-card"
          style={{ marginTop: 12, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}
        >
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={page <= 1 || loading}
            onClick={() => setPage(p => Math.max(1, p - 1))}
          >
            ← Anterior
          </button>
          <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            Página <strong>{page}</strong> de <strong>{totalPages}</strong>
            {' · '}
            <span style={{ color: 'var(--szv2-text-faint)' }}>
              {PER_PAGE} por página
            </span>
          </span>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={page >= totalPages || loading}
            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
          >
            Próximo →
          </button>
        </div>
      )}
    </div>
  )
}
