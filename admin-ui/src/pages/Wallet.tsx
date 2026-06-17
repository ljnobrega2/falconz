import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'

// ── Tipos ─────────────────────────────────────────────────────────────────────

type ClienteRow = {
  user_id: number
  nome: string
  email: string
  saldo: number
  saldo_reservado: number
  saldo_disponivel: number
  transacoes_count: number
  ultima_atualizacao: string
}

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

type WalletOwnerItem = {
  class_id: number
  class_name: string
  user_id: number
  user_nome: string
  user_email: string
}

type ConfigME = {
  token_masked: string
  token_from_env: boolean
  webhook_secret_masked: string
  webhook_secret_from_env: boolean
  jwt_secret_from_env: boolean
  webhook_url: string
  saldo_atual_me: number
  saldo_at: string
}

type ConfigRegras = { saldo_minimo: number }

type ConfigMotor = {
  pix_valid_minutes: number
  pix_auto_cancel_expired: boolean
  enforce_wallet_on_label: boolean
  block_duplicate_label: boolean
  checkout_template_id: number
}

type ConfigResp = { me: ConfigME; regras: ConfigRegras; motor: ConfigMotor }

type PixRow = {
  id: number
  user_id: number
  valor: number
  status: string
  me_pix_id: string | null
  expires_at: string | null
  paid_at: string | null
  created_at: string
}

type CronItem = {
  name: string
  frequency: string
  last_run: string | null
  last_status: string
  last_message: string
  next_run: string | null
}

type DetailResponse = {
  cliente: ClienteRow
  transacoes: Array<{
    id: number; tipo: string; valor: number; saldo_apos: number
    descricao: string; status: string; created_at: string
  }>
  recargas: Array<{
    id: number; valor: number; status: string; me_pix_id: string | null
    qr_src: string | null; copia_cola: string | null
    expires_at: string | null; created_at: string
  }>
}

type CreateRecargaResponse = {
  ok: boolean; recarga_id: number; user_id: number; valor: number
  qr_src: string; copia_cola: string; link: string
  expires_at: string; security_token: string; stub?: boolean
}

type Tab = 'clientes' | 'transacoes' | 'configuracoes' | 'restapi' | 'pix-reconciliacao'

// ── Helpers ──────────────────────────────────────────────────────────────────

const fmt = (v: number) =>
  'R$ ' + v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtBRL = (v: number) =>
  v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' })

const fmtDate = (s: string | null | undefined) =>
  s ? s.slice(0, 16).replace('T', ' ') : '—'

const TIPO_BADGE: Record<string, string> = {
  credito: 'szv2-badge-success',
  debito:  'szv2-badge-danger',
}

const STATUS_BADGE: Record<string, string> = {
  pendente:   'szv2-badge-warning',
  analise:    'szv2-badge-warning',
  confirmado: 'szv2-badge-success',
  pago:       'szv2-badge-success',
  cancelado:  'szv2-badge-danger',
  expirado:   'szv2-badge-neutral',
}

function KpiCard({
  label, value, sub, danger, success, brand,
}: {
  label: string; value: number | string; sub?: string
  danger?: boolean; success?: boolean; brand?: boolean
}) {
  const color = danger && Number(value) > 0
    ? 'var(--szv2-danger)'
    : success ? 'var(--szv2-success)'
    : brand ? 'var(--szv2-brand)'
    : 'var(--szv2-brand)'
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

// ── Modal: Emitir PIX ─────────────────────────────────────────────────────────

function PixModal({
  cliente, onClose, onSuccess,
}: {
  cliente: ClienteRow | null; onClose: () => void; onSuccess: () => void
}) {
  const [userIdInput, setUserIdInput] = useState(cliente?.user_id?.toString() || '')
  const [valor, setValor] = useState('100')
  const [motivo, setMotivo] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [result, setResult] = useState<CreateRecargaResponse | null>(null)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    if (!result?.expires_at) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [result])

  const remaining = useMemo(() => {
    if (!result?.expires_at) return 0
    const exp = new Date(result.expires_at).getTime()
    return Math.max(0, Math.floor((exp - now) / 1000))
  }, [result, now])

  function fmtCountdown(secs: number) {
    const m = Math.floor(secs / 60); const s = secs % 60
    return `${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault(); setErr('')
    const uid = parseInt(userIdInput, 10)
    const v = parseFloat(valor.replace(',', '.'))
    if (!uid || uid <= 0) { setErr('user_id inválido'); return }
    if (!v || v <= 0) { setErr('valor inválido'); return }
    setBusy(true)
    try {
      const r = await api<CreateRecargaResponse>(`/tpc-clientes/${uid}/recarga`, {
        method: 'POST', body: JSON.stringify({ valor: v, motivo }),
      })
      setResult(r); onSuccess()
    } catch (e: any) { setErr(e.message || 'Falha ao gerar PIX') }
    finally { setBusy(false) }
  }

  async function copyCopiaCola() {
    if (!result?.copia_cola) return
    try { await navigator.clipboard.writeText(result.copia_cola) } catch { /* ignora */ }
  }

  return (
    <div className="szv2-modal-overlay szv2-open" onClick={onClose}>
      <div className="szv2-modal szv2-modal-lg" onClick={e => e.stopPropagation()}>
        <div className="szv2-modal-head">
          <h3>Emitir PIX{cliente ? ` para ${cliente.nome || `#${cliente.user_id}`}` : ''}</h3>
          <button className="szv2-modal-x" onClick={onClose}>✕</button>
        </div>
        <div className="szv2-modal-body">
          {!result ? (
            <form onSubmit={submit}>
              {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}
              <div className="szv2-field" style={{ marginBottom: 12 }}>
                <label className="szv2-label">Cliente (user_id) *</label>
                <input className="szv2-input" type="number" required value={userIdInput}
                  disabled={!!cliente} onChange={e => setUserIdInput(e.target.value)}
                  placeholder="ID do usuário WordPress" />
                {cliente && (
                  <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                    {cliente.email || cliente.nome || `Usuário #${cliente.user_id}`}
                  </small>
                )}
              </div>
              <div className="szv2-field" style={{ marginBottom: 12 }}>
                <label className="szv2-label">Valor (R$) *</label>
                <input className="szv2-input" type="number" required min="1" step="0.01"
                  value={valor} onChange={e => setValor(e.target.value)} />
              </div>
              <div className="szv2-field" style={{ marginBottom: 12 }}>
                <label className="szv2-label">Motivo</label>
                <textarea className="szv2-input" rows={3} value={motivo}
                  onChange={e => setMotivo(e.target.value)}
                  placeholder="Ex.: ajuste de saldo solicitado pelo cliente" />
              </div>
            </form>
          ) : (
            <div>
              <div style={{ textAlign: 'center', marginBottom: 16 }}>
                <h3 style={{ margin: '0 0 4px', color: 'var(--szv2-brand)' }}>
                  PIX gerado — {fmt(result.valor)}
                </h3>
                <p style={{ margin: 0, fontSize: 13, color: 'var(--szv2-text-muted)' }}>
                  Recarga #{result.recarga_id} · user_id #{result.user_id}
                </p>
                {result.stub && (
                  <p style={{ margin: '8px 0 0', fontSize: 11, color: 'var(--szv2-warning)' }}>
                    Placeholder (modo dev) — aguardando integração com wallet-service.
                  </p>
                )}
              </div>
              <div style={{ display: 'flex', justifyContent: 'center', marginBottom: 12 }}>
                <img src={result.qr_src} alt="QR Code PIX"
                  style={{ width: 240, height: 240, border: '1px solid var(--szv2-divider)', borderRadius: 8, background: '#fff' }} />
              </div>
              <div style={{ textAlign: 'center', marginBottom: 12 }}>
                <span className="szv2-status-badge s-pendente" style={{ fontSize: 13, padding: '4px 12px' }}>
                  Expira em {fmtCountdown(remaining)}
                </span>
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Copia e cola</label>
                <textarea className="szv2-input" rows={3} readOnly value={result.copia_cola}
                  onFocus={e => e.currentTarget.select()}
                  style={{ fontFamily: 'monospace', fontSize: 11 }} />
                <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                  onClick={copyCopiaCola} style={{ marginTop: 8 }}>Copiar</button>
              </div>
            </div>
          )}
        </div>
        <div className="szv2-modal-foot">
          {!result ? (
            <>
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={onClose}>Cancelar</button>
              <button type="button" className="szv2-btn szv2-btn-brand" onClick={submit as any} disabled={busy}>
                {busy ? 'Gerando…' : 'Gerar PIX'}
              </button>
            </>
          ) : (
            <button type="button" className="szv2-btn szv2-btn-brand" onClick={onClose}>Fechar</button>
          )}
        </div>
      </div>
    </div>
  )
}

