// =============================================================================
// CHECKOUT-API — contrato publico (cliente final, SEM auth).
//
// Endpoints reais do orders-service (go/orders/internal/handlers/checkout.go +
// schedule.go). TODAS as respostas vem embrulhadas em {"ok":true, <chave>:...}
// (httpx.WriteOK) e os campos sao em PT-BR — este arquivo e a CAMADA DE MAPEAMENTO
// que traduz o shape do backend para o shape que a UI consome.
//
//   GET  /checkout-api/offer?token=<t>            -> {ok, oferta:{nome,preco,...}}
//   GET  /checkout-api/schedule?token=<t>&cep=<c> -> {ok, agenda:{datas:[...]}}
//   POST /checkout-api/order                      -> {ok, order_id, order_number, ...}
//
// REGRA DE SEGURANCA (HIGH): o PRECO e SEMPRE resolvido no servidor a partir do
// checkout_link (display_value). O front so envia o TOKEN + dados do cliente; o
// backend recalcula o valor e revalida data/zona. O front NUNCA e autoritativo.
//
// MOTOR DE AGENDAMENTO (server-side): o backend resolve a zona pelo CEP, calcula
// a janela (5 ou 30 datas) e CLASSIFICA cada data como 'agendamento' ou
// 'pre_agendado'. O front so consome `tipo`/`label` ja prontos. Se a zona nao
// resolver, /schedule devolve 422 ("fora de área de entrega") — tratado como
// out_of_area neste front (ver fetchSchedule).
// =============================================================================

// Em dev o Vite proxeia /checkout-api -> backend. Em prod e same-origin.
const BASE = import.meta.env.VITE_CHECKOUT_API_BASE || '/checkout-api'

/**
 * LGPD — versao do documento (Politica de Privacidade) aceita no checkout.
 * ASSUNCAO (2026-06-19): nao existe string de versao no backend/codebase ainda;
 * '1.0' e o valor inicial e DEVE casar com o que o endpoint POST /checkout-api/consent
 * (criado por go/orders neste mesmo lote) gravar/validar. Bump aqui ao publicar
 * uma nova versao da politica.
 */
export const LGPD_DOC_VERSION = '1.0'

/** Tipo de entrega da oferta. */
// 'misto': link único (FEAT-LINK-MISTO) — o modo real (motoboy|expedicao) só se
// sabe depois do CEP, via fetchResolveMode. Componentes que já tratam 'motoboy'|
// 'expedicao' devem usar o modo RESOLVIDO, nunca 'misto' diretamente.
export type DeliveryMode = 'motoboy' | 'expedicao' | 'misto'

/** Componente/linha da oferta (produto + brindes/itens). */
export interface OfferComponent {
  nome: string
  qtd: number
}

/**
 * CHECKOUT-PERSONALIZADO (2026-06-19) — identidade white-label da oferta/produtor.
 *
 * O checkout reflete a MARCA DO PRODUTOR/OFERTA, nunca "FALKZ" generico. Estes
 * campos sao OPCIONAIS e ADITIVOS: o backend /offer ainda NAO os devolve (so
 * nome/preco/tipo/image_url/qty_sellable). Quando ausentes, o front cai no
 * fallback azul FALK (#1E6FF2) + nome da oferta — sem quebrar nada.
 *
 * Forward-compatible: assim que go/orders incluir `marca`/`logo_url`/`cor`/
 * `descricao` na resposta, a UI passa a renderizar a marca AUTOMATICAMENTE, sem
 * mudanca no front. Tambem aceitamos override via querystring (?brand=&cor=&logo=)
 * para o produtor carregar a marca pelo proprio link antes do backend persistir.
 */
export interface OfferBrand {
  /** Nome da marca/loja exibido no cabecalho. "" => usa o nome da oferta. */
  name: string
  /** URL do logo da marca. "" => marca textual (sem imagem). */
  logo_url: string
  /** Cor de acento (hex "#RRGGBB" ou "#RGB"). "" => fallback azul FALK. */
  accent: string
  banner_url: string
  whatsapp: string
  text_color: string
}

