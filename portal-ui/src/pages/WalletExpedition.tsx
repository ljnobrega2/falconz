// Carteira de Expedição (TPC) — porte fiel de templates/portal/v2/sections/wallet-expedition.php.
// Carteira PRÉ-PAGA de FRETE (Melhor Envio), distinta da Carteira COD (Wallet.tsx).
//
// Ligada ao go/portal (namespace /wp-json/senderzz/v1):
//   GET /portal/wallet-expedition/summary  — KPI de saldo (available/balance/reserved/low_balance)
//   GET /portal/wallet-expedition/history  — extrato TPC (?period=hoje|7d|30d|all e/ou ?from=&to=)
//
// IMPORTANTE — shape de resposta (≠ Wallet.tsx): o handler embrulha o KPI sob `data`
// (objeto), NÃO sob `summary`. Ler r.data, nunca r.summary (senão KPI fica em branco).
//
// DESVIOS FIÉIS (contrato Go ≠ shape WP, mas UX idêntica):
//   - PIX (Gerar PIX / "Já paguei — verificar"): a emissão/confirmação NÃO faz parte do
//     contrato go/portal — permanece no WP via admin-ajax.php (action=senderzz_portal,
//     szaction=generate_pix / check_pix), conforme documentado no header do handler Go
//     (go/portal/.../wallet_expedition_portal.go:54-59) e em Wallet.tsx. Espelha o
//     "nonce gap" de Expedicao.tsx: o nonce 'senderzz_portal' ainda não é mintado pelo
//     SPA, então a chamada pode falhar → toast/inline de erro. Mantido para não dropar a
//     AÇÃO PRINCIPAL da seção (port fiel).
//   - Filtros de período: no WP os chips/date-inputs são cosméticos (a tabela é renderizada
//     uma vez, sem filtro). Aqui ligamos ao Go (?period= e ?from=&to=) — visualmente
//     idêntico, porém funcional. Chips limpam os date-inputs; date-inputs limpam o chip
//     ativo; default 7d (mirror do chip ativo do WP + default do Go).
//   - location.reload() do WP na confirmação do PIX → refetch de estado (idioma SPA).
//   - Placement: o WP não expõe esta seção como item de sidebar próprio (ausente do
//     section-map de dashboard-v2.php; vive dentro de Expedição). Aqui é uma página
//     própria em /wallet-expedition, alcançável por rota; nav fica a cargo do Layout.
//   - Brand accent SEMPRE via var(--szv2-brand) (#1E6FF2).
import { useCallback, useEffect, useState } from 'react'
import { api, labelsApi } from '../api'
import { useToast } from '../hooks/useToast'
import EmptyState from '../components/EmptyState'
import InlineLoading from '../components/InlineLoading'
import AlertError from '../components/AlertError'
import FalkDatePicker from '../components/FalkDatePicker'
import { brl } from '../utils/format'

// ── Shapes (espelham go/portal/internal/handlers/wallet_expedition_portal.go) ────

type Summary = {
  available: number // saldo disponível = max(0, saldo - reservado)
  balance: number // saldo bruto
  reserved: number // saldo reservado (frete em reserva)
  low_balance: boolean // saldo < R$10
}
// ATENÇÃO: o KPI vem sob `data`, NÃO `summary` (≠ wallet.go). Ver header.
type SummaryResp = { ok: boolean; data: Summary; scope: string }

type HistoryRow = {
  date: string // já formatado "dd/mm/aaaa hh:mm"
  description: string
  order: string // "#123" | "—"
  tipo: string // tipo cru
  tipo_label: string // rótulo legível (Recarga|Frete|Estorno|…)
  tipo_badge: string // success|neutral|warning (classe szv2-badge-*)
  value: number
  fee: number
  net: number
  status: string
}
type HistoryResp = { ok: boolean; data: HistoryRow[]; total: number; scope: string }

// Resposta do PIX via go/labels (POST /balance/pix) — mesmo contrato usado em
// LabelCredits.tsx. NÃO é mais admin-ajax.php do WP: FALK não roda WordPress,
// então esse endpoint sempre dava 405 (bug real, corrigido).
type PixData = {
  id?: number
  amount?: number
  qr_code?: string
  qr_code_image?: string
  link?: string
  expiry?: string
}

// ── Períodos (espelham os chips do WP: Hoje / 7d / 30d / Tudo) ───────────────────
const HIST_PERIODS: Array<[string, string]> = [
  ['Hoje', 'hoje'],
  ['7d', '7d'],
  ['30d', '30d'],
  ['Tudo', 'all'],
]

// Presets de recarga PIX (espelham os botões do WP).
const PIX_PRESETS = [50, 100, 200, 500]

const TXT_MUTED = 'var(--szv2-text-muted)'
const TXT_FAINT = 'var(--szv2-text-faint)'

