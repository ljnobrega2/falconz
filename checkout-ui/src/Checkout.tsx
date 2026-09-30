import { useEffect, useMemo, useRef, useState } from 'react'
import {
  fetchOffer,
  fetchSchedule,
  fetchFreight,
  fetchResolveMode,
  createOrder,
  postConsent,
  formatBRL,
  CheckoutApiError,
  LGPD_DOC_VERSION,
  type OfferResponse,
  type CheckoutCustomer,
  type ScheduleOption,
  type FreightOption,
} from './api'
import { emitToast } from './Toast'
import { track } from './analytics'
import {
  maskCEP,
  maskPhone,
  maskCPF,
  isValidCEP,
  isValidPhone,
  isValidCPF,
  lookupCEP,
} from './validation'
import type { OrderResult } from './App'

type FormState = {
  billing_name: string
  // FEAT-FRETE (2026-06-18): CPF VOLTA somente para correio (gate por !isMotoboy).
  // Motoboy ignora este campo (nunca exibido, nunca enviado).
  billing_cpf: string
  billing_phone: string
  billing_postcode: string
  billing_address_1: string
  billing_number: string
  billing_address_2: string
  billing_neighborhood: string
  billing_city: string
  billing_state: string
  sz_delivery_date: string
}

const EMPTY: FormState = {
  billing_name: '',
  billing_cpf: '',
  billing_phone: '',
  billing_postcode: '',
  billing_address_1: '',
  billing_number: '',
  billing_address_2: '',
  billing_neighborhood: '',
  billing_city: '',
  billing_state: '',
  sz_delivery_date: '',
}

type Errors = Partial<Record<keyof FormState, string>>

// Limite do campo "Complemento (Opcional)" — mesmo campo que ia pro backend
// como billing_address_2/complemento. Contador aparece à direita do label.
const COMPLEMENTO_MAX = 32

// Taxa administrativa padrão aplicada à simulação de parcelamento COD.
// O simulador é informativo: o pedido continua sendo criado como pagamento na
// entrega, mas todas as opções exibem o valor já acrescido da taxa padrão.
const ADMIN_FEE_PCT = 4.99

// Preposições/artigos PT-BR que ficam minúsculas no meio do nome (ex.: "Lucas
// de Jesus Nobrega"), exceto quando são a 1ª palavra. Espelha titleCaseName no
// backend (go/orders/internal/handlers/checkout.go) — aqui é só preview, o
// back normaliza de novo (rede de segurança contra POST direto na API).
const NAME_PARTICLES_LOWER = new Set(['de', 'da', 'do', 'das', 'dos', 'e'])
function titleCaseName(s: string): string {
  const words = s.trim().split(/\s+/).filter(Boolean)
  return words
    .map((w, i) => {
      const lw = w.toLowerCase()
      if (i > 0 && NAME_PARTICLES_LOWER.has(lw)) return lw
      return lw.charAt(0).toUpperCase() + lw.slice(1)
    })
    .join(' ')
}

// Secoes do acordeao (estilo Logzz, marca azul FALKZ):
//   info     -> Suas informacoes (nome/telefone — SEM CPF, CHECKOUT-NO-CPF 2026-06-18)
//   address  -> Endereco e entrega (cep/numero/complemento/observacao)
//   schedule -> Escolha o dia para receber (somente motoboy)
type Section = 'info' | 'address' | 'schedule'

// UUID estavel mesmo em browsers sem crypto.randomUUID.
function newUUID(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return 'fk-' + Date.now().toString(16) + '-' + Math.random().toString(16).slice(2)
}

