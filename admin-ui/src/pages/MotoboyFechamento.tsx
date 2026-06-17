// MotoboyFechamento — fechamento diário do motoboy (Alan + Repasse).
// Espelha tabela sz_motoboy_fechamento (AUDIT-ADMIN-WP.md §25.2).
//
// Fluxo: 1) Gerar fechamento  →  2) Alan confirma  →  3) Repasse confirmado.
// Match estilo AuditEngine.tsx (sem szv2-section-head; cards inline).

import { useEffect, useMemo, useState } from 'react'
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

// ─── Tipos ────────────────────────────────────────────────────────────────

type FechamentoItem = {
  id: number
  motoboy_id: number
  motoboy_nome: string
  cd_id: number | null
  cd_nome: string
  data_fechamento: string
  total_pedidos: number
  total_entregues: number
  total_frustrados: number
  total_dinheiro: number
  total_pix: number
  total_cartao: number
  total_a_repassar: number
  repasse_confirmado: boolean
  repasse_ts: string | null
  alan_confirmou: boolean
  alan_ts: string | null
  obs: string
  created_at: string
}

type Summary = {
  total_pedidos: number
  total_a_repassar: number
  pendentes_alan: number
  pendentes_repasse: number
  finalizados: number
}

type Motoboy = { id: number; nome: string }

type StatusFilter = 'all' | 'pendente_alan' | 'pendente_repasse' | 'finalizados'

type ModalKind =
  | { kind: 'alan_confirm';        id: number }
  | { kind: 'alan_desconfirm';     id: number }
  | { kind: 'repasse_confirm';     id: number }
  | { kind: 'repasse_desconfirm';  id: number }
  | { kind: 'generate' }
  | null

// ─── Helpers ──────────────────────────────────────────────────────────────

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtDate = (s: string) => {
  // YYYY-MM-DD → DD/MM/YYYY  (sem timezone surprises)
  if (!s) return '—'
  const [y, m, d] = s.slice(0, 10).split('-')
  if (!y || !m || !d) return s
  return `${d}/${m}/${y}`
}

const fmtTs = (s: string | null | undefined) => {
  if (!s) return ''
  return s.slice(0, 16).replace('T', ' ')
}

function todayISO(): string {
  return new Date().toISOString().slice(0, 10)
}

function daysAgoISO(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d.toISOString().slice(0, 10)
}

// Labels dos chips de status.
const STATUS_FILTERS: { key: StatusFilter; label: string }[] = [
  { key: 'all',              label: 'Todos'             },
  { key: 'pendente_alan',    label: 'Pendente Alan'     },
  { key: 'pendente_repasse', label: 'Pendente Repasse'  },
  { key: 'finalizados',      label: 'Finalizados'       },
]

// ─── KPI Card ─────────────────────────────────────────────────────────────

