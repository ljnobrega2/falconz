import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// MotoboyConciliacao — espelha sz_mb_tab_conciliacao() (admin.php:2064).
// Conciliação bancária por pedido: confronta valores recebidos do cliente
// (entrega) ou taxa de tentativa (frustrado) e libera o ganho do motoboy
// (sz_motoboy_ganhos.status: pendente → disponivel).
//
// Endpoints consumidos:
//   GET  /motoboy-conciliacao?date_from=&date_to=
//   POST /motoboy-conciliacao/{pedido_id}/conciliar

// ----- Tipos do handler Go --------------------------------------------------

type KPIs = {
  pedidos_periodo:        number
  entregues:              number
  frustrados:             number
  aguardando_conciliacao: number
  total_dinheiro:         number
  total_pix:              number
  total_cartao:           number
  taxa_frustrado_pct:     number
}

type Item = {
  pedido_id:       number
  data:            string
  wc_order_id:     number
  motoboy_id:      number | null
  motoboy_nome:    string
  zona_nome:       string
  dest_nome:       string
  status:          string
  forma:           string
  pgto_dinheiro:   number
  pgto_pix:        number
  pgto_cartao:     number
  taxa_frustrado:  number
  total_validado:  number
  conciliacao:     string
}

type Resp = {
  kpis:  KPIs
  items: Item[]
}

// ----- Helpers --------------------------------------------------------------

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtMoney = (v: number) => 'R$ ' + fmt(v)

const fmtDateBR = (s: string | null | undefined) => {
  if (!s) return '—'
  const m = s.match(/^(\d{4})-(\d{2})-(\d{2})/)
  if (!m) return s
  const [, y, mo, d] = m
  return `${d}/${mo}/${y}`
}

// Data ISO no fuso America/Sao_Paulo — não cai no "ontem" se o usuário
// estiver em UTC+0 (mesmo padrão de MotoboyDashboard.tsx).
function todayInSaoPaulo(): string {
  const fmt = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'America/Sao_Paulo',
    year: 'numeric', month: '2-digit', day: '2-digit',
  })
  return fmt.format(new Date())
}

function daysAgoSP(n: number): string {
  const fmt = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'America/Sao_Paulo',
    year: 'numeric', month: '2-digit', day: '2-digit',
  })
  const d = new Date(Date.now() - n * 24 * 60 * 60 * 1000)
  return fmt.format(d)
}

// Badge de conciliacao (status do ganho).
const CONC_BADGE: Record<string, string> = {
  pendente:   'szv2-badge-warning',
  disponivel: 'szv2-badge-success',
  pago:       'szv2-badge-neutral',
}

const CONC_LABEL: Record<string, string> = {
  pendente:   'Aguardando',
  disponivel: 'Conciliado',
  pago:       'Pago',
}

// ----- KpiCard --------------------------------------------------------------