/** Oferta JA NORMALIZADA para a UI (shape interno do front, nao do backend). */
export interface OfferResponse {
  /** Token do link de checkout (injetado pelo front — o backend nao o devolve). */
  token: string
  /** Nome da oferta ja normalizado server-side. */
  name: string
  /** Preco em reais (float), derivado de oferta.preco ("349.00" -> 349). */
  value: number
  /** String pronta para exibir, ex.: "R$ 349,00". Sempre presente apos o mapeamento. */
  display_value: string
  /** Componentes da oferta (itens inclusos). Opcional — vazio quando o backend nao envia. */
  components?: OfferComponent[]
  /**
   * Descricao/headline da oferta (white-label). "" quando ausente. Texto curto
   * exibido no resumo para dar contexto/confianca ao comprador.
   */
  description: string
  /** Identidade white-label (marca/logo/cor) — ver OfferBrand. Sempre presente. */
  brand: OfferBrand
  /** URL da imagem (thumbnail) do produto. "" quando o backend nao tem fonte. */
  image_url: string
  /** motoboy = pague na entrega com agendamento; expedicao = correio/transportadora. */
  delivery_mode: DeliveryMode
  /** Fluxo sempre COD (pague na entrega). */
  cod: boolean
  /**
   * Quantidade disponivel para venda, vinda do backend (campo `qty_sellable`).
   * REGRA (aditiva, defensiva):
   *   null    -> backend NAO enviou o campo = estoque ilimitado (nenhum aviso).
   *   <= 0    -> produto esgotado (bloqueia finalizar).
   *   1..5    -> ultimas unidades (aviso discreto).
   *   > 5     -> disponivel normal (nenhum aviso).
   * IMPORTANTE: 0 e um valor REAL (esgotado) — nunca colapsar em null com `||`.
   */
  qty_sellable: number | null
}

/** Classificacao de uma data de entrega, decidida SERVER-SIDE pelo motor. */
export type ScheduleType = 'agendamento' | 'pre_agendado'

/** Status do pedido associado ao tipo da data. */
export type ScheduleStatus = 'agendado' | 'pre_agendado'

/** Uma data de entrega ofertada, JA classificada pelo backend. */
export interface ScheduleOption {
  /** Data ISO yyyy-mm-dd (valor enviado de volta no pedido). */
  value: string
  /** Rotulo amigavel pronto para exibir, ex.: "Sexta-feira 20/06". */
  label: string
  /** 'agendamento' (verde) ou 'pre_agendado' (laranja). */
  tipo: ScheduleType
  /** Status do pedido se esta data for escolhida. */
  status: ScheduleStatus
}

/** Resposta JA NORMALIZADA de /schedule (shape interno do front). */
export interface ScheduleResponse {
  /** Datas de entrega disponiveis, ja classificadas pelo motor. */
  dates: ScheduleOption[]
}

/**
 * Uma opcao de frete (correio/transportadora) cotada pelo backend — FEAT-FRETE.
 * O `id` e OPACO: o front so o devolve no POST; o servidor re-cota e casa o id
 * (CRIT-01 — nunca confiar no `price` do cliente).
 */
export interface FreightOption {
  /** Id opaco da opcao (ex.: "est-eco"|"est-exp"|id do servico ME). */
  id: string
  /** Transportadora (ex.: "Correios", "Jadlog"). */
  company: string
  /** Servico (ex.: "PAC", "SEDEX", "Econômico"). */
  service: string
  /** Preco em reais (number). EXIBICAO apenas — o servidor recalcula no pedido. */
  price: number
  /** Prazo estimado em dias. */
  delivery_days: number
  /** true = cotacao estimada (fallback, ME indisponivel). Mostra badge "estimado". */
  estimated: boolean
  /** true = transportadora preferida do produtor (config em Frete no portal). Mostra badge "Indicado" e vem no topo da lista (servidor ja ordena). */
  preferred: boolean
  /** true = frete travado pelo produtor (fixo por transportadora e/ou bloqueio de
   *  Correios) — o front NAO mostra o seletor, so aplica o preco direto no total. */
  locked: boolean
}

/** Resposta JA NORMALIZADA de /freight (shape interno do front). */
export interface FreightResponse {
  /** CEP de origem usado na cotacao (informativo). "" quando o backend nao envia. */
  origin_cep: string
  /** Opcoes de frete. Vazio = oferta motoboy (nao usa frete). */
  options: FreightOption[]
}

/** Erro padronizado (normalizado a partir de {ok:false, erro:...}). */
export interface ApiError {
  code: string // ex.: 'out_of_area' | 'http_404' | 'http_422'
  message: string
}

