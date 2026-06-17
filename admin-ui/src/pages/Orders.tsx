import { useEffect, useState } from 'react'
import { api } from '../api'

// URL base do WP Admin para link "Abrir WC".
// Configurável via variável de ambiente; padrão assume mesma origem com /wp-admin.
const WP_ADMIN = (import.meta.env.VITE_WP_ADMIN_BASE as string) || '/wp-admin'

type P = {
  id: number
  wc_order_id?: number | null
  sz_order_id?: number | null
  motoboy_id?: number | null
  status: string
  valor: number
  dest_nome: string
  dest_cep: string
  dest_cidade: string
  cliente_nome: string
  produto: string
  afiliado_nome: string
  comissao: number
  created_at: string
}

const STATUS_CLS: Record<string, string> = {
  pendente:  's-pendente',
  agendado:  'szv2-badge-info',
  embalado:  'szv2-badge-info',
  em_rota:   'szv2-badge-brand',
  entregue:  'szv2-badge-success',
  frustrado: 'szv2-badge-danger',
  cancelado: 'szv2-badge-neutral',
}

const fmt = (v: number) => 'R$ ' + v.toLocaleString('pt-BR', { minimumFractionDigits: 2 })

function wcAdminUrl(wcOrderId: number): string {
  return `${WP_ADMIN}/admin.php?page=wc-orders&action=edit&id=${wcOrderId}`
}

export default function Orders() {
  const [items, setItems] = useState<P[]>([])
  const [status, setStatus] = useState('')
  const [date, setDate] = useState('')
  const [cidade, setCidade] = useState('')
  const [search, setSearch] = useState('')
  const [stopped, setStopped] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [busyId, setBusyId] = useState<number | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  function buildQS() {
    const p = new URLSearchParams()
    if (status) p.set('status', status)
    if (date) p.set('date', date)
    if (cidade) p.set('cidade', cidade)
    if (search) p.set('s', search)
    if (stopped) p.set('stopped', '1')
    p.set('limit', '100')
    return p.toString()
  }

  function load() {
    setErr('')
    api<{ items: P[] }>(`/orders/motoboy?${buildQS()}`)
      .then(r => setItems(r.items || []))
      .catch(e => setErr(e.message))
  }

  useEffect(() => { load() }, [status, date, cidade, search, stopped])

  async function handleAuditFix(p: P) {
    if (!window.confirm(`Auditar e corrigir pedido motoboy #${p.id} (WC #${p.wc_order_id ?? '—'})?\n\nCorrege comissão de afiliado e carteira COD do produtor caso haja divergência.`)) return
    setBusyId(p.id)
    try {
      await api(`/orders/motoboy/${p.id}/audit-fix`, { method: 'POST' })
      showToast('ok', `Pedido #${p.id} auditado com sucesso.`)
      load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao auditar')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div>
      {/* Filtros */}
      <div className="szv2-section-head" style={{ flexWrap: 'wrap', gap: 8 }}>
        <div>
          <h1>Pedidos Motoboy</h1>
          <p>{items.length} pedido(s) encontrado(s){stopped ? ' — parados 24h+' : ''}</p>
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          {/* Data */}
          <input
            type="date"
            className="szv2-input"
            style={{ width: 150 }}
            value={date}
            onChange={e => setDate(e.target.value)}
            title="Filtrar por data de criação"
          />
          {/* Status */}
          <select
            className="szv2-select"
            style={{ width: 160 }}
            value={status}
            onChange={e => setStatus(e.target.value)}
          >
            <option value="">Todos status</option>
            {Object.keys(STATUS_CLS).map(s => <option key={s} value={s}>{s}</option>)}
          </select>
          {/* Cidade */}
          <input
            type="text"
            className="szv2-input"
            style={{ width: 140 }}
            placeholder="Cidade"
            value={cidade}
            onChange={e => setCidade(e.target.value)}
          />
          {/* Busca por pedido / nome */}
          <input
            type="search"
            className="szv2-input"
            style={{ width: 180 }}
            placeholder="Pedido / nome"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
          {/* Parados 24h+ */}
          <label style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 13, cursor: 'pointer' }}>
            <input
              type="checkbox"
              checked={stopped}
              onChange={e => setStopped(e.target.checked)}
            />
            <span>Parados 24h+</span>
          </label>
        </div>
      </div>

      {/* Alertas */}
      {err && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{err}</div>}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 12 }}
        >
          {toast.msg}
        </div>
      )}

      {/* Tabela */}
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>Pedido</th>
              <th>Cliente</th>
              <th>Cidade</th>
              <th>Status</th>
              <th>Produto</th>
              <th>Afiliado</th>
              <th className="szv2-td-num">Comissão</th>
              <th>Criado</th>
              <th>Ações</th>
            </tr>
          </thead>
          <tbody>
            {items.map(p => (
              <tr key={p.id}>
                {/* Pedido — mostra ID motoboy e WC Order */}
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  <div>#{p.id}</div>
                  {p.wc_order_id && (
                    <div style={{ color: 'var(--szv2-text-faint)', fontSize: 11 }}>
                      WC #{p.wc_order_id}
                    </div>
                  )}
                </td>
                {/* Cliente */}
                <td style={{ fontWeight: 500 }}>
                  {p.cliente_nome || p.dest_nome || '—'}
                </td>
                {/* Cidade */}
                <td style={{ fontSize: 13 }}>
                  {p.dest_cidade || '—'}
                </td>
                {/* Status */}
                <td>
                  <span className={`sz-badge ${STATUS_CLS[p.status] || 'szv2-badge-neutral'}`}>
                    {p.status}
                  </span>
                </td>
                {/* Produto */}
                <td style={{ fontSize: 13 }}>
                  {p.produto || '—'}
                </td>
                {/* Afiliado */}
                <td style={{ fontSize: 13 }}>
                  {p.afiliado_nome || '—'}
                </td>
                {/* Comissão */}
                <td className="szv2-td-num" style={{ fontWeight: 600 }}>
                  {p.comissao > 0 ? fmt(p.comissao) : '—'}
                </td>
                {/* Criado */}
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  {p.created_at ? p.created_at.slice(0, 16).replace('T', ' ') : '—'}
                </td>
                {/* Ações */}
                <td>
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'nowrap' }}>
                    {p.wc_order_id ? (
                      <a
                        href={wcAdminUrl(p.wc_order_id)}
                        target="_blank"
                        rel="noreferrer"
                        className="szv2-btn-secondary"
                        style={{ fontSize: 12, padding: '3px 8px', textDecoration: 'none' }}
                      >
                        Abrir WC
                      </a>
                    ) : (
                      <span style={{ color: 'var(--szv2-text-faint)', fontSize: 12 }}>sem WC</span>
                    )}
                    <button
                      type="button"
                      className="szv2-btn-secondary"
                      style={{ fontSize: 12, padding: '3px 8px' }}
                      disabled={busyId === p.id}
                      onClick={() => handleAuditFix(p)}
                    >
                      {busyId === p.id ? '…' : 'Auditar'}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={9}>
                  <div className="szv2-empty">
                    <h3>Sem pedidos</h3>
                    <p>Tente outro filtro ou remova os filtros aplicados.</p>
                  </div>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
