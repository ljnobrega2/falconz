import { useEffect, useState } from 'react'
import { api } from '../api'
import { brl, brDate } from '../utils/format'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'
import DetailDrawer from '../components/DetailDrawer'
import { confirmAsync } from '../components/ConfirmDialog'
import { drawerTabsStyle, drawerTabBtnStyle } from '../components/drawerTabs'
import { emitToast } from '../hooks/useToast'

// ---------------------------------------------------------------------------
// Tipos espelhados do handler Go de produtores.
// GET /producers → { items, total }
// GET /producers/{user_id}/detail → ProducerDetail
// ---------------------------------------------------------------------------

type Producer = {
  user_id: number
  nome: string
  email: string
  telefone: string
  cpf: string
  pix_key: string
  status: string
  created_at: string
  // SHAPE: handler Go envia `valor_vendido_total` (não `valor_vendido`).
  valor_vendido_total: number
  comissao_pendente_afiliados: number
  afiliados_count: number
  produtos_count: number
  // Flag de expedição por produtor (settings->>'expedicao_ativa'). Default true.
  expedicao_ativa: boolean
}

type VinculoAfiliado = {
  afiliado_nome: string
  // SHAPE: handler de detalhe envia `produto_nome` (string|null) + `produto_id`.
  produto_nome: string | null
  produto_id?: number
  comissao_pct: number
  status?: string
}

type ContaCadastrada = {
  // SHAPE: handler Go (producerConta) envia `nome` (não `holder_name`),
  // sem `id` nem `holder_cpf`. Ver go/admin/.../producers.go.
  nome: string
  pix_type: string
  pix_key: string
  is_default: boolean
}

type ProducerDetail = {
  user_id: number
  nome: string
  email: string
  telefone: string
  cpf: string
  pix_key: string
  pix_tipo?: string
  // Flag de expedição por produtor — espelha a List (settings->>'expedicao_ativa').
  expedicao_ativa: boolean
  // Frete fixo por transportadora (settings->>'frete_fixo_*'). null = sem override
  // (usa markup normal pct/fixed do produto).
  frete_fixo_correios: number | null
  frete_fixo_outras: number | null
  bloqueio_correios: boolean
  link_misto_ativo: boolean
  // SHAPE: handler envia `afiliados` (não `vinculos`).
  afiliados: VinculoAfiliado[]
  contas: ContaCadastrada[]
}

// ---------------------------------------------------------------------------
// Helpers de status (mesma estética das demais telas do admin).
// ---------------------------------------------------------------------------

function statusBadgeClass(status: string): string {
  if (status === 'active' || status === 'ativo' || status === 'confirmado') return 's-confirmado'
  if (status === 'pending' || status === 'pendente' || status === 'aguardando') return 's-pendente'
  return 's-cancelado'
}

function statusLabel(status: string): string {
  if (status === 'active' || status === 'ativo' || status === 'confirmado') return 'Ativo'
  if (status === 'pending' || status === 'pendente' || status === 'aguardando') return 'Pendente'
  if (status === 'inactive' || status === 'inativo') return 'Inativo'
  return status || '—'
}

const PIX_TYPE_BADGE: Record<string, string> = {
  cpf:      'szv2-badge-info',
  cnpj:     'szv2-badge-info',
  email:    'szv2-badge-neutral',
  telefone: 'szv2-badge-neutral',
  phone:    'szv2-badge-neutral',
  aleatoria: 'szv2-badge-brand',
  evp:      'szv2-badge-brand',
}

// dash — exibe valor ou travessão cinza quando vazio.
function Dash({ value }: { value: string }) {
  return value
    ? <>{value}</>
    : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
}

// ---------------------------------------------------------------------------
// Drawer lateral: detalhe de um produtor (afiliados vinculados, dados, contas).
// ---------------------------------------------------------------------------

