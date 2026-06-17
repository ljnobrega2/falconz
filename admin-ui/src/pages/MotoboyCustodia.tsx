import { useEffect, useState } from 'react'
import { api, getToken } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'

// MotoboyCustodia — espelha sz_mb_tab_estoque_motoboy() (admin.php:1510).
// Custódia física de pacotes em rota / aguardando OL / com ocorrência.
//
// Endpoints consumidos:
//   GET  /motoboy-custodia/summary
//   GET  /motoboy-custodia/summary-by-motoboy
//   GET  /motoboy-custodia?status=&motoboy_id=&limit=300
//   POST /motoboy-custodia/route-assist
//   POST /motoboy-custodia/return       (multipart)
//
// Layout: 5 KPIs + 4 accordions (route-assist, devolução, resumo, lista).

// ----- Tipos retornados pelo handler Go -------------------------------------

type Kpi = { qty: number; pedidos: number }

type Summary = {
  com_motoboy:   Kpi
  frustrados:    Kpi
  aguardando_ol: Kpi
  avariados:     Kpi
  reservados:    Kpi
}

type Item = {
  id:                number
  wc_order_id:       number
  motoboy_id:        number | null
  motoboy_nome:      string
  product_id:        number
  product_name:      string
  quantity:          number
  physical_status:   string
  status_label:      string
  qr_code:           string
  ocorrencia_tipo:   string
  ocorrencia_nota:   string
  ocorrencia_fotos:  string[]
  cost_product:      number
  updated_at:        string
}

type ByMotoboy = {
  motoboy_id:          number | null
  motoboy_nome:        string
  pedidos:             number
  unidades:            number
  frustrados:          number
  aguardando_ol:       number
  custo_custodia:      number
  ultima_movimentacao: string
}

// ----- Lista mínima de motoboys (reaproveita /motoboys já existente) --------

type MotoboyOpt = { id: number; nome: string }

// ----- Helpers --------------------------------------------------------------

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtMoney = (v: number) => 'R$ ' + fmt(v)

const fmtDateBR = (s: string | null | undefined) => {
  if (!s) return '—'
  const m = s.match(/^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2}))?/)
  if (!m) return s
  const [, y, mo, d, h, mi] = m
  return h ? `${d}/${mo}/${y} ${h}:${mi}` : `${d}/${mo}/${y}`
}

const truncWords = (s: string, n: number) => {
  const words = (s || '').split(/\s+/).filter(Boolean)
  if (words.length <= n) return s
  return words.slice(0, n).join(' ') + '…'
}

// Badge color por physical_status — laranja brand pra rota, sucesso pra
// disponível, danger pra avariado, warning pros estados de espera.
const BADGE_CLASS: Record<string, string> = {
  with_motoboy:    'szv2-badge-warning',
  frustrated:      'szv2-badge-danger',
  return_declared: 'szv2-badge-warning',
  damaged:         'szv2-badge-danger',
  available:       'szv2-badge-success',
  reserved:        'szv2-badge-neutral',
  delivered:       'szv2-badge-success',
}

// ----- KpiCard --------------------------------------------------------------

function KpiCard({
  label, qty, pedSub, danger,
}: {
  label: string
  qty: number
  pedSub: string
  danger?: boolean
}) {
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span
          className="szv2-kpi-value"
          style={danger && qty > 0
            ? { color: 'var(--szv2-danger)' }
            : { color: 'var(--szv2-brand)' }}
        >
          {qty.toLocaleString('pt-BR')}
        </span>
        <span className="szv2-kpi-meta">{pedSub}</span>
      </div>
    </div>
  )
}

// ----- Página principal -----------------------------------------------------

