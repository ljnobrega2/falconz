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
import CopyButton from '../components/CopyButton'
import FalkSelect from '../components/FalkSelect'
import FalkDatePicker from '../components/FalkDatePicker'
import { confirmAsync } from '../components/ConfirmDialog'
import { emitToast } from '../hooks/useToast'

// FEAT-RBAC-2026-06-21 — link de convite fixo do usuário. O domínio público de
// resolução é falklog.com.br/r/{código}. Hoje o ÚNICO código por-usuário que a
// API admin devolve é `affiliate_code` (só afiliados têm). BACKEND GAP: um
// `referral_code` universal (todos os papéis: produtor/operador/etc.) ainda não
// existe — quando existir, trocar a fonte aqui. Sem código → não renderiza link.
const REFERRAL_BASE = 'https://falklog.com.br/r/'

type A = {
  user_id: number
  email: string
  nome: string
  telefone: string
  cpf: string
  pix_key: string
  affiliate_code: string | null
  comissao_pct: number
  status: string
  created_at: string
  vinculos: number
  links_count: number
  total_clicks: number
  total_vendido_30d: number
  total_comissao_30d: number
  pedidos_count_30d: number
  last_order_at: string | null
  // Vendas gerais — agregados acumulados (não 30d). Vindos de GET /affiliates.
  comissao_pendente: number
  comissao_disponivel: number
  valor_vendido_total: number
}

// ── Tipos do detalhe lateral — GET /affiliates/{user_id}/detail ──────────────
// Todos os campos são opcionais / null-safe: o endpoint pode ainda não estar
// publicado (drawer degrada com aviso em vez de quebrar) e o shape de `contas`
// é o menos especificado — renderizado de forma tolerante.
// Espelha o shape REAL do backend (go/admin .../affiliates.go affiliateVinculo).
type ProdutorVinculado = {
  produtor_nome?: string
  produto_id?: number | null
  produto_nome?: string | null
  comissao_pct?: number
  status?: string
}
// Espelha o shape REAL do backend (affiliateConta): é a própria conta PIX do
// afiliado (settings JSONB), não contas WC com email/status/data.
type ContaCadastrada = {
  nome?: string | null
  pix_key?: string | null
  pix_type?: string | null
  is_default?: boolean
}
// Espelha affiliateTaxas (backend) — resumo das taxas COBRADAS do afiliado pela
// plataforma. Todas as somas já vêm em R$ atribuídas pelo wp_user_id do afiliado.
type AffiliateTaxas = {
  taxa_transacao_total?: number
  penalidades_frustracao_total?: number
  taxa_saque_total?: number
  total_taxas?: number
}
type AffiliateDetail = {
  user_id?: number
  nome?: string
  email?: string
  telefone?: string
  cpf?: string
  pix_key?: string
  pix_tipo?: string
  vinculos?: ProdutorVinculado[]
  contas?: ContaCadastrada[]
  taxas?: AffiliateTaxas
}

// Status disponíveis no select. Backend pode não filtrar todos — o filtro
// status passa via querystring; se o handler ignorar, vira no-op gracioso.
// API do FalkSelect: { value, label } (não { key, label }).
const STATUS_OPTS = [
  { value: '',            label: 'Todos' },
  { value: 'active',      label: 'Ativo' },
  { value: 'sem_vinculo', label: 'Sem vínculo' },
]

// FEAT-2026-06-22: filtro vendas — com/sem faturamento atribuído. Casa o
// predicado do backend (?vendas=com|sem), que usa a MESMA atribuição da coluna
// "Valor vendido" (sz_orders.affiliate_id = wp_user_id).
const VENDAS_OPTS = [
  { value: '',    label: 'Todos' },
  { value: 'com', label: 'Com vendas' },
  { value: 'sem', label: 'Sem vendas' },
]

// "—" discreto reutilizado em vários pontos do drawer.
function Dash() {
  return <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>
}

