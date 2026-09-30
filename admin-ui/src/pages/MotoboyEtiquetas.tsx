// MotoboyEtiquetas — listagem de pedidos motoboy formatados como etiquetas
// para impressão. Espelha sz_mb_tab_etiquetas() (admin.php:1383, §2.5).
//
// Mudanças 2026-06-18 (pedido do dono):
//   • Sem "A cobrar"; sem +55 no telefone; mostra qtd+nome do produto.
//   • Sem zona/cidade no topo direito; sem label "QR ROTA / DEVOLUÇÃO".
//   • Impressão INDIVIDUAL por etiqueta (além de imprimir todas).
//   • Filtro por DATA DE ENTREGA (não data do pedido).

import { useEffect, useState } from 'react'
import { api } from '../api'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'

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
  produto_label: string // "2x Nome do Produto" (qtd + nome, uma vez só)
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

// Data "hoje" no fuso America/Sao_Paulo (UTC-3). Usar toISOString() devolve UTC
// e, das ~21h às 23h59 BRT, retorna o dia seguinte — fazendo a etiqueta cair na
// data de entrega errada e parecer vazia. Intl/en-CA garante o dia local correto.
const todayISO = () => {
  const fmt = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'America/Sao_Paulo',
    year: 'numeric', month: '2-digit', day: '2-digit',
  })
  return fmt.format(new Date()) // en-CA → "YYYY-MM-DD"
}

// CEP "12345678" → "12345-678" (paridade PHP). Tolera CEP curto/longo.
function formatCep(cep: string): string {
  const d = (cep || '').replace(/\D+/g, '')
  if (!d) return ''
  if (d.length <= 5) return d
  return d.slice(0, 5) + '-' + d.slice(5, 8)
}

// Telefone SEM +55 (paridade $sz4mb_fmt_phone): remove DDI 55 quando 12-13 dígitos.
function fmtPhone(tel: string): string {
  let d = (tel || '').replace(/\D+/g, '')
  if ((d.length === 12 || d.length === 13) && d.startsWith('55')) d = d.slice(2)
  if (d.length === 11) return `(${d.slice(0, 2)}) ${d.slice(2, 7)}-${d.slice(7)}`
  if (d.length === 10) return `(${d.slice(0, 2)}) ${d.slice(2, 6)}-${d.slice(6)}`
  return d
}

// Formas de pagamento com valor > 0. SEM "A cobrar" (vazio quando nada).
function pgtoString(e: Etiqueta): string {
  const parts: string[] = []
  if (e.pgto_dinheiro > 0) parts.push(`Dinheiro R$ ${fmtMoney(e.pgto_dinheiro)}`)
  if (e.pgto_pix > 0) parts.push(`PIX R$ ${fmtMoney(e.pgto_pix)}`)
  if (e.pgto_cartao > 0) parts.push(`Cartão R$ ${fmtMoney(e.pgto_cartao)}`)
  return parts.join(' + ')
}

function qrUrl(code: string): string {
  return `https://api.qrserver.com/v1/create-qr-code/?size=160x160&data=${encodeURIComponent(code)}`
}

const STATUS_LIST: { key: StatusFiltro; label: string }[] = [
  { key: 'agendado', label: 'Agendados' },
  { key: 'embalado', label: 'Embalados' },
  { key: 'em_rota', label: 'Em rota' },
]

// ─── Página ───────────────────────────────────────────────────────────────

