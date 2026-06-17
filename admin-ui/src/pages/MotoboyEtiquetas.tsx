// MotoboyEtiquetas — listagem de pedidos motoboy formatados como etiquetas
// para impressão. Espelha sz_mb_tab_etiquetas() (admin.php:1383, §2.5).
//
// Pontos relevantes:
//   • Grid auto-fill minmax(280px,1fr); cada card é uma etiqueta com QR.
//   • Botão "Imprimir etiquetas" chama window.print(). O CSS @media print
//     esconde tudo exceto #sz-etiq-print (cards) e usa border 1.5px.
//   • CEP renderizado como XXXXX-XXX (paridade PHP: substr 0..5 + "-" + 5..).
//   • QR via api.qrserver.com (mesma URL do PHP); package_code chega pronto
//     do backend e já é HMAC-SHA256 compatível com o leitor do PWA.

import { useEffect, useState } from 'react'
import { api } from '../api'

// ─── Tipos ────────────────────────────────────────────────────────────────

type Etiqueta = {
  pedido_id: number
  wc_order_id: number
  motoboy_nome: string
  zona_nome: string
  dest_nome: string
  dest_endereco: string
  dest_numero: string
  dest_complemento: string
  dest_bairro: string
  dest_cidade: string
  dest_uf: string
  dest_cep: string
  dest_telefone: string
  valor_pedido: number
  pgto_dinheiro: number
  pgto_pix: number
  pgto_cartao: number
  package_code: string
}

type StatusFiltro = 'agendado' | 'embalado' | 'em_rota'

// ─── Helpers ──────────────────────────────────────────────────────────────

const fmtMoney = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const todayISO = () => new Date().toISOString().slice(0, 10)

// CEP "12345678" → "12345-678" (paridade PHP). Tolera CEP curto/longo.
function formatCep(cep: string): string {
  const d = (cep || '').replace(/\D+/g, '')
  if (!d) return ''
  if (d.length <= 5) return d
  return d.slice(0, 5) + '-' + d.slice(5, 8)
}

// Concatena o que tiver valor > 0 nas três formas de pagamento.
function pgtoString(e: Etiqueta): string {
  const parts: string[] = []
  if (e.pgto_dinheiro > 0) parts.push(`Dinheiro R$ ${fmtMoney(e.pgto_dinheiro)}`)
  if (e.pgto_pix > 0)      parts.push(`PIX R$ ${fmtMoney(e.pgto_pix)}`)
  if (e.pgto_cartao > 0)   parts.push(`Cartão R$ ${fmtMoney(e.pgto_cartao)}`)
  return parts.length ? parts.join(' + ') : 'A cobrar'
}

// URL do QR. O backend já gera o package_code com HMAC-SHA256; aqui só
// fazemos o encoding do parâmetro `data`.
function qrUrl(code: string): string {
  return `https://api.qrserver.com/v1/create-qr-code/?size=160x160&data=${encodeURIComponent(code)}`
}

const STATUS_LIST: { key: StatusFiltro; label: string }[] = [
  { key: 'agendado', label: 'Agendados' },
  { key: 'embalado', label: 'Embalados' },
  { key: 'em_rota',  label: 'Em rota'   },
]

// ─── Página ───────────────────────────────────────────────────────────────