// ── Modal: Extrato do cliente ─────────────────────────────────────────────────

function ExtratoModal({
  cliente, onClose, onCancelled,
}: {
  cliente: ClienteRow; onClose: () => void; onCancelled: () => void
}) {
  const [subtab, setSubtab] = useState<'transacoes' | 'recargas'>('transacoes')
  const [detail, setDetail] = useState<DetailResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [cancelling, setCancelling] = useState<number | null>(null)

  async function load() {
    setLoading(true); setErr('')
    try {
      const r = await api<DetailResponse>(`/tpc-clientes/${cliente.user_id}`)
      setDetail(r)
    } catch (e: any) { setErr(e.message || 'Erro ao carregar extrato') }
    finally { setLoading(false) }
  }

  useEffect(() => { load() }, [cliente.user_id])

  async function cancelarRecarga(recargaID: number) {
    if (!window.confirm(`Cancelar recarga #${recargaID}?`)) return
    setCancelling(recargaID)
    try {
      await api(`/tpc-clientes/${cliente.user_id}/cancelar-recarga/${recargaID}`, { method: 'POST' })
      onCancelled(); await load()
    } catch (e: any) { setErr(e.message || 'Falha ao cancelar') }
    finally { setCancelling(null) }
  }

  return (
    <div className="szv2-modal-overlay szv2-open" onClick={onClose}>
      <div className="szv2-modal szv2-modal-lg" onClick={e => e.stopPropagation()}>
        <div className="szv2-modal-head">
          <h3>
            Extrato — {cliente.nome || `Usuário #${cliente.user_id}`}
            {cliente.email && (
              <span style={{ fontWeight: 400, fontSize: 13, color: 'var(--szv2-text-muted)', marginLeft: 8 }}>
                {cliente.email}
              </span>
            )}
          </h3>
          <button className="szv2-modal-x" onClick={onClose}>✕</button>
        </div>
        <div className="szv2-modal-body">
          {detail && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12, marginBottom: 16 }}>
              {[
                { label: 'Saldo total', value: fmt(detail.cliente.saldo), color: 'inherit' },
                { label: 'Reservado', value: fmt(detail.cliente.saldo_reservado), color: 'var(--szv2-warning)' },
                { label: 'Disponível', value: fmt(detail.cliente.saldo_disponivel), color: 'var(--szv2-success)' },
              ].map(k => (
                <div key={k.label} className="szv2-card" style={{ padding: 12, margin: 0 }}>
                  <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 }}>{k.label}</div>
                  <div style={{ fontSize: 20, fontWeight: 700, color: k.color, marginTop: 4 }}>{k.value}</div>
                </div>
              ))}
            </div>
          )}
          <div className="szv2-tabs" style={{ marginBottom: 12 }}>
            <button className="szv2-tab" aria-selected={subtab === 'transacoes'} onClick={() => setSubtab('transacoes')}>Transações</button>
            <button className="szv2-tab" aria-selected={subtab === 'recargas'} onClick={() => setSubtab('recargas')}>Recargas</button>
          </div>
          {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}
          {loading ? (
            <div className="szv2-empty"><h3>Carregando…</h3></div>
          ) : subtab === 'transacoes' ? (
            <div className="szv2-table-wrap">
              <table className="szv2-table">
                <thead><tr><th>Data</th><th>Tipo</th><th className="szv2-td-num">Valor</th><th>Status</th><th>Descrição</th></tr></thead>
                <tbody>
                  {(detail?.transacoes || []).map(t => (
                    <tr key={t.id}>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(t.created_at)}</td>
                      <td><span className={`szv2-status-badge ${t.tipo === 'credito' ? 's-confirmado' : t.tipo === 'debito' ? 's-cancelado' : 's-pendente'}`}>{t.tipo}</span></td>
                      <td className="szv2-td-num" style={{ fontWeight: 700, color: t.tipo === 'credito' ? 'var(--szv2-success)' : t.tipo === 'debito' ? 'var(--szv2-danger)' : 'inherit' }}>{fmt(t.valor)}</td>
                      <td><span className={`sz-badge ${STATUS_BADGE[t.status] || 'szv2-badge-neutral'}`}>{t.status}</span></td>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-soft)' }}>{t.descricao}</td>
                    </tr>
                  ))}
                  {(detail?.transacoes || []).length === 0 && (
                    <tr><td colSpan={5}><div className="szv2-empty"><h3>Sem transações</h3></div></td></tr>
                  )}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="szv2-table-wrap">
              <table className="szv2-table">
                <thead><tr><th>Data</th><th className="szv2-td-num">Valor</th><th>Status</th><th>PIX ID</th><th style={{ width: 140 }}>Ação</th></tr></thead>
                <tbody>
                  {(detail?.recargas || []).map(r => (
                    <tr key={r.id}>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(r.created_at)}</td>
                      <td className="szv2-td-num" style={{ fontWeight: 700 }}>{fmt(r.valor)}</td>
                      <td><span className={`sz-badge ${STATUS_BADGE[r.status] || 'szv2-badge-neutral'}`}>{r.status}</span></td>
                      <td style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--szv2-text-muted)', maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.me_pix_id ?? '—'}</td>
                      <td>{r.status === 'pendente' && (
                        <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-danger"
                          disabled={cancelling === r.id} onClick={() => cancelarRecarga(r.id)}>
                          {cancelling === r.id ? '…' : 'Cancelar'}
                        </button>
                      )}</td>
                    </tr>
                  ))}
                  {(detail?.recargas || []).length === 0 && (
                    <tr><td colSpan={5}><div className="szv2-empty"><h3>Sem recargas</h3></div></td></tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </div>
        <div className="szv2-modal-foot">
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={onClose}>Fechar</button>
        </div>
      </div>
    </div>
  )
}

// ── Aba Clientes ──────────────────────────────────────────────────────────────

