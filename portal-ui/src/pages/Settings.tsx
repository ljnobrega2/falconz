// Configurações da conta — paridade fiel com templates/portal/v2/sections/settings.php.
// 5 sub-abas: Conta / Saques / Notificações / Taxas & Prazos / Suporte.
//
// Endpoints reais usados (go/portal :8085):
//   GET  /portal/me                         → nome, email, role (Conta — read-only)
//   POST /portal/settings/2fa               → liga/desliga 2FA (Conta)
//   GET  /portal/wallet/accounts            → contas PIX de saque (Saques — read-only)
//   GET  /wp-json/sz-notif/v1/prefs?user_id= → prefs de notificação push (Notificações)
//   POST /wp-json/sz-notif/v1/prefs          → salva prefs de notificação push
//
// Endpoints AUSENTES / INCOMPLETOS no go/portal (ver backendReqs no relatório):
//   POST /portal/account/email     (existe — settings.ChangeEmail)
//   POST /portal/account/password  (existe — settings.ChangePassword)
//   POST /portal/account/delete    (existe — settings.DeleteAccount; LGPD, exige
//                                   { password, confirm:"EXCLUIR" })
//   POST /portal/wallet/accounts   (existe — wallet.AddAccount, MAS não persiste
//                                   agencia/conta; e ainda EXIGE holder_cpf válido)
//   /portal/me carece de document (CPF) + phone — exibidos como "—".
//
// #18 (PIX): titular/CPF da conta NÃO são mais editáveis — vêm do CADASTRO do
//   usuário. Como /portal/me NÃO retorna document hoje, o CPF aparece como "—"
//   e o POST envia holder_cpf de me.document (vazio até o backend expor o campo).
//   Agencia/Conta foram adicionados ao body — backend precisa aceitar/persistir.
//
// Suporte: embute o componente Support já existente (port fiel) — sem duplicar código.
import { FormEvent, useEffect, useState } from 'react'
import { api, getToken, clearToken } from '../api'
import { useNavigate } from 'react-router-dom'
import { useToast } from '../hooks/useToast'
import EmptyState from '../components/EmptyState'
import Drawer from '../components/Drawer'
import DocumentChangeDrawer from '../components/DocumentChangeDrawer'
import FalkSelect from '../components/FalkSelect'
import Support from './Support'

// ── Azul da marca FALK (#1E6FF2) — usado em acentos próprios desta tela ────────
const FALK_BLUE = '#1E6FF2'

// ── Bancos do Brasil — ~50 maiores (SELECT da seção Saques) ────────────────────
// Lista curada das instituições mais usadas (bancos + carteiras/fintechs com
// conta PIX). Valor enviado no body = nome do banco. Ordenada aproximadamente por
// relevância/uso no varejo brasileiro. Mantida hardcoded (não muda em runtime).
const BANCOS_BR: string[] = [
  'Nubank',
  'Itaú Unibanco',
  'Bradesco',
  'Banco do Brasil',
  'Santander',
  'Caixa Econômica Federal',
  'Inter',
  'C6 Bank',
  'BTG Pactual',
  'Sicoob',
  'Sicredi',
  'Banco Original',
  'PagBank (PagSeguro)',
  'Mercado Pago',
  'Banrisul',
  'Safra',
  'BV (Votorantim)',
  'Banco Pan',
  'Banco BMG',
  'Banco Daycoval',
  'Banco Neon',
  'Banco Next',
  'Banco Modal',
  'Banco Sofisa',
  'Banco Topázio',
  'Banco ABC Brasil',
  'Banestes',
  'Banco do Nordeste',
  'Banco da Amazônia',
  'Citibank',
  'Banco Bari (Agibank)',
  'Banco Agibank',
  'Banco C6 Consignado',
  'Banco Cooperativo do Brasil (Bancoob)',
  'Banco Rendimento',
  'Banco Mercantil do Brasil',
  'Banco Genial',
  'Banco Master',
  'Banco Will (Paraná Banco)',
  'Cora',
  'Stone (TON)',
  'Asaas',
  'InfinitePay',
  'PicPay',
  'Ame Digital',
  'Iti (Itaú)',
  'Banco Digio',
  'Banco Crefisa',
  'Banco Bmg',
  'Banco Tribanco',
  'Banco Cresol',
  'Banco Unicred',
  'Outro',
]

// ── Tipos ─────────────────────────────────────────────────────────────────────

type MeResp = {
  ok: boolean
  id: number
  wp_user_id: number
  email: string
  nome: string
  role: string
  plano: string
  twofa_enabled: boolean
  // shipping_class_id presente ⇒ o usuário usa EXPEDIÇÃO (classe de frete definida).
  // null/ausente ⇒ COD-only (cliente/afiliado sem expedição). Usado p/ esconder o
  // grupo de notificações de Expedição (#75a) — mesmo sinal de hasExp da Carteira.
  shipping_class_id?: number | string | null
  // /portal/me NÃO retorna document/phone hoje — ver backendReq.
  document?: string
  phone?: string
}

type WalletAccount = {
  id: number
  holder_name: string
  holder_cpf: string
  pix_type: string
  pix_key: string
  is_default: boolean
}
type AccountsResp = { ok: boolean; accounts: WalletAccount[]; scope: string }

// prefs de notificação chegam como objeto livre {evento: bool} (sz_notif_prefs.prefs JSONB).
type NotifPrefs = Record<string, boolean>
type NotifPrefsResp = { ok: boolean; user_id: number; prefs: NotifPrefs }

type TabKey = 'conta' | 'saques' | 'notif' | 'taxas' | 'marca' | 'convite' | 'suporte'

// CHECKOUT-BRANDING (white-label v1): marca do checkout do PRODUTOR.
// GET/POST /portal/settings/brand → { logo_url, primary_color } em
// senderzz_portal_user_meta. O go/orders GetOffer lê essas chaves e o checkout-ui
// renderiza logo + cor (fallback FALK quando vazias).
type BrandResp = { ok: boolean; name?: string; logo_url?: string; primary_color?: string; banner_url?: string; whatsapp?: string; text_color?: string }

// #71 — link de indicação FIXO/PERMANENTE do usuário (GET /portal/affiliates/referral).
// Movido da tela Afiliação para a aba "Convite de associado" do Perfil (pedido do dono).
type ReferralResp = {
  ok: boolean
  referral_code?: string
  referral_link?: string
}

// #23: o aceite/revogação versionado da Política de Privacidade saiu desta tela
// (é aceito no 1º acesso, não fica disponível na aba Conta). As constantes
// PRIVACY_DOC_* e o estado de consentimento foram removidos junto com o bloco.

// ── Eventos de notificação (#29) ──────────────────────────────────────────────
// As notificações são SEGMENTADAS por tipo em dois grupos distintos:
//   • Expedição           → eventos de pedido/frete (etiqueta, despacho, entrega).
//   • Motoboy / Cash on Delivery → eventos da entrega local pelo motoboy.
//
// IMPORTANTE: as `key` são a chave de persistência (gravadas em sz_notif_prefs como
// blob JSONB {evento:bool} via go/portal). Mantê-las BYTE-IDÊNTICAS — renomear órfã
// as prefs já salvas e quebra o match do disparador. Só `title`/`desc` são livres.
type NotifEvent = { key: string; title: string; desc: string }
type NotifGroup = { id: string; title: string; desc: string; events: NotifEvent[] }

const NOTIF_GROUPS: NotifGroup[] = [
  {
    id: 'expedicao',
    title: 'Expedição',
    desc: 'Eventos de pedido e frete — do novo pedido à entrega confirmada.',
    events: [
      { key: 'pedido_novo', title: 'Novo pedido', desc: 'Novo pedido recebido na plataforma.' },
      { key: 'enviado', title: 'Pedido enviado', desc: 'Etiqueta gerada e pedido despachado.' },
      { key: 'entregue', title: 'Entregue', desc: 'Entrega confirmada com sucesso.' },
    ],
  },
  {
    id: 'motoboy',
    title: 'Motoboy / Cash on Delivery',
    desc: 'Eventos da entrega local feita pelo motoboy (COD).',
    events: [
      { key: 'agendado', title: 'Agendamento', desc: 'Pedido agendado com o motoboy.' },
      { key: 'embalado', title: 'Pedido embalado', desc: 'Embalado e aguardando coleta.' },
      { key: 'acaminho', title: 'Em rota', desc: 'Motoboy saiu para entrega.' },
      // #75: PEDIDO ENTREGUE faltava na seção Cash on Delivery — adicionado. Usa a
      // MESMA chave de persistência 'entregue' do evento de entrega (settings.php
      // expõe um único 'entregue'; o disparador lê WHERE event='entregue'). Para o
      // usuário COD-only o grupo Expedição fica oculto (#75a), então este é o único
      // toggle de "entregue" visível — sem checkbox duplicado no caso comum.
      { key: 'entregue', title: 'Pedido entregue', desc: 'Entrega confirmada pelo motoboy.' },
      { key: 'frustrado', title: 'Frustrado', desc: 'Tentativa de entrega não concluída.' },
    ],
  },
]

// ── Helpers de formatação BR (espelham a lógica PHP de settings.php) ──────────

