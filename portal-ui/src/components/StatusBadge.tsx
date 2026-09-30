// StatusBadge — badge de status PADRONIZADO (regra do dono):
//   1. Label SEM underscore e capitalizado: "em_rota" → "Em rota".
//   2. Cada status tem COR ÚNICA — nenhuma igual à de outro status.
//   3. SEM símbolo/bolinha antes do nome (só o texto colorido).
//
// Fonte única da verdade de cor/label de status de PEDIDO do portal. Use em
// qualquer lugar que exiba status de pedido (lista, drawer, relatórios, cards,
// expedição, motoboy). Substitui os mapas locais BADGE_MAP que existiam em cada
// página (Orders/Motoboy/Expedicao/Reports/Dashboard/MotoboysDia).
import type { CSSProperties } from 'react'

// normaliza o slug cru do backend p/ a chave canônica do mapa:
//   - tira espaços e baixa caixa
//   - remove prefixo WooCommerce "wc-" (ex.: "wc-completed" → "completed")
//   - colapsa separadores (-, _, espaço) p/ casar "a_caminho"/"a-caminho"/"acaminho"
function normalize(status: string): string {
  let s = (status || '').trim().toLowerCase()
  if (s.startsWith('wc-')) s = s.slice(3)
  return s.replace(/[-_\s]+/g, '')
}

// Paleta de cores DISTINTAS — uma por status (chave já normalizada SEM separador).
// Mantida explícita para garantir que duas linhas nunca colidam de cor (auditável
// de relance). Marca = azul FALK; nenhum status usa o azul de marca puro.
const STATUS_COLOR: Record<string, string> = {
  // ── genéricos / WooCommerce ──
  pending: '#6B7280', // cinza-azulado
  pendente: '#71717A', // zinco
  aguardando: '#94A3B8', // cinza claro
  onhold: '#D97706', // âmbar escuro (em espera)
  processing: '#8B5CF6', // violeta
  processando: '#7C3AED', // violeta escuro
  emandamento: '#C2410C', // laranja queimado (AUDIT-2026-07-28: aprovando/emitindo etiqueta agora)
  postado: '#0D9488', // teal escuro

  // ── MOTOBOY (COD) — fluxo de entrega própria ──
  preagendado: '#A855F7', // roxo (pré-agendado)
  agendado: '#2563EB', // azul (agendado)
  embalado: '#0891B2', // ciano escuro (embalado)
  // enviado: definido na seção compartilhada abaixo (vale p/ motoboy E expedição)
  coletado: '#0284C7', // azul-aço (coletado)
  completo: '#22C55E', // verde vivo (completo) — legível no dark
  completed: '#22C55E', // (concluído — alias WooCommerce de completo)
  frustrado: '#EF4444', // vermelho claro (frustrado)
  acaminho: '#6366F1', // índigo (a caminho)
  emrota: '#0EA5E9', // azul céu (em rota)

  // ── EXPEDIÇÃO — fluxo de envio externo ──
  aaprovar: '#EAB308', // amarelo-ouro (a aprovar)
  aprovado: '#10B981', // esmeralda (aprovado)
  separado: '#06B6D4', // ciano vivo (separado) — legível no dark
  enviado: '#14B8A6', // teal (enviado) — compartilhado motoboy/expedição
  aretirar: '#F472B6', // rosa (a retirar) — distinto de "Em retirada"
  entregue: '#16A34A', // verde (entregue)
  malsucedido: '#FB7185', // coral/rosa-vermelho (mal sucedido) — distinto dos outros reds
  devolvido: '#F97316', // laranja (devolvido — status, NÃO marca)

  // ── outros status auxiliares ──
  emretirada: '#3B82F6', // azul claro (em retirada — distinto de "a retirar")
  reagendado: '#F59E0B', // âmbar (reagendado)
  avariado: '#B45309', // âmbar queimado (avariado)
  asuspender: '#9CA3AF', // cinza (a suspender)
  extravio: '#DC2626', // vermelho (extravio)
  failed: '#B91C1C', // vermelho escuro (falhou)
  saldoinsuficiente: '#991B1B', // vinho (saldo insuf.)
  emcancelamento: '#A16207', // mostarda (em cancelamento)
  cancelado: '#78716C', // pedra (cancelado)
  cancelled: '#78716C', // (alias WooCommerce de cancelado)
  reembolsado: '#A8A29E', // pedra clara (reembolsado)
  refunded: '#A8A29E', // (alias WooCommerce de reembolsado)
  estornado: '#D6D3D1', // areia (estornado)
  // ── Financeiro pós-entrega (separado do ciclo logístico) ──
  pagamentoagendado: '#9333EA',
  vencido: '#BE123C',
  concluido: '#0F766E',
}

// Rótulos especiais (PT-BR) — quando o ucfirst do slug não basta. As demais
// chaves caem no statusLabel() padrão (ucfirst + underscore→espaço).
const STATUS_LABEL_OVERRIDE: Record<string, string> = {
  // Regra do dono: o status `aguardando` é exibido como "Agendado" no portal
  // (pedido COD aceito e agendado, ainda não embalado/em rota).
  aguardando: 'Agendado',
  onhold: 'Em espera',
  completed: 'Concluído',
  completo: 'Concluído',
  // 'pending' é inglês puro — sem override caía no ucfirst() genérico e
  // aparecia como "Pending" na tela (AUDIT 2026-07-23).
  pending: 'Pendente',
  // AUDIT-2026-07-28: 'processing' é status de EXPEDIÇÃO = "Aprovado" (mesmo
  // rótulo do admin-ui ExpedicaoOrders.tsx STATUS_PT) — "Processando" divergia
  // do admin pro EXATO MESMO status, achado revisando a padronização pedida
  // pelo dono ("padronize status para aparecer iguais para admin e produtor").
  processing: 'Aprovado',
  emandamento: 'Em andamento',
  failed: 'Falhou',
  saldoinsuficiente: 'Saldo insuf.',
  emcancelamento: 'Em cancelamento',
  cancelled: 'Cancelado',
  refunded: 'Reembolsado',
  asuspender: 'A suspender',
  emretirada: 'Em retirada',
  preagendado: 'Pré-agendado',
  acaminho: 'A Caminho',
  pagamentoagendado: 'Pagamento Agendado',
  concluido: 'Concluído',
}

const FALLBACK = '#475569'

// normaliza "em_rota" → "Em rota" (troca _/- por espaço, capitaliza a 1ª letra).
export function statusLabel(status: string): string {
  const key = normalize(status)
  if (STATUS_LABEL_OVERRIDE[key]) return STATUS_LABEL_OVERRIDE[key]
  let s = (status || '').trim()
  if (s.toLowerCase().startsWith('wc-')) s = s.slice(3)
  s = s.replace(/[_-]+/g, ' ').trim()
  if (!s) return '—'
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function statusColor(status: string): string {
  return STATUS_COLOR[normalize(status)] || FALLBACK
}

type Props = { status: string; label?: string; style?: CSSProperties; className?: string }

export default function StatusBadge({ status, label, style, className }: Props) {
  const color = statusColor(status)
  return (
    <span
      className={className}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        // SEM bolinha/símbolo antes do nome — só o texto.
        padding: '3px 11px',
        borderRadius: 999,
        fontSize: 12,
        fontWeight: 700,
        lineHeight: 1.5,
        color,
        background: `${color}1A`, // 10% do tom (cor única por status)
        border: `1px solid ${color}33`,
        whiteSpace: 'nowrap',
        ...style,
      }}
    >
      {label ?? statusLabel(status)}
    </span>
  )
}
