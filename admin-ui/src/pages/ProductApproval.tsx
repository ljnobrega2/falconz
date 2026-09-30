// Tela de APROVAÇÃO DE PRODUTO (#20).
//
// Consome go/admin product_approval.go:
//   GET  /products/approval-queue          → produtos 'a_aprovar' (todas as infos)
//   POST /products/{id}/approve            → 'aprovado'
//   POST /products/{id}/reject             → 'reprovado'
//
// O drawer lateral (DetailDrawer) mostra TODAS as informações inseridas pelo
// produtor: nome, sku, barcode, categoria, descrição, dimensões físicas (altura/largura/
// comprimento/peso) e a imagem/rótulo (best-effort de sz_products.meta).
//
// Reprovar NÃO tem corpo/motivo (o backend é uma transição de status pura — não
// há coluna de notas em sz_products) — por isso a UI usa confirmação, não um
// modal de motivo obrigatório.
import { useEffect, useState } from 'react'
import { useToast } from '../hooks/useToast'
import { api } from '../api'
import DetailDrawer from '../components/DetailDrawer'
import StatusBadge from '../components/StatusBadge'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'
import { confirmAsync } from '../components/ConfirmDialog'

type PendingProduct = {
  id: number
  wp_post_id: number | null
  produtor_id: number
  produtor_nome: string
  nome: string
  sku: string | null
  barcode: string | null
  categoria: string | null
  descricao: string | null
  altura: number | null
  largura: number | null
  comprimento: number | null
  peso: number | null
  imagem_url: string | null
  status: string
  created_at: string
}

function fmtDate(raw: string | null): string {
  if (!raw) return '—'
  const d = raw.slice(0, 10)
  const [y, m, day] = d.split('-')
  return y && m && day ? `${day}/${m}/${y}` : raw
}

// dimensão em cm/kg — mostra "—" quando NULL (produtor não informou).
function fmtDim(v: number | null, unit: string): string {
  if (v == null) return '—'
  return `${v} ${unit}`
}

