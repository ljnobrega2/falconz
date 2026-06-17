// MotoboySaques — fila de saques / pagamentos de motoboys.
// Espelha sz_mb_tab_saques() em includes/motoboy/admin.php:2016 (§2.10).
//
// Layout: 4 KPI cards (total pago, total aguardando, count pago, count
// aguardando) → top bar com filtros (motoboy, status, range de data)
// → tabela com colunas ID/Motoboy/Telefone/Valor/Data saque/Status/Obs/Criado.

import { useEffect, useState } from 'react'
import { api } from '../api'

// ─── Tipos ────────────────────────────────────────────────────────────────

type Saque = {
  id: number
  motoboy_id: number
  motoboy_nome: string
  telefone: string
  valor_total: number
  data_pagamento: string
  status: string
  obs: string
  created_at: string
}

type Summary = {
  total_pago: number
  total_aguardando: number
  count_pago: number
  count_aguardando: number
}

type StatusFiltro = '' | 'aguardando' | 'pago'

// ─── Helpers ──────────────────────────────────────────────────────────────

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtDate = (s: string) => {
  if (!s) return '—'
  const [y, m, d] = s.slice(0, 10).split('-')
  if (!y || !m || !d) return s
  return `${d}/${m}/${y}`
}

const fmtTs = (s: string | null | undefined) => {
  if (!s) return ''
  // Aceita 'YYYY-MM-DDTHH:MM:SS' ou 'YYYY-MM-DD HH:MM:SS'.
  const t = s.slice(0, 16).replace('T', ' ')
  const [d, hm] = t.split(' ')
  if (!d) return s
  return `${fmtDate(d)} ${hm || ''}`.trim()
}

function daysAgoISO(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return d.toISOString().slice(0, 10)
}
const todayISO = () => new Date().toISOString().slice(0, 10)

const STATUS_FILTERS: { key: StatusFiltro; label: string }[] = [
  { key: '',           label: 'Todos'      },
  { key: 'aguardando', label: 'Aguardando' },
  { key: 'pago',       label: 'Pagos'      },
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

// ─── Página ───────────────────────────────────────────────────────────────

export default function MotoboySaques() {
  const [from, setFrom] = useState<string>(daysAgoISO(30))
  const [to, setTo]     = useState<string>(todayISO())
  const [status, setStatus]       = useState<StatusFiltro>('')
  const [motoboyID, setMotoboyID] = useState<string>('')

  const [items, setItems] = useState<Saque[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = new URLSearchParams()
      if (from)      qs.set('date_from', from)
      if (to)        qs.set('date_to', to)
      if (status)    qs.set('status', status)
      if (motoboyID) qs.set('motoboy_id', motoboyID)
      qs.set('limit', '200')

      const [list, sum] = await Promise.all([
        api<{ items: Saque[]; count: number }>(`/motoboy-saques?${qs.toString()}`),
        api<Summary>(`/motoboy-saques/summary?${qs.toString()}`),
      ])
      setItems(list.items || [])
      setSummary(sum)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line */ }, [from, to, status])

  function handleApply(e?: React.FormEvent) {
    if (e) e.preventDefault()
    load()
  }

  const kpis = summary ?? {
    total_pago: 0,
    total_aguardando: 0,
    count_pago: 0,
    count_aguardando: 0,
  }

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* 4 KPI cards */}
      <div
        className="szv2-kpi-grid"
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(4, minmax(0,1fr))',
          gap: 16,
          marginBottom: 16,
        }}
      >
        <KpiCard
          label="Total pago"
          value={`R$ ${fmt(kpis.total_pago)}`}
          sub="somatório de saques pagos"
          tone="success"
        />
        <KpiCard
          label="Total aguardando"
          value={`R$ ${fmt(kpis.total_aguardando)}`}
          sub="solicitações pendentes"
          tone="warning"
        />
        <KpiCard
          label="Saques pagos"
          value={kpis.count_pago}
          sub="quantidade no período"
          tone="success"
        />
        <KpiCard
          label="Aguardando"
          value={kpis.count_aguardando}
          sub="quantidade no período"
          tone="warning"
        />
      </div>

      {/* Top bar */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head" style={{ flexWrap: 'wrap', gap: 12 }}>
          <div>
            <h2>Saques / pagamentos</h2>
            <p className="szv2-card-sub">
              Fila de pedidos de saque dos motoboys. Solicitações criadas pelo PWA aparecem como aguardando.
            </p>
          </div>
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
              disabled={loading}
            />
          </div>
          <div className="szv2-field" style={{ minWidth: 150 }}>
            <label className="szv2-label">Até</label>
            <input
              type="date"
              className="szv2-input"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              disabled={loading}
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
              disabled={loading}
            />
          </div>
          <button
            type="submit"
            className="szv2-btn szv2-btn-brand"
            disabled={loading}
            style={{ marginLeft: 'auto' }}
          >
            Filtrar
          </button>
        </form>

        <div className="szv2-chip-group" style={{ marginTop: 12 }}>
          {STATUS_FILTERS.map(f => (
            <button
              key={f.key || 'all'}
              type="button"
              className="szv2-chip"
              aria-pressed={status === f.key}
              onClick={() => setStatus(f.key)}
              disabled={loading}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {/* Tabela */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Lançamentos</h2>
            <p className="szv2-card-sub">{items.length} registro(s)</p>
          </div>
        </div>

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : items.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum saque/pagamento registrado.
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Motoboy</th>
                  <th>Telefone</th>
                  <th style={{ textAlign: 'right' }}>Valor</th>
                  <th>Data saque</th>
                  <th>Status</th>
                  <th>Observação</th>
                  <th>Criado em</th>
                </tr>
              </thead>
              <tbody>
                {items.map(s => {
                  const isPago = s.status === 'pago'
                  return (
                    <tr key={s.id}>
                      <td><strong>#{s.id}</strong></td>
                      <td>
                        <div style={{ fontWeight: 700 }}>
                          {s.motoboy_nome || `Motoboy ${s.motoboy_id}`}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                          ID {s.motoboy_id}
                        </div>
                      </td>
                      <td>{s.telefone || '—'}</td>
                      <td style={{ textAlign: 'right', fontWeight: 700 }}>
                        R$ {fmt(s.valor_total)}
                      </td>
                      <td>{fmtDate(s.data_pagamento)}</td>
                      <td>
                        <span
                          className={`sz-badge ${isPago ? 'szv2-badge-success' : 'szv2-badge-warning'}`}
                        >
                          {isPago ? '✅ Pago' : '⏳ Aguardando'}
                        </span>
                      </td>
                      <td
                        style={{
                          maxWidth: 240,
                          whiteSpace: 'pre-wrap',
                          fontSize: 12,
                          color: 'var(--szv2-text-muted)',
                        }}
                      >
                        {s.obs || '—'}
                      </td>
                      <td style={{ fontSize: 12 }}>{fmtTs(s.created_at)}</td>
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