export default function MotoboyEtiquetas() {
  const [date, setDate] = useState<string>(todayISO())
  const [status, setStatus] = useState<StatusFiltro>('agendado')

  const [items, setItems] = useState<Etiqueta[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const qs = new URLSearchParams()
      qs.set('date', date)
      qs.set('status', status)
      const r = await api<{ items: Etiqueta[]; count: number }>(
        `/motoboy-etiquetas?${qs.toString()}`,
      )
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar etiquetas')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() /* eslint-disable-next-line */ }, [date, status])

  // Data formatada para o título auxiliar (DD/MM/YYYY).
  const dateLabel = (() => {
    const [y, m, d] = date.split('-')
    return y && m && d ? `${d}/${m}/${y}` : date
  })()

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* CSS print-only e cards. Mantemos no próprio componente para não
          poluir o CSS global e permitir uso em outras telas. */}
      <style>{`
        .sz-etiq-grid {
          display: grid;
          grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
          gap: 14px;
        }
        .sz-etiq {
          border: 1.5px solid #374151;
          border-radius: 10px;
          padding: 14px 16px 30px;
          font-size: 13px;
          background: #fff;
          color: #111;
          position: relative;
          page-break-inside: avoid;
        }
        .sz-etiq-header {
          display: flex;
          justify-content: space-between;
          align-items: flex-start;
          margin-bottom: 8px;
          gap: 8px;
        }
        .sz-etiq-num {
          font-size: 18px;
          font-weight: 700;
          color: #E8650A;
        }
        .sz-etiq-mb {
          font-size: 12px;
          background: #f3f4f6;
          padding: 3px 8px;
          border-radius: 6px;
          color: #374151;
          text-align: right;
        }
        .sz-etiq-dest {
          font-size: 14px;
          font-weight: 700;
          margin-bottom: 4px;
          color: #111827;
        }
        .sz-etiq-addr {
          color: #374151;
          margin-bottom: 8px;
          line-height: 1.4;
          font-size: 13px;
        }
        .sz-etiq-footer {
          display: flex;
          justify-content: space-between;
          gap: 8px;
          border-top: 1px dashed #d1d5db;
          padding-top: 8px;
          margin-top: 6px;
        }
        .sz-etiq-val {
          font-weight: 700;
          font-size: 14px;
          color: #111827;
        }
        .sz-etiq-pgto {
          font-size: 12px;
          color: #6b7280;
          margin-top: 2px;
        }
        .sz-etiq-tel {
          font-size: 12px;
          color: #6b7280;
          align-self: flex-end;
        }
        .sz-etiq-cep {
          position: absolute;
          bottom: 10px;
          right: 14px;
          font-size: 11px;
          color: #9ca3af;
        }
        .sz-etiq-qr-wrap {
          display: flex;
          align-items: center;
          gap: 10px;
          margin-top: 10px;
          border-top: 1px dashed #d1d5db;
          padding-top: 8px;
        }
        .sz-etiq-qr-wrap img {
          width: 64px;
          height: 64px;
          image-rendering: pixelated;
        }
        .sz-etiq-qr-code {
          font-size: 10px;
          line-height: 1.35;
          word-break: break-all;
          color: #111827;
        }

        @media print {
          body * { visibility: hidden !important; }
          #sz-etiq-print, #sz-etiq-print * { visibility: visible !important; }
          #sz-etiq-print {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            width: 100%;
            padding: 0;
            background: #fff;
          }
          .sz-etiq {
            border: 1.5px solid #000 !important;
            page-break-inside: avoid;
          }
        }
      `}</style>

      {/* Top bar: filtros + botão imprimir. Fica fora do #sz-etiq-print para
          ser ocultado pelo @media print. */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head" style={{ flexWrap: 'wrap', gap: 12 }}>
          <div>
            <h2>Etiquetas Motoboy</h2>
            <p className="szv2-card-sub">
              Lista pedidos do dia para impressão. QR Code é validado pelo PWA do motoboy ao iniciar rota.
            </p>
          </div>
          <button
            type="button"
            className="szv2-btn szv2-btn-brand"
            onClick={() => window.print()}
            disabled={loading || items.length === 0}
          >
            🖨️ Imprimir etiquetas
          </button>
        </div>

        <div style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: 12,
          alignItems: 'flex-end',
          padding: '12px 0 4px',
        }}>
          <div className="szv2-field" style={{ minWidth: 160 }}>
            <label className="szv2-label">Data</label>
            <input
              type="date"
              className="szv2-input"
              value={date}
              onChange={(e) => setDate(e.target.value)}
              disabled={loading}
            />
          </div>
          <div className="szv2-field" style={{ minWidth: 160 }}>
            <label className="szv2-label">Status</label>
            <select
              className="szv2-select"
              value={status}
              onChange={(e) => setStatus(e.target.value as StatusFiltro)}
              disabled={loading}
            >
              {STATUS_LIST.map(s => (
                <option key={s.key} value={s.key}>{s.label}</option>
              ))}
            </select>
          </div>
          <div style={{ marginLeft: 'auto', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            {items.length} etiqueta(s) • {dateLabel}
          </div>
        </div>
      </div>

      {/* Container que sobrevive ao @media print. */}
      <div id="sz-etiq-print">
        {loading ? (
          <div className="szv2-card" style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        ) : items.length === 0 ? (
          <div className="szv2-card" style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Nenhum pedido {STATUS_LIST.find(s => s.key === status)?.label.toLowerCase() ?? status} em {dateLabel}.
          </div>
        ) : (
          <div className="sz-etiq-grid">
            {items.map(e => (
              <div key={e.pedido_id} className="sz-etiq">
                <div className="sz-etiq-header">
                  <div className="sz-etiq-num">#{e.wc_order_id}</div>
                  <div className="sz-etiq-mb">
                    {e.motoboy_nome || '—'}
                    {e.zona_nome ? ` · ${e.zona_nome}` : ''}
                  </div>
                </div>
                <div className="sz-etiq-dest">{e.dest_nome || '—'}</div>
                <div className="sz-etiq-addr">
                  {e.dest_endereco}
                  {e.dest_numero ? `, ${e.dest_numero}` : ''}
                  {e.dest_complemento ? ` — ${e.dest_complemento}` : ''}
                  <br />
                  {e.dest_bairro}
                  {e.dest_bairro && (e.dest_cidade || e.dest_uf) ? ' · ' : ''}
                  {e.dest_cidade}
                  {e.dest_cidade && e.dest_uf ? '/' : ''}
                  {e.dest_uf}
                </div>
                <div className="sz-etiq-footer">
                  <div>
                    <div className="sz-etiq-val">R$ {fmtMoney(e.valor_pedido)}</div>
                    <div className="sz-etiq-pgto">{pgtoString(e)}</div>
                  </div>
                  {e.dest_telefone && (
                    <div className="sz-etiq-tel">{e.dest_telefone}</div>
                  )}
                </div>
                <div className="sz-etiq-qr-wrap">
                  <img src={qrUrl(e.package_code)} alt="QR Code do pacote" />
                  <div>
                    <strong>QR ROTA / DEVOLUÇÃO</strong>
                    <div className="sz-etiq-qr-code">{e.package_code}</div>
                  </div>
                </div>
                {e.dest_cep && (
                  <div className="sz-etiq-cep">CEP {formatCep(e.dest_cep)}</div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