export default function ProductApproval() {
  const [items, setItems] = useState<PendingProduct[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState('')
  const [detail, setDetail] = useState<PendingProduct | null>(null)
  const showToast = useToast()

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const p = new URLSearchParams()
      if (q.trim()) p.set('q', q.trim())
      p.set('limit', '200')
      const r = await api<{ items: PendingProduct[]; total: number }>(
        `/products/approval-queue?${p.toString()}`,
      )
      setItems(r.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load() /* eslint-disable-next-line react-hooks/exhaustive-deps */
  }, [q])

  // Aprova ou reprova um produto. Confirmação via confirmAsync (sem motivo — a
  // transição de status no backend é pura, sem coluna de notas em sz_products).
  //
  // IMPORTANTE: o overlay do confirmAsync usa z-index var(--szv2-z-modal)=900,
  // ABAIXO do DetailDrawer (overlay 1000 / painel 1001). Se o drawer estiver
  // aberto quando o confirm abre, ele renderiza ATRÁS do drawer (invisível e
  // inclicável). Por isso fechamos o drawer ANTES de pedir a confirmação quando
  // a ação parte do rodapé do drawer — assim o confirm fica sempre visível.
  async function decide(product: PendingProduct, kind: 'approve' | 'reject') {
    if (detail?.id === product.id) setDetail(null)
    const ok = await confirmAsync({
      title: kind === 'approve' ? 'Aprovar produto?' : 'Reprovar produto?',
      message:
        kind === 'approve'
          ? `O produto "${product.nome}" será aprovado e ficará disponível.`
          : `O produto "${product.nome}" será reprovado e sairá da fila.`,
      confirmLabel: kind === 'approve' ? 'Aprovar' : 'Reprovar',
      danger: kind === 'reject',
    })
    if (!ok) return
    setBusy(true)
    try {
      await api(`/products/${product.id}/${kind}`, { method: 'POST' })
      showToast('ok', kind === 'approve' ? 'Produto aprovado.' : 'Produto reprovado.')
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha na operação')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Aprovação de Produtos</h1>
          <p>
            {items.length} produto(s) aguardando aprovação
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <input
            type="search"
            className="szv2-input"
            placeholder="Buscar por nome…"
            value={q}
            onChange={e => setQ(e.target.value)}
            style={{ minWidth: 220 }}
          />
        </div>
      </div>

      {err && items.length > 0 && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>
      )}

      {loading && items.length === 0 ? (
        <TableSkeleton rows={5} cols={6} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={load} />
      ) : !loading && items.length === 0 ? (
        <EmptyState
          icon="📦"
          title="Nenhum produto na fila de aprovação."
          description="Produtos novos cadastrados pelos produtores aparecem aqui para revisão."
        />
      ) : (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Produto</th>
                <th>SKU</th>
                <th>Código de barras</th>
                <th>Categoria</th>
                <th>Produtor</th>
                <th>Enviado em</th>
                <th style={{ textAlign: 'right' }}>Ações</th>
              </tr>
            </thead>
            <tbody>
              {items.map(p => (
                <tr key={p.id}>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>#{p.id}</td>
                  <td style={{ fontWeight: 600 }}>{p.nome}</td>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>{p.sku || '—'}</td>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>{p.barcode || '—'}</td>
                  <td style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>{p.categoria || '—'}</td>
                  <td style={{ fontSize: 13 }}>{p.produtor_nome || `#${p.produtor_id}`}</td>
                  <td style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>{fmtDate(p.created_at)}</td>
                  <td style={{ textAlign: 'right' }}>
                    <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                        onClick={() => setDetail(p)}
                      >
                        Detalhes
                      </button>
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-brand"
                        disabled={busy}
                        onClick={() => decide(p, 'approve')}
                      >
                        Aprovar
                      </button>
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-danger"
                        disabled={busy}
                        onClick={() => decide(p, 'reject')}
                      >
                        Reprovar
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Drawer lateral de detalhe — TODAS as infos inseridas pelo produtor */}
      <DetailDrawer
        open={!!detail}
        onClose={() => setDetail(null)}
        large
        title={detail ? `Produto #${detail.id}` : ''}
        footer={
          detail ? (
            <>
              <button
                className="szv2-btn szv2-btn-danger"
                disabled={busy}
                onClick={() => decide(detail, 'reject')}
              >
                Reprovar
              </button>
              <button
                className="szv2-btn szv2-btn-brand"
                disabled={busy}
                onClick={() => decide(detail, 'approve')}
              >
                Aprovar
              </button>
            </>
          ) : null
        }
      >
        {detail && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
              <StatusBadge status={detail.status} />
              <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                enviado em {fmtDate(detail.created_at)}
              </span>
            </div>

            {detail.imagem_url ? (
              <img
                src={detail.imagem_url}
                alt={detail.nome}
                style={{
                  width: '100%',
                  maxHeight: 240,
                  objectFit: 'contain',
                  borderRadius: 10,
                  border: '1px solid var(--szv2-divider)',
                  background: 'var(--szv2-surface-alt)',
                }}
              />
            ) : (
              <div
                style={{
                  padding: '20px',
                  borderRadius: 10,
                  border: '1px dashed var(--szv2-border)',
                  background: 'var(--szv2-surface-alt)',
                  color: 'var(--szv2-text-muted)',
                  fontSize: 13,
                  textAlign: 'center',
                }}
              >
                Sem imagem/rótulo enviado.
              </div>
            )}

            <Field label="Nome">
              <span style={{ fontWeight: 600 }}>{detail.nome}</span>
            </Field>
            <Field label="SKU">
              <span style={{ fontFamily: 'var(--szv2-font-mono)' }}>{detail.sku || '—'}</span>
            </Field>
            <Field label="Código de barras">
              <span style={{ fontFamily: 'var(--szv2-font-mono)' }}>{detail.barcode || '—'}</span>
            </Field>
            <Field label="Categoria">{detail.categoria || '—'}</Field>
            <Field label="Produtor">
              {detail.produtor_nome || '—'}{' '}
              <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                (#{detail.produtor_id})
              </span>
            </Field>

            <Field label="Descrição">
              <span style={{ whiteSpace: 'pre-wrap', fontSize: 13 }}>
                {detail.descricao || '—'}
              </span>
            </Field>

            <div>
              <div
                style={{
                  fontSize: 11,
                  fontWeight: 700,
                  textTransform: 'uppercase',
                  letterSpacing: '.04em',
                  color: 'var(--szv2-text-muted)',
                  marginBottom: 8,
                }}
              >
                Dimensões físicas
              </div>
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(2, 1fr)',
                  gap: 10,
                }}
              >
                <MiniStat label="Altura" value={fmtDim(detail.altura, 'cm')} />
                <MiniStat label="Largura" value={fmtDim(detail.largura, 'cm')} />
                <MiniStat label="Comprimento" value={fmtDim(detail.comprimento, 'cm')} />
                <MiniStat label="Peso" value={fmtDim(detail.peso, 'kg')} />
              </div>
            </div>
          </div>
        )}
      </DetailDrawer>
    </div>
  )
}

// ── Helpers de apresentação ──────────────────────────────────────────────────

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div
        style={{
          fontSize: 11,
          fontWeight: 700,
          textTransform: 'uppercase',
          letterSpacing: '.04em',
          color: 'var(--szv2-text-muted)',
          marginBottom: 4,
        }}
      >
        {label}
      </div>
      <div style={{ color: 'var(--szv2-text)' }}>{children}</div>
    </div>
  )
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div
      style={{
        padding: '10px 12px',
        borderRadius: 10,
        border: '1px solid var(--szv2-divider)',
        background: 'var(--szv2-surface-alt)',
      }}
    >
      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>{label}</div>
      <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>{value}</div>
    </div>
  )
}
