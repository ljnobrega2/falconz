// MotoboyComprovantes — visualizador de comprovantes de pagamento dos
// pedidos motoboy. Espelha relatorios.php (includes/motoboy/relatorios.php)
// e expande para galeria de fotos.
//
// Funcionalidades:
//   - 6 KPIs no topo (total / entregues / frustrados / em_rota / receita / taxa_sucesso)
//   - Alerta visual "X pedido(s) entregue(s) sem comprovante" (sem_comp > 0)
//   - Filtros: período, motoboy_id, wc_order_id, tipo_pgto, baixa_por, zona, status
//   - Botão "Exportar CSV" (21 colunas — espelha relatorios.php:70-93)
//   - Grid galeria de cards 200x200 com lightbox
//   - Badge ADM / MB por baixa_por em cada card (canto superior direito)
//   - Soft-delete via DELETE /motoboy-comprovantes/{id} com X-Confirm: DELETE

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
import CardKpiSkeleton from '../components/CardKpiSkeleton'

// ─── Tipos ────────────────────────────────────────────────────────────────

type Comprovante = {
  id: number
  pedido_id: number
  wc_order_id: number
  motoboy_id: number
  motoboy_nome: string
  tipo_pgto: string
  foto_url: string
  foto_path: string
  baixa_por: string
  created_at: string
}

type Stats = {
  total: number
  entregues: number
  frustrados: number
  em_rota: number
  receita: number
  taxa_sucesso: number
  sem_comp: number
}

type Zona = {
  id: number
  nome: string
}

type TipoFiltro = '' | 'dinheiro' | 'pix' | 'cartao'
type BaixaPorFiltro = '' | 'motoboy' | 'admin'
type StatusFiltro = '' | 'agendado' | 'embalado' | 'em_rota' | 'entregue' | 'frustrado' | 'cancelado'

// ─── Helpers ──────────────────────────────────────────────────────────────

const TIPO_LABELS: { key: TipoFiltro; label: string; icon: string }[] = [
  { key: '',         label: 'Todos',    icon: '🧾' },
  { key: 'dinheiro', label: 'Dinheiro', icon: '💵' },
  { key: 'pix',      label: 'PIX',      icon: '📱' },
  { key: 'cartao',   label: 'Cartão',   icon: '💳' },
]

const BAIXA_POR_LABELS: { key: BaixaPorFiltro; label: string }[] = [
  { key: '',        label: 'Todos'    },
  { key: 'motoboy', label: 'Motoboy'  },
  { key: 'admin',   label: 'Admin/OL' },
]

const STATUS_OPTIONS: { key: StatusFiltro; label: string }[] = [
  { key: '',          label: 'Todos'     },
  { key: 'agendado',  label: 'Agendado'  },
  { key: 'embalado',  label: 'Embalado'  },
  { key: 'em_rota',   label: 'Em rota'   },
  { key: 'entregue',  label: 'Entregue'  },
  { key: 'frustrado', label: 'Frustrado' },
  { key: 'cancelado', label: 'Cancelado' },
]

function iconForTipo(tipo: string): string {
  switch ((tipo || '').toLowerCase()) {
    case 'dinheiro': return '💵'
    case 'pix':      return '📱'
    case 'cartao':   return '💳'
    default:         return '🧾'
  }
}

const fmtTs = (s: string | null | undefined) => {
  if (!s) return ''
  return s.slice(0, 16).replace('T', ' ')
}

const fmtBRL = (v: number) =>
  'R$ ' + v.toFixed(2).replace('.', ',').replace(/\B(?=(\d{3})+(?!\d))/g, '.')

function daysAgoISO(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d.toISOString().slice(0, 10)
}
const todayISO = () => new Date().toISOString().slice(0, 10)

// ─── Página ───────────────────────────────────────────────────────────────