// Formata CPF/CNPJ a partir dos dígitos crus.
// validCpf — valida CPF brasileiro (11 dígitos + DV), espelho 1:1 do validCPF do
// backend (go/portal wallet.go). Usado p/ dar um aviso inline amigável ANTES do POST
// quando o titular informa o CPF manualmente (cadastro do afiliado sem CPF).
function validCpf(raw: string): boolean {
  const cpf = (raw || '').replace(/\D/g, '')
  if (cpf.length !== 11) return false
  if (/^(\d)\1{10}$/.test(cpf)) return false // 11 dígitos iguais
  for (let t = 9; t < 11; t++) {
    let sum = 0
    for (let i = 0; i < t; i++) sum += parseInt(cpf[i], 10) * (t + 1 - i)
    let d = (sum * 10) % 11
    if (d === 10) d = 0
    if (d !== parseInt(cpf[t], 10)) return false
  }
  return true
}

// validCnpj — mesma família de validCpf (mod-11, pesos diferentes), espelho do
// validCNPJ do backend (go/portal wallet.go / document_change.go).
function validCnpj(raw: string): boolean {
  const cnpj = (raw || '').replace(/\D/g, '')
  if (cnpj.length !== 14) return false
  if (/^(\d)\1{13}$/.test(cnpj)) return false
  const calc = (weights: number[], upto: number) => {
    let sum = 0
    for (let i = 0; i < upto; i++) sum += parseInt(cnpj[i], 10) * weights[i]
    const r = sum % 11
    return r < 2 ? 0 : 11 - r
  }
  const w1 = [5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2]
  const w2 = [6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2]
  if (calc(w1, 12) !== parseInt(cnpj[12], 10)) return false
  if (calc(w2, 13) !== parseInt(cnpj[13], 10)) return false
  return true
}

// validDoc — CPF (11 dígitos) ou CNPJ (14), conforme o comprimento informado.
function validDoc(raw: string): boolean {
  const digits = (raw || '').replace(/\D/g, '')
  return digits.length === 14 ? validCnpj(digits) : validCpf(digits)
}

function fmtDoc(raw: string | undefined): { label: string; value: string } {
  const digits = (raw || '').replace(/\D/g, '')
  if (digits.length === 14) {
    return {
      label: 'CNPJ',
      value: digits.replace(/(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})/, '$1.$2.$3/$4-$5'),
    }
  }
  if (digits.length === 11) {
    return { label: 'CPF', value: digits.replace(/(\d{3})(\d{3})(\d{3})(\d{2})/, '$1.$2.$3-$4') }
  }
  return { label: 'CPF / CNPJ', value: raw || '—' }
}

// Formata telefone BR, removendo +55 (espelha $sz9st_fmt_phone).
function fmtPhone(raw: string | undefined): string {
  let d = (raw || '').replace(/\D/g, '')
  if (d.length > 11 && d.startsWith('55')) d = d.slice(2)
  if (d.length === 11) return `(${d.slice(0, 2)}) ${d.slice(2, 7)}-${d.slice(7)}`
  if (d.length === 10) return `(${d.slice(0, 2)}) ${d.slice(2, 6)}-${d.slice(6)}`
  return raw || '—'
}

// Rótulo de perfil (espelha $sz9st_role_labels).
const ROLE_LABELS: Record<string, string> = {
  affiliate: 'Afiliado',
  afiliado: 'Afiliado',
  operator: 'Operador Logístico',
  operador: 'Operador Logístico',
  operador_logistico: 'Operador Logístico',
  logistics_operator: 'Operador Logístico',
  admin: 'Admin',
  client: 'Produtor',
  producer: 'Produtor',
  produtor: 'Produtor',
}
function roleLabel(role: string): string {
  const r = (role || 'client').toLowerCase().trim()
  return ROLE_LABELS[r] || (r ? r.charAt(0).toUpperCase() + r.slice(1) : 'Produtor')
}

// Mascara chave PIX para exibição (espelha sz_portal_v2_mask_pix — meio oculto).
function maskPix(key: string): string {
  if (!key) return '••••'
  if (key.length <= 4) return '••••'
  return key.slice(0, 2) + '••••' + key.slice(-2)
}

// ── Base do namespace público sz-notif (api.ts é fixo em /senderzz/v1) ─────────
// Não editamos api.ts; o namespace de push é público e recebe user_id no body/query.
// Derivamos do MESMO VITE_API_BASE que api.ts usa, trocando o sufixo de namespace.
// Assim funciona tanto com o proxy relativo do Vite (dev) quanto com base absoluta
// (build/produção) — sem hardcodar uma origem que divergiria de api.ts.
const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) || '/wp-json/senderzz/v1'
const NOTIF_BASE = API_BASE.replace(/\/senderzz\/v1$/, '/sz-notif/v1')

async function notifFetch<T = any>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const tok = getToken()
  if (tok) headers.set('Authorization', `Bearer ${tok}`)
  const res = await fetch(`${NOTIF_BASE}${path}`, { ...init, headers })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body?.erro || `HTTP ${res.status}`)
  return body as T
}