export default function MotoboyCustodia() {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [items, setItems] = useState<Item[]>([])
  const [byMotoboy, setByMotoboy] = useState<ByMotoboy[]>([])
  const [motoboys, setMotoboys] = useState<MotoboyOpt[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  // Filtros aplicados.
  const [q, setQ] = useState('')
  const [motoboyID, setMotoboyID] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftMotoboy, setDraftMotoboy] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const p = new URLSearchParams()
      p.set('limit', '300')
      if (q) p.set('q', q)
      if (motoboyID) p.set('motoboy_id', motoboyID)
      if (statusFilter) p.set('status', statusFilter)
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      const [s, it, bm] = await Promise.all([
        api<Summary>('/motoboy-custodia/summary'),
        api<{ items: Item[]; count: number }>(`/motoboy-custodia?${p.toString()}`),
        api<{ items: ByMotoboy[]; count: number }>('/motoboy-custodia/summary-by-motoboy'),
      ])
      setSummary(s)
      setItems(it.items || [])
      setByMotoboy(bm.items || [])

      // Lista de motoboys (best-effort — não bloqueia a tela se falhar).
      try {
        const mList = await api<{ items?: MotoboyOpt[] } | MotoboyOpt[]>('/motoboys')
        const arr = Array.isArray(mList) ? mList : (mList?.items || [])
        setMotoboys(arr.map(m => ({ id: m.id, nome: m.nome })))
      } catch { /* opcional */ }
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [q, motoboyID, statusFilter, dataIni, dataFim])

  function openPanel() {
    setDraftQ(q); setDraftMotoboy(motoboyID); setDraftStatus(statusFilter); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setMotoboyID(draftMotoboy); setStatusFilter(draftStatus); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setMotoboyID(''); setStatusFilter(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftMotoboy(''); setDraftStatus(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (motoboyID) {
    const m = motoboys.find(mb => String(mb.id) === motoboyID)
    chips.push({ key: 'mb', label: `Motoboy: ${m?.nome || `#${motoboyID}`}`, onRemove: () => setMotoboyID('') })
  }
  if (statusFilter) chips.push({ key: 'status', label: `Status: ${statusFilter}`, onRemove: () => setStatusFilter('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  return (
    <div>
      {/* Header */}
      <div className="szv2-section-head">
        <div>
          <h1>Estoque Motoboy</h1>
          <p>
            Custódia física por QR: saída pelo motoboy, devolução declarada
            pelo motoboy e confirmação final pelo OL.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {/* 5 KPI Cards */}
      {summary && (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(5, minmax(0,1fr))' }}>
          <KpiCard
            label="Com motoboy"
            qty={summary.com_motoboy.qty}
            pedSub={`${summary.com_motoboy.pedidos} pedido(s)`}
          />
          <KpiCard
            label="Frustrados"
            qty={summary.frustrados.qty}
            pedSub="pendentes de bipagem"
            danger
          />
          <KpiCard
            label="Aguardando OL"
            qty={summary.aguardando_ol.qty}
            pedSub="devolução declarada"
            danger
          />
          <KpiCard
            label="Avariados"
            qty={summary.avariados.qty}
            pedSub="perda operacional"
            danger
          />
          <KpiCard
            label="Reservados"
            qty={summary.reservados.qty}
            pedSub="no CD para pedido"
          />
        </div>
      )}

      {/* Seção: Ações operacionais */}
      <h2 style={{ marginTop: 32, marginBottom: 8, fontWeight: 700, fontSize: 16 }}>
        Ações operacionais
      </h2>

      {/* Accordion 1: Rota assistida por QR */}
      <details className="szv2-card" style={{ marginTop: 8, padding: 0 }}>
        <summary
          style={{
            cursor: 'pointer',
            padding: '16px 20px',
            fontWeight: 700,
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
          }}
        >
          <span>
            Rota assistida por QR
            <small
              style={{
                display: 'block',
                fontWeight: 400,
                color: 'var(--szv2-text-muted)',
                fontSize: 12,
                marginTop: 2,
              }}
            >
              Use apenas se o celular do motoboy falhar.
            </small>
          </span>
          <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>abrir/fechar</span>
        </summary>
        <div style={{ padding: '0 20px 20px' }}>
          <p style={{ marginBottom: 12, color: 'var(--szv2-text-muted)', fontSize: 13 }}>
            O pedido continua exigindo QR da etiqueta e motoboy responsável.
          </p>
          <RouteAssistForm
            motoboys={motoboys}
            disabled={loading}
            onSuccess={() => {
              showToast('ok', 'Rota iniciada por QR.')
              load()
            }}
            onError={(m) => showToast('err', m)}
          />
        </div>
      </details>

      {/* Accordion 2: Confirmar devolução (open por default) */}
      <details className="szv2-card" style={{ marginTop: 12, padding: 0 }} open>
        <summary
          style={{
            cursor: 'pointer',
            padding: '16px 20px',
            fontWeight: 700,
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
          }}
        >
          <span>
            Confirmar devolução / condição
            <small
              style={{
                display: 'block',
                fontWeight: 400,
                color: 'var(--szv2-text-muted)',
                fontSize: 12,
                marginTop: 2,
              }}
            >
              Vendável volta ao estoque. Avariado/Extravio/Perda/Violado/Divergente exige foto + relato.
            </small>
          </span>
          <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>abrir/fechar</span>
        </summary>
        <div style={{ padding: '0 20px 20px' }}>
          <ReturnForm
            disabled={loading}
            onSuccess={() => {
              showToast('ok', 'Devolução registrada.')
              load()
            }}
            onError={(m) => showToast('err', m)}
          />
        </div>
      </details>

      {/* Seção: Consulta e auditoria */}
      <h2 style={{ marginTop: 32, marginBottom: 8, fontWeight: 700, fontSize: 16 }}>
        Consulta e auditoria
      </h2>

      {/* Accordion 3: Resumo por motoboy (open) */}
      <details className="szv2-card" style={{ marginTop: 8, padding: 0 }} open>
        <summary
          style={{
            cursor: 'pointer',
            padding: '16px 20px',
            fontWeight: 700,
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
          }}
        >
          <span>
            Resumo por motoboy
            <small
              style={{
                display: 'block',
                fontWeight: 400,
                color: 'var(--szv2-text-muted)',
                fontSize: 12,
                marginTop: 2,
              }}
            >
              Produtos e custos atualmente em custódia ({byMotoboy.length} motoboy(s)).
            </small>
          </span>
          <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>abrir/fechar</span>
        </summary>
        <div style={{ padding: '0 20px 20px' }}>
          {byMotoboy.length === 0 ? (
            <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Nenhum produto em custódia de motoboy.
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Motoboy</th>
                    <th style={{ textAlign: 'right' }}>Pedidos</th>
                    <th style={{ textAlign: 'right' }}>Unidades</th>
                    <th style={{ textAlign: 'right' }}>Frustrados</th>
                    <th style={{ textAlign: 'right' }}>Aguardando OL</th>
                    <th style={{ textAlign: 'right' }}>Custo em custódia</th>
                    <th>Última mov.</th>
                  </tr>
                </thead>
                <tbody>
                  {byMotoboy.map(r => (
                    <tr key={`m-${r.motoboy_id ?? 0}`}>
                      <td>
                        <strong>{r.motoboy_nome || 'Sem motoboy'}</strong>
                        {r.motoboy_id != null && (
                          <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                            ID {r.motoboy_id}
                          </div>
                        )}
                      </td>
                      <td style={{ textAlign: 'right' }}>{r.pedidos}</td>
                      <td style={{ textAlign: 'right', fontWeight: 700 }}>{r.unidades}</td>
                      <td style={{ textAlign: 'right', color: r.frustrados > 0 ? 'var(--szv2-danger)' : 'inherit' }}>
                        {r.frustrados}
                      </td>
                      <td style={{ textAlign: 'right', color: r.aguardando_ol > 0 ? 'var(--szv2-warning)' : 'inherit' }}>
                        {r.aguardando_ol}
                      </td>
                      <td style={{ textAlign: 'right' }}>{fmtMoney(r.custo_custodia)}</td>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                        {fmtDateBR(r.ultima_movimentacao)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </details>

      {/* Accordion 4: Pacotes em custódia / ocorrência */}
      <details className="szv2-card" style={{ marginTop: 16, padding: 0 }}>
        <summary
          style={{
            cursor: 'pointer',
            padding: '16px 20px',
            fontWeight: 700,
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
          }}
        >
          <span>
            Pacotes em custódia / ocorrência
            <small
              style={{
                display: 'block',
                fontWeight: 400,
                color: 'var(--szv2-text-muted)',
                fontSize: 12,
                marginTop: 2,
              }}
            >
              Lista detalhada: rota, frustrados, aguardando OL ou avariados ({items.length} pacote(s)).
            </small>
          </span>
          <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>abrir/fechar</span>
        </summary>
        <div style={{ padding: '0 20px 20px' }}>
          {loading && items.length === 0 ? (
            <TableSkeleton rows={5} cols={8} />
          ) : !loading && items.length === 0 ? (
            <EmptyState
              icon="📦"
              title="Nenhum pacote em custódia."
              description="Pacotes em rota, frustrados, aguardando OL ou avariados aparecem aqui."
            />
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Pedido</th>
                    <th>Status</th>
                    <th>Motoboy</th>
                    <th>Produto</th>
                    <th style={{ textAlign: 'right' }}>Qtd</th>
                    <th>QR</th>
                    <th>Ocorrência</th>
                    <th>Atualizado</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map(it => {
                    const badge = BADGE_CLASS[it.physical_status] || 'szv2-badge-neutral'
                    return (
                      <tr key={it.id}>
                        <td><strong>#{it.wc_order_id || it.id}</strong></td>
                        <td>
                          <span className={`sz-badge ${badge}`}>{it.status_label}</span>
                        </td>
                        <td>{it.motoboy_nome || '—'}</td>
                        <td>{it.product_name || 'Produto'}</td>
                        <td style={{ textAlign: 'right' }}>{it.quantity}</td>
                        <td>
                          <code style={{ fontSize: 11 }}>{it.qr_code || '—'}</code>
                        </td>
                        <td style={{ fontSize: 12 }}>
                          {it.ocorrencia_tipo ? (
                            <>
                              <strong style={{ textTransform: 'capitalize' }}>
                                {it.ocorrencia_tipo}
                              </strong>
                              {it.ocorrencia_nota && (
                                <span style={{ color: 'var(--szv2-text-muted)' }}>
                                  {' — '}
                                  {truncWords(it.ocorrencia_nota, 12)}
                                </span>
                              )}
                              {it.ocorrencia_fotos.length > 0 && (
                                <span style={{ marginLeft: 6 }}>
                                  {it.ocorrencia_fotos.map((url, i) => (
                                    <a
                                      key={i}
                                      href={url}
                                      target="_blank"
                                      rel="noopener noreferrer"
                                      style={{ marginRight: 4 }}
                                    >
                                      foto{i > 0 ? ` ${i + 1}` : ''}
                                    </a>
                                  ))}
                                </span>
                              )}
                            </>
                          ) : '—'}
                        </td>
                        <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                          {fmtDateBR(it.updated_at)}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </details>

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
            value={draftIni}
            max={draftFim || undefined}
            onChange={e => setDraftIni(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftFim}
            min={draftIni || undefined}
            onChange={e => setDraftFim(e.target.value)}
          />
        </FilterField>
        <FilterField label="Motoboy">
          <select
            style={filterInputStyle}
            value={draftMotoboy}
            onChange={e => setDraftMotoboy(e.target.value)}
          >
            <option value="">Todos</option>
            {motoboys.map(m => (
              <option key={m.id} value={String(m.id)}>{m.nome} (#{m.id})</option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value)}
          >
            <option value="">Todos</option>
            <option value="with_motoboy">Com motoboy</option>
            <option value="frustrated">Frustrado</option>
            <option value="return_declared">Devolução declarada</option>
            <option value="damaged">Avariado</option>
            <option value="reserved">Reservado</option>
          </select>
        </FilterField>
        <FilterField label="Busca (pedido / produto / QR)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: SZ-1234-…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}

// ----- Sub-form: Rota assistida ---------------------------------------------

function RouteAssistForm({
  motoboys,
  disabled,
  onSuccess,
  onError,
}: {
  motoboys: MotoboyOpt[]
  disabled: boolean
  onSuccess: () => void
  onError: (msg: string) => void
}) {
  const [qrCode, setQrCode] = useState('')
  const [motoboyID, setMotoboyID] = useState<string>('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!qrCode.trim() || !motoboyID) {
      onError('Informe QR e motoboy.')
      return
    }
    setBusy(true)
    try {
      await api('/motoboy-custodia/route-assist', {
        method: 'POST',
        body: JSON.stringify({
          qr_code:    qrCode.trim(),
          motoboy_id: parseInt(motoboyID, 10),
        }),
      })
      setQrCode('')
      setMotoboyID('')
      onSuccess()
    } catch (e: any) {
      onError(e.message || 'Falha ao iniciar rota.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
      <div className="szv2-field" style={{ flex: '2 1 280px' }}>
        <label className="szv2-label">QR / Código do pacote *</label>
        <input
          className="szv2-input"
          type="text"
          required
          value={qrCode}
          onChange={e => setQrCode(e.target.value)}
          placeholder="SZ-1234-55-..."
        />
      </div>
      <div className="szv2-field" style={{ flex: '1 1 220px' }}>
        <label className="szv2-label">Motoboy *</label>
        <select
          className="szv2-select"
          required
          value={motoboyID}
          onChange={e => setMotoboyID(e.target.value)}
        >
          <option value="">Selecione…</option>
          {motoboys.map(m => (
            <option key={m.id} value={m.id}>{m.nome}</option>
          ))}
        </select>
      </div>
      <div style={{ flex: '0 0 auto' }}>
        <button
          type="submit"
          className="szv2-btn-brand"
          disabled={disabled || busy}
        >
          {busy ? 'Iniciando…' : 'Iniciar por QR'}
        </button>
      </div>
    </form>
  )
}

// ----- Sub-form: Devolução --------------------------------------------------

const CONDITIONS = [
  { value: 'vendavel',   label: 'Vendável (volta ao estoque)' },
  { value: 'avariado',   label: 'Avariado' },
  { value: 'extravio',   label: 'Extravio' },
  { value: 'perda',      label: 'Perda' },
  { value: 'violado',    label: 'Violado' },
  { value: 'divergente', label: 'Divergente' },
]

function ReturnForm({
  disabled,
  onSuccess,
  onError,
}: {
  disabled: boolean
  onSuccess: () => void
  onError: (msg: string) => void
}) {
  const [qrCode, setQrCode] = useState('')
  const [condition, setCondition] = useState<string>('vendavel')
  const [note, setNote] = useState('')
  const [photo, setPhoto] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)

  const noteRequired = ['avariado', 'extravio', 'perda'].includes(condition)
  const photoRequired = condition !== 'vendavel'

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!qrCode.trim()) {
      onError('Informe o QR do pacote.')
      return
    }
    if (noteRequired && !note.trim()) {
      onError(`Relato obrigatório para condição "${condition}".`)
      return
    }
    if (photoRequired && !photo) {
      onError(`Foto obrigatória para condição "${condition}".`)
      return
    }

    // Multipart — não usar api() helper (esse força Content-Type JSON).
    const fd = new FormData()
    fd.append('qr_code', qrCode.trim())
    fd.append('condition', condition)
    fd.append('note', note)
    if (photo) fd.append('photo', photo)

    setBusy(true)
    try {
      const BASE = import.meta.env.VITE_API_BASE || '/wp-json/senderzz/v1/admin'
      const tok = getToken()
      const headers: Record<string, string> = {}
      if (tok) headers['Authorization'] = `Bearer ${tok}`

      const res = await fetch(`${BASE}/motoboy-custodia/return`, {
        method: 'POST',
        headers, // Content-Type setado pelo browser pro FormData (com boundary)
        body: fd,
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body?.error?.message || `HTTP ${res.status}`)
      }

      setQrCode('')
      setCondition('vendavel')
      setNote('')
      setPhoto(null)
      // Limpa o input file também — controlado via key seria mais limpo,
      // mas o reset acima já cobre a UX prática.
      const el = document.getElementById('sz-custodia-photo-input') as HTMLInputElement | null
      if (el) el.value = ''
      onSuccess()
    } catch (e: any) {
      onError(e.message || 'Falha ao registrar devolução.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={submit}
      encType="multipart/form-data"
      style={{ display: 'grid', gap: 12, gridTemplateColumns: 'repeat(2, minmax(0,1fr))' }}
    >
      <div className="szv2-field">
        <label className="szv2-label">QR / Código do pacote *</label>
        <input
          className="szv2-input"
          type="text"
          required
          value={qrCode}
          onChange={e => setQrCode(e.target.value)}
          placeholder="SZ-1234-55-..."
        />
      </div>
      <div className="szv2-field">
        <label className="szv2-label">Condição *</label>
        <select
          className="szv2-select"
          required
          value={condition}
          onChange={e => setCondition(e.target.value)}
        >
          {CONDITIONS.map(c => (
            <option key={c.value} value={c.value}>{c.label}</option>
          ))}
        </select>
      </div>
      <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
        <label className="szv2-label">
          Relato do OL {noteRequired && <span style={{ color: 'var(--szv2-danger)' }}>*</span>}
        </label>
        <textarea
          className="szv2-input"
          rows={2}
          value={note}
          onChange={e => setNote(e.target.value)}
          placeholder={noteRequired ? 'Obrigatório para avariado/extravio/perda.' : 'Opcional'}
          required={noteRequired}
        />
      </div>
      <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
        <label className="szv2-label">
          Foto / evidência {photoRequired && <span style={{ color: 'var(--szv2-danger)' }}>*</span>}
        </label>
        <input
          id="sz-custodia-photo-input"
          className="szv2-input"
          type="file"
          accept="image/*"
          onChange={e => setPhoto(e.target.files?.[0] || null)}
          required={photoRequired}
        />
        <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
          Máximo 8 MB. Obrigatório quando a condição não é "vendável".
        </small>
      </div>
      <div style={{ gridColumn: '1 / -1', display: 'flex', justifyContent: 'flex-end' }}>
        <button
          type="submit"
          className="szv2-btn-brand"
          disabled={disabled || busy}
        >
          {busy ? 'Registrando…' : 'Registrar'}
        </button>
      </div>
    </form>
  )
}