function KpiCard({
  label, value, color, sub,
}: {
  label: string
  value: string
  color?: 'success' | 'danger' | 'warning' | 'brand'
  sub?: string
}) {
  const c =
    color === 'success' ? 'var(--szv2-success)'
    : color === 'danger'  ? 'var(--szv2-danger)'
    : color === 'warning' ? 'var(--szv2-warning)'
    :                       'var(--szv2-brand)'
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

// ----- Página principal -----------------------------------------------------

export default function MotoboyConciliacao() {
  const [dateFrom, setDateFrom] = useState<string>(daysAgoSP(7))
  const [dateTo, setDateTo]     = useState<string>(todayInSaoPaulo())
  const [resp, setResp] = useState<Resp | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busyConc, setBusyConc] = useState<number | null>(null)
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  // Filtro client-side: status de conciliação (pendente/disponivel/pago).
  const [conciliacaoStatus, setConciliacaoStatus] = useState<string>('')

  // Painel
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftFrom, setDraftFrom] = useState(daysAgoSP(7))
  const [draftTo, setDraftTo] = useState(todayInSaoPaulo())
  const [draftConcStatus, setDraftConcStatus] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = `?date_from=${encodeURIComponent(dateFrom)}&date_to=${encodeURIComponent(dateTo)}`
      const r = await api<Resp>(`/motoboy-conciliacao${qs}`)
      setResp(r)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  function quickLast7() {
    setDateFrom(daysAgoSP(7))
    setDateTo(todayInSaoPaulo())
  }

  async function handleConciliar(pedidoID: number, wcOrderID: number) {
    if (!window.confirm(
      `Conciliar somente o pedido #${wcOrderID || pedidoID} e liberar a taxa para a carteira?`,
    )) return
    setBusyConc(pedidoID)
    try {
      const r = await api<{ ok: boolean; ganhos_atualizados: number; hint?: string }>(
        `/motoboy-conciliacao/${pedidoID}/conciliar`,
        { method: 'POST' },
      )
      if (r.ganhos_atualizados === 0) {
        showToast('err', r.hint || 'Nenhum lançamento pendente para esse pedido.')
      } else {
        showToast('ok', `Pedido #${wcOrderID || pedidoID} conciliado (${r.ganhos_atualizados} lançamento(s) liberado(s)).`)
      }
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao conciliar')
    } finally {
      setBusyConc(null)
    }
  }

  // CSV export client-side: extrai TR/TH/TD da própria tabela renderizada.
  function exportCSV() {
    if (!resp || resp.items.length === 0) return
    const head = [
      'Data', 'Pedido', 'Motoboy', 'Zona', 'Cliente', 'Status', 'Forma',
      'Dinheiro', 'PIX', 'Cartão', 'Taxa frustrado', 'Total validado', 'Conciliação',
    ]
    const lines = [head.join(',')]
    for (const it of resp.items) {
      const row = [
        fmtDateBR(it.data),
        `#${it.wc_order_id || it.pedido_id}`,
        it.motoboy_nome || '—',
        it.zona_nome || '—',
        it.dest_nome || '—',
        it.status.toUpperCase(),
        it.forma,
        fmtMoney(it.pgto_dinheiro),
        fmtMoney(it.pgto_pix),
        fmtMoney(it.pgto_cartao),
        fmtMoney(it.taxa_frustrado),
        fmtMoney(it.total_validado),
        CONC_LABEL[it.conciliacao] || it.conciliacao,
      ]
      // Escapa aspas e envolve em ".." (paridade com szMbExportCSV no PHP).
      lines.push(
        row
          .map(s => `"${String(s).replace(/"/g, '""').replace(/\n/g, ' ')}"`)
          .join(','),
      )
    }
    // U+FEFF (BOM) — Excel detecta UTF-8 corretamente. Usar o escape Unicode
    // ﻿ (em vez do caractere literal) — alguns editores removem o BOM solto.
    const blob = new Blob(['﻿' + lines.join('\n')], { type: 'text/csv;charset=utf-8' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `conciliacao-pedidos-${dateFrom}-${dateTo}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(a.href)
  }

  const kpis = resp?.kpis
  const allItems = resp?.items || []
  // Aplica filtro de status de conciliação no client.
  const items = conciliacaoStatus
    ? allItems.filter(it => it.conciliacao === conciliacaoStatus)
    : allItems

  function openPanel() {
    setDraftFrom(dateFrom); setDraftTo(dateTo); setDraftConcStatus(conciliacaoStatus)
    setFilterOpen(true)
  }
  function applyFilters() {
    setDateFrom(draftFrom); setDateTo(draftTo); setConciliacaoStatus(draftConcStatus)
    setFilterOpen(false)
    setTimeout(load, 0)
  }
  function clearFilters() {
    const def_from = daysAgoSP(7); const def_to = todayInSaoPaulo()
    setDateFrom(def_from); setDateTo(def_to); setConciliacaoStatus('')
    setDraftFrom(def_from); setDraftTo(def_to); setDraftConcStatus('')
    setFilterOpen(false)
  }

  const chips: ActiveChip[] = []
  if (dateFrom !== daysAgoSP(7)) chips.push({ key: 'from', label: `De: ${dateFrom}`, onRemove: () => { setDateFrom(daysAgoSP(7)); setTimeout(load, 0) } })
  if (dateTo !== todayInSaoPaulo()) chips.push({ key: 'to', label: `Até: ${dateTo}`, onRemove: () => { setDateTo(todayInSaoPaulo()); setTimeout(load, 0) } })
  if (conciliacaoStatus) chips.push({ key: 'cs', label: `Conciliação: ${CONC_LABEL[conciliacaoStatus] || conciliacaoStatus}`, onRemove: () => setConciliacaoStatus('') })

  // Total aguardando R$ — útil pra header (paridade com PHP $totais['aguardando']).
  const totalAguardando = useMemo(() => {
    return items
      .filter(it => it.conciliacao === 'pendente')
      .reduce((acc, it) => acc + it.total_validado, 0)
  }, [items])

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Conciliação bancária — Motoboy</h1>
          <p>
            Cada linha é um pedido. Entregas validam o valor recebido do cliente;
            frustrados validam apenas a taxa de frustrado gravada no pedido.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton
            active={chips.length > 0}
            count={chips.length}
            onClick={openPanel}
          />
          {items.length > 0 && (
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary"
              onClick={exportCSV}
            >
              ↓ CSV
            </button>
          )}
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
        <FilterField label="Status de conciliação">
          <select
            style={filterInputStyle}
            value={draftConcStatus}
            onChange={e => setDraftConcStatus(e.target.value)}
          >
            <option value="">Todos</option>
            <option value="pendente">Aguardando</option>
            <option value="disponivel">Conciliado</option>
            <option value="pago">Pago</option>
          </select>
        </FilterField>
      </FilterTopPanel>

      {/* 8 KPI cards (2 fileiras de 4) */}
      {kpis && (
        <>
          <div
            className="szv2-kpi-grid"
            style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))', marginBottom: 16 }}
          >
            <KpiCard
              label="Pedidos no período"
              value={kpis.pedidos_periodo.toLocaleString('pt-BR')}
            />
            <KpiCard
              label="Entregues"
              value={kpis.entregues.toLocaleString('pt-BR')}
              color="success"
            />
            <KpiCard
              label="Frustrados"
              value={kpis.frustrados.toLocaleString('pt-BR')}
              color="danger"
            />
            <KpiCard
              label="Aguardando conciliação"
              value={kpis.aguardando_conciliacao.toLocaleString('pt-BR')}
              color="warning"
              sub={totalAguardando > 0 ? fmtMoney(totalAguardando) : undefined}
            />
          </div>
          <div
            className="szv2-kpi-grid"
            style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))' }}
          >
            <KpiCard
              label="Dinheiro"
              value={fmtMoney(kpis.total_dinheiro)}
            />
            <KpiCard
              label="PIX"
              value={fmtMoney(kpis.total_pix)}
            />
            <KpiCard
              label="Cartão"
              value={fmtMoney(kpis.total_cartao)}
            />
            <KpiCard
              label="Taxa frustrado"
              value={`${kpis.taxa_frustrado_pct.toFixed(1)}%`}
              color="danger"
              sub="frustrados / pedidos"
            />
          </div>
        </>
      )}

      {/* Tabela conciliação */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>
              Conciliação por pedido — {fmtDateBR(dateFrom)} a {fmtDateBR(dateTo)}
            </h2>
            <p className="szv2-card-sub">{items.length} pedido(s)</p>
          </div>
        </div>

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : items.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum pedido válido no período.
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Pedido</th>
                  <th>Motoboy</th>
                  <th>Zona</th>
                  <th>Cliente</th>
                  <th>Status</th>
                  <th>Forma</th>
                  <th style={{ textAlign: 'right' }}>Dinheiro</th>
                  <th style={{ textAlign: 'right' }}>PIX</th>
                  <th style={{ textAlign: 'right' }}>Cartão</th>
                  <th style={{ textAlign: 'right' }}>Taxa frust.</th>
                  <th style={{ textAlign: 'right' }}>Total validado</th>
                  <th>Conciliação</th>
                  <th style={{ width: 140 }}>Ação</th>
                </tr>
              </thead>
              <tbody>
                {items.map(it => {
                  const conciliado = it.conciliacao === 'disponivel' || it.conciliacao === 'pago'
                  const badge = CONC_BADGE[it.conciliacao] || 'szv2-badge-neutral'
                  return (
                    <tr key={`p-${it.pedido_id}`}>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                        {fmtDateBR(it.data)}
                      </td>
                      <td><strong>#{it.wc_order_id || it.pedido_id}</strong></td>
                      <td>{it.motoboy_nome || '—'}</td>
                      <td>{it.zona_nome || '—'}</td>
                      <td>{it.dest_nome || '—'}</td>
                      <td>
                        <span
                          className={`sz-badge ${it.status === 'frustrado' ? 'szv2-badge-danger' : 'szv2-badge-success'}`}
                        >
                          {it.status.toUpperCase()}
                        </span>
                      </td>
                      <td style={{ fontSize: 12 }}>{it.forma}</td>
                      <td style={{ textAlign: 'right' }}>{fmtMoney(it.pgto_dinheiro)}</td>
                      <td style={{ textAlign: 'right' }}>{fmtMoney(it.pgto_pix)}</td>
                      <td style={{ textAlign: 'right' }}>{fmtMoney(it.pgto_cartao)}</td>
                      <td
                        style={{
                          textAlign: 'right',
                          fontWeight: it.taxa_frustrado > 0 ? 700 : 400,
                          color: it.taxa_frustrado > 0 ? 'var(--szv2-danger)' : 'inherit',
                        }}
                      >
                        {fmtMoney(it.taxa_frustrado)}
                      </td>
                      <td
                        style={{
                          textAlign: 'right',
                          fontWeight: 700,
                          color: 'var(--szv2-brand)',
                        }}
                      >
                        {fmtMoney(it.total_validado)}
                      </td>
                      <td>
                        <span className={`sz-badge ${badge}`}>
                          {CONC_LABEL[it.conciliacao] || it.conciliacao}
                        </span>
                      </td>
                      <td>
                        {conciliado ? (
                          <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                            Liberado
                          </span>
                        ) : (
                          <button
                            type="button"
                            className="szv2-btn-brand"
                            disabled={busyConc === it.pedido_id}
                            onClick={() => handleConciliar(it.pedido_id, it.wc_order_id)}
                          >
                            {busyConc === it.pedido_id ? 'Conciliando…' : 'Conciliar'}
                          </button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