/** Dados do cliente coletados no checkout (shape interno do form). */
export interface CheckoutCustomer {
  /** Nome completo (campo unico). O backend grava como `nome`. */
  billing_name: string
  /**
   * CPF do cliente — FEAT-FRETE (2026-06-18). VOLTA somente para ofertas tipo
   * correio (os Correios exigem CPF do destinatario). Motoboy NAO envia.
   * Mascarado no form; enviado so com digitos no payload (campo `cpf`).
   */
  billing_cpf?: string
  billing_phone: string
  billing_postcode: string // CEP
  billing_address_1: string // rua
  billing_number: string // numero
  billing_address_2: string // complemento (opcional)
  billing_neighborhood: string // bairro
  billing_city: string
  billing_state: string // UF
  /** Data de entrega escolhida (somente motoboy). */
  sz_delivery_date?: string
  /** Tipo da data escolhida (informativo — o backend revalida). */
  sz_delivery_type?: ScheduleType
  /**
   * Id da opcao de frete escolhida — FEAT-FRETE (somente correio). O backend
   * RE-cota e casa este id, usando O PRECO DO SERVIDOR (CRIT-01). Sem preco aqui.
   */
  freight_id?: string
}

/** Corpo logico de POST /checkout-api/order. SEM preco. */
export interface CreateOrderRequest {
  token: string
  cliente: CheckoutCustomer
  /** FEAT-AFF-ATTRIBUTION: token de rastreio do afiliado (?r=). Opaco; o servidor
   *  decodifica → senderzz_affiliates.id e credita a venda. Ausente = sem afiliado. */
  ref?: string
}