// ─────────────────────────────────────────────────────────────────────────────
// Drawer lateral de detalhe do afiliado.
// Espelha a casca do FilterTopPanel (fixed à direita, translateX, overlay, Esc)
// e empresta os padrões de conteúdo do TxDrawer (fetch-on-open com flag `active`,
// estados loading/erro/vazio, blocos com var(--szv2-*-bg)).
// ─────────────────────────────────────────────────────────────────────────────
function AffiliateDetailDrawer({
  affiliate,
  onClose,
  onSaved,
}: {
  affiliate: A
  onClose: () => void
  onSaved: () => void
}) {
  const [detail, setDetail] = useState<AffiliateDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // ── Edição administrativa: CPF + telefone manuais. Paridade com o produtor.
  //    Fonte = senderzz_portal_user_meta (_billing_cpf/_billing_phone), chaveada
  //    pelo portal id. PUT /affiliates/{user_id} {cpf, phone}. PIX permanece
  //    somente-leitura (vem de settings/contas).
  const [editCpf, setEditCpf] = useState('')
  const [editPhone, setEditPhone] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedOk, setSavedOk] = useState(false)

  // Esc fecha o painel.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Carrega o detalhe ao abrir; flag `active` evita setState após desmontar.
  // Se o endpoint /detail ainda não tem linha para este afiliado (responde 404
  // mesmo o afiliado existindo na lista), não tratamos como erro vermelho: o
  // drawer degrada usando os campos que já vieram da lista. Só erros reais
  // (500, rede, sessão) viram banner. `unavailable` é um aviso neutro.
  const [unavailable, setUnavailable] = useState(false)
  useEffect(() => {
    let active = true
    setLoading(true); setErr(''); setUnavailable(false); setSavedOk(false)
    // Seed inicial dos campos editáveis com o que veio da LISTA — vale mesmo se
    // o /detail degradar para `unavailable` (404). Sobrescreve abaixo com o
    // detalhe quando disponível (read-after-write consistente).
    setEditCpf(affiliate.cpf ?? '')
    setEditPhone(affiliate.telefone ?? '')
    api<AffiliateDetail>(`/affiliates/${affiliate.user_id}/detail`)
      .then(d => {
        if (!active) return
        setDetail(d)
        setEditCpf(d.cpf ?? affiliate.cpf ?? '')
        setEditPhone(d.telefone ?? affiliate.telefone ?? '')
      })
      .catch(e => {
        if (!active) return
        const msg = String(e?.message || '')
        // 404 / "não encontrado" → detalhe indisponível, degrada em silêncio.
        if (/404|não encontrado|nao encontrado|not_found|not found/i.test(msg)) {
          setUnavailable(true)
        } else {
          setErr(msg || 'Erro ao carregar detalhe')
        }
      })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [affiliate.user_id, affiliate.cpf, affiliate.telefone])

  // Salvar dados administrativos do afiliado (CPF/telefone) → PUT /affiliates/{id}.
  async function handleSave() {
    setSaving(true); setErr(''); setSavedOk(false)
    try {
      await api(`/affiliates/${affiliate.user_id}`, {
        method: 'PUT',
        body: JSON.stringify({ cpf: editCpf.trim(), phone: editPhone.trim() }),
      })
      // Reflete localmente no detalhe carregado (read-after-write consistente).
      setDetail(d => d ? { ...d, cpf: editCpf.trim(), telefone: editPhone.trim() } : d)
      setSavedOk(true)
      emitToast('ok', 'Dados do afiliado salvos.')
      onSaved()
    } catch (e: any) {
      setErr(e.message || 'Erro ao salvar dados do afiliado')
      emitToast('err', e.message || 'Erro ao salvar dados do afiliado')
    } finally {
      setSaving(false)
    }
  }

  // Fallback de exibição: usa o detalhe quando presente, senão o que já veio da
  // lista. Mantém o drawer útil mesmo se o endpoint /detail ainda não existir.
  // (CPF/telefone agora são editáveis via editCpf/editPhone — ver bloco "Dados".)
  const pixKey = detail?.pix_key ?? affiliate.pix_key
  const pixType = detail?.pix_tipo ?? ''
  const produtores = detail?.vinculos ?? []
  const contas = detail?.contas ?? []
  // Taxas cobradas: SÓ existem no detalhe (/detail). Na degradação 404 (`unavailable`),
  // `detail` é null → não renderizamos a seção (não afirmar "nenhuma taxa" sem dado).
  const taxas = detail?.taxas
  // FEAT-RBAC-2026-06-21 — link de convite fixo: falklog.com.br/r/{affiliate_code}.
  const refCode = (affiliate.affiliate_code || '').trim()
  const refLink = refCode ? `${REFERRAL_BASE}${refCode}` : ''

  const labelStyle: React.CSSProperties = {
    fontSize: 11,
    fontWeight: 700,
    color: 'var(--szv2-text-muted)',
    textTransform: 'uppercase',
    letterSpacing: 0.4,
    marginBottom: 8,
  }

  return (
    <>
      {/* Overlay — click fecha. */}
      <div
        onClick={onClose}
        style={{
          position: 'fixed',
          inset: 0,
          background: 'rgba(0,0,0,0.3)',
          zIndex: 501,
        }}
      />

      {/* Painel lateral direito. */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Detalhe do afiliado"
        style={{
          position: 'fixed',
          top: 0,
          right: 0,
          height: '100vh',
          width: 420,
          maxWidth: '94vw',
          background: 'var(--szv2-surface)',
          borderLeft: '1px solid var(--szv2-divider)',
          boxShadow: '-12px 0 32px rgba(0,0,0,.18)',
          zIndex: 502,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
        }}
      >
        {/* Header */}
        <div
          style={{
            padding: '16px 20px',
            borderBottom: '1px solid var(--szv2-divider)',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'flex-start',
            gap: 12,
          }}
        >
          <div style={{ minWidth: 0 }}>
            <div style={{ fontWeight: 700, fontSize: 15, color: 'var(--szv2-text)' }}>
              {affiliate.nome || '(sem nome)'}
            </div>
            <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {affiliate.email}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Fechar painel"
            style={{
              width: 32,
              height: 32,
              border: 0,
              background: 'transparent',
              borderRadius: 8,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--szv2-text-muted)',
              fontSize: 18,
              lineHeight: 1,
              flexShrink: 0,
            }}
          >
            ✕
          </button>
        </div>

        {/* Body */}
        <div
          style={{
            flex: 1,
            padding: '20px',
            display: 'flex',
            flexDirection: 'column',
            gap: 24,
            overflowY: 'auto',
          }}
        >
          {err && <div className="sz-alert-danger">{err}</div>}
          {unavailable && (
            <div
              style={{
                fontSize: 12,
                color: 'var(--szv2-text-muted)',
                background: 'var(--szv2-surface-alt)',
                border: '1px solid var(--szv2-border)',
                borderRadius: 8,
                padding: '8px 12px',
              }}
            >
              Detalhe completo indisponível para este afiliado. Exibindo dados da lista.
            </div>
          )}

          {loading ? (
            <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
              Carregando…
            </div>
          ) : (
            <>
              {/* Produtores vinculados — a quais produtores o afiliado está afiliado. */}
              <section>
                <div style={labelStyle}>Produtores vinculados</div>
                {produtores.length > 0 ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {produtores.map((p, i) => (
                      <div
                        key={i}
                        style={{
                          padding: '10px 12px',
                          background: 'var(--szv2-surface-alt)',
                          border: '1px solid var(--szv2-border)',
                          borderRadius: 10,
                        }}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                          <span style={{ fontWeight: 600, fontSize: 13 }}>
                            {p.produtor_nome || <Dash />}
                          </span>
                          <span
                            style={{
                              fontSize: 12,
                              fontWeight: 700,
                              color: 'var(--szv2-brand)',
                              background: 'var(--szv2-brand-light, rgba(30,111,242,.10))',
                              padding: '2px 8px',
                              borderRadius: 6,
                              whiteSpace: 'nowrap',
                            }}
                          >
                            {(p.comissao_pct ?? 0)}%
                          </span>
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 2 }}>
                          {p.produto_nome || 'Todos os produtos'}
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
                    Nenhum produtor vinculado.
                  </div>
                )}
              </section>

              {/* Taxas cobradas — resumo das taxas que a plataforma cobrou deste
                  afiliado (transação 4,99%, penalidades de frustração, taxas de saque).
                  Só renderiza quando o detalhe veio do /detail (`taxas` presente): na
                  degradação 404 não afirmamos "nenhuma taxa" sem dado real. */}
              {taxas && (
                <section>
                  <div style={labelStyle}>Taxas cobradas</div>
                  {(taxas.total_taxas ?? 0) > 0 ? (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 12 }}>
                        <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Taxa de transação (4,99%)</span>
                        <span style={{ fontSize: 13, color: 'var(--szv2-text)', fontWeight: 600 }}>
                          {brl(taxas.taxa_transacao_total ?? 0)}
                        </span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 12 }}>
                        <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Penalidades de frustração</span>
                        <span style={{ fontSize: 13, color: 'var(--szv2-text)', fontWeight: 600 }}>
                          {brl(taxas.penalidades_frustracao_total ?? 0)}
                        </span>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 12 }}>
                        <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Taxa de saque</span>
                        <span style={{ fontSize: 13, color: 'var(--szv2-text)', fontWeight: 600 }}>
                          {brl(taxas.taxa_saque_total ?? 0)}
                        </span>
                      </div>
                      {/* Total — destacado, separado por borda superior. */}
                      <div
                        style={{
                          display: 'flex',
                          justifyContent: 'space-between',
                          alignItems: 'baseline',
                          gap: 12,
                          marginTop: 4,
                          paddingTop: 10,
                          borderTop: '1px solid var(--szv2-divider)',
                        }}
                      >
                        <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--szv2-text)' }}>Total de taxas</span>
                        <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--szv2-brand)' }}>
                          {brl(taxas.total_taxas ?? 0)}
                        </span>
                      </div>
                    </div>
                  ) : (
                    <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
                      Nenhuma taxa cobrada ainda.
                    </div>
                  )}
                </section>
              )}

              {/* Dados sensíveis — CPF e telefone EDITÁVEIS (paridade com o
                  produtor). Gravam em senderzz_portal_user_meta via PUT
                  /affiliates/{id}. PIX e Link de convite permanecem read-only. */}
              <section>
                <div style={labelStyle}>Dados</div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
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

                  <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
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
                        ✓ Salvo
                      </span>
                    )}
                  </div>

                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                    <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>Chave PIX</span>
                    <span style={{ fontSize: 13, textAlign: 'right', fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-text-soft)', wordBreak: 'break-all' }}>
                      {pixKey
                        ? <>{!!pixType && <span style={{ color: 'var(--szv2-text-muted)' }}>{pixType.toUpperCase()} · </span>}{pixKey}</>
                        : <Dash />}
                    </span>
                  </div>
                  {/* FEAT-RBAC-2026-06-21 — link de convite fixo (falklog.com.br/r/{código}).
                      Sem affiliate_code → "—". CopyButton já usa var(--szv2-brand) = #1E6FF2. */}
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
                    <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>Link de convite</span>
                    <span style={{ fontSize: 13, textAlign: 'right', wordBreak: 'break-all', display: 'flex', alignItems: 'center', gap: 6, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                      {refLink ? (
                        <>
                          <span style={{ fontFamily: 'var(--szv2-font-mono)', color: 'var(--szv2-text-soft)', fontSize: 12 }}>{refLink}</span>
                          <CopyButton text={refLink} variant="icon" title="Copiar link de convite" />
                        </>
                      ) : (
                        <Dash />
                      )}
                    </span>
                  </div>
                </div>
              </section>

              {/* Contas cadastradas. */}
              <section>
                <div style={labelStyle}>Contas cadastradas</div>
                {contas.length > 0 ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {contas.map((c, i) => (
                      <div
                        key={i}
                        style={{
                          padding: '10px 12px',
                          background: 'var(--szv2-surface-alt)',
                          border: '1px solid var(--szv2-border)',
                          borderRadius: 10,
                        }}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                          {!!c.nome && (
                            <div style={{ fontWeight: 600, fontSize: 13 }}>{c.nome}</div>
                          )}
                          {c.is_default && (
                            <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--szv2-brand)', whiteSpace: 'nowrap' }}>
                              Padrão
                            </span>
                          )}
                        </div>
                        {!!c.pix_key && (
                          <div style={{ fontSize: 12, color: 'var(--szv2-text-soft)', fontFamily: 'var(--szv2-font-mono)', marginTop: !!c.nome ? 4 : 0, wordBreak: 'break-all' }}>
                            {!!c.pix_type && <span style={{ color: 'var(--szv2-text-muted)' }}>{c.pix_type.toUpperCase()} · </span>}{c.pix_key}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>
                    Nenhuma conta cadastrada.
                  </div>
                )}
              </section>
            </>
          )}
        </div>
      </div>
    </>
  )
}

export default function Affiliates() {
  const [items, setItems] = useState<A[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Afiliado selecionado para o drawer lateral de detalhe.
  const [selected, setSelected] = useState<A | null>(null)
  const [deletingId, setDeletingId] = useState<number | null>(null)

  // Filtros aplicados — disparam fetch.
  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')
  // FEAT-2026-06-22: filtros ricos. vendas (com/sem), valorMin (piso de
  // faturamento), produtor (nome do produtor vinculado — busca textual).
  const [vendas, setVendas] = useState('')
  const [valorMin, setValorMin] = useState('')
  const [produtor, setProdutor] = useState('')

  // FEAT-2026-06-23: filtro "Produtor vinculado" vira SELECT (regra do dono:
  // "não usar filtros com escrita — filtros de texto viram SELECT"). Carrega a
  // lista de produtores no mount; opções = nome (value=label=nome, casa o
  // ILIKE textual que o backend já faz em ?produtor=). Falha de fetch degrada
  // para só "Todos" — nunca quebra a página.
  const [producers, setProducers] = useState<{ user_id: number; nome: string; email: string }[]>([])

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')
  const [draftVendas, setDraftVendas] = useState('')
  const [draftValorMin, setDraftValorMin] = useState('')
  const [draftProdutor, setDraftProdutor] = useState('')

  const [filterOpen, setFilterOpen] = useState(false)

  function buildQs() {
    const p = new URLSearchParams()
    if (q.trim()) p.set('q', q.trim())
    if (status) p.set('status', status)
    if (dataIni) p.set('data_ini', dataIni)
    if (dataFim) p.set('data_fim', dataFim)
    if (vendas) p.set('vendas', vendas)
    if (valorMin.trim()) p.set('valor_min', valorMin.trim())
    if (produtor.trim()) p.set('produtor', produtor.trim())
    p.set('limit', '100')
    return p.toString()
  }

  async function load() {
    setLoading(true)
    try {
      const r = await api<{ items: A[]; total: number }>(`/affiliates?${buildQs()}`)
      setItems(r.items ?? [])
      setTotal(r.total)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load() }, [q, status, dataIni, dataFim, vendas, valorMin, produtor])

  // Excluir afiliado = SOFT-DELETE no backend (ativo=false). Reaproveita o MESMO
  // endpoint do cliente (DELETE /clientes/{id}) — o handler aceita cliente E
  // afiliado e gateia por papel. a.user_id JÁ é o portal id (affiliates.go:
  // `u.id AS user_id` / `FROM senderzz_portal_users u`), idêntico ao /clientes.
  // stopPropagation: a <tr> tem onClick que abre o drawer — sem isso, excluir
  // também abriria o detalhe.
  async function handleDelete(e: React.MouseEvent, a: A) {
    e.stopPropagation()
    const ok = await confirmAsync({
      variant: 'danger',
      title: 'Excluir afiliado',
      message: `Excluir o afiliado ${a.nome || a.email || `#${a.user_id}`}? Sai da listagem; histórico preservado.`,
      confirmLabel: 'Excluir',
    })
    if (!ok) return
    setDeletingId(a.user_id)
    try {
      await api(`/clientes/${a.user_id}`, { method: 'DELETE' })
      emitToast('ok', 'Afiliado excluído.')
      await load()
    } catch (err: any) {
      emitToast('err', err.message || 'Erro ao excluir afiliado')
    } finally {
      setDeletingId(null)
    }
  }

  // Carrega os produtores (opções do select "Produtor vinculado") uma vez no
  // mount. Shape REAL confirmado em go/admin .../producers.go List: { items:
  // [{ user_id, nome, email }], total }. Erro é silenciado → filtro fica só
  // com "Todos".
  useEffect(() => {
    let active = true
    api<{ items: { user_id: number; nome: string; email: string }[] }>('/producers?limit=300')
      .then(r => { if (active) setProducers(r.items ?? []) })
      .catch(() => { /* degrada para só "Todos" — não quebra a página */ })
    return () => { active = false }
  }, [])

  // Opções do select: "Todos" (vazio = sem filtro) + nomes únicos não-vazios.
  // O backend faz COALESCE(nome,'') → produtor pode ter nome '' (colidiria com
  // o sentinel "Todos"); dedup + drop de vazios evita isso e nomes repetidos.
  const produtorOpts = [
    { value: '', label: 'Todos' },
    ...Array.from(new Set(producers.map(p => p.nome).filter(n => n && n.trim())))
      .map(n => ({ value: n, label: n })),
  ]

  function openPanel() {
    setDraftQ(q)
    setDraftStatus(status)
    setDraftIni(dataIni)
    setDraftFim(dataFim)
    setDraftVendas(vendas)
    setDraftValorMin(valorMin)
    setDraftProdutor(produtor)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ)
    setStatus(draftStatus)
    setDataIni(draftIni)
    setDataFim(draftFim)
    setVendas(draftVendas)
    setValorMin(draftValorMin)
    setProdutor(draftProdutor)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setStatus(''); setDataIni(''); setDataFim('')
    setVendas(''); setValorMin(''); setProdutor('')
    setDraftQ(''); setDraftStatus(''); setDraftIni(''); setDraftFim('')
    setDraftVendas(''); setDraftValorMin(''); setDraftProdutor('')
    setFilterOpen(false)
  }

  // KPIs agregados sobre a página atual (últimos 30 dias).
  const sumVendido = items.reduce((s, a) => s + (a.total_vendido_30d || 0), 0)
  const sumComissao = items.reduce((s, a) => s + (a.total_comissao_30d || 0), 0)
  const sumPedidos = items.reduce((s, a) => s + (a.pedidos_count_30d || 0), 0)

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (status) {
    const sl = STATUS_OPTS.find(s => s.value === status)?.label ?? status
    chips.push({ key: 'status', label: `Status: ${sl}`, onRemove: () => setStatus('') })
  }
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  if (vendas) {
    const vl = VENDAS_OPTS.find(v => v.value === vendas)?.label ?? vendas
    chips.push({ key: 'vendas', label: `Vendas: ${vl}`, onRemove: () => setVendas('') })
  }
  if (valorMin) chips.push({ key: 'valorMin', label: `Vendido ≥ ${brl(Number(valorMin) || 0)}`, onRemove: () => setValorMin('') })
  if (produtor) chips.push({ key: 'produtor', label: `Produtor: ${produtor}`, onRemove: () => setProdutor('') })
  const activeCount = chips.length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Afiliados</h1>
          <p>{total} afiliados cadastrados</p>
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          <FilterButton
            active={activeCount > 0}
            count={activeCount}
            onClick={openPanel}
          />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {/* Banner só com dados na tela (erro de refresh/ação): não apaga a tabela.
          Falha de carregamento inicial vira ErrorState abaixo. */}
      {err && items.length > 0 && <div className="sz-alert-danger">{err}</div>}

      {/* KPIs últimos 30d (página atual) */}
      <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', marginBottom: 12 }}>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Vendido (30d) — página</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{brl(sumVendido)}</span>
          <span className="szv2-kpi-meta">{sumPedidos} pedido(s)</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Comissões geradas (30d)</span>
          <span className="szv2-kpi-value" style={{ color: 'var(--szv2-success)' }}>{brl(sumComissao)}</span>
          <span className="szv2-kpi-meta">soma da página</span>
        </div></div>
        <div className="szv2-card"><div className="szv2-kpi">
          <span className="szv2-kpi-label">Afiliados ativos (página)</span>
          <span className="szv2-kpi-value">{items.length}</span>
          <span className="szv2-kpi-meta">de {total} total</span>
        </div></div>
      </div>

      {loading && items.length === 0 ? (
        <TableSkeleton rows={6} cols={6} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={() => { setErr(''); load() }} />
      ) : !loading && items.length === 0 ? (
        <EmptyState
          icon="🤝"
          title="Nenhum afiliado cadastrado ainda."
          description="Quando houver afiliados, eles aparecem aqui."
        />
      ) : (
      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            {/* Cabeçalho de grupo: "Vendas gerais" cobre as 3 colunas de R$. */}
            <tr>
              <th rowSpan={2}>Nome / Email</th>
              <th colSpan={3} style={{ textAlign: 'center', borderBottom: '1px solid var(--szv2-divider)' }}>
                Vendas gerais
              </th>
              <th rowSpan={2}>Status</th>
              <th rowSpan={2}>Último pedido</th>
              <th rowSpan={2} style={{ textAlign: 'right' }}>Ações</th>
            </tr>
            <tr>
              <th className="szv2-td-num">Valor vendido</th>
              <th className="szv2-td-num">Comissão pendente</th>
              <th className="szv2-td-num">Comissão disponível</th>
            </tr>
          </thead>
          <tbody>
            {items.map(a => (
              <tr
                key={a.user_id}
                onClick={() => setSelected(a)}
                style={{ cursor: 'pointer' }}
                title="Ver detalhes do afiliado"
              >
                {/* Nome / Email empilhados: nome em cima, email logo abaixo em fonte menor. */}
                <td style={{ fontSize: 13 }}>
                  <div style={{ fontWeight: 600 }}>{a.nome || <span style={{ color: 'var(--szv2-text-faint)' }}>(sem nome)</span>}</div>
                  <div style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>{a.email}</div>
                </td>
                <td className="szv2-td-num" style={{ fontSize: 13 }}>
                  {a.valor_vendido_total > 0 ? brl(a.valor_vendido_total) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-warning)', fontWeight: 600 }}>
                  {a.comissao_pendente > 0 ? brl(a.comissao_pendente) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td className="szv2-td-num" style={{ fontSize: 13, color: 'var(--szv2-success)', fontWeight: 600 }}>
                  {a.comissao_disponivel > 0 ? brl(a.comissao_disponivel) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td>
                  {/* MED26: 'aprovado'/'approved' (legado PT-BR/EN) também são vínculo confirmado
                      → badge verde. Antes caíam no else e exibiam vermelho 'Cancelado'. */}
                  <span className={`szv2-status-badge ${a.status === 'active' || a.status === 'ativo' || a.status === 'aprovado' || a.status === 'approved' ? 's-confirmado' : a.status === 'pending' || a.status === 'pendente' ? 's-pendente' : 's-cancelado'}`}>
                    {a.status === 'active' || a.status === 'ativo' || a.status === 'aprovado' || a.status === 'approved' ? 'Confirmado'
                      : a.status === 'pending' || a.status === 'pendente' ? 'Pendente'
                      : a.status === 'sem_vinculo' ? 'Sem vínculo'
                      : a.status}
                  </span>
                </td>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>
                  {a.last_order_at ? brDate(a.last_order_at) : <span style={{ color: 'var(--szv2-text-faint)' }}>—</span>}
                </td>
                <td style={{ textAlign: 'right' }}>
                  <button
                    type="button"
                    className="szv2-btn szv2-btn-danger szv2-btn-sm"
                    disabled={deletingId === a.user_id}
                    onClick={(e) => handleDelete(e, a)}
                  >
                    {deletingId === a.user_id ? 'Excluindo…' : 'Excluir'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      )}

      {selected && (
        <AffiliateDetailDrawer
          affiliate={selected}
          onClose={() => setSelected(null)}
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
        <FilterField label="Data inicial">
          <FalkDatePicker
            value={draftIni}
            max={draftFim || undefined}
            onChange={v => setDraftIni(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Data final">
          <FalkDatePicker
            value={draftFim}
            min={draftIni || undefined}
            onChange={v => setDraftFim(v)}
            placeholder="dd/mm/aaaa"
          />
        </FilterField>
        <FilterField label="Status">
          <FalkSelect
            value={draftStatus}
            onChange={v => setDraftStatus(v)}
            options={STATUS_OPTS}
            aria-label="Status"
          />
        </FilterField>
        <FilterField label="Vendas">
          <FalkSelect
            value={draftVendas}
            onChange={v => setDraftVendas(v)}
            options={VENDAS_OPTS}
            aria-label="Vendas"
          />
        </FilterField>
        <FilterField label="Valor vendido mínimo (R$)">
          <input
            type="number"
            inputMode="decimal"
            min={0}
            step="0.01"
            style={filterInputStyle}
            placeholder="ex.: 1000"
            value={draftValorMin}
            onChange={e => setDraftValorMin(e.target.value)}
          />
        </FilterField>
        <FilterField label="Produtor vinculado">
          <FalkSelect
            value={draftProdutor}
            onChange={v => setDraftProdutor(v)}
            options={produtorOpts}
            placeholder="Todos"
            aria-label="Produtor vinculado"
          />
        </FilterField>
        <FilterField label="Busca (email / nome)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: joao@…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