function ProducerDrawer({
  producer, onClose, onDeleted, onSaved,
}: {
  producer: Producer
  onClose: () => void
  onDeleted: () => void
  onSaved: () => void
}) {
  const [detail, setDetail] = useState<ProducerDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [tab, setTab] = useState<'resumo' | 'afiliados' | 'contas'>('resumo')

  // ── Edição administrativa: CPF (document) + telefone (phone) manuais (#84) e
  //    flag de expedição por produtor (M). PATCH /producers/{id}.
  const [editCpf, setEditCpf] = useState('')
  const [editPhone, setEditPhone] = useState('')
  const [editExpedicao, setEditExpedicao] = useState(true)
  // Frete fixo por transportadora, por produtor (pedido dono 2026-07-27).
  // string no input (permite campo vazio = sem override); convertido no save.
  const [editFreteCorreios, setEditFreteCorreios] = useState('')
  const [editFreteOutras, setEditFreteOutras] = useState('')
  const [editBloqueioCorreios, setEditBloqueioCorreios] = useState(false)
  const [editLinkMisto, setEditLinkMisto] = useState(false)
  const [saving, setSaving] = useState(false)
  const [savedOk, setSavedOk] = useState(false)

  // Salvar dados administrativos do produtor (CPF/telefone/expedição/frete fixo).
  async function handleSave() {
    setSaving(true); setErr(''); setSavedOk(false)
    try {
      const correios = editFreteCorreios.trim() === '' ? null : parseFloat(editFreteCorreios.replace(',', '.'))
      const outras = editFreteOutras.trim() === '' ? null : parseFloat(editFreteOutras.replace(',', '.'))
      await api(`/producers/${producer.user_id}`, {
        method: 'PUT',
        body: JSON.stringify({
          document: editCpf.trim(),
          phone: editPhone.trim(),
          expedicao_ativa: editExpedicao,
          frete_fixo_correios: correios,
          frete_fixo_outras: outras,
          bloqueio_correios: editBloqueioCorreios,
          link_misto_ativo: editLinkMisto,
        }),
      })
      // Reflete localmente no detalhe carregado (read-after-write consistente).
      setDetail(d => d ? {
        ...d,
        cpf: editCpf.trim(),
        telefone: editPhone.trim(),
        expedicao_ativa: editExpedicao,
        frete_fixo_correios: correios,
        frete_fixo_outras: outras,
        bloqueio_correios: editBloqueioCorreios,
        link_misto_ativo: editLinkMisto,
      } : d)
      setSavedOk(true)
      emitToast('ok', 'Dados do produtor salvos.')
      onSaved()
    } catch (e: any) {
      setErr(e.message || 'Erro ao salvar dados do produtor')
      emitToast('err', e.message || 'Erro ao salvar dados do produtor')
    } finally {
      setSaving(false)
    }
  }

  // Excluir produtor = SOFT-DELETE no backend (ativo=false): some da listagem,
  // perde acesso, mas histórico de pedidos/comissões fica intacto e é reversível.
  async function handleDelete() {
    const ok = await confirmAsync({
      variant: 'danger',
      title: 'Excluir produtor',
      message: `Excluir o produtor "${producer.nome || producer.email || `#${producer.user_id}`}"? Ele sai da listagem e perde o acesso. O histórico de pedidos e comissões é preservado — a ação pode ser revertida.`,
      confirmLabel: 'Excluir',
    })
    if (!ok) return
    setDeleting(true)
    try {
      await api(`/producers/${producer.user_id}`, { method: 'DELETE' })
      emitToast('ok', 'Produtor excluído.')
      onDeleted()
    } catch (e: any) {
      setErr(e.message || 'Erro ao excluir produtor')
      emitToast('err', e.message || 'Erro ao excluir produtor')
      setDeleting(false)
    }
  }

  useEffect(() => {
    let active = true
    setLoading(true); setErr(''); setSavedOk(false)
    api<ProducerDetail>(`/producers/${producer.user_id}/detail`)
      .then(r => {
        if (!active) return
        setDetail(r)
        // Seed dos campos editáveis com o que veio do detalhe (fallback p/ a linha da listagem).
        setEditCpf(r.cpf ?? producer.cpf ?? '')
        setEditPhone(r.telefone ?? producer.telefone ?? '')
        setEditExpedicao(r.expedicao_ativa ?? producer.expedicao_ativa ?? true)
        setEditFreteCorreios(r.frete_fixo_correios != null ? String(r.frete_fixo_correios) : '')
        setEditFreteOutras(r.frete_fixo_outras != null ? String(r.frete_fixo_outras) : '')
        setEditBloqueioCorreios(r.bloqueio_correios ?? false)
        setEditLinkMisto(r.link_misto_ativo ?? false)
      })
      .catch(e => { if (active) setErr(e.message || 'Erro ao carregar detalhes') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [producer.user_id, producer.cpf, producer.telefone, producer.expedicao_ativa])

  const vinculos = detail?.afiliados ?? []
  const contas = detail?.contas ?? []

  return (
    <DetailDrawer
      open
      onClose={onClose}
      width={620}
      title={`Produtor — ${producer.nome || producer.email || `#${producer.user_id}`}`}
      footer={
        <>
          <button
            type="button"
            className="szv2-btn szv2-btn-danger"
            disabled={deleting}
            onClick={handleDelete}
            style={{ marginRight: 'auto' }}
          >
            {deleting ? 'Excluindo…' : 'Excluir produtor'}
          </button>
          <button type="button" className="szv2-btn szv2-btn-brand" onClick={onClose}>
            Fechar
          </button>
        </>
      }
    >
      {/* Abas */}
      <div style={drawerTabsStyle}>
        {([
          ['resumo', 'Resumo'],
          ['afiliados', `Afiliados (${producer.afiliados_count})`],
          ['contas', 'Contas'],
        ] as const).map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            style={drawerTabBtnStyle(tab === key)}
          >
            {label}
          </button>
        ))}
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {loading ? (
        <div style={{ padding: 40, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando…
        </div>
      ) : (
        <>
          {/* ── Aba: Resumo (KPIs + dados) ───────────────────── */}
          {tab === 'resumo' && (
            <>
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(2, minmax(0,1fr))',
                  gap: 12,
                }}
              >
                <div style={{ padding: 12, background: 'var(--szv2-brand-light)', borderRadius: 8 }}>
                  <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Valor vendido</div>
                  <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-brand)' }}>
                    {brl(producer.valor_vendido_total)}
                  </div>
                </div>
                <div style={{ padding: 12, background: 'var(--szv2-warning-bg)', borderRadius: 8 }}>
                  <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Comissão pendente</div>
                  <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-warning)' }}>
                    {brl(producer.comissao_pendente_afiliados)}
                  </div>
                </div>
                <div style={{ padding: 12, background: 'var(--szv2-info-bg)', borderRadius: 8 }}>
                  <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Afiliados</div>
                  <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-info)' }}>
                    {producer.afiliados_count}
                  </div>
                </div>
                <div style={{ padding: 12, background: 'var(--szv2-success-bg)', borderRadius: 8 }}>
                  <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>Produtos</div>
                  <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--szv2-success)' }}>
                    {producer.produtos_count}
                  </div>
                </div>
              </div>

              {/* Dados de contato editáveis (#84) — CPF e telefone manuais. Sem
                  esses campos o saque do produtor mostra "CPF do cadastro
                  indisponível". PIX permanece somente-leitura (vem das contas). */}
              <div
                style={{
                  marginTop: 16,
                  padding: 14,
                  background: 'var(--szv2-bg-soft, #f8fafc)',
                  borderRadius: 8,
                  border: '1px solid var(--szv2-border, #e5e7eb)',
                }}
              >
                <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12, color: 'var(--szv2-text-soft)' }}>
                  Dados do produtor
                </div>
                <div
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(2, minmax(0,1fr))',
                    gap: 12,
                  }}
                >
                  <label style={{ display: 'block' }}>
                    <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 4 }}>CPF</div>
                    <input
                      type="text"
                      className="szv2-input"
                      style={{ width: '100%', fontFamily: 'var(--szv2-font-mono)', fontSize: 13 }}
                      placeholder="000.000.000-00"
                      value={editCpf}
                      onChange={e => { setEditCpf(e.target.value); setSavedOk(false) }}
                    />
                  </label>
                  <label style={{ display: 'block' }}>
                    <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 4 }}>Telefone</div>
                    <input
                      type="text"
                      className="szv2-input"
                      style={{ width: '100%', fontFamily: 'var(--szv2-font-mono)', fontSize: 13 }}
                      placeholder="(00) 00000-0000"
                      value={editPhone}
                      onChange={e => { setEditPhone(e.target.value); setSavedOk(false) }}
                    />
                  </label>
                </div>

                <div style={{ marginTop: 12 }}>
                  <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 4 }}>PIX (somente leitura)</div>
                  <div style={{ fontSize: 13, fontFamily: 'var(--szv2-font-mono)' }}>
                    <Dash value={detail?.pix_key ?? producer.pix_key ?? ''} />
                  </div>
                </div>

                {/* Toggle de expedição por produtor (M) — settings->>'expedicao_ativa'. */}
                <label
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    marginTop: 16,
                    cursor: 'pointer',
                  }}
                >
                  <input
                    type="checkbox"
                    checked={editExpedicao}
                    onChange={e => { setEditExpedicao(e.target.checked); setSavedOk(false) }}
                  />
                  <span style={{ fontSize: 13, fontWeight: 600 }}>Expedição ativa</span>
                  <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                    {editExpedicao
                      ? 'O produtor pode expedir pedidos.'
                      : 'Expedição desabilitada para este produtor.'}
                  </span>
                </label>

                {/* Bloqueia Correios e trava a escolha na mais barata entre as
                    transportadoras privadas — cliente não escolhe. */}
                <label
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    marginTop: 16,
                    cursor: 'pointer',
                  }}
                >
                  <input
                    type="checkbox"
                    checked={editBloqueioCorreios}
                    onChange={e => { setEditBloqueioCorreios(e.target.checked); setSavedOk(false) }}
                  />
                  <span style={{ fontSize: 13, fontWeight: 600 }}>Bloquear Correios (só transportadora privada)</span>
                  <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                    {editBloqueioCorreios
                      ? 'Correios removido; sistema trava na mais barata das demais, cliente não escolhe.'
                      : 'Todas as transportadoras habilitadas aparecem normalmente.'}
                  </span>
                </label>

                {/* Link misto: 1 link só decide COD ou Expedição pelo CEP no checkout,
                    sem o cliente escolher. Só faz sentido com Expedição ativa. */}
                <label
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    marginTop: 16,
                    cursor: 'pointer',
                  }}
                >
                  <input
                    type="checkbox"
                    checked={editLinkMisto}
                    onChange={e => { setEditLinkMisto(e.target.checked); setSavedOk(false) }}
                  />
                  <span style={{ fontSize: 13, fontWeight: 600 }}>Link misto (Expedição + COD automático)</span>
                  <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                    {editLinkMisto
                      ? 'Novas ofertas geram 1 link só; o sistema decide pelo CEP do cliente.'
                      : 'Novas ofertas geram 2 links (Expedição + Cash on Delivery separados).'}
                  </span>
                </label>

                {/* Frete fixo por transportadora (por produtor) — substitui o markup
                    normal quando preenchido. Vazio = sem override. */}
                <div style={{ marginTop: 16 }}>
                  <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Frete fixo (por transportadora)</div>
                  <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 8 }}>
                    Preço absoluto pra este produtor, aplicado no checkout e na cobrança da carteira. Vazio = usa o markup normal.
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0,1fr))', gap: 12 }}>
                    <label style={{ display: 'block' }}>
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 4 }}>Correios (R$)</div>
                      <input
                        type="text"
                        inputMode="decimal"
                        className="szv2-input"
                        style={{ width: '100%', fontFamily: 'var(--szv2-font-mono)', fontSize: 13 }}
                        placeholder="ex: 25,00"
                        value={editFreteCorreios}
                        onChange={e => { setEditFreteCorreios(e.target.value); setSavedOk(false) }}
                      />
                    </label>
                    <label style={{ display: 'block' }}>
                      <div style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 4 }}>Demais transportadoras (R$)</div>
                      <input
                        type="text"
                        inputMode="decimal"
                        className="szv2-input"
                        style={{ width: '100%', fontFamily: 'var(--szv2-font-mono)', fontSize: 13 }}
                        placeholder="ex: 32,00"
                        value={editFreteOutras}
                        onChange={e => { setEditFreteOutras(e.target.value); setSavedOk(false) }}
                      />
                    </label>
                  </div>
                </div>

                <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 16 }}>
                  <button
                    type="button"
                    className="szv2-btn szv2-btn-brand"
                    disabled={saving}
                    onClick={handleSave}
                  >
                    {saving ? 'Salvando…' : 'Salvar dados'}
                  </button>
                  {savedOk && (
                    <span style={{ fontSize: 12, color: 'var(--szv2-success, #16a34a)', fontWeight: 600 }}>
                      Salvo ✓
                    </span>
                  )}
                </div>
              </div>
            </>
          )}

          {/* ── Aba: Afiliados vinculados ────────────────────── */}
          {tab === 'afiliados' && (
            vinculos.length === 0 ? (
              <div className="szv2-empty">
                <h3>Nenhum afiliado vinculado</h3>
                <p>Este produtor ainda não tem afiliados ativos.</p>
              </div>
            ) : (
              <div style={{ overflowX: 'auto' }}>
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Afiliado</th>
                      <th>Produto</th>
                      <th className="szv2-td-num">Comissão</th>
                    </tr>
                  </thead>
                  <tbody>
                    {vinculos.map((v, i) => (
                      <tr key={`${v.afiliado_nome}-${v.produto_nome ?? ''}-${i}`}>
                        <td style={{ fontWeight: 600 }}>
                          <Dash value={v.afiliado_nome} />
                        </td>
                        <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)' }}>
                          <Dash value={v.produto_nome ?? ''} />
                        </td>
                        <td className="szv2-td-num" style={{ color: 'var(--szv2-brand)', fontWeight: 700 }}>
                          {v.comissao_pct}%
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
          )}

          {/* ── Aba: Contas cadastradas ──────────────────────── */}
          {tab === 'contas' && (
            contas.length === 0 ? (
              <div className="szv2-empty">
                <h3>Nenhuma conta cadastrada</h3>
                <p>O produtor ainda não cadastrou contas para recebimento.</p>
              </div>
            ) : (
              <div style={{ overflowX: 'auto' }}>
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Titular</th>
                      <th>Tipo</th>
                      <th>Chave</th>
                      <th>Padrão</th>
                    </tr>
                  </thead>
                  <tbody>
                    {contas.map((c, i) => (
                      <tr key={`${c.pix_key}-${i}`}>
                        <td style={{ fontWeight: 600 }}>
                          <Dash value={c.nome} />
                        </td>
                        <td>
                          <span className={`sz-badge ${PIX_TYPE_BADGE[c.pix_type] || 'szv2-badge-neutral'}`}>
                            <Dash value={c.pix_type} />
                          </span>
                        </td>
                        <td
                          style={{
                            fontFamily: 'var(--szv2-font-mono)',
                            fontSize: 12,
                            maxWidth: 220,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                          title={c.pix_key}
                        >
                          <Dash value={c.pix_key} />
                        </td>
                        <td>
                          {c.is_default
                            ? <span className="sz-badge szv2-badge-success">padrão</span>
                            : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
          )}
        </>
      )}
    </DetailDrawer>
  )
}

// ---------------------------------------------------------------------------
// Página principal — Produtores
// ---------------------------------------------------------------------------

export default function Producers() {
  const [items, setItems] = useState<Producer[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [drawer, setDrawer] = useState<Producer | null>(null)

  // Filtros aplicados — disparam fetch.
  const [q, setQ] = useState('')

  // Drafts no painel de filtros.
  const [draftQ, setDraftQ] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  function buildQs() {
    const p = new URLSearchParams()
    if (q.trim()) p.set('q', q.trim())
    p.set('limit', '300')
    return p.toString()
  }

  async function load() {
    setLoading(true); setErr('')
    try {
      const r = await api<{ items: Producer[]; total: number }>(`/producers?${buildQs()}`)
      setItems(r.items ?? [])
      setTotal(r.total ?? (r.items?.length ?? 0))
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar produtores')
    } finally {
      setLoading(false)
    }
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load() }, [q])

  function openPanel() {
    setDraftQ(q)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ('')
    setDraftQ('')
    setFilterOpen(false)
  }

  // KPIs agregados sobre a página atual.
  const sumVendido = items.reduce((s, p) => s + (p.valor_vendido_total || 0), 0)
  const sumAfiliados = items.reduce((s, p) => s + (p.afiliados_count || 0), 0)

  // Chips de filtros ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  const activeCount = chips.length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Produtores</h1>
          <p>{total} produtor(es) cadastrado(s)</p>
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {/* Banner só para erro com dados na tela (refresh/ação falhou): não
          apaga a tabela. Falha de carregamento inicial vira ErrorState abaixo. */}
      {err && items.length > 0 && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>
      )}

      {/* KPIs (página atual) */}
      <div
        className="szv2-kpi-grid"
        style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 12 }}
      >
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Total vendido — página</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{brl(sumVendido)}</span>
          <span className="szv2-kpi-meta">soma da página</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Produtores</span>
          <span className="szv2-kpi-value">{items.length}</span>
          <span className="szv2-kpi-meta">de {total} total</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Afiliados vinculados</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-info)' }}>{sumAfiliados}</span>
          <span className="szv2-kpi-meta">soma da página</span>
        </div></div>
      </div>

      {loading && items.length === 0 ? (
        <TableSkeleton rows={6} cols={7} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={load} />
      ) : !loading && items.length === 0 ? (
        <EmptyState
          icon="🏭"
          title="Nenhum produtor cadastrado ainda."
          description="Quando houver produtores, eles aparecem aqui."
        />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>Nome / Email</th>
              <th className="szv2-td-num">Valor vendido</th>
              <th className="szv2-td-num">Comissão pendente afiliados</th>
              <th className="szv2-td-num">Afiliados</th>
              <th className="szv2-td-num">Produtos</th>
              <th>Status</th>
              <th>Cadastrado em</th>
            </tr>
          </thead>
          <tbody>
            {items.map(p => (
              <tr
                key={p.user_id}
                onClick={() => setDrawer(p)}
                style={{ cursor: 'pointer' }}
                title="Ver detalhes do produtor"
              >
                {/* Nome / Email empilhados. */}
                <td style={{ fontSize: 13 }}>
                  <div style={{ fontWeight: 600 }}>
                    {p.nome || <span style={{ color: 'var(--szv2-text-faint)' }}>(sem nome)</span>}
                  </div>
                  <div style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>{p.email}</div>
                </td>
                <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-brand)', fontWeight: 700 }}>
                  {p.valor_vendido_total > 0 ? brl(p.valor_vendido_total) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-warning)', fontWeight: 600 }}>
                  {p.comissao_pendente_afiliados > 0
                    ? brl(p.comissao_pendente_afiliados)
                    : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-soft)', fontWeight: 600 }}>
                  {!!p.afiliados_count
                    ? p.afiliados_count
                    : <span style={{ color: 'var(--szv2-text-faint)' }}>0</span>}
                </td>
                <td className="szv2-td-num" style={{ color: 'var(--szv2-text-soft)', fontWeight: 600 }}>
                  {!!p.produtos_count
                    ? p.produtos_count
                    : <span style={{ color: 'var(--szv2-text-faint)' }}>0</span>}
                </td>
                <td>
                  <span className={`szv2-status-badge ${statusBadgeClass(p.status)}`}>
                    {statusLabel(p.status)}
                  </span>
                </td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  {p.created_at ? brDate(p.created_at) : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      )}

      {drawer && (
        <ProducerDrawer
          producer={drawer}
          onClose={() => setDrawer(null)}
          onDeleted={() => { setDrawer(null); load() }}
          onSaved={() => { load() }}
        />
      )}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="Busca (nome / email)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: maria@…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