export default function MotoboyComprovantes() {
  // Filtros
  const [from, setFrom]               = useState<string>(daysAgoISO(7))
  const [to, setTo]                   = useState<string>(todayISO())
  const [tipo, setTipo]               = useState<TipoFiltro>('')
  const [baixaPor, setBaixaPor]       = useState<BaixaPorFiltro>('')
  const [statusFiltro, setStatusFiltro] = useState<StatusFiltro>('')
  const [zonaID, setZonaID]           = useState<string>('')
  const [motoboyID, setMotoboyID]     = useState<string>('')
  const [pedidoBusca, setPedidoBusca] = useState<string>('')

  // Dados
  const [items, setItems]             = useState<Comprovante[]>([])
  const [stats, setStats]             = useState<Stats | null>(null)
  const [zonas, setZonas]             = useState<Zona[]>([])

  // Estado de UI
  const [loading, setLoading]         = useState(true)
  const [loadingStats, setLoadingStats] = useState(true)
  const [busy, setBusy]               = useState(false)
  const [err, setErr]                 = useState('')
  const [toast, setToast]             = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [modal, setModal]             = useState<Comprovante | null>(null)

  // Painel de filtros
  const [filterOpen, setFilterOpen]   = useState(false)
  const [draftFrom, setDraftFrom]     = useState(from)
  const [draftTo, setDraftTo]         = useState(to)
  const [draftTipo, setDraftTipo]     = useState<TipoFiltro>('')
  const [draftBaixa, setDraftBaixa]   = useState<BaixaPorFiltro>('')
  const [draftStatusF, setDraftStatusF] = useState<StatusFiltro>('')
  const [draftZona, setDraftZona]     = useState('')
  const [draftMotoboyID, setDraftMotoboyID] = useState('')
  const [draftPedidoBusca, setDraftPedidoBusca] = useState('')

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  // Carrega zonas para o select (uma vez).
  useEffect(() => {
    api<{ items: Zona[] }>('/zonas')
      .then(r => setZonas(r.items || []))
      .catch(() => setZonas([]))
  }, [])

  // Constrói querystring compartilhado entre galeria e stats.
  function buildQS(extra: Record<string, string> = {}): string {
    const qs = new URLSearchParams()
    if (from)        qs.set('date_from', from)
    if (to)          qs.set('date_to', to)
    if (motoboyID)   qs.set('motoboy_id', motoboyID)
    if (zonaID)      qs.set('zona_id', zonaID)
    if (statusFiltro) qs.set('status', statusFiltro)
    if (baixaPor)    qs.set('baixa_por', baixaPor)
    for (const [k, v] of Object.entries(extra)) {
      if (v) qs.set(k, v)
    }
    return qs.toString()
  }

  async function loadStats() {
    setLoadingStats(true)
    try {
      const qs = buildQS()
      const r = await api<Stats>(`/motoboy-comprovantes/stats?${qs}`)
      setStats(r)
    } catch {
      setStats(null)
    } finally {
      setLoadingStats(false)
    }
  }

  async function loadGallery() {
    setLoading(true)
    setErr('')
    try {
      const qs = new URLSearchParams()
      if (from)        qs.set('date_from', from)
      if (to)          qs.set('date_to', to)
      if (tipo)        qs.set('tipo', tipo)
      if (baixaPor)    qs.set('baixa_por', baixaPor)
      if (motoboyID)   qs.set('motoboy_id', motoboyID)
      if (pedidoBusca) qs.set('wc_order_id', pedidoBusca)
      qs.set('limit', '300')

      const r = await api<{ items: Comprovante[]; count: number }>(
        `/motoboy-comprovantes?${qs.toString()}`,
      )
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  // Carrega na montagem e quando filtros de data mudam (para ambos).
  useEffect(() => {
    loadStats()
    loadGallery()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from, to])

  // Botão "Filtrar" — aplica todos os filtros.
  function handleApply(e?: React.FormEvent) {
    if (e) e.preventDefault()
    loadStats()
    loadGallery()
  }

  // Recarrega apenas a galeria com os valores passados diretamente (evita
  // ler state desatualizado após setTipo/setBaixaPor).
  function reloadGallery(opts: { tipo?: TipoFiltro; baixaPor?: BaixaPorFiltro } = {}) {
    const t  = opts.tipo     !== undefined ? opts.tipo     : tipo
    const bp = opts.baixaPor !== undefined ? opts.baixaPor : baixaPor
    setLoading(true)
    setErr('')
    const qs = new URLSearchParams()
    if (from)        qs.set('date_from', from)
    if (to)          qs.set('date_to', to)
    if (t)           qs.set('tipo', t)
    if (bp)          qs.set('baixa_por', bp)
    if (motoboyID)   qs.set('motoboy_id', motoboyID)
    if (pedidoBusca) qs.set('wc_order_id', pedidoBusca)
    qs.set('limit', '300')
    api<{ items: Comprovante[] }>(`/motoboy-comprovantes?${qs.toString()}`)
      .then(r => setItems(r.items || []))
      .catch(e => setErr(e.message || 'Erro'))
      .finally(() => setLoading(false))
  }

  function handleTipoChange(t: TipoFiltro) {
    setTipo(t)
    reloadGallery({ tipo: t })
  }

  function handleBaixaPorChange(bp: BaixaPorFiltro) {
    setBaixaPor(bp)
    reloadGallery({ baixaPor: bp })
  }

  // Export CSV — fetch direto com Bearer token, dispara download.
  async function handleExportCSV() {
    try {
      const tok = getToken()
      const base = (import.meta as any).env?.VITE_API_BASE || '/wp-json/senderzz/v1/admin'
      const qs = buildQS()
      const res = await fetch(`${base}/motoboy-comprovantes/export-csv?${qs}`, {
        headers: tok ? { Authorization: `Bearer ${tok}` } : {},
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `relatorio-cod-${from}-${to}.csv`
      a.click()
      URL.revokeObjectURL(url)
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao exportar CSV')
    }
  }

  // Exclusão (soft) — exige header X-Confirm: DELETE.
  async function handleDelete(id: number) {
    if (!window.confirm(
      `Excluir comprovante #${id}?\n\nEssa ação remove a foto da listagem (soft-delete).\nO registro continua disponível para auditoria.`,
    )) return

    setBusy(true)
    try {
      const tok = getToken()
      const base = (import.meta as any).env?.VITE_API_BASE || '/wp-json/senderzz/v1/admin'
      const res = await fetch(`${base}/motoboy-comprovantes/${id}`, {
        method: 'DELETE',
        headers: {
          'Content-Type': 'application/json',
          'X-Confirm': 'DELETE',
          ...(tok ? { Authorization: `Bearer ${tok}` } : {}),
        },
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body?.error?.message || `HTTP ${res.status}`)
      }
      showToast('ok', `Comprovante #${id} removido.`)
      setModal(null)
      await loadGallery()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao excluir')
    } finally {
      setBusy(false)
    }
  }

  // ─── Painel de filtros ────────────────────────────────────────────────────
  function openPanel() {
    setDraftFrom(from); setDraftTo(to)
    setDraftTipo(tipo); setDraftBaixa(baixaPor); setDraftStatusF(statusFiltro)
    setDraftZona(zonaID); setDraftMotoboyID(motoboyID); setDraftPedidoBusca(pedidoBusca)
    setFilterOpen(true)
  }
  function applyFilters() {
    setFrom(draftFrom); setTo(draftTo)
    setTipo(draftTipo); setBaixaPor(draftBaixa); setStatusFiltro(draftStatusF)
    setZonaID(draftZona); setMotoboyID(draftMotoboyID); setPedidoBusca(draftPedidoBusca)
    setFilterOpen(false)
    // useEffect [from, to] dispara reload; reloadGallery direto cobre demais
    setTimeout(() => { loadStats(); loadGallery() }, 0)
  }
  function clearFilters() {
    const def_from = daysAgoISO(7)
    const def_to = todayISO()
    setFrom(def_from); setTo(def_to)
    setTipo(''); setBaixaPor(''); setStatusFiltro('')
    setZonaID(''); setMotoboyID(''); setPedidoBusca('')
    setDraftFrom(def_from); setDraftTo(def_to)
    setDraftTipo(''); setDraftBaixa(''); setDraftStatusF('')
    setDraftZona(''); setDraftMotoboyID(''); setDraftPedidoBusca('')
    setFilterOpen(false)
  }

  // Helper: ao remover chip, dispara reload já que useEffect só observa [from, to].
  function triggerReload() {
    setTimeout(() => { loadStats(); loadGallery() }, 0)
  }
  // Chips ativos.
  const chips: ActiveChip[] = []
  if (from && from !== daysAgoISO(7)) chips.push({ key: 'from', label: `De: ${from}`, onRemove: () => setFrom(daysAgoISO(7)) })
  if (to && to !== todayISO()) chips.push({ key: 'to', label: `Até: ${to}`, onRemove: () => setTo(todayISO()) })
  if (tipo) chips.push({ key: 'tipo', label: `Tipo: ${tipo}`, onRemove: () => { setTipo(''); reloadGallery({ tipo: '' }) } })
  if (baixaPor) chips.push({ key: 'baixa', label: `Baixa: ${baixaPor}`, onRemove: () => { setBaixaPor(''); reloadGallery({ baixaPor: '' }) } })
  if (statusFiltro) chips.push({ key: 'status', label: `Status: ${statusFiltro}`, onRemove: () => { setStatusFiltro(''); triggerReload() } })
  if (zonaID) chips.push({ key: 'zona', label: `Zona: ${zonas.find(z => String(z.id) === zonaID)?.nome || zonaID}`, onRemove: () => { setZonaID(''); triggerReload() } })
  if (motoboyID) chips.push({ key: 'mb', label: `Motoboy: #${motoboyID}`, onRemove: () => { setMotoboyID(''); triggerReload() } })
  if (pedidoBusca) chips.push({ key: 'ped', label: `Pedido: #${pedidoBusca}`, onRemove: () => { setPedidoBusca(''); triggerReload() } })

  // ─── KPI card helper ──────────────────────────────────────────────────────
  function KpiCard({
    label, value, bg, border, color,
  }: { label: string; value: string | number; bg: string; border: string; color: string }) {
    return (
      <div style={{
        background: bg,
        border: `1px solid ${border}`,
        borderRadius: 10,
        padding: '12px 14px',
        minWidth: 130,
      }}>
        <div style={{ fontSize: 11, fontWeight: 700, color: '#9ca3af', marginBottom: 4 }}>{label}</div>
        <div style={{ fontSize: 22, fontWeight: 700, color }}>{value}</div>
      </div>
    )
  }

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

      <style>{`
        .sz-comp-grid {
          display: grid;
          grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
          gap: 14px;
        }
        .sz-comp-card {
          border: 1px solid var(--szv2-border, #e5e7eb);
          border-radius: 10px;
          overflow: hidden;
          background: var(--szv2-surface, #fff);
          cursor: pointer;
          transition: transform .08s ease, box-shadow .15s ease;
          display: flex;
          flex-direction: column;
        }
        .sz-comp-card:hover {
          transform: translateY(-2px);
          box-shadow: 0 4px 16px rgba(0,0,0,.08);
        }
        .sz-comp-thumb {
          position: relative;
          width: 100%;
          aspect-ratio: 1;
          background: #f3f4f6;
          overflow: hidden;
        }
        .sz-comp-thumb img {
          width: 100%;
          height: 100%;
          object-fit: cover;
          display: block;
        }
        .sz-comp-badge {
          position: absolute;
          top: 8px;
          left: 8px;
          background: rgba(17,24,39,.85);
          color: #fff;
          padding: 4px 8px;
          border-radius: 999px;
          font-size: 11px;
          font-weight: 700;
          display: inline-flex;
          align-items: center;
          gap: 4px;
        }
        /* Badge ADM / MB — canto superior direito (espelha order-metabox.php:328-335) */
        .sz-comp-badge-baixa {
          position: absolute;
          top: 8px;
          right: 8px;
          padding: 3px 7px;
          border-radius: 999px;
          font-size: 10px;
          font-weight: 700;
        }
        .sz-comp-badge-baixa.adm {
          background: #dbeafe;
          color: #1e40af;
        }
        .sz-comp-badge-baixa.mb {
          background: #dcfce7;
          color: #166534;
        }
        .sz-comp-meta {
          padding: 8px 10px;
          font-size: 12px;
          color: var(--szv2-text-muted, #6b7280);
          line-height: 1.4;
        }
        .sz-comp-meta strong {
          color: var(--szv2-text, #111827);
          font-size: 12px;
        }

        .sz-comp-modal {
          position: fixed;
          inset: 0;
          background: rgba(0,0,0,.85);
          display: flex;
          align-items: center;
          justify-content: center;
          z-index: 99990;
          padding: 24px;
        }
        .sz-comp-modal-inner {
          max-width: 1000px;
          max-height: 92vh;
          width: 100%;
          background: #fff;
          border-radius: 12px;
          overflow: hidden;
          display: flex;
          flex-direction: column;
        }
        .sz-comp-modal-img {
          flex: 1;
          background: #111;
          display: flex;
          align-items: center;
          justify-content: center;
          overflow: auto;
          min-height: 240px;
        }
        .sz-comp-modal-img img {
          max-width: 100%;
          max-height: 100%;
          object-fit: contain;
        }
        .sz-comp-modal-foot {
          padding: 12px 16px;
          border-top: 1px solid #e5e7eb;
          display: flex;
          gap: 12px;
          align-items: center;
          flex-wrap: wrap;
          background: #fff;
        }
        .sz-comp-modal-foot .meta {
          flex: 1;
          font-size: 13px;
          color: #374151;
        }
        .sz-comp-kpi-row {
          display: flex;
          flex-wrap: wrap;
          gap: 10px;
          margin-bottom: 16px;
        }
      `}</style>

      {/* ─── Cabeçalho + filtros ─── */}
      <div className="szv2-section-head" style={{ flexWrap: 'wrap', gap: 12 }}>
        <div>
          <h1>Comprovantes Motoboy</h1>
          <p>Relatório COD: fotos de comprovante + KPIs de entrega por período.</p>
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
            onClick={handleExportCSV}
          >
            ⬇ Exportar CSV
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {/* ─── KPIs ─── */}
      {loadingStats && !stats ? (
        <CardKpiSkeleton count={6} />
      ) : (
      <div className="sz-comp-kpi-row">
        {stats ? (
          <>
            <KpiCard label="📦 Total de pedidos" value={stats.total}
              bg="#f9fafb" border="#e5e7eb" color="#374151" />
            <KpiCard label="✅ Entregues (c/ comprovante)" value={stats.entregues}
              bg="#dcfce7" border="#bbf7d0" color="#166534" />
            <KpiCard label="❌ Frustrados" value={stats.frustrados}
              bg="#fee2e2" border="#fecaca" color="#991b1b" />
            <KpiCard label="🛵 Em rota" value={stats.em_rota}
              bg="#dbeafe" border="#bfdbfe" color="#1e40af" />
            <KpiCard label="💰 Receita total (entregues)" value={fmtBRL(stats.receita)}
              bg="#f0fdf4" border="#bbf7d0" color="#166534" />
            <KpiCard label="📊 Taxa de sucesso" value={`${stats.taxa_sucesso.toFixed(1)}%`}
              bg="#f9fafb" border="#e5e7eb" color="#374151" />
          </>
        ) : null}
      </div>
      )}

      {/* ─── Alerta sem_comp ─── (espelha relatorios.php:177-181) */}
      {!loadingStats && stats && stats.sem_comp > 0 && (
        <div style={{
          background: '#fef3c7',
          border: '1px solid #f59e0b',
          borderRadius: 8,
          padding: '10px 14px',
          marginBottom: 16,
          fontSize: 13,
          color: '#92400e',
          fontWeight: 700,
        }}>
          ⚠️ {stats.sem_comp} pedido(s) entregue(s) sem comprovante de pagamento registrado.
        </div>
      )}

      {/* ─── Galeria ─── */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Galeria de Comprovantes</h2>
            <p className="szv2-card-sub">{items.length} comprovante(s)</p>
          </div>
        </div>

        {loading && items.length === 0 ? (
          <TableSkeleton rows={3} cols={6} />
        ) : !loading && items.length === 0 ? (
          <EmptyState
            icon="📸"
            title="Nenhum comprovante encontrado."
            description="Comprovantes de entrega aparecem aqui assim que motoboys baixarem pedidos no PWA."
          />
        ) : (
          <div className="sz-comp-grid">
            {items.map(c => (
              <div
                key={c.id}
                className="sz-comp-card"
                onClick={() => setModal(c)}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') setModal(c) }}
              >
                <div className="sz-comp-thumb">
                  <img
                    src={c.foto_url}
                    alt={`Comprovante #${c.id}`}
                    loading="lazy"
                    onError={(e) => {
                      (e.currentTarget as HTMLImageElement).style.display = 'none'
                    }}
                  />
                  {/* Badge tipo pgto — canto superior esquerdo */}
                  <span className="sz-comp-badge">
                    {iconForTipo(c.tipo_pgto)} {c.tipo_pgto || '—'}
                  </span>
                  {/* Badge ADM / MB — canto superior direito (espelha order-metabox.php:328-335) */}
                  {c.baixa_por === 'admin' && (
                    <span className="sz-comp-badge-baixa adm">ADM</span>
                  )}
                  {c.baixa_por === 'motoboy' && (
                    <span className="sz-comp-badge-baixa mb">MB</span>
                  )}
                </div>
                <div className="sz-comp-meta">
                  <strong>{c.motoboy_nome || `Motoboy ${c.motoboy_id}`}</strong>
                  <div>Pedido #{c.wc_order_id}</div>
                  <div style={{ fontSize: 11, color: '#9ca3af' }}>{fmtTs(c.created_at)}</div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* ─── Modal lightbox ─── */}
      {modal && (
        <div
          className="sz-comp-modal"
          onClick={(e) => { if (e.target === e.currentTarget && !busy) setModal(null) }}
        >
          <div className="sz-comp-modal-inner">
            <div className="sz-comp-modal-img">
              <img src={modal.foto_url} alt={`Comprovante #${modal.id}`} />
            </div>
            <div className="sz-comp-modal-foot">
              <div className="meta">
                <div style={{ fontWeight: 700, fontSize: 14 }}>
                  {iconForTipo(modal.tipo_pgto)} {modal.tipo_pgto || '—'} · Comprovante #{modal.id}
                </div>
                <div>
                  Motoboy: <strong>{modal.motoboy_nome || `#${modal.motoboy_id}`}</strong>
                  {' · '}Pedido <strong>#{modal.wc_order_id}</strong>
                  {' · '}Baixa:{' '}
                  {modal.baixa_por === 'admin' ? (
                    <span style={{ background: '#dbeafe', color: '#1e40af', borderRadius: 99, padding: '1px 7px', fontWeight: 700, fontSize: 11 }}>ADM</span>
                  ) : modal.baixa_por === 'motoboy' ? (
                    <span style={{ background: '#dcfce7', color: '#166534', borderRadius: 99, padding: '1px 7px', fontWeight: 700, fontSize: 11 }}>MB</span>
                  ) : modal.baixa_por || '—'}
                </div>
                <div style={{ fontSize: 12, color: '#6b7280' }}>{fmtTs(modal.created_at)}</div>
              </div>
              <a
                href={modal.foto_url}
                download={`comprovante-${modal.id}.jpg`}
                className="szv2-btn szv2-btn-secondary"
                target="_blank"
                rel="noopener noreferrer"
              >
                Baixar
              </a>
              <button
                type="button"
                className="szv2-btn szv2-btn-danger"
                onClick={() => handleDelete(modal.id)}
                disabled={busy}
              >
                {busy ? 'Excluindo…' : 'Excluir'}
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                onClick={() => setModal(null)}
                disabled={busy}
              >
                Fechar
              </button>
            </div>
          </div>
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
        <FilterField label="Tipo pgto">
          <select
            style={filterInputStyle}
            value={draftTipo}
            onChange={e => setDraftTipo(e.target.value as TipoFiltro)}
          >
            {TIPO_LABELS.map(t => <option key={t.key} value={t.key}>{t.icon} {t.label}</option>)}
          </select>
        </FilterField>
        <FilterField label="Baixa por">
          <select
            style={filterInputStyle}
            value={draftBaixa}
            onChange={e => setDraftBaixa(e.target.value as BaixaPorFiltro)}
          >
            {BAIXA_POR_LABELS.map(b => <option key={b.key} value={b.key}>{b.label}</option>)}
          </select>
        </FilterField>
        <FilterField label="Status do pedido">
          <select
            style={filterInputStyle}
            value={draftStatusF}
            onChange={e => setDraftStatusF(e.target.value as StatusFiltro)}
          >
            {STATUS_OPTIONS.map(s => <option key={s.key} value={s.key}>{s.label}</option>)}
          </select>
        </FilterField>
        <FilterField label="Zona">
          <select
            style={filterInputStyle}
            value={draftZona}
            onChange={e => setDraftZona(e.target.value)}
          >
            <option value="">Todas</option>
            {zonas.map(z => <option key={z.id} value={String(z.id)}>{z.nome}</option>)}
          </select>
        </FilterField>
        <FilterField label="Motoboy (ID)">
          <input
            type="number"
            style={filterInputStyle}
            placeholder="ID do motoboy"
            value={draftMotoboyID}
            onChange={e => setDraftMotoboyID(e.target.value)}
          />
        </FilterField>
        <FilterField label="Nº do pedido / busca">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex: 5678"
            value={draftPedidoBusca}
            onChange={e => setDraftPedidoBusca(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