function TabClientes() {
  const [items, setItems] = useState<ClienteRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const perPage = 100
  const [q, setQ] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [pixModal, setPixModal] = useState<ClienteRow | null>(null)
  const [extratoModal, setExtratoModal] = useState<ClienteRow | null>(null)

  async function load() {
    setLoading(true); setErr('')
    try {
      const qs = new URLSearchParams({ q, limit: String(perPage), page: String(page) }).toString()
      const r = await api<{ items: ClienteRow[]; total: number }>(`/tpc-clientes?${qs}`)
      setItems(r.items || []); setTotal(r.total || 0)
    } catch (e: any) { setErr(e.message || 'Erro ao carregar') }
    finally { setLoading(false) }
  }

  useEffect(() => { load() }, [q, page])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg }); setTimeout(() => setToast(null), 5000)
  }

  function applySearch(e: React.FormEvent) {
    e.preventDefault(); setPage(1); setQ(searchInput.trim())
  }

  const totalPages = Math.max(1, Math.ceil(total / perPage))

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <span style={{ fontSize: 14, color: 'var(--szv2-text-muted)' }}>{total} cliente(s) com carteira</span>
        <button type="button" className="szv2-btn szv2-btn-brand"
          onClick={() => setPixModal({} as ClienteRow)}>
          + Recarga PIX por cliente
        </button>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'} style={{ marginBottom: 16 }}>
          {toast.msg}
        </div>
      )}

      {/* Busca */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <form onSubmit={applySearch} style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
          <input className="szv2-input" placeholder="Buscar por email, nome ou user_id…"
            value={searchInput} onChange={e => setSearchInput(e.target.value)} style={{ flex: 1 }} />
          <button type="submit" className="szv2-btn szv2-btn-secondary">Buscar</button>
          {q && (
            <button type="button" className="szv2-btn szv2-btn-secondary"
              onClick={() => { setSearchInput(''); setQ(''); setPage(1) }}>Limpar</button>
          )}
        </form>
      </div>

      {/* Tabela */}
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th><th>Nome</th><th>Email</th>
              <th className="szv2-td-num">Saldo</th>
              <th className="szv2-td-num">Reservado</th>
              <th className="szv2-td-num">Disponível</th>
              <th className="szv2-td-num"># Tx</th>
              <th>Última atualização</th>
              <th style={{ width: 220 }}>Ações</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={9}><div className="szv2-empty"><h3>Carregando…</h3></div></td></tr>
            ) : items.length === 0 ? (
              <tr><td colSpan={9}><div className="szv2-empty"><h3>Nenhum cliente com carteira</h3></div></td></tr>
            ) : items.map(c => (
              <tr key={c.user_id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{c.user_id}</td>
                <td style={{ fontSize: 13 }}>{c.nome || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}</td>
                <td style={{ fontSize: 13 }}>{c.email || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}</td>
                <td className="szv2-td-num" style={{ fontWeight: 700 }}>{fmt(c.saldo)}</td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-warning)' }}>{fmt(c.saldo_reservado)}</td>
                <td className="szv2-td-num" style={{ fontWeight: 700, color: 'var(--szv2-success)' }}>{fmt(c.saldo_disponivel)}</td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-muted)' }}>{c.transacoes_count}</td>
                <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(c.ultima_atualizacao)}</td>
                <td>
                  <div style={{ display: 'flex', gap: 6 }}>
                    <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                      onClick={() => setExtratoModal(c)}>Ver extrato</button>
                    <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-brand"
                      onClick={() => setPixModal(c)}>Emitir PIX</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Paginação */}
      {total > perPage && (
        <div className="szv2-card" style={{ marginTop: 12, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <button type="button" className="szv2-btn szv2-btn-secondary"
            disabled={page <= 1 || loading} onClick={() => setPage(p => Math.max(1, p - 1))}>← Anterior</button>
          <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            Página <strong>{page}</strong> de <strong>{totalPages}</strong>
          </span>
          <button type="button" className="szv2-btn szv2-btn-secondary"
            disabled={page >= totalPages || loading} onClick={() => setPage(p => Math.min(totalPages, p + 1))}>Próximo →</button>
        </div>
      )}

      {/* Zona de perigo — reset total */}
      <details className="szv2-card" style={{ marginTop: 32, borderColor: 'var(--szv2-danger)', background: 'rgba(220,38,38,.04)' }}>
        <summary style={{ cursor: 'pointer', fontWeight: 700, color: 'var(--szv2-danger)', padding: '4px 0', listStyle: 'none' }}>
          Zona de perigo — Reset total das carteiras
        </summary>
        <ResetWalletPanel onResult={showToast} onReload={load} />
      </details>

      {pixModal && (
        <PixModal
          cliente={pixModal.user_id ? pixModal : null}
          onClose={() => setPixModal(null)}
          onSuccess={() => { showToast('ok', 'PIX gerado'); load() }}
        />
      )}
      {extratoModal && (
        <ExtratoModal
          cliente={extratoModal}
          onClose={() => setExtratoModal(null)}
          onCancelled={() => showToast('ok', 'Recarga cancelada')}
        />
      )}
    </div>
  )
}

// ── Reset wallet panel ────────────────────────────────────────────────────────

function ResetWalletPanel({
  onResult, onReload,
}: { onResult: (kind: 'ok' | 'err', msg: string) => void; onReload: () => void }) {
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const ready = confirm === 'RESETAR'

  async function reset() {
    if (!ready) return
    if (!window.confirm('Tem ABSOLUTA certeza? Essa ação apaga TODAS as carteiras, transações e recargas.')) return
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; result: { carteira_deleted: number; transacoes_deleted: number; recargas_deleted: number } }>(
        '/tpc-clientes/reset-wallet-all', { method: 'POST', body: JSON.stringify({ confirm: 'RESETAR' }) }
      )
      const x = r.result
      onResult('ok', `Reset OK · ${x.carteira_deleted} carteiras, ${x.transacoes_deleted} transações, ${x.recargas_deleted} recargas removidas.`)
      setConfirm(''); onReload()
    } catch (e: any) { onResult('err', e.message || 'Falha ao resetar') }
    finally { setBusy(false) }
  }

  return (
    <div style={{ marginTop: 16 }}>
      <p style={{ fontSize: 14, lineHeight: 1.5, marginTop: 0 }}>
        <strong>Esta ação é IRREVERSÍVEL.</strong> Todos os registros em{' '}
        <code>tpc_carteira</code>, <code>tpc_transacoes</code> e <code>tpc_recargas</code>{' '}
        serão deletados.
      </p>
      <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
        Para confirmar, digite exatamente <strong>RESETAR</strong> abaixo (case-sensitive):
      </p>
      <div style={{ display: 'flex', gap: 12, alignItems: 'flex-end', marginTop: 12 }}>
        <div className="szv2-field" style={{ flex: 1 }}>
          <label className="szv2-label">Confirmação</label>
          <input className="szv2-input" value={confirm}
            onChange={e => setConfirm(e.target.value)} placeholder="Digite RESETAR" autoComplete="off" />
        </div>
        <button type="button" className="szv2-btn szv2-btn-danger"
          disabled={!ready || busy} onClick={reset} style={{ marginBottom: 0 }}>
          {busy ? 'Resetando…' : 'RESET TOTAL'}
        </button>
      </div>
    </div>
  )
}

// ── Aba Transações ────────────────────────────────────────────────────────────

type TipoFilter = '' | 'credito' | 'debito'
type StatusTxFilter = '' | 'pendente' | 'analise' | 'confirmado' | 'cancelado'

const TIPO_CHIPS = [
  { key: '' as TipoFilter, label: 'Todos' },
  { key: 'credito' as TipoFilter, label: 'Crédito' },
  { key: 'debito' as TipoFilter, label: 'Débito' },
]

const STATUS_TX_CHIPS = [
  { key: '' as StatusTxFilter, label: 'Todos' },
  { key: 'pendente' as StatusTxFilter, label: 'Pendente' },
  { key: 'analise' as StatusTxFilter, label: 'Análise' },
  { key: 'confirmado' as StatusTxFilter, label: 'Confirmado' },
  { key: 'cancelado' as StatusTxFilter, label: 'Cancelado' },
]

function Chip({ active, onClick, children, disabled }: {
  active: boolean; onClick: () => void; children: React.ReactNode; disabled?: boolean
}) {
  return (
    <button type="button" className="szv2-tab" aria-selected={active}
      disabled={disabled} onClick={onClick}
      style={{ minHeight: 32, padding: '6px 14px', fontSize: 13 }}>
      {children}
    </button>
  )
}

