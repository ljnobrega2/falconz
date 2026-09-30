import { useEffect, useRef, useState } from 'react'
import { api, BASE, getToken } from '../api'
import { confirmAsync } from '../components/ConfirmDialog'
import FalkDatePicker from '../components/FalkDatePicker'
import FalkSelect from '../components/FalkSelect'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import TableSkeleton from '../components/TableSkeleton'
import DetailDrawer from '../components/DetailDrawer'
import EmptyState from '../components/EmptyState'
import { emitToast } from '../hooks/useToast'
import { brDate } from '../utils/format'

// AUDIT-2026-07-14 — Estoque MERGEADO nesta tela (pedido do dono: "mescla
// estoque e produtos numa tela só"). Estoque é por (product_id, variation_id,
// cd_id) — 1:N por CD. Hoje só existe 1 CD ativo, então na prática cada
// produto casa com 1 linha de estoque; a coluna mostra o CD pra não esconder
// o dado se/quando existir mais de um (decisão do dono: revisitar quando
// houver 2º CD).
type StockItem = {
  id: number
  product_id: number
  variation_id: number
  product_name: string
  cd_id: number
  cd_nome: string
  qty_available: number
  qty_reserved: number
  qty_sellable: number
  low_stock_threshold: number
  updated_at: string
}

type StockMovement = {
  id: number
  tipo: string
  delta_available: number
  delta_reserved: number
  order_id: number | null
  motivo: string | null
  created_at: string
}

type Cd = { id: number; nome: string }

const MOV_TIPO_LABEL: Record<string, string> = {
  entrada:  'Entrada',
  ajuste:   'Ajuste',
  reserva:  'Reserva',
  baixa:    'Baixa',
  estorno:  'Estorno',
}

function intOf(v: string): number {
  const t = v.trim()
  if (t === '') return 0
  const n = Number(t)
  return Number.isFinite(n) ? Math.trunc(n) : 0
}

function fmtDateTime(s: string): string {
  if (!s) return '—'
  const d = new Date(s.replace(' ', 'T'))
  if (Number.isNaN(d.getTime())) return s.substring(0, 16)
  return d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
}

// Produto NÃO tem preço (regra do dono v469): o preço de venda vive no link de
// oferta, não no produto. O produto carrega SOMENTE as dimensões físicas
// (altura/largura/comprimento em cm, peso em kg) — todas opcionais (null = não
// informado, nunca 0 inventado).
type Product = {
  id: number
  wp_post_id: number | null
  produtor_id: number
  produtor_nome: string
  nome: string
  sku: string | null
  barcode: string | null
  descricao: string | null
  categoria: string | null
  status: string
  // Variação — atributo do produto (ex.: 'pote', 'cápsula'). Regra do dono: todo
  // produto tem o campo, cadastrado aqui no menu. Backend grava string ('' = vazio).
  variacao: string
  altura: number | null
  largura: number | null
  comprimento: number | null
  peso: number | null
  // image_url — URL da imagem do produto (OPCIONAL). Vem de sz_products.meta
  // (chave image_url). "" quando não há imagem. Gravada no Create/Update e lida pelo
  // checkout/portal (a mesma chave), então a miniatura aparece no checkout.
  image_url: string
  // vitrine_visible — toggle "Visível na vitrine" (migração 479). true = aparece na
  // Vitrine do portal (descoberta de afiliados); false = oculto. A tela Produtos NÃO
  // filtra por isto (o admin vê os ocultos p/ religá-los); quem filtra é a vitrine.
  vitrine_visible: boolean
  created_at: string
  afiliados_count: number
  // parent_product_id/parent_product_nome — FEAT-VARIACAO: quando preenchido,
  // este produto é uma variação de outro produto (mesmo padrão de linha própria
  // em sz_products, só com o vínculo de exibição). null = produto solto.
  parent_product_id: number | null
  parent_product_nome: string | null
}

type Producer = { id: number; nome: string; email: string }

// Dono do produto = QUALQUER usuário do portal (cliente/afiliado/produtor). Fonte:
// GET /clientes?role=all. Quando o dono NÃO é produtor, o produto nasce 'a_aprovar'
// e, ao ser aprovado, promove o dono a produtor (product_approval.go).
type Owner = { id: number; nome: string; email: string; role: string }

const ROLE_LABEL: Record<string, string> = {
  cliente:  'Cliente',
  afiliado: 'Afiliado',
  produtor: 'Produtor',
}

const STATUSES = ['active', 'inactive', 'draft', 'archived']

// Lista fixa de categorias (fallback). Não há endpoint de categorias no backend
// (sz_products.categoria é texto livre); então o <select> é alimentado por esta
// lista sensata MERGE com as categorias já existentes nos produtos carregados —
// assim nunca fica vazio e categorias antigas não somem. Se o backend ganhar um
// endpoint de categorias no futuro, trocar esta constante por um fetch.
const CATEGORIAS_FIXAS = [
  'Suplementos',
  'Cosméticos',
  'Eletrônicos',
  'Moda e Acessórios',
  'Casa e Decoração',
  'Saúde e Bem-estar',
  'Alimentos e Bebidas',
  'Infantil',
  'Pet',
  'Esportes',
  'Outros',
]

// Labels PT-BR de status. Inclui o ciclo de aprovação de produto
// ('a_aprovar'/'aprovado'/'reprovado'), produzido pelo backend de aprovação
// (go/admin product_approval.go) — produto criado pelo produtor nasce
// 'a_aprovar'. Sem estes tokens, a tabela renderizava o token cru (BAIXO55).
// Wording alinhado ao StatusBadge canônico (statusLabel: "A aprovar" etc.).
const STATUS_LABEL: Record<string, string> = {
  active:    'Ativo',
  inactive:  'Inativo',
  draft:     'Rascunho',
  archived:  'Arquivado',
  a_aprovar: 'A aprovar',
  aprovado:  'Aprovado',
  reprovado: 'Reprovado',
}

const STATUS_CLASS: Record<string, string> = {
  active:    'szv2-badge-success',
  inactive:  'szv2-badge-neutral',
  draft:     'szv2-badge-warning',
  archived:  'szv2-badge-neutral',
  a_aprovar: 'szv2-badge-warning', // ouro — aguardando aprovação do admin
  aprovado:  'szv2-badge-success', // verde — aprovado
  reprovado: 'szv2-badge-danger',  // vermelho — reprovado
}

function emptyForm(): Omit<Product, 'id' | 'produtor_nome' | 'created_at' | 'afiliados_count' | 'wp_post_id' | 'parent_product_nome'> & { id: number } {
  return {
    id: 0,
    produtor_id: 0,
    nome: '',
    sku: null,
    barcode: null,
    descricao: null,
    categoria: null,
    status: 'active',
    variacao: '',
    altura: null,
    largura: null,
    comprimento: null,
    peso: null,
    image_url: '',
    vitrine_visible: true, // default visível (espelha o DEFAULT true da coluna 479)
    parent_product_id: null,
  }
}

