// Fila de aprovação: transferência Carteira COD → Carteira de Expedição (TPC).
// Produtor solicita em portal-ui (Wallet.tsx); nada executa até o admin
// decidir aqui. "Aprovar" NÃO credita na hora — abre um PIX REAL (Melhor
// Envio) no valor da transferência; só depois que o admin confirma que pagou
// (botão "Confirmar pagamento" dentro do modal) é que tpc_carteira é
// creditada. Reject cancela o PIX (se gerado) e estorna o débito COD. FEAT-WFT.
import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import { brl } from '../utils/format'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import StatusBadge from '../components/StatusBadge'
import Modal from '../components/Modal'

type Row = {
  id: number
  user_id: number
  wp_user_id: number
  amount: number
  status: 'pending' | 'approved' | 'rejected'
  admin_note: string
  requested_at: string
  decided_at: string | null
  recarga_id: number | null
  recarga_status: string
  pix_qr: string
  pix_codigo: string
  nome: string
  email: string
}

type PixResp = {
  ok: boolean
  id: number
  recarga_id: number
  qr_src: string
  copia_cola: string
  expires_at: string
}

const STATUS_LABELS: Record<string, string> = {
  pending: 'Pendente',
  approved: 'Aprovada',
  rejected: 'Rejeitada',
}