function TabTransacoes() {
  const PER_PAGE = 20
  const [items, setItems] = useState<TransacaoRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [tipo, setTipo] = useState<TipoFilter>('')
  const [status, setStatus] = useState<StatusTxFilter>('')
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
    setToast({ kind, msg }); setTimeout(() => setToast(null), 5000)
  }

  async function load() {
    setLoading(true); setErr('')
    try {
      const qs = new URLSearchParams()
      qs.set('page', String(page)); qs.set('per_page', String(PER_PAGE))
      if (tipo)          qs.set('tipo', tipo)
      if (status)        qs.set('status', status)
      if (dataIni)       qs.set('data_ini', dataIni)
      if (dataFim)       qs.set('data_fim', dataFim)
      if (userIdApplied) qs.set('user_id', userIdApplied)
      const r = await api<{ items: TransacaoRow[]; total: number }>(`/tpc-transacoes?${qs.toString()}`)
      setItems(r.items || []); setTotal(r.total || 0)
    } catch (e: any) { setErr(e.message || 'Erro ao carregar') }
    finally { setLoading(false) }
  }

  useEffect(() => { load() }, [page, tipo, status, dataIni, dataFim, userIdApplied])

  function applyTipo(t: TipoFilter)        { setPage(1); setTipo(t) }
  function applyStatus(s: StatusTxFilter)  { setPage(1); setStatus(s) }

  function applyUserId(e: React.FormEvent) {
    e.preventDefault(); setPage(1); setUserIdApplied(userIdInput.trim())
  }

  function clearAll() {
    setTipo(''); setStatus(''); setDataIni(''); setDataFim('')
    setUserIdInput(''); setUserIdApplied(''); setPage(1)
    setDraftTipo(''); setDraftStatus('')
    setDraftIni(''); setDraftFim(''); setDraftUser('')
    setFilterOpen(false)
  }

  // Painel
  const [filterOpen, setFilterOpen] = useState(false)
  const [draftTipo, setDraftTipo] = useState<TipoFilter>('')
  const [draftStatus, setDraftStatus] = useState<StatusTxFilter>('')
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
    if (!window.confirm(`Excluir transação #${id}?\n\nO saldo do cliente será recalculado. IRREVERSÍVEL.`)) return
    setDeletingId(id)
    try {
      const r = await api<{ ok: boolean; user_id: number; novo_saldo: number }>(`/tpc-transacoes/${id}`, { method: 'DELETE' })
      showToast('ok', `Transação #${id} excluída. Novo saldo user_id ${r.user_id}: ${fmt(r.novo_saldo)}`)
      if (items.length === 1 && page > 1) setPage(p => Math.max(1, p - 1))
      else await load()
    } catch (e: any) { showToast('err', e.message || 'Falha ao excluir') }
    finally { setDeletingId(null) }
  }

  async function handleVerificarPix() {
    if (!window.confirm('Disparar verificação de PIX pendentes agora?')) return
    setBusy(true)
    try {
      const r = await api<{ ok: boolean; queued: number; stub?: boolean }>('/tpc-transacoes/verificar-pix', { method: 'POST' })
      const note = r.stub ? ' (modo dev — wallet-service ainda não integrado)' : ''
      showToast('ok', `${r.queued} PIX pendente(s) enfileirado(s)${note}.`)
      await load()
    } catch (e: any) { showToast('err', e.message || 'Falha ao verificar PIX') }
    finally { setBusy(false) }
  }

  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE))
  const hasFilter = tipo !== '' || status !== '' || dataIni !== '' || dataFim !== '' || userIdApplied !== ''

  // Chips ativos
  const chips: ActiveChip[] = []
  if (tipo) chips.push({ key: 'tipo', label: `Tipo: ${tipo}`, onRemove: () => applyTipo('') })
  if (status) chips.push({ key: 'status', label: `Status: ${status}`, onRemove: () => applyStatus('') })
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => { setDataIni(''); setPage(1) } })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => { setDataFim(''); setPage(1) } })
  if (userIdApplied) chips.push({ key: 'user', label: `User: #${userIdApplied}`, onRemove: () => { setUserIdInput(''); setUserIdApplied(''); setPage(1) } })

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <span style={{ fontSize: 14, color: 'var(--szv2-text-muted)' }}>
          {total} transação(ões){hasFilter ? ' filtrada(s)' : ''}
        </span>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton
            active={chips.length > 0}
            count={chips.length}
            onClick={openPanel}
          />
          <button type="button" className="szv2-btn szv2-btn-secondary"
            onClick={handleVerificarPix} disabled={busy}>
            {busy ? 'Verificando…' : 'Verificar PIX agora'}
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearAll} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'} style={{ marginBottom: 16 }}>
          {toast.msg}
        </div>
      )}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearAll}
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
            {TIPO_CHIPS.map(c => <option key={c.key || 'all-t'} value={c.key}>{c.label}</option>)}
          </select>
        </FilterField>
        <FilterField label="Status">
          <select
            style={filterInputStyle}
            value={draftStatus}
            onChange={e => setDraftStatus(e.target.value as StatusTxFilter)}
          >
            {STATUS_TX_CHIPS.map(c => <option key={c.key || 'all-s'} value={c.key}>{c.label}</option>)}
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
              <th>ID</th><th>Usuário</th><th>Tipo</th><th>Status</th>
              <th className="szv2-td-num">Valor</th><th className="szv2-td-num">Saldo após</th>
              <th>Descrição</th><th>Data</th><th style={{ width: 90 }}>Ações</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={9}><div className="szv2-empty"><h3>Carregando…</h3></div></td></tr>
            ) : items.length === 0 ? (
              <tr><td colSpan={9}><div className="szv2-empty"><h3>Nenhuma transação encontrada</h3>{hasFilter && <p>Tente ajustar os filtros.</p>}</div></td></tr>
            ) : items.map(t => {
              const isC = t.tipo === 'credito'; const isD = t.tipo === 'debito'
              return (
                <tr key={t.id}>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{t.id}</td>
                  <td style={{ fontSize: 13 }}>
                    {t.nome || t.email ? (
                      <div>
                        <div style={{ fontWeight: 600 }}>{t.nome || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}</div>
                        {t.email && <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{t.email}</div>}
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-faint)' }}>ID #{t.user_id}</div>
                      </div>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-faint)' }}>#{t.user_id}</span>
                    )}
                  </td>
                  <td><span className={`sz-badge ${TIPO_BADGE[t.tipo] || 'szv2-badge-neutral'}`}>{isC ? '+ Crédito' : isD ? '− Débito' : t.tipo}</span></td>
                  <td><span className={`sz-badge ${STATUS_BADGE[t.status] || 'szv2-badge-neutral'}`}>{t.status}</span></td>
                  <td className="szv2-td-num" style={{ fontWeight: 700, color: isC ? 'var(--szv2-success)' : isD ? 'var(--szv2-danger)' : 'inherit' }}>{fmt(t.valor)}</td>
                  <td className="szv2-td-num" style={{ color: 'var(--szv2-text-muted)' }}>{fmt(t.saldo_apos)}</td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-soft)', maxWidth: 280 }}>
                    {t.descricao || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                    {t.order_id && <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginTop: 2 }}>Pedido #{t.order_id}</div>}
                  </td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>{fmtDate(t.created_at)}</td>
                  <td>
                    <button type="button" className="szv2-btn szv2-btn-sm szv2-btn-danger"
                      disabled={deletingId === t.id} onClick={() => handleDelete(t.id)}>
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
        <div className="szv2-card" style={{ marginTop: 12, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <button type="button" className="szv2-btn szv2-btn-secondary"
            disabled={page <= 1 || loading} onClick={() => setPage(p => Math.max(1, p - 1))}>← Anterior</button>
          <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            Página <strong>{page}</strong> de <strong>{totalPages}</strong>
          </span>
          <button type="button" className="szv2-btn szv2-btn-secondary"
            disabled={page >= totalPages || loading} onClick={() => setPage(p => Math.min(totalPages, p + 1))}>Próximo →</button>
        </div>
      )}
    </div>
  )
}

// ── Aba Configurações ─────────────────────────────────────────────────────────