export default function MotoboyEtiquetas() {
  const [date, setDate] = useState<string>(todayISO())
  const [status, setStatus] = useState<StatusFiltro>('agendado')

  const [items, setItems] = useState<Etiqueta[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Impressão individual: quando setado, só essa etiqueta aparece no @media print.
  const [soloPrint, setSoloPrint] = useState<number | null>(null)

  const [draftDate, setDraftDate] = useState<string>(date)
  const [draftStatus, setDraftStatus] = useState<StatusFiltro>(status)
  const [filterOpen, setFilterOpen] = useState(false)

  function openPanel() {
    setDraftDate(date); setDraftStatus(status)
    setFilterOpen(true)
  }
  function applyFilters() {
    if (draftDate) setDate(draftDate)
    setStatus(draftStatus)
    setFilterOpen(false)
  }
  function clearFilters() {
    const t = todayISO()
    setDate(t); setStatus('agendado')
    setDraftDate(t); setDraftStatus('agendado')
    setFilterOpen(false)
  }

  function printAll() {
    setSoloPrint(null)
    setTimeout(() => window.print(), 30)
  }
  function printOne(pedidoId: number) {
    setSoloPrint(pedidoId)
    setTimeout(() => {
      window.print()
      setTimeout(() => setSoloPrint(null), 400)
    }, 30)
  }

  const today = todayISO()
  const chips: ActiveChip[] = []
  if (date !== today) chips.push({ key: 'date', label: `Entrega: ${date}`, onRemove: () => setDate(today) })
  if (status !== 'agendado') {
    const lbl = STATUS_LIST.find(s => s.key === status)?.label ?? status
    chips.push({ key: 'status', label: `Status: ${lbl}`, onRemove: () => setStatus('agendado') })
  }
  const activeCount = chips.length

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

  const dateLabel = (() => {
    const [y, m, d] = date.split('-')
    return y && m && d ? `${d}/${m}/${y}` : date
  })()

  return (
    <div>
      {/* Banner só com dados na tela (erro de refresh/ação). Falha de
          carregamento inicial vira ErrorState na área das etiquetas. */}
      {err && items.length > 0 && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

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
        .sz-etiq-num { font-size: 18px; font-weight: 700; color: #1E6FF2; }
        .sz-etiq-mb {
          font-size: 12px; background: #f3f4f6; padding: 3px 8px;
          border-radius: 6px; color: #374151; text-align: right;
        }
        .sz-etiq-print-one {
          font-size: 11px; border: 1px solid #d1d5db; background: #fff;
          border-radius: 6px; padding: 2px 8px; cursor: pointer; color: #374151;
        }
        .sz-etiq-print-one:hover { border-color: #1E6FF2; color: #1E6FF2; }
        .sz-etiq-dest { font-size: 14px; font-weight: 700; margin-bottom: 2px; color: #111827; }
        .sz-etiq-prod { font-size: 13px; font-weight: 700; color: #1E6FF2; margin-bottom: 6px; }
        .sz-etiq-addr { color: #374151; margin-bottom: 8px; line-height: 1.4; font-size: 13px; }
        .sz-etiq-footer {
          display: flex; justify-content: space-between; gap: 8px;
          border-top: 1px dashed #d1d5db; padding-top: 8px; margin-top: 6px;
        }
        .sz-etiq-val { font-weight: 700; font-size: 14px; color: #111827; }
        .sz-etiq-pgto { font-size: 12px; color: #6b7280; margin-top: 2px; }
        .sz-etiq-tel { font-size: 12px; color: #6b7280; align-self: flex-end; }
        .sz-etiq-cep { position: absolute; bottom: 10px; right: 14px; font-size: 11px; color: #9ca3af; }
        .sz-etiq-qr-wrap {
          display: flex; align-items: center; gap: 10px; margin-top: 10px;
          border-top: 1px dashed #d1d5db; padding-top: 8px;
        }
        .sz-etiq-qr-wrap img { width: 64px; height: 64px; image-rendering: pixelated; }
        .sz-etiq-qr-code { font-size: 10px; line-height: 1.35; word-break: break-all; color: #111827; }

        @media print {
          body * { visibility: hidden !important; }
          #sz-etiq-print, #sz-etiq-print * { visibility: visible !important; }
          #sz-etiq-print {
            position: fixed; top: 0; left: 0; right: 0; width: 100%; padding: 0; background: #fff;
          }
          .sz-etiq { border: 1.5px solid #000 !important; page-break-inside: avoid; }
          .sz-etiq-print-one { display: none !important; }
          .sz-etiq-hide-print { display: none !important; }
        }
      `}</style>

      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head" style={{ flexWrap: 'wrap', gap: 12 }}>
          <div>
            <h2>Etiquetas Motoboy</h2>
            <p className="szv2-card-sub">
              {items.length} etiqueta(s) • entrega {dateLabel} — QR validado pelo PWA ao iniciar rota.
            </p>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
            <button
              type="button"
              className="szv2-btn szv2-btn-brand"
              onClick={printAll}
              disabled={loading || items.length === 0}
            >
              🖨️ Imprimir todas
            </button>
          </div>
        </div>
        <ActiveFilterChips chips={chips} onClearAll={clearFilters} />
      </div>

      <div id="sz-etiq-print">
        {loading && items.length === 0 ? (
          <TableSkeleton rows={4} cols={5} />
        ) : err && items.length === 0 ? (
          <ErrorState message={err} onRetry={load} />
        ) : !loading && items.length === 0 ? (
          <EmptyState
            icon="🏷️"
            title={`Nenhum pedido ${STATUS_LIST.find(s => s.key === status)?.label.toLowerCase() ?? status} com entrega em ${dateLabel}.`}
            description="Selecione outro dia de entrega ou outro status."
          />
        ) : (
          <div className="sz-etiq-grid">
            {items.map((e) => (
              <div
                key={e.pedido_id}
                className={'sz-etiq' + (soloPrint !== null && soloPrint !== e.pedido_id ? ' sz-etiq-hide-print' : '')}
              >
                <div className="sz-etiq-header">
                  <div className="sz-etiq-num">{e.wc_order_id}</div>
                  <button
                    type="button"
                    className="sz-etiq-print-one"
                    onClick={() => printOne(e.pedido_id)}
                    title="Imprimir só esta etiqueta"
                  >
                    🖨️ Imprimir
                  </button>
                </div>
                <div className="sz-etiq-dest">{e.dest_nome || '—'}</div>
                {e.produto_label && <div className="sz-etiq-prod">{e.produto_label}</div>}
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
                    {pgtoString(e) && <div className="sz-etiq-pgto">{pgtoString(e)}</div>}
                  </div>
                  {e.dest_telefone && (
                    <div className="sz-etiq-tel">{fmtPhone(e.dest_telefone)}</div>
                  )}
                </div>
                <div className="sz-etiq-qr-wrap">
                  <img src={qrUrl(e.package_code)} alt="QR Code do pacote" />
                  <div className="sz-etiq-qr-code">{e.package_code}</div>
                </div>
                {e.dest_cep && (
                  <div className="sz-etiq-cep">CEP {formatCep(e.dest_cep)}</div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Data de entrega">
          <FalkDatePicker
            value={draftDate}
            onChange={v => setDraftDate(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Status">
          <FalkSelect
            value={draftStatus}
            onChange={v => setDraftStatus(v as StatusFiltro)}
            options={STATUS_LIST.map(s => ({ value: s.key, label: s.label }))}
            aria-label="Status"
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
