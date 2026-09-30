// Créditos de Etiquetas — recarga da conta Melhor Envio via PIX pelo produtor.
//
// Fluxo:
//   1. GET /balance → exibe saldo atual da conta ME da plataforma
//   2. Produtor digita valor → POST /balance/pix → recebe QR PIX
//   3. Produtor paga PIX → conta ME é creditada automaticamente pela ME
//   4. GET /balance/history → histórico de recargas do produtor
//
// Rotas do labels-service (go/labels, via labelsApi):
//   GET  /balance         — saldo atual da conta ME
//   POST /balance/pix     — gera PIX de recarga
//   GET  /balance/history — histórico de recargas do produtor
import { useCallback, useEffect, useRef, useState } from 'react'
import { labelsApi } from '../api'
import { useToast } from '../hooks/useToast'
import InlineLoading from '../components/InlineLoading'
import AlertError from '../components/AlertError'
import { brl } from '../utils/format'

type HistoryRow = {
  id: number
  amount: number
  status: string
  expiry: string | null
  created_at: string
}

type PixResp = {
  ok: boolean
  id: number
  amount: number
  qr_code: string
  qr_code_image: string
  link: string
  expiry: string
  status: string
}

const STATUS_LABELS: Record<string, string> = {
  pending: 'Aguardando pagamento',
  paid: 'Pago',
  expired: 'Expirado',
  cancelled: 'Cancelado',
}
const STATUS_CLASS: Record<string, string> = {
  pending: 'sz-badge-warning',
  paid: 'sz-badge-success',
  expired: 'sz-badge-neutral',
  cancelled: 'sz-badge-neutral',
}