/** Resposta JA NORMALIZADA de POST /checkout-api/order. */
export interface CreateOrderResponse {
  order_id: number
  order_number: string
  /**
   * Code ASSINADO do rastreio, formato "<order_number>-<sig>" (HMAC server-side).
   * E OPACO: nunca dividir/parsear no front — apenas repassar inteiro para a URL
   * de rastreio e o GET /order/{code}. A verificacao da assinatura e do backend.
   * Fallback para `order_number` quando o backend (ainda) nao manda.
   */
  tracking_code: string
  status: string // 'agendado' | 'pre_agendado' | 'pending'
  delivery_mode: DeliveryMode
  delivery_text?: string
  sz_delivery_date?: string
  sz_delivery_type?: ScheduleType
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers de transporte
// ─────────────────────────────────────────────────────────────────────────────

/** Erro tipado lancado pelas chamadas. */
export class CheckoutApiError extends Error {
  code: string
  /** Status HTTP, util para classificar (ex.: 422 do /schedule = out_of_area). */
  status: number
  constructor(err: ApiError, status: number) {
    super(err.message)
    this.code = err.code
    this.status = status
  }
}

/** Le o erro do backend. Formato: {ok:false, erro:"mensagem"} (httpx.WriteErr). */
async function parseError(res: Response): Promise<ApiError> {
  try {
    const body = await res.json()
    if (body && typeof body === 'object') {
      // httpx usa a chave `erro`; aceitamos tambem error/message por robustez.
      const msg: string =
        body.erro || body.error || (body.message as string) || ''
      const code: string = body.code || `http_${res.status}`
      if (msg) return { code, message: msg }
    }
  } catch {
    /* sem corpo JSON */
  }
  return { code: `http_${res.status}`, message: mensagemPadrao(res.status) }
}

function mensagemPadrao(status: number): string {
  if (status === 404) return 'Oferta não encontrada ou link inválido.'
  if (status === 422 || status === 400) return 'Verifique os dados informados.'
  if (status >= 500) return 'Tivemos um problema. Tente novamente em instantes.'
  return 'Não foi possível concluir. Tente novamente.'
}

/** Formata um valor float em BRL para exibicao. */
export function formatBRL(value: number): string {
  return new Intl.NumberFormat('pt-BR', {
    style: 'currency',
    currency: 'BRL',
  }).format(value)
}

/** Mapeia o `tipo` do link (motoboy|correio|misto|...) para o modo de entrega da UI. */
function deliveryModeFromTipo(tipo: string): DeliveryMode {
  const t = (tipo || '').trim().toLowerCase()
  if (t === 'motoboy') return 'motoboy'
  if (t === 'misto') return 'misto'
  return 'expedicao'
}

// ─────────────────────────────────────────────────────────────────────────────
// Chamadas
// ─────────────────────────────────────────────────────────────────────────────

/** Shape cru de oferta vindo do backend: { ok, oferta: {...} }. */
interface RawOffer {
  nome?: string
  preco?: string // "349.00"
  produtor_id?: number
  slug?: string
  tipo?: string // 'motoboy' | 'correio'
  disponivel?: boolean
  image_url?: string // thumbnail do produto; "" quando sem fonte (best-effort)
  // qty_sellable: opcional. Quando AUSENTE = estoque ilimitado (sem aviso). 0 = esgotado.
  qty_sellable?: number | null
  // CHECKOUT-PERSONALIZADO: campos white-label OPCIONAIS (backend ainda nao envia).
  // Aceitamos varios aliases por robustez ao shape futuro do go/orders.
  marca?: string
  brand_name?: string
  logo_url?: string
  cor?: string // hex de acento
  accent_color?: string
  banner_url?: string
  whatsapp?: string
  texto?: string
  descricao?: string
  description?: string
  // Itens/brindes inclusos na oferta (componentes). Opcional.
  componentes?: { nome?: string; qtd?: number }[]
  itens?: { nome?: string; qtd?: number }[]
}

/**
 * Sanitiza uma cor hex vinda de fonte nao confiavel (backend/querystring) para
 * uso seguro em CSS custom property. Aceita "#RGB"/"#RRGGBB" (com ou sem '#').
 * Qualquer coisa fora do formato -> "" (cai no fallback azul FALK). Evita injecao
 * de valor arbitrario na var CSS (ex.: "red;background:url(...)").
 */
export function sanitizeHexColor(raw: string | undefined | null): string {
  const v = (raw || '').trim()
  if (!v) return ''
  const hex = v.startsWith('#') ? v.slice(1) : v
  if (/^[0-9a-fA-F]{3}$/.test(hex) || /^[0-9a-fA-F]{6}$/.test(hex)) {
    return '#' + hex.toLowerCase()
  }
  return ''
}

/**
 * GET da oferta pelo token. Preco vem RESOLVIDO do servidor (oferta.preco, string
 * "349.00"), mapeado aqui para value (number) + display_value ("R$ 349,00").
 */
export async function fetchOffer(token: string): Promise<OfferResponse> {
  const res = await fetch(`${BASE}/offer?token=${encodeURIComponent(token)}`, {
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) throw new CheckoutApiError(await parseError(res), res.status)

  const body = (await res.json()) as { oferta?: RawOffer }
  const o: RawOffer = body.oferta || {}

  // preco e string "349.00" — Number() lida com isso; NaN -> 0 (defensivo).
  const parsed = Number(o.preco)
  const value = Number.isFinite(parsed) ? parsed : 0

  // CHECKOUT-PERSONALIZADO: monta a identidade white-label da oferta. Todos os
  // campos sao opcionais — quando ausentes, "" e o front aplica o fallback FALK.
  const brand: OfferBrand = {
    name: (o.marca || o.brand_name || '').trim(),
    logo_url: (o.logo_url || '').trim(),
    accent: sanitizeHexColor(o.cor || o.accent_color),
    banner_url: (o.banner_url || '').trim(),
    whatsapp: (o.whatsapp || '').trim(),
    text_color: sanitizeHexColor(o.texto),
  }
  // Componentes/itens inclusos (aceita 'componentes' ou 'itens'). So entradas com nome.
  const rawComp = o.componentes || o.itens || []
  const components: OfferComponent[] = rawComp
    .map((c) => ({ nome: (c.nome || '').trim(), qtd: typeof c.qtd === 'number' ? c.qtd : 1 }))
    .filter((c) => c.nome !== '')

  return {
    token, // backend nao devolve token — injetamos o argumento.
    name: o.nome || 'Oferta',
    value,
    display_value: formatBRL(value),
    description: (o.descricao || o.description || '').trim(),
    brand,
    components,
    image_url: (o.image_url || '').trim(), // "" quando o backend nao tem fonte.
    delivery_mode: deliveryModeFromTipo(o.tipo || ''),
    cod: true, // fluxo sempre pague-na-entrega.
    // ATENCAO: usar checagem de tipo (NAO `||`). 0 e esgotado, nao "ausente".
    // `o.qty_sellable || null` transformaria 0 -> null (ilimitado) — bug critico.
    qty_sellable:
      typeof o.qty_sellable === 'number' && Number.isFinite(o.qty_sellable)
        ? o.qty_sellable
        : null,
  }
}

/** Shape cru de data vinda do backend (agenda.datas[]). */
interface RawDate {
  data?: string // YYYY-MM-DD
  tipo?: string // 'agendamento' | 'pre_agendado'
  label?: string
}

/**
 * GET das datas de entrega disponiveis para o CEP (ETAPA 2, somente motoboy).
 *
 * Backend: { ok, agenda: { zona, frequencia, janela_dias, datas:[{data,tipo,label}] } }.
 * Mapeamos datas -> dates ({value,label,tipo,status}).
 *
 * "Fora de área": o backend responde 422 com {ok:false, erro:"fora de área..."}.
 * Como este metodo so e chamado para ofertas motoboy E com CEP de 8 digitos
 * (pre-condicoes garantidas no Checkout.tsx), qualquer 422 aqui significa, na
 * pratica, CEP fora de área de entrega — normalizamos para code 'out_of_area'.
 */
export async function fetchSchedule(
  token: string,
  cep: string
): Promise<ScheduleResponse> {
  const res = await fetch(
    `${BASE}/schedule?token=${encodeURIComponent(token)}&cep=${encodeURIComponent(cep)}`,
    { headers: { Accept: 'application/json' } }
  )
  if (!res.ok) {
    const err = await parseError(res)
    // 422 em /schedule (token motoboy + CEP valido) = fora de área de entrega.
    if (res.status === 422) err.code = 'out_of_area'
    throw new CheckoutApiError(err, res.status)
  }

  const body = (await res.json()) as { agenda?: { datas?: RawDate[] } }
  const raw = body.agenda?.datas || []
  const dates: ScheduleOption[] = raw.map((d) => {
    const tipo: ScheduleType =
      d.tipo === 'agendamento' ? 'agendamento' : 'pre_agendado'
    return {
      value: d.data || '',
      label: d.label || d.data || '',
      tipo,
      status: tipo === 'agendamento' ? 'agendado' : 'pre_agendado',
    }
  })
  return { dates }
}

/** Shape cru de uma opcao de frete vinda do backend (options[]). */
interface RawFreightOption {
  id?: string
  company?: string
  service?: string
  price?: number
  delivery_days?: number
  estimated?: boolean
  preferred?: boolean
  locked?: boolean
}

/**
 * GET das opcoes de frete para o CEP de destino (FEAT-FRETE, somente correio).
 *
 * ATENCAO ao SHAPE: diferente de /offer e /schedule (que aninham sob `oferta`/
 * `agenda`), o handler de frete espalha os campos no TOPO via httpx.WriteOK:
 *   { ok:true, origin_cep:"...", options:[{id,company,service,price,delivery_days,estimated}] }
 * Por isso lemos `body.options` e `body.origin_cep` DIRETO (sem chave wrapper).
 *
 * Oferta motoboy -> backend devolve `options: []` (motoboy nao usa frete).
 * token/cep invalidos -> 4xx {ok:false, erro} -> CheckoutApiError.
 */
export async function fetchFreight(
  token: string,
  cep: string
): Promise<FreightResponse> {
  const res = await fetch(
    `${BASE}/freight?token=${encodeURIComponent(token)}&cep=${encodeURIComponent(cep)}`,
    { headers: { Accept: 'application/json' } }
  )
  if (!res.ok) throw new CheckoutApiError(await parseError(res), res.status)

  const body = (await res.json()) as {
    origin_cep?: string
    options?: RawFreightOption[]
  }
  const raw = body.options || []
  const options: FreightOption[] = raw.map((o) => ({
    id: (o.id || '').trim(),
    company: (o.company || '').trim(),
    service: (o.service || '').trim(),
    price: typeof o.price === 'number' && Number.isFinite(o.price) ? o.price : 0,
    delivery_days:
      typeof o.delivery_days === 'number' && Number.isFinite(o.delivery_days)
        ? o.delivery_days
        : 0,
    estimated: o.estimated === true,
    preferred: o.preferred === true,
    locked: o.locked === true,
  }))
  return { origin_cep: (body.origin_cep || '').trim(), options }
}

// ─────────────────────────────────────────────────────────────────────────────
// LINK MISTO — GET /checkout-api/resolve (FEAT-LINK-MISTO)
//
// So chamado quando offer.delivery_mode === 'misto'. Descobre, pelo CEP, se o
// pedido vai por COD (motoboy) ou Expedicao — decisao do SERVIDOR (o POST /order
// re-resolve de novo, isto aqui e so preview/UX pra render o formulario certo).
// ─────────────────────────────────────────────────────────────────────────────

export async function fetchResolveMode(
  token: string,
  cep: string
): Promise<'motoboy' | 'expedicao'> {
  const res = await fetch(
    `${BASE}/resolve?token=${encodeURIComponent(token)}&cep=${encodeURIComponent(cep)}`,
    { headers: { Accept: 'application/json' } }
  )
  if (!res.ok) throw new CheckoutApiError(await parseError(res), res.status)
  const body = (await res.json()) as { mode?: string }
  return body.mode === 'motoboy' ? 'motoboy' : 'expedicao'
}

// ─────────────────────────────────────────────────────────────────────────────
// CONSENTIMENTO LGPD — POST /checkout-api/consent
//
// Registra o aceite da Politica de Privacidade / tratamento de dados (LGPD) antes
// de finalizar o pedido. O CHECKBOX OBRIGATORIO no front e quem BLOQUEIA o submit;
// esta chamada e o REGISTRO DE AUDITORIA do aceite (token do link + doc_version).
//
// Endpoint criado por go/orders NESTE MESMO LOTE. Por isso a chamada e
// BEST-EFFORT no Checkout.tsx (try/catch que nao quebra o fluxo): se o backend
// ainda nao estiver no ar / responder 404, a finalizacao do pedido NAO pode falhar.
// O gate de obrigatoriedade continua garantido client-side pelo checkbox.
//
// Body token-based ({ token, doc_version }) por consistencia com offer/schedule/
// freight — no momento da finalizacao ainda nao ha order_id.
// ─────────────────────────────────────────────────────────────────────────────

/** Resposta normalizada de POST /checkout-api/consent (campos best-effort). */
export interface ConsentResponse {
  /** Id do registro de consentimento gravado (0 quando o backend nao envia). */
  consent_id: number
  /** Versao do documento confirmada pelo servidor (eco do doc_version). */
  doc_version: string
}

/**
 * POST do aceite LGPD. Lanca CheckoutApiError em falha — o CHAMADOR decide se
 * ignora (best-effort no checkout). NAO envia dados pessoais: so token + versao.
 */
export async function postConsent(
  token: string,
  docVersion: string
): Promise<ConsentResponse> {
  const res = await fetch(`${BASE}/consent`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify({ token, doc_version: docVersion }),
  })
  if (!res.ok) throw new CheckoutApiError(await parseError(res), res.status)

  const body = (await res.json()) as {
    consent_id?: number
    doc_version?: string
  }
  return {
    consent_id:
      typeof body.consent_id === 'number' && Number.isFinite(body.consent_id)
        ? body.consent_id
        : 0,
    doc_version: (body.doc_version || docVersion).trim(),
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// RASTREIO — GET /checkout-api/order/{code}
//
// Contrato do backend (go/orders/internal/handlers/tracking.go):
//   { ok:true, rastreio: { code, status, status_timeline:[{key,label,at}],
//     entrega:{tipo,data,destino_bairro,cidade,uf}, itens:[{nome,qtd,total,image_url}],
//     cliente:{nome,telefone,email,cpf,endereco}, total } }
//
// REGRAS DE INTERPRETACAO (do proprio handler — nao mudar sem ler tracking.go):
//   - `status` (topo) e a FONTE DE VERDADE do progresso do stepper. NAO usar
//     `at != null` para decidir etapa atingida: `em_separacao` e SEMPRE null por
//     design e `a_caminho` pode ser null mesmo em pedido entregue (ladder nao
//     monotonico). Etapa atingida = indice <= indice de `status` no ladder.
//   - `entrega.data` e YYYY-MM-DD (data civil) — formatar com T00:00:00 local
//     para nao cair no dia anterior em BRT (UTC-3).
//   - `status_timeline[].at` e RFC3339 ja em horario de Sao Paulo — seguro com Date.
//   - `total` e `itens[].total` sao STRINGS ("349.00") — Number() antes de formatar.
//   - `cliente.telefone` vem SEM +55 (strip server-side). Para o whatsapp, re-adicionar.
//   - `image_url` frequentemente "" (imagem real e attachment do WP) — usar placeholder.
// ─────────────────────────────────────────────────────────────────────────────

/** Chaves canonicas do ladder de rastreio (ordem = ordem de exibicao). */
export type TrackingStageKey =
  | 'agendado'
  | 'em_separacao'
  | 'separado'
  | 'em_rota'
  | 'a_caminho'
  | 'completo'

/** Um passo do timeline. `at` = RFC3339 quando atingido, null caso contrario. */
export interface TrackingTimelineStep {
  key: string
  label: string
  at: string | null
}

/** Bloco de entrega do rastreio. */
export interface TrackingEntrega {
  tipo: DeliveryMode
  data: string | null // YYYY-MM-DD
  previsao_horario?: string // HH:00, somente Motoboy
  horario_entrega?: string // HH:MM, hora real quando entregue
  destino_bairro: string | null
  cidade: string | null
  uf: string | null
  codigo_rastreio: string | null // código real da transportadora (nunca nomeia a plataforma intermediária)
}

/** Item do pedido no rastreio. `total` e string ("349.00"). */
export interface TrackingItem {
  nome: string
  qtd: number
  total: string
  image_url: string
}

/** Dados do cliente no rastreio (todos opcionais). */
export interface TrackingCliente {
  nome: string | null
  telefone: string | null // SEM +55
  email: string | null
  cpf: string | null // 000.000.000-00
  endereco: string | null
}

/** Status confirmado agora na ME (consulta ao vivo, best-effort). null quando indisponível. */
export interface TrackingMELive {
  status: string
  checked_at: string
}

export interface TrackingCarrierEvent {
  at: string
  description: string
  location: string
  stage: string
}

/** Resposta JA NORMALIZADA de GET /checkout-api/order/{code}. */
export interface TrackingResponse {
  code: string
  status: string // chave do ladder OU terminal ('frustrado'|'cancelado'|'alerta')
  status_timeline: TrackingTimelineStep[]
  entrega: TrackingEntrega
  itens: TrackingItem[]
  cliente: TrackingCliente
  total: string // string ("349.00")
  me_live: TrackingMELive | null
  carrier_events: TrackingCarrierEvent[]
  brand: OfferBrand
}

/** Shape cru de rastreio vindo do backend (so o que consumimos). */
interface RawTracking {
  code?: string
  status?: string
  status_timeline?: { key?: string; label?: string; at?: string | null }[]
  entrega?: {
    tipo?: string
    data?: string | null
    previsao_horario?: string
    horario_entrega?: string
    destino_bairro?: string | null
    cidade?: string | null
    uf?: string | null
    codigo_rastreio?: string | null
  }
  itens?: { nome?: string; qtd?: number; total?: string; image_url?: string }[]
  cliente?: {
    nome?: string | null
    telefone?: string | null
    email?: string | null
    cpf?: string | null
    endereco?: string | null
  }
  total?: string
  me_live?: { status?: string; checked_at?: string } | null
  carrier_events?: { at?: string; description?: string; location?: string; stage?: string }[]
  brand?: { name?: string; logo_url?: string; cor?: string; banner_url?: string; whatsapp?: string; texto?: string }
}

/**
 * GET do rastreio pelo code (order_number "SZ-0000052" ou id "1584").
 * O code e o proprio segredo — endpoint publico, sem auth. 404 = pedido inexistente.
 */
export async function fetchTracking(code: string): Promise<TrackingResponse> {
  const res = await fetch(`${BASE}/order/${encodeURIComponent(code)}`, {
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) throw new CheckoutApiError(await parseError(res), res.status)

  const body = (await res.json()) as { rastreio?: RawTracking }
  const r: RawTracking = body.rastreio || {}

  const timeline: TrackingTimelineStep[] = (r.status_timeline || []).map((s) => ({
    key: (s.key || '').trim(),
    label: (s.label || '').trim(),
    at: s.at || null,
  }))

  return {
    code: r.code || code,
    status: (r.status || '').trim(),
    status_timeline: timeline,
    entrega: {
      tipo: deliveryModeFromTipo(r.entrega?.tipo || ''),
      data: r.entrega?.data || null,
      previsao_horario: r.entrega?.previsao_horario || '',
      horario_entrega: r.entrega?.horario_entrega || '',
      destino_bairro: r.entrega?.destino_bairro || null,
      cidade: r.entrega?.cidade || null,
      uf: r.entrega?.uf || null,
      codigo_rastreio: r.entrega?.codigo_rastreio || null,
    },
    itens: (r.itens || []).map((it) => ({
      nome: (it.nome || '').trim(),
      qtd: typeof it.qtd === 'number' ? it.qtd : 0,
      total: it.total || '0.00',
      image_url: (it.image_url || '').trim(),
    })),
    cliente: {
      nome: r.cliente?.nome || null,
      telefone: r.cliente?.telefone || null,
      email: r.cliente?.email || null,
      cpf: r.cliente?.cpf || null,
      endereco: r.cliente?.endereco || null,
    },
    total: r.total || '0.00',
    me_live: r.me_live && r.me_live.status ? { status: r.me_live.status, checked_at: r.me_live.checked_at || '' } : null,
    carrier_events: (r.carrier_events || []).map((e) => ({
      at: e.at || '',
      description: e.description || '',
      location: e.location || '',
      stage: e.stage || '',
    })),
    brand: {
      name: (r.brand?.name || '').trim(),
      logo_url: (r.brand?.logo_url || '').trim(),
      accent: sanitizeHexColor(r.brand?.cor),
      banner_url: (r.brand?.banner_url || '').trim(),
      whatsapp: (r.brand?.whatsapp || '').trim(),
      text_color: sanitizeHexColor(r.brand?.texto),
    },
  }
}

/** Shape cru da resposta do POST /order. */
interface RawOrder {
  order_id?: number
  order_number?: string
  /** Code assinado do rastreio "<order_number>-<sig>" (opaco — repassar inteiro). */
  tracking_code?: string
  data_entrega?: string // YYYY-MM-DD (so quando agendou)
  tipo?: string // 'agendado' | 'pre_agendado' (STATUS, ja mapeado server-side)
  requer_confirmacao?: boolean
  idempotente?: boolean
}

/**
 * POST do pedido. Envia o body FLAT esperado pelo backend (nome, cpf, telefone,
 * cep, logradouro, numero, complemento, bairro, cidade, uf, data_entrega, tipo).
 * SEM preco — o backend resolve pelo token.
 *
 * idempotencyKey: gerado uma vez por oferta — evita pedido duplicado.
 * (O backend tambem deduplica por token+cpf; o header e best-effort.)
 *
 * deliveryMode entra como argumento porque o backend NAO devolve delivery_mode;
 * a UI (ThankYou) precisa dele e o front ja o conhece pela oferta.
 */
export async function createOrder(
  body: CreateOrderRequest,
  idempotencyKey: string,
  deliveryMode: DeliveryMode
): Promise<CreateOrderResponse> {
  const c = body.cliente
  const isMotoboy = deliveryMode === 'motoboy'
  // Body FLAT conforme checkoutOrderRequest (Go). NOME e um campo unico.
  const flat = {
    token: body.token,
    nome: c.billing_name,
    // FEAT-FRETE (2026-06-18): cpf VOLTA somente para correio (os Correios exigem
    // CPF do destinatario). Motoboy NAO envia (backend aceita vazio). So digitos.
    ...(!isMotoboy && c.billing_cpf ? { cpf: c.billing_cpf.replace(/\D+/g, '') } : {}),
    telefone: c.billing_phone,
    email: '', // campo de e-mail removido do checkout — backend aceita vazio.
    cep: c.billing_postcode,
    logradouro: c.billing_address_1,
    numero: c.billing_number,
    complemento: c.billing_address_2,
    bairro: c.billing_neighborhood,
    cidade: c.billing_city,
    uf: c.billing_state,
    ...(c.sz_delivery_date ? { data_entrega: c.sz_delivery_date } : {}),
    // tipo enviado e informativo — o backend reclassifica server-side.
    ...(c.sz_delivery_type ? { tipo: c.sz_delivery_type } : {}),
    // FEAT-FRETE: freight_id da opcao escolhida (correio). SEM preco — o backend
    // re-cota e usa o preco DO SERVIDOR (CRIT-01). Motoboy nao envia.
    ...(!isMotoboy && c.freight_id ? { freight_id: c.freight_id } : {}),
    // AUDIT-2026-06-21 #11/#24: consentimento LGPD PERSISTE server-side via o proprio
    // POST /order (handler grava sz_order_meta + senderzz_consents na MESMA tx — ver
    // checkout.go). A rota /consent dedicada nunca existiu (404); o consentimento e
    // OBRIGATORIO no payload do pedido. O checkbox e o gate que BLOQUEIA o submit no
    // Checkout.tsx, entao quando createOrder roda o aceite ja e garantido => true fixo.
    // doc_version carimba QUAL versao da politica foi aceita (prova Art. 8 LGPD).
    consent_accepted: true,
    consent_doc_version: LGPD_DOC_VERSION,
    // FEAT-AFF-ATTRIBUTION: repassa o token de afiliado (?r=) quando presente. O
    // backend (checkout.go) decodifica → senderzz_affiliates.id, valida ativo e credita.
    ...(body.ref && body.ref.trim() ? { ref: body.ref.trim() } : {}),
  }

  const res = await fetch(`${BASE}/order`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
    body: JSON.stringify(flat),
  })
  if (!res.ok) {
    const err = await parseError(res)
    // 422 do /order com data_entrega = CEP saiu de área entre etapas; sinaliza.
    if (res.status === 422 && c.sz_delivery_date) err.code = 'out_of_area'
    throw new CheckoutApiError(err, res.status)
  }

  const o = (await res.json()) as RawOrder
  // Backend devolve `tipo` ja como STATUS ('agendado'|'pre_agendado').
  const status = o.tipo || (o.data_entrega ? 'agendado' : 'pending')
  const szType: ScheduleType | undefined = o.tipo
    ? o.tipo === 'agendado'
      ? 'agendamento'
      : 'pre_agendado'
    : undefined

  const orderNumber = o.order_number || ''
  // Code assinado e OPACO: repassamos inteiro. Fallback para o order_number cru
  // (ou id) quando o backend ainda nao emitir tracking_code — link continua valido.
  const trackingCode =
    (o.tracking_code && o.tracking_code.trim()) ||
    orderNumber ||
    String(o.order_id || '')

  return {
    order_id: o.order_id || 0,
    order_number: orderNumber,
    tracking_code: trackingCode,
    status,
    delivery_mode: deliveryMode, // injetado — o backend nao devolve.
    sz_delivery_date: o.data_entrega,
    sz_delivery_type: szType,
  }
}
