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
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head" style={{ flexWrap: 'wrap', gap: 12 }}>
          <div>
            <h2>Comprovantes Motoboy</h2>
            <p className="szv2-card-sub">
              Relatório COD: fotos de comprovante + KPIs de entrega por período.
            </p>
          </div>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            onClick={handleExportCSV}
            style={{ marginLeft: 'auto' }}
          >
            ⬇ Exportar CSV
          </button>
        </div>

        <form
          onSubmit={handleApply}
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: 12,
            alignItems: 'flex-end',
            padding: '12px 0 4px',
          }}
        >
          <div className="szv2-field" style={{ minWidth: 150 }}>
            <label className="szv2-label">De</label>
            <input
              type="date"
              className="szv2-input"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
              disabled={loading || busy}
            />
          </div>
          <div className="szv2-field" style={{ minWidth: 150 }}>
            <label className="szv2-label">Até</label>
            <input
              type="date"
              className="szv2-input"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              disabled={loading || busy}
            />
          </div>
          <div className="szv2-field" style={{ minWidth: 140 }}>
            <label className="szv2-label">Motoboy (ID)</label>
            <input
              type="number"
              className="szv2-input"
              placeholder="ID do motoboy"
              value={motoboyID}
              onChange={(e) => setMotoboyID(e.target.value)}
              disabled={loading || busy}
            />
          </div>
          <div className="szv2-field" style={{ minWidth: 140 }}>
            <label className="szv2-label">Nº do pedido</label>
            <input
              type="number"
              className="szv2-input"
              placeholder="ex: 5678"
              value={pedidoBusca}
              onChange={(e) => setPedidoBusca(e.target.value)}
              disabled={loading || busy}
            />
          </div>
          {/* Zona — filtro que vai para KPIs + galeria (via buildQS) */}
          <div className="szv2-field" style={{ minWidth: 160 }}>
            <label className="szv2-label">Zona</label>
            <select
              className="szv2-input"
              value={zonaID}
              onChange={(e) => setZonaID(e.target.value)}
              disabled={loading || busy}
            >
              <option value="">Todas</option>
              {zonas.map(z => (
                <option key={z.id} value={String(z.id)}>{z.nome}</option>
              ))}
            </select>
          </div>
          {/* Status do pedido — filtro que vai para KPIs e CSV */}
          <div className="szv2-field" style={{ minWidth: 150 }}>
            <label className="szv2-label">Status do pedido</label>
            <select
              className="szv2-input"
              value={statusFiltro}
              onChange={(e) => setStatusFiltro(e.target.value as StatusFiltro)}
              disabled={loading || busy}
            >
              {STATUS_OPTIONS.map(s => (
                <option key={s.key} value={s.key}>{s.label}</option>
              ))}
            </select>
          </div>
          <button
            type="submit"
            className="szv2-btn szv2-btn-brand"
            disabled={loading || busy}
          >
            Filtrar
          </button>
        </form>

        {/* Chips tipo pgto */}
        <div className="szv2-chip-group" style={{ marginTop: 12, flexWrap: 'wrap', gap: 8 }}>
          <span style={{ fontSize: 11, color: '#9ca3af', alignSelf: 'center', marginRight: 4 }}>Tipo pgto:</span>
          {TIPO_LABELS.map(t => (
            <button
              key={t.key}
              type="button"
              className="szv2-chip"
              aria-pressed={tipo === t.key}
              onClick={() => handleTipoChange(t.key)}
              disabled={loading || busy}
            >
              {t.icon} {t.label}
            </button>
          ))}
          <span style={{ fontSize: 11, color: '#9ca3af', alignSelf: 'center', marginLeft: 12, marginRight: 4 }}>Baixa por:</span>
          {BAIXA_POR_LABELS.map(b => (
            <button
              key={b.key}
              type="button"
              className="szv2-chip"
              aria-pressed={baixaPor === b.key}
              onClick={() => handleBaixaPorChange(b.key)}
              disabled={loading || busy}
            >
              {b.label}
            </button>
          ))}
        </div>
      </div>

      {/* ─── KPIs ─── */}
      <div className="sz-comp-kpi-row">
        {loadingStats ? (
          <div style={{ fontSize: 13, color: '#9ca3af', padding: '8px 0' }}>Carregando KPIs…</div>
        ) : stats ? (
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

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : items.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum comprovante encontrado.
          </div>
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
    </div>
  )
}
