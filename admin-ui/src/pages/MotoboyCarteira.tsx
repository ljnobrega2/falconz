import { useEffect, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// ----- Tipos retornados pelo handler Go -------------------------------------

type Summary = {
  total_disponivel: number
  total_a_conciliar: number
  total_pago: number
}

type Row = {
  motoboy_id: number
  motoboy_nome: string
  telefone: string
  zona_nome: string
  saldo_disponivel: number
  saldo_a_conciliar: number
  saldo_pago: number
  qtd_disponivel: number
  qtd_pendente: number
  qtd_pago: number
}

type HistRow = {
  id: number
  data: string
  pedido_id: number
  wc_order_id: number
  tipo: string
  valor: number
  pago_neste_ganho: number
  saldo_aberto: number
  data_saque: string | null
  status: string
  recebido_cliente: number
}

type PagamentoResponse = {
  ok: boolean
  pagamento_id: number
  ganhos_atualizados: number
  valor_aplicado: number
}

type SyncResponse = {
  ok: boolean
  ganhos_removidos: number
  ganhos_upserted: number
  ganhos_conciliados: number
  fechamentos: number
}

// ----- Helpers ---------------------------------------------------------------

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtMoney = (v: number) => 'R$ ' + fmt(v)

const fmtDateBR = (s: string | null | undefined) => {
  if (!s) return '—'
  // Aceita "YYYY-MM-DD" ou "YYYY-MM-DD HH:MM:SS[.ffffff]" (timestamp do Postgres).
  const m = s.match(/^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2}))?/)
  if (!m) return s
  const [, y, mo, d, h, mi] = m
  return h ? `${d}/${mo}/${y} ${h}:${mi}` : `${d}/${mo}/${y}`
}

const todayISO = () => {
  const d = new Date()
  const y = d.getFullYear()
  const mo = String(d.getMonth() + 1).padStart(2, '0')
  const da = String(d.getDate()).padStart(2, '0')
  return `${y}-${mo}-${da}`
}

const STATUS_BADGE: Record<string, string> = {
  disponivel: 'szv2-badge-success',
  pendente: 'szv2-badge-warning',
  pago: 'szv2-badge-neutral',
}

// ----- KPI card (mesma estética da AuditEngine) ------------------------------

