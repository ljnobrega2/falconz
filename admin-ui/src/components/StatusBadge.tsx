// StatusBadge — badge de status PADRONIZADO (regra do dono):
//   1. Label SEM underscore e capitalizado: "em_rota" → "Em rota".
//   2. Cada status tem COR ÚNICA — nenhuma igual à de outro status
//      (sinônimos de idioma compartilham cor: pending=pendente, paid=pago…).
//   3. SEM símbolo/bolinha antes do nome (só o texto colorido — usa estilo
//      inline, NÃO a classe global .sz-badge que tem ::before bolinha).
//
// Espelha portal-ui/src/components/StatusBadge.tsx, estendido com o vocabulário
// dos painéis financeiros do admin (saques, carteira, conciliação, PIX).
// Fonte única da verdade de cor/label de status no admin.
import type { CSSProperties } from 'react'

// Paleta de cores DISTINTAS — uma por status (sinônimos de idioma compartilham).
// Mantida explícita para garantir que duas linhas nunca colidam de cor.
const STATUS_COLOR: Record<string, string> = {
  // ── Ciclo de entrega (pedido / motoboy) ──────────────────────
  pending: '#6B7280', // cinza-azulado
  pendente: '#6B7280', // (= pending)
  aguardando: '#94A3B8', // cinza claro
  analise: '#A78BAA', // ardósia-lilás (PIX em análise)
  em_analise: '#A78BAA', // (= analise)
  analysis: '#A78BAA', // (= analise)
  pre_agendado: '#A855F7', // roxo
  agendado: '#2563EB', // azul
  'on-hold': '#CA8A04', // ouro escuro
  em_separacao: '#0E7490', // ciano profundo
  embalado: '#0891B2', // ciano escuro
  em_rota: '#0EA5E9', // azul céu
  a_caminho: '#6366F1', // índigo
  reagendado: '#F59E0B', // âmbar
  entregue: '#16A34A', // verde
  completo: '#15803D', // verde escuro
  confirmado: '#22A06B', // verde-jade (PIX/fechamento confirmado)
  aprovado: '#10B981', // esmeralda
  approved: '#10B981', // (= aprovado)
  a_aprovar: '#CA8A04', // ouro — produto aguardando aprovação do admin
  reprovado: '#B91C1C', // vermelho escuro — produto reprovado (= rejected)
  enviado: '#14B8A6', // teal
  postado: '#0D9488', // teal escuro
  processing: '#8B5CF6', // violeta
  processando: '#7C3AED', // violeta escuro
  devolvido: '#F97316', // laranja
  expirado: '#9CA3AF', // cinza-prata (PIX expirado)
  extravio: '#DC2626', // vermelho
  frustrado: '#EF4444', // vermelho claro
  rejected: '#B91C1C', // vermelho escuro (saque rejeitado)
  rejeitado: '#B91C1C', // (= rejected)
  cancelado: '#78716C', // pedra
  cancelled: '#78716C', // (= cancelado)
  reembolsado: '#A8A29E', // pedra clara
  estornado: '#D6D3D1', // areia
  // ── Financeiro (saques / carteira / conciliação) ─────────────
  disponivel: '#0284C7', // azul-aço (ganho liberado)
  available: '#0284C7', // (= disponivel)
  pago: '#047857', // verde-floresta (pagamento liquidado — ≠ completo)
  paid: '#047857', // (= pago)
  // ── Financeiro pós-entrega (separado do ciclo logístico) ─────
  pagamento_agendado: '#9333EA', // púrpura — distinto de agendado/pre_agendado
  vencido: '#BE123C', // rose forte — distinto de frustrado/extravio
  concluido: '#0F766E', // teal profundo — distinto de entregue/completo
}

const FALLBACK = '#475569'

// normaliza "em_rota" → "Em rota" (troca _ por espaço, capitaliza a 1ª letra).
export function statusLabel(status: string): string {
  if ((status || '').trim().toLowerCase() === 'a_caminho') return 'A Caminho'
  if ((status || '').trim().toLowerCase() === 'pagamento_agendado') return 'Pagamento Agendado'
  if ((status || '').trim().toLowerCase() === 'concluido') return 'Concluído'
  const s = (status || '').trim().replace(/_/g, ' ')
  if (!s) return '—'
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function statusColor(status: string): string {
  return STATUS_COLOR[(status || '').trim().toLowerCase()] || FALLBACK
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