// "Y-m-d" no fuso America/Sao_Paulo (mirror de current_time('Y-m-d') do WP).
// en-CA dá "YYYY-MM-DD"; offset aplicado em dias antes da formatação.
function isoDay(offsetDays = 0): string {
  const d = new Date()
  d.setDate(d.getDate() + offsetDays)
  return d.toLocaleDateString('en-CA', { timeZone: 'America/Sao_Paulo' })
}

export default function WalletExpedition() {
  const toast = useToast()

  // ── KPI de saldo ──────────────────────────────────────────────────────────
  const [summary, setSummary] = useState<Summary>({ available: 0, balance: 0, reserved: 0, low_balance: false })
  const [loadingSummary, setLoadingSummary] = useState(true)
  const [err, setErr] = useState('')

  // ── Recarga PIX ─────────────────────────────────────────────────────────────
  const [selectedPreset, setSelectedPreset] = useState(0)
  const [customAmount, setCustomAmount] = useState('')
  const [pixGenerating, setPixGenerating] = useState(false)
  const [pixError, setPixError] = useState('')

  // ── Modal PIX ─────────────────────────────────────────────────────────────
  const [pixModalOpen, setPixModalOpen] = useState(false)
  const [pixCode, setPixCode] = useState('')
  const [pixQr, setPixQr] = useState('')
  const [recargaId, setRecargaId] = useState(0)
  const [copied, setCopied] = useState(false)
  const [pixChecking, setPixChecking] = useState(false)
  const [pixCheckMsg, setPixCheckMsg] = useState<{ text: string; ok: boolean } | null>(null)

  // ── Histórico ─────────────────────────────────────────────────────────────
  const [history, setHistory] = useState<HistoryRow[]>([])
  const [loadingHist, setLoadingHist] = useState(true)
  const [histPeriod, setHistPeriod] = useState('7d')
  const [from, setFrom] = useState(isoDay(-7))
  const [to, setTo] = useState(isoDay(0))

  // ── Carregamentos ─────────────────────────────────────────────────────────

  const loadSummary = useCallback(() => {
    setLoadingSummary(true)
    setErr('')
    api<SummaryResp>('/portal/wallet-expedition/summary')
      .then(r => setSummary(r.data || { available: 0, balance: 0, reserved: 0, low_balance: false }))
      .catch(e => setErr(e.message || 'Erro ao carregar a carteira de expedição'))
      .finally(() => setLoadingSummary(false))
  }, [])

  // Carrega o extrato. Se from+to vierem preenchidos têm prioridade sobre period
  // (mirror do Go: date-inputs sobrescrevem o chip).
  const loadHistory = useCallback((period: string, useRange: boolean, f?: string, t?: string) => {
    setLoadingHist(true)
    const qs = useRange && f && t
      ? `from=${encodeURIComponent(f)}&to=${encodeURIComponent(t)}`
      : `period=${encodeURIComponent(period)}`
    api<HistoryResp>(`/portal/wallet-expedition/history?${qs}`)
      .then(r => setHistory(r.data || []))
      .catch(() => setHistory([]))
      .finally(() => setLoadingHist(false))
  }, [])

  useEffect(() => {
    loadSummary()
    loadHistory('7d', false)
  }, [loadSummary, loadHistory])

  // Chip de período → refetch; limpa os date-inputs (mirror do WP).
  function selectHistPeriod(p: string) {
    setHistPeriod(p)
    loadHistory(p, false)
  }
  // Date-inputs → refetch por range; limpa o chip ativo (mirror do WP).
  function onRangeChange(nextFrom: string, nextTo: string) {
    setFrom(nextFrom)
    setTo(nextTo)
    setHistPeriod('')
    if (nextFrom && nextTo) loadHistory('', true, nextFrom, nextTo)
  }

  // ── Recarga PIX (preset/custom) ──────────────────────────────────────────────

  function selectPreset(val: number) {
    setSelectedPreset(val)
    setCustomAmount(String(val))
    // Auto-gera o PIX imediatamente ao escolher um preset (mirror do WP).
    gerarPix(val)
  }

  function onCustomInput(v: string) {
    setCustomAmount(v)
    setSelectedPreset(0)
  }

  // Emite o PIX via go/labels (POST /balance/pix — mesmo endpoint de LabelCredits.tsx).
  // amountArg permite disparar a partir do preset sem esperar o setState do input.
  async function gerarPix(amountArg?: number) {
    const valor = amountArg || selectedPreset || parseFloat(customAmount) || 0
    if (valor < 10) {
      toast('warn', 'Informe um valor mínimo de R$ 10,00.')
      return
    }
    setPixGenerating(true)
    setPixError('')
    try {
      const d = await labelsApi<PixData>('/balance/pix', {
        method: 'POST',
        body: JSON.stringify({ amount: valor }),
      })
      const code = d?.qr_code || ''
      if (code) {
        setPixCode(code)
        setPixQr(d?.qr_code_image || '')
        setRecargaId(d?.id || 0)
        setCopied(false)
        setPixCheckMsg(null)
        setPixModalOpen(true)
      } else {
        setPixError('Erro ao gerar PIX. Tente novamente.')
      }
    } catch (e: any) {
      setPixError(e?.message || 'Erro de conexão. Tente novamente.')
    } finally {
      setPixGenerating(false)
    }
  }

  async function copyPixCode() {
    try {
      await navigator.clipboard?.writeText(pixCode)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      toast('err', 'Não foi possível copiar o código PIX.')
    }
  }

  // "Já paguei — verificar confirmação". O Melhor Envio não emite webhook de
  // pagamento (painel deles só oferece "Atualização das etiquetas criadas e
  // editadas" — confirmado 2026-07-27); a confirmação é ATIVA: go/labels compara
  // o saldo real da conta ME contra o que já foi contabilizado e, se bater,
  // credita a carteira TPC (a que trava emissão de etiqueta) nesta mesma chamada.
  async function checkPix() {
    if (!recargaId) {
      setPixCheckMsg({ text: 'Nenhum PIX gerado nesta sessão.', ok: false })
      return
    }
    setPixChecking(true)
    setPixCheckMsg(null)
    try {
      const r = await labelsApi<{ ok: boolean; confirmed: boolean }>(`/balance/pix/${recargaId}/confirm`, {
        method: 'POST',
      })
      const ok = !!r.confirmed
      setPixCheckMsg({
        text: ok
          ? 'Pagamento confirmado! Saldo atualizado.'
          : 'Ainda não confirmado. A compensação do PIX pode levar alguns instantes — tente de novo em breve.',
        ok,
      })
      if (ok) {
        const sr = await api<SummaryResp>('/portal/wallet-expedition/summary')
        setSummary(sr.data || summary)
        setTimeout(() => {
          setPixModalOpen(false)
          loadHistory(histPeriod || '7d', !histPeriod, from, to)
        }, 2000)
      }
    } catch {
      setPixCheckMsg({ text: 'Erro de conexão.', ok: false })
    } finally {
      setPixChecking(false)
    }
  }

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <section id="sec-wallet-expedition" className="sz-sec">
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Recarga de frete
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Saldo pré-pago usado para pagar o frete dos seus envios. Recarregue por PIX.
        </p>
      </div>

      {!!err && <AlertError message={err} onRetry={loadSummary} />}

      {/* KPI grid: saldo disponível (1 col) + recarga PIX (span 2) */}
      <div className="szv2-kpi-grid szv2-kpi-grid-3col" style={{ marginBottom: 'var(--szv2-space-3)' }}>
        <div className="szv2-card szv2-kpi">
          <span className="szv2-kpi-label">Saldo disponível</span>
          <span
            className="szv2-kpi-value szv2-num"
            style={{ color: summary.low_balance ? 'var(--szv2-danger)' : 'var(--szv2-brand)' }}
          >
            {loadingSummary ? '…' : brl(summary.available)}
          </span>
          <span className="szv2-kpi-meta">
            {summary.low_balance ? 'Saldo insuficiente para envios' : 'Reservado para frete'}
          </span>
        </div>

        <div className="szv2-card szv2-kpi" style={{ gridColumn: 'span 2' }}>
          <span className="szv2-kpi-label">Recarregar via PIX</span>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center', margin: '8px 0' }}>
            {PIX_PRESETS.map(val => (
              <button
                key={val}
                type="button"
                className={`szv2-btn szv2-btn-sm ${selectedPreset === val ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                onClick={() => selectPreset(val)}
                disabled={pixGenerating}
              >
                R$ {val.toLocaleString('pt-BR')}
              </button>
            ))}
            <input
              type="number"
              className="szv2-input szv2-input-sm"
              placeholder="Outro valor (mín. R$ 10)"
              min={10}
              step={1}
              style={{ width: 160 }}
              value={customAmount}
              onChange={e => onCustomInput(e.target.value)}
            />
            <button
              type="button"
              className="szv2-btn szv2-btn-brand"
              onClick={() => gerarPix()}
              disabled={pixGenerating}
            >
              {pixGenerating ? 'Gerando…' : 'Gerar PIX'}
            </button>
          </div>
          {!!pixError && <p style={{ color: 'var(--szv2-danger)', fontSize: 13, margin: 0 }}>{pixError}</p>}
        </div>
      </div>

      {/* Histórico de movimentações */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Histórico</h2>
            <p className="szv2-card-sub">Recargas e consumos de frete.</p>
          </div>
        </div>

        {/* Filtros de período — abaixo do cabeçalho, igual ao COD */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap', marginBottom: 12 }}>
          {HIST_PERIODS.map(([lbl, slug]) => (
            <button
              key={slug}
              type="button"
              className={`szv2-period-btn ${histPeriod === slug ? 'szv2-period-btn--active' : ''}`}
              onClick={() => selectHistPeriod(slug)}
            >
              {lbl}
            </button>
          ))}
          <FalkDatePicker
            value={from}
            style={{ width: 138, marginLeft: 4 }}
            aria-label="Data inicial"
            onChange={v => onRangeChange(v, to)}
          />
          <span style={{ fontSize: 12, color: TXT_MUTED }}>–</span>
          <FalkDatePicker
            value={to}
            style={{ width: 138 }}
            aria-label="Data final"
            onChange={v => onRangeChange(from, v)}
          />
        </div>

        {loadingHist ? (
          <InlineLoading label="Carregando extrato…" />
        ) : history.length === 0 ? (
          <EmptyState
            title="Nenhuma movimentação encontrada"
            description="Recargas e consumos de frete aparecerão aqui."
          />
        ) : (
          <div className="szv2-table-wrap szv2-table-flush">
            <table className="szv2-table">
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Descrição</th>
                  <th>Pedido</th>
                  <th>Tipo</th>
                  <th className="szv2-td-num">Valor</th>
                  <th className="szv2-td-num">Taxa</th>
                  <th className="szv2-td-num">Líquido</th>
                </tr>
              </thead>
              <tbody>
                {history.slice(0, 60).map((tx, i) => (
                  <tr key={i}>
                    <td style={{ color: TXT_MUTED }}>{tx.date}</td>
                    <td>{tx.description}</td>
                    <td className="szv2-num">{tx.order}</td>
                    <td>
                      <span className={`sz-badge szv2-badge-${tx.tipo_badge || 'neutral'}`}>{tx.tipo_label || '—'}</span>
                    </td>
                    <td className="szv2-td-num szv2-num">{brl(tx.value)}</td>
                    <td className="szv2-td-num szv2-num">{brl(tx.fee)}</td>
                    <td className="szv2-td-num szv2-num">{brl(tx.net)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* ── Modal PIX ── */}
      {pixModalOpen && (
        <div
          className="szv2-modal-overlay szv2-open"
          role="dialog"
          aria-modal="true"
          aria-label="Confirmar pagamento PIX"
          onClick={() => setPixModalOpen(false)}
        >
          <div className="szv2-modal" onClick={e => e.stopPropagation()} style={{ maxWidth: 460 }}>
            <div className="szv2-modal-head">
              <h3>Confirmar Pagamento</h3>
              <button type="button" className="szv2-modal-x" aria-label="Fechar" onClick={() => setPixModalOpen(false)}>
                &times;
              </button>
            </div>
            <div className="szv2-modal-body" style={{ textAlign: 'center' }}>
              <p style={{ fontSize: 13, color: TXT_MUTED, margin: '0 0 16px' }}>
                Escaneie o QR Code ou copie o código abaixo
              </p>
              <div
                style={{
                  margin: '0 auto 16px',
                  width: 200,
                  height: 200,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  background: 'var(--szv2-surface-alt)',
                  borderRadius: 'var(--szv2-radius-md)',
                }}
              >
                {pixQr ? (
                  <img
                    src={pixQr}
                    style={{ width: 180, height: 180, display: 'block', borderRadius: 4 }}
                    alt="QR Code PIX"
                  />
                ) : (
                  <span style={{ color: TXT_FAINT, fontSize: 12 }}>QR Code indisponível</span>
                )}
              </div>
              <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
                <input
                  type="text"
                  className="szv2-input szv2-input-sm"
                  readOnly
                  value={pixCode}
                  placeholder="Código PIX"
                  style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 11, flex: 1 }}
                />
                <button type="button" className="szv2-btn szv2-btn-brand szv2-btn-sm" onClick={copyPixCode}>
                  {copied ? 'Copiado!' : 'Copiar'}
                </button>
              </div>
              <p style={{ fontSize: 11, color: TXT_FAINT, margin: 0 }}>
                O PIX pode levar até 1 minuto para ser confirmado.
              </p>
              <div style={{ marginTop: 12, textAlign: 'center' }}>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-brand"
                  onClick={checkPix}
                  disabled={pixChecking}
                  style={{ gap: 8 }}
                >
                  {pixChecking ? 'Verificando…' : 'Já paguei — verificar confirmação'}
                </button>
                {!!pixCheckMsg && (
                  <div
                    style={{
                      marginTop: 8,
                      fontSize: 13,
                      color: pixCheckMsg.ok ? 'var(--szv2-success)' : TXT_MUTED,
                    }}
                  >
                    {pixCheckMsg.text}
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
