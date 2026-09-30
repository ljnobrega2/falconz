// Produtos — porte fiel de templates/portal/v2/sections/products.php para React.
// Ligada ao go/portal:
//   GET  /portal/products                         — lista produtos do usuário (escopo por role)
//   GET  /portal/products/cds                      — CDs reais (sz_motoboy_cds) p/ o seletor de CD
//   GET  /portal/products/{id}                     — detalhe (não usado aqui: a lista já traz tudo)
//   POST /portal/products/{id}/vitrine-description — salva descrição da vitrine (só produtor dono)
//
// UX/UI espelha a section WP: seletor de CD → abas por produto → cabeçalho do
// produto → 5 KPI cards → sub-abas (Checkouts / Envios / Movimentações).
//
// DESVIOS FIÉIS (limitações do schema PG migrado — documentadas no handler Go):
//   - KPIs físicos (Disponíveis/Reservados/Em rota/Entregues/Frustrados) vêm 0
//     porque sz_motoboy_stock_custody não existe no PG. Os 5 cards renderizam com 0.
//   - Sub-abas "Envios" e "Movimentações" mostram empty state (tabelas/funcs ausentes no PG).
//   - Ações de checkout (comissão %, toggle afiliado, excluir) reaproveitam os
//     endpoints já existentes em /portal/links/{id}/* — o `id` do productCheckout
//     É o cl.id (attachCheckouts seleciona cl.id), então não há endpoint novo:
//       POST   /portal/links/{id}/affiliate-toggle  — liga/desliga visível p/ afiliado
//       POST   /portal/links/{id}/commission        — salva comissão % (coluna dedicada, live)
//       DELETE /portal/links/{id}                    — exclui o checkout
//   - "+ Gerar checkout": fluxo REAL — POST /portal/links (links_portal.go::Create) faz
//     INSERT em senderzz_checkout_links. O drawer envia product_id (=active.id), name,
//     price e affiliate_commission_pct (pré-preenchido c/ o padrão do produtor;
//     override confirmado se alterado). NÃO envia mais `tipo`: o backend cria 1 ou 3
//     links AUTOMATICAMENTE conforme a expedição ativa do produtor
//     (settings->>'expedicao_ativa') — ativa → Expedição + Cash on Delivery + Link único; inativa →
//     só Cash on Delivery. "+ Adicionar produto": POST /portal/products.
//   - LINKS por checkout: o produtor vê cada link individualmente (Expedição,
//     Cash on Delivery e Link único); o afiliado recebe apenas Cash on Delivery.
import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { sanitizeProductHtml } from '../utils/sanitize'
import { useToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'
import Drawer from '../components/Drawer'
import EmptyState from '../components/EmptyState'
import SectionLoading from '../components/SectionLoading'
import AlertError from '../components/AlertError'
import FalkSelect from '../components/FalkSelect'

// ── Shapes (espelham go/portal/internal/handlers/products.go) ───────────────────

type ProductCheckout = {
  id: number
  name: string
  price_label: string
  display_value: number
  tipo: string
  url: string
  // cod_url — link do checkout Cash on Delivery (motoboy) PAREADO a esta oferta,
  // resolvido no backend por nome (cl.name + " — Motoboy"). "" quando não há par
  // COD. Pode vir ausente do backend (tolerante). Quando o produtor NÃO tem
  // expedição, o backend devolve a linha com cod_url preenchido e url vazia.
  cod_url?: string | null
  slug: string
  affiliate_visible: boolean
  affiliate_commission_pct: number
}

type ProductCheckoutGroup = {
  key: string
  links: ProductCheckout[]
  primary: ProductCheckout
}

type ProductItem = {
  id: number
  wp_post_id: number | null
  name: string
  sku: string | null
  descricao: string | null
  categoria: string | null
  status: string
  altura: number | null
  largura: number | null
  comprimento: number | null
  peso: number | null
  // custo — COGS do produto (FEAT-CRM-FINANCEIRO 2026-07-28). Privado: só o
  // próprio produtor (+ admin) vê. null = não informado.
  custo?: number | null
  // image — URL da imagem do produto (a MESMA do WP). Vem de sz_products.meta
  // (best-effort, mesmas chaves do admin/checkout). "" quando o meta não traz
  // imagem → cai no placeholder SVG. Pode vir ausente do backend (tolerante).
  image?: string | null
  // variacao — atributo livre do produto (sz_products.variacao). Tratado como lista
  // separada por vírgula: o SELECT de variação no modal "Gerar checkout" só aparece
  // quando há MAIS DE UMA opção (splitVariacoes(...).length > 1). FORWARD-COMPAT:
  // products.go::List ainda NÃO seleciona sp.variacao (gap de backend documentado no
  // relatório — análogo ao gap `imagem`), então hoje chega undefined e o select fica
  // DORMENTE. Quando o backend adicionar a coluna ao SELECT, o select passa a renderizar
  // sem tocar este arquivo. O backend de Create já lê a variação autoritativamente.
  variacao?: string | null
  available: number
  reserved: number
  route: number
  delivered: number
  frustrated: number
  vitrine_visible: boolean
  // Tolerante de propósito: o backend deveria mandar [], mas guardamos contra
  // null/undefined no wire para nunca quebrar o render (resiliência).
  checkouts: ProductCheckout[] | null
}

type CD = { id: number; nome: string; ativo: boolean }

// Envelopes tolerantes: data pode vir null/ausente do backend — guardado no .then.
type ListResp = { ok?: boolean; data?: ProductItem[] | null; total?: number }
type CDResp = { ok?: boolean; data?: CD[] | null; total?: number }
type MeResp = { role?: string }

type SubTab = 'checkouts' | 'envios' | 'movs'

// ── Categorias pré-definidas (lista fixa — best practices e-commerce/logística) ──
// O produtor SELECIONA de uma lista comum; não digita texto livre (evita ruído de
// catálogo). O backend (sz_products.categoria VARCHAR) aceita qualquer string — a
// restrição é só no front. Valor enviado = o próprio rótulo (PT-BR).
const PRODUCT_CATEGORIES = [
  'Eletrônicos',
  'Moda e Vestuário',
  'Beleza e Cuidados Pessoais',
  'Casa e Decoração',
  'Saúde e Bem-estar',
  'Alimentos e Bebidas',
  'Suplementos',
  'Infantil e Bebês',
  'Pet',
  'Esporte e Lazer',
  'Livros e Papelaria',
  'Ferramentas e Construção',
  'Automotivo',
  'Outros',
] as const

// splitVariacoes — interpreta sz_products.variacao (texto livre) como lista separada
// por vírgula. O SELECT de variação no modal "Gerar checkout" aparece quando há AO
// MENOS UMA opção (>= 1) e, nesse caso, é OBRIGATÓRIO (REGRA DO DONO 2026-06-26: sem
// a opção "Sem variação"). Datalaprox="pote" → 1 opção (mostra select, obriga "pote");
// ""=0 (sem select → oferta sem variação). O backend (products.go::List) JÁ envia
// sp.variacao, então o select é funcional (não é mais dormente).
function splitVariacoes(v?: string | null): string[] {
  return String(v || '')
    .split(',')
    .map(s => s.trim())
    .filter(Boolean)
}

// offerQty — extrai a QUANTIDADE (número à frente do nome da oferta: "3 Potes…" → 3,
// "1 Datalaprox" → 1). Usada pelo filtro de quantidade dos checkouts. Sem número à
// frente (ex.: "Dorvax") → null (cai no grupo "Outros").
function offerQty(name: string): number | null {
  const m = /^\s*(\d+)\b/.exec(name || '')
  return m ? parseInt(m[1], 10) : null
}

// ── Card de KPI físico — espelha sz_v2_kpi_card() (div.szv2-card.szv2-kpi) ──────
function KpiCard({ label, value }: { label: string; value: number }) {
  return (
    <div className="szv2-card szv2-kpi">
      <span className="szv2-kpi-label">{label}</span>
      <span className="szv2-kpi-value szv2-num">{String(value)}</span>
      <span className="szv2-kpi-meta">unidades</span>
    </div>
  )
}

export default function Products() {
  const toast = useToast()
  const [items, setItems] = useState<ProductItem[]>([])
  const [cds, setCds] = useState<CD[]>([])
  const [role, setRole] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Seleção de CD (filtro visual — espelha o seletor "antes dos produtos").
  const [cdFilter, setCdFilter] = useState<number>(0)
  // Aba de produto ativa (data-prod).
  const [activeProd, setActiveProd] = useState<number>(0)
  // Sub-aba ativa por produto (Checkouts / Envios / Movimentações).
  const [subTab, setSubTab] = useState<Record<number, SubTab>>({})

  // Edição da descrição da vitrine (só produtor dono).
  const [descDraft, setDescDraft] = useState<Record<number, string>>({})
  const [savingDesc, setSavingDesc] = useState<number>(0)

  // Rascunho de comissão por checkout (input controlado) — chave = checkout id (cl.id).
  const [commDraft, setCommDraft] = useState<Record<number, string>>({})
  // Linhas com a comissão em modo de EDIÇÃO (chave = checkout id). Fora desse modo a
  // célula mostra só o valor + um lápis discreto; clicar no lápis abre input + Salvar.
  const [commEditing, setCommEditing] = useState<Record<number, boolean>>({})

  // Checkouts carregados sob demanda por produto (GET /portal/products/{id}/checkouts).
  // O attach do List NÃO casa as ofertas migradas (cl.post_id é o CANAL, não o
  // wp_post_id) — então buscamos os checkouts do produto pelo endpoint dedicado
  // (escopado ao dono) quando ele fica ativo. checkoutsLoaded marca quais produtos
  // já tiveram a busca concluída (evita refetch); checkoutsLoading mostra o spinner.
  const [checkoutsLoaded, setCheckoutsLoaded] = useState<Record<number, boolean>>({})
  const [checkoutsLoading, setCheckoutsLoading] = useState<Record<number, boolean>>({})
  // Filtro de QUANTIDADE da tabela de checkouts (null = todos; -1 = "Outros" sem nº).
  // Default = null/todos (pedido do dono 2026-07-17: não focar de cara na oferta de
  // 1 unidade — mostra tudo que existir, os chips deixam o produtor filtrar se quiser).
  // Reseta ao trocar de produto ativo (effect abaixo).
  const [qtyFilter, setQtyFilter] = useState<number | null>(null)
  // Ações em andamento — chaves "toggle-<id>", "comm-<id>", "del-<id>".
  const [busy, setBusy] = useState<Record<string, boolean>>({})
  function setBusyKey(key: string, v: boolean) {
    setBusy(prev => ({ ...prev, [key]: v }))
  }
  const [detailsDrawerOpen, setDetailsDrawerOpen] = useState(false)

  // ── Criar produto — drawer lateral (POST /portal/products) ────────────────────
  // ESCOPO DO PRODUTOR (decisão do dono): o produtor cadastra SÓ a parte comercial
  // do produto — nome (obrigatório), categoria (select, obrigatório), descrição
  // (aceita shortcode) e imagem (URL). SKU, dimensões (altura/largura/comprimento)
  // e peso são preenchidos EXCLUSIVAMENTE pelo ADMIN (são dados logísticos) e por
  // isso NÃO aparecem neste form — o backend trata esses campos como NULL na criação.
  // Comissão %/CD também não entram aqui (comissão vive no link de oferta; CD não é
  // campo de produto).
  //
  // GAP DE BACKEND (imagem): o handler Go products.go::Create NÃO tem campo de
  // imagem — createProductRequest não declara `imagem` e o INSERT não escreve em
  // sz_products.meta (a coluna JSONB existe mas não é alimentada). O decoder do Go
  // descarta chaves desconhecidas SILENCIOSAMENTE. Para NUNCA fingir sucesso:
  //   - enviamos a URL sob a chave `imagem` (forward-compat: quando o backend
  //     declarar o campo + persistir em meta, passa a funcionar sem mexer no front);
  //   - avisamos o usuário no form que a imagem só é exibida após o ajuste do
  //     backend (nota visível abaixo do campo). Ver relatório.
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  // Rascunho do form — tudo string (input controlado).
  const emptyCreateForm = {
    nome: '',
    descricao: '',
    categoria: '',
    imagem: '',
    custo: '',
  }
  const [createForm, setCreateForm] = useState<Record<string, string>>(emptyCreateForm)
  // Flag de erro da pré-visualização da imagem — controlada por state (não por DOM)
  // p/ que uma URL válida subsequente volte a tentar carregar (sem mutar style).
  const [imgPreviewError, setImgPreviewError] = useState(false)
  function setCreateField(key: string, v: string) {
    setCreateForm(prev => ({ ...prev, [key]: v }))
    // Ao mudar a URL da imagem, reseta o erro de preview p/ revalidar a nova URL.
    if (key === 'imagem') setImgPreviewError(false)
  }
  function openCreate() {
    setCreateForm(emptyCreateForm)
    setImgPreviewError(false)
    setCreateOpen(true)
  }

  // ── Gerar checkout/oferta — drawer lateral (POST /portal/links) ────────────────
  // FEAT-PORTAL-SALES: o backend Go expõe POST /portal/links (links_portal.go::Create)
  // que faz INSERT REAL em senderzz_checkout_links. O front estava com stub
  // (notImplemented) por uma doc desatualizada — agora é fluxo real.
  //
  // GOVERNANÇA da comissão (modelo confirmado pelo dono):
  //   - O campo "Comissão do afiliado (%)" vem PRÉ-PREENCHIDO com a comissão PADRÃO
  //     do produtor (_sz_aff_default_commission_pct → fallback global). NÃO existe um
  //     "padrão por produto" no schema: a fonte é o default do produtor, lido de
  //     GET /portal/affiliates.default_commission_pct (mesma option que a aba Config
  //     de Afiliados edita). Documentado no relatório.
  //   - Se o usuário ALTERAR esse valor, ao salvar abre um confirm (FALK) avisando que
  //     sobrescreve o padrão do produto para ESTA oferta. Comparação no submit (não a
  //     cada tecla) — trata também o caso "muda e volta ao padrão".
  //   - O valor vai no body sob a chave `affiliate_commission_pct` (createLinkRequest);
  //     ATENÇÃO: o endpoint de UPDATE (/commission) usa `commission_pct` — chaves
  //     diferentes. Como o decoder Go ignora chaves desconhecidas, errar a chave aqui
  //     criaria a oferta com comissão 0 silenciosamente — por isso é literal e testada.
  const [linkOpen, setLinkOpen] = useState(false)
  const [linkSaving, setLinkSaving] = useState(false)
  // Comissão padrão do produtor (pré-preenchimento) — lida de /portal/affiliates.
  const [producerDefaultComm, setProducerDefaultComm] = useState(0)
  // REDESENHO 2026-06-23 — composição da oferta (substitui o NOME LIVRE):
  //   - O usuário preenche QUANTIDADE; o PRODUTO da 1ª linha já vem do produto de onde
  //     o modal abriu (sem select por padrão).
  //   - "+ adicionar produto" acrescenta linhas (bundle): aí cada linha mostra um SELECT
  //     de produto + quantidade.
  //   - Variação: select só quando o produto tem >1 variação (splitVariacoes>1) — hoje
  //     dormente (backend não envia variacao ainda).
  //   - O NOME é montado pelo BACKEND (canônico); o front só mostra um PREVIEW.
  // CompLine usa productId = ProductItem.id (sz_products.id) — NÃO wp_post_id (produtos
  // nativos têm wp_post_id NULL; o backend casa por sp.id).
  type CompLine = { productId: number; qty: string; variacao: string }
  const emptyLinkForm = { price: '', comm: '', affiliateVisible: false }
  const [linkForm, setLinkForm] = useState(emptyLinkForm)
  const [linkLines, setLinkLines] = useState<CompLine[]>([])
  function setLinkField<K extends keyof typeof emptyLinkForm>(key: K, v: (typeof emptyLinkForm)[K]) {
    setLinkForm(prev => ({ ...prev, [key]: v }))
  }
  // defaultVariacao — 1ª variação CADASTRADA do produto, ou '' quando não há nenhuma.
  // REGRA DO DONO (2026-06-26): se o produto TEM variação cadastrada, a oferta é
  // OBRIGADA a ter uma (não existe mais "Sem variação") → pré-selecionamos a 1ª para
  // o select já nascer com um valor válido (o select só lista variações reais).
  function defaultVariacao(productId: number, excludeIdx?: number): string {
    const vars = splitVariacoes(items.find(it => it.id === productId)?.variacao)
    if (vars.length === 0) return ''
    const used = new Set(
      linkLines.filter((l, i) => i !== excludeIdx && l.productId === productId).map(l => l.variacao)
    )
    return vars.find(v => !used.has(v)) ?? vars[0]
  }
  function openLink() {
    if (!active) return
    // 1ª linha = o produto de onde o modal abriu. A variação nasce na 1ª variação
    // cadastrada (se houver) — produto COM variação não pode ficar "Sem variação".
    setLinkLines([{ productId: active.id, qty: '1', variacao: defaultVariacao(active.id) }])
    // Pré-preenche a comissão com o padrão do produtor (string, ponto decimal),
    // já respeitando o teto #93 (99%).
    const def = producerDefaultComm > 0 ? Math.min(producerDefaultComm, 99) : 0
    setLinkForm({ ...emptyLinkForm, comm: def > 0 ? String(def) : '' })
    setLinkOpen(true)
  }

  const isAffiliate = role === 'afiliado' || role === 'affiliate'
  const isOperator = role === 'operator'
  // canManage: só o produtor dono gerencia (espelha $sz9pr_can_mgr do WP).
  const canManage = role === 'produtor'

  const load = useCallback(() => {
    let cancelled = false
    setLoading(true)
    setErr('')
    Promise.all([
      api<ListResp>('/portal/products'),
      api<CDResp>('/portal/products/cds').catch(() => ({ ok: true, data: [] as CD[], total: 0 })),
      api<MeResp>('/portal/me').catch(() => ({ role: '' } as MeResp)),
      // Comissão padrão do produtor — pré-preenche o form de "Gerar checkout".
      // Não bloqueante: se falhar, cai em 0 (form ainda abre, campo vazio).
      api<{ default_commission_pct?: number }>('/portal/affiliates').catch(
        () => ({ default_commission_pct: 0 }),
      ),
    ])
      .then(([prods, cdsResp, me, aff]) => {
        if (cancelled) return
        // Resiliência: o backend pode devolver data null/ausente — sempre cair em [].
        const list: ProductItem[] = Array.isArray(prods?.data) ? prods.data : []
        // Garante que checkouts é sempre array (nunca null) por produto.
        const safeList = list.map(p => ({ ...p, checkouts: Array.isArray(p?.checkouts) ? p.checkouts : [] }))
        setItems(safeList)
        setCds(Array.isArray(cdsResp?.data) ? cdsResp.data : [])
        setRole((me?.role || '').toLowerCase())
        const def = Number(aff?.default_commission_pct)
        setProducerDefaultComm(Number.isFinite(def) && def > 0 ? def : 0)
        if (safeList.length > 0) setActiveProd(safeList[0].id)
        // Sub-aba inicial = checkouts para todos.
        const sub: Record<number, SubTab> = {}
        const draft: Record<number, string> = {}
        // Rascunho de comissão por checkout (chave = cl.id) — seeded de cada oferta.
        const comm: Record<number, string> = {}
        safeList.forEach(p => {
          sub[p.id] = 'checkouts'
          draft[p.id] = p?.descricao || ''
          ;(p.checkouts ?? []).forEach(c => {
            comm[c.id] = String(Math.round(c.affiliate_commission_pct || 0))
          })
        })
        setSubTab(sub)
        setDescDraft(draft)
        setCommDraft(comm)
        // Reseta o cache de "checkouts carregados" — o efeito refaz a busca pelo
        // endpoint dedicado p/ o produto ativo (os checkouts do List vêm vazios
        // por causa do mismatch de post_id; a fonte real é /products/{id}/checkouts).
        setCheckoutsLoaded({})
        setCheckoutsLoading({})
      })
      .catch(e => !cancelled && setErr(e.message || 'Erro ao carregar produtos'))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => load(), [load])

  const active = useMemo(() => items.find(p => p.id === activeProd) || null, [items, activeProd])

  // Troca de produto ativo volta o filtro de quantidade pro default (todos) — evita
  // filtro "preso" de outro produto (ex.: ficar em qtyFilter=5 sem "5" existir aqui).
  useEffect(() => { setQtyFilter(null) }, [activeProd])

  // Quantidades distintas presentes nos checkouts do produto ativo (p/ os chips de filtro)
  // + a lista VISÍVEL após aplicar o filtro. ckHasOutros = há oferta sem nº à frente.
  const activeCheckouts = active?.checkouts ?? []
  const ckQtys = useMemo(() => {
    const s = new Set<number>()
    for (const c of activeCheckouts) {
      const q = offerQty(c.name)
      if (q != null) s.add(q)
    }
    return Array.from(s).sort((a, b) => a - b)
  }, [activeCheckouts])
  const ckHasOutros = activeCheckouts.some(c => offerQty(c.name) == null)
  const visibleCheckouts =
    qtyFilter == null
      ? activeCheckouts
      : qtyFilter === -1
        ? activeCheckouts.filter(c => offerQty(c.name) == null)
        : activeCheckouts.filter(c => offerQty(c.name) === qtyFilter)

  // Uma oferta pode ter até três linhas no banco (Expedição, COD e Link único),
  // mas deve aparecer como UMA linha na tabela. O nome sem o sufixo Motoboy +
  // preço é a chave estável do trio, inclusive para ofertas antigas.
  const visibleCheckoutGroups = useMemo<ProductCheckoutGroup[]>(() => {
    const groups = new Map<string, ProductCheckoutGroup>()
    for (const link of visibleCheckouts) {
      const baseName = (link.name || '').replace(/ — Motoboy$/, '')
      const key = `${baseName}\u0000${link.display_value}`
      const current = groups.get(key)
      if (current) {
        current.links.push(link)
      } else {
        groups.set(key, { key, links: [link], primary: link })
      }
    }
    for (const group of groups.values()) {
      group.primary = group.links.find(link => link.tipo !== 'motoboy') || group.links[0]
    }
    return Array.from(groups.values())
  }, [visibleCheckouts])

  // ── Carrega os checkouts do produto sob demanda (endpoint dedicado) ───────────
  // GET /portal/products/{id}/checkouts devolve as ofertas escopadas ao dono. Buscado
  // quando o produto fica ativo (uma vez por produto). Substitui a.checkouts no item.
  const loadCheckouts = useCallback(
    async (pid: number) => {
      if (!pid) return
      setCheckoutsLoading(prev => ({ ...prev, [pid]: true }))
      try {
        const resp = await api<{ ok?: boolean; data?: ProductCheckout[] | null }>(
          `/portal/products/${pid}/checkouts`,
        )
        const list: ProductCheckout[] = Array.isArray(resp?.data) ? resp.data : []
        setItems(prev => prev.map(it => (it.id === pid ? { ...it, checkouts: list } : it)))
        // Semeia o rascunho de comissão por checkout (chave = cl.id).
        setCommDraft(prev => {
          const next = { ...prev }
          list.forEach(c => {
            next[c.id] = String(Math.round(c.affiliate_commission_pct || 0))
          })
          return next
        })
      } catch {
        // Silencioso: o produto segue com o que veio do List (possivelmente vazio).
        // O empty state já comunica "nenhum checkout vinculado".
      } finally {
        setCheckoutsLoaded(prev => ({ ...prev, [pid]: true }))
        setCheckoutsLoading(prev => ({ ...prev, [pid]: false }))
      }
    },
    [],
  )

  // Busca os checkouts do produto ativo na primeira vez que ele é selecionado.
  useEffect(() => {
    if (activeProd && !checkoutsLoaded[activeProd] && !checkoutsLoading[activeProd]) {
      loadCheckouts(activeProd)
    }
  }, [activeProd, checkoutsLoaded, checkoutsLoading, loadCheckouts])

  // Validação forte do form de criação: nome E categoria são obrigatórios.
  // O botão "Enviar" fica desabilitado enquanto qualquer um estiver vazio.
  const canSubmitCreate = createForm.nome.trim() !== '' && createForm.categoria.trim() !== ''

  function setSub(pid: number, t: SubTab) {
    setSubTab(prev => ({ ...prev, [pid]: t }))
  }

  async function saveDesc(p: ProductItem) {
    setSavingDesc(p.id)
    try {
      await api(`/portal/products/${p.id}/vitrine-description`, {
        method: 'POST',
        body: JSON.stringify({ description: descDraft[p.id] ?? '' }),
      })
      toast('ok', 'Descrição salva com sucesso.')
      setItems(prev => prev.map(it => (it.id === p.id ? { ...it, descricao: descDraft[p.id] ?? '' } : it)))
    } catch (e: any) {
      toast('err', e.message || 'Erro ao salvar descrição.')
    } finally {
      setSavingDesc(0)
    }
  }

  async function uploadProductImage(file: File): Promise<string> {
    if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) throw new Error('Use uma imagem JPG, PNG ou WEBP.')
    if (file.size > 8 * 1024 * 1024) throw new Error('A imagem deve ter no máximo 8 MB.')
    const form = new FormData()
    form.append('image', file)
    const uploaded = await api<{ url: string }>('/portal/settings/brand/upload', { method: 'POST', body: form })
    return uploaded.url
  }

  async function changeProductImage(product: ProductItem, file: File) {
    const key = `image-${product.id}`
    setBusyKey(key, true)
    try {
      const url = await uploadProductImage(file)
      await api(`/portal/products/${product.id}/image`, { method: 'POST', body: JSON.stringify({ imagem: url }) })
      setItems(prev => prev.map(p => p.id === product.id ? { ...p, image: url } : p))
      toast('ok', 'Foto do produto atualizada.')
    } catch (e: any) { toast('err', e.message || 'Erro ao atualizar a foto.') }
    finally { setBusyKey(key, false) }
  }

  // Atualiza um checkout (cl.id) dentro do produto ativo no state local.
  function patchCheckout(pid: number, cid: number, patch: Partial<ProductCheckout>) {
    setItems(prev =>
      prev.map(it =>
        it.id !== pid
          ? it
          : { ...it, checkouts: (it.checkouts ?? []).map(c => (c.id === cid ? { ...c, ...patch } : c)) },
      ),
    )
  }

  // ── Toggle "Visível para afiliados" (só produtor) — /portal/links/{id}/affiliate-toggle ──
  // Otimista: reflete imediatamente; reverte no erro (espelha Links.tsx).
  async function toggleAff(pid: number, c: ProductCheckout, enabled: boolean) {
    const key = `toggle-${c.id}`
    setBusyKey(key, true)
    patchCheckout(pid, c.id, { affiliate_visible: enabled })
    try {
      const resp = await api<{ ok: boolean; message: string }>(`/portal/links/${c.id}/affiliate-toggle`, {
        method: 'POST',
        body: JSON.stringify({ enabled }),
      })
      toast('ok', resp.message || (enabled ? 'Oferta liberada para afiliados.' : 'Oferta removida dos afiliados.'))
    } catch (e: any) {
      patchCheckout(pid, c.id, { affiliate_visible: !enabled })
      toast('err', e.message || 'Erro ao salvar.')
    } finally {
      setBusyKey(key, false)
    }
  }

  // Lápis discreto: entra em modo de edição da comissão. Semeia o rascunho com o
  // valor atual (se ainda não houver) para o input não abrir vazio.
  function startEditComm(c: ProductCheckout) {
    setCommDraft(prev => ({
      ...prev,
      [c.id]: prev[c.id] ?? String(Math.round(c.affiliate_commission_pct || 0)),
    }))
    setCommEditing(prev => ({ ...prev, [c.id]: true }))
  }
  // Cancela a edição: descarta o rascunho de volta ao valor atual e fecha o input.
  function cancelEditComm(c: ProductCheckout) {
    setCommDraft(prev => ({ ...prev, [c.id]: String(Math.round(c.affiliate_commission_pct || 0)) }))
    setCommEditing(prev => ({ ...prev, [c.id]: false }))
  }

  // ── Salvar comissão % (só produtor) — /portal/links/{id}/commission ────────────
  // O backend hoje devolve 501 (coluna não migrada): NÃO fingimos sucesso — o erro vira toast.
  async function saveCommission(pid: number, c: ProductCheckout) {
    const key = `comm-${c.id}`
    const raw = commDraft[c.id] ?? ''
    const pct = parseFloat(raw)
    if (!Number.isFinite(pct) || pct < 0 || pct > 100) {
      toast('err', 'A comissão deve ficar entre 0% e 100%.')
      return
    }
    setBusyKey(key, true)
    try {
      await api(`/portal/links/${c.id}/commission`, {
        method: 'POST',
        body: JSON.stringify({ commission_pct: pct }),
      })
      // Caminho de sucesso reservado p/ quando a coluna for migrada (hoje cai no catch/501).
      patchCheckout(pid, c.id, { affiliate_commission_pct: pct })
      setCommEditing(prev => ({ ...prev, [c.id]: false }))
      toast('ok', 'Comissão atualizada.')
    } catch (e: any) {
      toast('err', e.message || 'Não foi possível atualizar a comissão.')
    } finally {
      setBusyKey(key, false)
    }
  }

  // ── Excluir checkout (só produtor) — DELETE /portal/links/{id} ─────────────────
  async function removeCheckout(pid: number, c: ProductCheckout) {
    const ok = await confirmAsync({
      title: 'Excluir checkout',
      message: `Excluir o checkout “${c.name || '—'}”? Esta ação não pode ser desfeita.`,
      confirmLabel: 'Excluir',
      danger: true,
    })
    if (!ok) return
    const key = `del-${c.id}`
    setBusyKey(key, true)
    try {
      const resp = await api<{ ok: boolean; message: string }>(`/portal/links/${c.id}`, { method: 'DELETE' })
      setItems(prev =>
        prev.map(it =>
          it.id !== pid ? it : { ...it, checkouts: (it.checkouts ?? []).filter(x => x.id !== c.id) },
        ),
      )
      toast('ok', resp.message || 'Checkout excluído.')
    } catch (e: any) {
      toast('err', e.message || 'Erro ao excluir o checkout.')
    } finally {
      setBusyKey(key, false)
    }
  }

  // ── Submeter criação de produto — POST /portal/products ───────────────────────
  // Validação client FORTE: nome E categoria obrigatórios (botão desabilitado já
  // impede o submit; aqui é a segunda barreira). Strings opcionais vazias são
  // OMITIDAS do body (Go nil → NULL). A URL da imagem vai sob `imagem` (forward-compat
  // — hoje o backend descarta; ver comentário do form e relatório).
  async function submitCreate() {
    const nome = createForm.nome.trim()
    const categoria = createForm.categoria.trim()
    if (!nome) {
      toast('err', 'Informe o nome do produto.')
      return
    }
    if (!categoria) {
      toast('err', 'Selecione a categoria do produto.')
      return
    }
    // Monta o body só com os campos preenchidos (espelha createProductRequest +
    // a chave forward-compat `imagem`). SKU/dimensões/peso NÃO vão (admin-only).
    const body: Record<string, unknown> = { nome, categoria }
    const descricao = createForm.descricao.trim()
    const imagem = createForm.imagem.trim()
    if (descricao) body.descricao = descricao
    if (imagem) body.imagem = imagem // forward-compat: backend ainda não persiste.
    // custo — FEAT-CRM-FINANCEIRO (2026-07-28): privado do produtor+admin.
    const custoRaw = createForm.custo.trim()
    if (custoRaw) {
      const custoNum = parseFloat(custoRaw.replace(',', '.'))
      if (!Number.isNaN(custoNum)) body.custo = custoNum
    }

    setCreating(true)
    try {
      const resp = await api<{ ok: boolean; id?: number; status?: string; mensagem?: string }>(
        '/portal/products',
        { method: 'POST', body: JSON.stringify(body) },
      )
      // O backend coloca status 'a aprovar' (ou 'aprovado' p/ auto-aprovação) e
      // devolve a mensagem certa para cada caso — preferimos ela ao literal fixo.
      toast('ok', resp?.mensagem || 'Produto enviado para aprovação.')
      setCreateOpen(false)
      setCreateForm(emptyCreateForm)
      load() // recarrega a lista pelo loader existente.
    } catch (e: any) {
      toast('err', e.message || 'Erro ao cadastrar produto.')
    } finally {
      setCreating(false)
    }
  }

  // ── Composição da oferta (linhas de bundle) ───────────────────────────────────
  // productById resolve o ProductItem de uma linha (p/ nome/variações no preview).
  const productById = useCallback((id: number) => items.find(p => p.id === id) || null, [items])

  function setLine(idx: number, patch: Partial<CompLine>) {
    setLinkLines(prev =>
      prev.map((l, i) => {
        if (i !== idx) return l
        const next = { ...l, ...patch }
        // Ao TROCAR de produto numa linha, a variação anterior pode não existir no novo
        // produto → cai na variação PADRÃO do novo produto (1ª cadastrada, ou '' se o
        // novo produto não tem variação). Mantém a regra "produto com variação não fica
        // sem variação" também na troca de produto.
        if (patch.productId !== undefined && patch.productId !== l.productId)
          next.variacao = defaultVariacao(patch.productId, idx)
        return next
      }),
    )
  }
  function addLine() {
    // Nova linha de bundle: 1º produto AINDA NÃO usado noutra linha (cada linha é um SKU
    // fixo — ver nota em "Produto" no select). Cai no ativo/1º item só se todos já usados.
    setLinkLines(prev => {
      const usedIds = new Set(prev.map(l => l.productId))
      const def = items.find(p => !usedIds.has(p.id))?.id ?? active?.id ?? items[0]?.id ?? 0
      return [...prev, { productId: def, qty: '1', variacao: defaultVariacao(def) }]
    })
  }
  function removeLine(idx: number) {
    setLinkLines(prev => (prev.length <= 1 ? prev : prev.filter((_, i) => i !== idx)))
  }

  // namePreview reproduz a BASE do nome do backend (buildBaseName): junta
  // "{qtd} {nomeProduto}[ {variacao}]" com " + ", SEM valor e SEM comissão (decisão do
  // dono 2026-06-24). O ESTÁGIO de funil (Downsell/Remarketing) é decidido pelo backend
  // pela ORDEM DE PREÇO entre as ofertas do produto — o front NÃO o prevê (depende das
  // irmãs já criadas).
  const namePreview = useMemo(() => {
    const parts = linkLines.map(l => {
      const p = productById(l.productId)
      const qty = String(parseInt(l.qty, 10) || 1)
      const nome = p?.name || '—'
      const v = l.variacao.trim()
      return v ? `${qty} ${nome} ${v}` : `${qty} ${nome}`
    })
    return parts.join(' + ')
  }, [linkLines, productById])

  // ── Submeter "Gerar checkout" — POST /portal/links (só produtor) ──────────────
  // REDESENHO 2026-06-23: o front NÃO manda mais nome livre. Envia a COMPOSIÇÃO
  // (lista {product_id, qty, variacao?}) + preço + comissão. O backend monta o nome
  // canônico, aplica dedup/diferenciação e cria 1/3 links. Validação client: preço > 0,
  // comissão 0..99 (#93). Se a comissão diferir do padrão, confirm de override antes.
  async function submitLink() {
    if (!active) return
    const price = parseFloat(linkForm.price.replace(',', '.'))
    if (!Number.isFinite(price) || price <= 0) {
      toast('err', 'Informe um preço de venda maior que zero.')
      return
    }
    // Composição: toda linha precisa de produto válido e qty >= 1.
    const composition = linkLines.map(l => ({
      product_id: l.productId,
      qty: Math.max(1, parseInt(l.qty, 10) || 1),
      variacao: l.variacao.trim(),
    }))
    if (composition.length === 0 || composition.some(c => !c.product_id)) {
      toast('err', 'Selecione ao menos um produto na composição.')
      return
    }
    // REGRA DO DONO (2026-06-26): produto COM variação cadastrada EXIGE variação na
    // oferta (não há mais "Sem variação"). Guarda defensiva — o select já força isso.
    const semVariacao = linkLines.some(l => {
      const vars = splitVariacoes(productById(l.productId)?.variacao)
      return vars.length >= 1 && l.variacao.trim() === ''
    })
    if (semVariacao) {
      toast('err', 'Selecione a variação do produto.')
      return
    }
    // Comissão: vazio = 0; senão valida 0..99 (#93 — teto 99%).
    const commRaw = linkForm.comm.trim()
    let comm = 0
    if (commRaw !== '') {
      comm = parseFloat(commRaw.replace(',', '.'))
      if (!Number.isFinite(comm) || comm < 0 || comm > 99) {
        toast('err', 'A comissão deve ficar entre 0% e 99%.')
        return
      }
    }

    // Override do padrão do produto: dispara o confirm só quando o valor efetivo
    // diferir do padrão pré-preenchido (comparação no submit trata muda-e-volta).
    if (comm !== producerDefaultComm) {
      const ok = await confirmAsync({
        title: 'Comissão diferente do padrão',
        message: `Isto sobrescreve o padrão do produto para esta oferta. A comissão do afiliado nesta oferta será ${comm}% (padrão: ${producerDefaultComm}%). Deseja continuar?`,
        confirmLabel: 'Salvar oferta',
      })
      if (!ok) return
    }

    setLinkSaving(true)
    try {
      // Body novo: product_id (principal = 1ª linha, p/ compat) + composition + price +
      // comissão. O backend IGNORA `name` (monta canônico). Chave da comissão =
      // affiliate_commission_pct (createLinkRequest), NÃO commission_pct (essa é a do UPDATE).
      const resp = await api<{ ok: boolean; mensagem?: string; name?: string }>('/portal/links', {
        method: 'POST',
        body: JSON.stringify({
          product_id: composition[0].product_id,
          composition,
          price,
          affiliate_visible: linkForm.affiliateVisible,
          affiliate_commission_pct: comm,
        }),
      })
      toast('ok', resp?.mensagem || 'Oferta criada com sucesso.')
      setLinkOpen(false)
      setLinkForm(emptyLinkForm)
      setLinkLines([])
      load() // recarrega a lista (a nova oferta aparece nos checkouts do produto).
    } catch (e: any) {
      // O backend devolve 409 "Oferta idêntica já existe." — o api() propaga a mensagem.
      toast('err', e.message || 'Erro ao criar a oferta.')
    } finally {
      setLinkSaving(false)
    }
  }

  // ── Loading / vazio ───────────────────────────────────────────────────────────
  if (loading) {
    return (
      <section id="sec-products" className="sz-sec" aria-busy="true">
        <div className="szv2-page-head" style={{ marginBottom: 16 }}>
          <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
            Produtos
          </h2>
        </div>
        <SectionLoading label="Carregando produtos…" />
      </section>
    )
  }

  return (
    <section id="sec-products" className="sz-sec">
      {!!err && <AlertError message={err} onRetry={load} />}

      {/* Seletor de CD movido p/ dentro do painel do produto (linha fina entre o
          cabeçalho do produto e os KPIs) — pedido do dono 2026-06-24. O "+ Adicionar
          produto" passou p/ a linha das abas de produto. */}

      {/* ── Sem produtos no escopo ────────────────────────────────────────────── */}
      {items.length === 0 ? (
        <>
          <EmptyState
            icon="📦"
            title="Nenhum produto disponível"
            description={
              isOperator
                ? 'Operadores não possuem produtos próprios.'
                : isAffiliate
                ? 'Você ainda não está vinculado a nenhum produto.'
                : 'Seus produtos aparecem aqui assim que forem aprovados.'
            }
          />
          {/* Produtor sem produtos ainda também pode cadastrar o primeiro — mesmo
              botão/fluxo de quem já tem produto (openCreate), só que sem a linha
              de abas pra ancorar (aqui fica centralizado, abaixo do empty state). */}
          {canManage && (
            <div style={{ display: 'flex', justifyContent: 'center', marginTop: 16 }}>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                onClick={openCreate}
              >
                + Adicionar produto
              </button>
            </div>
          )}
        </>
      ) : (
        <>
          {/* Cabeçalho da página (microcopy de auditoria) */}
          <div className="szv2-page-head" style={{ marginBottom: 16 }}>
            <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>Produtos</h2>
            <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>Gerencie seus produtos, checkouts e estoque em um só lugar.</p>
          </div>
          {/* ── Abas de produto + botão Adicionar (fallback sem CD) ─────────── */}
          <div className="szv2-prod-tabs-row">
            <div className="szv2-prod-tabs" role="tablist">
              {items.map(p => {
                const act = p.id === activeProd
                return (
                  <button
                    key={p.id}
                    type="button"
                    className={`szv2-prod-tab${act ? ' szv2-prod-tab--active' : ''}`}
                    role="tab"
                    aria-selected={act ? 'true' : 'false'}
                    onClick={() => setActiveProd(p.id)}
                  >
                    {p.name}
                    {p.variacao && ` (${p.variacao})`}
                  </button>
                )
              })}
            </div>
            {/* "+ Adicionar produto" alinhado à direita das abas de produto
                (Datalaprox/Dorvax) — pedido do dono 2026-06-24. */}
            {canManage && (
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                onClick={openCreate}
              >
                + Adicionar produto
              </button>
            )}
          </div>

          {/* ── Painel do produto ativo ─────────────────────────────────────── */}
          {!!active && (
            <div className="szv2-prod-panel" role="tabpanel">
              {/* Cabeçalho do produto */}
              <div className="szv2-card szv2-prod-head-card">
                {/* Thumbnail = a imagem do produto (a MESMA do WP), vinda de
                    sz_products.meta via /portal/products. Quando há URL, mostra a foto
                    (object-fit: cover, recorte fiel); senão cai no placeholder SVG.
                    onError volta ao placeholder se a URL quebrar (sem fingir imagem). */}
                {active.image ? (
                  <div className="szv2-prod-thumb" style={{ overflow: 'hidden', padding: 0 }}>
                    <img
                      src={active.image}
                      alt={active.name}
                      style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
                      onError={e => {
                        // Some a imagem quebrada e revela o placeholder irmão (CSS).
                        const el = e.currentTarget
                        el.style.display = 'none'
                        const ph = el.parentElement?.nextElementSibling as HTMLElement | null
                        if (ph) ph.style.display = 'flex'
                      }}
                    />
                  </div>
                ) : null}
                <div
                  className="szv2-prod-thumb szv2-prod-thumb--ph"
                  aria-hidden="true"
                  style={active.image ? { display: 'none' } : undefined}
                >
                  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
                    <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                  </svg>
                </div>
                <div className="szv2-prod-head-meta">
                  <h2>{active.name}</h2>
                  <p>Checkouts, estoque, envios e movimentações deste produto.</p>
                  <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center', marginTop: 4 }}>
                    {!!active.categoria && (
                      <span
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          padding: '2px 10px',
                          borderRadius: 999,
                          fontSize: 12,
                          fontWeight: 600,
                          background: 'var(--szv2-brand-light)',
                          color: 'var(--szv2-brand)',
                        }}
                      >
                        {active.categoria}
                      </span>
                    )}
                    {!!active.sku && <span className="szv2-prod-sku">SKU {active.sku}</span>}
                    {canManage && (
                      <button
                        type="button"
                        className={`szv2-btn szv2-btn-sm ${active.vitrine_visible !== false ? 'szv2-btn-ghost' : 'szv2-btn-danger'}`}
                        title={active.vitrine_visible !== false ? 'Clique para ocultar da vitrine' : 'Clique para mostrar na vitrine'}
                        onClick={async () => {
                          try {
                            const res = await api<{ vitrine_visible: boolean }>(`/portal/products/${active.id}/vitrine-toggle`, { method: 'POST' })
                            setItems(prev => prev.map(p => p.id === active.id ? { ...p, vitrine_visible: res.vitrine_visible } : p))
                          } catch { /* ignore */ }
                        }}
                      >
                        {active.vitrine_visible !== false ? 'Visível na vitrine' : 'Oculto da vitrine'}
                      </button>
                    )}
                    {canManage && (
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                        onClick={() => setDetailsDrawerOpen(true)}
                      >
                        Mais detalhes
                      </button>
                    )}
                  </div>
                </div>
              </div>

              {/* GATE: produto ainda não aprovado (ou reprovado) — some com CD/KPIs/
                  checkouts/estoque/movimentações DESTE produto só; navegação no resto
                  do site (sidebar, outras seções, outros produtos) continua normal.
                  Pedido do dono 2026-07-11: escopado ao painel do produto, não ao site
                  inteiro. */}
              {active.status === 'a_aprovar' || active.status === 'reprovado' ? (
                <div className="szv2-card" style={{ textAlign: 'center', padding: '48px 24px' }}>
                  <div style={{ fontSize: 40, marginBottom: 16 }}>
                    {active.status === 'reprovado' ? '⚠️' : '⏳'}
                  </div>
                  <h3 style={{ fontSize: 17, fontWeight: 700, margin: '0 0 8px', color: 'var(--szv2-text)' }}>
                    {active.status === 'reprovado' ? 'Produto reprovado' : 'Produto em análise'}
                  </h3>
                  <p style={{ fontSize: 13.5, color: 'var(--szv2-text-muted)', margin: 0, maxWidth: 380, marginLeft: 'auto', marginRight: 'auto', lineHeight: 1.5 }}>
                    {active.status === 'reprovado'
                      ? 'Este produto foi reprovado pela nossa equipe. Entre em contato com o suporte para mais detalhes.'
                      : 'Recebemos o cadastro e nossa equipe está avaliando. Assim que for aprovado, checkouts, estoque e movimentações liberam automaticamente aqui.'}
                  </p>
                </div>
              ) : (
              <>
              {/* Seletor de CD — fino, linha única, ENTRE o cabeçalho do produto e os
                  KPIs (pedido do dono 2026-06-24). Mesmo cdFilter de antes (escopo global). */}
              {cds.length >= 1 && (
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 'var(--szv2-space-2)',
                    flexWrap: 'wrap',
                    margin: 'var(--szv2-space-3) 0',
                  }}
                >
                  <span style={{ fontSize: 12.5, fontWeight: 600, color: 'var(--szv2-text-faint)', whiteSpace: 'nowrap' }}>
                    Centro de Distribuição:
                  </span>
                  <button
                    type="button"
                    className={`szv2-btn szv2-btn-sm ${cdFilter === 0 ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                    onClick={() => setCdFilter(0)}
                  >
                    Todos os CDs
                  </button>
                  {cds.map(cd => (
                    <button
                      key={cd.id}
                      type="button"
                      className={`szv2-btn szv2-btn-sm ${cdFilter === cd.id ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                      onClick={() => setCdFilter(cd.id)}
                    >
                      {cd.nome}
                    </button>
                  ))}
                </div>
              )}

              {/* Card "Dimensões e peso" REMOVIDO da visão do produtor (decisão do dono:
                  "as medidas pode remover para o produtor"). Os campos altura/largura/
                  comprimento/peso continuam no shape ProductItem (o backend ainda os
                  envia; são dados logísticos do admin) — apenas não exibimos mais aqui. */}

              {/* KPIs físicos (5 cards) — fallback 0 (espelha WP sem custódia no PG) */}
              <div className="szv2-kpi-grid szv2-kpi-grid-5">
                <KpiCard label="Disponíveis" value={active.available ?? 0} />
                <KpiCard label="Reservados" value={active.reserved ?? 0} />
                <KpiCard label="Em rota" value={active.route ?? 0} />
                <KpiCard label="Entregues" value={active.delivered ?? 0} />
                <KpiCard label="Frustrados" value={active.frustrated ?? 0} />
              </div>

              {/* Sub-abas */}
              <div className="szv2-prod-subtabs" role="tablist">
                <button
                  type="button"
                  className={`szv2-prod-subtab${(subTab[active.id] || 'checkouts') === 'checkouts' ? ' szv2-prod-subtab--active' : ''}`}
                  onClick={() => setSub(active.id, 'checkouts')}
                >
                  Checkouts
                  {(active.checkouts ?? []).length > 0 && (
                    <span className="szv2-prod-subtab-count">{String((active.checkouts ?? []).length)}</span>
                  )}
                </button>
                <button
                  type="button"
                  className={`szv2-prod-subtab${(subTab[active.id] || 'checkouts') === 'envios' ? ' szv2-prod-subtab--active' : ''}`}
                  onClick={() => setSub(active.id, 'envios')}
                >
                  Envios
                </button>
                <button
                  type="button"
                  className={`szv2-prod-subtab${(subTab[active.id] || 'checkouts') === 'movs' ? ' szv2-prod-subtab--active' : ''}`}
                  onClick={() => setSub(active.id, 'movs')}
                >
                  Movimentações
                </button>
              </div>

              {/* SUB: Checkouts */}
              {(subTab[active.id] || 'checkouts') === 'checkouts' && (
                <div className="szv2-prod-sub" data-sub-panel="checkouts">
                  <div className="szv2-card">
                    <div className="szv2-card-head">
                      <div>
                        {isAffiliate && (
                          <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', marginBottom: 2 }}>
                            {active.name}
                          </p>
                        )}
                        <h3>Checkouts deste produto</h3>
                        <p className="szv2-card-sub">Links de Expedição vinculados.</p>
                      </div>
                      {canManage && (
                        <button
                          type="button"
                          className="szv2-btn szv2-btn-brand szv2-btn-sm"
                          onClick={openLink}
                        >
                          + Gerar checkout
                        </button>
                      )}
                    </div>
                    {checkoutsLoading[active.id] && (active.checkouts ?? []).length === 0 ? (
                      <SectionLoading label="Carregando checkouts…" />
                    ) : (active.checkouts ?? []).length === 0 ? (
                      <EmptyState
                        title="Nenhum checkout vinculado"
                        description={
                          canManage
                            ? 'Clique em "+ Gerar checkout" para criar o primeiro link.'
                            : 'Nenhum checkout liberado para você neste produto.'
                        }
                      />
                    ) : (
                      <>
                        {(ckQtys.length >= 1 || ckHasOutros) && (
                          <div
                            style={{
                              display: 'flex',
                              flexWrap: 'wrap',
                              gap: 6,
                              alignItems: 'center',
                              marginBottom: 12,
                            }}
                          >
                            <span style={{ fontSize: 12, color: 'var(--szv2-text-faint)', marginRight: 2 }}>
                              Quantidade:
                            </span>
                            {[
                              // Chip "Todos" e contadores (N) removidos (pedido do dono 2026-06-24).
                              // Sem "Todos", clicar de novo no chip ativo limpa o filtro (= mostra todos).
                              ...ckQtys.map(q => ({
                                key: `q${q}`,
                                label: `${q}`,
                                val: q as number | null,
                              })),
                              ...(ckHasOutros
                                ? [{
                                    key: 'outros' as const,
                                    label: `Outros`,
                                    val: -1 as number | null,
                                  }]
                                : []),
                            ].map(chip => {
                              const on = qtyFilter === chip.val
                              return (
                                <button
                                  key={chip.key}
                                  type="button"
                                  onClick={() => setQtyFilter(on ? null : chip.val)}
                                  style={{
                                    padding: '4px 11px',
                                    borderRadius: 999,
                                    fontSize: 12.5,
                                    lineHeight: 1.4,
                                    cursor: 'pointer',
                                    whiteSpace: 'nowrap',
                                    border: `1px solid ${on ? 'var(--szv2-brand)' : 'var(--szv2-border, #d4d9e0)'}`,
                                    background: on ? 'var(--szv2-brand)' : 'transparent',
                                    color: on ? '#fff' : 'var(--szv2-text)',
                                    fontWeight: on ? 700 : 500,
                                  }}
                                >
                                  {chip.label}
                                </button>
                              )
                            })}
                          </div>
                        )}
                        <div className="szv2-table-wrap">
                          <table className="szv2-table">
                            <thead>
                              <tr>
                                <th>Nome</th>
                                <th>Valor</th>
                                <th style={{ whiteSpace: 'nowrap' }}>Comissão %</th>
                                <th>Links</th>
                                {canManage && <th>Afiliados</th>}
                                {canManage && <th></th>}
                              </tr>
                            </thead>
                            <tbody>
                              {visibleCheckoutGroups.map(group => {
                                const lk = group.primary
                                const expedition = group.links.find(c => c.tipo === 'correio' || c.tipo === 'expedicao')
                                const mixed = group.links.find(c => c.tipo === 'misto')
                                const cod = group.links.find(c => c.tipo === 'motoboy')
                                return (
                                <tr key={group.key} data-link-id={lk.id}>
                                  <td>
                                    <div className="szv2-td-main">{lk.name || '—'}</div>
                                    {lk.tipo !== 'motoboy' && !!lk.slug && (
                                      <div
                                        role="button"
                                        title="Copiar token (uso na API de pedidos)"
                                        onClick={async e => {
                                          e.stopPropagation()
                                          try {
                                            await navigator.clipboard.writeText(lk.slug)
                                            toast('ok', 'Token copiado!')
                                          } catch {
                                            toast('err', 'Não foi possível copiar.')
                                          }
                                        }}
                                        style={{
                                          fontSize: 11,
                                          color: 'var(--szv2-text-muted)',
                                          fontFamily: 'var(--szv2-font-mono)',
                                          cursor: 'pointer',
                                          display: 'inline-flex',
                                          alignItems: 'center',
                                          gap: 4,
                                        }}
                                      >
                                        {lk.slug} 📋
                                      </div>
                                    )}
                                  </td>
                                  <td className="szv2-num" style={{ whiteSpace: 'nowrap' }}>
                                    {lk.price_label || '—'}
                                  </td>
                                  <td>
                                    {canManage ? (
                                      commEditing[lk.id] ? (
                                        <div
                                          style={{
                                            display: 'flex',
                                            alignItems: 'center',
                                            gap: 6,
                                            justifyContent: 'flex-start',
                                          }}
                                        >
                                          <input
                                            type="number"
                                            className="szv2-input"
                                            value={commDraft[lk.id] ?? ''}
                                            min={0}
                                            max={99}
                                            step={1}
                                            autoFocus
                                            onChange={e =>
                                              setCommDraft(prev => ({ ...prev, [lk.id]: e.target.value }))
                                            }
                                            style={{
                                              width: 52,
                                              height: 32,
                                              textAlign: 'center',
                                              padding: '4px 6px',
                                            }}
                                          />
                                          <button
                                            type="button"
                                            className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                                            disabled={!!busy[`comm-${lk.id}`]}
                                            onClick={() => saveCommission(active.id, lk)}
                                          >
                                            Salvar
                                          </button>
                                          <button
                                            type="button"
                                            aria-label="Cancelar edição da comissão"
                                            title="Cancelar"
                                            disabled={!!busy[`comm-${lk.id}`]}
                                            onClick={() => cancelEditComm(lk)}
                                            style={{
                                              display: 'inline-flex',
                                              alignItems: 'center',
                                              justifyContent: 'center',
                                              width: 26,
                                              height: 26,
                                              padding: 0,
                                              border: 'none',
                                              background: 'transparent',
                                              cursor: 'pointer',
                                              color: 'var(--szv2-text-muted)',
                                              borderRadius: 6,
                                            }}
                                          >
                                            <svg
                                              width="14"
                                              height="14"
                                              viewBox="0 0 24 24"
                                              fill="none"
                                              stroke="currentColor"
                                              strokeWidth="2"
                                              strokeLinecap="round"
                                              strokeLinejoin="round"
                                              aria-hidden="true"
                                            >
                                              <line x1="18" y1="6" x2="6" y2="18" />
                                              <line x1="6" y1="6" x2="18" y2="18" />
                                            </svg>
                                          </button>
                                        </div>
                                      ) : (
                                        <div
                                          style={{
                                            display: 'flex',
                                            alignItems: 'center',
                                            gap: 6,
                                            justifyContent: 'flex-start',
                                          }}
                                        >
                                          <span className="szv2-num">
                                            {Math.round(lk.affiliate_commission_pct || 0)}%
                                          </span>
                                          <button
                                            type="button"
                                            aria-label="Editar comissão"
                                            title="Editar comissão"
                                            onClick={() => startEditComm(lk)}
                                            style={{
                                              display: 'inline-flex',
                                              alignItems: 'center',
                                              justifyContent: 'center',
                                              width: 26,
                                              height: 26,
                                              padding: 0,
                                              border: 'none',
                                              background: 'transparent',
                                              cursor: 'pointer',
                                              color: 'var(--szv2-text-muted)',
                                              borderRadius: 6,
                                            }}
                                          >
                                            <svg
                                              width="14"
                                              height="14"
                                              viewBox="0 0 24 24"
                                              fill="none"
                                              stroke="currentColor"
                                              strokeWidth="2"
                                              strokeLinecap="round"
                                              strokeLinejoin="round"
                                              aria-hidden="true"
                                            >
                                              <path d="M12 20h9" />
                                              <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z" />
                                            </svg>
                                          </button>
                                        </div>
                                      )
                                    ) : (
                                      <span className="szv2-num">
                                        {Math.round(lk.affiliate_commission_pct || 0)}%
                                      </span>
                                    )}
                                  </td>
                                  <td>
                                    {/* Uma oferta = uma linha; os canais ficam lado a lado.
                                        Ofertas antigas podem ainda ter só Expedição + COD;
                                        o backfill acrescenta o Link único quando aplicável.
                                        O botão ABRE o checkout em nova aba (<a target=_blank>).
                                        Para o PRODUTOR a url/cod_url vêm PURAS do backend (sem
                                        ?r= de afiliado); para o AFILIADO o backend já injeta o
                                        ?r= rastreável na url (products.go::Checkouts ramo isAff
                                        → appendRefToken) — a comissão é atribuída ao abrir. */}
                                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center' }}>
                                      {!!expedition?.url && (
                                        <a
                                          href={expedition.url}
                                          target="_blank"
                                          rel="noopener noreferrer"
                                          className="szv2-btn szv2-btn-brand szv2-btn-sm"
                                          style={{ textDecoration: 'none' }}
                                        >
                                          Expedição
                                        </a>
                                      )}
                                      {!!cod?.cod_url && (
                                        <a
                                          href={cod.cod_url as string}
                                          target="_blank"
                                          rel="noopener noreferrer"
                                          className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                                          style={{ textDecoration: 'none' }}
                                        >
                                          Cash on Delivery
                                        </a>
                                      )}
                                      {!!mixed?.url && (
                                        <a
                                          href={mixed.url}
                                          target="_blank"
                                          rel="noopener noreferrer"
                                          className="szv2-btn szv2-btn-brand szv2-btn-sm"
                                          style={{ textDecoration: 'none' }}
                                        >
                                          Link único
                                        </a>
                                      )}
                                      {!expedition?.url && !cod?.cod_url && !mixed?.url && (
                                        <span style={{ fontSize: 12, color: 'var(--szv2-text-faint)' }}>—</span>
                                      )}
                                    </div>
                                  </td>
                                  {canManage && (
                                    <td>
                                      <label className="szv2-toggle-lbl">
                                        <input
                                          type="checkbox"
                                          checked={!!lk.affiliate_visible}
                                          disabled={!!busy[`toggle-${lk.id}`]}
                                          onChange={e => toggleAff(active.id, lk, e.target.checked)}
                                        />
                                        <span className="szv2-toggle-slider"></span>
                                      </label>
                                    </td>
                                  )}
                                  {canManage && (
                                    <td style={{ textAlign: 'right' }}>
                                      <button
                                        type="button"
                                        className="szv2-btn szv2-btn-sm szv2-btn-danger"
                                        disabled={!!busy[`del-${lk.id}`]}
                                        onClick={() => removeCheckout(active.id, lk)}
                                      >
                                        Excluir
                                      </button>
                                    </td>
                                  )}
                                </tr>
                                )
                              })}
                            </tbody>
                          </table>
                        </div>
                        <p style={{ fontSize: 12, color: 'var(--szv2-text-faint)', marginTop: 8 }}>
                          Se o checkout tiver mais produtos, ele aparece em todos os produtos vinculados.
                        </p>
                      </>
                    )}
                  </div>
                </div>
              )}

              {/* SUB: Envios */}
              {(subTab[active.id] || 'checkouts') === 'envios' && (
                <div className="szv2-prod-sub" data-sub-panel="envios">
                  <div className="szv2-card">
                    <div className="szv2-card-head">
                      <div>
                        <h3>Envios de estoque</h3>
                        <p className="szv2-card-sub">Reposições enviadas para o CD.</p>
                      </div>
                    </div>
                    <EmptyState
                      title="Nenhum envio registrado"
                      description="Os envios de reposição aparecem aqui assim que forem registrados."
                    />
                  </div>
                </div>
              )}

              {/* SUB: Movimentações */}
              {(subTab[active.id] || 'checkouts') === 'movs' && (
                <div className="szv2-prod-sub" data-sub-panel="movs">
                  <div className="szv2-card">
                    <div className="szv2-card-head">
                      <div>
                        <h3>Movimentações de estoque</h3>
                        <p className="szv2-card-sub">Entradas e saídas deste produto.</p>
                      </div>
                    </div>
                    <EmptyState
                      title="Nenhuma entrada ou saída"
                      description="As movimentações deste produto aparecem aqui."
                    />
                  </div>
                </div>
              )}
              </>
              )}

            </div>
          )}
        </>
      )}

      {/* ── Drawer: criar produto (POST /portal/products — só produtor) ─────────── */}
      <Drawer
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        title="Adicionar produto"
        ariaLabel="Adicionar produto"
        width={440}
      >
        <form
          onSubmit={e => {
            e.preventDefault()
            if (!creating && canSubmitCreate) submitCreate()
          }}
        >
          <p style={{ margin: '0 0 16px', fontSize: 13, color: 'var(--szv2-text-soft)', lineHeight: 1.5 }}>
            Cadastre as informações comerciais do produto. Medidas, SKU e peso ficam por conta da nossa equipe.
          </p>

          {/* Nome — obrigatório */}
          <div className="szv2-input-group" style={{ marginBottom: 14 }}>
            <label className="szv2-label" htmlFor="szv2-cp-nome">
              Nome do produto <span style={{ color: 'var(--szv2-brand)' }}>*</span>
            </label>
            <input
              id="szv2-cp-nome"
              type="text"
              className="szv2-input"
              value={createForm.nome}
              onChange={e => setCreateField('nome', e.target.value)}
              placeholder="Ex.: Camiseta Premium"
              autoFocus
              required
              aria-required="true"
            />
          </div>

          {/* Categoria — SELECT pré-definido, obrigatório */}
          <div className="szv2-input-group" style={{ marginBottom: 14 }}>
            <label className="szv2-label" htmlFor="szv2-cp-categoria">
              Categoria <span style={{ color: 'var(--szv2-brand)' }}>*</span>
            </label>
            <FalkSelect
              id="szv2-cp-categoria"
              value={createForm.categoria}
              onChange={v => setCreateField('categoria', v)}
              placeholder="Selecione uma categoria…"
              options={[
                { value: '', label: 'Selecione uma categoria…', disabled: true },
                ...PRODUCT_CATEGORIES.map(cat => ({ value: cat, label: cat })),
              ]}
            />
          </div>

          {/* Descrição — aceita shortcode (texto salvo como está; render bonito fica p/ depois) */}
          <div className="szv2-input-group" style={{ marginBottom: 14 }}>
            <label className="szv2-label" htmlFor="szv2-cp-descricao">
              Descrição
            </label>
            <textarea
              id="szv2-cp-descricao"
              className="szv2-input"
              rows={4}
              value={createForm.descricao}
              onChange={e => setCreateField('descricao', e.target.value)}
              placeholder="Descreva o produto. Você pode usar shortcodes, ex.: [destaque]Frete grátis[/destaque]"
            />
            <p style={{ margin: '6px 0 0', fontSize: 12, color: 'var(--szv2-text-faint)', lineHeight: 1.4 }}>
              Aceita shortcodes — o texto é salvo exatamente como digitado.
            </p>
          </div>

          {/* Custo (COGS) — FEAT-CRM-FINANCEIRO (2026-07-28): privado, só você e o
              admin veem. Base pra relatórios financeiros futuros (margem real). */}
          <div className="szv2-input-group" style={{ marginBottom: 18 }}>
            <label className="szv2-label" htmlFor="szv2-cp-custo">
              Custo do produto (R$) <span style={{ fontWeight: 400, color: 'var(--szv2-text-faint)' }}>— privado, só você e o admin veem</span>
            </label>
            <input
              id="szv2-cp-custo"
              type="text"
              inputMode="decimal"
              className="szv2-input"
              value={createForm.custo}
              onChange={e => setCreateField('custo', e.target.value)}
              placeholder="ex: 12,50"
            />
          </div>

          {/* Imagem — URL ou upload de arquivo. */}
          <div className="szv2-input-group" style={{ marginBottom: 18 }}>
            <label className="szv2-label" htmlFor="szv2-cp-imagem">
              Imagem do produto (URL)
            </label>
            <input
              id="szv2-cp-imagem"
              type="url"
              className="szv2-input"
              value={createForm.imagem}
              onChange={e => setCreateField('imagem', e.target.value)}
              placeholder="https://…/imagem.jpg"
              inputMode="url"
            />
            {createForm.imagem.trim() !== '' && !imgPreviewError && (
              <div
                style={{
                  marginTop: 10,
                  borderRadius: 10,
                  overflow: 'hidden',
                  border: '1px solid var(--szv2-border)',
                  background: 'var(--szv2-brand-muted)',
                  maxWidth: 160,
                }}
              >
                <img
                  src={createForm.imagem.trim()}
                  alt="Pré-visualização da imagem do produto"
                  style={{ display: 'block', width: '100%', height: 120, objectFit: 'cover' }}
                  onError={() => setImgPreviewError(true)}
                />
              </div>
            )}
            <label className="szv2-btn szv2-btn-secondary szv2-btn-sm" style={{ display: 'inline-flex', marginTop: 8, cursor: 'pointer' }}>
              Enviar arquivo
              <input type="file" accept="image/png,image/jpeg,image/webp" hidden onChange={async e => { const f = e.target.files?.[0]; if (!f) return; try { setCreateField('imagem', await uploadProductImage(f)) } catch (err: any) { toast('err', err.message || 'Erro ao carregar imagem.') } e.currentTarget.value = '' }} />
            </label>
            <p style={{ margin: '6px 0 0', fontSize: 12, color: 'var(--szv2-text-faint)', lineHeight: 1.4 }}>
              JPG, PNG ou WEBP, até 8 MB.
            </p>
          </div>

          {/* ── Avisos ANTES da ação de decisão (pedido do dono — antes ficavam
              abaixo dos botões, então dava pra enviar sem ler). Ícones SVG (mesmo
              padrão dos ícones do app) — os emoji antigos (✓/⏳/★) renderizavam com
              cor própria do sistema operacional (ex.: ⏳ sempre laranja/âmbar),
              quebrando a paleta uniforme do badge. */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 16 }}>
            {[
              { icon: <path d="M8 12.5 5 9.5l1.4-1.4L8 9.7l5.6-5.6L15 5.5 8 12.5z" />, txt: 'As informações cadastradas devem coincidir com a realidade do produto.' },
              { icon: <path d="M6 3h8v2l-3 5 3 5v2H6v-2l3-5-3-5V3zm2 2v.5l2.2 4-2.2 4V15h4v-.5l-2.2-4 2.2-4V3H8z" fillRule="evenodd" clipRule="evenodd" />, txt: 'O produto passará por aprovação antes de ficar disponível.' },
              { icon: <path d="M10 2.5l1.9 4.4 4.7.5-3.6 3.2 1 4.7L10 13l-4 2.3 1-4.7-3.6-3.2 4.7-.5L10 2.5z" />, txt: 'A descrição ficará disponível para os afiliados na vitrine.' },
            ].map((n, i) => (
              <div
                key={i}
                style={{
                  display: 'flex',
                  alignItems: 'flex-start',
                  gap: 10,
                  padding: '10px 12px',
                  borderRadius: 10,
                  background: 'var(--szv2-brand-light)',
                  border: '1px solid var(--szv2-brand-muted)',
                  fontSize: 12.5,
                  lineHeight: 1.45,
                  color: 'var(--szv2-text-soft)',
                }}
              >
                <span
                  aria-hidden="true"
                  style={{
                    flexShrink: 0,
                    width: 22,
                    height: 22,
                    borderRadius: '50%',
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    background: 'var(--szv2-brand)',
                    color: '#fff',
                  }}
                >
                  <svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor">{n.icon}</svg>
                </span>
                <span>{n.txt}</span>
              </div>
            ))}
          </div>

          {/* Ações */}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary szv2-btn-sm"
              onClick={() => setCreateOpen(false)}
              disabled={creating}
            >
              Cancelar
            </button>
            <button
              type="submit"
              className="szv2-btn szv2-btn-brand szv2-btn-sm"
              disabled={creating || !canSubmitCreate}
              title={!canSubmitCreate ? 'Informe o nome e selecione a categoria.' : undefined}
            >
              {creating ? 'Enviando…' : 'Enviar para aprovação'}
            </button>
          </div>
        </form>
      </Drawer>

      {/* ── Drawer: gerar checkout/oferta (POST /portal/links — só produtor) ──────── */}
      <Drawer
        open={linkOpen}
        onClose={() => setLinkOpen(false)}
        title="Gerar checkout"
        ariaLabel="Gerar checkout"
        width={440}
      >
        <form
          onSubmit={e => {
            e.preventDefault()
            if (!linkSaving) submitLink()
          }}
        >
          <p style={{ margin: '0 0 16px', fontSize: 13, color: 'var(--szv2-text-soft)', lineHeight: 1.5 }}>
            Monte a oferta de checkout para{' '}
            <strong style={{ color: 'var(--szv2-text)' }}>{active?.name || 'este produto'}</strong>. Informe a
            quantidade, o preço de venda e a comissão do afiliado. O nome é gerado automaticamente.
          </p>

          {/* ── Composição da oferta (quantidade + bundle + variação) ───────────────
              1ª linha: o produto já vem do produto ativo (sem select). "+ adicionar
              produto" acrescenta linhas de bundle, aí cada linha mostra um SELECT de
              produto. O SELECT de variação só aparece quando o produto tem >1 variação. */}
          <div className="szv2-input-group" style={{ marginBottom: 14 }}>
            <label className="szv2-label">
              Composição <span style={{ color: 'var(--szv2-brand)' }}>*</span>
            </label>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {linkLines.map((line, idx) => {
                const lineProd = productById(line.productId)
                const allVars = splitVariacoes(lineProd?.variacao)
                const usedVars = new Set(
                  linkLines
                    .filter((l, i) => i !== idx && l.productId === line.productId)
                    .map(l => l.variacao)
                )
                const vars = allVars.filter(v => v === line.variacao || !usedVars.has(v))
                // Produto — cada linha do catálogo é um SKU fixo (nome + variação única,
                // ex.: "Egipzya"/Sérum e "Egipzya"/Espuma são 2 produtos DIFERENTES com o
                // MESMO nome). Sem filtro, dava p/ escolher o MESMO id em 2 linhas (produto
                // "duplicado" na composição). Exclui ids já usados noutras linhas (inclui a
                // linha 0, cujo produto é o `active` fixo) e desambigua o label com a variação.
                const usedProductIds = new Set(
                  linkLines.filter((l, i) => i !== idx).map(l => l.productId)
                )
                const productOptions = items
                  .filter(p => p.id === line.productId || !usedProductIds.has(p.id))
                  .map(p => ({
                    value: String(p.id),
                    label: p.variacao ? `${p.name} (${p.variacao})` : p.name,
                  }))
                const showProductSelect = idx > 0 || linkLines.length > 1
                return (
                  <div
                    key={idx}
                    style={{
                      display: 'flex',
                      gap: 8,
                      alignItems: 'flex-end',
                      flexWrap: 'wrap',
                      padding: showProductSelect ? '10px' : 0,
                      borderRadius: 10,
                      border: showProductSelect ? '1px solid var(--szv2-border)' : 'none',
                      background: showProductSelect ? 'var(--szv2-brand-muted)' : 'transparent',
                    }}
                  >
                    {/* Quantidade — sempre visível. Coluna é flex-column (label
                        BLOCO acima do campo) + flexShrink:0 p/ travar 72px. O input
                        precisa de width:100% explícito: sem isso ele assume a largura
                        intrínseca (~170px) e transborda a coluna, pintando SOBRE o
                        produto ao lado (#93). box-sizing:border-box é global. */}
                    <div
                      style={{
                        width: 72,
                        flexShrink: 0,
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 4,
                      }}
                    >
                      <label
                        className="szv2-label"
                        htmlFor={`szv2-gc-qty-${idx}`}
                        style={{ fontSize: 11 }}
                      >
                        Qtd
                      </label>
                      <input
                        id={`szv2-gc-qty-${idx}`}
                        type="number"
                        className="szv2-input"
                        value={line.qty}
                        min={1}
                        step={1}
                        onChange={e => setLine(idx, { qty: e.target.value })}
                        style={{ width: '100%', height: 38, textAlign: 'center' }}
                        inputMode="numeric"
                      />
                    </div>

                    {/* Produto — só quando há bundle (linha extra ou >1 linha). A 1ª
                        linha sozinha NÃO mostra select (o produto é o ativo). */}
                    {showProductSelect ? (
                      <div
                        style={{
                          flex: '1 1 160px',
                          minWidth: 0,
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 4,
                        }}
                      >
                        <label className="szv2-label" style={{ fontSize: 11 }}>
                          Produto
                        </label>
                        <FalkSelect
                          value={String(line.productId)}
                          onChange={v => setLine(idx, { productId: Number(v) })}
                          options={productOptions}
                        />
                      </div>
                    ) : (
                      <div
                        style={{
                          flex: '1 1 160px',
                          minWidth: 0,
                          display: 'flex',
                          alignItems: 'center',
                        }}
                      >
                        <span
                          style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            height: 38,
                            fontSize: 13,
                            fontWeight: 600,
                            color: 'var(--szv2-text)',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          {lineProd?.name || active?.name || '—'}
                        </span>
                      </div>
                    )}

                    {/* Variação — SELECT das variações CADASTRADAS no produto
                        (sz_products.variacao, enviado por products.go). OBRIGATÓRIA quando
                        existe (REGRA DO DONO 2026-06-26: "se tiver variação não deixa colocar
                        sem variação") — por isso NÃO há mais a opção "Sem variação"; o select
                        lista só variações reais e nasce na 1ª (defaultVariacao). Só aparece
                        quando o produto TEM variação cadastrada; sem variação, nem renderiza. */}
                    {vars.length >= 1 && (
                      <div
                        style={{
                          flex: '1 1 160px',
                          minWidth: 0,
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 4,
                        }}
                      >
                        <label className="szv2-label" style={{ fontSize: 11 }}>
                          Variação <span style={{ color: 'var(--szv2-brand)' }}>*</span>
                        </label>
                        <FalkSelect
                          value={line.variacao}
                          onChange={v => setLine(idx, { variacao: v })}
                          options={vars.map(v => ({ value: v, label: v }))}
                        />
                      </div>
                    )}

                    {/* Remover linha (só quando há bundle) */}
                    {linkLines.length > 1 && (
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-sm szv2-btn-danger"
                        onClick={() => removeLine(idx)}
                        style={{ height: 38 }}
                        aria-label="Remover produto da composição"
                      >
                        Remover
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
            {items.length > 1 && (
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                onClick={addLine}
                style={{ marginTop: 10 }}
              >
                + adicionar produto
              </button>
            )}
          </div>

          {/* Preview do nome canônico (montado igual ao backend; o real é autoritativo) */}
          <div
            style={{
              marginBottom: 14,
              padding: '10px 12px',
              borderRadius: 10,
              background: 'var(--szv2-brand-light)',
              border: '1px solid var(--szv2-brand-muted)',
            }}
          >
            <span style={{ fontSize: 11, color: 'var(--szv2-text-faint)', display: 'block', marginBottom: 2 }}>
              Nome da oferta (automático)
            </span>
            <strong style={{ fontSize: 13.5, color: 'var(--szv2-text)' }}>{namePreview || '—'}</strong>
            <p style={{ margin: '4px 0 0', fontSize: 11.5, color: 'var(--szv2-text-faint)', lineHeight: 1.4 }}>
              O estágio é definido automaticamente pela ordem de preço: a oferta mais cara é a Principal;
              a 2ª vira Downsell, a 3ª Remarketing e as seguintes Remarketing 2, 3… O valor e a comissão
              não entram no nome.
            </p>
          </div>

          {/* Preço de venda — obrigatório */}
          <div className="szv2-input-group" style={{ marginBottom: 14 }}>
            <label className="szv2-label" htmlFor="szv2-gc-price">
              Preço de venda (R$) <span style={{ color: 'var(--szv2-brand)' }}>*</span>
            </label>
            <input
              id="szv2-gc-price"
              type="number"
              className="szv2-input"
              value={linkForm.price}
              min={0.01}
              step={0.01}
              onChange={e => setLinkField('price', e.target.value)}
              placeholder="Ex.: 149,90"
              inputMode="decimal"
              required
              aria-required="true"
            />
          </div>

          {/* Comissão do afiliado — PRÉ-PREENCHIDA com o padrão do produtor */}
          <div className="szv2-input-group" style={{ marginBottom: 14 }}>
            <label className="szv2-label" htmlFor="szv2-gc-comm">
              Comissão do afiliado (%)
            </label>
            <input
              id="szv2-gc-comm"
              type="number"
              className="szv2-input"
              value={linkForm.comm}
              min={0}
              max={99}
              step={0.01}
              onChange={e => {
                // #93 — teto 99%: clampa no input (antes aceitava "50544444"). Também
                // impede negativo. Vazio é permitido (= 0 no submit).
                const raw = e.target.value
                if (raw === '') {
                  setLinkField('comm', '')
                  return
                }
                const n = parseFloat(raw.replace(',', '.'))
                if (!Number.isFinite(n)) {
                  setLinkField('comm', raw)
                  return
                }
                const clamped = Math.min(99, Math.max(0, n))
                setLinkField('comm', String(clamped))
              }}
              placeholder="0"
              inputMode="decimal"
            />
            <p style={{ margin: '6px 0 0', fontSize: 12, color: 'var(--szv2-text-faint)', lineHeight: 1.4 }}>
              Pré-preenchido com o padrão do produto ({producerDefaultComm}%). Máximo 99%. Ao alterar,
              confirmamos antes de sobrescrever o padrão para esta oferta.
            </p>
          </div>

          {/* Liberar para afiliados já na criação — o toggle (.szv2-toggle-lbl) é uma
              caixa fixa 40×22 com slider absolute(inset:0); ele DEVE envolver só o
              input+slider. O texto vai como IRMÃO num flex externo (padrão de
              Affiliates.tsx/Integrations.tsx) — senão o rótulo dentro da caixa de 40px
              transborda e pinta SOBRE o texto de ajuda da comissão (#92). */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              marginBottom: 18,
            }}
          >
            <label className="szv2-toggle-lbl" style={{ flexShrink: 0 }}>
              <input
                type="checkbox"
                checked={linkForm.affiliateVisible}
                onChange={e => setLinkField('affiliateVisible', e.target.checked)}
              />
              <span className="szv2-toggle-slider" />
            </label>
            <span style={{ fontSize: 13, color: 'var(--szv2-text-soft)', lineHeight: 1.4 }}>
              Liberar para afiliados já na criação
            </span>
          </div>

          {/* Ações */}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginBottom: 4 }}>
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary szv2-btn-sm"
              onClick={() => setLinkOpen(false)}
              disabled={linkSaving}
            >
              Cancelar
            </button>
            <button type="submit" className="szv2-btn szv2-btn-brand szv2-btn-sm" disabled={linkSaving}>
              {linkSaving ? 'Gerando…' : 'Gerar checkout'}
            </button>
          </div>
        </form>
      </Drawer>

      {/* ── Drawer: mais detalhes do produto (descrição da vitrine — só produtor) ── */}
      <Drawer
        open={detailsDrawerOpen}
        onClose={() => setDetailsDrawerOpen(false)}
        title={active ? `Detalhes — ${active.name}` : 'Detalhes do produto'}
      >
        {active && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--szv2-space-4)' }}>
            <div>
              <label className="szv2-label">Foto do produto</label>
              <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: '0 0 8px' }}>
                Esta imagem aparece na vitrine e no checkout.
              </p>
              <label className="szv2-btn szv2-btn-secondary szv2-btn-sm" style={{ display: 'inline-flex', cursor: busy[`image-${active.id}`] ? 'wait' : 'pointer' }}>
                {busy[`image-${active.id}`] ? 'Enviando foto…' : 'Trocar foto'}
                <input type="file" accept="image/png,image/jpeg,image/webp" hidden disabled={!!busy[`image-${active.id}`]} onChange={e => { const f = e.target.files?.[0]; if (f) changeProductImage(active, f); e.currentTarget.value = '' }} />
              </label>
            </div>
            <div>
              <label className="szv2-label">Descrição para a Vitrine</label>
              <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 8 }}>
                Texto mostrado aos afiliados: benefícios, público-alvo, diferenciais.
              </p>
              <textarea
                className="szv2-input"
                rows={6}
                value={descDraft[active.id] ?? ''}
                onChange={e => setDescDraft(prev => ({ ...prev, [active.id]: e.target.value }))}
                placeholder="Descreva o produto para afiliados: benefícios, público-alvo, diferenciais..."
              />
            </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                onClick={() => setDetailsDrawerOpen(false)}
              >
                Fechar
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-brand szv2-btn-sm"
                disabled={savingDesc === active.id}
                onClick={async () => {
                  await saveDesc(active)
                  setDetailsDrawerOpen(false)
                }}
              >
                {savingDesc === active.id ? 'Salvando…' : 'Salvar descrição'}
              </button>
            </div>
          </div>
        )}
      </Drawer>
    </section>
  )
}