function TabConfiguracoes() {
  const [cfg, setCfg] = useState<ConfigResp | null>(null)
  const [meTokenInput, setMeTokenInput] = useState('')
  const [whSecretInput, setWhSecretInput] = useState('')
  const [showMeToken, setShowMeToken] = useState(false)
  const [showWhSecret, setShowWhSecret] = useState(false)
  const [saldoMinimo, setSaldoMinimo] = useState(0)
  const [pixValidMinutes, setPixValidMinutes] = useState(30)
  const [pixAutoCancel, setPixAutoCancel] = useState(true)
  const [enforceWallet, setEnforceWallet] = useState(true)
  const [blockDuplicate, setBlockDuplicate] = useState(true)
  const [checkoutTemplateID, setCheckoutTemplateID] = useState(140)
  const [owners, setOwners] = useState<WalletOwnerItem[]>([])
  const [ownersOpen, setOwnersOpen] = useState(false)
  const [showAddOwner, setShowAddOwner] = useState(false)
  const [addClassID, setAddClassID] = useState('')
  const [addUserID, setAddUserID] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg }); setTimeout(() => setToast(null), 5000)
  }

  async function loadAll() {
    setLoading(true); setErr('')
    try {
      const [c, ow] = await Promise.all([
        api<ConfigResp>('/tpc-config'),
        api<{ items: WalletOwnerItem[] }>('/tpc-config/wallet-owners'),
      ])
      setCfg(c)
      setSaldoMinimo(c.regras.saldo_minimo)
      setPixValidMinutes(c.motor.pix_valid_minutes)
      setPixAutoCancel(c.motor.pix_auto_cancel_expired)
      setEnforceWallet(c.motor.enforce_wallet_on_label)
      setBlockDuplicate(c.motor.block_duplicate_label)
      setCheckoutTemplateID(c.motor.checkout_template_id)
      setMeTokenInput(''); setWhSecretInput('')
      setOwners(ow.items || [])
    } catch (e: any) { setErr(e.message || 'Erro ao carregar configurações') }
    finally { setLoading(false) }
  }

  useEffect(() => { loadAll() }, [])

  async function handleSave() {
    if (!cfg) return
    if (pixValidMinutes < 5 || pixValidMinutes > 1440) {
      showToast('err', 'PIX válido deve estar entre 5 e 1440 minutos.'); return
    }
    setSaving(true)
    try {
      const body: any = {
        saldo_minimo: saldoMinimo, pix_valid_minutes: pixValidMinutes,
        pix_auto_cancel_expired: pixAutoCancel, enforce_wallet_on_label: enforceWallet,
        block_duplicate_label: blockDuplicate, checkout_template_id: checkoutTemplateID,
      }
      if (meTokenInput.trim() && !cfg.me.token_from_env) body.me_token = meTokenInput.trim()
      if (whSecretInput.trim() && !cfg.me.webhook_secret_from_env) body.webhook_secret = whSecretInput.trim()
      await api('/tpc-config', { method: 'POST', body: JSON.stringify(body) })
      showToast('ok', 'Configurações salvas.'); await loadAll()
    } catch (e: any) { showToast('err', e.message || 'Falha ao salvar') }
    finally { setSaving(false) }
  }

  async function handleRegenerate(which: 'webhook' | 'jwt') {
    if (!cfg) return
    const label = which === 'webhook' ? 'Webhook' : 'JWT'
    const fromEnv = which === 'webhook' ? cfg.me.webhook_secret_from_env : cfg.me.jwt_secret_from_env
    if (fromEnv) { showToast('err', `Segredo ${label} está gerenciado por variável de ambiente.`); return }
    if (!window.confirm(`Gerar novo segredo ${label}?\n\nO valor antigo será invalidado. Continuar?`)) return
    try {
      const r = await api<{ ok: boolean; masked: string }>('/tpc-config/regenerate-secret', {
        method: 'POST', body: JSON.stringify({ which }),
      })
      showToast('ok', `Segredo ${label} regenerado: ${r.masked}`); await loadAll()
    } catch (e: any) { showToast('err', e.message || 'Falha ao regenerar') }
  }

  async function copyToClipboard(text: string) {
    try { await navigator.clipboard.writeText(text); showToast('ok', 'Copiado.') }
    catch { showToast('err', 'Não foi possível copiar.') }
  }

  function handleAddOwner() {
    const cid = parseInt(addClassID.trim(), 10); const uid = parseInt(addUserID.trim(), 10)
    if (!isFinite(cid) || cid < 0) { showToast('err', 'ID da classe inválido.'); return }
    if (!isFinite(uid) || uid <= 0) { showToast('err', 'ID do usuário inválido.'); return }
    setOwners(prev => [
      ...prev.filter(p => p.class_id !== cid),
      { class_id: cid, class_name: `Classe #${cid}`, user_id: uid, user_nome: '', user_email: '' },
    ])
    setAddClassID(''); setAddUserID(''); setShowAddOwner(false)
  }

  function handleRemoveOwner(class_id: number) {
    if (!window.confirm(`Remover mapeamento da classe #${class_id}?`)) return
    setOwners(prev => prev.filter(p => p.class_id !== class_id))
  }

  async function handleSaveOwners() {
    setSaving(true)
    try {
      await api('/tpc-config/wallet-owners', {
        method: 'POST',
        body: JSON.stringify({ items: owners.map(o => ({ class_id: o.class_id, user_id: o.user_id })) }),
      })
      showToast('ok', 'Mapeamentos salvos.'); await loadAll()
    } catch (e: any) { showToast('err', e.message || 'Falha ao salvar mapeamentos') }
    finally { setSaving(false) }
  }

  if (loading) return <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>Carregando configurações…</div>
  if (!cfg) return <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err || 'Não foi possível carregar.'}</div>

  const envBanner = (
    <div style={{ background: 'rgba(234,88,12,.08)', border: '1px solid rgba(234,88,12,.25)', color: 'var(--szv2-brand)', padding: '8px 12px', borderRadius: 8, fontSize: 13, marginBottom: 12 }}>
      Esta configuração está sendo gerenciada por variável de ambiente.
    </div>
  )

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'} style={{ marginBottom: 16 }}>
          {toast.msg}
        </div>
      )}

      {/* Saldo ME */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head"><div><h2>Melhor Envio</h2><p className="szv2-card-sub">Token OAuth, segredo de webhook e saldo atual na conta ME.</p></div></div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {/* Token */}
          <div className="szv2-field">
            <label className="szv2-label">Token de acesso ME</label>
            {cfg.me.token_from_env && envBanner}
            {cfg.me.token_masked && <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>Atual: <code>{cfg.me.token_masked}</code></div>}
            {cfg.me.token_from_env ? (
              <input className="szv2-input" type="text" value={cfg.me.token_masked} readOnly style={{ color: 'var(--szv2-text-muted)', cursor: 'default' }} />
            ) : (
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input className="szv2-input" type={showMeToken ? 'text' : 'password'}
                  value={meTokenInput} onChange={e => setMeTokenInput(e.target.value)}
                  placeholder={cfg.me.token_masked ? 'Digite o novo token para substituir…' : 'Cole o Bearer aqui'}
                  autoComplete="off" style={{ flex: 1 }} />
                <button type="button" className="szv2-btn-secondary" onClick={() => setShowMeToken(s => !s)}>
                  {showMeToken ? 'Esconder' : 'Mostrar'}
                </button>
              </div>
            )}
          </div>
          {/* Webhook secret */}
          <div className="szv2-field">
            <label className="szv2-label">Webhook secret</label>
            {cfg.me.webhook_secret_from_env && envBanner}
            {cfg.me.webhook_secret_masked && <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>Atual: <code>{cfg.me.webhook_secret_masked}</code></div>}
            {cfg.me.webhook_secret_from_env ? (
              <input className="szv2-input" type="text" value={cfg.me.webhook_secret_masked} readOnly style={{ color: 'var(--szv2-text-muted)', cursor: 'default' }} />
            ) : (
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input className="szv2-input" type={showWhSecret ? 'text' : 'password'}
                  value={whSecretInput} onChange={e => setWhSecretInput(e.target.value)}
                  placeholder={cfg.me.webhook_secret_masked ? 'Digite o novo segredo para substituir…' : 'Gere ou cole aqui'}
                  autoComplete="off" style={{ flex: 1 }} />
                <button type="button" className="szv2-btn-secondary" onClick={() => setShowWhSecret(s => !s)}>{showWhSecret ? 'Esconder' : 'Mostrar'}</button>
                <button type="button" className="szv2-btn-secondary" onClick={() => handleRegenerate('webhook')}>Regenerar</button>
              </div>
            )}
          </div>
          {/* JWT secret */}
          <div className="szv2-field">
            <label className="szv2-label">JWT secret</label>
            {cfg.me.jwt_secret_from_env && envBanner}
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <button type="button" className="szv2-btn-secondary"
                onClick={() => handleRegenerate('jwt')} disabled={cfg.me.jwt_secret_from_env}>
                Regenerar JWT
              </button>
              <span className="szv2-card-sub">O segredo JWT só é exibido no momento da regeneração.</span>
            </div>
          </div>
          {/* Webhook URL */}
          <div className="szv2-field">
            <label className="szv2-label">URL do webhook PIX</label>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input className="szv2-input" type="text" value={cfg.me.webhook_url} readOnly style={{ flex: 1, color: 'var(--szv2-text-muted)', cursor: 'default' }} />
              <button type="button" className="szv2-btn-secondary" onClick={() => copyToClipboard(cfg.me.webhook_url)}>Copiar</button>
            </div>
          </div>
          {/* Saldo ME */}
          <div className="szv2-kpi">
            <span className="szv2-kpi-label">Saldo atual na conta ME</span>
            <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)', fontSize: 32 }}>{fmtBRL(cfg.me.saldo_atual_me)}</span>
            {cfg.me.saldo_at && <span className="szv2-kpi-meta">Atualizado em {cfg.me.saldo_at}</span>}
          </div>
        </div>
      </div>

      {/* Regras carteira */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head"><div><h2>Regras da carteira</h2><p className="szv2-card-sub">Limites e alertas do saldo do cliente.</p></div></div>
        <div className="szv2-field">
          <label className="szv2-label">Saldo mínimo para alerta (R$)</label>
          <input className="szv2-input" type="number" min="0" step="0.01"
            value={isFinite(saldoMinimo) ? saldoMinimo : 0}
            onChange={e => setSaldoMinimo(parseFloat(e.target.value) || 0)} style={{ maxWidth: 180 }} />
        </div>
      </div>

      {/* Motor Senderzz */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head"><div><h2>Motor Senderzz</h2><p className="szv2-card-sub">PIX, emissão de etiquetas e template de checkout.</p></div></div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
          <div className="szv2-field">
            <label className="szv2-label">Validade visual do PIX (minutos) — atual: {pixValidMinutes}</label>
            <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
              <input type="range" min={5} max={1440} step={1} value={pixValidMinutes}
                onChange={e => setPixValidMinutes(parseInt(e.target.value, 10) || 30)} style={{ flex: 1 }} />
              <input className="szv2-input" type="number" min={5} max={1440} step={1} value={pixValidMinutes}
                onChange={e => { const v = parseInt(e.target.value, 10); if (isFinite(v)) setPixValidMinutes(v) }} style={{ width: 100 }} />
            </div>
          </div>
          {[
            { checked: pixAutoCancel, onChange: setPixAutoCancel, title: 'Cancelar PIX expirado automaticamente', desc: 'Cancela localmente recargas PIX vencidas se ainda estiverem pendentes.' },
            { checked: enforceWallet, onChange: setEnforceWallet, title: 'Validar saldo ao emitir etiqueta', desc: 'Bloqueia emissão se o cliente não tiver saldo disponível.' },
            { checked: blockDuplicate, onChange: setBlockDuplicate, title: 'Bloquear etiqueta duplicada', desc: 'Impede gerar nova etiqueta se o pedido já tiver protocolo/rastreio.' },
          ].map(it => (
            <label key={it.title} style={{ display: 'flex', alignItems: 'flex-start', gap: 10, cursor: 'pointer' }}>
              <input type="checkbox" checked={it.checked} onChange={e => it.onChange(e.target.checked)} style={{ marginTop: 3 }} />
              <div>
                <div style={{ fontWeight: 600 }}>{it.title}</div>
                <div className="szv2-card-sub">{it.desc}</div>
              </div>
            </label>
          ))}
          <div className="szv2-field">
            <label className="szv2-label">Template de Checkout FunnelKit</label>
            <input className="szv2-input" type="number" min={1} step={1}
              value={isFinite(checkoutTemplateID) ? checkoutTemplateID : 140}
              onChange={e => setCheckoutTemplateID(parseInt(e.target.value, 10) || 140)} style={{ maxWidth: 180 }} />
          </div>
        </div>
      </div>

      {/* Dono financeiro por classe */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head" style={{ cursor: 'pointer' }} onClick={() => setOwnersOpen(o => !o)}>
          <div>
            <h2>Dono financeiro por classe de entrega</h2>
            <p className="szv2-card-sub">{ownersOpen ? 'Clique para recolher' : 'Clique para expandir'} · {owners.length} mapeamento(s)</p>
          </div>
          <span style={{ fontSize: 18, color: 'var(--szv2-text-muted)', transform: ownersOpen ? 'rotate(180deg)' : 'rotate(0deg)', transition: 'transform .15s' }}>▼</span>
        </div>
        {ownersOpen && (
          <div>
            {owners.length === 0 ? (
              <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)', border: '1px dashed var(--szv2-border)', borderRadius: 8, marginBottom: 12 }}>Nenhum mapeamento configurado.</div>
            ) : (
              <div style={{ overflowX: 'auto', marginBottom: 12 }}>
                <table className="szv2-table">
                  <thead><tr><th>Class ID</th><th>Nome</th><th>User ID</th><th>Usuário</th><th>Ação</th></tr></thead>
                  <tbody>
                    {owners.map(o => (
                      <tr key={o.class_id}>
                        <td><code>#{o.class_id}</code></td>
                        <td>{o.class_name}</td>
                        <td>
                          <input className="szv2-input" type="number" min={1} step={1} value={o.user_id || ''}
                            onChange={e => {
                              const uid = parseInt(e.target.value, 10)
                              setOwners(prev => prev.map(p => p.class_id === o.class_id ? { ...p, user_id: isFinite(uid) && uid > 0 ? uid : 0 } : p))
                            }} style={{ width: 110 }} />
                        </td>
                        <td>{o.user_nome || o.user_email ? <span><strong>{o.user_nome || '—'}</strong>{o.user_email && <span style={{ color: 'var(--szv2-text-muted)', marginLeft: 6 }}>({o.user_email})</span>}</span> : <span style={{ color: 'var(--szv2-text-muted)' }}>(não encontrado)</span>}</td>
                        <td><button type="button" className="szv2-btn-secondary" onClick={() => handleRemoveOwner(o.class_id)}>Remover</button></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <button type="button" className="szv2-btn-secondary" onClick={() => setShowAddOwner(true)}>Adicionar mapeamento</button>
              <button type="button" className="szv2-btn-brand" onClick={handleSaveOwners} disabled={saving}>{saving ? 'Salvando…' : 'Salvar mapeamentos'}</button>
            </div>
            {showAddOwner && (
              <div style={{ marginTop: 12, padding: 12, border: '1px solid var(--szv2-border)', borderRadius: 8, background: 'rgba(234,88,12,.04)' }}>
                <div style={{ display: 'flex', gap: 12, alignItems: 'flex-end' }}>
                  <div style={{ flex: 1 }}>
                    <label className="szv2-label">Class ID</label>
                    <input className="szv2-input" type="number" min={0} step={1} value={addClassID} onChange={e => setAddClassID(e.target.value)} placeholder="0 = sem classe" />
                  </div>
                  <div style={{ flex: 1 }}>
                    <label className="szv2-label">User ID</label>
                    <input className="szv2-input" type="number" min={1} step={1} value={addUserID} onChange={e => setAddUserID(e.target.value)} placeholder="ex.: 5" />
                  </div>
                  <button type="button" className="szv2-btn-brand" onClick={handleAddOwner}>Adicionar</button>
                  <button type="button" className="szv2-btn-secondary" onClick={() => { setShowAddOwner(false); setAddClassID(''); setAddUserID('') }}>Cancelar</button>
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Footer salvar */}
      <div style={{ marginTop: 24, display: 'flex', gap: 12, alignItems: 'center', justifyContent: 'flex-end', borderTop: '1px solid var(--szv2-border)', paddingTop: 16 }}>
        <button type="button" className="szv2-btn-secondary" onClick={loadAll} disabled={saving}>Recarregar</button>
        <button type="button" className="szv2-btn-brand" onClick={handleSave} disabled={saving} style={{ minWidth: 200 }}>
          {saving ? 'Salvando…' : 'Salvar configurações'}
        </button>
      </div>
    </div>
  )
}

// ── Aba REST API (estática) ───────────────────────────────────────────────────

function TabRestApi() {
  const [webhookUrl, setWebhookUrl] = useState('')

  useEffect(() => {
    api<ConfigResp>('/tpc-config').then(c => setWebhookUrl(c.me.webhook_url)).catch(() => {})
  }, [])

  const endpoints = [
    { method: 'GET',  path: '/wp-json/tp-carteira/v1/carteira',         desc: 'Retorna saldo e saldo_reservado do usuário autenticado.' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/recarga',          desc: 'Inicia recarga PIX (gera QR code e copia-cola).' },
    { method: 'GET',  path: '/wp-json/tp-carteira/v1/recargas',         desc: 'Lista recargas do usuário (paginado).' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/webhook/pix',      desc: 'Webhook PIX entrante (HMAC validado). Confirma recarga.' },
    { method: 'GET',  path: '/wp-json/tp-carteira/v1/transacoes',       desc: 'Extrato de transações do usuário.' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/reservar',         desc: 'Reserva saldo para emissão de etiqueta (BEGIN reserva).' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/debitar-reserva',  desc: 'Debita reserva após etiqueta emitida com sucesso.' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/estornar-reserva', desc: 'Libera reserva em caso de falha na emissão.' },
  ]

  const jsFetch = `const url = '${webhookUrl || 'https://app.senderzz.com.br/wp-json/tp-carteira/v1/webhook/pix'}';

// Exemplo: notificação PIX confirmado
const body = {
  type: 'PAYMENT',
  pix_id: 'stub-abc123',
  status: 'paid',
  amount: 100.00,
};

// Assinar com HMAC-SHA256 usando o webhook_secret
// No lado do servidor, o header é: X-Webhook-Signature
const sig = hmacSHA256(JSON.stringify(body), webhookSecret);

fetch(url, {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'X-Webhook-Signature': sig,
  },
  body: JSON.stringify(body),
});`

  return (
    <div>
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head"><div><h2>Endpoints tp-carteira/v1</h2><p className="szv2-card-sub">8 endpoints do módulo carteira/PIX registrados no WordPress.</p></div></div>
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr><th style={{ width: 80 }}>Método</th><th>Endpoint</th><th>Descrição</th></tr></thead>
            <tbody>
              {endpoints.map(e => (
                <tr key={e.path}>
                  <td>
                    <span className={`sz-badge ${e.method === 'GET' ? 'szv2-badge-brand' : 'szv2-badge-success'}`} style={{ fontFamily: 'monospace', fontSize: 11 }}>
                      {e.method}
                    </span>
                  </td>
                  <td><code style={{ fontSize: 12 }}>{e.path}</code></td>
                  <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)' }}>{e.desc}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head"><div><h2>URL do webhook PIX</h2><p className="szv2-card-sub">Configure no painel Melhor Envio para receber notificações.</p></div></div>
        <div className="szv2-field">
          <input className="szv2-input" type="text" readOnly
            value={webhookUrl || 'Carregando…'}
            style={{ fontFamily: 'monospace', fontSize: 13, color: 'var(--szv2-text-muted)', cursor: 'default' }} />
        </div>
      </div>

      <div className="szv2-card">
        <div className="szv2-card-head"><div><h2>Exemplo de integração JS</h2><p className="szv2-card-sub">Como assinar e enviar notificação ao webhook PIX.</p></div></div>
        <pre style={{
          background: 'var(--szv2-surface-alt, #f5f5f5)', borderRadius: 8, padding: 16,
          fontSize: 12, fontFamily: 'monospace', overflowX: 'auto', lineHeight: 1.6,
          color: 'var(--szv2-text)', border: '1px solid var(--szv2-border)',
        }}>
          {jsFetch}
        </pre>
      </div>
    </div>
  )
}

// ── Aba PIX / Reconciliação ───────────────────────────────────────────────────

const PIX_STATUS_CLS: Record<string, string> = {
  pendente: 's-pendente', confirmado: 's-confirmado', expirado: 's-expirado', cancelado: 's-cancelado',
}

function TabPixReconciliacao() {
  const [pixItems, setPixItems] = useState<PixRow[]>([])
  const [pixTotal, setPixTotal] = useState(0)
  const [pixStatus, setPixStatus] = useState('pendente')
  const [crons, setCrons] = useState<CronItem[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [acting, setActing] = useState<number | null>(null)
  const [triggerBusy, setTriggerBusy] = useState(false)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg }); setTimeout(() => setToast(null), 5000)
  }

  async function loadAll() {
    setLoading(true); setErr('')
    try {
      const [pr, cr] = await Promise.all([
        api<{ items: PixRow[]; total: number }>(`/pix?status=${pixStatus}&limit=100`),
        api<{ items: CronItem[] }>('/crons'),
      ])
      setPixItems(pr.items || []); setPixTotal(pr.total || 0)
      const names = ['sz_pix_auto_reconcile_cron', 'sz_wallet_divergence_check', 'tpc_cron_verificar_recargas_pix']
      setCrons((cr.items || []).filter(c => names.includes(c.name)))
    } catch (e: any) { setErr(e.message || 'Erro ao carregar') }
    finally { setLoading(false) }
  }

  useEffect(() => { loadAll() }, [pixStatus])

  async function setPixStatusFn(id: number, s: string) {
    setActing(id)
    try {
      await api(`/pix/${id}/status`, { method: 'PUT', body: JSON.stringify({ status: s }) })
      showToast('ok', `PIX #${id} marcado como ${s}.`); loadAll()
    } catch (e: any) { showToast('err', e.message || 'Falha') }
    finally { setActing(null) }
  }

  async function triggerReconcile() {
    if (!window.confirm('Disparar reconciliação PIX agora (sz_pix_auto_reconcile_cron)?')) return
    setTriggerBusy(true)
    try {
      await api('/crons/sz_pix_auto_reconcile_cron/trigger', { method: 'POST' })
      showToast('ok', 'Cron de reconciliação disparado.'); loadAll()
    } catch (e: any) { showToast('err', e.message || 'Falha ao disparar cron') }
    finally { setTriggerBusy(false) }
  }

  const reconcileCron = crons.find(c => c.name === 'sz_pix_auto_reconcile_cron')
  const verifyPix = crons.find(c => c.name === 'tpc_cron_verificar_recargas_pix')
  const divergCheck = crons.find(c => c.name === 'sz_wallet_divergence_check')

  function cronBadgeCls(status: string) {
    if (status === 'ok' || status === 'manual_trigger') return 'szv2-badge-success'
    if (status === 'err') return 'szv2-badge-danger'
    if (status === 'never') return 'szv2-badge-warning'
    return 'szv2-badge-neutral'
  }

  const pendingCount = pixItems.filter(p => p.status === 'pendente').length
  const expiredCount = pixItems.filter(p => p.status === 'expirado').length

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'} style={{ marginBottom: 16 }}>
          {toast.msg}
        </div>
      )}

      {/* KPIs de reconciliação */}
      <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 20 }}>
        <KpiCard label="PIX pendentes" value={pendingCount} danger sub="precisam ser verificados" />
        <KpiCard label="PIX expirados" value={expiredCount} danger sub="aguardando cancelamento" />
        <KpiCard label="Total (filtro atual)" value={pixTotal} sub={`status: ${pixStatus || 'todos'}`} />
      </div>

      {/* Status dos crons */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head">
          <div><h2>Status dos crons PIX</h2><p className="szv2-card-sub">Crons de reconciliação e verificação automática.</p></div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button type="button" className="szv2-btn szv2-btn-secondary"
              onClick={loadAll} disabled={loading || triggerBusy}>Atualizar</button>
            <button type="button" className="szv2-btn szv2-btn-brand"
              onClick={triggerReconcile} disabled={loading || triggerBusy}>
              {triggerBusy ? 'Disparando…' : 'Reconciliar agora'}
            </button>
          </div>
        </div>
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr><th>Cron</th><th>Frequência</th><th>Última execução</th><th>Status</th><th>Próxima</th></tr></thead>
            <tbody>
              {[reconcileCron, verifyPix, divergCheck].filter(Boolean).map(c => c && (
                <tr key={c.name}>
                  <td style={{ fontFamily: 'monospace', fontSize: 12 }}>{c.name}</td>
                  <td><span className="sz-badge szv2-badge-brand">{c.frequency}</span></td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{c.last_run ? c.last_run.slice(0, 16).replace('T', ' ') : '—'}</td>
                  <td><span className={`sz-badge ${cronBadgeCls(c.last_status)}`}>{c.last_status}</span></td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{c.next_run ? c.next_run.slice(0, 16).replace('T', ' ') : '—'}</td>
                </tr>
              ))}
              {crons.length === 0 && (
                <tr><td colSpan={5}><div className="szv2-empty"><h3>{loading ? 'Carregando…' : 'Sem dados de cron disponíveis'}</h3></div></td></tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Lista de recargas PIX */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div><h2>Recargas PIX</h2><p className="szv2-card-sub">{pixTotal} registro(s)</p></div>
          <select className="szv2-select" style={{ width: 180 }} value={pixStatus} onChange={e => setPixStatus(e.target.value)}>
            <option value="">Todos status</option>
            <option value="pendente">Pendente</option>
            <option value="confirmado">Confirmado</option>
            <option value="expirado">Expirado</option>
            <option value="cancelado">Cancelado</option>
          </select>
        </div>
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>ID</th><th>Usuário</th><th className="szv2-td-num">Valor</th>
                <th>Status</th><th>PIX ID</th><th>Expira</th><th>Pago em</th><th>Criado</th><th>Ações</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr><td colSpan={9}><div className="szv2-empty"><h3>Carregando…</h3></div></td></tr>
              ) : pixItems.length === 0 ? (
                <tr><td colSpan={9}><div className="szv2-empty"><h3>Nenhuma recarga encontrada</h3></div></td></tr>
              ) : pixItems.map(p => (
                <tr key={p.id}>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{p.id}</td>
                  <td style={{ fontSize: 13 }}>#{p.user_id}</td>
                  <td className="szv2-td-num" style={{ fontWeight: 700, color: 'var(--szv2-success)' }}>{fmt(p.valor)}</td>
                  <td><span className={`szv2-status-badge ${PIX_STATUS_CLS[p.status] || 's-pendente'}`}>{p.status}</span></td>
                  <td style={{ maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis', fontSize: 11, color: 'var(--szv2-text-muted)' }}>{p.me_pix_id ?? '—'}</td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(p.expires_at)}</td>
                  <td style={{ fontSize: 12, color: p.paid_at ? 'var(--szv2-success)' : 'var(--szv2-text-muted)' }}>{fmtDate(p.paid_at)}</td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(p.created_at)}</td>
                  <td>
                    {p.status === 'pendente' && (
                      <div style={{ display: 'flex', gap: 6 }}>
                        <button disabled={acting === p.id} onClick={() => setPixStatusFn(p.id, 'confirmado')}
                          className="szv2-btn szv2-btn-sm"
                          style={{ height: 28, fontSize: 11, background: 'var(--szv2-success-bg)', color: 'var(--szv2-success)', borderColor: 'var(--szv2-success)' }}>
                          Confirmar
                        </button>
                        <button disabled={acting === p.id} onClick={() => setPixStatusFn(p.id, 'cancelado')}
                          className="szv2-btn szv2-btn-sm szv2-btn-danger" style={{ height: 28, fontSize: 11 }}>
                          Cancelar
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

// ── Página principal ──────────────────────────────────────────────────────────

export default function Wallet() {
  const [tab, setTab] = useState<Tab>('clientes')
  const [config, setConfig] = useState<{ saldo_me: number; pix_pendentes: number; ultima_reconciliacao: string | null } | null>(null)

  // KPIs do header — carregados uma vez ao montar a página
  useEffect(() => {
    async function loadKpis() {
      try {
        const [cfg, pix, crons] = await Promise.all([
          api<ConfigResp>('/tpc-config').catch(() => null),
          api<{ total: number }>('/pix?status=pendente&limit=1').catch(() => null),
          api<{ items: CronItem[] }>('/crons').catch(() => null),
        ])
        const reconciliaCron = crons?.items?.find(c => c.name === 'sz_pix_auto_reconcile_cron')
        setConfig({
          saldo_me: cfg?.me?.saldo_atual_me ?? 0,
          pix_pendentes: pix?.total ?? 0,
          ultima_reconciliacao: reconciliaCron?.last_run ?? null,
        })
      } catch { /* ignora — KPIs são informativos */ }
    }
    loadKpis()
  }, [])

  const TABS: { key: Tab; label: string }[] = [
    { key: 'clientes',           label: 'Clientes' },
    { key: 'transacoes',         label: 'Transações' },
    { key: 'configuracoes',      label: 'Configurações' },
    { key: 'restapi',            label: 'REST API' },
    { key: 'pix-reconciliacao',  label: 'PIX / Reconciliação' },
  ]

  return (
    <div>
      <div className="szv2-section-head">
        <div><h1>Carteira</h1><p>Saldos, movimentações financeiras e PIX</p></div>
      </div>

      {/* KPI band — saldo ME, PIX pendentes, última reconciliação */}
      {config && (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 20 }}>
          <KpiCard
            label="Saldo atual ME"
            value={fmtBRL(config.saldo_me)}
            sub="saldo na conta Melhor Envio"
            brand
          />
          <KpiCard
            label="PIX pendentes"
            value={config.pix_pendentes}
            sub="aguardando confirmação"
            danger
          />
          <KpiCard
            label="Última reconciliação"
            value={config.ultima_reconciliacao ? config.ultima_reconciliacao.slice(0, 16).replace('T', ' ') : '—'}
            sub="sz_pix_auto_reconcile_cron"
          />
        </div>
      )}

      {/* Tabs de navegação */}
      <div className="szv2-tabs" style={{ marginBottom: 20 }}>
        {TABS.map(t => (
          <button key={t.key} className="szv2-tab" aria-selected={tab === t.key}
            onClick={() => setTab(t.key)}>
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'clientes'          && <TabClientes />}
      {tab === 'transacoes'        && <TabTransacoes />}
      {tab === 'configuracoes'     && <TabConfiguracoes />}
      {tab === 'restapi'           && <TabRestApi />}
      {tab === 'pix-reconciliacao' && <TabPixReconciliacao />}
    </div>
  )
}