export default function WalletFreightTransfers() {
  const toast = useToast()
  const [status, setStatus] = useState<'pending' | 'all'>('pending')
  const [rows, setRows] = useState<Row[]>([])
  const [loading, setLoading] = useState(true)
  const [busyId, setBusyId] = useState<number | null>(null)

  // Modal de PIX (Aprovar → gera PIX → admin paga → confirma).
  const [pixRow, setPixRow] = useState<Row | null>(null)
  const [pix, setPix] = useState<PixResp | null>(null)
  const [pixLoading, setPixLoading] = useState(false)
  const [confirming, setConfirming] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const r = await api<{ ok: boolean; data: Row[] }>(`/wallet-freight-transfers?status=${status}`)
      setRows(r.data ?? [])
    } catch (e: any) {
      toast('err', e.message || 'Erro ao carregar transferências')
    } finally {
      setLoading(false)
    }
  }, [status, toast])

  useEffect(() => {
    load()
  }, [load])

  async function openApprove(row: Row) {
    setPixRow(row)
    setPix(null)
    setPixLoading(true)
    try {
      const r = await api<PixResp>(`/wallet-freight-transfers/${row.id}/generate-pix`, { method: 'POST' })
      setPix(r)
    } catch (e: any) {
      toast('err', e.message || 'Erro ao gerar PIX')
      setPixRow(null)
    } finally {
      setPixLoading(false)
    }
  }

  async function confirmPayment() {
    if (!pixRow) return
    setConfirming(true)
    try {
      await api(`/wallet-freight-transfers/${pixRow.id}/confirm-pix`, {
        method: 'POST',
        body: JSON.stringify({}),
      })
      toast('ok', 'Pagamento confirmado — carteira de frete creditada.')
      setPixRow(null)
      setPix(null)
      await load()
    } catch (e: any) {
      toast('err', e.message || 'Erro ao confirmar pagamento')
    } finally {
      setConfirming(false)
    }
  }

  function copyPix() {
    if (!pix?.copia_cola) return
    navigator.clipboard.writeText(pix.copia_cola)
    toast('ok', 'Código copia-e-cola copiado.')
  }

  async function reject(id: number) {
    const note = window.prompt('Motivo da rejeição (opcional):') ?? ''
    setBusyId(id)
    try {
      await api(`/wallet-freight-transfers/${id}/reject`, {
        method: 'POST',
        body: JSON.stringify({ admin_note: note }),
      })
      toast('ok', 'Transferência rejeitada e estornada.')
      await load()
    } catch (e: any) {
      toast('err', e.message || 'Erro ao rejeitar transferência')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div>
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Transferências para Carteira de Frete
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Solicitações do produtor para mover saldo da Carteira COD para a Carteira de Expedição.
        </p>
      </div>

      <div className="szv2-prod-subtabs" role="tablist" style={{ marginBottom: 16 }}>
        <button
          type="button"
          className={`szv2-prod-subtab ${status === 'pending' ? 'szv2-prod-subtab--active' : ''}`}
          role="tab"
          aria-selected={status === 'pending'}
          onClick={() => setStatus('pending')}
        >
          Pendentes
        </button>
        <button
          type="button"
          className={`szv2-prod-subtab ${status === 'all' ? 'szv2-prod-subtab--active' : ''}`}
          role="tab"
          aria-selected={status === 'all'}
          onClick={() => setStatus('all')}
        >
          Todas
        </button>
      </div>

      <div className="szv2-card">
        {loading ? (
          <TableSkeleton rows={5} cols={6} />
        ) : rows.length === 0 ? (
          <EmptyState title="Nenhuma transferência" description="Não há solicitações nesta fila." />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Produtor</th>
                  <th style={{ textAlign: 'right' }}>Valor</th>
                  <th>Status</th>
                  <th>Nota do admin</th>
                  <th style={{ width: 200 }}>Ações</th>
                </tr>
              </thead>
              <tbody>
                {rows.map(row => (
                  <tr key={row.id}>
                    <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                      {new Date(row.requested_at).toLocaleString('pt-BR')}
                    </td>
                    <td>
                      <div style={{ fontSize: 13 }}>{row.nome || `wp_user_id ${row.wp_user_id}`}</div>
                      {row.email && (
                        <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{row.email}</div>
                      )}
                    </td>
                    <td style={{ textAlign: 'right', fontWeight: 700, color: 'var(--szv2-brand)' }}>
                      {brl(row.amount)}
                    </td>
                    <td><StatusBadge status={row.status} label={STATUS_LABELS[row.status]} /></td>
                    <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{row.admin_note || '—'}</td>
                    <td>
                      {row.status === 'pending' ? (
                        <div style={{ display: 'flex', gap: 6 }}>
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-brand szv2-btn-sm"
                            disabled={busyId === row.id}
                            onClick={() => openApprove(row)}
                          >
                            Aprovar
                          </button>
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-danger szv2-btn-sm"
                            disabled={busyId === row.id}
                            onClick={() => reject(row.id)}
                          >
                            Rejeitar
                          </button>
                        </div>
                      ) : (
                        <span style={{ color: 'var(--szv2-text-muted)' }}>—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <Modal
        open={!!pixRow}
        onClose={() => { if (!confirming) { setPixRow(null); setPix(null) } }}
        title={`Pagar PIX — ${pixRow?.nome || 'produtor'}`}
        width="440px"
      >
        <div style={{ textAlign: 'center' }}>
          {pixLoading ? (
            <TableSkeleton rows={3} cols={1} />
          ) : pix ? (
            <>
              <p style={{ margin: '0 0 12px', fontSize: 14 }}>
                Valor: <strong>{brl(pixRow?.amount ?? 0)}</strong>
              </p>
              <p style={{ margin: '0 0 12px', fontSize: 12, color: 'var(--szv2-warning)' }}>
                Pague este PIX com a conta da empresa. Só clique em "Confirmar pagamento"
                depois que o valor sair de fato — a carteira de frete do produtor só é
                creditada após a confirmação.
              </p>
              {pix.qr_src && (
                <img
                  src={pix.qr_src}
                  alt="QR Code PIX"
                  style={{ width: 220, height: 220, margin: '0 auto 12px', display: 'block' }}
                />
              )}
              {pix.copia_cola && (
                <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
                  <input
                    readOnly
                    className="szv2-input"
                    value={pix.copia_cola}
                    style={{ flex: 1, fontSize: 11 }}
                    onFocus={e => e.target.select()}
                  />
                  <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={copyPix}>
                    Copiar
                  </button>
                </div>
              )}
              <div style={{ display: 'flex', justifyContent: 'center', gap: 8 }}>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-secondary"
                  onClick={() => { setPixRow(null); setPix(null) }}
                  disabled={confirming}
                >
                  Fechar
                </button>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-brand"
                  onClick={confirmPayment}
                  disabled={confirming}
                >
                  {confirming ? 'Confirmando…' : 'Confirmar pagamento'}
                </button>
              </div>
            </>
          ) : (
            <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Erro ao gerar PIX.</p>
          )}
        </div>
      </Modal>
    </div>
  )
}
