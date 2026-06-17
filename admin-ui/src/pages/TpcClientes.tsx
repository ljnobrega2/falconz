import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'

// ----- Tipos retornados pelo handler Go ---------------------------------------

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

type Transacao = {
  id: number
  tipo: string
  valor: number
  saldo_apos: number
  descricao: string
  status: string
  created_at: string
}

type Recarga = {
  id: number
  valor: number
  status: string
  me_pix_id: string | null
  qr_src: string | null
  copia_cola: string | null
  expires_at: string | null
  created_at: string
}

type DetailResponse = {
  cliente: ClienteRow
  transacoes: Transacao[]
  recargas: Recarga[]
}

type CreateRecargaResponse = {
  ok: boolean
  recarga_id: number
  user_id: number
  valor: number
  qr_src: string
  copia_cola: string
  link: string
  expires_at: string
  security_token: string
  stub?: boolean
}

type ResetResponse = {
  ok: boolean
  result: {
    carteira_deleted: number
    transacoes_deleted: number
    recargas_deleted: number
    order_meta_deleted: number
  }
}

// ----- Helpers ---------------------------------------------------------------

const fmt = (v: number) =>
  'R$ ' + v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtDate = (s: string | null | undefined) =>
  s ? s.slice(0, 16).replace('T', ' ') : '—'

const STATUS_CLS: Record<string, string> = {
  pendente: 's-pendente',
  confirmado: 's-confirmado',
  pago: 's-confirmado',
  cancelado: 's-cancelado',
  expirado: 's-expirado',
}

const BADGE_CLS: Record<string, string> = {
  pendente: 'szv2-badge-warning',
  confirmado: 'szv2-badge-success',
  pago: 'szv2-badge-success',
  cancelado: 'szv2-badge-danger',
  expirado: 'szv2-badge-neutral',
}

// ----- Página principal -------------------------------------------------------