export default function Settings() {
  const toast = useToast()
  const navigate = useNavigate()
  const [tab, setTab] = useState<TabKey>('conta')

  // ── Conta ────────────────────────────────────────────────────────────────
  const [me, setMe] = useState<MeResp | null>(null)
  const [loadingMe, setLoadingMe] = useState(true)
  const [meErr, setMeErr] = useState('')
  // FEAT-DOC-CHANGE-2026-07-03: drawer de troca CPF⇄CNPJ.
  const [docChangeOpen, setDocChangeOpen] = useState(false)
  const [twofaEnabled, setTwofaEnabled] = useState(false)
  const [autoEmitLabels, setAutoEmitLabels] = useState(false)
  const [savingAutoEmit, setSavingAutoEmit] = useState(false)

  // Alterar e-mail
  // #83: removido o campo "Confirmar novo e-mail" (repetição) — basta "Novo e-mail" + "Senha atual".
  const [newEmail, setNewEmail] = useState('')
  // AUDIT-2026-06-21 #9: troca de e-mail agora exige a senha atual (re-auth server-side).
  const [emailCurPw, setEmailCurPw] = useState('')
  const [savingEmail, setSavingEmail] = useState(false)
  const [emailMsg, setEmailMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null)

  // Alterar senha
  const [pwCur, setPwCur] = useState('')
  const [pwNew, setPwNew] = useState('')
  const [savingPw, setSavingPw] = useState(false)
  const [pwMsg, setPwMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null)

  // ── Saques (contas PIX) ──────────────────────────────────────────────────
  const [accounts, setAccounts] = useState<WalletAccount[]>([])
  const [loadingAcc, setLoadingAcc] = useState(false)
  const [accLoaded, setAccLoaded] = useState(false)
  const [accScope, setAccScope] = useState('')
  // #18: titular/CPF NÃO são editáveis — a conta é sempre do titular do cadastro.
  // pixBanco vira SELECT; agencia/conta são novos. pixTipo='cpf' puxa o CPF do
  // cadastro (read-only) como conteúdo da chave.
  const [pixBanco, setPixBanco] = useState('')
  const [pixAgencia, setPixAgencia] = useState('')
  const [pixConta, setPixConta] = useState('')
  const [pixTipo, setPixTipo] = useState('cpf')
  const [pixChave, setPixChave] = useState('')
  // CPF do titular informado MANUALMENTE no form. Só é usado quando o cadastro NÃO
  // tem CPF (me.document vazio) — caso comum p/ afiliado. Quando o cadastro tem CPF,
  // o titular é read-only e este estado é ignorado (a fonte é me.document).
  const [pixCpf, setPixCpf] = useState('')
  const [savingPix, setSavingPix] = useState(false)
  const [pixMsg, setPixMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null)

  // ── Excluir conta (LGPD — #43) ────────────────────────────────────────────
  const [delOpen, setDelOpen] = useState(false)
  const [delPassword, setDelPassword] = useState('')
  const [delConfirm, setDelConfirm] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [delErr, setDelErr] = useState('')

  // ── Notificações ─────────────────────────────────────────────────────────
  const [notifPrefs, setNotifPrefs] = useState<NotifPrefs>({})
  const [loadingNotif, setLoadingNotif] = useState(false)
  const [notifLoaded, setNotifLoaded] = useState(false)

  // ── LGPD: exportar dados (direito de acesso/portabilidade — mantido) ───────
  // #23: aceite/revogação de consentimento saiu desta tela (1º acesso).
  const [exporting, setExporting] = useState(false)

  // ── #71: Convite de associado (link de indicação FIXO/PERMANENTE) ──────────
  // Movido da tela Afiliação para a aba "Convite de associado" do Perfil (pedido
  // do dono). Fonte: GET /portal/affiliates/referral → {referral_code, referral_link}.
  const [referral, setReferral] = useState<ReferralResp | null>(null)
  const [loadingRef, setLoadingRef] = useState(false)
  const [refLoaded, setRefLoaded] = useState(false)
  const [refErr, setRefErr] = useState('')

  // ── CHECKOUT-BRANDING (white-label v1) — marca do checkout (produtor) ───────
  // logo_url + primary_color persistidos em senderzz_portal_user_meta via
  // /portal/settings/brand. Vazio = padrão FALK (logo falcão + azul #1E6FF2).
  const [brandColor, setBrandColor] = useState('')
  const [brandName, setBrandName] = useState('')
  const [brandBanner, setBrandBanner] = useState('')
  const [brandWhatsApp, setBrandWhatsApp] = useState('')
  const [brandTextColor, setBrandTextColor] = useState('')
  const [loadingBrand, setLoadingBrand] = useState(false)
  const [brandLoaded, setBrandLoaded] = useState(false)
  const [savingBrand, setSavingBrand] = useState(false)

  // Carrega /portal/me e /portal/settings uma vez (Conta + base p/ user_id das prefs de notif).
  useEffect(() => {
    api<MeResp>('/portal/me')
      .then(r => {
        setMe(r)
        setTwofaEnabled(!!r.twofa_enabled)
      })
      .catch(e => setMeErr(e.message || 'Erro ao carregar dados da conta.'))
      .finally(() => setLoadingMe(false))
    api<{ auto_emit_labels?: boolean }>('/portal/settings')
      .then(r => setAutoEmitLabels(!!r.auto_emit_labels))
      .catch(() => {})
  }, [])

  // Carrega contas PIX ao abrir a aba Saques (lazy — só quando visível).
  useEffect(() => {
    if (tab !== 'saques' || accLoaded) return
    setLoadingAcc(true)
    api<AccountsResp>('/portal/wallet/accounts')
      .then(r => {
        setAccounts(r.accounts || [])
        setAccScope(r.scope || '')
      })
      .catch(e => toast('err', e.message || 'Erro ao carregar contas de saque.'))
      .finally(() => {
        setLoadingAcc(false)
        setAccLoaded(true)
      })
  }, [tab, accLoaded, toast])

  // Carrega prefs de notificação ao abrir a aba Notificações (lazy).
  useEffect(() => {
    if (tab !== 'notif' || notifLoaded || !me) return
    setLoadingNotif(true)
    notifFetch<NotifPrefsResp>(`/prefs?user_id=${me.wp_user_id}`)
      .then(r => setNotifPrefs(r.prefs || {}))
      .catch(e => toast('err', e.message || 'Erro ao carregar notificações.'))
      .finally(() => {
        setLoadingNotif(false)
        setNotifLoaded(true)
      })
  }, [tab, notifLoaded, me, toast])

  // #71: carrega o link de indicação ao abrir a aba "Convite de associado" (lazy).
  // O backend gera o referral_code on-demand (ensureReferralCode). Se a migração 433
  // não rodou, devolve 503 ("recurso de indicação indisponível") — tratado como
  // mensagem inline amigável, não erro duro (degrada como o backend).
  useEffect(() => {
    if (tab !== 'convite' || refLoaded) return
    setLoadingRef(true)
    setRefErr('')
    api<ReferralResp>('/portal/affiliates/referral')
      .then(r => setReferral(r))
      .catch(e => setRefErr(e.message || 'Erro ao carregar o link de convite.'))
      .finally(() => {
        setLoadingRef(false)
        setRefLoaded(true)
      })
  }, [tab, refLoaded])

  // #71: copia o link de indicação (mesmo padrão do antigo copyInvite da Afiliação).
  function copyReferral(url: string) {
    navigator.clipboard?.writeText(url).then(
      () => toast('ok', 'Link copiado.'),
      () => toast('err', 'Não foi possível copiar o link.'),
    )
  }

  // CHECKOUT-BRANDING: carrega a marca atual ao abrir a aba "Marca do checkout" (lazy).
  useEffect(() => {
    if (tab !== 'marca' || brandLoaded) return
    setLoadingBrand(true)
    api<BrandResp>('/portal/settings/brand')
      .then(r => {
        setBrandColor(r.primary_color || '')
        setBrandName(r.name || '')
        setBrandBanner(r.banner_url || '')
        setBrandWhatsApp(r.whatsapp || '')
        setBrandTextColor(r.text_color || '')
      })
      .catch(e => toast('err', e.message || 'Erro ao carregar a marca do checkout.'))
      .finally(() => {
        setLoadingBrand(false)
        setBrandLoaded(true)
      })
  }, [tab, brandLoaded, toast])

  // ── Ação: salvar a marca do checkout (logo + cor primária) ──────────────────
  // POST /portal/settings/brand { logo_url, primary_color }. Envia "" para limpar
  // (volta ao padrão FALK). Validação espelha o backend (https:// + hex) p/ aviso
  // amigável antes do 400. emitToast no sucesso (regra do projeto).
  async function saveBrand(e: FormEvent) {
    e.preventDefault()
    const color = brandColor.trim()
    const name = brandName.trim()
    const banner = brandBanner.trim()
    const whatsapp = brandWhatsApp.trim()
    const textColor = brandTextColor.trim()
    if (color && !/^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(color)) {
      toast('err', 'Cor inválida (use hex, ex.: #2563eb).')
      return
    }
    if (name.length > 80) { toast('err', 'O nome da marca deve ter no máximo 80 caracteres.'); return }
    if (whatsapp && (whatsapp.replace(/\D/g, '').length < 10 || whatsapp.replace(/\D/g, '').length > 13)) {
      toast('err', 'Informe um WhatsApp válido com DDD.'); return
    }
    if (textColor && !/^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(textColor)) {
      toast('err', 'Cor de texto inválida.'); return
    }
    setSavingBrand(true)
    try {
      await api('/portal/settings/brand', {
        method: 'POST',
        body: JSON.stringify({ name, primary_color: color, banner_url: banner, whatsapp, text_color: textColor }),
      })
      toast('ok', 'Marca do checkout salva.')
    } catch (err: any) {
      toast('err', err.message || 'Erro ao salvar a marca do checkout.')
    } finally {
      setSavingBrand(false)
    }
  }

  async function uploadBrandImage(file: File) {
    if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) {
      toast('err', 'Use uma imagem JPG, PNG ou WEBP.')
      return
    }
    if (file.size > 8 * 1024 * 1024) { toast('err', 'A imagem deve ter no máximo 8 MB.'); return }
    const form = new FormData()
    form.append('image', file)
    try {
      const r = await api<{ url: string }>('/portal/settings/brand/upload', { method: 'POST', body: form })
      setBrandBanner(r.url)
      toast('ok', 'Imagem carregada. Salve a marca para aplicar.')
    } catch (e: any) { toast('err', e.message || 'Erro ao carregar imagem.') }
  }

  // ── Ações: auto_emit_labels (produtor c/ expedição) ─────────────────────
  async function toggleAutoEmit() {
    const next = !autoEmitLabels
    setSavingAutoEmit(true)
    try {
      await api('/portal/settings', {
        method: 'POST',
        body: JSON.stringify({ auto_emit_labels: next }),
      })
      setAutoEmitLabels(next)
      toast('ok', next ? 'Emissão automática de etiquetas ativada.' : 'Emissão automática desativada.')
    } catch (e: any) {
      toast('err', e.message || 'Erro ao salvar preferência.')
    } finally {
      setSavingAutoEmit(false)
    }
  }

  // ── Ações: 2FA ──────────────────────────────────────────────────────────
  async function toggle2fa() {
    const next = !twofaEnabled
    setTwofaEnabled(next) // otimista
    try {
      await api('/portal/settings/2fa', {
        method: 'POST',
        body: JSON.stringify({ enabled: next }),
      })
      toast('ok', next ? '2FA ativado.' : '2FA desativado.')
    } catch (e: any) {
      setTwofaEnabled(!next) // reverte
      toast('err', e.message || 'Erro ao salvar 2FA.')
    }
  }

  // ── Ações: alterar e-mail (espelha szV2StSaveEmail) ──────────────────────
  async function saveEmail(e: FormEvent) {
    e.preventDefault()
    setEmailMsg(null)
    const email = newEmail.trim()
    if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email)) {
      setEmailMsg({ kind: 'err', text: 'Informe um e-mail válido.' })
      return
    }
    // AUDIT-2026-06-21 #9: re-autenticação obrigatória (server fail-closed).
    if (!emailCurPw) {
      setEmailMsg({ kind: 'err', text: 'Informe a senha atual para confirmar.' })
      return
    }
    setSavingEmail(true)
    try {
      await api('/portal/account/email', {
        method: 'POST',
        body: JSON.stringify({ email, current_password: emailCurPw }),
      })
      setEmailMsg({ kind: 'ok', text: 'E-mail atualizado. Faça login novamente.' })
      toast('ok', 'E-mail atualizado.')
      setNewEmail('')
      setEmailCurPw('')
    } catch (err: any) {
      setEmailMsg({ kind: 'err', text: err.message || 'Erro ao atualizar e-mail.' })
      toast('err', err.message || 'Erro ao atualizar e-mail.')
    } finally {
      setSavingEmail(false)
    }
  }

  // ── Ações: alterar senha (espelha szV2StSavePw) ──────────────────────────
  async function savePassword(e: FormEvent) {
    e.preventDefault()
    setPwMsg(null)
    if (!pwCur) {
      setPwMsg({ kind: 'err', text: 'Informe a senha atual.' })
      return
    }
    if (pwNew.length < 8) {
      setPwMsg({ kind: 'err', text: 'A nova senha deve ter no mínimo 8 caracteres.' })
      return
    }
    setSavingPw(true)
    try {
      await api('/portal/account/password', {
        method: 'POST',
        body: JSON.stringify({ current_password: pwCur, new_password: pwNew }),
      })
      setPwMsg({ kind: 'ok', text: 'Senha alterada. Faça login novamente.' })
      toast('ok', 'Senha alterada.')
      setPwCur('')
      setPwNew('')
    } catch (err: any) {
      setPwMsg({ kind: 'err', text: err.message || 'Erro ao alterar senha.' })
      toast('err', err.message || 'Erro ao alterar senha.')
    } finally {
      setSavingPw(false)
    }
  }

  // ── Ações: adicionar conta PIX (espelha szV2PixAdd) ──────────────────────
  // #18: titular vem do CADASTRO (me.nome). O CPF do titular vem do cadastro
  // (me.document) quando existe; quando o cadastro NÃO tem CPF (comum p/ afiliado),
  // o titular informa o CPF MANUALMENTE no form (pixCpf). Quando pixTipo='cpf', a
  // chave é o próprio CPF do titular (cadastro ou manual).
  async function addPixAccount(e: FormEvent) {
    e.preventDefault()
    setPixMsg(null)
    const holderName = (me?.nome || '').trim()
    const cadDoc = (me?.document || '').replace(/\D/g, '')
    // Documento efetivo do titular: do cadastro se houver, senão o informado no form.
    const holderDocDigits = cadDoc || pixCpf.replace(/\D/g, '')
    // Quando a chave é CPF/CNPJ, o conteúdo é o próprio documento do titular.
    const chave = pixTipo === 'cpf' || pixTipo === 'cnpj' ? holderDocDigits : pixChave.trim()
    if (holderName.length < 3) {
      setPixMsg({ kind: 'err', text: 'Não foi possível ler o titular do seu cadastro.' })
      return
    }
    // Documento do titular obrigatório e válido (o backend valida o DV — checamos
    // antes p/ dar um aviso amigável em vez do 422 cru).
    if (!validDoc(holderDocDigits)) {
      setPixMsg({
        kind: 'err',
        text: cadDoc
          ? 'CPF/CNPJ do cadastro inválido. Atualize seu cadastro.'
          : 'Informe um CPF ou CNPJ de titular válido.',
      })
      return
    }
    if (!pixBanco.trim()) {
      setPixMsg({ kind: 'err', text: 'Selecione o banco.' })
      return
    }
    if (chave.length < 3) {
      setPixMsg({
        kind: 'err',
        text:
          pixTipo === 'cpf' || pixTipo === 'cnpj'
            ? 'Documento do titular indisponível para usar como chave PIX.'
            : 'Informe a chave PIX.',
      })
      return
    }
    setSavingPix(true)
    try {
      await api('/portal/wallet/accounts', {
        method: 'POST',
        body: JSON.stringify({
          // Titular e CPF derivam do cadastro — não há mais inputs editáveis.
          holder_name: holderName,
          holder_cpf: holderDocDigits,
          banco: pixBanco.trim(),
          // #18: novos campos — backend ainda NÃO persiste (ver relatório).
          agencia: pixAgencia.trim(),
          conta: pixConta.trim(),
          pix_type: pixTipo,
          pix_key: chave,
        }),
      })
      setPixMsg({ kind: 'ok', text: 'Conta PIX adicionada.' })
      toast('ok', 'Conta PIX adicionada.')
      setPixBanco('')
      setPixAgencia('')
      setPixConta('')
      setPixTipo('cpf')
      setPixChave('')
      setPixCpf('')
      // recarrega lista
      setAccLoaded(false)
    } catch (err: any) {
      setPixMsg({ kind: 'err', text: err.message || 'Erro ao adicionar conta PIX.' })
      toast('err', err.message || 'Erro ao adicionar conta PIX.')
    } finally {
      setSavingPix(false)
    }
  }

  // ── Ação: excluir minha conta (LGPD — #43) ───────────────────────────────
  // POST /portal/account/delete { password, confirm:"EXCLUIR" } (settings.go).
  // O backend anonimiza (não hard-delete) e revoga sessões. Após sucesso,
  // limpamos o token e redirecionamos ao login.
  async function deleteAccount() {
    setDelErr('')
    if (!delPassword) {
      setDelErr('Informe sua senha para confirmar.')
      return
    }
    if (delConfirm.trim().toUpperCase() !== 'EXCLUIR') {
      setDelErr('Digite EXCLUIR para confirmar a exclusão.')
      return
    }
    setDeleting(true)
    try {
      await api('/portal/account/delete', {
        method: 'POST',
        body: JSON.stringify({ password: delPassword, confirm: 'EXCLUIR' }),
      })
      toast('ok', 'Conta excluída. Seus dados foram anonimizados.')
      clearToken()
      navigate('/login')
    } catch (err: any) {
      setDelErr(err?.message || 'Não foi possível excluir a conta agora.')
    } finally {
      setDeleting(false)
    }
  }

  // ── Ação: salvar pref de notificação (espelha szV2NotifSave) ─────────────
  async function toggleNotif(eventKey: string) {
    if (!me) return
    const next = { ...notifPrefs, [eventKey]: !notifPrefs[eventKey] }
    setNotifPrefs(next) // otimista
    try {
      await notifFetch('/prefs', {
        method: 'POST',
        body: JSON.stringify({ user_id: me.wp_user_id, prefs: next }),
      })
      toast('ok', 'Preferência de notificação salva.')
    } catch (err: any) {
      setNotifPrefs(notifPrefs) // reverte
      toast('err', err.message || 'Erro ao salvar notificação.')
    }
  }

  // ── Ação: exportar meus dados (LGPD — direito de acesso/portabilidade) ────
  // GET /portal/account/export (Bearer JWT). api() já retorna o JSON parseado;
  // re-serializamos num Blob e disparamos o download de meus-dados-falkz.json.
  // Se o endpoint ainda não existir em runtime, cai no catch → toast de erro.
  async function exportData() {
    setExporting(true)
    try {
      const data = await api('/portal/account/export')
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: 'application/json',
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'meus-dados-falk.json' // ALIGN-2026-06-21: nome de arquivo de marca falkz -> falk
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
      toast('ok', 'Download dos seus dados iniciado.')
    } catch (err: any) {
      toast('err', err?.message || 'Não foi possível exportar seus dados agora.')
    } finally {
      setExporting(false)
    }
  }

  // #23: handlers de aceite/revogação de consentimento removidos (o aceite ocorre
  // no 1º acesso, não nesta aba). Endpoints /portal/account/consent[/revoke]
  // seguem existindo no backend para o fluxo de 1º acesso.

  const doc = fmtDoc(me?.document)
  const phone = fmtPhone(me?.phone)

  // #75a: o usuário "usa EXPEDIÇÃO" quando NÃO é afiliado/cliente E tem classe de
  // frete definida (shipping_class_id). Espelha o hasExp da Carteira (Wallet.tsx):
  // `role !== 'affiliate' && !!me.shipping_class_id`. Afiliado/cliente são COD-only.
  // Quando false, o grupo de notificações de Expedição é ocultado (cliente COD-only
  // não recebe eventos de expedição, então as opções não fazem sentido p/ ele).
  const roleRaw = (me?.role || '').toLowerCase().trim()
  const isAffiliateRole = roleRaw === 'affiliate' || roleRaw === 'afiliado' || roleRaw === 'cliente'
  // CHECKOUT-BRANDING: a marca do checkout é GOVERNANÇA DO PRODUTOR (dono dos
  // links). Gate idêntico ao backend isProdutorRole (produtor|producer) — assim a
  // aba só aparece para quem o POST /portal/settings/brand aceita (operator é
  // !isAffiliateRole mas NÃO produtor, então não veria a aba indevidamente).
  const isProducer = roleRaw === 'produtor' || roleRaw === 'producer'
  const hasExp = !isAffiliateRole && !!me?.shipping_class_id
  // Esconde o grupo 'expedicao' quando o usuário não usa expedição (COD-only).
  const visibleNotifGroups = NOTIF_GROUPS.filter(g => g.id !== 'expedicao' || hasExp)

  // ── Render ────────────────────────────────────────────────────────────────
  return (
    <section id="sec-settings" className="sz-sec">
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        {/* #38: heading alinhado ao item de menu renomeado 'Perfil' (era Configurações). */}
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Perfil
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Gerencie sua conta, saques, notificações e segurança.
        </p>
      </div>

      {/* Sub-abas (espelha szv2-prod-subtabs do WP). Só renderiza DEPOIS que /portal/me
          carrega — a aba "Marca do checkout" (gate por isProducer, que depende do role
          async) aparece JUNTO com as demais, sem piscar (pop-in). Fixa igual às outras. */}
      {!loadingMe && (
      <div className="szv2-prod-subtabs" role="tablist" style={{ marginBottom: 16 }}>
        {([
          ['conta', 'Conta'],
          ['saques', 'Saques'],
          ['notif', 'Notificações'],
          ['taxas', 'Taxas & Prazos'],
          // CHECKOUT-BRANDING: aba "Marca do checkout" só para o PRODUTOR.
          ...(isProducer ? [['marca', 'Marca do checkout'] as [TabKey, string]] : []),
          ['convite', 'Convite de associado'],
          ['suporte', 'Suporte'],
        ] as [TabKey, string][]).map(([key, label]) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={tab === key}
            className={`szv2-prod-subtab${tab === key ? ' szv2-prod-subtab--active' : ''}`}
            onClick={() => setTab(key)}
          >
            {label}
          </button>
        ))}
      </div>
      )}

      {/* ───────────── Conta ───────────── */}
      {tab === 'conta' && (
        <div className="szv2-conn-panel">
          {meErr && <div className="sz-alert-danger">{meErr}</div>}

          {/* Card: Sua conta (read-only) */}
          <div className="szv2-card">
            <div className="szv2-card-head" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h2>Sua conta</h2>
              <button
                type="button"
                className="szv2-btn"
                onClick={() => setDocChangeOpen(true)}
              >
                Alterar CPF / CNPJ
              </button>
            </div>
            {loadingMe ? (
              <p className="szv2-empty-inline" style={{ padding: 12 }}>Carregando…</p>
            ) : (
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fit,minmax(150px,1fr))',
                  gap: 12,
                }}
              >
                <div className="szv2-info-cell">
                  <div className="szv2-info-label">Nome</div>
                  <div className="szv2-info-value">{me?.nome || '—'}</div>
                </div>
                <div className="szv2-info-cell">
                  <div className="szv2-info-label">{doc.label}</div>
                  <div className="szv2-info-value">{doc.value}</div>
                </div>
                <div className="szv2-info-cell">
                  <div className="szv2-info-label">E-mail</div>
                  <div className="szv2-info-value">{me?.email || '—'}</div>
                </div>
                <div className="szv2-info-cell">
                  <div className="szv2-info-label">Telefone</div>
                  <div className="szv2-info-value">{phone}</div>
                </div>
                <div className="szv2-info-cell">
                  <div className="szv2-info-label">Perfil</div>
                  <div className="szv2-info-value">{roleLabel(me?.role || '')}</div>
                </div>
              </div>
            )}
          </div>

          {/* Drawer: troca CPF ⇄ CNPJ (aprovação do admin). */}
          <DocumentChangeDrawer
            open={docChangeOpen}
            onClose={() => setDocChangeOpen(false)}
            onSubmitted={() => {
              // Recarrega /portal/me (o nome só muda após aprovação, mas mantém a UI fresca).
              api<MeResp>('/portal/me').then(setMe).catch(() => {})
            }}
          />

          {/* Card: Dados de acesso (e-mail + senha + 2FA) */}
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Dados de acesso</h2>
                <p className="szv2-card-sub">Atualize seu e-mail, senha e a verificação em duas etapas.</p>
              </div>
            </div>
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fit,minmax(280px,1fr))',
                gap: 24,
                alignItems: 'flex-start',
              }}
            >
              {/* Alterar e-mail */}
              <form onSubmit={saveEmail}>
                <p style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', margin: '0 0 4px' }}>
                  Alterar e-mail
                </p>
                <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: '0 0 12px' }}>
                  Ao alterar, você precisará fazer login novamente com o novo e-mail.
                </p>
                <div className="szv2-input-group">
                  <label className="szv2-label">Novo e-mail</label>
                  <input
                    type="text"
                    className="szv2-input"
                    placeholder="novo@email.com"
                    autoComplete="off"
                    value={newEmail}
                    onChange={e => setNewEmail(e.target.value)}
                  />
                </div>
                <div className="szv2-input-group">
                  <label className="szv2-label">Senha atual</label>
                  <input
                    type="password"
                    className="szv2-input"
                    placeholder="confirme com sua senha atual"
                    autoComplete="current-password"
                    value={emailCurPw}
                    onChange={e => setEmailCurPw(e.target.value)}
                  />
                </div>
                <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: '0 0 8px' }}>
                  Você será desconectado após alterar o e-mail.
                </p>
                {emailMsg && (
                  <div
                    style={{
                      fontSize: 13,
                      marginBottom: 8,
                      color: emailMsg.kind === 'ok' ? 'var(--szv2-success)' : 'var(--szv2-danger)',
                    }}
                  >
                    {emailMsg.text}
                  </div>
                )}
                <button type="submit" className="szv2-btn szv2-btn-brand" disabled={savingEmail}>
                  {savingEmail ? 'Atualizando…' : 'Atualizar e-mail'}
                </button>
              </form>

              {/* Alterar senha */}
              <form onSubmit={savePassword}>
                <p style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', margin: '0 0 4px' }}>
                  Alterar senha
                </p>
                <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: '0 0 12px' }}>
                  Use uma senha forte com no mínimo 8 caracteres.
                </p>
                <div className="szv2-input-group">
                  <label className="szv2-label">Senha atual</label>
                  <input
                    type="password"
                    className="szv2-input"
                    placeholder="Senha atual"
                    autoComplete="current-password"
                    value={pwCur}
                    onChange={e => setPwCur(e.target.value)}
                  />
                </div>
                <div className="szv2-input-group">
                  <label className="szv2-label">Nova senha</label>
                  <input
                    type="password"
                    className="szv2-input"
                    placeholder="Nova senha"
                    autoComplete="new-password"
                    value={pwNew}
                    onChange={e => setPwNew(e.target.value)}
                  />
                </div>
                <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: '0 0 8px' }}>
                  Você será desconectado após alterar a senha.
                </p>
                {pwMsg && (
                  <div
                    style={{
                      fontSize: 13,
                      marginBottom: 8,
                      color: pwMsg.kind === 'ok' ? 'var(--szv2-success)' : 'var(--szv2-danger)',
                    }}
                  >
                    {pwMsg.text}
                  </div>
                )}
                <button type="submit" className="szv2-btn szv2-btn-brand" disabled={savingPw}>
                  {savingPw ? 'Alterando…' : 'Alterar senha'}
                </button>
              </form>
            </div>

            {/* 2FA */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginTop: 20,
                paddingTop: 20,
                borderTop: '1px solid var(--szv2-divider)',
              }}
            >
              <div>
                <p style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', margin: '0 0 2px' }}>
                  Verificação em duas etapas (2FA)
                </p>
                <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: 0 }}>
                  Código por e-mail a cada login. Recomendado para proteger o acesso ao painel.
                </p>
              </div>
              <label className="szv2-toggle-lbl">
                <input type="checkbox" checked={twofaEnabled} onChange={toggle2fa} />
                <span className="szv2-toggle-slider" />
              </label>
            </div>
          </div>

          {/* Card "Privacidade & dados (LGPD)" MOVIDO p/ o FINAL da aba Suporte
              (pedido do dono). Render agora no bloco {tab === 'suporte'} abaixo. */}

          {/* Card: Expedição automática (só para produtores com frete configurado) */}
          {hasExp && (
            <div className="szv2-card">
              <div className="szv2-card-head">
                <div>
                  <h2>Expedição automática</h2>
                  <p className="szv2-card-sub">
                    Quando ativado, etiquetas são emitidas automaticamente assim que o pedido é aprovado.
                    Desativado, você aprova cada pedido manualmente em <strong>Expedição → Emitir Etiqueta</strong>.
                  </p>
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '12px 0 0' }}>
                <label className="szv2-toggle-lbl">
                  <input
                    type="checkbox"
                    checked={autoEmitLabels}
                    onChange={toggleAutoEmit}
                    disabled={savingAutoEmit}
                  />
                  <span className="szv2-toggle-slider" />
                </label>
                <span style={{ fontSize: 14, color: 'var(--szv2-text-soft)' }}>
                  {autoEmitLabels ? 'Emissão automática ativada' : 'Emissão automática desativada (manual)'}
                </span>
              </div>
            </div>
          )}
        </div>
      )}

      {/* ───────────── Saques (contas PIX) ───────────── */}
      {tab === 'saques' && (
        <div className="szv2-conn-panel">
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Contas para saque</h2>
                <p className="szv2-card-sub">
                  Cadastre suas contas PIX. A conta deve pertencer ao titular do cadastro.
                </p>
              </div>
            </div>

            {/* #75 (REVERTIDO): o afiliado agora gerencia as PRÓPRIAS contas PIX —
                o gate read-only "gerenciadas na carteira COD do produtor" saiu.
                Mesmo form do produtor; o backend escopa por user_id sem colisão. */}
            <form onSubmit={addPixAccount}>
                {/* #18: card de aviso — a conta deve pertencer ao titular do cadastro */}
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 10,
                    padding: '12px 14px',
                    marginBottom: 16,
                    background: 'rgba(30, 111, 242, 0.08)',
                    border: `1px solid ${FALK_BLUE}`,
                    borderRadius: 'var(--szv2-radius-md)',
                  }}
                >
                  <span aria-hidden="true" style={{ color: FALK_BLUE, fontSize: 16, lineHeight: 1.3 }}>ⓘ</span>
                  <div>
                    <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', marginBottom: 2 }}>
                      A conta deve pertencer ao titular do cadastro
                    </div>
                    <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: 0, lineHeight: 1.5 }}>
                      Por segurança, os saques só são pagos para uma conta no nome de{' '}
                      <strong style={{ color: 'var(--szv2-text)' }}>{me?.nome || 'você'}</strong>
                      {doc.value !== '—' ? <> (CPF/CNPJ <strong style={{ color: 'var(--szv2-text)' }}>{doc.value}</strong>)</> : null}.
                      {doc.value !== '—'
                        ? ' O titular e o documento são preenchidos automaticamente a partir do seu cadastro.'
                        : ' Seu cadastro não tem um CPF — informe o CPF do titular abaixo para concluir.'}
                    </p>
                  </div>
                </div>

                <div
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fit,minmax(220px,1fr))',
                    gap: 12,
                  }}
                >
                  {/* Banco — SELECT com ~50 maiores do Brasil */}
                  <div className="szv2-input-group">
                    <label className="szv2-label">Banco</label>
                    <FalkSelect
                      aria-label="Banco"
                      value={pixBanco}
                      onChange={v => setPixBanco(v)}
                      placeholder="Selecione o banco…"
                      options={[
                        { value: '', label: 'Selecione o banco…' },
                        ...BANCOS_BR.map(b => ({ value: b, label: b })),
                      ]}
                    />
                  </div>
                  {/* Agência (novo) */}
                  <div className="szv2-input-group">
                    <label className="szv2-label">Agência</label>
                    <input
                      type="text"
                      className="szv2-input"
                      placeholder="0000"
                      inputMode="numeric"
                      autoComplete="off"
                      value={pixAgencia}
                      onChange={e => setPixAgencia(e.target.value)}
                    />
                  </div>
                  {/* Conta (novo) */}
                  <div className="szv2-input-group">
                    <label className="szv2-label">Conta</label>
                    <input
                      type="text"
                      className="szv2-input"
                      placeholder="00000-0"
                      autoComplete="off"
                      value={pixConta}
                      onChange={e => setPixConta(e.target.value)}
                    />
                  </div>
                  {/* CPF/CNPJ do titular — do cadastro (read-only) quando existe (label segue
                      o tipo real: CPF ou CNPJ); quando o cadastro NÃO tem documento (comum
                      p/ afiliado), o titular informa aqui. */}
                  <div className="szv2-input-group">
                    <label className="szv2-label">
                      {doc.value !== '—' ? doc.label : 'CPF/CNPJ'} do titular
                      {doc.value !== '—' && (
                        <span style={{ fontWeight: 400, color: 'var(--szv2-text-muted)', marginLeft: 6 }}>
                          (do cadastro)
                        </span>
                      )}
                    </label>
                    <input
                      type="text"
                      className="szv2-input"
                      placeholder="000.000.000-00 ou 00.000.000/0000-00"
                      inputMode="numeric"
                      autoComplete="off"
                      readOnly={doc.value !== '—'}
                      value={doc.value !== '—' ? doc.value : pixCpf}
                      onChange={e => setPixCpf(e.target.value)}
                      style={doc.value !== '—' ? { background: 'var(--szv2-surface-alt)', cursor: 'not-allowed' } : undefined}
                    />
                    {doc.value === '—' && (
                      <p style={{ fontSize: 11.5, color: 'var(--szv2-text-muted)', margin: '4px 0 0' }}>
                        Seu cadastro não tem CPF/CNPJ. Informe o documento do titular da conta.
                      </p>
                    )}
                  </div>
                  {/* Tipo de chave PIX */}
                  <div className="szv2-input-group">
                    <label className="szv2-label">Tipo de chave PIX</label>
                    <FalkSelect
                      aria-label="Tipo de chave PIX"
                      value={pixTipo}
                      onChange={v => setPixTipo(v)}
                      options={[
                        { value: 'cpf', label: 'CPF' },
                        { value: 'cnpj', label: 'CNPJ' },
                        { value: 'email', label: 'E-mail' },
                        { value: 'telefone', label: 'Telefone' },
                        { value: 'aleatoria', label: 'Chave aleatória' },
                      ]}
                    />
                  </div>
                  {/* Conteúdo da chave PIX — quando tipo=CPF, espelha o CPF do titular
                      (cadastro OU informado acima), read-only. */}
                  <div className="szv2-input-group" style={{ gridColumn: '1 / -1' }}>
                    <label className="szv2-label">
                      Conteúdo da chave PIX
                      {(pixTipo === 'cpf' || pixTipo === 'cnpj') && (
                        <span style={{ fontWeight: 400, color: 'var(--szv2-text-muted)', marginLeft: 6 }}>
                          ({pixTipo === 'cpf' ? 'CPF' : 'CNPJ'} do titular)
                        </span>
                      )}
                    </label>
                    <input
                      type="text"
                      className="szv2-input"
                      placeholder={
                        pixTipo === 'cpf' || pixTipo === 'cnpj'
                          ? pixTipo === 'cpf' ? '000.000.000-00' : '00.000.000/0000-00'
                          : 'E-mail, telefone ou chave aleatória'
                      }
                      autoComplete="off"
                      readOnly={pixTipo === 'cpf' || pixTipo === 'cnpj'}
                      value={
                        pixTipo === 'cpf' || pixTipo === 'cnpj'
                          ? doc.value !== '—'
                            ? doc.value
                            : pixCpf
                          : pixChave
                      }
                      onChange={e => setPixChave(e.target.value)}
                      style={pixTipo === 'cpf' || pixTipo === 'cnpj' ? { background: 'var(--szv2-surface-alt)', cursor: 'not-allowed' } : undefined}
                    />
                    {(pixTipo === 'cpf' || pixTipo === 'cnpj') && (
                      <p style={{ fontSize: 11.5, color: 'var(--szv2-text-muted)', margin: '4px 0 0' }}>
                        Quando o tipo é {pixTipo === 'cpf' ? 'CPF' : 'CNPJ'}, a chave é o próprio {pixTipo === 'cpf' ? 'CPF' : 'CNPJ'} do titular informado acima.
                      </p>
                    )}
                  </div>
                </div>
                {pixMsg && (
                  <div
                    style={{
                      fontSize: 13,
                      marginTop: 8,
                      marginBottom: 8,
                      color: pixMsg.kind === 'ok' ? 'var(--szv2-success)' : 'var(--szv2-danger)',
                    }}
                  >
                    {pixMsg.text}
                  </div>
                )}
                <button type="submit" className="szv2-btn szv2-btn-brand" disabled={savingPix} style={{ marginTop: 12 }}>
                  {savingPix ? 'Adicionando…' : 'Adicionar conta'}
                </button>
              </form>

            {/* Lista de contas cadastradas */}
            <div style={{ marginTop: 20 }}>
              {loadingAcc ? (
                <p className="szv2-empty-inline" style={{ padding: 12 }}>Carregando contas…</p>
              ) : accounts.length === 0 ? (
                <EmptyState
                  icon="🏦"
                  title="Nenhuma conta cadastrada"
                  description="Adicione uma conta PIX para solicitar saques."
                />
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  <div className="szv2-info-label">Contas cadastradas</div>
                  {accounts.map(a => (
                    <div
                      key={a.id}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 12,
                        padding: '10px 14px',
                        background: 'var(--szv2-surface-alt)',
                        border: '1px solid var(--szv2-border)',
                        borderRadius: 'var(--szv2-radius-md)',
                      }}
                    >
                      <div>
                        <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)' }}>
                          {a.holder_name || '—'}
                          {a.is_default && (
                            <span
                              className="sz-badge szv2-badge-success"
                              style={{ marginLeft: 8, fontSize: 11 }}
                            >
                              Padrão
                            </span>
                          )}
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                          {(a.pix_type || '').charAt(0).toUpperCase() + (a.pix_type || '').slice(1)}:{' '}
                          {maskPix(a.pix_key)}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* ───────────── Notificações (#29: segmentadas por tipo) ───────────── */}
      {tab === 'notif' && (
        <div className="szv2-conn-panel">
          {/* Cabeçalho geral da aba — contexto comum aos dois grupos. */}
          <p
            style={{
              fontSize: 13,
              color: 'var(--szv2-text-muted)',
              margin: '0 0 16px',
              lineHeight: 1.5,
            }}
          >
            Receba alertas push no navegador quando houver atividade nos pedidos. As notificações
            são separadas por tipo — ative apenas os eventos que importam para você.
          </p>

          {loadingNotif ? (
            <div className="szv2-card">
              <p className="szv2-empty-inline" style={{ padding: 12 }}>Carregando preferências…</p>
            </div>
          ) : (
            // Um card por grupo (Expedição e Motoboy / COD) — header com acento FALK.
            // #75a: o grupo Expedição só aparece p/ quem usa expedição (hasExp).
            visibleNotifGroups.map(group => (
              <div className="szv2-card" key={group.id}>
                <div className="szv2-card-head">
                  <div>
                    {/* Header do grupo com a barra de acento azul FALK (#1E6FF2). */}
                    <h2
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 8,
                      }}
                    >
                      <span
                        aria-hidden="true"
                        style={{
                          display: 'inline-block',
                          width: 4,
                          height: 16,
                          borderRadius: 2,
                          background: FALK_BLUE,
                        }}
                      />
                      {group.title}
                    </h2>
                    <p className="szv2-card-sub">{group.desc}</p>
                  </div>
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
                  {group.events.map((ev, i) => (
                    <div
                      key={ev.key}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        padding: '14px 0',
                        borderBottom:
                          i === group.events.length - 1 ? 'none' : '1px solid var(--szv2-divider)',
                      }}
                    >
                      <div>
                        <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', marginBottom: 2 }}>
                          {ev.title}
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>{ev.desc}</div>
                      </div>
                      <label className="szv2-toggle-lbl">
                        <input
                          type="checkbox"
                          checked={!!notifPrefs[ev.key]}
                          onChange={() => toggleNotif(ev.key)}
                        />
                        <span className="szv2-toggle-slider" />
                      </label>
                    </div>
                  ))}
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {/* ───────────── Taxas & Prazos (cards informativos) ───────────── */}
      {/* #76: os valores abaixo (R$ 2,99 / 4,99% / R$ 8,00 / 7 dias) são a config de
          PRODUTOR — o cliente/afiliado NÃO herda essa governança. Para esses papéis,
          as taxas/prazos serão definidos pela FAIXA do usuário (tiers por volume), que
          ainda não foi implementada (aguarda números do dono). Até lá, mostramos um
          placeholder honesto em vez dos números de produtor. */}
      {tab === 'taxas' && isAffiliateRole && (
        <div className="szv2-conn-panel">
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Taxas &amp; prazos</h2>
                <span className="szv2-card-sub">
                  Definidos conforme a sua faixa de operação.
                </span>
              </div>
            </div>
            <div
              style={{
                display: 'flex',
                alignItems: 'flex-start',
                gap: 10,
                padding: '14px 16px',
                background: 'rgba(30, 111, 242, 0.08)',
                border: `1px solid ${FALK_BLUE}`,
                borderRadius: 'var(--szv2-radius-md)',
              }}
            >
              <span aria-hidden="true" style={{ color: FALK_BLUE, fontSize: 16, lineHeight: 1.3 }}>ⓘ</span>
              <div>
                <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', marginBottom: 4 }}>
                  Suas taxas e prazos são definidos pela sua faixa
                </div>
                <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', margin: 0, lineHeight: 1.6 }}>
                  As taxas (saque, antecipação, frustração) e o prazo de liberação dos recebíveis
                  variam de acordo com a <strong style={{ color: 'var(--szv2-text)' }}>faixa</strong> em
                  que você se enquadra — escalonada pelo volume de pedidos. Assim que a sua faixa for
                  definida, os valores aplicáveis aparecerão aqui. Em caso de dúvida, fale com o suporte.
                </p>
              </div>
            </div>
            <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: '12px 2px 0', lineHeight: 1.5 }}>
              Sempre confira o valor líquido na carteira antes de solicitar saque.
            </p>
          </div>
        </div>
      )}

      {tab === 'taxas' && !isAffiliateRole && (
        <div className="szv2-conn-panel">
          {/* Prazo de recebíveis */}
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Prazo de recebíveis</h2>
                <span className="szv2-card-sub">
                  Tempo para crédito aparecer como disponível para saque.
                </span>
              </div>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(240px,1fr))', gap: 12 }}>
              <div
                style={{
                  background: 'var(--szv2-surface-alt)',
                  borderRadius: 'var(--szv2-radius-md)',
                  padding: '16px 20px',
                }}
              >
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>
                  Retenção após entrega
                </div>
                <div style={{ fontSize: 28, fontWeight: 700, color: 'var(--szv2-brand)', lineHeight: 1 }}>
                  7<span style={{ fontSize: 14, fontWeight: 500, marginLeft: 4 }}>dias</span>
                </div>
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 4 }}>
                  após entrega confirmada
                </div>
              </div>
              <div
                style={{
                  background: 'var(--szv2-surface-alt)',
                  borderRadius: 'var(--szv2-radius-md)',
                  padding: '16px 20px',
                  display: 'flex',
                  alignItems: 'center',
                }}
              >
                <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', margin: 0, lineHeight: 1.5 }}>
                  O prazo protege contra chargebacks. Ao final, o saldo muda de{' '}
                  <strong style={{ color: 'var(--szv2-text)' }}>Pendente</strong> para{' '}
                  <strong style={{ color: 'var(--szv2-success)' }}>Disponível</strong> automaticamente.
                </p>
              </div>
            </div>
          </div>

          {/* Taxas de saque */}
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Taxas de saque</h2>
                <p className="szv2-card-sub">Valores cobrados ao solicitar ou antecipar um saque.</p>
              </div>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(200px,1fr))', gap: 12, marginBottom: 16 }}>
              <div style={{ background: 'var(--szv2-surface-alt)', borderRadius: 'var(--szv2-radius-md)', padding: '16px 20px' }}>
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>Taxa de saque padrão</div>
                <div style={{ fontSize: 26, fontWeight: 700, color: 'var(--szv2-brand)', lineHeight: 1 }}>R$&nbsp;2,99</div>
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 4 }}>por saque aprovado</div>
              </div>
              <div style={{ background: 'var(--szv2-surface-alt)', borderRadius: 'var(--szv2-radius-md)', padding: '16px 20px' }}>
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>Taxa de antecipação</div>
                <div style={{ fontSize: 26, fontWeight: 700, color: 'var(--szv2-brand)', lineHeight: 1 }}>
                  4,99<span style={{ fontSize: 16, marginLeft: 2 }}>%</span>
                </div>
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 4 }}>sobre o valor antecipado</div>
              </div>
              <div style={{ background: 'var(--szv2-surface-alt)', borderRadius: 'var(--szv2-radius-md)', padding: '16px 20px' }}>
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>Taxa de frustração</div>
                <div style={{ fontSize: 26, fontWeight: 700, color: 'var(--szv2-brand)', lineHeight: 1 }}>R$&nbsp;8,00</div>
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginTop: 4 }}>1ª ocorrência grátis</div>
              </div>
            </div>
            <div
              style={{
                padding: '12px 14px',
                background: 'var(--szv2-surface-alt)',
                borderLeft: '3px solid var(--szv2-border)',
                borderRadius: '0 var(--szv2-radius-sm) var(--szv2-radius-sm) 0',
                fontSize: 12,
                color: 'var(--szv2-text-muted)',
              }}
            >
              Taxas e prazos podem variar conforme a configuração comercial da conta. Sempre verifique o
              valor líquido na carteira antes de solicitar saque.
            </div>
          </div>
        </div>
      )}

      {/* ───────────── Marca do checkout (white-label v1 — produtor) ───────────── */}
      {tab === 'marca' && isProducer && (
        <div className="szv2-conn-panel">
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Marca do checkout</h2>
                <p className="szv2-card-sub">
                  Personalize a identidade visual exibida no checkout dos seus links.
                </p>
              </div>
            </div>

            {loadingBrand ? (
              <p className="szv2-empty-inline" style={{ padding: 12 }}>Carregando…</p>
            ) : (
              <form onSubmit={saveBrand}>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(240px,1fr))', gap: 16 }}>
                  <div className="szv2-input-group">
                    <label className="szv2-label">Nome da marca</label>
                    <input className="szv2-input" value={brandName} maxLength={80} placeholder="Ex.: Loja da Ana" onChange={e => setBrandName(e.target.value)} />
                    <p style={{ fontSize: 11.5, color: 'var(--szv2-text-muted)', margin: '4px 0 0' }}>Substitui o nome da plataforma no cabeçalho e rodapé.</p>
                  </div>
                  {/* Logo (URL https) */}
                  <div className="szv2-input-group">
                    <label className="szv2-label">WhatsApp de atendimento</label>
                    <input className="szv2-input" type="tel" inputMode="tel" placeholder="(11) 99999-9999" value={brandWhatsApp} onChange={e => setBrandWhatsApp(e.target.value)} />
                    <p style={{ fontSize: 11.5, color: 'var(--szv2-text-muted)', margin: '4px 0 0' }}>Usado no botão “Fale no WhatsApp” do checkout, confirmação e acompanhamento.</p>
                  </div>
                  <div className="szv2-input-group">
                    <label className="szv2-label">Cor dos textos</label>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                      <input type="color" aria-label="Selecionar cor dos textos" value={brandTextColor || '#182235'} onChange={e => setBrandTextColor(e.target.value)} style={{ width: 44, height: 38, padding: 2, border: '1px solid var(--szv2-border)', borderRadius: 'var(--szv2-radius-sm)', background: 'var(--szv2-surface)', cursor: 'pointer' }} />
                      <input className="szv2-input" placeholder="#182235" value={brandTextColor} onChange={e => setBrandTextColor(e.target.value)} />
                      <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setBrandTextColor('')}>Padrão</button>
                    </div>
                    <p style={{ fontSize: 11.5, color: 'var(--szv2-text-muted)', margin: '4px 0 0' }}>Aplicada aos textos das páginas pós-compra.</p>
                  </div>
                  {/* Cor primária */}
                  <div className="szv2-input-group">
                    <label className="szv2-label">Cor principal</label>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                      <input
                        type="color"
                        aria-label="Selecionar cor principal"
                        value={brandColor || FALK_BLUE}
                        onChange={e => setBrandColor(e.target.value)}
                        style={{
                          width: 44,
                          height: 38,
                          padding: 2,
                          border: '1px solid var(--szv2-border)',
                          borderRadius: 'var(--szv2-radius-sm)',
                          background: 'var(--szv2-surface)',
                          cursor: 'pointer',
                          flex: '0 0 auto',
                        }}
                      />
                      <input
                        type="text"
                        className="szv2-input"
                        placeholder="#2563eb"
                        autoComplete="off"
                        value={brandColor}
                        onChange={e => setBrandColor(e.target.value)}
                        style={{ flex: '1 1 auto', minWidth: 0 }}
                      />
                      <button
                        type="button"
                        className="szv2-btn szv2-btn-secondary"
                        style={{ whiteSpace: 'nowrap' }}
                        onClick={() => setBrandColor('')}
                      >
                        Padrão FALK
                      </button>
                    </div>
                    <p style={{ fontSize: 11.5, color: 'var(--szv2-text-muted)', margin: '4px 0 0', lineHeight: 1.5 }}>
                      Cor dos botões e destaques do checkout.
                    </p>
                  </div>
                </div>

                <div className="szv2-input-group" style={{ marginTop: 16 }}>
                  <label className="szv2-label">Banner padrão dos checkouts</label>
                  <p style={{ fontSize: 11.5, color: 'var(--szv2-text-muted)', margin: '0 0 6px' }}>Usado quando um checkout não tiver banner próprio. Para escolher por checkout, use `Checkouts & Links`. Recomendado: imagem horizontal 1200×360.</p>
                  <input className="szv2-input" type="text" inputMode="url" placeholder="URL opcional — ou envie um arquivo abaixo" value={brandBanner} onChange={e => setBrandBanner(e.target.value)} />
                  <input type="file" accept="image/png,image/jpeg,image/webp" onChange={e => e.target.files?.[0] && uploadBrandImage(e.target.files[0])} style={{ marginTop: 8, maxWidth: '100%' }} />
                  {brandBanner && <img src={brandBanner} alt="Pré-visualização do banner" style={{ display: 'block', width: '100%', maxHeight: 130, objectFit: 'cover', borderRadius: 10, marginTop: 10 }} />}
                </div>

                {/* Pré-visualização: cabeçalho + botão na cor escolhida */}
                <div style={{ marginTop: 20 }}>
                  <div className="szv2-info-label" style={{ marginBottom: 8 }}>Pré-visualização</div>
                  <div
                    style={{
                      border: '1px solid var(--szv2-border)',
                      borderRadius: 'var(--szv2-radius-md)',
                      overflow: 'hidden',
                      background: 'var(--szv2-surface-alt)',
                    }}
                  >
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 10,
                        padding: '12px 16px',
                        borderBottom: '1px solid var(--szv2-border)',
                        background: 'var(--szv2-surface)',
                      }}
                    >
                      <span style={{ fontWeight: 800, fontSize: 18, color: brandColor || FALK_BLUE }}>{brandName || 'Sua marca'}</span>
                      <span style={{ marginLeft: 'auto', fontSize: 12, fontWeight: 600, color: brandColor || FALK_BLUE }}>
                        🔒 Compra segura
                      </span>
                    </div>
                    <div style={{ padding: 16 }}>
                      <button
                        type="button"
                        disabled
                        style={{
                          width: '100%',
                          padding: '12px 16px',
                          border: 'none',
                          borderRadius: 'var(--szv2-radius-md)',
                          background: brandColor || FALK_BLUE,
                          color: '#fff',
                          fontWeight: 700,
                          fontSize: 14,
                          cursor: 'default',
                        }}
                      >
                        Finalizar compra
                      </button>
                    </div>
                  </div>
                </div>

                <button type="submit" className="szv2-btn szv2-btn-brand" disabled={savingBrand} style={{ marginTop: 16 }}>
                  {savingBrand ? 'Salvando…' : 'Salvar marca'}
                </button>
              </form>
            )}
          </div>
        </div>
      )}

      {/* ───────────── Convite de associado (#71) ───────────── */}
      {/* Link de indicação FIXO/PERMANENTE do usuário (/r/{code}). MOVIDO da tela
          Afiliação para cá (pedido do dono). Compartilhe para que novos associados
          se cadastrem vinculados a você. #82: copy 100% em "associado/indicação" —
          não misturar com o conceito de "afiliação" (programa separado). */}
      {tab === 'convite' && (
        <div className="szv2-conn-panel">
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Convite de associado</h2>
                <span className="szv2-card-sub">
                  Compartilhe seu link de indicação para que novos associados se
                  cadastrem vinculados a você.
                </span>
              </div>
            </div>

            {loadingRef ? (
              <p className="szv2-empty-inline" style={{ padding: 12 }}>Carregando…</p>
            ) : refErr ? (
              <div className="sz-alert-danger" style={{ margin: '8px 0' }}>{refErr}</div>
            ) : referral?.referral_link ? (
              <div className="szv2-input-group" style={{ marginTop: 4 }}>
                <label className="szv2-label">Seu link de indicação</label>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                  <input
                    type="text"
                    className="szv2-input"
                    readOnly
                    value={referral.referral_link}
                    onFocus={e => e.currentTarget.select()}
                    style={{ flex: '1 1 280px', minWidth: 0 }}
                  />
                  <button
                    type="button"
                    className="szv2-btn szv2-btn-brand"
                    style={{ whiteSpace: 'nowrap' }}
                    onClick={() => copyReferral(referral.referral_link as string)}
                  >
                    Copiar link
                  </button>
                </div>
                <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: '8px 2px 0', lineHeight: 1.5 }}>
                  Este é o seu link permanente. Quem se cadastrar por ele fica vinculado
                  a você como associado.
                </p>
              </div>
            ) : (
              <EmptyState
                title="Link de convite indisponível"
                description="Não foi possível gerar seu link de indicação no momento. Tente novamente mais tarde."
              />
            )}
          </div>

          {/* #82: Valores recebidos por associado.
              PLACEHOLDER HONESTO: hoje NÃO existe endpoint no go/portal que exponha
              o valor gerado por cada associado vinculado (referred_by / sz_referral_payout
              existem só no backend de payout, sem rota de leitura no portal). Enquanto
              o backend não expuser esse dado, mostramos a copy de associado + estado
              vazio honesto, sem reaproveitar o "repasse" de AFILIAÇÃO (conceito separado). */}
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Valores recebidos por associado</h2>
                <span className="szv2-card-sub">
                  Acompanhe quanto cada associado vinculado já gerou para você.
                </span>
              </div>
            </div>
            <EmptyState
              icon="🤝"
              title="Nenhum valor ainda"
              description="Quando seus associados começarem a vender, os valores recebidos por cada um aparecerão aqui."
            />
          </div>
        </div>
      )}

      {/* ───────────── Suporte (embute o componente Support) ───────────── */}
      {tab === 'suporte' && (
        <div className="szv2-conn-panel">
          <Support />

          {/* Card: Privacidade & dados (LGPD) — MOVIDO da aba Conta para o FINAL
              do submenu Suporte (pedido do dono). Reaproveita os handlers/estado da
              própria tela Settings (exportData/exporting + setDel*) e o Drawer de
              exclusão abaixo — sem migração de lógica entre componentes. */}
          <div className="szv2-card" style={{ marginTop: 'var(--szv2-space-4)' }}>
            <div className="szv2-card-head"><h2>Privacidade &amp; dados (LGPD)</h2></div>

            {/* Baixar meus dados (direito de acesso/portabilidade) */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 16,
                flexWrap: 'wrap',
              }}
            >
              <div>
                <p style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', margin: '0 0 2px' }}>
                  Baixar meus dados
                </p>
                <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: 0 }}>
                  Exporte uma cópia dos seus dados pessoais em formato JSON (direito de acesso e portabilidade).
                </p>
              </div>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                disabled={exporting}
                onClick={exportData}
              >
                {exporting ? 'Gerando…' : 'Baixar meus dados (LGPD)'}
              </button>
            </div>

            {/* Excluir minha conta (LGPD — #43). Confirmação em DRAWER lateral. */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 16,
                flexWrap: 'wrap',
                marginTop: 20,
                paddingTop: 20,
                borderTop: '1px solid var(--szv2-divider)',
              }}
            >
              <div>
                <p style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-danger)', margin: '0 0 2px' }}>
                  Excluir minha conta
                </p>
                <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)', margin: 0, maxWidth: 520 }}>
                  Exclua permanentemente sua conta. Seus dados pessoais serão anonimizados conforme a LGPD.
                  Esta ação é irreversível e você perderá o acesso ao painel.
                </p>
              </div>
              <button
                type="button"
                className="szv2-btn szv2-btn-danger"
                onClick={() => {
                  setDelErr('')
                  setDelPassword('')
                  setDelConfirm('')
                  setDelOpen(true)
                }}
              >
                Excluir minha conta
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ───────────── Excluir conta — confirmação em DRAWER lateral (#43) ───────────── */}
      <Drawer
        open={delOpen}
        onClose={() => { if (!deleting) setDelOpen(false) }}
        title="Excluir minha conta"
        ariaLabel="Excluir minha conta"
      >
        <p style={{ fontSize: 14, lineHeight: 1.6, color: 'var(--szv2-text-soft)', margin: '0 0 12px' }}>
          Esta ação é <strong style={{ color: 'var(--szv2-danger)' }}>irreversível</strong>. Ao confirmar:
        </p>
        <ul style={{ fontSize: 13, lineHeight: 1.7, color: 'var(--szv2-text-muted)', margin: '0 0 16px', paddingLeft: 18 }}>
          <li>Seus dados pessoais (nome, e-mail, chave PIX) serão anonimizados conforme a LGPD.</li>
          <li>Você perderá imediatamente o acesso ao painel e será desconectado.</li>
          <li>Registros financeiros são mantidos de forma anônima por obrigação fiscal/contábil.</li>
        </ul>

        <div className="szv2-input-group" style={{ marginBottom: 12 }}>
          <label className="szv2-label">Sua senha</label>
          <input
            type="password"
            className="szv2-input"
            placeholder="Digite sua senha"
            autoComplete="current-password"
            value={delPassword}
            onChange={e => setDelPassword(e.target.value)}
          />
        </div>
        <div className="szv2-input-group" style={{ marginBottom: 12 }}>
          <label className="szv2-label">
            Digite <strong>EXCLUIR</strong> para confirmar
          </label>
          <input
            type="text"
            className="szv2-input"
            placeholder="EXCLUIR"
            autoComplete="off"
            value={delConfirm}
            onChange={e => setDelConfirm(e.target.value)}
          />
        </div>

        {delErr && (
          <div style={{ fontSize: 13, color: 'var(--szv2-danger)', marginBottom: 12 }}>{delErr}</div>
        )}

        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 8 }}>
          <button
            type="button"
            className="szv2-btn szv2-btn-secondary"
            disabled={deleting}
            onClick={() => setDelOpen(false)}
          >
            Cancelar
          </button>
          <button
            type="button"
            className="szv2-btn szv2-btn-danger"
            disabled={deleting}
            onClick={deleteAccount}
          >
            {deleting ? 'Excluindo…' : 'Excluir minha conta'}
          </button>
        </div>
      </Drawer>
    </section>
  )
}
