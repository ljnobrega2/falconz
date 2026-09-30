// Layout do Portal V2 (PRODUTOR e AFILIADO). Nav montada a partir do role
// retornado por GET /portal/me — espelha templates/portal/v2/dashboard-v2.php.
// Estrutura visual idêntica ao portal WP: .sz-dashboard-v2 > aside.szv2-sidebar + .szv2-main.
import { useState, useEffect, useCallback } from 'react'
import { NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import { api, clearToken, getToken, registerApi401Handlers } from '../api'
import ToastHost from './ToastHost'
import { ConfirmHost, confirmAsync } from './ConfirmDialog'
// A tela admin-orders/ (cópia verbatim da tela admin de Pedidos) usa SEU PRÓPRIO
// módulo ConfirmDialog (estado de módulo independente). O confirmAsync dela só
// renderiza se ESTE host estiver montado — senão reagendar/cancelar/clone travam
// (promise nunca resolve). Montamos ambos os hosts; só um fica ativo por vez.
import { ConfirmHost as AdminOrdersConfirmHost } from '../admin-orders/components/ConfirmDialog'
import { useToast } from '../hooks/useToast'
import DpoFooter from './DpoFooter'
import ProgressTier from './ProgressTier'
import SectionLoading from './SectionLoading'
import FalkLogo from './FalkLogo'

// ── Ícones (copiados de templates/portal/v2/sidebar.php $szs_icons) ─────────────
const ICONS: Record<string, JSX.Element> = {
  dashboard:    <svg viewBox="0 0 20 20"><path d="M3 3h6v8H3zM11 3h6v5h-6zM11 10h6v7h-6zM3 13h6v4H3z"/></svg>,
  orders:       <svg viewBox="0 0 20 20"><path d="M4 3h12a1 1 0 0 1 1 1v13l-3-2-2 2-2-2-2 2-2-2-3 2V4a1 1 0 0 1 1-1zm2 4v2h8V7H6zm0 4v2h6v-2H6z"/></svg>,
  motoboy:      <svg viewBox="0 0 20 20"><path d="M5 14a2.5 2.5 0 1 0 0 .01zM15 14a2.5 2.5 0 1 0 0 .01zM11 5h3l3 4v4h-2a3 3 0 0 0-6 0H8a3 3 0 0 0-5.4-1.8L2 9l4-1 2-3h3z"/></svg>,
  expedicao:    <svg viewBox="0 0 20 20"><path d="M10 2 3 5.5v9L10 18l7-3.5v-9L10 2zm0 2.2 4.6 2.3L10 8.8 5.4 6.5 10 4.2zM5 8.1l4 2v5.3l-4-2V8.1zm10 0v5.3l-4 2v-5.3l4-2z"/></svg>,
  links:        <svg viewBox="0 0 20 20"><path d="M8.6 11.4a1 1 0 0 1 0-1.4l2.8-2.8a3 3 0 1 1 4.2 4.2l-1.4 1.4-1.4-1.4 1.4-1.4a1 1 0 1 0-1.4-1.4L10 11.4a1 1 0 0 1-1.4 0zm2.8-2.8a1 1 0 0 1 0 1.4L8.6 12.8a3 3 0 1 1-4.2-4.2l1.4-1.4 1.4 1.4-1.4 1.4a1 1 0 1 0 1.4 1.4L10 8.6a1 1 0 0 1 1.4 0z"/></svg>,
  products:     <svg viewBox="0 0 20 20"><path d="M4 4h5v5H4zM11 4h5v5h-5zM4 11h5v5H4zM11 11h5v5h-5z"/></svg>,
  affiliates:   <svg viewBox="0 0 20 20"><path d="M7 9a3 3 0 1 0-.01-6.01A3 3 0 0 0 7 9zm6 1a2.5 2.5 0 1 0-.01-5.01A2.5 2.5 0 0 0 13 10zM2 17v-1.5C2 13.6 4.2 12 7 12s5 1.6 5 3.5V17H2zm12 0v-1.5c0-1-.4-1.9-1.1-2.6.7-.3 1.4-.4 2.1-.4 2.2 0 4 1.3 4 3V17h-5z"/></svg>,
  wallet:       <svg viewBox="0 0 20 20"><path d="M3 5a2 2 0 0 1 2-2h10v3H5a2 2 0 0 1-2-1zm0 2.5V15a2 2 0 0 0 2 2h12V7.5H3zM14 12a1.2 1.2 0 1 1 0 .01z"/></svg>,
  webhooks:     <svg viewBox="0 0 20 20"><path d="M10 3a3 3 0 0 1 2.6 4.5l-2 3.4-1.7-1 2-3.4A1 1 0 1 0 9 5.4L7.3 4.6A3 3 0 0 1 10 3zM4.6 17a3 3 0 0 1-1.3-5.7l3.6-1.7.8 1.8-3.5 1.6a1 1 0 1 0 1 1.7l1.7 1A3 3 0 0 1 4.6 17zm10.8 0a3 3 0 0 1-2.9-2.2H8.4v-2h4.1a3 3 0 1 1 2.9 4.2z"/></svg>,
  integrations: <svg viewBox="0 0 20 20"><path d="M8 3h4v3.1a4 4 0 0 1 0 7.8V17H8v-3.1a4 4 0 0 1 0-7.8V3zm2 5a2 2 0 1 0 0 4 2 2 0 0 0 0-4z"/></svg>,
  freight:      <svg viewBox="0 0 20 20"><path d="M2 5h10v8H2zM12 8h3l3 3v2h-6V8zM5.5 17a1.8 1.8 0 1 1 0-.01zM14.5 17a1.8 1.8 0 1 1 0-.01z"/></svg>,
  labels:       <svg viewBox="0 0 20 20"><path d="M3 4a1 1 0 0 1 1-1h7.6l5.4 5.4V16a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4zm2 1v10h10V10h-4V5H5zm8 .4V8h2.6L13 5.4z"/></svg>,
  settings:     <svg viewBox="0 0 20 20"><path d="m11.4 2 .5 2.1c.5.2 1 .4 1.4.7l2-.9 1.4 2.4-1.6 1.5c.1.5.1 1 0 1.4l1.6 1.5-1.4 2.4-2-.9c-.4.3-.9.5-1.4.7l-.5 2.1H8.6l-.5-2.1a6 6 0 0 1-1.4-.7l-2 .9-1.4-2.4 1.6-1.5a6 6 0 0 1 0-1.4L3.3 6.3 4.7 4l2 .9c.4-.3.9-.5 1.4-.7L8.6 2h2.8zM10 7.5A2.5 2.5 0 1 0 10 12.5 2.5 2.5 0 0 0 10 7.5z"/></svg>,
  localidades:  <svg viewBox="0 0 20 20"><path d="M10 2a5 5 0 0 0-5 5c0 3.5 5 11 5 11s5-7.5 5-11a5 5 0 0 0-5-5zm0 7a2 2 0 1 1 0-4 2 2 0 0 1 0 4z"/></svg>,
  vitrine:      <svg viewBox="0 0 20 20"><path d="M2 3h16v2.5l-1 1H3l-1-1V3zm1 4h14v10H3V7zm3 2v2h8V9H6zm0 4v2h5v-2H6z"/></svg>,
  users:        <svg viewBox="0 0 20 20"><path d="M7 9a3 3 0 1 0-.01-6.01A3 3 0 0 0 7 9zm6 1a2.5 2.5 0 1 0-.01-5.01A2.5 2.5 0 0 0 13 10zM2 17v-1.5C2 13.6 4.2 12 7 12s5 1.6 5 3.5V17H2zm12 0v-1.5c0-1-.4-1.9-1.1-2.6.7-.3 1.4-.4 2.1-.4 2.2 0 4 1.3 4 3V17h-5z"/></svg>,
  support:      <svg viewBox="0 0 20 20"><path d="M10 2a7 7 0 0 0-7 7v4a2 2 0 0 0 2 2h2V9H5a5 5 0 0 1 10 0h-2v6h2a2 2 0 0 0 2-2V9a7 7 0 0 0-7-7zm-1 14h2v2H9z"/></svg>,
}

// ── Mapa de seções (key → [rota, rótulo, ícone]). Espelha dashboard-v2.php. ────
const SECTION_META: Record<string, { label: string; icon: keyof typeof ICONS }> = {
  dashboard:    { label: 'Dashboard',        icon: 'dashboard' },
  orders:       { label: 'Pedidos',          icon: 'orders' },
  motoboy:      { label: 'Cash On Delivery', icon: 'motoboy' },
  'motoboys-dia': { label: 'Motoboys — dia', icon: 'motoboy' },
  expedicao:    { label: 'Expedição',        icon: 'expedicao' },
  products:     { label: 'Produtos',         icon: 'products' },
  vitrine:      { label: 'Vitrine',          icon: 'vitrine' },
  affiliates:   { label: 'Afiliação',        icon: 'affiliates' },
  links:        { label: 'Meus links',       icon: 'links' },
  wallet:       { label: 'Carteira',         icon: 'wallet' },
  saques:       { label: 'Saques',            icon: 'wallet' },
  webhooks:     { label: 'Webhooks',         icon: 'webhooks' },
  integrations: { label: 'Integrações',      icon: 'integrations' },
  freight:      { label: 'Frete',            icon: 'freight' },
  localidades:  { label: 'Localidades',      icon: 'localidades' },
  users:        { label: 'Usuários',         icon: 'users' },
  support:      { label: 'Suporte',          icon: 'support' },
  settings:     { label: 'Perfil',           icon: 'settings' },
  stock:        { label: 'Estoque',          icon: 'products' },
  reports:      { label: 'Relatórios',       icon: 'dashboard' },
  'wallet-expedition': { label: 'Recarga de frete', icon: 'wallet' },
  'label-credits': { label: 'Créditos de Etiquetas', icon: 'labels' },
}

type NavItem = { to: string; key: string; label: string; icon: JSX.Element }
type NavGroup = { kicker: string; items: NavItem[] }

// SCOPING POR PAPEL (UX-AUDIT §3.1 — tema #1). Três personas, três menus:
// produtor (default), afiliado, OL/operator. Cada papel renderiza SÓ o que age.
// O grupo 'Plataforma' é "config-once" (tocado uma vez) → vem colapsado por padrão
// (CONFIG_ONCE_GROUP), não compete com o trabalho diário.
const CONFIG_ONCE_GROUP = 'Plataforma'

// hasExpedicao = produtor HABILITADO (settings.expedicao_ativa, admin liga em
// Produtores). Gateia o menu adicional "Recarga de frete" (Carteira de Expedição)
// — só quem usa Fulfillment vê.
function buildNavGroups(role: string, hasExpedicao: boolean, expedicaoAtiva: boolean, vitrineHasProducts: boolean, freteTravado: boolean): NavGroup[] {
  const item = (key: string, labelOverride?: string): NavItem => ({
    to: '/' + key,
    key,
    label: labelOverride ?? SECTION_META[key].label,
    icon: ICONS[SECTION_META[key].icon],
  })
  // AUDIT-2026-07-11: filtra 'vitrine' de qualquer grupo quando não há produto
  // visível na vitrine em TODA a plataforma — pedido do dono ("oculta a todos do
  // site, visível apenas para admin"). Aplicado no fim de cada branch de role.
  const dropVitrine = (groups: NavGroup[]): NavGroup[] =>
    vitrineHasProducts ? groups : groups.map(g => ({ ...g, items: g.items.filter(it => it.key !== 'vitrine') }))

  const isOperator = role === 'operator' || role === 'operador'
  // FALLBACK LEAST-PRIVILEGE (signup→cliente): a nav é COSMÉTICA; o gate real é o
  // backend (role 'cliente' está nos gates de afiliado, e os de produtor são allowlist
  // → cliente cai fora = 403). Aqui o catch-all NÃO pode mais ser PRODUTOR: produtor e
  // operator são gateados EXPLICITAMENTE, e tudo o que não for um deles (afiliado,
  // cliente, role desconhecido/vazio) cai no menu do AFILIADO/CLIENTE — o subconjunto
  // de menor privilégio (COD + vitrine; sem produtor/fulfillment/products/expedicao).
  const isProducer = role === 'produtor' || role === 'producer'

  if (isOperator) {
    // OL / OPERADOR (UX-AUDIT §2.4 — persona mais poluída): SEM grupo 'Vendas'
    // (Produtos/Vitrine/Afiliação) e SEM 'Plataforma' (Webhooks/Integrações/Frete).
    // OL vê: Dashboard, Pedidos (tela unificada), Motoboys-dia, Expedição,
    // Carteira (repasse/custódia) + Configurações. Expedição é sempre exibida
    // (a rota existe em App.tsx); o gating por shipping_class foi removido porque
    // /portal/me não expõe esse campo — o gate ficava sempre falso e escondia a tela.
    // NAV UNIFICADO: 'Pedidos' (orders) é a tela canônica role-aware (lista
    // /portal/motoboy p/ operator). O antigo item 'motoboy' (Cash On Delivery) saiu
    // do menu — a rota /motoboy aponta p/ a mesma tela (deep-link em App.tsx).
    // 'motoboys-dia' (tela antiga, pré-unificação) SAI do menu — dono reportou
    // operador usando essa tela achando que era o painel de pedidos oficial,
    // vendo dado desatualizado. Rota permanece em App.tsx (deep-link legado).
    // AUDIT-2026-07-10: reestruturação Operacional/Estratégico é só produtor/afiliado/
    // cliente por ora (pedido do dono) — operador FICA como estava.
    const pedidos = [item('expedicao')]
    return [
      { kicker: '', items: [item('dashboard'), item('orders')] },
      { kicker: 'Pedidos', items: pedidos },
      { kicker: 'Financeiro', items: [item('wallet'), item('saques')] },
      // 'support' (Suporte) SAI do menu (#38) — rota mantida em App.tsx (deep-link).
      { kicker: 'Conta', items: [item('settings')] },
    ]
  }

  // PRODUTOR (gateado EXPLICITAMENTE — não é mais o catch-all) — UX-AUDIT §2.2:
  //   '' => [dashboard]; Pedidos => [orders, expedicao];
  //   Vendas => [products, vitrine, affiliates]; Financeiro => [wallet];
  //   Plataforma (config-once, colapsado) => [webhooks, integrations, freight, localidades, users, support, settings]
  // NAV UNIFICADO: 'Pedidos' (orders) é a tela canônica role-aware. Para o produtor a
  // lista vem de /portal/motoboy (COD) com reagendar/cancelar/clone do dono; o antigo
  // item 'motoboy' (Cash On Delivery) foi REMOVIDO do menu — a rota /motoboy aponta
  // p/ a MESMA tela (deep-link em App.tsx), então não vira link morto.
  // REMOVIDOS do menu (rota mantida em App.tsx p/ deep-link):
  //   - 'reports'         → é a Dashboard duas vezes (mesmo /portal/reports).
  //   - 'wallet-expedition' → já é aba dentro de Carteira (Wallet embute WalletExpedition).
  //   - 'stock'           → estoque é integrado ao produto, não menu separado (linkado do produto).
  // Expedição é sempre exibida (rota existe em App.tsx); o gating por shipping_class
  // foi removido — /portal/me não expõe esse campo, então o gate ficava sempre falso.
  // motoboys-dia é OL-only — NÃO aparece p/ produtor nem afiliado (pedido do dono).
  if (isProducer) {
    // OPERACIONAL (1ª seção, pedido do dono 2026-07-10): Dashboard, Cash on Delivery
    // (renomeado — mesma tela/rota /orders), Expedição. Expedição só aparece se o
    // produtor tem expedição ATIVA (settings.expedicao_ativa, admin liga/desliga em
    // Produtores). Desativada → some do menu.
    // "Expedição" não é mais item de menu separado (o produtor tinha que clicar
    // manualmente nele pra ver os pedidos de frete) — agora é aba dentro da própria
    // tela "Cash on Delivery" (OrdersHub), aparece sozinha quando expedicaoAtiva.
    const operacional = [item('dashboard'), item('orders')]
    return dropVitrine([
      { kicker: 'Operacional', items: operacional },
      // ESTRATÉGICO (2ª seção, pedido do dono 2026-07-10): Vitrine, Produtos, Afiliação
      // — nessa ordem exata.
      { kicker: 'Estratégico', items: [item('vitrine'), item('products'), item('affiliates')] },
      {
        // "Recarga de frete" (Carteira de Expedição) = menu ADICIONAL, só p/ produtor
        // HABILITADO (expedição ativa). Produtor só-COD vê apenas a Carteira.
        kicker: 'Financeiro',
        // Financeiro virou hub único em /wallet: as rotas antigas continuam
        // vivas para deep-link, mas o menu mostra só a entrada canônica.
        items: [item('wallet')],
      },
      {
        // 'support' (Suporte) removido do menu (#38) — rota mantida em App.tsx (deep-link).
        // 'Integrações' e 'Frete' (freight) são menus de EXPEDIÇÃO → só aparecem com
        // expedição ATIVA (mesmo gate de Expedição/Recarga de frete). Produtor só-COD
        // vê apenas Webhooks (que tem o tipo COD) + Localidades/Usuários/Perfil.
        // 'Frete' some quando o admin travou o frete (fixo por transportadora e/ou
        // bloqueio de Correios em Produtores) — pedido dono 2026-07-27: favoritas/
        // bloqueadas escolhidas pelo produtor aqui ficam SEM EFEITO nesse caso
        // (applyFixedFreight/applyCorreiosLock rodam DEPOIS e sobrescrevem), então
        // deixar a tela visível só confunde.
        kicker: 'Plataforma',
        items: [
          item('webhooks'),
          ...(expedicaoAtiva ? [item('integrations'), ...(freteTravado ? [] : [item('freight')])] : []),
          item('localidades'), item('users'), item('settings'),
        ],
      },
    ])
  }

  // AFILIADO / CLIENTE (DEFAULT = MENOR PRIVILÉGIO) — UX-AUDIT §2.3.
  // Catch-all SEGURO: afiliado, cliente (signup) e qualquer role desconhecido/vazio
  // caem aqui. Menu enxuto eixo COMISSÃO/COD — subconjunto do afiliado: NUNCA mostra
  // produtor/fulfillment/products/expedicao/webhooks (esses são gateados no ramo produtor).
  // 3 telas redundantes (Vitrine × Afiliação × Meus links) consolidadas: Vitrine =
  // descobrir/afiliar; Afiliação = "Minha afiliação & links" (já lista os links de
  // venda). 'Meus links' (links) SAI do menu — a rota fica em App.tsx (deep-link).
  // OPERACIONAL / AFILIAÇÃO — mesmo padrão do produtor (pedido do dono 2026-07-10):
  // Dashboard, Cash on Delivery, Expedição (só se hasExpedicao/expedicaoAtiva — pro
  // afiliado/cliente isso é sempre falso hoje, então Expedição fica oculta), depois
  // Vitrine + Afiliação nessa ordem.
  // "Expedição" some do menu do afiliado/cliente igual ao produtor (vira aba dentro
  // de "Cash on Delivery" quando expedicaoAtiva — hoje sempre falso pra esse papel).
  const operacional = [item('dashboard'), item('orders')]
  return dropVitrine([
    { kicker: 'Operacional', items: operacional },
    // ESTRATÉGICO: Vitrine, Afiliação (sem Produtos — afiliado/cliente não tem essa rota).
    { kicker: 'Estratégico', items: [item('vitrine'), item('affiliates')] },
    { kicker: 'Financeiro', items: [item('wallet')] },
    // 'support' (Suporte) SAI do menu (#38) — já existe como submenu em outro lugar.
    // A rota permanece em App.tsx (deep-link), sem virar link morto.
    { kicker: 'Plataforma', items: [item('webhooks'), item('localidades'), item('settings')] },
  ])
}

export type PortalMe = {
  id: number
  nome: string
  email: string
  role: string
  plano?: string
  shipping_class_id?: number | string | null
  // settings (jsonb do produtor) — /portal/me devolve o objeto inteiro. expedicao_ativa
  // (boolean, default true) gateia o item de nav "Expedição"; o admin desliga em Produtores.
  settings?: ({ expedicao_ativa?: boolean | string } & Record<string, unknown>) | null
  // vitrine_has_products: EXISTS site-wide (qualquer produtor) de produto
  // vitrine_visible=true e aprovado. false → item "Vitrine" some do menu de
  // TODOS os papéis do portal (produtor/afiliado/cliente). Admin não passa por
  // aqui (admin-ui é outro app, gerencia produto direto).
  vitrine_has_products?: boolean
}

// PAGE_TITLES — título da ABA por rota (pedido do dono: nada de "FALK LOG" padrão em
// tudo). Formato final: "<Página> · FALK". Fallback "Painel" p/ rota não mapeada.
const PAGE_TITLES: Record<string, string> = {
  '/dashboard': 'Visão geral',
  '/orders': 'Pedidos',
  '/motoboy': 'Pedidos',
  '/motoboys-dia': 'Motoboys do dia',
  '/expedicao': 'Expedição',
  '/products': 'Produtos',
  '/stock': 'Estoque',
  '/vitrine': 'Vitrine',
  '/affiliates': 'Afiliação',
  '/links': 'Checkouts & Links',
  '/wallet': 'Carteira',
  '/wallet-expedition': 'Recarga de frete',
  '/label-credits': 'Créditos de Etiquetas',
  '/webhooks': 'Conexões',
  '/reports': 'Relatórios',
  '/integrations': 'Integrações',
  '/freight': 'Frete',
  '/localidades': 'Áreas de operação',
  '/users': 'Usuários',
  '/settings': 'Perfil',
  '/support': 'Suporte',
}

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const showToast = useToast()

  // Título da aba por página (browser tab). Roda a cada navegação SPA.
  useEffect(() => {
    const page = PAGE_TITLES[location.pathname] || 'Painel'
    document.title = `${page} · FALK`
  }, [location.pathname])
  // FOUC-FIX: theme/sidebar nascem DA localStorage no PRIMEIRO render (lazy init),
  // não em useEffect. Antes inicializavam no default ('light'/'open') e eram
  // sobrescritos depois do paint → flash claro→escuro (tema) + layout shift
  // (sidebar open→collapsed). Lazy init = primeiro render já correto, sem piscada.
  const [sidebar, setSidebar] = useState<'open' | 'collapsed'>(
    () => (typeof localStorage !== 'undefined' && (localStorage.getItem('szV2Sidebar') as 'open' | 'collapsed' | null)) || 'open',
  )
  const [mobileDrawer, setMobileDrawer] = useState(false)
  const [theme, setTheme] = useState<'light' | 'dark'>(
    () => (typeof localStorage !== 'undefined' && (localStorage.getItem('szV2Theme') as 'light' | 'dark' | null)) || 'light',
  )
  const [me, setMe] = useState<PortalMe | null>(null)
  // GANHO CREDITADO (vitalício) p/ a barra de gamificação (rumo a 1MM) na sidebar.
  // FONTE: GET /portal/earnings-credited → { creditado } = SÓ o que foi EFETIVAMENTE
  // creditado ao usuário (não faturamento bruto). O backend (wallet.go EarningsCredited)
  // ramifica por papel: afiliado/cliente = comissão LÍQUIDA creditada (commission
  // approved/paid − penalty, exclui pending/cancelled/saque); produtor = COD recebido
  // na carteira (cod_received available/paid); operador/demais = 0. Vitalício e exato
  // (sem cap nem janela) — substitui o antigo cod.receita+exp.receita de /portal/reports
  // (Σ valor dos pedidos), que media receita GERADA, não o que caiu na conta.
  const [faturamento, setFaturamento] = useState(0)
  const [faturamentoLoading, setFaturamentoLoading] = useState(true)
  // AUDIT-2026-07-30 (dono): "coloca o saldo da carteira do produtor pra
  // aparecer na sidebar abaixo do ranking (carteira de expedição)" — saldo
  // DISPONÍVEL (max(0, saldo-reservado), mesmo cálculo da tela Carteira
  // Expedição) exibido logo abaixo da barra de gamificação.
  const [saldoExpedicao, setSaldoExpedicao] = useState<number | null>(null)

  // /portal/me devolve o role cru do banco (produtor|afiliado|operator|cliente).
  // FALLBACK LEAST-PRIVILEGE: ausente/desconhecido → 'cliente' (menor privilégio),
  // NUNCA produtor. O valor do banco é 'cliente' (PT-BR) — não 'client'.
  const role = (me?.role || 'cliente').toLowerCase()
  const isProducer = role === 'produtor' || role === 'producer'
  // Cliente (signup) é tratado como afiliado na UI (subconjunto COD + vitrine). Não há
  // flag isClient dedicada: o badge cai em 'Cliente' por DEFAULT (qualquer role que não
  // seja operator/produtor/afiliado) e a nav usa o ramo least-privilege em buildNavGroups.
  // hasExpedicao/expedicaoAtiva: fonte ÚNICA = settings.expedicao_ativa (toggle do
  // admin em Produtores). AUDIT-2026-07-24: me.shipping_class_id era um fallback
  // legado (Wallet antigo, WP) que o toggle do admin nunca preenche — produtor
  // ficava com expedição "ativada" no admin mas o menu (Frete/Integrações)
  // continuava escondido no portal. FALK não depende mais desse legado — dropado.
  // AUDIT-2026-07-11 (mantido): opt-IN, nasce DESATIVADA por padrão — exige
  // isProducer E flag EXPLICITAMENTE true (nunca vaza pro menu de afiliado/cliente).
  const expFlag = me?.settings?.expedicao_ativa
  const expFlagOn = expFlag === true || expFlag === 'true'
  const hasExpedicao = isProducer && expFlagOn
  const expedicaoAtiva = hasExpedicao
  // Default TRUE enquanto `me` não carrega (evita flash de "Vitrine sumiu" antes da
  // 1ª resposta de /portal/me); vira false só quando o backend confirma que não há
  // nada visível na vitrine em toda a plataforma.
  const vitrineHasProducts = me?.vitrine_has_products !== false
  // freteTravado: admin fixou preço por transportadora e/ou bloqueou Correios
  // (Produtores → Frete fixo/Bloquear Correios). Ver comentário no item 'freight' acima.
  const settingsAny = me?.settings as Record<string, unknown> | null | undefined
  const freteTravado =
    settingsAny?.frete_fixo_correios != null ||
    settingsAny?.frete_fixo_outras != null ||
    settingsAny?.bloqueio_correios === true ||
    settingsAny?.bloqueio_correios === 'true'
  const navGroups = buildNavGroups(role, hasExpedicao, expedicaoAtiva, vitrineHasProducts, freteTravado)

  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({})

  // theme/sidebar já vêm da localStorage no lazy-init (acima) — NÃO reler aqui
  // (era a causa do flash). Este efeito só busca dados de rede no mount.
  useEffect(() => {
    if (getToken()) {
      api<PortalMe>('/portal/me').then(r => setMe(r)).catch(() => {})
      // Ganho CREDITADO (vitalício, por papel) p/ a barra rumo a 1MM — só o que foi
      // efetivamente creditado (comissão líquida do afiliado / COD recebido do produtor).
      api<{ creditado?: number }>('/portal/earnings-credited')
        .then(r => {
          setFaturamento(Number(r?.creditado) || 0)
        })
        .catch(() => {})
        .finally(() => setFaturamentoLoading(false))
    } else {
      setFaturamentoLoading(false)
    }
  }, [])

  // Saldo da carteira de expedição (sidebar) — só busca quando o produtor tem
  // expedição ativa (hasExpedicao só fica certo DEPOIS de /portal/me chegar).
  useEffect(() => {
    if (!hasExpedicao || !getToken()) return
    api<{ data?: { available?: number } }>('/portal/wallet-expedition/summary')
      .then(r => setSaldoExpedicao(Number(r?.data?.available) || 0))
      .catch(() => {})
  }, [hasExpedicao])

  // Grupos abertos por padrão quando a nav muda (role resolvida) — EXCETO o grupo
  // config-once 'Plataforma' (UX-AUDIT §2.2/2.3), que nasce colapsado p/ não competir
  // com o trabalho diário. O usuário expande quando precisa; a escolha persiste no estado.
  useEffect(() => {
    setOpenGroups(prev => {
      const next = { ...prev }
      navGroups.forEach(g => {
        if (g.kicker && !(g.kicker in next)) next[g.kicker] = g.kicker !== CONFIG_ONCE_GROUP
      })
      return next
    })
  }, [role]) // eslint-disable-line react-hooks/exhaustive-deps

  const stableNavigate = useCallback((path: string) => navigate(path), [navigate])
  useEffect(() => {
    registerApi401Handlers(stableNavigate, showToast)
  }, [stableNavigate, showToast])

  // ── Resgate de convite de afiliado ─────────────────────────────────────────
  // O token foi capturado em main.tsx (antes do redirect de rota engolir a query)
  // e guardado em sessionStorage. Aqui, no primeiro carregamento autenticado,
  // oferecemos resgatar. Removemos a chave em sucesso OU erro p/ não repetir.
  useEffect(() => {
    const token = (() => {
      try { return sessionStorage.getItem('sz_pending_convite') } catch { return null }
    })()
    if (!token) return
    // Consome a chave imediatamente: evita re-disparo em re-render / StrictMode.
    try { sessionStorage.removeItem('sz_pending_convite') } catch { /* noop */ }

    let cancelled = false
    ;(async () => {
      const aceitar = await confirmAsync({
        title: 'Convite de afiliado',
        message:
          'Você recebeu um convite para se tornar afiliado deste produtor. Deseja resgatar e ativar sua afiliação agora?',
        confirmLabel: 'Resgatar convite',
        cancelLabel: 'Agora não',
      })
      if (cancelled || !aceitar) return
      try {
        await api('/portal/affiliates/invites/redeem', {
          method: 'POST',
          body: JSON.stringify({ token }),
        })
        showToast('ok', 'Afiliação realizada com sucesso!')
      } catch (e: any) {
        // Mensagem PT-BR vinda do backend (convite inválido/expirado/já usado,
        // não pode usar o próprio convite etc.) — api() já extrai body.erro.
        showToast('err', e?.message || 'Não foi possível resgatar o convite.')
      }
    })()
    return () => { cancelled = true }
  }, [showToast])

  useEffect(() => { setMobileDrawer(false) }, [location.pathname])

  function toggleGroup(kicker: string) {
    setOpenGroups(prev => ({ ...prev, [kicker]: !prev[kicker] }))
  }

  function toggleSidebar() {
    const next = sidebar === 'open' ? 'collapsed' : 'open'
    setSidebar(next)
    localStorage.setItem('szV2Sidebar', next)
  }

  function toggleTheme() {
    const next = theme === 'light' ? 'dark' : 'light'
    setTheme(next)
    localStorage.setItem('szV2Theme', next)
  }

  // LOGIN-UNICO — "Sair" limpa o token local e leva à página única de login na RAIZ
  // (/), fora do basename /portal. window.location.assign (não navigate) porque o
  // login não vive mais dentro do SPA do portal. Best-effort: invalida a sessão no
  // backend antes de sair (não bloqueia o redirect se falhar).
  function logout() {
    api('/portal/logout', { method: 'POST' }).catch(() => {})
    clearToken()
    window.location.assign('/')
  }

  const firstName = (me?.nome || '').trim().split(' ')[0] || 'Bem-vindo'
  // O nome da página NÃO é mais exibido na topbar branca (#36): cada página já
  // renderiza seu próprio heading (h2.szv2-page-title) logo abaixo. Manter o título
  // também na topbar causava duplicata + flicker percebido (topbar ↔ heading da página).

  // AUDIT-2026-07-11: gate de "produto em análise" mudou de global (bloqueava o
  // painel inteiro) para ESCOPADO — só o painel do produto pendente, dentro da
  // seção Produtos (ver Products.tsx). Navegação normal no resto do site.

  return (
    <div className="sz-root sz-dashboard-v2" data-theme={theme} data-sidebar={sidebar} data-role={role}>
      {mobileDrawer && (
        <div className="szv2-mobile-backdrop" onClick={() => setMobileDrawer(false)} aria-hidden="true" />
      )}

      {/* ── Sidebar ─────────────────────────────────────────── */}
      <aside className="szv2-sidebar" aria-label="Navegação principal" data-mobile-open={mobileDrawer ? '1' : '0'}>
        <div className="szv2-sidebar-head">
          {/* Logo LIMPA do site — FalkLogo já resolve tema (dark/light) E normaliza
              o tamanho VISÍVEL da arte entre os dois PNGs (o dark tem margem vazia
              grande, o light é justo — renderizar na mesma altura de <img> fazia a
              arte do dark parecer menor; bug reportado 2026-07-14). */}
          <span className="szv2-logo-full">
            <FalkLogo variant="full" size={44} />
          </span>
          <span className="szv2-logo-mark" aria-hidden="true">
            <img
              src={`${import.meta.env.BASE_URL}falk-falcon-dark.png`}
              alt="FALK"
              style={{ height: 32, width: 32, objectFit: 'contain', display: 'block' }}
            />
          </span>
        </div>

        {/* Saudação + barra de gamificação (rumo a R$ 1MM) no TOPO da sidebar, acima
            do menu — pedido do dono 2026-06-25 (voltou do topbar pra cá). Some no menu
            colapsado e no mobile fechado (regras .szv2-sidebar-hello já existentes). */}
        {/* Só monta a saudação/progresso quando `me` (nome) e faturamento JÁ chegaram —
            evita o flash "Bem-vindo"/"—" → nome/valor real (pedido do dono: sem piscar,
            carrega direto o valor atual). */}
        {me && !faturamentoLoading && (
          <div className="szv2-sidebar-hello">
            <strong>Olá, {firstName} 👋</strong>
            <ProgressTier value={faturamento} loading={false} />
            {/* AUDIT-2026-07-30 (dono): saldo da carteira de expedição, abaixo
                do ranking/gamificação — só pra produtor com expedição ativa. */}
            {hasExpedicao && saldoExpedicao !== null && (
              <div className="szv2-sidebar-saldo-exp">
                <span className="szv2-sidebar-saldo-exp-label">Carteira Expedição</span>
                <span className="szv2-sidebar-saldo-exp-valor">
                  R$ {saldoExpedicao.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                </span>
              </div>
            )}
          </div>
        )}

        <nav className="szv2-nav">
          {/* Grupos SEMPRE expandidos (pedido do dono) — sem toggle/chevron. O kicker
              é só um rótulo; todos os itens renderizam sempre.
              AUDIT-2026-07-11: só monta DEPOIS que `me` carrega — antes disso role cai
              no fallback 'cliente' (menor privilégio) e o menu monta com o subconjunto
              errado (ex.: "Produtos" ausente), depois pisca pro menu certo assim que
              /portal/me responde. Espera `me` = zero flicker de item entrando/saindo. */}
          {me && navGroups.map((g, gi) => (
            <div key={gi} className="szv2-nav-group" data-open="1">
              {g.kicker && <div className="szv2-nav-kicker">{g.kicker}</div>}
              {g.items.map(it => (
                <NavLink
                  key={it.to}
                  to={it.to}
                  end={it.to === '/dashboard'}
                  title={it.label}
                  className={({ isActive }) => 'sz-ni' + (isActive ? ' sz-ni-on' : '')}
                >
                  <span className="szv2-ni-icon" aria-hidden="true">{it.icon}</span>
                  <span className="szv2-ni-label">{it.label}</span>
                </NavLink>
              ))}
            </div>
          ))}
        </nav>

        <div className="szv2-sidebar-foot">
          <button type="button" className="szv2-sidebar-foot-btn" onClick={toggleTheme} title="Alternar tema">
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3a7 7 0 0 0 0 14c1.2 0 2.4-.3 3.4-.9A7 7 0 0 1 10 3z"/></svg>
            <span>Tema</span>
          </button>
          <button type="button" className="szv2-sidebar-foot-btn" onClick={logout} title="Sair da conta">
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M8 3h5a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H8v-2h5V5H8V3zm1 5 3 2-3 2v-1.2H3V9.2h6V8z"/></svg>
            <span>Sair</span>
          </button>
        </div>
      </aside>

      {/* ── Main ────────────────────────────────────────────── */}
      <div className="szv2-main">
        <header className="szv2-topbar">
          <button
            type="button"
            className="szv2-topbar-toggle"
            onClick={() => {
              if (window.innerWidth < 768) setMobileDrawer(d => !d)
              else toggleSidebar()
            }}
            aria-label="Abrir ou recolher menu"
          >
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 5h14v2H3zM3 9h14v2H3zM3 13h14v2H3z"/></svg>
          </button>
          {/* Saudação + barra de gamificação foram pro TOPO da sidebar (pedido do dono
              2026-06-25). Badge de papel + avatar removidos da topbar (pedido do dono). */}
          <div className="szv2-topbar-spacer" />
        </header>

        <main className="szv2-content" id="szv2-content">
          {/* AUDIT-2026-07-11: só monta a página (Outlet) DEPOIS que `me` carrega —
              mesmo gate do nav da sidebar. Antes, o conteúdo central renderizava na
              hora (cada página busca seus próprios dados) enquanto a sidebar ainda
              esperava /portal/me, dando sensação de carregamento desencontrado/
              "esquisito". Agora os dois aparecem juntos. */}
          {me ? (
            <>
              <Outlet context={{ me }} />
              {/* Canal do Encarregado de Dados (DPO) — LGPD Art. 41. */}
              <DpoFooter variant="app" />
            </>
          ) : (
            <div style={{ minHeight: '60vh' }}>
              <SectionLoading label="Carregando…" />
            </div>
          )}
        </main>
      </div>

      <ToastHost />
      <ConfirmHost />
      {/* Host do ConfirmDialog da tela admin-orders/ (módulo separado) — necessário
          p/ os confirmar de reagendar/cancelar/clone da tela de Pedidos. */}
      <AdminOrdersConfirmHost />
    </div>
  )
}