function KpiCard(props: {
  label: string
  value: number | string
  sub?: string
  tone?: 'brand' | 'success' | 'warning' | 'danger'
}) {
  const { label, value, sub, tone = 'brand' } = props
  const color =
    tone === 'success' ? 'var(--szv2-success)' :
    tone === 'warning' ? 'var(--szv2-warning)' :
    tone === 'danger'  ? 'var(--szv2-danger)'  :
                         'var(--szv2-brand)'
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span className="szv2-kpi-value" style={{ color }}>{value}</span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

// ─── Modal (uma só, multi-kind) ───────────────────────────────────────────

function ActionModal(props: {
  modal: ModalKind
  busy: boolean
  motoboys: Motoboy[]
  onClose: () => void
  onConfirmText: (text: string) => void
  onConfirmGenerate: (motoboyID: number, date: string) => void
}) {
  const { modal, busy, motoboys, onClose, onConfirmText, onConfirmGenerate } = props
  const [text, setText] = useState('')
  const [genMotoboy, setGenMotoboy] = useState<number>(0)
  const [genDate, setGenDate] = useState<string>(todayISO())

  // Reset ao abrir/trocar alvo.
  useEffect(() => {
    setText('')
    if (modal && modal.kind === 'generate') {
      setGenMotoboy(0)
      setGenDate(todayISO())
    }
  }, [modal?.kind, modal && 'id' in modal ? modal.id : 0])

  if (!modal) return null

  // ── Modo gerar fechamento ─────────────────────────────────────────────
  if (modal.kind === 'generate') {
    const canSubmit = genMotoboy > 0 && !!genDate && !busy
    return (
      <div
        className="szv2-modal-overlay szv2-open"
        onClick={(e) => { if (e.target === e.currentTarget && !busy) onClose() }}
      >
        <div className="szv2-modal">
          <div className="szv2-modal-head">
            <h3>Gerar fechamento</h3>
            <button className="szv2-modal-x" onClick={onClose} disabled={busy} aria-label="Fechar">✕</button>
          </div>
          <div className="szv2-modal-body" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div className="szv2-field">
              <label className="szv2-label">Motoboy</label>
              <select
                className="szv2-select"
                value={genMotoboy}
                onChange={(e) => setGenMotoboy(parseInt(e.target.value || '0', 10))}
                disabled={busy}
              >
                <option value={0}>— selecione —</option>
                {motoboys.map(m => (
                  <option key={m.id} value={m.id}>{m.nome} (#{m.id})</option>
                ))}
              </select>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Data do fechamento</label>
              <input
                type="date"
                className="szv2-input"
                value={genDate}
                onChange={(e) => setGenDate(e.target.value)}
                disabled={busy}
              />
              <span className="szv2-text-xs szv2-text-muted">
                Calcula totais a partir dos pedidos do motoboy na data informada.
                Bloqueia se Alan já tiver confirmado.
              </span>
            </div>
          </div>
          <div className="szv2-modal-foot">
            <button type="button" className="szv2-btn szv2-btn-secondary" onClick={onClose} disabled={busy}>
              Cancelar
            </button>
            <button
              type="button"
              className="szv2-btn szv2-btn-brand"
              onClick={() => {
                if (!canSubmit) return
                if (!window.confirm(`Gerar fechamento do motoboy #${genMotoboy} em ${fmtDate(genDate)}?`)) return
                onConfirmGenerate(genMotoboy, genDate)
              }}
              disabled={!canSubmit}
            >
              {busy ? 'Gerando…' : 'Gerar'}
            </button>
          </div>
        </div>
      </div>
    )
  }

  // ── Modos com texto (obs/motivo) ──────────────────────────────────────
  type CfgShape = { title: string; textLabel: string; cta: string; danger: boolean; requireText: boolean }
  const CFG_MAP: Record<'alan_confirm' | 'alan_desconfirm' | 'repasse_confirm' | 'repasse_desconfirm', CfgShape> = {
    alan_confirm:       { title: 'Confirmar Alan',       textLabel: 'Observação (opcional)', cta: 'Confirmar Alan',       danger: false, requireText: false },
    alan_desconfirm:    { title: 'Desconfirmar Alan',    textLabel: 'Motivo',                cta: 'Desconfirmar Alan',    danger: true,  requireText: true  },
    repasse_confirm:    { title: 'Confirmar Repasse',    textLabel: 'Observação (opcional)', cta: 'Confirmar Repasse',    danger: false, requireText: false },
    repasse_desconfirm: { title: 'Desconfirmar Repasse', textLabel: 'Motivo',                cta: 'Desconfirmar Repasse', danger: true,  requireText: true  },
  }
  const cfg: CfgShape = CFG_MAP[modal.kind]

  const canSubmit = !busy && (!cfg.requireText || text.trim().length > 0)

  return (
    <div
      className="szv2-modal-overlay szv2-open"
      onClick={(e) => { if (e.target === e.currentTarget && !busy) onClose() }}
    >
      <div className="szv2-modal">
        <div className="szv2-modal-head">
          <h3>{cfg.title} — fechamento #{modal.id}</h3>
          <button className="szv2-modal-x" onClick={onClose} disabled={busy} aria-label="Fechar">✕</button>
        </div>
        <div className="szv2-modal-body" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div className="szv2-field">
            <label className="szv2-label">{cfg.textLabel}</label>
            <textarea
              className="szv2-input"
              rows={3}
              style={{ height: 'auto', padding: '8px 12px', resize: 'vertical' }}
              placeholder={cfg.requireText ? 'Descreva o motivo…' : 'Anote algo se quiser…'}
              value={text}
              onChange={(e) => setText(e.target.value)}
              disabled={busy}
            />
          </div>
        </div>
        <div className="szv2-modal-foot">
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={onClose} disabled={busy}>
            Cancelar
          </button>
          <button
            type="button"
            className={`szv2-btn ${cfg.danger ? 'szv2-btn-danger' : 'szv2-btn-brand'}`}
            onClick={() => onConfirmText(text.trim())}
            disabled={!canSubmit}
          >
            {busy ? 'Enviando…' : cfg.cta}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─── Página ───────────────────────────────────────────────────────────────

export default function MotoboyFechamento() {
  const [from, setFrom] = useState<string>(daysAgoISO(7))
  const [to, setTo]     = useState<string>(todayISO())
  const [status, setStatus] = useState<StatusFilter>('all')
  const [filterMotoboy, setFilterMotoboy] = useState<number>(0)

  const [items, setItems] = useState<FechamentoItem[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [motoboys, setMotoboys] = useState<Motoboy[]>([])

  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [modal, setModal] = useState<ModalKind>(null)

  // Drafts no painel.
  const [draftFrom, setDraftFrom] = useState<string>(from)
  const [draftTo, setDraftTo] = useState<string>(to)
  const [draftStatus, setDraftStatus] = useState<StatusFilter>(status)
  const [draftMotoboy, setDraftMotoboy] = useState<number>(filterMotoboy)
  const [filterOpen, setFilterOpen] = useState(false)

  function openPanel() {
    setDraftFrom(from); setDraftTo(to); setDraftStatus(status); setDraftMotoboy(filterMotoboy)
    setFilterOpen(true)
  }
  function applyFilters() {
    if (draftFrom) setFrom(draftFrom)
    if (draftTo) setTo(draftTo)
    setStatus(draftStatus); setFilterMotoboy(draftMotoboy)
    setFilterOpen(false)
  }
  function clearFilters() {
    const f = daysAgoISO(7); const t = todayISO()
    setFrom(f); setTo(t); setStatus('all'); setFilterMotoboy(0)
    setDraftFrom(f); setDraftTo(t); setDraftStatus('all'); setDraftMotoboy(0)
    setFilterOpen(false)
  }

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  // ── Loaders ────────────────────────────────────────────────────────────
  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = new URLSearchParams()
      qs.set('from', from)
      qs.set('to', to)
      if (status !== 'all') qs.set('status', status)
      if (filterMotoboy > 0) qs.set('motoboy_id', String(filterMotoboy))
      qs.set('limit', '200')

      const [list, sum] = await Promise.all([
        api<{ items: FechamentoItem[]; count: number }>(`/motoboy-fechamento?${qs.toString()}`),
        api<Summary>(`/motoboy-fechamento/summary?from=${from}&to=${to}`),
      ])
      setItems(list.items || [])
      setSummary(sum)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  // Motoboys são carregados uma vez (lista grande, pouco rotativa).
  async function loadMotoboys() {
    try {
      const r = await api<{ items: Array<{ id: number; nome: string; ativo?: boolean }> }>('/motoboys')
      setMotoboys((r.items || []).filter(m => m.ativo !== false).map(m => ({ id: m.id, nome: m.nome })))
    } catch {
      // Falha silenciosa: lista vazia ainda permite testar a UI.
      setMotoboys([])
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line */ }, [from, to, status, filterMotoboy])
  useEffect(() => { loadMotoboys() }, [])

  // ── Ações de fechamento ────────────────────────────────────────────────

  async function doAction(text: string) {
    if (!modal || modal.kind === 'generate') return
    const id = modal.id
    const path = (() => {
      switch (modal.kind) {
        case 'alan_confirm':       return `/motoboy-fechamento/${id}/alan-confirmar`
        case 'alan_desconfirm':    return `/motoboy-fechamento/${id}/alan-desconfirmar`
        case 'repasse_confirm':    return `/motoboy-fechamento/${id}/repasse-confirmar`
        case 'repasse_desconfirm': return `/motoboy-fechamento/${id}/repasse-desconfirmar`
      }
    })()
    // Body: confirmar usa {obs}, desconfirmar usa {motivo}.
    const body =
      modal.kind === 'alan_confirm' || modal.kind === 'repasse_confirm'
        ? { obs: text }
        : { motivo: text }

    setBusy(true)
    try {
      await api(path, { method: 'POST', body: JSON.stringify(body) })
      showToast('ok', `Fechamento #${id} atualizado.`)
      setModal(null)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha na operação')
    } finally {
      setBusy(false)
    }
  }

  async function doGenerate(motoboyID: number, date: string) {
    setBusy(true)
    try {
      const r = await api<{ id: number; total_pedidos: number; total_a_repassar: number }>(
        '/motoboy-fechamento/generate',
        { method: 'POST', body: JSON.stringify({ motoboy_id: motoboyID, data_fechamento: date }) },
      )
      showToast('ok',
        `Fechamento #${r.id} gerado: ${r.total_pedidos} pedido(s), R$ ${fmt(r.total_a_repassar)} a repassar.`)
      setModal(null)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao gerar fechamento')
    } finally {
      setBusy(false)
    }
  }

  async function doSyncWallets() {
    if (!window.confirm(
      `Sincronizar fechamentos do período ${fmtDate(from)} → ${fmtDate(to)}?\n\n` +
      'Recalcula totais (pedidos, dinheiro, PIX, cartão) de todos os fechamentos sem confirmação de Alan no período.\n' +
      'Fechamentos já confirmados por Alan não serão alterados.'
    )) return
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; fechamentos_sync: number; hint: string }>(
        '/motoboy-fechamento/sync-wallets',
        { method: 'POST', body: JSON.stringify({ from, to }) },
      )
      showToast('ok', `Sincronização concluída: ${r.fechamentos_sync} fechamento(s) atualizado(s).`)
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha na sincronização')
    } finally {
      setBusy(false)
    }
  }

  // ── Derivados ──────────────────────────────────────────────────────────
  const kpis = useMemo(() => summary ?? {
    total_pedidos: 0, total_a_repassar: 0, pendentes_alan: 0, pendentes_repasse: 0, finalizados: 0,
  }, [summary])

  // Chips ativos. Range default = últimos 7 dias — só vira chip quando alterado.
  const defaultFrom = daysAgoISO(7)
  const defaultTo = todayISO()
  const chips: ActiveChip[] = []
  if (from !== defaultFrom) chips.push({ key: 'from', label: `De: ${fmtDate(from)}`, onRemove: () => setFrom(defaultFrom) })
  if (to !== defaultTo) chips.push({ key: 'to', label: `Até: ${fmtDate(to)}`, onRemove: () => setTo(defaultTo) })
  if (status !== 'all') {
    const lbl = STATUS_FILTERS.find(s => s.key === status)?.label ?? status
    chips.push({ key: 'status', label: `Status: ${lbl}`, onRemove: () => setStatus('all') })
  }
  if (filterMotoboy > 0) {
    const m = motoboys.find(mb => mb.id === filterMotoboy)
    chips.push({ key: 'mb', label: `Motoboy: ${m?.nome || `#${filterMotoboy}`}`, onRemove: () => setFilterMotoboy(0) })
  }
  const activeCount = chips.length

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

      {/* Top bar */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div
          className="szv2-card-head"
          style={{ flexWrap: 'wrap', gap: 12 }}
        >
          <div>
            <h2>Fechamento Motoboy</h2>
            <p className="szv2-card-sub">
              Alan confirma o fechamento diário antes do repasse efetivo. Período padrão: últimos 7 dias.
            </p>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary"
              onClick={doSyncWallets}
              disabled={busy || loading}
              title="Reconstrói totais (dinheiro/PIX/cartão) dos fechamentos não confirmados por Alan no período"
            >
              ↻ Sincronizar fechamentos
            </button>
            <button
              type="button"
              className="szv2-btn szv2-btn-brand"
              onClick={() => setModal({ kind: 'generate' })}
              disabled={busy || loading}
            >
              + Gerar fechamento
            </button>
          </div>
        </div>
        <ActiveFilterChips chips={chips} onClearAll={clearFilters} />
      </div>

      {/* 5 KPI cards */}
      <div
        className="szv2-kpi-grid"
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(5, minmax(0,1fr))',
          gap: 16,
          marginBottom: 16,
        }}
      >
        <KpiCard label="Pedidos no período"     value={kpis.total_pedidos}                sub="contagem agregada" />
        <KpiCard label="A repassar (R$)"        value={`R$ ${fmt(kpis.total_a_repassar)}`} sub="soma do período" />
        <KpiCard label="Pendentes Alan"         value={kpis.pendentes_alan}                sub="aguardando confirmação"  tone="warning" />
        <KpiCard label="Pendentes Repasse"      value={kpis.pendentes_repasse}             sub="Alan OK, repasse aberto" tone="warning" />
        <KpiCard label="Finalizados"            value={kpis.finalizados}                   sub="repasse confirmado"      tone="success" />
      </div>

      {/* Tabela */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Fechamentos</h2>
            <p className="szv2-card-sub">{items.length} registro(s) no período</p>
          </div>
        </div>

        {loading && items.length === 0 ? (
          <TableSkeleton rows={5} cols={13} />
        ) : !loading && items.length === 0 ? (
          <EmptyState
            icon="🧾"
            title="Nenhum fechamento no período selecionado."
            description="Fechamentos diários aparecem aqui após Alan confirmar e o repasse ser registrado."
          />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Motoboy</th>
                  <th>CD</th>
                  <th style={{ textAlign: 'right' }}>Pedidos</th>
                  <th style={{ textAlign: 'right' }}>Entregues</th>
                  <th style={{ textAlign: 'right' }}>Frustrados</th>
                  <th style={{ textAlign: 'right' }}>Dinheiro</th>
                  <th style={{ textAlign: 'right' }}>PIX</th>
                  <th style={{ textAlign: 'right' }}>Cartão</th>
                  <th style={{ textAlign: 'right' }}>A repassar</th>
                  <th>Alan</th>
                  <th>Repasse</th>
                  <th style={{ width: 220 }}>Ações</th>
                </tr>
              </thead>
              <tbody>
                {items.map(it => (
                  <tr key={it.id}>
                    <td><strong>{fmtDate(it.data_fechamento)}</strong></td>
                    <td>
                      <div style={{ fontSize: 13 }}>{it.motoboy_nome || `#${it.motoboy_id}`}</div>
                      {it.obs && (
                        <div
                          style={{ fontSize: 10, color: 'var(--szv2-text-muted)', whiteSpace: 'pre-line', marginTop: 2 }}
                          title={it.obs}
                        >
                          {it.obs.length > 60 ? it.obs.slice(0, 60) + '…' : it.obs}
                        </div>
                      )}
                    </td>
                    <td>{it.cd_nome || '—'}</td>
                    <td style={{ textAlign: 'right' }}>{it.total_pedidos}</td>
                    <td style={{ textAlign: 'right', color: 'var(--szv2-success)' }}>{it.total_entregues}</td>
                    <td style={{ textAlign: 'right', color: it.total_frustrados > 0 ? 'var(--szv2-danger)' : undefined }}>
                      {it.total_frustrados}
                    </td>
                    <td style={{ textAlign: 'right' }}>R$ {fmt(it.total_dinheiro)}</td>
                    <td style={{ textAlign: 'right' }}>R$ {fmt(it.total_pix)}</td>
                    <td style={{ textAlign: 'right' }}>R$ {fmt(it.total_cartao)}</td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: 'var(--szv2-brand)' }}>
                      R$ {fmt(it.total_a_repassar)}
                    </td>
                    <td>
                      {it.alan_confirmou ? (
                        <div>
                          <span className="sz-badge szv2-badge-success">✅ Confirmado</span>
                          {it.alan_ts && (
                            <div style={{ fontSize: 10, color: 'var(--szv2-text-muted)', marginTop: 2 }}>
                              {fmtTs(it.alan_ts)}
                            </div>
                          )}
                        </div>
                      ) : (
                        <span className="sz-badge szv2-badge-warning">⏳ Pendente</span>
                      )}
                    </td>
                    <td>
                      {it.repasse_confirmado ? (
                        <div>
                          <span className="sz-badge szv2-badge-success">✅ Confirmado</span>
                          {it.repasse_ts && (
                            <div style={{ fontSize: 10, color: 'var(--szv2-text-muted)', marginTop: 2 }}>
                              {fmtTs(it.repasse_ts)}
                            </div>
                          )}
                        </div>
                      ) : (
                        <span className="sz-badge szv2-badge-warning">⏳ Pendente</span>
                      )}
                    </td>
                    <td>
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                        {/* Ações condicionais: cada estado expõe só o que faz sentido. */}
                        {!it.alan_confirmou && (
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-brand szv2-btn-sm"
                            onClick={() => setModal({ kind: 'alan_confirm', id: it.id })}
                            disabled={busy}
                          >
                            Confirmar Alan
                          </button>
                        )}
                        {it.alan_confirmou && !it.repasse_confirmado && (
                          <>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-brand szv2-btn-sm"
                              onClick={() => setModal({ kind: 'repasse_confirm', id: it.id })}
                              disabled={busy}
                            >
                              Confirmar Repasse
                            </button>
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                              onClick={() => setModal({ kind: 'alan_desconfirm', id: it.id })}
                              disabled={busy}
                            >
                              Desconfirmar Alan
                            </button>
                          </>
                        )}
                        {it.repasse_confirmado && (
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-danger szv2-btn-sm"
                            onClick={() => setModal({ kind: 'repasse_desconfirm', id: it.id })}
                            disabled={busy}
                          >
                            Desconfirmar Repasse
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <ActionModal
        modal={modal}
        busy={busy}
        motoboys={motoboys}
        onClose={() => setModal(null)}
        onConfirmText={doAction}
        onConfirmGenerate={doGenerate}
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
            max={draftTo || undefined}
            onChange={e => setDraftFrom(e.target.value)}
          />
        </FilterField>
        <FilterField label="Data final">
          <input
            type="date"
            style={filterInputStyle}
            value={draftTo}
            min={draftFrom || undefined}
            onChange={e => setDraftTo(e.target.value)}
          />
        </FilterField>
        <FilterField label="Motoboy">
          <select
            style={filterInputStyle}
            value={draftMotoboy}
            onChange={e => setDraftMotoboy(parseInt(e.target.value || '0', 10))}
          >
            <option value={0}>Todos</option>
            {motoboys.map(m => (
              <option key={m.id} value={m.id}>{m.nome} (#{m.id})</option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value as StatusFilter)}
          >
            {STATUS_FILTERS.map(f => (
              <option key={f.key} value={f.key}>{f.label}</option>
            ))}
          </select>
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
