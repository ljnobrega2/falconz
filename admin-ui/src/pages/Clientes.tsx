import { useEffect, useState } from 'react'
import { api } from '../api'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import { confirmAsync } from '../components/ConfirmDialog'
import { emitToast } from '../hooks/useToast'
import { brDate } from '../utils/format'

// Clientes = senderzz_portal_users role='cliente'. Quando o admin cadastra um
// produto para um cliente e esse produto é APROVADO, o cliente vira produtor
// (promoção em product_approval.go). produtos_count = produtos já vinculados ao
// cliente (sz_products.produtor_id = id). Shape do GET /clientes.
type Cliente = {
  id: number
  nome: string
  email: string
  telefone: string
  cpf: string
  role: string
  created_at: string
  produtos_count: number
}

export default function Clientes() {
  const [items, setItems] = useState<Cliente[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [err, setErr] = useState('')
  const [q, setQ] = useState('')
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [promotingId, setPromotingId] = useState<number | null>(null)

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<{ items: Cliente[]; total: number }>('/clientes?limit=200')
      setItems(r.items ?? [])
      setTotal(r.total ?? (r.items?.length ?? 0))
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  // Excluir cliente = SOFT-DELETE no backend (ativo=false): some da listagem, mas
  // o histórico financeiro/pedidos fica intacto e é reversível. DELETE /clientes/{id}.
  async function handleDelete(c: Cliente) {
    const ok = await confirmAsync({
      variant: 'danger',
      title: 'Excluir cliente',
      message: `Excluir o cliente ${c.nome || c.email || `#${c.id}`}? Sai da listagem; histórico preservado.`,
      confirmLabel: 'Excluir',
    })
    if (!ok) return
    setDeletingId(c.id)
    try {
      await api(`/clientes/${c.id}`, { method: 'DELETE' })
      emitToast('ok', 'Cliente excluído.')
      await load()
    } catch (e: any) {
      emitToast('err', e.message || 'Erro ao excluir cliente')
    } finally {
      setDeletingId(null)
    }
  }

  async function handlePromote(c: Cliente) {
    const ok = await confirmAsync({
      title: 'Promover a produtor',
      message: `Promover ${c.nome || c.email || `#${c.id}`} a produtor?`,
      confirmLabel: 'Promover',
    })
    if (!ok) return
    setPromotingId(c.id)
    try {
      await api(`/users/${c.id}`, { method: 'PUT', body: JSON.stringify({ role: 'produtor' }) })
      emitToast('ok', `${c.nome || c.email} promovido a produtor.`)
      await load()
    } catch (e: any) {
      emitToast('err', e.message || 'Erro ao promover a produtor')
    } finally {
      setPromotingId(null)
    }
  }

  const qNorm = q.trim().toLowerCase()
  const filtered = items.filter((c) => {
    if (!qNorm) return true
    const hay = `${c.nome} ${c.email}`.toLowerCase()
    return hay.includes(qNorm)
  })

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Clientes</h1>
          <p>{filtered.length} de {total} cliente(s)</p>
        </div>
      </div>

      <div className="szv2-kpi-grid" style={{ marginBottom: 24 }}>
        <div className="szv2-card">
          <div className="szv2-kpi">
            <span className="szv2-kpi-label">Clientes cadastrados</span>
            <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{total}</span>
          </div>
        </div>
      </div>

      <div className="szv2-card" style={{ marginBottom: 24 }}>
        <input
          className="szv2-input"
          type="search"
          aria-label="Buscar clientes por nome ou e-mail"
          placeholder="Buscar por nome ou e-mail"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {loading && filtered.length === 0 ? (
        <TableSkeleton rows={6} cols={3} />
      ) : !loading && filtered.length === 0 ? (
        <EmptyState
          icon="🧑"
          title={
            items.length === 0
              ? 'Nenhum cliente cadastrado ainda.'
              : 'Nenhum cliente encontrado com essa busca.'
          }
          description={
            items.length === 0
              ? 'Os clientes aparecerão aqui assim que se cadastrarem. Ao ter um produto aprovado, o cliente vira produtor.'
              : 'Ajuste o termo de busca ou limpe o filtro.'
          }
        />
      ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>Nome / Email</th>
                <th>Telefone</th>
                <th>CPF</th>
                <th className="szv2-td-num">Produtos</th>
                <th>Cadastrado em</th>
                <th style={{ textAlign: 'right' }}>Ações</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((c) => (
                <tr key={c.id}>
                  <td>
                    <div style={{ fontWeight: 500 }}>{c.nome || '—'}</div>
                    <div style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                      {c.email}
                    </div>
                  </td>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                    {c.telefone || '—'}
                  </td>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                    {c.cpf || '—'}
                  </td>
                  <td className="szv2-td-num" style={{ fontWeight: 600 }}>
                    {c.produtos_count > 0
                      ? c.produtos_count
                      : <span style={{ color: 'var(--szv2-text-faint)' }}>0</span>}
                  </td>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                    {c.created_at ? brDate(c.created_at) : '—'}
                  </td>
                  <td style={{ textAlign: 'right' }}>
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                      disabled={promotingId === c.id}
                      onClick={() => handlePromote(c)}
                      style={{ marginRight: 8 }}
                    >
                      {promotingId === c.id ? 'Promovendo…' : 'Promover a produtor'}
                    </button>
                    <button
                      type="button"
                      className="szv2-btn szv2-btn-danger szv2-btn-sm"
                      disabled={deletingId === c.id}
                      onClick={() => handleDelete(c)}
                    >
                      {deletingId === c.id ? 'Excluindo…' : 'Excluir'}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
