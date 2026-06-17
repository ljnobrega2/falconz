import { useEffect, useRef, useState } from 'react'
import { api } from '../api'

type NormStatus = {
  orders_total: number
  orders_normalized: number
  orders_pending: number
  divergences_count: number
  last_run: { at: string; updated: number; next_offset: number } | null
  last_run_updated: number
  hpos_active: boolean
}

type NormResult = {
  done: number
  updated: number
  next_offset: number
  dry_run: boolean
}

type Divergence = {
  order_id: number
  canonical_key: string
  canonical_value: string
  legacy_key: string
  legacy_value: string
  action: string // "divergence" | "filled" | "would_fill"
}

function KpiCard({ label, value, sub, warn }: { label: string; value: number | string; sub?: string; warn?: boolean }) {
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span
          className="szv2-kpi-value"
          style={warn && Number(value) > 0
            ? { color: 'var(--szv2-danger)' }
            : { color: 'var(--szv2-brand)' }}
        >
          {value}
        </span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

export default function OrderMetaNormalization() {
  const [status, setStatus] = useState<NormStatus | null>(null)
  const [divergences, setDivergences] = useState<Divergence[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  // Campos do formulário de normalização.
  const [batchSize, setBatchSize] = useState(50)
  const [offset, setOffset] = useState(0)
  const [dryRun, setDryRun] = useState(false)
  const [busy, setBusy] = useState(false)
  const [lastResult, setLastResult] = useState<NormResult | null>(null)

  // Controla collapsible de divergências.
  const detailsRef = useRef<HTMLDetailsElement>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 6000)
  }

  async function loadStatus() {
    setLoading(true)
    setErr('')
    try {
      const s = await api<NormStatus>('/order-meta/normalization-status')
      setStatus(s)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar status')
    } finally {
      setLoading(false)
    }
  }

  async function loadDivergences() {
    try {
      const d = await api<{ items: Divergence[] }>('/order-meta/divergences?limit=50')
      setDivergences(d.items || [])
    } catch {
      // silencioso — divergências são opcionais
    }
  }

  useEffect(() => {
    loadStatus()
    loadDivergences()
  }, [])

  async function handleNormalize() {
    setBusy(true)
    try {
      const r = await api<NormResult>('/order-meta/normalize', {
        method: 'POST',
        body: JSON.stringify({ batch_size: batchSize, offset, dry_run: dryRun }),
      })
      setLastResult(r)
      if (!dryRun) {
        setOffset(r.next_offset)
      }
      showToast('ok',
        dryRun
          ? `[Simulação] ${r.done} pedido(s) processados. Nenhuma alteração gravada.`
          : `${r.done} pedido(s) processados, ${r.updated} meta(s) atualizadas.`)
      await loadStatus()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao executar normalização')
    } finally {
      setBusy(false)
    }
  }

  async function handleClearLog() {
    if (!window.confirm('Limpar todo o log de divergências?')) return
    try {
      await api('/order-meta/divergence-log', { method: 'DELETE' })
      setDivergences([])
      showToast('ok', 'Log de divergências limpo.')
      await loadStatus()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao limpar log')
    }
  }

  const fmtDate = (iso: string) => {
    try {
      return new Date(iso).toLocaleString('pt-BR')
    } catch {
      return iso
    }
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

      {/* KPIs de status */}
      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando…
        </div>
      ) : status && (
        <>
          <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))' }}>
            <KpiCard label="Total de pedidos"  value={status.orders_total}      sub="no banco" />
            <KpiCard label="Normalizados"      value={status.orders_normalized} sub="com meta canônica" />
            <KpiCard label="Pendentes"         value={status.orders_pending}    sub="sem normalização" warn />
            <KpiCard label="Divergências"      value={status.divergences_count} sub="no log" warn />
          </div>

          {/* Informações adicionais */}
          <div className="szv2-card" style={{ marginTop: 16 }}>
            <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
              <span>
                <strong>HPOS:</strong>{' '}
                <span style={{ color: status.hpos_active ? 'var(--szv2-brand)' : 'var(--szv2-danger)' }}>
                  {status.hpos_active ? 'Ativo' : 'Inativo'}
                </span>
              </span>
              {status.last_run && (
                <>
                  <span><strong>Última execução:</strong> {fmtDate(status.last_run.at)}</span>
                  <span><strong>Próximo offset:</strong> {status.last_run.next_offset}</span>
                </>
              )}
            </div>
          </div>
        </>
      )}

      {/* Execução de normalização */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Executar normalização</h2>
            <p className="szv2-card-sub">
              Processa pedidos em lote e atualiza campos canônicos de meta. A lógica de escrita
              reside no PHP legado; este painel agenda e rastreia o progresso.
            </p>
          </div>
        </div>

        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', alignItems: 'flex-end', marginTop: 16 }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
            Tamanho do batch
            <input
              type="number"
              min={10}
              max={500}
              value={batchSize}
              onChange={e => setBatchSize(Number(e.target.value))}
              className="szv2-input"
              style={{ width: 100 }}
              disabled={busy}
            />
          </label>

          <label style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
            Offset inicial
            <input
              type="number"
              min={0}
              value={offset}
              onChange={e => setOffset(Number(e.target.value))}
              className="szv2-input"
              style={{ width: 120 }}
              disabled={busy}
            />
          </label>

          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, cursor: 'pointer', paddingBottom: 2 }}>
            <input
              type="checkbox"
              checked={dryRun}
              onChange={e => setDryRun(e.target.checked)}
              disabled={busy}
            />
            Simulação — não grava
          </label>

          <button
            type="button"
            className="szv2-btn-brand"
            onClick={handleNormalize}
            disabled={busy}
            style={{ paddingBottom: 2 }}
          >
            {busy ? 'Executando…' : 'Executar normalização'}
          </button>
        </div>

        {/* Resultado da última execução */}
        {lastResult && (
          <div style={{
            marginTop: 16,
            padding: '10px 14px',
            background: 'rgba(234,88,12,.08)',
            borderRadius: 8,
            fontSize: 13,
            display: 'flex',
            gap: 16,
            flexWrap: 'wrap',
            alignItems: 'center',
          }}>
            <span>
              <strong>{lastResult.done}</strong> pedido(s) processados,{' '}
              <strong>{lastResult.updated}</strong> meta(s) atualizadas.
              {lastResult.dry_run && <span style={{ color: 'var(--szv2-text-muted)' }}> [simulação]</span>}
            </span>
            <span style={{ color: 'var(--szv2-text-muted)' }}>
              Próximo offset: <strong>{lastResult.next_offset}</strong>
            </span>
            {!lastResult.dry_run && (
              <button
                type="button"
                className="szv2-btn-secondary"
                onClick={handleNormalize}
                disabled={busy}
              >
                Continuar (offset {lastResult.next_offset})
              </button>
            )}
          </div>
        )}
      </div>

      {/* Divergências — collapsible */}
      <details ref={detailsRef} style={{ marginTop: 24 }}>
        <summary style={{
          cursor: 'pointer',
          padding: '12px 16px',
          background: 'var(--szv2-card-bg, #fff)',
          borderRadius: 8,
          fontWeight: 600,
          fontSize: 14,
          border: '1px solid var(--szv2-border, #e5e7eb)',
          listStyle: 'none',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
        }}>
          <span>
            Divergências{' '}
            {divergences.length > 0 && (
              <span className="sz-badge szv2-badge-danger" style={{ marginLeft: 8 }}>
                {divergences.length}
              </span>
            )}
          </span>
          <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>▾ expandir</span>
        </summary>

        <div className="szv2-card" style={{ marginTop: 4, borderTopLeftRadius: 0, borderTopRightRadius: 0 }}>
          <div className="szv2-card-head" style={{ marginBottom: 12 }}>
            <p className="szv2-card-sub">
              Campos canônicos com valor diferente do campo legado correspondente.
            </p>
            {divergences.length > 0 && (
              <button
                type="button"
                className="szv2-btn-danger"
                onClick={handleClearLog}
                disabled={busy}
              >
                Limpar log
              </button>
            )}
          </div>

          {divergences.length === 0 ? (
            <div style={{ padding: '32px 0', textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Nenhuma divergência registrada.
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Pedido</th>
                    <th>Campo canônico</th>
                    <th>Valor canônico</th>
                    <th>Campo legado</th>
                    <th>Valor legado</th>
                    <th>Ação</th>
                  </tr>
                </thead>
                <tbody>
                  {divergences.map((d, i) => {
                    const actionColor =
                      d.action === 'divergence' ? 'var(--szv2-danger)'
                      : d.action === 'filled' ? 'var(--szv2-brand)'
                      : 'var(--szv2-text-muted)' // would_fill
                    return (
                      <tr key={`${d.order_id}-${d.canonical_key}-${i}`}>
                        <td><strong>#{d.order_id}</strong></td>
                        <td><code>{d.canonical_key}</code></td>
                        <td>{d.canonical_value || <span style={{ color: 'var(--szv2-text-muted)' }}>—</span>}</td>
                        <td><code>{d.legacy_key}</code></td>
                        <td style={{ color: d.action === 'divergence' ? 'var(--szv2-danger)' : undefined }}>
                          {d.legacy_value || <span style={{ color: 'var(--szv2-text-muted)' }}>—</span>}
                        </td>
                        <td style={{ color: actionColor, fontWeight: 500, fontSize: 12 }}>
                          {d.action || '—'}
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
    </div>
  )
}