// Converte o valor de um <input type="number"> em number|null: vazio → null
// (dimensão não informada), nunca 0 inventado. NaN também vira null.
function numOrNull(v: string): number | null {
  const t = v.trim()
  if (t === '') return null
  const n = Number(t)
  return Number.isNaN(n) ? null : n
}

// Formata uma dimensão para exibição na tabela com a unidade; null → '—'.
function fmtDim(v: number | null, unidade: string): string {
  if (v === null || v === undefined) return '—'
  return `${v} ${unidade}`
}

type ProductsStats = {
  active_count: number
  total_affiliates: number
  revenue_30d: number
}

function fmtBRL(n: number): string {
  return n.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' })
}

export default function Products() {
  const [items, setItems] = useState<Product[]>([])
  const [total, setTotal] = useState(0)
  const [stats, setStats] = useState<ProductsStats | null>(null)
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)

  // Filtros aplicados.
  const [q, setQ] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [produtorID, setProdutorID] = useState('')
  const [dataIni, setDataIni] = useState('')
  const [dataFim, setDataFim] = useState('')

  // Drafts no painel.
  const [draftQ, setDraftQ] = useState('')
  const [draftStatus, setDraftStatus] = useState('')
  const [draftProdutor, setDraftProdutor] = useState('')
  const [draftIni, setDraftIni] = useState('')
  const [draftFim, setDraftFim] = useState('')

  const [filterOpen, setFilterOpen] = useState(false)

  // Form state
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState(emptyForm())
  const [saving, setSaving] = useState(false)
  // Upload de imagem do produto (multipart). Não usa o helper api() porque ele
  // força Content-Type: application/json — o que apagaria o boundary do multipart.
  const [uploadingImg, setUploadingImg] = useState(false)
  const [producers, setProducers] = useState<Producer[]>([])
  // Donos elegíveis (qualquer cliente/afiliado/produtor) p/ o seletor do form.
  const [owners, setOwners] = useState<Owner[]>([])

  // ── Estoque (mergeado) ──────────────────────────────────────────────────────
  const [stockByProduct, setStockByProduct] = useState<Record<number, StockItem>>({})
  const [cds, setCds] = useState<Cd[]>([])

  // Bipagem de entrada (card inline): produtor + SKU + quantidade.
  const [bipProducerId, setBipProducerId] = useState('')
  const [bipSku, setBipSku] = useState('')
  const [bipQty, setBipQty] = useState('1')
  const [bipBusy, setBipBusy] = useState(false)
  const [bipErr, setBipErr] = useState('')
  const [bipSaldo, setBipSaldo] = useState<string | null>(null)
  const bipSkuRef = useRef<HTMLInputElement>(null)

  // Drawer Entrada de estoque.
  const [showEntrada, setShowEntrada] = useState(false)
  const [entProductId, setEntProductId] = useState('')
  const [entCdId, setEntCdId] = useState('')
  const [entQty, setEntQty] = useState('')
  const [entMin, setEntMin] = useState('')
  const [savingEntrada, setSavingEntrada] = useState(false)
  const [stockErr, setStockErr] = useState('')

  // Drawer Ajuste de estoque.
  const [adjustItem, setAdjustItem] = useState<StockItem | null>(null)
  const [adjDelta, setAdjDelta] = useState('')
  const [adjMotivo, setAdjMotivo] = useState('')
  const [savingAdj, setSavingAdj] = useState(false)

  // Drawer Movimentos de estoque.
  const [movItem, setMovItem] = useState<StockItem | null>(null)
  const [movs, setMovs] = useState<StockMovement[]>([])
  const [loadingMovs, setLoadingMovs] = useState(false)

  async function loadStock() {
    try {
      const r = await api<{ ok: boolean; items: StockItem[]; total: number }>('/stock?per_page=300&page=1')
      const byProduct: Record<number, StockItem> = {}
      for (const it of r.items ?? []) {
        // Estoque é único e compartilhado por Expedição + COD. A API mantém
        // linhas por CD para auditoria, mas esta tela exibe o total consolidado.
        const prev = byProduct[it.product_id]
        if (!prev) {
          byProduct[it.product_id] = { ...it, cd_id: 0, cd_nome: 'Estoque único' }
        } else {
          const available = prev.qty_available + it.qty_available
          const reserved = prev.qty_reserved + it.qty_reserved
          byProduct[it.product_id] = {
            ...prev,
            qty_available: available,
            qty_reserved: reserved,
            qty_sellable: Math.max(available - reserved, 0),
            low_stock_threshold: Math.max(prev.low_stock_threshold, it.low_stock_threshold),
          }
        }
      }
      setStockByProduct(byProduct)
    } catch { /* estoque fica vazio nas linhas — coluna mostra "—" */ }
  }

  async function loadCds() {
    try {
      const r = await api<{ items: Cd[] }>('/cds')
      setCds(r.items ?? [])
    } catch { /* dropdown fica vazio (CD opcional) */ }
  }

  function isLow(it: StockItem): boolean {
    return it.qty_sellable <= it.low_stock_threshold
  }

  async function biparEntrada(e: React.FormEvent) {
    e.preventDefault()
    setBipErr('')
    setBipSaldo(null)
    if (intOf(bipProducerId) <= 0) { setBipErr('Selecione o produtor.'); return }
    const sku = bipSku.trim()
    if (!sku) { setBipErr('Bipe ou digite o SKU (código de barras).'); return }
    const qty = intOf(bipQty)
    if (qty <= 0) { setBipErr('Informe uma quantidade maior que zero.'); return }

    setBipBusy(true)
    try {
      const tok = getToken()
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (tok) headers['Authorization'] = `Bearer ${tok}`

      const res = await fetch(`${BASE}/stock/bipar`, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          producer_id:  intOf(bipProducerId),
          sku,
          quantity:     qty,
          manual_typed: true,
        }),
      })

      const body: any = await res.json().catch(() => ({}))

      if (res.status === 422) {
        const msg = body?.error?.message || `SKU "${sku}" não encontrado. Nada foi creditado.`
        setBipErr(msg)
        emitToast('err', msg)
        return
      }
      if (!res.ok) {
        const msg = body?.error?.message || `Falha ao dar entrada (HTTP ${res.status}).`
        setBipErr(msg)
        emitToast('err', msg)
        return
      }

      const novoSaldo = body.qty_available ?? body.qty_sellable ?? body.new_balance ?? body.novo_saldo ?? body.saldo ?? null
      const nome = body.product_name ? ` (${body.product_name})` : ''
      if (novoSaldo != null) {
        setBipSaldo(`Novo saldo do SKU "${sku}"${nome}: ${novoSaldo}`)
        emitToast('ok', `Entrada registrada${nome}. Novo saldo: ${novoSaldo}.`)
      } else {
        setBipSaldo(`Entrada registrada para o SKU "${sku}"${nome}.`)
        emitToast('ok', 'Entrada registrada com sucesso.')
      }

      setBipSku('')
      setBipQty('1')
      setTimeout(() => bipSkuRef.current?.focus(), 0)
      loadStock()
    } catch (e: any) {
      const msg = e?.message || 'Erro de rede ao dar entrada.'
      setBipErr(msg)
      emitToast('err', msg)
    } finally {
      setBipBusy(false)
    }
  }

  function openEntrada(productId?: number) {
    setEntProductId(productId ? String(productId) : '')
    setEntCdId('')
    setEntQty('')
    setEntMin('')
    setStockErr('')
    setShowEntrada(true)
  }

  async function saveEntrada(e: React.FormEvent) {
    e.preventDefault()
    if (intOf(entProductId) <= 0) { setStockErr('Informe o ID do produto.'); return }
    setSavingEntrada(true)
    setStockErr('')
    try {
      const payload = {
        product_id:          intOf(entProductId),
        variation_id:        0,
        cd_id:               intOf(entCdId),
        qty_available:       intOf(entQty),
        low_stock_threshold: intOf(entMin),
      }
      await api('/stock', { method: 'POST', body: JSON.stringify(payload) })
      emitToast('ok', 'Entrada registrada com sucesso.')
      setShowEntrada(false)
      loadStock()
    } catch (e: any) { setStockErr(e.message); emitToast('err', e.message) }
    finally { setSavingEntrada(false) }
  }

  function openAdjust(it: StockItem) {
    setAdjustItem(it)
    setAdjDelta('')
    setAdjMotivo('')
    setStockErr('')
  }

  async function saveAdjust(e: React.FormEvent) {
    e.preventDefault()
    if (!adjustItem) return
    if (intOf(adjDelta) === 0) { setStockErr('Informe um delta diferente de zero.'); return }
    if (!adjMotivo.trim()) { setStockErr('O motivo do ajuste é obrigatório.'); return }
    setSavingAdj(true)
    setStockErr('')
    try {
      await api(`/stock/${adjustItem.id}/ajuste`, {
        method: 'POST',
        body: JSON.stringify({ delta_available: intOf(adjDelta), motivo: adjMotivo.trim() }),
      })
      emitToast('ok', 'Ajuste aplicado.')
      setAdjustItem(null)
      loadStock()
    } catch (e: any) { setStockErr(e.message); emitToast('err', e.message) }
    finally { setSavingAdj(false) }
  }

  async function openMovs(it: StockItem) {
    setMovItem(it)
    setMovs([])
    setLoadingMovs(true)
    try {
      const r = await api<{ ok: boolean; movements: StockMovement[] }>(`/stock/${it.id}/movements?limit=50`)
      setMovs(r.movements ?? [])
    } catch (e: any) { emitToast('err', e.message) }
    finally { setLoadingMovs(false) }
  }

  async function loadOwners() {
    try {
      // Fonte de DONOS do produto: GET /clientes?role=all → cliente+afiliado+produtor.
      // id = senderzz_portal_users.id (mesmo id-space de produtor_id). Permite ao
      // admin cadastrar produto para um cliente/afiliado e disparar a fila de aprovação.
      const r = await api<{ items: Owner[] }>('/clientes?role=all&limit=200')
      setOwners(r.items ?? [])
    } catch { /* ignora — seletor de dono fica vazio se a busca falhar */ }
  }

  async function loadProducers() {
    try {
      // Fonte canônica de produtores: GET /producers (server-side
      // role='produtor' AND ativo — go/admin/.../producers.go List). Retorna
      // { items:[{ user_id, nome, email, … }], total }. Mapeia user_id→id
      // (= senderzz_portal_users.id, o id-space que o filtro `produtor_id` do
      // /products espera — products.go:152 `sp.produtor_id = $3` por igualdade
      // numérica, NÃO ILIKE por nome). Por isso o value do select é o id, não o
      // nome. limit=200 = teto do endpoint (acima disso volta a 100).
      const r = await api<{ items: { user_id: number; nome: string; email: string }[] }>('/producers?limit=200')
      setProducers((r.items ?? []).map(p => ({ id: p.user_id, nome: p.nome, email: p.email })))
    } catch { /* ignora — select fica vazio */ }
  }

  async function loadStats() {
    try {
      const s = await api<ProductsStats>('/products/stats')
      setStats(s)
    } catch { /* silencia — KPIs ficam zerados */ }
  }

  async function load() {
    setLoading(true)
    try {
      const p = new URLSearchParams()
      if (q.trim()) p.set('q', q.trim())
      if (statusFilter) p.set('status', statusFilter)
      if (produtorID) p.set('produtor_id', produtorID)
      if (dataIni) p.set('data_ini', dataIni)
      if (dataFim) p.set('data_fim', dataFim)
      p.set('limit', '100')
      const r = await api<{ items: Product[]; total: number }>(`/products?${p.toString()}`)
      setItems(r.items ?? [])
      setTotal(r.total ?? 0)
    } catch (e: any) { setErr(e.message) }
    finally { setLoading(false) }
  }

  // Sincroniza produtos a partir do histórico de pedidos (idempotente no backend).
  async function syncFromOrders() {
    setSyncing(true)
    setErr('')
    try {
      const r = await api<{ ok: boolean; synced: number }>('/products/sync-from-orders', { method: 'POST' })
      if (r.synced === 0) {
        setErr('Nenhum produto novo foi encontrado no histórico de pedidos.')
        emitToast('info', 'Nenhum produto novo a sincronizar.')
      } else {
        emitToast('ok', `${r.synced} produto(s) sincronizado(s).`)
      }
      await load()
      await loadStats()
    } catch (e: any) { setErr(e.message); emitToast('err', e.message || 'Falha ao sincronizar produtos.') }
    finally { setSyncing(false) }
  }

  useEffect(() => { load(); loadStats() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [q, statusFilter, produtorID, dataIni, dataFim])

  // Lista de produtores (filtro do painel) + donos elegíveis (seletor do form)
  // + estoque mergeado (CDs + saldos por produto).
  useEffect(() => { loadProducers(); loadOwners(); loadStock(); loadCds() }, [])

  async function save(e: React.FormEvent) {
    e.preventDefault()

    // Validação: TODOS os campos são obrigatórios (regra do dono). Além do
    // `required` no HTML, checamos aqui para cobrir whitespace-only e dimensões
    // null (number vazio) com mensagem clara em PT-BR.
    const faltando: string[] = []
    if (!form.nome.trim())                 faltando.push('Nome')
    if (!form.produtor_id)                 faltando.push('Dono')
    if (!(form.sku ?? '').trim())          faltando.push('SKU')
    if (!(form.categoria ?? '').trim())    faltando.push('Categoria')
    if (!form.status.trim())               faltando.push('Status')
    if (form.altura === null)              faltando.push('Altura')
    if (form.largura === null)             faltando.push('Largura')
    if (form.comprimento === null)         faltando.push('Comprimento')
    if (form.peso === null)                faltando.push('Peso')
    if (!(form.descricao ?? '').trim())    faltando.push('Descrição')
    if (faltando.length > 0) {
      setErr(`Preencha todos os campos obrigatórios: ${faltando.join(', ')}.`)
      emitToast('err', `Preencha todos os campos obrigatórios: ${faltando.join(', ')}.`)
      return
    }

    setSaving(true)
    setErr('')
    try {
      // FEAT cliente/afiliado→produtor: ao CRIAR um produto para um dono que ainda
      // não é produtor, o produto nasce 'a_aprovar' (fila de aprovação). Ao aprovar,
      // o backend promove o dono a produtor (product_approval.go). Na edição mantém
      // o status escolhido.
      const owner = owners.find(o => o.id === form.produtor_id)
      const needsApproval = !form.id && !!owner && owner.role !== 'produtor'
      const payload = {
        wp_post_id:  null,
        produtor_id: form.produtor_id,
        nome:        form.nome,
        sku:         form.sku || null,
        barcode:     form.barcode || null,
        descricao:   form.descricao || null,
        categoria:   form.categoria || null,
        status:      needsApproval ? 'a_aprovar' : form.status,
        variacao:    (form.variacao ?? '').trim(),
        altura:      form.altura,
        largura:     form.largura,
        comprimento: form.comprimento,
        peso:        form.peso,
        image_url:   (form.image_url ?? '').trim(),
        vitrine_visible: form.vitrine_visible,
        parent_product_id: form.parent_product_id || null,
      }
      if (form.id) {
        await api(`/products/${form.id}`, { method: 'PUT', body: JSON.stringify(payload) })
      } else {
        await api('/products', { method: 'POST', body: JSON.stringify(payload) })
      }
      emitToast(
        'ok',
        form.id
          ? 'Produto atualizado.'
          : needsApproval
            ? `Produto criado e enviado para aprovação. Ao aprovar, ${owner?.nome || owner?.email || 'o dono'} vira produtor.`
            : 'Produto criado.',
      )
      setShowForm(false)
      setForm(emptyForm())
      load()
      loadStats()
    } catch (e: any) { setErr(e.message); emitToast('err', e.message || 'Falha ao salvar produto.') }
    finally { setSaving(false) }
  }

  // Envia o arquivo escolhido (PC do admin) como multipart para o backend, que
  // valida por magic bytes (jpeg/png/webp) e devolve a URL pública. Em sucesso,
  // grava a URL em form.image_url — o save existente persiste em meta.image_url e
  // a prévia <img> abaixo aparece automaticamente. Fetch cru (não api()): precisa
  // do boundary do multipart, então NÃO setamos Content-Type; só replicamos o
  // header Authorization: Bearer <token> que o api() usa (api.ts).
  async function uploadImage(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = '' // permite re-selecionar o mesmo arquivo depois
    if (!file) return
    setUploadingImg(true)
    setErr('')
    try {
      const fd = new FormData()
      fd.append('image', file)
      const tok = getToken()
      const headers: Record<string, string> = {}
      if (tok) headers['Authorization'] = `Bearer ${tok}`
      const res = await fetch(`${BASE}/products/upload-image`, { method: 'POST', headers, body: fd })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body?.error?.message || `HTTP ${res.status}`)
      }
      const data = await res.json() as { ok: boolean; url: string }
      setForm(f => ({ ...f, image_url: data.url }))
      emitToast('ok', 'Imagem enviada.')
    } catch (e: any) {
      setErr(e.message); emitToast('err', e.message || 'Falha ao enviar imagem.')
    } finally {
      setUploadingImg(false)
    }
  }

  async function del(p: Product) {
    if (!await confirmAsync({ message: `Excluir produto "${p.nome}"? Esta ação não pode ser desfeita.`, danger: true })) return
    try {
      await api(`/products/${p.id}`, { method: 'DELETE' })
      emitToast('ok', 'Produto excluído.')
      load()
      loadStats()
    } catch (e: any) { setErr(e.message); emitToast('err', e.message || 'Falha ao excluir produto.') }
  }

  function openCreate() {
    setForm(emptyForm())
    loadProducers()
    loadOwners()
    setShowForm(true)
  }

  function openEdit(p: Product) {
    setForm({
      id:          p.id,
      produtor_id: p.produtor_id,
      nome:        p.nome,
      sku:         p.sku,
      barcode:     p.barcode,
      descricao:   p.descricao,
      categoria:   p.categoria,
      status:      p.status,
      variacao:    p.variacao ?? '',
      altura:      p.altura,
      largura:     p.largura,
      comprimento: p.comprimento,
      peso:        p.peso,
      image_url:   p.image_url ?? '',
      vitrine_visible: p.vitrine_visible ?? true,
      parent_product_id: p.parent_product_id,
    })
    loadProducers()
    loadOwners()
    setShowForm(true)
  }

  // Opções do <select> de categoria: lista fixa MERGE com as categorias já
  // existentes nos produtos carregados (não há endpoint dedicado). Garante que
  // categorias antigas/customizadas continuem disponíveis e nada some.
  const categoriasOpcoes = (() => {
    const set = new Set<string>(CATEGORIAS_FIXAS)
    for (const p of items) {
      const c = (p.categoria ?? '').trim()
      if (c) set.add(c)
    }
    // Garante que a categoria atual do form (ex.: ao editar) esteja na lista.
    const atual = (form.categoria ?? '').trim()
    if (atual) set.add(atual)
    return Array.from(set).sort((a, b) => a.localeCompare(b, 'pt-BR'))
  })()

  const qNorm = q.trim().toLowerCase()
  const filtered = items.filter(p => {
    if (qNorm && !`${p.nome} ${p.sku ?? ''} ${p.barcode ?? ''} ${p.produtor_nome}`.toLowerCase().includes(qNorm)) return false
    if (statusFilter && p.status !== statusFilter) return false
    if (produtorID && String(p.produtor_id) !== produtorID) return false
    return true
  })

  function openPanel() {
    setDraftQ(q); setDraftStatus(statusFilter); setDraftProdutor(produtorID); setDraftIni(dataIni); setDraftFim(dataFim)
    setFilterOpen(true)
  }
  function applyFilters() {
    setQ(draftQ); setStatusFilter(draftStatus); setProdutorID(draftProdutor); setDataIni(draftIni); setDataFim(draftFim)
    setFilterOpen(false)
  }
  function clearFilters() {
    setQ(''); setStatusFilter(''); setProdutorID(''); setDataIni(''); setDataFim('')
    setDraftQ(''); setDraftStatus(''); setDraftProdutor(''); setDraftIni(''); setDraftFim('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (q) chips.push({ key: 'q', label: `Busca: ${q}`, onRemove: () => setQ('') })
  if (statusFilter) chips.push({ key: 'status', label: `Status: ${STATUS_LABEL[statusFilter] ?? statusFilter}`, onRemove: () => setStatusFilter('') })
  if (produtorID) {
    const p = producers.find(pp => String(pp.id) === produtorID)
    chips.push({ key: 'prod', label: `Produtor: ${p?.nome || p?.email || `#${produtorID}`}`, onRemove: () => setProdutorID('') })
  }
  if (dataIni) chips.push({ key: 'ini', label: `De: ${dataIni}`, onRemove: () => setDataIni('') })
  if (dataFim) chips.push({ key: 'fim', label: `Até: ${dataFim}`, onRemove: () => setDataFim('') })
  const activeCount = chips.length

  // Dono selecionado no form + se cair na fila de aprovação (criação p/ não-produtor).
  const selectedOwner = owners.find(o => o.id === form.produtor_id)
  const ownerNeedsApproval = !form.id && !!selectedOwner && selectedOwner.role !== 'produtor'

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Produtos</h1>
          <p>{filtered.length} de {total} produto(s)</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
          {items.length === 0 && (
            <button
              className="szv2-btn szv2-btn-secondary"
              onClick={syncFromOrders}
              disabled={syncing}
              title="Importa produtos do histórico de pedidos"
            >
              {syncing ? 'Sincronizando…' : '🔄 Sincronizar do histórico de pedidos'}
            </button>
          )}
          <button className="szv2-btn szv2-btn-brand" onClick={openCreate}>
            + Novo Produto
          </button>
          <button className="szv2-btn szv2-btn-secondary" onClick={() => openEntrada()}>
            + Entrada de estoque
          </button>
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* KPIs */}
      {stats && (
        <div
          className="szv2-kpi-grid"
          style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))', gap: 16, marginBottom: 16 }}
        >
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Produtos ativos</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>
                {stats.active_count.toLocaleString('pt-BR')}
              </span>
              <span className="szv2-kpi-meta">com status = ativo</span>
            </div>
          </div>
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Total afiliados vinculados</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-success)' }}>
                {stats.total_affiliates.toLocaleString('pt-BR')}
              </span>
              <span className="szv2-kpi-meta">vínculos produtor ↔ afiliado</span>
            </div>
          </div>
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Receita 30d</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>
                {fmtBRL(stats.revenue_30d)}
              </span>
              <span className="szv2-kpi-meta">soma dos itens dos pedidos</span>
            </div>
          </div>
        </div>
      )}

      {/* Form em MODAL overlay — não empurra nem esconde a tabela (clique fora fecha). */}
      {showForm && (
        <div
          onClick={() => setShowForm(false)}
          style={{ position: 'fixed', inset: 0, zIndex: 1000, background: 'rgba(0,0,0,.5)', display: 'flex', alignItems: 'flex-start', justifyContent: 'center', overflowY: 'auto', padding: 24 }}
        >
        <div className="szv2-card" onClick={e => e.stopPropagation()} style={{ maxWidth: 920, width: '100%', marginTop: 24, marginBottom: 24 }}>
          <div className="szv2-card-head">
            <div><h2>{form.id ? 'Editar Produto' : 'Novo Produto'}</h2></div>
            <button className="szv2-modal-x" onClick={() => setShowForm(false)}>✕</button>
          </div>
          <form onSubmit={save}>
            <div className="sz-form-grid sz-form-grid-3" style={{ marginBottom: 16 }}>
              {!form.id && (
                <div className="szv2-field" style={{ gridColumn: '1 / 4' }}>
                  <label className="szv2-label">Criar variação de… (opcional)</label>
                  <FalkSelect
                    aria-label="Produto base para variação"
                    placeholder="Produto solto (sem base)…"
                    value={String(form.parent_product_id || '')}
                    onChange={v => {
                      const base = items.find(p => p.id === Number(v))
                      if (!base) { setForm(f => ({ ...f, parent_product_id: null })); return }
                      // Prefill dos campos compartilhados do produto base — o admin só
                      // ajusta SKU/variação/estoque próprios da nova variação.
                      setForm(f => ({
                        ...f,
                        parent_product_id: base.id,
                        produtor_id: base.produtor_id,
                        nome:        base.nome,
                        categoria:   base.categoria,
                        descricao:   base.descricao,
                        altura:      base.altura,
                        largura:     base.largura,
                        comprimento: base.comprimento,
                        peso:        base.peso,
                        sku:         null,
                        barcode:     null,
                        variacao:    '',
                      }))
                    }}
                    options={[
                      { value: '', label: 'Produto solto (sem base)…' },
                      ...items.map(p => ({ value: String(p.id), label: `#${p.id} — ${p.nome}${p.variacao ? ` (${p.variacao})` : ''}` })),
                    ]}
                  />
                </div>
              )}
              <div className="szv2-field" style={{ gridColumn: '1 / 3' }}>
                <label className="szv2-label">Nome *</label>
                <input
                  className="szv2-input"
                  required
                  value={form.nome}
                  onChange={e => setForm({ ...form, nome: e.target.value })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Dono *</label>
                <FalkSelect
                  aria-label="Dono do produto"
                  placeholder="Selecione…"
                  value={String(form.produtor_id || '')}
                  onChange={v => setForm({ ...form, produtor_id: Number(v) })}
                  options={[
                    { value: '', label: 'Selecione…' },
                    // Qualquer cliente/afiliado/produtor pode ser dono. O papel vai no
                    // rótulo p/ o admin saber quem ainda será promovido a produtor.
                    ...owners.map(o => ({
                      value: String(o.id),
                      label: `${o.nome || '(sem nome)'} · ${o.email || '(sem e-mail)'}${o.role ? ` · ${ROLE_LABEL[o.role] ?? o.role}` : ''}`,
                    })),
                  ]}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">SKU *</label>
                <input
                  className="szv2-input"
                  required
                  value={form.sku ?? ''}
                  onChange={e => setForm({ ...form, sku: e.target.value || null })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Código de barras</label>
                <input
                  className="szv2-input"
                  inputMode="numeric"
                  placeholder="EAN / UPC / código interno"
                  value={form.barcode ?? ''}
                  onChange={e => setForm({ ...form, barcode: e.target.value || null })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Categoria *</label>
                <FalkSelect
                  aria-label="Categoria"
                  placeholder="Selecione…"
                  value={form.categoria ?? ''}
                  onChange={v => setForm({ ...form, categoria: v || null })}
                  options={[
                    { value: '', label: 'Selecione…' },
                    ...categoriasOpcoes.map(c => ({ value: c, label: c })),
                  ]}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Status *</label>
                {ownerNeedsApproval ? (
                  // Dono ainda não é produtor → produto entra na fila de aprovação.
                  // Mostra um aviso no lugar do seletor (o status enviado é 'a_aprovar').
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <span className="sz-badge szv2-badge-warning" style={{ alignSelf: 'flex-start' }}>
                      A aprovar
                    </span>
                    <p style={{ margin: 0, fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                      O dono ainda não é produtor — o produto entra na fila de aprovação. Ao
                      aprovar, ele é promovido a produtor automaticamente.
                    </p>
                  </div>
                ) : (
                  <FalkSelect
                    aria-label="Status"
                    value={form.status}
                    onChange={v => setForm({ ...form, status: v })}
                    options={STATUSES.map(s => ({ value: s, label: STATUS_LABEL[s] }))}
                  />
                )}
              </div>

              {/* Variação — atributo do produto (regra do dono). Texto livre; a lista
                  de pedidos lê esta variação do produto do 1º item (ex.: Datalaprox = pote). */}
              <div className="szv2-field">
                <label className="szv2-label">Variação</label>
                <input
                  className="szv2-input"
                  placeholder="ex.: pote, cápsula, sachê"
                  value={form.variacao}
                  onChange={e => setForm({ ...form, variacao: e.target.value })}
                />
              </div>

              {/* Imagem (URL) — OPCIONAL. Gravada em sz_products.meta (chave image_url),
                  a MESMA que o checkout lê (resolveProductImage) → a miniatura aparece no
                  checkout. Mostra prévia da imagem atual quando há valor. */}
              <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
                <label className="szv2-label">Imagem</label>
                <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
                  <input
                    className="szv2-input"
                    type="url"
                    placeholder="https://… ou envie um arquivo (opcional)"
                    value={form.image_url}
                    onChange={e => setForm({ ...form, image_url: e.target.value })}
                    style={{ flex: 1 }}
                  />
                  {(form.image_url ?? '').trim() !== '' && (
                    <img
                      src={form.image_url}
                      alt="Prévia da imagem do produto"
                      style={{
                        width: 56,
                        height: 56,
                        objectFit: 'cover',
                        borderRadius: 8,
                        border: '1px solid var(--szv2-border)',
                        flexShrink: 0,
                      }}
                      onError={e => { (e.currentTarget as HTMLImageElement).style.display = 'none' }}
                    />
                  )}
                </div>
                {/* Enviar imagem do PC — POSTa o arquivo (multipart) ao backend, que
                    valida por magic bytes e devolve a URL; o sucesso preenche o campo
                    acima (e a prévia). Mantém o campo de URL: colar URL também funciona. */}
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 8 }}>
                  <label
                    className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                    style={{ cursor: uploadingImg ? 'wait' : 'pointer', margin: 0 }}
                  >
                    {uploadingImg ? 'Enviando…' : '⬆ Enviar imagem'}
                    <input
                      type="file"
                      accept="image/jpeg,image/png,image/webp"
                      disabled={uploadingImg}
                      onChange={uploadImage}
                      style={{ display: 'none' }}
                    />
                  </label>
                  <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    JPEG, PNG ou WEBP até 8MB.
                  </span>
                </div>
                <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  Opcional — cole uma URL ou envie um arquivo. Aparece como miniatura no checkout deste produto.
                </p>
              </div>

              {/* Visível na vitrine — toggle (migração 479). Liga/desliga a aparição do
                  produto na Vitrine do portal (descoberta de afiliados). Desligado some da
                  vitrine, mas continua nesta lista do admin p/ poder religar. */}
              <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 14, cursor: 'pointer', color: 'var(--szv2-text-soft)' }}>
                  <input
                    type="checkbox"
                    checked={form.vitrine_visible}
                    onChange={e => setForm({ ...form, vitrine_visible: e.target.checked })}
                  />
                  Visível na vitrine
                </label>
                <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  Quando desligado, o produto não aparece na Vitrine do portal (afiliados/produtores). Continua visível aqui no admin.
                </p>
              </div>

              {/* Dimensões físicas — o produto tem SÓ isto, sem preço (regra v469).
                  Vazio = não informado (null), nunca 0. */}
              <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
                <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  Dimensões do pacote — o preço de venda é definido no link de oferta, não no produto.
                </p>
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Altura (cm) *</label>
                <input
                  className="szv2-input"
                  type="number"
                  step="0.01"
                  min="0"
                  required
                  value={form.altura ?? ''}
                  onChange={e => setForm({ ...form, altura: numOrNull(e.target.value) })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Largura (cm) *</label>
                <input
                  className="szv2-input"
                  type="number"
                  step="0.01"
                  min="0"
                  required
                  value={form.largura ?? ''}
                  onChange={e => setForm({ ...form, largura: numOrNull(e.target.value) })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Comprimento (cm) *</label>
                <input
                  className="szv2-input"
                  type="number"
                  step="0.01"
                  min="0"
                  required
                  value={form.comprimento ?? ''}
                  onChange={e => setForm({ ...form, comprimento: numOrNull(e.target.value) })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Peso (kg) *</label>
                <input
                  className="szv2-input"
                  type="number"
                  step="0.001"
                  min="0"
                  required
                  value={form.peso ?? ''}
                  onChange={e => setForm({ ...form, peso: numOrNull(e.target.value) })}
                />
              </div>

              <div className="szv2-field" style={{ gridColumn: '1 / -1' }}>
                <label className="szv2-label">Descrição *</label>
                <textarea
                  className="szv2-input"
                  rows={3}
                  required
                  value={form.descricao ?? ''}
                  onChange={e => setForm({ ...form, descricao: e.target.value || null })}
                />
              </div>
            </div>
            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={saving}>
                {saving ? 'Salvando…' : form.id ? 'Salvar Alterações' : 'Criar Produto'}
              </button>
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setShowForm(false)}>
                Cancelar
              </button>
            </div>
          </form>
        </div>
        </div>
      )}

      {/* ── Bipagem de entrada de estoque ────────────────────────────────────
          Dá entrada por SKU (código de barras). Loop: após cada bipada, limpa
          SKU/quantidade e refoca — pensado pra leitor de código de barras. */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
          <h2 style={{ margin: 0, fontSize: 16 }}>Bipagem de entrada (estoque)</h2>
          <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
            Bipe ou digite o SKU para dar entrada no estoque
          </span>
        </div>
        <form onSubmit={biparEntrada} style={{ display: 'flex', gap: 12, alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <div className="szv2-field" style={{ minWidth: 200 }}>
            <label className="szv2-label">Produtor *</label>
            <FalkSelect
              value={bipProducerId}
              onChange={v => setBipProducerId(v)}
              placeholder="Selecione o produtor…"
              aria-label="Produtor"
              options={[
                { value: '', label: 'Selecione o produtor…' },
                ...producers.map(p => ({ value: String(p.id), label: p.nome || `Produtor #${p.id}` })),
              ]}
            />
          </div>
          <div className="szv2-field" style={{ flex: 1, minWidth: 220 }}>
            <label className="szv2-label">SKU (código de barras) *</label>
            <input
              ref={bipSkuRef}
              className="szv2-input"
              type="text"
              inputMode="text"
              autoComplete="off"
              value={bipSku}
              onChange={e => setBipSku(e.target.value)}
              placeholder="Bipe o código de barras ou digite o SKU…"
              style={{ fontFamily: 'var(--szv2-font-mono)' }}
            />
          </div>
          <div className="szv2-field" style={{ width: 120 }}>
            <label className="szv2-label">Quantidade *</label>
            <input
              className="szv2-input"
              type="number"
              min="1"
              value={bipQty}
              onChange={e => setBipQty(e.target.value)}
              placeholder="1"
            />
          </div>
          <button type="submit" className="szv2-btn szv2-btn-brand" disabled={bipBusy} style={{ minWidth: 160 }}>
            {bipBusy ? 'Dando entrada…' : 'Dar entrada (bipar)'}
          </button>
        </form>
        {bipErr && <div className="sz-alert-danger" style={{ marginTop: 12 }}>{bipErr}</div>}
        {bipSaldo && !bipErr && (
          <div
            className="sz-alert-success"
            style={{ marginTop: 12, padding: '8px 12px', borderRadius: 8, fontSize: 13, background: 'rgba(30,111,242,.08)', color: 'var(--falkz, #1E6FF2)', fontWeight: 600 }}
          >
            ✓ {bipSaldo}
          </div>
        )}
      </div>

      {/* Tabela */}
      {loading && items.length === 0 && <TableSkeleton rows={6} cols={6} />}

      {!loading && filtered.length === 0 && (
        <div className="szv2-card" style={{ textAlign: 'center', padding: 48, color: 'var(--szv2-text-muted)' }}>
          <p style={{ fontSize: 32, margin: '0 0 8px' }}>📦</p>
          <p style={{ margin: '0 0 16px' }}>
            Nenhum produto cadastrado ainda. Importe automaticamente dos pedidos existentes ou crie manualmente.
          </p>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'center', flexWrap: 'wrap' }}>
            <button
              className="szv2-btn szv2-btn-secondary"
              onClick={syncFromOrders}
              disabled={syncing}
            >
              {syncing ? 'Sincronizando…' : '🔄 Sincronizar do histórico de pedidos'}
            </button>
            <button className="szv2-btn szv2-btn-brand" onClick={openCreate}>
              + Novo Produto
            </button>
          </div>
        </div>
      )}

      {filtered.length > 0 && (
        <div className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
          <table className="szv2-table" style={{ width: '100%' }}>
            <thead>
              <tr>
                <th>ID</th>
                <th>Produtor</th>
                <th>Nome</th>
                <th>SKU</th>
                <th>Peso</th>
                <th>Categoria</th>
                <th>Status</th>
                <th>Vitrine</th>
                <th>Afiliados</th>
                <th style={{ textAlign: 'right' }}>Estoque</th>
                <th style={{ textAlign: 'right' }}>Reservado</th>
                <th style={{ textAlign: 'right' }}>Total</th>
                <th>Data</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(p => {
                // Chave de estoque = COALESCE(wp_post_id, id) — mesma regra do backend
                // (stock.go stockProductNameExpr / Bipar): produto sincronizado do WP usa
                // wp_post_id, produto nativo usa id.
                const stockKey = p.wp_post_id || p.id
                const stock = stockByProduct[stockKey]
                return (
                <tr key={p.id}>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>#{p.id}</td>
                  <td style={{ maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {p.produtor_nome || '—'}
                  </td>
                  <td style={{ fontWeight: 600 }}>
                    {p.nome}
                    {p.variacao && ` (${p.variacao})`}
                  </td>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>{p.sku ?? '—'}</td>
                  <td style={{ fontSize: 12, whiteSpace: 'nowrap' }}>{fmtDim(p.peso, 'kg')}</td>
                  <td>{p.categoria ?? '—'}</td>
                  <td>
                    <span className={`sz-badge ${STATUS_CLASS[p.status] ?? 'szv2-badge-neutral'}`}>
                      {STATUS_LABEL[p.status] ?? p.status}
                    </span>
                  </td>
                  <td>
                    <span className={`sz-badge ${p.vitrine_visible ? 'szv2-badge-success' : 'szv2-badge-neutral'}`}>
                      {p.vitrine_visible ? 'Visível' : 'Oculto'}
                    </span>
                  </td>
                  <td style={{ textAlign: 'center' }}>{p.afiliados_count}</td>
                  <td style={{ textAlign: 'right' }}>
                    {stock ? (
                      <span
                        className={`sz-badge ${isLow(stock) ? 'szv2-badge-danger' : 'szv2-badge-brand'}`}
                        title={isLow(stock) ? 'Vendável abaixo ou igual ao mínimo' : 'Estoque saudável'}
                      >
                        {stock.qty_sellable}
                      </span>
                    ) : '—'}
                  </td>
                  <td style={{ textAlign: 'right' }}>{stock ? stock.qty_reserved : '—'}</td>
                  <td style={{ textAlign: 'right' }}>{stock ? stock.qty_available : '—'}</td>
                  <td style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    {p.created_at ? brDate(p.created_at) : '—'}
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                        onClick={() => openEdit(p)}
                      >
                        Editar
                      </button>
                      {stock ? (
                        <>
                          <button className="szv2-btn szv2-btn-sm szv2-btn-secondary" onClick={() => openAdjust(stock)}>
                            Ajustar estoque
                          </button>
                          <button className="szv2-btn szv2-btn-sm szv2-btn-secondary" onClick={() => openMovs(stock)}>
                            Movimentos
                          </button>
                        </>
                      ) : (
                        <button className="szv2-btn szv2-btn-sm szv2-btn-secondary" onClick={() => openEntrada(stockKey)}>
                          + Estoque
                        </button>
                      )}
                      <button
                        className="szv2-btn szv2-btn-sm szv2-btn-danger"
                        onClick={() => del(p)}
                      >
                        Excluir
                      </button>
                    </div>
                  </td>
                </tr>
              )})}
            </tbody>
          </table>
        </div>
      )}

      {/* ── Drawer: Entrada de estoque ───────────────────────────────────── */}
      <DetailDrawer
        open={showEntrada}
        onClose={() => setShowEntrada(false)}
        title="Nova entrada de estoque"
        footer={
          <>
            <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setShowEntrada(false)}>
              Cancelar
            </button>
            <button type="submit" form="sz-stock-entrada-form" className="szv2-btn szv2-btn-brand" disabled={savingEntrada}>
              {savingEntrada ? 'Salvando…' : 'Registrar entrada'}
            </button>
          </>
        }
      >
        <form id="sz-stock-entrada-form" onSubmit={saveEntrada}>
          {stockErr && showEntrada && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{stockErr}</div>}
          <div className="szv2-field" style={{ marginBottom: 12 }}>
            <label className="szv2-label">Produto / variação *</label>
            <FalkSelect
              aria-label="Produto ou variação"
              placeholder="Selecione…"
              value={entProductId}
              onChange={v => setEntProductId(v)}
              options={[
                { value: '', label: 'Selecione…' },
                ...items.map(p => ({
                  value: String(p.id),
                  label: `#${p.id} — ${p.nome}${p.variacao ? ` (${p.variacao})` : ''}`,
                })),
              ]}
            />
          </div>
          <div className="szv2-field" style={{ marginBottom: 12 }}>
            <label className="szv2-label">Centro de distribuição</label>
            <FalkSelect
              value={entCdId}
              onChange={v => setEntCdId(v)}
              aria-label="Centro de distribuição"
              placeholder="Selecione…"
              options={cds.map(c => ({ value: String(c.id), label: c.nome }))}
            />
          </div>
          <div className="szv2-field" style={{ marginBottom: 12 }}>
            <label className="szv2-label">Quantidade disponível *</label>
            <input
              className="szv2-input"
              type="number"
              min="0"
              value={entQty}
              onChange={e => setEntQty(e.target.value)}
              placeholder="contagem total atual"
            />
          </div>
          <div className="szv2-field">
            <label className="szv2-label">Estoque mínimo</label>
            <input
              className="szv2-input"
              type="number"
              min="0"
              value={entMin}
              onChange={e => setEntMin(e.target.value)}
              placeholder="alerta de estoque baixo"
            />
          </div>
          <p style={{ margin: '12px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
            A entrada define a contagem total do item. Se já existir estoque para esta
            combinação produto/variação/CD, o valor é sobrescrito e o movimento registra a diferença.
          </p>
        </form>
      </DetailDrawer>

      {/* ── Drawer: Ajuste de estoque ────────────────────────────────────── */}
      <DetailDrawer
        open={!!adjustItem}
        onClose={() => setAdjustItem(null)}
        title={adjustItem ? `Ajustar estoque — ${adjustItem.product_name || `Produto #${adjustItem.product_id}`}` : 'Ajustar estoque'}
        footer={
          <>
            <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setAdjustItem(null)}>
              Cancelar
            </button>
            <button type="submit" form="sz-stock-ajuste-form" className="szv2-btn szv2-btn-brand" disabled={savingAdj}>
              {savingAdj ? 'Aplicando…' : 'Aplicar ajuste'}
            </button>
          </>
        }
      >
        {adjustItem && (
          <form id="sz-stock-ajuste-form" onSubmit={saveAdjust}>
            {stockErr && adjustItem && <div className="sz-alert-danger" style={{ marginBottom: 12 }}>{stockErr}</div>}
            <div style={{ display: 'flex', gap: 16, marginBottom: 16, padding: 12, background: 'var(--szv2-surface-alt)', borderRadius: 8, fontSize: 13 }}>
              <span>Disponível atual: <strong>{adjustItem.qty_available}</strong></span>
              <span style={{ color: 'var(--szv2-text-muted)' }}>Reservado: {adjustItem.qty_reserved}</span>
              <span>Vendável: <strong>{adjustItem.qty_sellable}</strong></span>
            </div>
            <div className="szv2-field" style={{ marginBottom: 12 }}>
              <label className="szv2-label">Delta (+ entrada / − baixa) *</label>
              <input
                className="szv2-input"
                type="number"
                value={adjDelta}
                onChange={e => setAdjDelta(e.target.value)}
                placeholder="ex: +10 ou -3"
                autoFocus
              />
              <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                Novo disponível: <strong>{Math.max(0, adjustItem.qty_available + intOf(adjDelta))}</strong>
              </p>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Motivo *</label>
              <textarea
                className="szv2-input"
                rows={3}
                value={adjMotivo}
                onChange={e => setAdjMotivo(e.target.value)}
                placeholder="ex: contagem física, avaria, devolução…"
              />
            </div>
          </form>
        )}
      </DetailDrawer>

      {/* ── Drawer: Movimentos de estoque ────────────────────────────────── */}
      <DetailDrawer
        open={!!movItem}
        onClose={() => setMovItem(null)}
        large
        title={movItem ? `Movimentos — ${movItem.product_name || `Produto #${movItem.product_id}`}` : 'Movimentos'}
        footer={
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setMovItem(null)}>
            Fechar
          </button>
        }
      >
        {loadingMovs ? (
          <TableSkeleton rows={5} cols={5} />
        ) : movs.length === 0 ? (
          <EmptyState icon="🗒️" title="Nenhuma movimentação registrada para este item." />
        ) : (
          <div className="szv2-table-wrap">
            <table className="szv2-table" style={{ width: '100%' }}>
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Tipo</th>
                  <th style={{ textAlign: 'right' }}>Δ Disponível</th>
                  <th style={{ textAlign: 'right' }}>Δ Reservado</th>
                  <th>Motivo</th>
                  <th>Pedido</th>
                </tr>
              </thead>
              <tbody>
                {movs.map(m => (
                  <tr key={m.id}>
                    <td style={{ fontSize: 12, whiteSpace: 'nowrap' }}>{fmtDateTime(m.created_at)}</td>
                    <td><span className="sz-badge szv2-badge-neutral">{MOV_TIPO_LABEL[m.tipo] ?? m.tipo}</span></td>
                    <td style={{ textAlign: 'right', fontWeight: 600, color: m.delta_available < 0 ? 'var(--szv2-danger)' : 'var(--szv2-brand)' }}>
                      {m.delta_available > 0 ? '+' : ''}{m.delta_available}
                    </td>
                    <td style={{ textAlign: 'right', color: 'var(--szv2-text-muted)' }}>
                      {m.delta_reserved > 0 ? '+' : ''}{m.delta_reserved}
                    </td>
                    <td style={{ fontSize: 13 }}>{m.motivo || '—'}</td>
                    <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12 }}>{m.order_id ? `${m.order_id}` : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </DetailDrawer>

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
            aria-label="Status"
            value={draftStatus}
            onChange={v => setDraftStatus(v)}
            options={[
              { value: '', label: 'Todos' },
              ...STATUSES.map(s => ({ value: s, label: STATUS_LABEL[s] })),
            ]}
          />
        </FilterField>
        <FilterField label="Produtor">
          <FalkSelect
            aria-label="Produtor"
            value={draftProdutor}
            onChange={v => setDraftProdutor(v)}
            options={[
              { value: '', label: 'Todos' },
              ...producers.map(p => ({ value: String(p.id), label: p.nome || p.email })),
            ]}
          />
        </FilterField>
        <FilterField label="Busca (nome / SKU)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex: Curso, Recarga…"
            value={draftQ}
            onChange={e => setDraftQ(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