export default function TpcClientes() {
  const [items, setItems] = useState<ClienteRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [perPage] = useState(100)
  const [q, setQ] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  // Estado de modal
  const [pixModal, setPixModal] = useState<ClienteRow | null>(null)
  const [extratoModal, setExtratoModal] = useState<ClienteRow | null>(null)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = new URLSearchParams({
        q,
        limit: String(perPage),
        page: String(page),
      }).toString()
      const r = await api<{
        items: ClienteRow[]
        total: number
        page: number
        per_page: number
      }>(`/tpc-clientes?${qs}`)
      setItems(r.items || [])
      setTotal(r.total || 0)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [q, page])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  function applySearch(e: React.FormEvent) {
    e.preventDefault()
    setPage(1)
    setQ(searchInput.trim())
  }

  const totalPages = Math.max(1, Math.ceil(total / perPage))

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Carteira Expedição</h1>
          <p>{total} cliente(s) com carteira</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button
            type="button"
            className="szv2-btn szv2-btn-brand"
            onClick={() => setPixModal({} as ClienteRow)}
          >
            + Recarga PIX por cliente
          </button>
        </div>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {/* Search bar */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <form onSubmit={applySearch} style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
          <input
            className="szv2-input"
            placeholder="Buscar por email, nome ou user_id…"
            value={searchInput}
            onChange={e => setSearchInput(e.target.value)}
            style={{ flex: 1 }}
          />
          <button type="submit" className="szv2-btn szv2-btn-secondary">Buscar</button>
          {q && (
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary"
              onClick={() => { setSearchInput(''); setQ(''); setPage(1) }}
            >
              Limpar
            </button>
          )}
        </form>
      </div>

      {/* Tabela principal */}
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Nome</th>
              <th>Email</th>
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
              <tr>
                <td colSpan={9}>
                  <div className="szv2-empty"><h3>Carregando…</h3></div>
                </td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={9}>
                  <div className="szv2-empty"><h3>Nenhum cliente com carteira</h3></div>
                </td>
              </tr>
            ) : items.map(c => (
              <tr key={c.user_id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{c.user_id}</td>
                <td style={{ fontSize: 13 }}>{c.nome || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}</td>
                <td style={{ fontSize: 13 }}>{c.email || <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}</td>
                <td className="szv2-td-num" style={{ fontWeight: 700 }}>{fmt(c.saldo)}</td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-warning)' }}>{fmt(c.saldo_reservado)}</td>
                <td className="szv2-td-num" style={{ fontWeight: 700, color: 'var(--szv2-success)' }}>
                  {fmt(c.saldo_disponivel)}
                </td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-muted)' }}>{c.transacoes_count}</td>
                <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(c.ultima_atualizacao)}</td>
                <td>
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                      onClick={() => setExtratoModal(c)}
                    >
                      Ver extrato
                    </button>
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-sm szv2-btn-brand"
                      onClick={() => setPixModal(c)}
                    >
                      Emitir PIX
                    </button>
                    {/* CRIT-01: crédito manual desativado — só via PIX confirmado pelo Melhor Envio */}
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                      disabled
                      title="Ajuste manual desativado. Use recarga PIX confirmada pelo Melhor Envio. (CRIT-01)"
                      style={{ opacity: 0.5, cursor: 'not-allowed' }}
                    >
                      Ajuste manual bloqueado
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Paginação simples */}
      {total > perPage && (
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

      {/* Reset wallet — collapsible danger */}
      <ResetWalletPanel onResult={showToast} onReload={load} />

      {/* Modais */}
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

// ----- Modal: Emitir PIX ------------------------------------------------------

function PixModal({
  cliente,
  onClose,
  onSuccess,
}: {
  cliente: ClienteRow | null
  onClose: () => void
  onSuccess: () => void
}) {
  // Quando aberto via botão "+" (sem cliente), permite busca por e-mail
  const [emailInput, setEmailInput] = useState<string>('')
  const [emailSearching, setEmailSearching] = useState(false)
  const [resolvedCliente, setResolvedCliente] = useState<ClienteRow | null>(cliente)
  const [userIdInput, setUserIdInput] = useState<string>(cliente?.user_id?.toString() || '')
  const [valor, setValor] = useState<string>('100')
  const [motivo, setMotivo] = useState<string>('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [result, setResult] = useState<CreateRecargaResponse | null>(null)
  const [now, setNow] = useState(Date.now())

  // Modo standalone: sem cliente pré-selecionado
  const isStandalone = !cliente

  // Timer countdown — só ativa quando há resultado
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
    const m = Math.floor(secs / 60)
    const s = secs % 60
    return `${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
  }

  // Busca cliente pelo e-mail via GET /tpc-clientes?q=email (espelha WP get_user_by('email'))
  async function resolveByEmail() {
    const email = emailInput.trim()
    if (!email) return
    setEmailSearching(true)
    setErr('')
    try {
      const r = await api<{ items: ClienteRow[]; total: number }>(
        `/tpc-clientes?q=${encodeURIComponent(email)}&limit=10&page=1`
      )
      const exatos = (r.items || []).filter(
        c => (c.email || '').toLowerCase() === email.toLowerCase()
      )
      if (exatos.length === 1) {
        setResolvedCliente(exatos[0])
        setUserIdInput(String(exatos[0].user_id))
        setErr('')
      } else if (r.items && r.items.length > 0) {
        // Múltiplas correspondências parciais — preenche user_id do primeiro e avisa
        setResolvedCliente(r.items[0])
        setUserIdInput(String(r.items[0].user_id))
        setErr(`E-mail não encontrado exato — usando primeiro resultado (user_id #${r.items[0].user_id}). Verifique.`)
      } else {
        setErr(`Nenhum cliente encontrado com e-mail "${email}".`)
        setResolvedCliente(null)
        setUserIdInput('')
      }
    } catch {
      setErr('Erro ao buscar cliente por e-mail.')
    } finally {
      setEmailSearching(false)
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    const uid = parseInt(userIdInput, 10)
    const v = parseFloat(valor.replace(',', '.'))
    if (!uid || uid <= 0) { setErr('user_id inválido'); return }
    if (!v || v < 10) { setErr('Valor mínimo: R$ 10,00.'); return }

    setBusy(true)
    try {
      const r = await api<CreateRecargaResponse>(`/tpc-clientes/${uid}/recarga`, {
        method: 'POST',
        body: JSON.stringify({ valor: v, motivo }),
      })
      setResult(r)
      onSuccess()
    } catch (e: any) {
      setErr(e.message || 'Falha ao gerar PIX')
    } finally {
      setBusy(false)
    }
  }

  async function copyCopiaCola() {
    if (!result?.copia_cola) return
    try {
      await navigator.clipboard.writeText(result.copia_cola)
    } catch {
      // ignora — alguns browsers exigem permissão
    }
  }

  // Cliente exibido no cabeçalho: pré-selecionado ou resolvido por e-mail
  const displayCliente = resolvedCliente || (cliente?.user_id ? cliente : null)

  return (
    <div className="szv2-modal-overlay szv2-open" onClick={onClose}>
      <div className="szv2-modal szv2-modal-lg" onClick={e => e.stopPropagation()}>
        <div className="szv2-modal-head">
          <h3>Emitir PIX{displayCliente ? ` para ${displayCliente.nome || `#${displayCliente.user_id}`}` : ''}</h3>
          <button className="szv2-modal-x" onClick={onClose}>✕</button>
        </div>
        <div className="szv2-modal-body">
          {!result ? (
            <form onSubmit={submit}>
              {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}

              {/* Modo standalone: busca por e-mail (espelha WP get_user_by('email')) */}
              {isStandalone && (
                <div className="szv2-field" style={{ marginBottom: 12 }}>
                  <label className="szv2-label">Buscar cliente por e-mail</label>
                  <div style={{ display: 'flex', gap: 8 }}>
                    <input
                      className="szv2-input"
                      type="email"
                      value={emailInput}
                      onChange={e => setEmailInput(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && (e.preventDefault(), resolveByEmail())}
                      placeholder="cliente@email.com"
                      style={{ flex: 1 }}
                    />
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-secondary"
                      onClick={resolveByEmail}
                      disabled={emailSearching || !emailInput.trim()}
                    >
                      {emailSearching ? '…' : 'Buscar'}
                    </button>
                  </div>
                  {resolvedCliente && (
                    <small style={{ color: 'var(--szv2-success)', fontSize: 12 }}>
                      Cliente encontrado: {resolvedCliente.nome || resolvedCliente.email} (user_id #{resolvedCliente.user_id})
                    </small>
                  )}
                </div>
              )}

              <div className="szv2-field" style={{ marginBottom: 12 }}>
                <label className="szv2-label">Cliente (user_id) *</label>
                <input
                  className="szv2-input"
                  type="number"
                  required
                  value={userIdInput}
                  disabled={!!resolvedCliente && !isStandalone}
                  onChange={e => {
                    setUserIdInput(e.target.value)
                    // Se digitou manualmente, limpa o cliente resolvido por e-mail
                    if (isStandalone) setResolvedCliente(null)
                  }}
                  placeholder="ID do usuário WordPress"
                />
                {displayCliente && (
                  <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                    {displayCliente.email || displayCliente.nome || `Usuário #${displayCliente.user_id}`}
                  </small>
                )}
              </div>

              <div className="szv2-field" style={{ marginBottom: 12 }}>
                <label className="szv2-label">Valor (R$) * <small style={{ color: 'var(--szv2-text-muted)' }}>(mínimo R$ 10,00)</small></label>
                <input
                  className="szv2-input"
                  type="number"
                  required
                  min="10"
                  step="0.01"
                  value={valor}
                  onChange={e => setValor(e.target.value)}
                />
              </div>

              <div className="szv2-field" style={{ marginBottom: 12 }}>
                <label className="szv2-label">Motivo</label>
                <textarea
                  className="szv2-input"
                  rows={3}
                  value={motivo}
                  onChange={e => setMotivo(e.target.value)}
                  placeholder="Ex.: recarga via PIX pelo admin"
                />
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
                    ⚠ Placeholder (modo dev) — aguardando integração com wallet-service.
                  </p>
                )}
              </div>

              <div style={{ display: 'flex', justifyContent: 'center', marginBottom: 12 }}>
                <img
                  src={result.qr_src}
                  alt="QR Code PIX"
                  style={{
                    width: 240,
                    height: 240,
                    border: '1px solid var(--szv2-divider)',
                    borderRadius: 8,
                    background: '#fff',
                  }}
                />
              </div>

              <div style={{ textAlign: 'center', marginBottom: 12 }}>
                <span
                  className="szv2-status-badge s-pendente"
                  style={{ fontSize: 13, padding: '4px 12px' }}
                >
                  Expira em {fmtCountdown(remaining)}
                </span>
              </div>

              <div className="szv2-field">
                <label className="szv2-label">Copia e cola</label>
                <textarea
                  className="szv2-input"
                  rows={3}
                  readOnly
                  value={result.copia_cola}
                  onFocus={e => e.currentTarget.select()}
                  style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 11 }}
                />
                <button
                  type="button"
                  className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                  onClick={copyCopiaCola}
                  style={{ marginTop: 8 }}
                >
                  Copiar
                </button>
              </div>
            </div>
          )}
        </div>
        <div className="szv2-modal-foot">
          {!result ? (
            <>
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={onClose}>
                Cancelar
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                onClick={submit as any}
                disabled={busy}
              >
                {busy ? 'Gerando…' : 'Gerar PIX'}
              </button>
            </>
          ) : (
            <button type="button" className="szv2-btn szv2-btn-brand" onClick={onClose}>
              Fechar
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// ----- Modal: Ver extrato -----------------------------------------------------

function ExtratoModal({
  cliente,
  onClose,
  onCancelled,
}: {
  cliente: ClienteRow
  onClose: () => void
  onCancelled: () => void
}) {
  const [tab, setTab] = useState<'transacoes' | 'recargas'>('transacoes')
  const [detail, setDetail] = useState<DetailResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [cancelling, setCancelling] = useState<number | null>(null)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<DetailResponse>(`/tpc-clientes/${cliente.user_id}`)
      setDetail(r)
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar extrato')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [cliente.user_id])

  async function cancelarRecarga(recargaID: number) {
    if (!window.confirm(`Cancelar recarga #${recargaID}?`)) return
    setCancelling(recargaID)
    try {
      await api(`/tpc-clientes/${cliente.user_id}/cancelar-recarga/${recargaID}`, {
        method: 'POST',
      })
      onCancelled()
      await load()
    } catch (e: any) {
      setErr(e.message || 'Falha ao cancelar')
    } finally {
      setCancelling(null)
    }
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
          {/* Resumo de saldo */}
          {detail && (
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(3, 1fr)',
                gap: 12,
                marginBottom: 16,
              }}
            >
              <KpiMini label="Saldo total" value={fmt(detail.cliente.saldo)} />
              <KpiMini label="Reservado" value={fmt(detail.cliente.saldo_reservado)} warning />
              <KpiMini label="Disponível" value={fmt(detail.cliente.saldo_disponivel)} success />
            </div>
          )}

          {/* Tabs */}
          <div className="szv2-tabs" style={{ marginBottom: 12 }}>
            <button
              className="szv2-tab"
              aria-selected={tab === 'transacoes'}
              onClick={() => setTab('transacoes')}
            >
              Transações
            </button>
            <button
              className="szv2-tab"
              aria-selected={tab === 'recargas'}
              onClick={() => setTab('recargas')}
            >
              Recargas
            </button>
          </div>

          {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}

          {loading ? (
            <div className="szv2-empty"><h3>Carregando…</h3></div>
          ) : tab === 'transacoes' ? (
            <div className="szv2-table-wrap">
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Data</th>
                    <th>Tipo</th>
                    <th className="szv2-td-num">Valor</th>
                    <th>Status</th>
                    <th>Descrição</th>
                  </tr>
                </thead>
                <tbody>
                  {(detail?.transacoes || []).map(t => (
                    <tr key={t.id}>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(t.created_at)}</td>
                      <td>
                        <span
                          className={`szv2-status-badge ${
                            t.tipo === 'credito' || t.tipo === 'recarga' ? 's-confirmado'
                            : t.tipo === 'debito' ? 's-cancelado'
                            : 's-pendente'
                          }`}
                        >
                          {t.tipo}
                        </span>
                      </td>
                      <td
                        className="szv2-td-num"
                        style={{
                          fontWeight: 700,
                          color:
                            t.tipo === 'credito' || t.tipo === 'recarga' ? 'var(--szv2-success)'
                            : t.tipo === 'debito' ? 'var(--szv2-danger)'
                            : 'inherit',
                        }}
                      >
                        {fmt(t.valor)}
                      </td>
                      <td>
                        <span className={`sz-badge ${BADGE_CLS[t.status] || 'szv2-badge-neutral'}`}>{t.status}</span>
                      </td>
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
                <thead>
                  <tr>
                    <th>Data</th>
                    <th className="szv2-td-num">Valor</th>
                    <th>Status</th>
                    <th>PIX ID</th>
                    <th style={{ width: 140 }}>Ação</th>
                  </tr>
                </thead>
                <tbody>
                  {(detail?.recargas || []).map(r => (
                    <tr key={r.id}>
                      <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{fmtDate(r.created_at)}</td>
                      <td className="szv2-td-num" style={{ fontWeight: 700 }}>{fmt(r.valor)}</td>
                      <td>
                        <span className={`sz-badge ${BADGE_CLS[r.status] || 'szv2-badge-neutral'}`}>{r.status}</span>
                      </td>
                      <td
                        style={{
                          fontFamily: 'var(--szv2-font-mono)',
                          fontSize: 11,
                          color: 'var(--szv2-text-muted)',
                          maxWidth: 160,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                      >
                        {r.me_pix_id ?? '—'}
                      </td>
                      <td>
                        {r.status === 'pendente' && (
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-sm szv2-btn-danger"
                            disabled={cancelling === r.id}
                            onClick={() => cancelarRecarga(r.id)}
                          >
                            {cancelling === r.id ? '…' : 'Cancelar'}
                          </button>
                        )}
                      </td>
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
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={onClose}>
            Fechar
          </button>
        </div>
      </div>
    </div>
  )
}

// ----- Reset wallet collapsible ----------------------------------------------

function ResetWalletPanel({
  onResult,
  onReload,
}: {
  onResult: (kind: 'ok' | 'err', msg: string) => void
  onReload: () => void
}) {
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const ready = confirm === 'RESETAR'

  async function reset() {
    if (!ready) return
    if (!window.confirm('Tem ABSOLUTA certeza? Essa ação apaga TODAS as carteiras, transações e recargas.')) return
    setBusy(true)
    try {
      const r = await api<ResetResponse>('/tpc-clientes/reset-wallet-all', {
        method: 'POST',
        body: JSON.stringify({ confirm: 'RESETAR' }),
      })
      const x = r.result
      onResult(
        'ok',
        `Reset OK · ${x.carteira_deleted} carteiras, ${x.transacoes_deleted} transações, ${x.recargas_deleted} recargas, ${x.order_meta_deleted} metas de pedidos removidas.`,
      )
      setConfirm('')
      onReload()
    } catch (e: any) {
      onResult('err', e.message || 'Falha ao resetar')
    } finally {
      setBusy(false)
    }
  }

  return (
    <details
      className="szv2-card"
      style={{
        marginTop: 32,
        borderColor: 'var(--szv2-danger)',
        background: 'rgba(220, 38, 38, 0.04)',
      }}
    >
      <summary
        style={{
          cursor: 'pointer',
          fontWeight: 700,
          color: 'var(--szv2-danger)',
          padding: '4px 0',
          listStyle: 'none',
        }}
      >
        ⚠ Zona de perigo — Reset total das carteiras
      </summary>

      <div style={{ marginTop: 16 }}>
        <p style={{ fontSize: 14, lineHeight: 1.5, marginTop: 0 }}>
          <strong>Esta ação é IRREVERSÍVEL.</strong> Todos os registros em{' '}
          <code>tpc_carteira</code>, <code>tpc_transacoes</code> e{' '}
          <code>tpc_recargas</code> serão deletados. Saldos, históricos e recargas
          pendentes serão perdidos para sempre.
        </p>
        <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Para confirmar, digite exatamente <strong>RESETAR</strong> abaixo (case-sensitive):
        </p>

        <div style={{ display: 'flex', gap: 12, alignItems: 'flex-end', marginTop: 12 }}>
          <div className="szv2-field" style={{ flex: 1 }}>
            <label className="szv2-label">Confirmação</label>
            <input
              className="szv2-input"
              value={confirm}
              onChange={e => setConfirm(e.target.value)}
              placeholder="Digite RESETAR"
              autoComplete="off"
            />
          </div>
          <button
            type="button"
            className="szv2-btn szv2-btn-danger"
            disabled={!ready || busy}
            onClick={reset}
            style={{ marginBottom: 0 }}
          >
            {busy ? 'Resetando…' : 'RESET TOTAL'}
          </button>
        </div>
      </div>
    </details>
  )
}

// ----- KpiMini --------------------------------------------------------------

function KpiMini({
  label,
  value,
  warning,
  success,
}: {
  label: string
  value: string
  warning?: boolean
  success?: boolean
}) {
  const color = success
    ? 'var(--szv2-success)'
    : warning
    ? 'var(--szv2-warning)'
    : 'var(--szv2-text)'
  return (
    <div className="szv2-card" style={{ padding: 12, margin: 0 }}>
      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 }}>
        {label}
      </div>
      <div style={{ fontSize: 20, fontWeight: 700, color, marginTop: 4 }}>{value}</div>
    </div>
  )
}