export default function Checkout({
  token,
  affRef,
  onDone,
  onOffer,
}: {
  token: string
  // FEAT-AFF-ATTRIBUTION (2026-06-24): token de rastreio do afiliado vindo de ?r=.
  // OPACO no front (so repassa); o servidor decodifica/valida o afiliado ativo e
  // atribui a venda. Best-practice: identidade do afiliado SEMPRE resolvida no back.
  affRef?: string
  onDone: (r: OrderResult) => void
  // CHECKOUT-PERSONALIZADO: reporta a oferta carregada ao App para elevar a marca
  // white-label (logo/cor/nome no cabecalho/rodape). Opcional — fail-safe.
  onOffer?: (o: OfferResponse | null) => void
}) {
  const [loading, setLoading] = useState(true)
  const [offer, setOffer] = useState<OfferResponse | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [form, setForm] = useState<FormState>(EMPTY)
  const [errors, setErrors] = useState<Errors>({})
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [cepLoading, setCepLoading] = useState(false)

  // -- Acordeao: secao aberta no momento ------------------------------------
  // Substitui o antigo stepper de 2 etapas. As regras de liberacao progressiva
  // (infoOk -> step1Valid -> data) continuam identicas; muda so a apresentacao.
  const [section, setSection] = useState<Section>('info')

  // -- ETAPA 2: datas vindas do motor de agendamento (server-side) ----------
  const [scheduleLoading, setScheduleLoading] = useState(false)
  const [scheduleError, setScheduleError] = useState<string | null>(null)
  const [dates, setDates] = useState<ScheduleOption[]>([])
  // Aba de datas (2026-07-28): separa Agendamento/Pré-agendamento em toggle na
  // etapa 3 — só apresentação, mesma lista/regra de datas de antes (evita a
  // grade gigante com Pré-agendamento dominando visualmente por padrão).
  const [dateTab, setDateTab] = useState<'agendamento' | 'pre_agendado'>('agendamento')
  // CEP para o qual a zona foi resolvida — evita refetch-loop e datas stale.
  const [scheduledForCep, setScheduledForCep] = useState<string>('')
  // Resultado da resolucao de zona do CEP atual (motoboy):
  //   null      -> ainda nao resolvido
  //   'in'      -> zona OK, datas carregadas, endereco/etapa 2 liberados
  //   'out'     -> fora de área (popup); finalizacao motoboy bloqueada
  const [zoneStatus, setZoneStatus] = useState<null | 'in' | 'out'>(null)

  // -- Trava de endereco pelo ViaCEP -----------------------------------------
  // Rua/bairro travam (read-only) quando o ViaCEP devolve o dado pra aquele CEP
  // (endereco exato — nunca editavel manualmente, em nenhuma hipotese). Em CEP
  // "unico" (regiao sem rua/bairro no ViaCEP) libera digitar rua/bairro na mao.
  // Cidade/UF NUNCA sao editaveis manualmente, com ou sem CEP unico.
  const [streetLocked, setStreetLocked] = useState(true)
  const [neighborhoodLocked, setNeighborhoodLocked] = useState(true)

  // -- LINK MISTO (FEAT-LINK-MISTO) -----------------------------------------
  // offer.delivery_mode === 'misto' -> modo real (motoboy|expedicao) so se sabe
  // apos o CEP, via fetchResolveMode. null = ainda nao resolvido (nao mostra
  // nem o fluxo motoboy nem o de frete ate saber).
  const [mistoMode, setMistoMode] = useState<null | 'motoboy' | 'expedicao'>(null)
  const [mistoResolving, setMistoResolving] = useState(false)
  const [mistoForCep, setMistoForCep] = useState<string>('')

  // -- FRETE (correio/expedicao) — FEAT-FRETE -------------------------------
  // Cotacao server-side a partir do CEP. Motoboy NAO usa (tudo inert sob !isMotoboy).
  const [freightLoading, setFreightLoading] = useState(false)
  const [freightError, setFreightError] = useState<string | null>(null)
  const [freightOptions, setFreightOptions] = useState<FreightOption[]>([])
  // Id da opcao escolhida pelo cliente (null = nenhuma). SEM preco — o servidor recalcula.
  const [freightId, setFreightId] = useState<string | null>(null)
  // CEP para o qual o frete foi cotado — evita refetch-loop e opcoes stale.
  const [freightForCep, setFreightForCep] = useState<string>('')

  // -- Cupom (placeholder local — sem sistema de cupom no backend) -----------
  const [coupon, setCoupon] = useState('')
  const [couponMsg, setCouponMsg] = useState<string | null>(null)

  // -- Simular parcelamento (apenas informativo — tudo "na entrega") --------
  const [installments, setInstallments] = useState<number>(1)

  // -- Popup de aviso para pre-agendamento ----------------------------------
  const [preWarning, setPreWarning] = useState<ScheduleOption | null>(null)
  // -- Popup "não entregamos por motoboy nesse CEP" -------------------------
  const [outOfAreaPopup, setOutOfAreaPopup] = useState(false)

  // Idempotencia: uma chave por oferta carregada. Nao regenerar ao navegar.
  const idempotencyKey = useRef<string>(newUUID())

  // -- OTIMIZACAO: evita ViaCEP/zona redundantes ----------------------------
  // Ultimo CEP (8 digitos) ja resolvido com sucesso (ViaCEP + zona). Enquanto o
  // CEP nao mudar, blur/auto-trigger nao refazem as chamadas.
  const lastLookedUpCep = useRef<string>('')
  // Timer do debounce do auto-lookup ao completar 8 digitos.
  const cepDebounce = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Limpa o timer pendente ao desmontar (evita setState em componente morto).
  useEffect(() => {
    return () => {
      if (cepDebounce.current) clearTimeout(cepDebounce.current)
    }
  }, [])

  const isMisto = offer?.delivery_mode === 'misto'
  // Pra misto, o modo efetivo so existe depois do CEP resolver (mistoMode).
  // Antes disso isMotoboy fica false (nao mostra nem motoboy nem frete —
  // ver mistoResolving usado pra gatear as secoes dependentes de endereco).
  const isMotoboy = isMisto ? mistoMode === 'motoboy' : offer?.delivery_mode === 'motoboy'
  // Oferta de expedicao (correio/transportadora) nao agenda data.
  const hasScheduleStep = isMotoboy

  // -- Carrega a oferta pelo token ------------------------------------------
  useEffect(() => {
    let alive = true
    if (!token) {
      setLoading(false)
      setLoadError(
        'Link de checkout inválido. Verifique o endereço ou peça um novo link ao vendedor.'
      )
      return
    }
    setLoading(true)
    setLoadError(null)
    fetchOffer(token)
      .then((o) => {
        if (!alive) return
        setOffer(o)
        onOffer?.(o) // CHECKOUT-PERSONALIZADO: eleva a marca no App (header/rodape).
        idempotencyKey.current = newUUID()
      })
      .catch((e: unknown) => {
        if (!alive) return
        const msg =
          e instanceof CheckoutApiError
            ? e.message
            : 'Não foi possível carregar a oferta. Tente novamente.'
        setLoadError(msg)
      })
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [token])

  useEffect(() => {
    if (!offer) return
    track('view_item', {
      item_id: offer.token,
      item_name: offer.name,
      value: offer.value,
      currency: 'BRL',
      delivery_mode: offer.delivery_mode,
    })
  }, [offer?.token])

  const priceLabel = useMemo(() => {
    if (!offer) return ''
    return offer.display_value || formatBRL(offer.value)
  }, [offer])

  // -- Disponibilidade (aviso vindo do back: qty_sellable) -------------------
  // Aditivo: quando o backend NAO envia qty_sellable (null) => ilimitado, sem
  // aviso e sem bloqueio (fluxo sem-CPF inalterado). 0 e um valor REAL (esgotado).
  //   qty <= 0   -> esgotado (bloqueia finalizar)
  //   1..5       -> ultimas unidades (aviso discreto)
  //   null/>5    -> nada
  const qtySellable = offer ? offer.qty_sellable : null
  const isSoldOut = qtySellable !== null && qtySellable <= 0
  const isLowStock = qtySellable !== null && qtySellable >= 1 && qtySellable <= 5

  // ===========================================================================
  // LIBERACAO PROGRESSIVA — cada campo so habilita apos o anterior estar VALIDO.
  // Ordem info: Nome completo -> Telefone. (CHECKOUT-NO-CPF 2026-06-18: sem CPF.)
  // Ordem endereco: CEP -> (zona OK / motoboy) endereco.
  // ===========================================================================
  // Exige nome + sobrenome (>=2 palavras com >=2 letras cada) — nome sozinho
  // ("Isaias") não é endereçável na entrega/motoboy.
  const nameOk = form.billing_name.trim().split(/\s+/).filter(w => w.length >= 2).length >= 2
  // FEAT-FRETE: CPF obrigatorio SOMENTE para correio. Motoboy => sempre "ok".
  const cpfOk = isMotoboy || isValidCPF(form.billing_cpf)
  const phoneOk = isValidPhone(form.billing_phone)
  const cepOk = isValidCEP(form.billing_postcode)

  // Secao 1 (Suas informacoes) completa. Correio exige CPF valido.
  const infoOk = nameOk && cpfOk && phoneOk

  // FEAT-FRETE: frete escolhido (correio). Motoboy => sempre "ok" (nao usa frete).
  const freightOk = isMotoboy || freightId !== null
  const selectedFreight = freightOptions.find((o) => o.id === freightId) || null

  // Lista exibida: quando ha preferida real (config do produtor), o backend ja
  // manda ordenada com ela no topo. SEM preferida configurada, o default e a mais
  // barata (mesma que ja e pre-selecionada) — ela tambem sobe pro topo e ganha o
  // mesmo destaque visual (badge + pulso), senao o "default" fica escondido no
  // meio da lista sem nenhuma evidencia de que e a opcao recomendada.
  const displayFreightOptions = useMemo(() => {
    if (freightOptions.length === 0) return freightOptions
    if (freightOptions.some((o) => o.preferred)) return freightOptions
    const cheapest = freightOptions.reduce((a, b) => (b.price < a.price ? b : a))
    const rest = freightOptions.filter((o) => o.id !== cheapest.id)
    return [{ ...cheapest, preferred: true }, ...rest]
  }, [freightOptions])

  // Para motoboy o endereco so libera com zona resolvida (in). Para expedicao
  // basta o CEP ser valido (sem agendamento / sem zona).
  const addressUnlocked = isMotoboy ? cepOk && zoneStatus === 'in' : cepOk

  const enabled = {
    name: true,
    cpf: nameOk,
    phone: isMotoboy ? nameOk : nameOk && cpfOk,
    cep: true,
    address: addressUnlocked,
    // Rua/bairro: editaveis so em CEP unico (ViaCEP nao devolveu o dado). Fora
    // isso, travados — o endereco exato do ViaCEP nunca e sobrescritivel a mao.
    street: addressUnlocked && !streetLocked,
    neighborhood: addressUnlocked && !neighborhoodLocked,
    // Cidade/UF: NUNCA editaveis manualmente (sempre o valor do ViaCEP).
    cityState: false,
  }

  // Endereco completo (todos os campos obrigatorios preenchidos) — so a partir
  // daqui a etapa 3 (modalidades de frete) calcula/libera. Evita cotar/mostrar
  // frete assim que o CEP resolve (ainda faltando numero/complemento etc) e o
  // usuario dar Enter cedo demais sem os campos necessarios preenchidos.
  const addressComplete =
    form.billing_address_1.trim() !== '' &&
    form.billing_number.trim() !== '' &&
    form.billing_neighborhood.trim() !== '' &&
    form.billing_city.trim() !== '' &&
    form.billing_state.trim() !== ''

  // Secao 2 (Endereco e entrega) completa e valida — gate p/ liberar a data /
  // finalizar (expedicao). Correio tambem exige uma opcao de frete escolhida.
  const step1Valid =
    nameOk &&
    cpfOk &&
    phoneOk &&
    cepOk &&
    (isMotoboy ? zoneStatus === 'in' : freightOk) &&
    addressComplete

  // -- Resumo / parcelamento ------------------------------------------------
  const orderValue = offer?.value || 0
  // FEAT-FRETE: preco do frete selecionado (correio). Motoboy => 0.
  const freightPrice = selectedFreight ? selectedFreight.price : 0
  // Total exibido = valor da oferta + frete escolhido. (Motoboy: frete 0.)
  // Correio/expedicao SEM frete escolhido ainda -> exibe so o subtotal (produto);
  // o frete (com markup) so entra na soma depois de escolhido.
  // OBS (CRIT-01): este total e SO EXIBICAO; o backend recalcula o frete no POST.
  const totalValue = orderValue + freightPrice
  const totalLabel = useMemo(() => formatBRL(totalValue), [totalValue])
  // Parcelamento informativo: SO no fluxo motoboy (correio/expedicao nunca mostra —
  // o total muda quando o frete e escolhido, simular parcela antes disso confunde).
  const parcelas = useMemo(() => {
    // 1x..12x de R$ X/n — todas "na entrega" (apenas simulacao informativa).
    // A taxa administrativa padrão incide uma vez sobre o total e depois o
    // valor acrescido é dividido pelo número de parcelas.
    if (!isMotoboy || totalValue <= 0) return [] as { n: number; label: string }[]
    const totalComTaxa = totalValue * (1 + ADMIN_FEE_PCT / 100)
    return Array.from({ length: 12 }, (_, i) => {
      const n = i + 1
      return {
        n,
        label: `${n}x de ${formatBRL(totalComTaxa / n)} na entrega`,
      }
    })
  }, [totalValue])

  // -- Campo helpers --------------------------------------------------------
  function setField<K extends keyof FormState>(key: K, value: string) {
    setForm((f) => ({ ...f, [key]: value }))
    if (errors[key]) setErrors((e) => ({ ...e, [key]: undefined }))
    if (submitError) setSubmitError(null)
  }

  // Mudar o CEP invalida qualquer zona/datas resolvidas para o CEP anterior.
  // OTIMIZACAO: ao completar 8 digitos, dispara o lookup com DEBOUNCE (350ms) —
  // assim digitar/colar nao gera uma cascata de requests; o blur tambem dispara
  // (imediato), mas o guard de CEP repetido evita refazer o que ja foi resolvido.
  function onCepChange(v: string) {
    const masked = maskCEP(v)
    setField('billing_postcode', masked)
    // CEP mudou -> invalida resolucoes anteriores e libera novo lookup.
    setZoneStatus(null)
    setDates([])
    setDateTab('agendamento')
    setScheduledForCep('')
    setScheduleError(null)
    // Numero e do endereco ANTERIOR — CEP novo pode ser outra rua inteira, o
    // numero antigo nao faz sentido nela. Limpa junto (pedido dono 2026-07-28).
    setForm((f) => ({ ...f, sz_delivery_date: '', billing_number: '' }))
    lastLookedUpCep.current = ''
    // Trava rua/bairro de novo por padrao ate o proximo ViaCEP confirmar (evita
    // deixar campo destravado de um CEP unico anterior enquanto o novo carrega).
    setStreetLocked(true)
    setNeighborhoodLocked(true)
    // FEAT-FRETE: CEP mudou -> invalida cotacao/escolha de frete (correio).
    setFreightOptions([])
    setFreightId(null)
    setFreightForCep('')
    setFreightError(null)
    // LINK MISTO: CEP mudou -> invalida o modo resolvido (precisa re-resolver).
    setMistoMode(null)
    setMistoForCep('')

    if (cepDebounce.current) clearTimeout(cepDebounce.current)
    if (isValidCEP(masked)) {
      cepDebounce.current = setTimeout(() => {
        void runCepLookup(masked)
      }, 350)
    }
  }

  // CEP perdeu o foco -> resolve ja (sem esperar o debounce).
  function onCepBlur() {
    if (cepDebounce.current) clearTimeout(cepDebounce.current)
    void runCepLookup(form.billing_postcode)
  }

  // CEP valido -> ViaCEP (autofill) + (motoboy) resolve zona/datas.
  // OTIMIZACAO: corta requests redundantes (blur + auto-trigger no mesmo CEP) com
  // um guard duplo:
  //   - cepLoading/scheduleLoading -> ja ha lookup em andamento, nao reentra;
  //   - lastLookedUpCep -> ViaCEP ja preencheu o endereco deste CEP, nao repete.
  // A zona (motoboy) e re-resolvida quando ainda nao esta 'in' para o CEP atual,
  // permitindo retry apos erro de rede sem re-bater no ViaCEP.
  async function runCepLookup(cep: string) {
    if (!isValidCEP(cep)) return
    if (cepLoading || scheduleLoading) return // lookup em andamento

    const alreadyAutofilled = cep === lastLookedUpCep.current

    if (!alreadyAutofilled) {
      // 1) ViaCEP — preenche rua/bairro/cidade/UF (so quando vazio p/ rua/bairro).
      setCepLoading(true)
      const r = await lookupCEP(cep)
      setCepLoading(false)
      if (!r) {
        setErrors((e) => ({
          ...e,
          billing_postcode: 'CEP não encontrado. Confira o número.',
        }))
        // CEP nao encontrado no ViaCEP -> nao cota frete (nao faz sentido mostrar
        // opcoes/precos de envio pra um CEP que nao existe). O usuario corrige o
        // CEP; a cotacao roda de novo no proximo lookup bem-sucedido.
        setFreightOptions([])
        setFreightId(null)
        setFreightForCep('')
        return
      }
      // Rua/bairro: o ViaCEP MANDA nesse CEP (sempre sobrescreve — nunca mantém
      // valor de um CEP anterior, mesmo que o campo já estivesse preenchido).
      // CEP único (ViaCEP devolve vazio) destrava o campo E limpa pra
      // preenchimento do zero — não herda texto de um CEP anterior digitado
      // manualmente. Cidade/UF: sempre o valor do ViaCEP.
      const streetFromCep = r.logradouro.trim() !== ''
      const neighborhoodFromCep = r.bairro.trim() !== ''
      setStreetLocked(streetFromCep)
      setNeighborhoodLocked(neighborhoodFromCep)
      setForm((f) => ({
        ...f,
        billing_address_1: streetFromCep ? r.logradouro : '',
        billing_neighborhood: neighborhoodFromCep ? r.bairro : '',
        billing_city: r.localidade || f.billing_city,
        billing_state: r.uf || f.billing_state,
      }))
      lastLookedUpCep.current = cep
    }

    // 1b) LINK MISTO -> descobre COD ou Expedicao pelo CEP ANTES de decidir o
    // fluxo abaixo. So re-resolve se o CEP mudou (evita refetch em cada blur).
    let effectiveIsMotoboy = isMotoboy
    if (isMisto && mistoForCep !== cep) {
      setMistoResolving(true)
      setMistoMode(null)
      try {
        const mode = await fetchResolveMode(offer!.token, cep)
        setMistoMode(mode)
        setMistoForCep(cep)
        effectiveIsMotoboy = mode === 'motoboy'
      } catch {
        // Falha ao resolver -> cai pra Expedicao (mais conservador: sempre
        // pede CPF/cota frete; nunca finaliza como COD sem confirmar zona).
        setMistoMode('expedicao')
        setMistoForCep(cep)
        effectiveIsMotoboy = false
      } finally {
        setMistoResolving(false)
      }
    }

    // 2) Motoboy -> resolve a zona (regra do dono). Expedicao nao agenda.
    // So resolve se ainda nao esta 'in' para este CEP (evita refetch de datas
    // ja carregadas; permite retry quando uma tentativa anterior falhou).
    if (effectiveIsMotoboy && !(zoneStatus === 'in' && scheduledForCep === cep)) {
      await resolveZone(cep)
    }

    // 3) Correio (FEAT-FRETE): a cotacao NAO dispara aqui — so quando o endereco
    // estiver COMPLETO (ver useEffect abaixo). Cotar so com o CEP permitia dar
    // Enter/avancar com numero/bairro ainda vazios.
  }

  // -- Cota o frete pelo CEP (correio) — FEAT-FRETE -------------------------
  // Pre-condicoes: !isMotoboy e CEP de 8 digitos. Em falha de ME o backend ja
  // devolve opcoes estimadas (estimated:true) — aqui so tratamos erro de rede/4xx.
  async function resolveFreight(cep: string) {
    if (!offer) return
    setFreightLoading(true)
    setFreightError(null)
    setFreightOptions([])
    setFreightId(null)
    try {
      const r = await fetchFreight(offer.token, cep)
      const list = r.options || []
      setFreightOptions(list)
      setFreightForCep(cep)
      // Pre-seleciona a opcao preferida do produtor (config em Frete no portal); sem
      // preferida, cai pra mais barata. Conveniencia — o usuario pode trocar.
      // CRIT-01: o backend re-cota e casa o id no POST — a escolha aqui e so UX.
      if (list.length > 0) {
        const preferred = list.find((o) => o.preferred)
        const cheapest = list.reduce((a, b) => (b.price < a.price ? b : a))
        setFreightId((preferred || cheapest).id)
      }
    } catch (err: unknown) {
      const msg =
        err instanceof CheckoutApiError
          ? err.message
          : 'Não foi possível calcular o frete para este CEP. Tente novamente.'
      setFreightError(msg)
      setFreightOptions([])
      setFreightId(null)
      setFreightForCep('')
    } finally {
      setFreightLoading(false)
    }
  }

  // Dispara a cotacao de frete SO quando o endereco esta completo (nao so o
  // CEP) — evita mostrar/calcular modalidades com numero/bairro ainda vazios,
  // e evita cotar de novo se o usuario so mudou complemento/referencia depois.
  useEffect(() => {
    if (isMotoboy || !offer) return
    // LINK MISTO: so cota frete depois do CEP resolver o modo (senao cotaria
    // frete pra um CEP que ainda pode virar motoboy).
    if (isMisto && (mistoResolving || mistoForCep !== form.billing_postcode)) return
    if (!cepOk || errors.billing_postcode) return
    if (!addressComplete) return
    const cep = form.billing_postcode
    if (freightForCep === cep && (freightOptions.length > 0 || freightLoading)) return
    void resolveFreight(cep)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isMotoboy, offer, cepOk, errors.billing_postcode, addressComplete, form.billing_postcode, isMisto, mistoResolving, mistoForCep])

  // -- Resolve zona pelo CEP (motoboy) --------------------------------------
  // Pre-condicoes garantidas: isMotoboy === true e CEP de 8 digitos. Por isso
  // qualquer 422 do /schedule e tratado como out_of_area (ver api.ts).
  async function resolveZone(cep: string) {
    if (!offer) return
    setScheduleLoading(true)
    setScheduleError(null)
    setDates([])
    setForm((f) => ({ ...f, sz_delivery_date: '' }))
    try {
      const r = await fetchSchedule(offer.token, cep)
      const list = r.dates || []
      if (list.length === 0) {
        // Zona resolvida porem sem datas ofertaveis = fora de área na pratica.
        setZoneStatus('out')
        setScheduledForCep(cep)
        setOutOfAreaPopup(true)
        return
      }
      setDates(list)
      setZoneStatus('in')
      setScheduledForCep(cep)
      // pre-seleciona a primeira 'agendamento' (ou a primeira disponivel).
      const first = list.find((d) => d.tipo === 'agendamento') || list[0]
      if (first) {
        setForm((f) => ({ ...f, sz_delivery_date: first.value }))
      }
    } catch (err: unknown) {
      if (err instanceof CheckoutApiError && err.code === 'out_of_area') {
        // CEP fora de área de entrega por motoboy -> popup bloqueante.
        setZoneStatus('out')
        setScheduledForCep(cep)
        setOutOfAreaPopup(true)
      } else {
        const msg =
          err instanceof CheckoutApiError
            ? err.message
            : 'Não foi possível verificar a entrega para este CEP. Tente novamente.'
        setScheduleError(msg)
        setZoneStatus(null)
      }
    } finally {
      setScheduleLoading(false)
    }
  }

  // -- Concluir secao 1 (Suas informacoes) -> abre Endereco -----------------
  function continueFromInfo() {
    setSubmitError(null)
    if (!infoOk) {
      const e: Errors = {}
      if (!nameOk) e.billing_name = 'Informe nome e sobrenome do cliente.'
      // FEAT-FRETE: CPF so e exigido p/ correio (cpfOk ja e true em motoboy).
      if (!cpfOk) e.billing_cpf = 'CPF inválido.'
      if (!phoneOk) e.billing_phone = 'Telefone inválido (DDD + número).'
      setErrors(e)
      return
    }
    track('begin_checkout', { item_id: offer?.token, value: offer?.value, currency: 'BRL' })
    setSection('address')
  }

  // -- Confirmar endereco (secao 2) -> abre Data (motoboy) ou finaliza ------
  // Equivale ao antigo `goToStep2`: valida step1Valid, expedicao finaliza
  // direto, motoboy avanca para escolher a data (re-resolvendo a zona se preciso).
  async function confirmAddress() {
    if (!offer) return
    setSubmitError(null)
    if (!step1Valid) {
      // Marca os campos faltantes para feedback.
      const e: Errors = {}
      if (!nameOk) e.billing_name = 'Informe nome e sobrenome do cliente.'
      if (!cpfOk) e.billing_cpf = 'CPF inválido.'
      if (!phoneOk) e.billing_phone = 'Telefone inválido (DDD + número).'
      if (!cepOk) e.billing_postcode = 'CEP inválido.'
      if (!form.billing_address_1.trim()) e.billing_address_1 = 'Informe o endereço.'
      if (!form.billing_number.trim()) e.billing_number = 'Informe o número.'
      if (!form.billing_neighborhood.trim()) e.billing_neighborhood = 'Informe o bairro.'
      if (!form.billing_city.trim()) e.billing_city = 'Informe a cidade.'
      if (!form.billing_state.trim()) e.billing_state = 'UF.'
      // FEAT-FRETE: feedback de frete nao escolhido (correio) vai no bloco de frete.
      if (!isMotoboy && !freightOk && freightOptions.length > 0) {
        setFreightError('Selecione uma opção de frete.')
      }
      setErrors(e)
      return
    }
    // Expedicao: sem etapa de agendamento -> finaliza direto.
    track('add_shipping_info', {
      item_id: offer.token,
      delivery_mode: isMotoboy ? 'motoboy' : 'expedicao',
    })
    if (!hasScheduleStep) {
      await doSubmit()
      return
    }
    // Motoboy: zona ja resolvida (in). Abre a secao de data.
    setSection('schedule')
    // Garante datas para o CEP atual (caso o usuario tenha trocado algo).
    const cep = form.billing_postcode
    if (scheduledForCep !== cep || dates.length === 0) {
      await resolveZone(cep)
    }
  }

  // -- Selecao de data ------------------------------------------------------
  function onSelectDate(d: ScheduleOption) {
    setForm((f) => ({ ...f, sz_delivery_date: d.value }))
    // Pre-agendamento: abre popup informativo (NAO limpa a selecao).
    if (d.tipo === 'pre_agendado') {
      setPreWarning(d)
    }
  }

  // -- Cupom (placeholder) — sem sistema de cupom: sempre "cupom inválido". --
  function applyCoupon() {
    const c = coupon.trim()
    if (!c) {
      setCouponMsg('Digite um cupom.')
      return
    }
    setCouponMsg('Cupom inválido.')
  }

  // -- Submit final ---------------------------------------------------------
  async function doSubmit() {
    if (submitting || !offer) return
    // Esgotado: chokepoint unico — cobre expedicao (confirmAddress->doSubmit),
    // motoboy (submit da secao data) E o Enter (onSubmit chama doSubmit).
    // O `disabled` dos botoes nao basta: Enter ignora disabled. Bare return: o
    // aviso "Produto esgotado" (azul da marca) ja esta sempre visivel no resumo;
    // nao usar setSubmitError aqui para nao mostrar alerta vermelho fora de spec.
    if (isSoldOut) return
    setSubmitError(null)

    // LGPD: consentimento passa a ser por AÇÃO (clicar Finalizar = concordar —
    // ver ConsentNotice). postConsent() abaixo registra o aceite no ato deste
    // clique, sem checkbox/gate separado.

    if (hasScheduleStep) {
      if (zoneStatus !== 'in') {
        setOutOfAreaPopup(true)
        return
      }
      if (!form.sz_delivery_date) {
        setErrors((e) => ({ ...e, sz_delivery_date: 'Escolha uma data de entrega.' }))
        return
      }
    }

    // FEAT-FRETE: correio exige CPF valido + opcao de frete escolhida. Guard
    // defensivo (o Enter ignora o `disabled` do botao). Motoboy nao entra aqui.
    if (!isMotoboy) {
      if (!cpfOk) {
        setErrors((e) => ({ ...e, billing_cpf: 'CPF inválido.' }))
        setSection('info')
        return
      }
      if (!freightOk) {
        setFreightError('Selecione uma opção de frete.')
        return
      }
    }

    const selected = dates.find((d) => d.value === form.sz_delivery_date)

    // Cliente. NAO enviamos preco — o backend resolve pelo token.
    // FEAT-FRETE: correio envia CPF + freight_id (SEM preco). Motoboy: nenhum dos dois.
    const cliente: CheckoutCustomer = {
      billing_name: form.billing_name.trim(),
      billing_phone: form.billing_phone,
      billing_postcode: form.billing_postcode,
      billing_address_1: form.billing_address_1.trim(),
      billing_number: form.billing_number.trim(),
      billing_address_2: form.billing_address_2.trim(),
      billing_neighborhood: form.billing_neighborhood.trim(),
      billing_city: form.billing_city.trim(),
      billing_state: form.billing_state.trim().toUpperCase().slice(0, 2),
      ...(!isMotoboy
        ? {
            billing_cpf: form.billing_cpf,
            ...(freightId ? { freight_id: freightId } : {}),
          }
        : {}),
      ...(hasScheduleStep && form.sz_delivery_date
        ? {
            sz_delivery_date: form.sz_delivery_date,
            ...(selected ? { sz_delivery_type: selected.tipo } : {}),
          }
        : {}),
    }

    setSubmitting(true)
    try {
      // LGPD: registra o aceite ANTES de criar o pedido. BEST-EFFORT — o endpoint
      // /consent e criado por go/orders neste mesmo lote; se ainda nao estiver no
      // ar (404) ou falhar, NAO quebra o pedido. O gate obrigatorio ja foi
      // garantido acima pelo checkbox; este POST e o registro de auditoria.
      try {
        await postConsent(offer.token, LGPD_DOC_VERSION)
      } catch {
        /* best-effort — nao bloquear a finalizacao do pedido */
      }

      const order = await createOrder(
        { token: offer.token, cliente, ref: affRef },
        idempotencyKey.current,
        offer.delivery_mode
      )
      track('purchase', {
        transaction_id: order.order_number || String(order.order_id),
        item_id: offer.token,
        item_name: offer.name,
        value: offer.value,
        currency: 'BRL',
        delivery_mode: offer.delivery_mode,
      })
      onDone({
        order,
        offer,
        customerName: cliente.billing_name,
        selectedDate: selected || null,
      })
    } catch (err: unknown) {
      if (err instanceof CheckoutApiError) {
        if (err.code === 'out_of_area') {
          // CEP saiu de área entre etapas (motoboy) -> popup + volta ao endereco.
          setSection('address')
          setZoneStatus('out')
          setOutOfAreaPopup(true)
        } else {
          setSubmitError(err.message)
          emitToast('err', err.message)
        }
      } else {
        const msg = 'Não foi possível concluir o pedido. Tente novamente.'
        setSubmitError(msg)
        emitToast('err', msg)
      }
      setSubmitting(false)
    }
  }

  function onSubmit(ev: React.FormEvent) {
    ev.preventDefault()
    // Enter na secao info -> avanca para endereco.
    if (section === 'info') {
      continueFromInfo()
      return
    }
    // Enter na secao endereco -> confirma (motoboy abre data; expedicao finaliza).
    if (section === 'address') {
      void confirmAddress()
      return
    }
    // Enter na secao data (motoboy) -> finaliza.
    void doSubmit()
  }

  // -- Render: estados ------------------------------------------------------
  if (loading) {
    return (
      <div className="fk-center">
        <div className="fk-spinner" />
        <p className="fk-sub">Carregando sua oferta…</p>
      </div>
    )
  }

  if (loadError || !offer) {
    return (
      <div className="fk-center">
        <div className="fk-emoji">⚠️</div>
        <h1 className="fk-h1">Link indisponível</h1>
        <p className="fk-sub">{loadError || 'Oferta não encontrada.'}</p>
      </div>
    )
  }

  const installmentControl = parcelas.length > 0 ? (
    <div className="fk-installments fk-installments-final">
      <label className="fk-coupon-label" htmlFor="fk-parcela">
        Simular parcelamento (taxa administrativa de {ADMIN_FEE_PCT.toLocaleString('pt-BR', { minimumFractionDigits: 2 })}% incluída)
      </label>
      <select
        id="fk-parcela"
        className="fk-input"
        value={installments}
        onChange={(e) => setInstallments(Number(e.target.value))}
      >
        {parcelas.map((p) => (
          <option key={p.n} value={p.n}>
            {p.label}
          </option>
        ))}
      </select>
    </div>
  ) : null

  // -- Render: checkout (2 colunas, estilo Logzz / marca azul FALKZ) ---------
  return (
    <form onSubmit={onSubmit} noValidate className="fk-checkout">
      {isMotoboy && (
        <div className="fk-cod-strip" aria-label="Benefícios do pagamento na entrega">
          <div className="fk-cod-strip-track">
            {[0, 1].map(copy => (
              <span className="fk-cod-strip-copy" key={copy} aria-hidden={copy === 1}>
                <span><i aria-hidden="true">🛵</i> Pagamento na entrega</span><b>•</b>
                <span><i aria-hidden="true">💳</i> Aceitamos cartão, Pix e dinheiro</span><b>•</b>
                <span><i aria-hidden="true">⏱️</i> Entrega no mesmo dia</span><b>•</b>
              </span>
            ))}
          </div>
        </div>
      )}
      {/* ===================== COLUNA ESQUERDA — FORM ===================== */}
      <div className="fk-col-form">
        {submitError && (
          <div className="fk-alert" role="alert">
            <span className="fk-alert-icon" aria-hidden="true">
              ⚠️
            </span>
            <span>{submitError}</span>
          </div>
        )}

        {/* Aviso de expedicao (sem agendamento) */}
        {!hasScheduleStep && (
          <div className="fk-info">
            📦 Checkout Único — seu pedido a um clique.
          </div>
        )}

        {/* LGPD (M3 — Art. 9 §1º / Art. 6 VI): aviso OSTENSIVO de tratamento no
            proprio ponto de coleta, acima dos campos de dados pessoais. Sobrio e
            expansivel (nao intrusivo) — controlador, finalidade, com quem os
            dados sao compartilhados e link para a Politica completa. */}
        <PrivacyNotice />

        <div className="fk-accordion">
          {/* ---------- SECAO 1 — SUAS INFORMACOES ---------- */}
          <AccordionSection
            index={1}
            title="Suas informações"
            open={section === 'info'}
            done={infoOk && section !== 'info'}
            onEdit={() => setSection('info')}
          >
            <Field
              label="Nome do cliente"
              value={form.billing_name}
              onChange={(v) => setField('billing_name', v)}
              error={errors.billing_name}
              autoComplete="name"
              placeholder="Digite o nome do cliente completo"
            />

            {/* FEAT-FRETE (2026-06-18): CPF VOLTA somente para correio (os Correios
                exigem CPF do destinatario). Motoboy NAO exibe este campo. */}
            {!isMotoboy && (
              <Field
                label="CPF do cliente"
                value={form.billing_cpf}
                onChange={(v) => setField('billing_cpf', maskCPF(v))}
                error={errors.billing_cpf}
                inputMode="numeric"
                placeholder="000.000.000-00"
                autoComplete="off"
                disabled={!enabled.cpf}
                infoIcon
                infoTitle="Exigido pelos Correios/transportadora para envio."
              />
            )}

            <Field
              label="Telefone"
              value={form.billing_phone}
              onChange={(v) => setField('billing_phone', maskPhone(v))}
              error={errors.billing_phone}
              inputMode="tel"
              placeholder="(00) 00000-0000"
              autoComplete="tel"
              disabled={!enabled.phone}
              flag="🇧🇷 +55"
            />

            <button
              type="button"
              className="fk-btn"
              onClick={continueFromInfo}
              disabled={!infoOk}
            >
              Continuar
            </button>
          </AccordionSection>

          {/* ---------- SECAO 2 — ENDERECO E ENTREGA ---------- */}
          <AccordionSection
            index={2}
            title="Endereço e entrega"
            open={section === 'address'}
            done={step1Valid && section === 'schedule'}
            locked={!infoOk}
            onEdit={() => infoOk && setSection('address')}
          >
            <Field
              label="CEP"
              value={form.billing_postcode}
              onChange={onCepChange}
              onBlur={onCepBlur}
              error={errors.billing_postcode}
              inputMode="numeric"
              placeholder="00000-000"
              autoComplete="postal-code"
              disabled={!enabled.cep}
              hint={
                cepLoading || scheduleLoading
                  ? 'Verificando endereço e entrega…'
                  : 'Preenche o endereço automaticamente'
              }
            />

            {/* Aviso quando o CEP do motoboy esta fora de área. */}
            {isMotoboy && zoneStatus === 'out' && (
              <div className="fk-alert" style={{ marginTop: 4, marginBottom: 8 }}>
                Não entregamos por motoboy nesse CEP/região.
              </div>
            )}
            {isMotoboy && scheduleError && (
              <div className="fk-alert" style={{ marginTop: 4, marginBottom: 8 }}>
                {scheduleError}
              </div>
            )}

            <div className="fk-row">
              <Field
                label="Endereço (rua)"
                value={form.billing_address_1}
                onChange={(v) => setField('billing_address_1', v)}
                error={errors.billing_address_1}
                autoComplete="address-line1"
                disabled={!enabled.street}
              />
              <Field
                className="fk-col-2"
                label="Número"
                value={form.billing_number}
                onChange={(v) => setField('billing_number', v)}
                error={errors.billing_number}
                inputMode="numeric"
                disabled={!enabled.address}
              />
            </div>

            <Field
              label="Complemento (Opcional)"
              value={form.billing_address_2}
              onChange={(v) => setField('billing_address_2', v.slice(0, COMPLEMENTO_MAX))}
              autoComplete="address-line2"
              placeholder="Apto, bloco, ponto de referência"
              disabled={!enabled.address}
              maxLength={COMPLEMENTO_MAX}
              counter
            />

            <div className="fk-row">
              <Field
                label="Bairro"
                value={form.billing_neighborhood}
                onChange={(v) => setField('billing_neighborhood', v)}
                error={errors.billing_neighborhood}
                autoComplete="address-level3"
                disabled={!enabled.neighborhood}
              />
              <Field
                label="Cidade"
                value={form.billing_city}
                onChange={(v) => setField('billing_city', v)}
                error={errors.billing_city}
                autoComplete="address-level2"
                disabled={!enabled.cityState}
              />
              <Field
                className="fk-col-2"
                label="UF"
                value={form.billing_state}
                onChange={(v) => setField('billing_state', v.toUpperCase().slice(0, 2))}
                error={errors.billing_state}
                autoComplete="address-level1"
                placeholder="UF"
                disabled={!enabled.cityState}
              />
            </div>

            {/* LINK MISTO: enquanto o CEP ainda resolve COD ou Expedicao, mostra um
                loading em vez de piscar a secao errada (ou sumir — bug reportado
                2026-07-27 num contexto parecido). */}
            {isMisto && (mistoResolving || (mistoMode === null && cepOk && addressComplete)) && (
              <div className="fk-freight">
                <div className="fk-sched-loading">
                  <div className="fk-spinner" />
                  <p className="fk-sub">Verificando forma de entrega…</p>
                </div>
              </div>
            )}

            {/* ---------- FRETE (correio/expedicao) — FEAT-FRETE ---------- */}
            {/* So aparece para correio, CEP valido e ENCONTRADO, e com o ENDEREÇO
                COMPLETO (rua/número/bairro/cidade/UF) — senão mostra frete calculado
                pra um endereço que ainda nem foi totalmente informado. LINK MISTO só
                entra aqui depois de resolver (mistoMode==='expedicao'). */}
            {!isMotoboy && cepOk && !errors.billing_postcode && addressComplete &&
              !(isMisto && (mistoResolving || mistoMode === null)) && (
              <div className="fk-freight">
                {/* Frete travado pelo produtor (fixo por transportadora e/ou bloqueio de
                    Correios) — pedido dono 2026-07-27: "sem o cliente poder mexer".
                    Sem seletor nenhum: so uma linha informativa, preco ja no total. */}
                {!freightLoading && !freightError && freightOptions.length > 0 && freightOptions[0].locked ? (
                  <>
                    <div className="fk-freight-head">Frete</div>
                    <div className="fk-freight-locked-info">
                      <span>{freightOptions[0].company}{freightOptions[0].service ? ` · ${freightOptions[0].service}` : ''}</span>
                      <span className="fk-freight-price">{formatBRL(freightOptions[0].price)}</span>
                    </div>
                  </>
                ) : (
                  <>
                <div className="fk-freight-head">Opções de envio</div>
                <p className="fk-sub">Escolha como receber. O frete é cobrado junto com o pedido, na entrega.</p>

                {freightLoading && (
                  <div className="fk-sched-loading">
                    <div className="fk-spinner" />
                    <p className="fk-sub">Calculando o frete…</p>
                  </div>
                )}

                {!freightLoading && freightError && (
                  <div className="fk-alert" style={{ marginTop: 4 }}>
                    {freightError}
                    <button
                      type="button"
                      className="fk-link-retry"
                      onClick={() => void resolveFreight(form.billing_postcode)}
                    >
                      Tentar novamente
                    </button>
                  </div>
                )}

                {!freightLoading && !freightError && freightOptions.length === 0 && (
                  <p className="fk-sub">
                    Sem frete para este CEP agora. Confira o CEP ou tente de novo em instantes.
                  </p>
                )}

                {!freightLoading && !freightError && freightOptions.length > 0 && (
                  <div className="fk-freight-list">
                    {displayFreightOptions.map((o) => {
                      const checked = freightId === o.id
                      return (
                        <button
                          type="button"
                          key={o.id}
                          className={`fk-freight-card${checked ? ' is-selected' : ''}${o.preferred ? ' is-preferred' : ''}`}
                          onClick={() => {
                            setFreightId(o.id)
                            if (freightError) setFreightError(null)
                          }}
                          aria-pressed={checked}
                        >
                          <span className="fk-freight-radio" aria-hidden="true" />
                          <span className="fk-freight-info">
                            <span className="fk-freight-name">
                              {o.company}
                              {o.service ? ` · ${o.service}` : ''}
                              {o.preferred && (
                                <span className="fk-freight-badge fk-freight-badge-preferred">Indicado</span>
                              )}
                              {o.estimated && (
                                <span className="fk-freight-badge">estimado</span>
                              )}
                            </span>
                            <span className="fk-freight-meta">
                              {o.delivery_days > 0
                                ? `Prazo ${o.delivery_days} dia${o.delivery_days > 1 ? 's' : ''}`
                                : 'Prazo a confirmar'}
                            </span>
                          </span>
                          <span className="fk-freight-price">{formatBRL(o.price)}</span>
                        </button>
                      )
                    })}
                  </div>
                )}
                  </>
                )}
              </div>
            )}

            {/* LGPD — aviso de consentimento (expedicao finaliza nesta secao). */}
            {!hasScheduleStep && <ConsentNotice />}

            <button
              type="button"
              className={`fk-btn${hasScheduleStep ? '' : ' fk-btn-cta'}`}
              onClick={() => void confirmAddress()}
              disabled={
                submitting ||
                !step1Valid ||
                (!hasScheduleStep && isSoldOut)
              }
            >
              {submitting && !hasScheduleStep && (
                <span className="fk-btn-spinner" aria-hidden="true" />
              )}
              {hasScheduleStep
                ? 'Confirmar endereço'
                : isSoldOut
                  ? 'Produto esgotado'
                  : submitting
                    ? 'Enviando pedido…'
                    : `Finalizar compra · ${totalLabel} na entrega`}
            </button>
          </AccordionSection>

          {/* ---------- SECAO 3 — ESCOLHA O DIA (motoboy) ---------- */}
          {hasScheduleStep && (
            <AccordionSection
              index={3}
              title="Escolha o dia para receber o entregador"
              open={section === 'schedule'}
              done={false}
              locked={!step1Valid}
              onEdit={() => step1Valid && setSection('schedule')}
            >
              {scheduleLoading && (
                <div className="fk-sched-loading">
                  <div className="fk-spinner" />
                  <p className="fk-sub">Buscando datas disponíveis…</p>
                </div>
              )}

              {!scheduleLoading && scheduleError && (
                <div className="fk-alert">{scheduleError}</div>
              )}

              {!scheduleLoading && !scheduleError && dates.length === 0 && (
                <p className="fk-sub">
                  Sem datas para este endereço agora. Confira o CEP ou fale com o vendedor.
                </p>
              )}

              {!scheduleLoading && !scheduleError && dates.length > 0 && (() => {
                const agendamentoDates = dates.filter((d) => d.tipo !== 'pre_agendado')
                const preDates = dates.filter((d) => d.tipo === 'pre_agendado')
                // Aba ativa cai pra a que tiver datas, se a escolhida estiver vazia.
                const activeTab =
                  dateTab === 'agendamento' && agendamentoDates.length === 0 && preDates.length > 0
                    ? 'pre_agendado'
                    : dateTab === 'pre_agendado' && preDates.length === 0 && agendamentoDates.length > 0
                      ? 'agendamento'
                      : dateTab
                const shown = activeTab === 'agendamento' ? agendamentoDates : preDates
                return (
                  <>
                    <p className="fk-sched-help">
                      <span className="fk-sched-help-desktop">
                        Datas em <strong className="fk-tipo-ag">Agendamento</strong> têm
                        entrega garantida. Datas em{' '}
                        <strong className="fk-tipo-pre">Pré-agendamento</strong> precisam
                        de confirmação.
                      </span>
                      <span className="fk-sched-help-mobile">Aproveite, últimas unidades em estoque!</span>
                    </p>
                    <div className="fk-date-tabs" role="tablist" aria-label="Tipo de data">
                      <button
                        type="button"
                        role="tab"
                        aria-selected={activeTab === 'agendamento'}
                        className={`fk-date-tab${activeTab === 'agendamento' ? ' is-active' : ''}`}
                        onClick={() => setDateTab('agendamento')}
                        disabled={agendamentoDates.length === 0}
                      >
                        Agendamento
                      </button>
                      <button
                        type="button"
                        role="tab"
                        aria-selected={activeTab === 'pre_agendado'}
                        className={`fk-date-tab${activeTab === 'pre_agendado' ? ' is-active' : ''}`}
                        onClick={() => setDateTab('pre_agendado')}
                        disabled={preDates.length === 0}
                      >
                        Pré-agendamento
                      </button>
                    </div>
                    <div className="fk-day-grid">
                      {shown.map((d) => {
                        const checked = form.sz_delivery_date === d.value
                        const isPre = d.tipo === 'pre_agendado'
                        const day = splitDayLabel(d.label)
                        return (
                          <button
                            type="button"
                            key={d.value}
                            className={`fk-day-card${checked ? ' is-selected' : ''}${
                              isPre ? ' is-pre' : ''
                            }`}
                            onClick={() => onSelectDate(d)}
                            aria-pressed={checked}
                          >
                            <span className="fk-day-radio" aria-hidden="true" />
                            <span className="fk-day-top">{day.top}</span>
                            <span className="fk-day-num">{day.num}</span>
                            <span
                              className={`fk-date-tag ${
                                isPre ? 'fk-date-tag-pre' : 'fk-date-tag-ag'
                              }`}
                            >
                              {isPre ? 'Pré-agendamento' : 'Agendamento'}
                            </span>
                          </button>
                        )
                      })}
                    </div>
                    {errors.sz_delivery_date && (
                      <div className="fk-error-msg">{errors.sz_delivery_date}</div>
                    )}
                  </>
                )
              })()}

              <div className="fk-order-value">
                Valor do pedido: <strong>{priceLabel}</strong>
              </div>

              {installmentControl}

              {/* LGPD — aviso de consentimento (motoboy finaliza nesta secao). */}
              <ConsentNotice />

              <button
                type="submit"
                className="fk-btn fk-btn-cta"
                disabled={
                  submitting ||
                  scheduleLoading ||
                  dates.length === 0 ||
                  isSoldOut
                }
              >
                {submitting && (
                  <span className="fk-btn-spinner" aria-hidden="true" />
                )}
                {isSoldOut
                  ? 'Produto esgotado'
                  : submitting
                    ? 'Enviando pedido…'
                    : `Finalizar compra · ${priceLabel} na entrega`}
              </button>
            </AccordionSection>
          )}
        </div>
      </div>

      {/* ===================== COLUNA DIREITA — RESUMO ===================== */}
      <aside className="fk-col-summary">
        <div className="fk-summary">
          <h3 className="fk-summary-title">Resumo do pedido</h3>

          <div className="fk-summary-product">
            {offer.image_url ? (
              <img
                className="fk-summary-img"
                src={offer.image_url}
                alt={offer.name}
                loading="lazy"
              />
            ) : (
              <div className="fk-summary-img fk-summary-img-ph" aria-hidden="true">
                📦
              </div>
            )}
            <div className="fk-summary-prod-info">
              <div className="fk-summary-prod-name">{offer.name}</div>
              <div className="fk-summary-prod-price">Valor: {priceLabel}</div>
            </div>
          </div>

          {isMotoboy && (
            <div className="fk-cod-pulse" role="status">
              <span className="fk-cod-pulse-dot" aria-hidden="true" />
              <span>Pagamento na entrega</span>
            </div>
          )}

          {/* CHECKOUT-PERSONALIZADO: descricao da oferta (white-label) — so quando
              o backend/link a informam. Da contexto/confianca ao comprador. */}
          {offer.description && (
            <p className="fk-summary-desc">{offer.description}</p>
          )}

          {/* Resumo ao vivo dos dados do lead, preenchidos progressivamente no form ao
              lado — fica ACIMA da lista de itens (pedido do dono) pra conferência final
              sem precisar rolar de volta pro topo. Só mostra linha com valor preenchido. */}
          {(form.billing_name.trim() ||
            form.billing_phone.trim() ||
            form.billing_cpf.trim() ||
            form.billing_postcode.trim()) && (
            <div className="fk-summary-lines" style={{ marginBottom: 12 }}>
              {form.billing_name.trim() && (
                <div className="fk-summary-line">
                  <span>Nome</span>
                  <span>{titleCaseName(form.billing_name)}</span>
                </div>
              )}
              {form.billing_phone.trim() && (
                <div className="fk-summary-line">
                  <span>Telefone</span>
                  <span>{form.billing_phone.trim()}</span>
                </div>
              )}
              {!isMotoboy && form.billing_cpf.trim() && (
                <div className="fk-summary-line">
                  <span>CPF</span>
                  <span>{form.billing_cpf.trim()}</span>
                </div>
              )}
              {form.billing_postcode.trim() && (
                <div className="fk-summary-line">
                  <span>CEP</span>
                  <span>{form.billing_postcode.trim()}</span>
                </div>
              )}
              {(form.billing_address_1.trim() || form.billing_number.trim()) && (
                <div className="fk-summary-line">
                  <span>Endereço</span>
                  <span>
                    {[form.billing_address_1.trim(), form.billing_number.trim()]
                      .filter(Boolean)
                      .join(', ')}
                  </span>
                </div>
              )}
              {form.billing_address_2.trim() && (
                <div className="fk-summary-line">
                  <span>Complemento</span>
                  <span>{form.billing_address_2.trim()}</span>
                </div>
              )}
              {(form.billing_neighborhood.trim() || form.billing_city.trim() || form.billing_state.trim()) && (
                <div className="fk-summary-line">
                  <span>Bairro/Cidade</span>
                  <span>
                    {[form.billing_neighborhood.trim(), form.billing_city.trim(), form.billing_state.trim()]
                      .filter(Boolean)
                      .join(' — ')}
                  </span>
                </div>
              )}
            </div>
          )}

          {/* Aviso de disponibilidade (qty_sellable do back). Marca azul FALKZ.
              Esgotado tem prioridade sobre "ultimas unidades". null => nada. */}
          {isSoldOut ? (
            <div className="fk-stock fk-stock-out" role="status">
              <span className="fk-stock-dot" aria-hidden="true" />
              <span>
                <strong>Produto esgotado</strong>
                <small>Este produto não está disponível para compra no momento.</small>
              </span>
            </div>
          ) : isLowStock ? (
            <div className="fk-stock fk-stock-low" role="status">
              <span className="fk-stock-dot" aria-hidden="true" />
              <span>Últimas unidades</span>
            </div>
          ) : null}

          {/* Cupom (placeholder local) */}
          <div className="fk-coupon">
            <label className="fk-coupon-label">Adicionar cupom</label>
            <div className="fk-coupon-row">
              <input
                className="fk-input"
                value={coupon}
                onChange={(e) => {
                  setCoupon(e.target.value)
                  if (couponMsg) setCouponMsg(null)
                }}
                placeholder="Código do cupom"
                autoComplete="off"
              />
              <button type="button" className="fk-btn-coupon" onClick={applyCoupon}>
                Aplicar
              </button>
            </div>
            {couponMsg && <div className="fk-error-msg">{couponMsg}</div>}
          </div>

          <div className="fk-summary-lines">
            <div className="fk-summary-line">
              <span>Subtotal</span>
              <span>{priceLabel}</span>
            </div>
            {/* COD (motoboy) não tem conceito de frete — some a linha inteira,
                não só mostra R$0 (pedido dono 2026-07-28). */}
            {!isMotoboy && (
              <div className="fk-summary-line">
                <span>Frete</span>
                <span>{selectedFreight ? formatBRL(freightPrice) : '—'}</span>
              </div>
            )}
          </div>

          <div className="fk-summary-total">
            <span>Valor Final</span>
            {/* FEAT-FRETE: total = oferta + frete escolhido (correio); motoboy = oferta. */}
            <span className="fk-summary-total-v">{totalLabel}</span>
          </div>
        </div>

      </aside>

      {/* Popup de aviso de pre-agendamento */}
      {preWarning && (
        <PreScheduleModal option={preWarning} onClose={() => setPreWarning(null)} />
      )}

      {/* Popup: fora de área de motoboy */}
      {outOfAreaPopup && <OutOfAreaModal onClose={() => setOutOfAreaPopup(false)} />}
    </form>
  )
}

// ---------------------------------------------------------------------------
// "Sexta-feira 20/06" -> { top: "Sexta-feira", num: "20/06" } (best-effort).
// ---------------------------------------------------------------------------
function splitDayLabel(label: string): { top: string; num: string } {
  const m = label.match(/^(.*?)[\s,]+(\d{1,2}[/.]\d{1,2}.*)$/)
  if (m) return { top: m[1].trim(), num: m[2].trim() }
  // Sem data ("Amanhã"/"Hoje") -> destaca o proprio rotulo no num.
  return { top: '', num: label }
}

// ---------------------------------------------------------------------------
// Secao do acordeao (cabecalho com dot/check + "Editar" quando completa).
// ---------------------------------------------------------------------------
function AccordionSection({
  index,
  title,
  open,
  done,
  locked,
  onEdit,
  children,
}: {
  index: number
  title: string
  open: boolean
  done: boolean
  locked?: boolean
  onEdit: () => void
  children: React.ReactNode
}) {
  return (
    <section
      className={`fk-acc${open ? ' is-open' : ''}${done ? ' is-done' : ''}${
        locked ? ' is-locked' : ''
      }`}
    >
      <header className="fk-acc-head">
        <span className="fk-acc-dot" aria-hidden="true">
          {done ? '✓' : index}
        </span>
        <h3 className="fk-acc-title">{title}</h3>
        {done && !open && (
          <button type="button" className="fk-acc-edit" onClick={onEdit}>
            Editar
          </button>
        )}
      </header>
      {open && <div className="fk-acc-body">{children}</div>}
    </section>
  )
}

// ---------------------------------------------------------------------------
// Popup de aviso: pre-agendamento exige confirmacao
// ---------------------------------------------------------------------------
function PreScheduleModal({
  option,
  onClose,
}: {
  option: ScheduleOption
  onClose: () => void
}) {
  return (
    <div
      className="fk-modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="fk-pre-title"
      onClick={onClose}
    >
      <div className="fk-modal" onClick={(e) => e.stopPropagation()}>
        <div className="fk-modal-icon">⏳</div>
        <h3 id="fk-pre-title" className="fk-modal-title">
          Pré-agendamento
        </h3>
        <p className="fk-modal-text">
          Você escolheu uma data em <strong>pré-agendamento</strong>. Confirme o pedido até{' '}
          <strong>3 dias de entrega antes de {option.label}</strong>, senão ele será cancelado
          automaticamente.
        </p>
        <button type="button" className="fk-btn" onClick={onClose}>
          Entendi
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Popup: CEP fora de área de entrega por motoboy
// ---------------------------------------------------------------------------
function OutOfAreaModal({ onClose }: { onClose: () => void }) {
  return (
    <div
      className="fk-modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="fk-ooa-title"
      onClick={onClose}
    >
      <div className="fk-modal" onClick={(e) => e.stopPropagation()}>
        <div className="fk-modal-icon">🛵</div>
        <h3 id="fk-ooa-title" className="fk-modal-title">
          Fora da área de entrega
        </h3>
        <p className="fk-modal-text">
          Não entregamos por motoboy nesse CEP/região. Confira o CEP informado ou
          fale com o vendedor para outras opções de entrega.
        </p>
        <button type="button" className="fk-btn" onClick={onClose}>
          Entendi
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// LGPD (M3) — aviso de tratamento no ponto de coleta
//
// Resumo OSTENSIVO e curto, exibido acima dos campos de dados pessoais. Cumpre
// o dever de informacao no momento da coleta (Art. 9 §1º / Art. 6 VI): quem e o
// controlador, para que os dados sao usados, com quem sao compartilhados e link
// para a Politica de Privacidade completa. Bloco discreto e expansivel (<details>)
// para nao competir com o formulario. NAO substitui o consentimento (checkbox).
// ---------------------------------------------------------------------------
function PrivacyNotice() {
  return (
    <details className="fk-privacy-notice">
      <summary className="fk-privacy-summary">
        🔒 Como usamos os seus dados (LGPD)
      </summary>
      <div className="fk-privacy-body">
        <p>
          O controlador dos seus dados é a <strong>Falk Log</strong>. Coletamos
          os dados acima apenas para <strong>faturar, entregar e permitir o
          rastreio do seu pedido</strong>.
        </p>
        <p>
          Para concluir a entrega, compartilhamos o necessário com os{' '}
          <strong>Correios/transportadora</strong>, com o{' '}
          <strong>motoboy</strong> (quando a entrega é por motoboy) e com o{' '}
          <strong>produtor da oferta</strong>.
        </p>
        <p>
          Detalhes, suas bases legais e os seus direitos estão na{' '}
          <a
            href="https://falklog.com.br/privacidade.html"
            target="_blank"
            rel="noopener noreferrer"
            className="fk-consent-link"
          >
            Política de Privacidade
          </a>
          .
        </p>
      </div>
    </details>
  )
}

// ---------------------------------------------------------------------------
// LGPD — aviso de consentimento (SEM checkbox)
//
// Pedido dono 2026-07-28: tirar o clique extra do checkbox. O consentimento
// passa a ser por AÇÃO — clicar "Finalizar compra" já é o ato de concordar,
// desde que o aviso esteja visível e claro ANTES do clique (mesmo padrão que
// várias plataformas de e-commerce usam). Continua sendo o PRÓPRIO cliente
// consentindo (nunca produtor/admin marcando por ele — isso anularia o
// consentimento, LGPD Art. 8º exige que seja do titular). postConsent() em
// doSubmit registra o aceite (doc_version) no ato do clique, igual antes.
// Renderizado logo acima do botao de finalizar em cada fluxo (expedicao/motoboy).
// Link da Politica de Privacidade abre em nova aba.
// ---------------------------------------------------------------------------
function ConsentNotice() {
  return (
    <p className="fk-consent-notice">
      Ao finalizar a compra, você concorda com nossa{' '}
      <a
        href="https://falklog.com.br/privacidade.html"
        target="_blank"
        rel="noopener noreferrer"
        className="fk-consent-link"
      >
        Política de Privacidade
      </a>{' '}
      e o tratamento dos seus dados (LGPD).
    </p>
  )
}

// ---------------------------------------------------------------------------
// Campo de input reutilizavel
// ---------------------------------------------------------------------------
function Field({
  label,
  value,
  onChange,
  onBlur,
  error,
  hint,
  className,
  disabled,
  infoIcon,
  infoTitle,
  flag,
  maxLength,
  counter,
  ...rest
}: {
  label: string
  value: string
  onChange: (v: string) => void
  onBlur?: () => void
  error?: string
  hint?: string
  className?: string
  disabled?: boolean
  infoIcon?: boolean
  infoTitle?: string
  flag?: string
  maxLength?: number
  /** Mostra "{len}/{maxLength}" à direita do label (precisa de maxLength). */
  counter?: boolean
} & Pick<
  React.InputHTMLAttributes<HTMLInputElement>,
  'inputMode' | 'placeholder' | 'autoComplete' | 'type'
>) {
  return (
    <div className={`fk-field${className ? ' ' + className : ''}${disabled ? ' is-locked' : ''}`}>
      <label className="fk-label fk-label-row">
        <span>
          {label}
          {infoIcon && (
            <span className="fk-label-info" title={infoTitle} aria-label={infoTitle}>
              ℹ️
            </span>
          )}
        </span>
        {counter && maxLength != null && (
          <span className="fk-counter fk-counter-inline">
            {value.length}/{maxLength}
          </span>
        )}
      </label>
      <div className={`fk-input-wrap${flag ? ' has-flag' : ''}`}>
        {flag && <span className="fk-input-flag">{flag}</span>}
        <input
          className={`fk-input${error ? ' fk-invalid' : ''}`}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onBlur={onBlur}
          disabled={disabled}
          maxLength={maxLength}
          {...rest}
        />
      </div>
      {error ? (
        <div className="fk-error-msg">{error}</div>
      ) : hint ? (
        <div className="fk-hint">{hint}</div>
      ) : null}
    </div>
  )
}