export default function LabelCredits() {
  const toast = useToast()

  const [balance, setBalance] = useState<number | null>(null)
  const [balanceLoading, setBalanceLoading] = useState(true)
  const [balanceErr, setBalanceErr] = useState('')

  const [history, setHistory] = useState<HistoryRow[]>([])
  const [histLoading, setHistLoading] = useState(true)

  const [amount, setAmount] = useState('')
  const [generating, setGenerating] = useState(false)
  const [pix, setPix] = useState<PixResp | null>(null)
  const copyTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [copied, setCopied] = useState(false)

  const loadBalance = useCallback(async () => {
    setBalanceLoading(true)
    setBalanceErr('')
    try {
      const r = await labelsApi<{ ok: boolean; balance: number }>('/balance')
      setBalance(r.balance)
    } catch (e: any) {
      setBalanceErr(e.message || 'Erro ao consultar saldo ME')
    } finally {
      setBalanceLoading(false)
    }
  }, [])

  const loadHistory = useCallback(async () => {
    setHistLoading(true)
    try {
      const r = await labelsApi<{ ok: boolean; data: HistoryRow[] }>('/balance/history')
      setHistory(r.data ?? [])
    } catch {
      // histórico é best-effort; não bloqueia a tela
    } finally {
      setHistLoading(false)
    }
  }, [])

  useEffect(() => {
    loadBalance()
    loadHistory()
  }, [loadBalance, loadHistory])

  async function handleGenerate() {
    const val = parseFloat(amount.replace(',', '.'))
    if (!val || val < 10) {
      toast('warn', 'Valor mínimo é R$ 10,00')
      return
    }
    if (val > 5000) {
      toast('warn', 'Valor máximo é R$ 5.000,00')
      return
    }
    setGenerating(true)
    try {
      const r = await labelsApi<PixResp>('/balance/pix', {
        method: 'POST',
        body: JSON.stringify({ amount: val }),
      })
      setPix(r)
      setAmount('')
      toast('ok', `PIX de ${brl(val)} gerado. Pague para creditar a conta ME.`)
      await loadHistory()
    } catch (e: any) {
      toast('err', e.message || 'Erro ao gerar PIX')
    } finally {
      setGenerating(false)
    }
  }

  function handleCopy(text: string) {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      if (copyTimeoutRef.current) clearTimeout(copyTimeoutRef.current)
      copyTimeoutRef.current = setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <div className="sz-section">
      <div className="sz-section-header">
        <h2 className="sz-section-title">Créditos de Etiquetas</h2>
        <p className="sz-section-subtitle">
          Recarregue via PIX para emitir etiquetas de envio.
        </p>
      </div>

      {/* Saldo atual */}
      <div className="sz-card sz-mb-4">
        <div className="sz-card-body">
          <p className="sz-label">Saldo atual</p>
          {balanceLoading ? (
            <InlineLoading />
          ) : balanceErr ? (
            <AlertError message={balanceErr} onRetry={loadBalance} />
          ) : (
            <p className="sz-value-lg">{balance !== null ? brl(balance) : '—'}</p>
          )}
        </div>
      </div>

      {/* Formulário de recarga */}
      <div className="sz-card sz-mb-4">
        <div className="sz-card-body">
          <h3 className="sz-card-title">Nova Recarga via PIX</h3>
          <div className="sz-form-row" style={{ alignItems: 'flex-end', gap: 12 }}>
            <div className="sz-form-group" style={{ flex: 1 }}>
              <label className="sz-label" htmlFor="recharge-amount">Valor (R$)</label>
              <input
                id="recharge-amount"
                className="sz-input"
                type="number"
                min="10"
                max="5000"
                step="1"
                placeholder="Ex: 50"
                value={amount}
                onChange={e => setAmount(e.target.value)}
                disabled={generating}
              />
              <p className="sz-input-hint">Mínimo R$ 10,00 · Máximo R$ 5.000,00</p>
            </div>
            <button
              className="sz-btn sz-btn-primary"
              onClick={handleGenerate}
              disabled={generating || !amount}
              style={{ marginBottom: 20 }}
            >
              {generating ? 'Gerando…' : 'Gerar PIX'}
            </button>
          </div>
        </div>
      </div>

      {/* QR Code gerado */}
      {pix && (
        <div className="sz-card sz-mb-4" style={{ borderColor: 'var(--szv2-brand)' }}>
          <div className="sz-card-body" style={{ textAlign: 'center' }}>
            <h3 className="sz-card-title">PIX de {brl(pix.amount)}</h3>
            {pix.qr_code_image && (
              <img
                src={pix.qr_code_image}
                alt="QR Code PIX"
                style={{ width: 200, height: 200, margin: '12px auto', display: 'block' }}
              />
            )}
            {pix.qr_code && (
              <div style={{ display: 'flex', gap: 8, justifyContent: 'center', marginTop: 8 }}>
                <input
                  readOnly
                  className="sz-input"
                  value={pix.qr_code}
                  style={{ flex: 1, maxWidth: 420, fontSize: 12 }}
                  onFocus={e => e.target.select()}
                />
                <button
                  className={`sz-btn ${copied ? 'sz-btn-success' : 'sz-btn-secondary'}`}
                  onClick={() => handleCopy(pix.qr_code)}
                >
                  {copied ? 'Copiado!' : 'Copiar'}
                </button>
              </div>
            )}
            {pix.link && (
              <a
                href={pix.link}
                target="_blank"
                rel="noopener noreferrer"
                className="sz-link"
                style={{ display: 'block', marginTop: 8 }}
              >
                Abrir link de pagamento
              </a>
            )}
            {pix.expiry && (
              <p className="sz-text-muted" style={{ marginTop: 8, fontSize: 12 }}>
                Válido até: {new Date(pix.expiry).toLocaleString('pt-BR')}
              </p>
            )}
            <div style={{ display: 'flex', gap: 8, justifyContent: 'center', marginTop: 16 }}>
              <button className="sz-btn sz-btn-secondary" onClick={loadBalance}>
                Verificar saldo
              </button>
              <button className="sz-btn sz-btn-ghost" onClick={() => setPix(null)}>
                Fechar
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Histórico */}
      <div className="sz-card">
        <div className="sz-card-body">
          <h3 className="sz-card-title">Histórico de Recargas</h3>
          {histLoading ? (
            <InlineLoading />
          ) : history.length === 0 ? (
            <p className="sz-text-muted">Nenhuma recarga registrada.</p>
          ) : (
            <table className="sz-table">
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Valor</th>
                  <th>Status</th>
                  <th>Validade</th>
                </tr>
              </thead>
              <tbody>
                {history.map(row => (
                  <tr key={row.id}>
                    <td>{new Date(row.created_at).toLocaleString('pt-BR')}</td>
                    <td>{brl(row.amount)}</td>
                    <td>
                      <span className={`sz-badge ${STATUS_CLASS[row.status] ?? 'sz-badge-neutral'}`}>
                        {STATUS_LABELS[row.status] ?? row.status}
                      </span>
                    </td>
                    <td>
                      {row.expiry
                        ? new Date(row.expiry).toLocaleString('pt-BR')
                        : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}