function KpiCard({
  label,
  value,
  sub,
  color,
}: {
  label: string
  value: string
  sub?: string
  color?: 'success' | 'warning' | 'brand'
}) {
  const c =
    color === 'success' ? 'var(--szv2-success)'
    : color === 'warning' ? 'var(--szv2-warning)'
    : 'var(--szv2-brand)'
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span className="szv2-kpi-value" style={{ color: c }}>{value}</span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

// ----- Página principal ------------------------------------------------------

export default function MotoboyCarteira() {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [rows, setRows] = useState<Row[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  // Filtros aplicados.
  const [q, setQ] = useState('')
  const [motoboyID, setMotoboyID] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftMotoboy, setDraftMotoboy] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  // Estado de modais
  const [payModal, setPayModal] = useState<Row | null>(null)
  const [histModal, setHistModal] = useState<Row | null>(null)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const s = await api<Summary>('/motoboy-carteira/summary')
      setSummary(s)
      const p = new URLSearchParams()
      if (q) p.set('q', q)
      if (motoboyID) p.set('motoboy_id', motoboyID)
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      p.set('limit', '200')
      const r = await api<{ items: Row[]; count: number }>(`/motoboy-carteira?${p.toString()}`)
      setRows(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [q, motoboyID, dataIni, dataFim])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  function openPanel() {
    setDraftQ(q); setDraftMotoboy(motoboyID); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setMotoboyID(draftMotoboy); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setMotoboyID(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftMotoboy(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (motoboyID) chips.push({ key: 'mb', label: `Motoboy #${motoboyID}`, onRemove: () => setMotoboyID('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  async function handleSync() {
    if (!window.confirm('Recalcular os saldos da carteira dos motoboys?')) return
    setBusy(true)
    try {
      const res = await api<SyncResponse>('/motoboy-carteira/sync', { method: 'POST' })
      showToast(
        'ok',
        `Sync concluído — ${res.ganhos_upserted} ganho(s) atualizados, ` +
        `${res.ganhos_conciliados} conciliados, ${res.ganhos_removidos} removidos, ` +
        `${res.fechamentos} fechamento(s) atualizados.`,
      )
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao sincronizar')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      {/* Header */}
      <div className="szv2-section-head">
        <div>
          <h1>Carteira dos Motoboys</h1>
          <p>
            Disponível = saldo conciliado e ainda não pago. Pagamentos podem ser parciais.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          <button
            type="button"
            className="szv2-btn-secondary"
            disabled={busy || loading}
            onClick={handleSync}
          >
            {busy ? 'Atualizando…' : '🔄 Atualizar dados'}
          </button>
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

      {/* KPIs */}
      {summary && (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))' }}>
          <KpiCard
            label="Disponível"
            value={fmtMoney(summary.total_disponivel)}
            sub="para pagamento"
            color="success"
          />
          <KpiCard
            label="A conciliar"
            value={fmtMoney(summary.total_a_conciliar)}
            sub="ainda pendente"
            color="warning"
          />
          <KpiCard
            label="Pago acumulado"
            value={fmtMoney(summary.total_pago)}
            sub="histórico"
            color="brand"
          />
        </div>
      )}

      {/* Tabela principal */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Motoboys</h2>
            <p className="szv2-card-sub">{rows.length} registro(s)</p>
          </div>
        </div>

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : rows.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum motoboy encontrado.
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Motoboy</th>
                  <th>Zona</th>
                  <th>Telefone</th>
                  <th style={{ textAlign: 'right' }}>Disponível</th>
                  <th style={{ textAlign: 'right' }}>A conciliar</th>
                  <th style={{ textAlign: 'right' }}>Pago</th>
                  <th>Lançamentos</th>
                  <th style={{ width: 220 }}>Ações</th>
                </tr>
              </thead>
              <tbody>
                {rows.map(r => {
                  const hasDisp = r.saldo_disponivel > 0
                  return (
                    <tr key={r.motoboy_id}>
                      <td>
                        <strong>{r.motoboy_nome || `Motoboy #${r.motoboy_id}`}</strong>
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                          ID {r.motoboy_id}
                        </div>
                      </td>
                      <td>{r.zona_nome || '—'}</td>
                      <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>
                        {r.telefone || '—'}
                      </td>
                      <td style={{ textAlign: 'right', fontWeight: 700, color: 'var(--szv2-success)' }}>
                        {fmtMoney(r.saldo_disponivel)}
                      </td>
                      <td style={{ textAlign: 'right', fontWeight: 700, color: 'var(--szv2-warning)' }}>
                        {fmtMoney(r.saldo_a_conciliar)}
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        {fmtMoney(r.saldo_pago)}
                      </td>
                      <td>
                        <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                          <span
                            className="sz-badge szv2-badge-success"
                            title="Lançamentos disponíveis"
                            style={{ fontSize: 11, padding: '2px 6px' }}
                          >
                            {r.qtd_disponivel} disp.
                          </span>
                          <span
                            className="sz-badge szv2-badge-warning"
                            title="Lançamentos pendentes"
                            style={{ fontSize: 11, padding: '2px 6px' }}
                          >
                            {r.qtd_pendente} pend.
                          </span>
                          <span
                            className="sz-badge szv2-badge-neutral"
                            title="Lançamentos pagos"
                            style={{ fontSize: 11, padding: '2px 6px' }}
                          >
                            {r.qtd_pago} pagos
                          </span>
                        </div>
                      </td>
                      <td>
                        <div style={{ display: 'flex', gap: 6 }}>
                          <button
                            type="button"
                            className="szv2-btn-brand"
                            disabled={!hasDisp || busy}
                            onClick={() => setPayModal(r)}
                          >
                            Pagamento
                          </button>
                          <button
                            type="button"
                            className="szv2-btn-secondary"
                            onClick={() => setHistModal(r)}
                          >
                            Histórico
                          </button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Modais */}
      {payModal && (
        <PagamentoModal
          motoboy={payModal}
          onClose={() => setPayModal(null)}
          onSuccess={(p) => {
            showToast(
              'ok',
              `Pagamento #${p.pagamento_id} registrado · ${p.ganhos_atualizados} ganho(s) atualizado(s) · ${fmtMoney(p.valor_aplicado)} aplicado(s).`,
            )
            setPayModal(null)
            load()
          }}
          onError={(m) => showToast('err', m)}
        />
      )}
      {histModal && (
        <HistoricoModal motoboy={histModal} onClose={() => setHistModal(null)} />
      )}

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
        <FilterField label="Motoboy ID">
          <input
            type="number"
            style={filterInputStyle}
            placeholder="ex.: 12"
            value={draftMotoboy}
            onChange={e => setDraftMotoboy(e.target.value)}
          />
        </FilterField>
        <FilterField label="Busca (nome)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: João"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}

// ----- Modal: Registrar pagamento --------------------------------------------

function PagamentoModal({
  motoboy,
  onClose,
  onSuccess,
  onError,
}: {
  motoboy: Row
  onClose: () => void
  onSuccess: (p: PagamentoResponse) => void
  onError: (msg: string) => void
}) {
  // Default = saldo disponível (paridade com input value do PHP em admin.php:1965).
  const [valor, setValor] = useState<string>(motoboy.saldo_disponivel.toFixed(2))
  const [data, setData] = useState<string>(todayISO())
  const [obs, setObs] = useState<string>('')
  const [busy, setBusy] = useState(false)
  const [localErr, setLocalErr] = useState('')

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr('')

    // Aceita "1.234,56" (BR) ou "1234.56" — converte sempre para ponto decimal.
    const v = parseFloat(valor.replace(/\./g, '').replace(',', '.'))
    if (!v || v <= 0) { setLocalErr('Valor inválido'); return }
    if (v > motoboy.saldo_disponivel + 0.0001) {
      setLocalErr(`Valor maior que o disponível (${fmtMoney(motoboy.saldo_disponivel)}).`)
      return
    }
    if (!/^\d{4}-\d{2}-\d{2}$/.test(data)) {
      setLocalErr('Data inválida')
      return
    }

    setBusy(true)
    try {
      const r = await api<PagamentoResponse>(
        `/motoboy-carteira/${motoboy.motoboy_id}/pagamento`,
        {
          method: 'POST',
          body: JSON.stringify({
            valor_pago: v,
            data_pagamento: data,
            obs,
          }),
        },
      )
      onSuccess(r)
    } catch (e: any) {
      const msg = e.message || 'Falha ao registrar pagamento'
      setLocalErr(msg)
      onError(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="szv2-modal-overlay szv2-open" onClick={onClose}>
      <div className="szv2-modal" onClick={e => e.stopPropagation()}>
        <div className="szv2-modal-head">
          <h3>Registrar pagamento — {motoboy.motoboy_nome || `Motoboy #${motoboy.motoboy_id}`}</h3>
          <button className="szv2-modal-x" onClick={onClose}>✕</button>
        </div>
        <form onSubmit={submit}>
          <div className="szv2-modal-body">
            {localErr && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{localErr}</div>}

            <div
              style={{
                background: 'var(--szv2-surface-alt)',
                borderRadius: 8,
                padding: 12,
                marginBottom: 16,
                fontSize: 13,
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
              }}
            >
              <span style={{ color: 'var(--szv2-text-muted)' }}>Disponível</span>
              <strong style={{ color: 'var(--szv2-success)', fontSize: 16 }}>
                {fmtMoney(motoboy.saldo_disponivel)}
              </strong>
            </div>

            <div className="szv2-field" style={{ marginBottom: 12 }}>
              <label className="szv2-label">Valor a pagar (R$) *</label>
              <input
                className="szv2-input"
                type="text"
                inputMode="decimal"
                required
                value={valor}
                onChange={e => setValor(e.target.value)}
                placeholder="0,00"
              />
              <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                Use ponto ou vírgula como separador decimal. Default = saldo disponível.
              </small>
            </div>

            <div className="szv2-field" style={{ marginBottom: 12 }}>
              <label className="szv2-label">Data do pagamento *</label>
              <input
                className="szv2-input"
                type="date"
                required
                value={data}
                onChange={e => setData(e.target.value)}
              />
            </div>

            <div className="szv2-field" style={{ marginBottom: 0 }}>
              <label className="szv2-label">Observação</label>
              <textarea
                className="szv2-input"
                rows={3}
                value={obs}
                onChange={e => setObs(e.target.value)}
                placeholder="Ex.: pagamento parcial referente à semana 23"
              />
            </div>
          </div>
          <div className="szv2-modal-foot">
            <button type="button" className="szv2-btn-secondary" onClick={onClose} disabled={busy}>
              Cancelar
            </button>
            <button type="submit" className="szv2-btn-brand" disabled={busy}>
              {busy ? 'Registrando…' : 'Registrar pagamento'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ----- Modal: Histórico ------------------------------------------------------

function HistoricoModal({
  motoboy,
  onClose,
}: {
  motoboy: Row
  onClose: () => void
}) {
  const [items, setItems] = useState<HistRow[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<{ items: HistRow[]; count: number }>(
        `/motoboy-carteira/${motoboy.motoboy_id}/historico?limit=150`,
      )
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar histórico')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [motoboy.motoboy_id])

  return (
    <div className="szv2-modal-overlay szv2-open" onClick={onClose}>
      <div className="szv2-modal szv2-modal-lg" onClick={e => e.stopPropagation()}>
        <div className="szv2-modal-head">
          <h3>
            Histórico — {motoboy.motoboy_nome || `Motoboy #${motoboy.motoboy_id}`}
            <span
              style={{
                fontWeight: 400,
                fontSize: 13,
                color: 'var(--szv2-text-muted)',
                marginLeft: 8,
              }}
            >
              {items.length} lançamento(s)
            </span>
          </h3>
          <button className="szv2-modal-x" onClick={onClose}>✕</button>
        </div>
        <div className="szv2-modal-body">
          {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}

          {loading ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Carregando…
            </div>
          ) : items.length === 0 ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Sem lançamentos.
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Data</th>
                    <th>Pedido</th>
                    <th>Tipo</th>
                    <th style={{ textAlign: 'right' }}>Ganho</th>
                    <th style={{ textAlign: 'right' }}>Pago</th>
                    <th style={{ textAlign: 'right' }}>Saldo aberto</th>
                    <th>Data saque</th>
                    <th>Status</th>
                    <th style={{ textAlign: 'right' }}>Recebido cliente</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map(h => (
                    <tr key={h.id}>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                        {fmtDateBR(h.data)}
                      </td>
                      <td>
                        <strong>#{h.wc_order_id || h.pedido_id || h.id}</strong>
                      </td>
                      <td style={{ fontSize: 12 }}>{h.tipo || '—'}</td>
                      <td style={{ textAlign: 'right', fontWeight: 700 }}>
                        {fmtMoney(h.valor)}
                      </td>
                      <td style={{ textAlign: 'right' }}>{fmtMoney(h.pago_neste_ganho)}</td>
                      <td
                        style={{
                          textAlign: 'right',
                          fontWeight: 600,
                          color: h.saldo_aberto > 0 ? 'var(--szv2-warning)' : 'inherit',
                        }}
                      >
                        {fmtMoney(h.saldo_aberto)}
                      </td>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                        {fmtDateBR(h.data_saque)}
                      </td>
                      <td>
                        <span
                          className={`sz-badge ${STATUS_BADGE[h.status] || 'szv2-badge-neutral'}`}
                        >
                          {h.status || '—'}
                        </span>
                      </td>
                      <td style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>
                        {fmtMoney(h.recebido_cliente)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
        <div className="szv2-modal-foot">
          <button type="button" className="szv2-btn-secondary" onClick={onClose}>
            Fechar
          </button>
        </div>
      </div>
    </div>
  )
}
