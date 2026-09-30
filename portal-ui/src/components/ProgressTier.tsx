// ProgressTier — barra de gamificação 0 → R$ 1.000.000 (rumo a 1MM).
// Vive no cabeçalho da sidebar (substitui a saudação "Olá, {nome}" — o nome já
// está no topbar, não duplicar). Tema FALK: faixas com nomes de voo/falcão, em
// PT-BR. Identidade FALK AZUL (este portal-ui usa --szv2-brand: #1E6FF2, NÃO o
// laranja do portal WP legado).
//
// Preenchimento: GLOBAL e proporcional a value / 1.000.000 (NÃO por faixa). A
// sensação de progresso vem do NOME da faixa; a barra fica baixa cedo de propósito.

// Marcos (R$): tier = maior limiar ≤ valor; próximo marco = próximo limiar acima.
const MILESTONES = [0, 10_000, 100_000, 500_000, 1_000_000] as const

// Faixas temáticas FALK (aves de rapina / níveis de voo logístico). PT-BR, sem cafonice.
//  0–10k        → Primeiro Voo
//  10,1k–99,9k  → Em Rota
//  100k–499,9k  → Altitude de Cruzeiro
//  500k–999,9k  → Falcão de Elite
//  1MM          → Ápice FALK
type Tier = { min: number; name: string }
const TIERS: Tier[] = [
  { min: 0,          name: 'Primeiro Voo' },
  { min: 10_000,     name: 'Em Rota' },
  { min: 100_000,    name: 'Altitude de Cruzeiro' },
  { min: 500_000,    name: 'Falcão de Elite' },
  { min: 1_000_000,  name: 'Ápice FALK' },
]

const GOAL = 1_000_000

// Moeda compacta (R$ 12,5 mil / R$ 1,2 mi) — cabe no trilho estreito da sidebar.
function brlCompact(v: number): string {
  if (v >= 1_000_000) return `R$ ${(v / 1_000_000).toLocaleString('pt-BR', { maximumFractionDigits: 1 })} mi`
  if (v >= 1_000) return `R$ ${(v / 1_000).toLocaleString('pt-BR', { maximumFractionDigits: 1 })} mil`
  return `R$ ${v.toLocaleString('pt-BR', { maximumFractionDigits: 0 })}`
}

function currentTier(v: number): Tier {
  let t: Tier = TIERS[0]
  for (const tier of TIERS) if (v >= tier.min) t = tier
  return t
}

function nextMilestone(v: number): number | null {
  for (const m of MILESTONES) if (v < m) return m
  return null // já atingiu/ultrapassou 1MM — sem próximo marco
}

export default function ProgressTier({ value, loading }: { value: number; loading?: boolean }) {
  const safe = Number.isFinite(value) && value > 0 ? value : 0
  const tier = currentTier(safe)
  const next = nextMilestone(safe)
  // Preenchimento GLOBAL (value / 1MM), nunca > 100%.
  const pct = Math.min(100, (safe / GOAL) * 100)

  return (
    <div className="szv2-tier" aria-label="Progresso de faturamento rumo a 1 milhão">
      <div className="szv2-tier-head">
        <span className="szv2-tier-name">{tier.name}</span>
        <span className="szv2-tier-goal">rumo a R$ 1 mi</span>
      </div>
      <div className="szv2-tier-track" role="progressbar" aria-valuemin={0} aria-valuemax={GOAL} aria-valuenow={Math.round(safe)}>
        <div className="szv2-tier-fill" style={{ width: loading ? '0%' : `${pct}%` }} />
      </div>
      <div className="szv2-tier-foot">
        <span className="szv2-tier-current">{loading ? '—' : brlCompact(safe)}</span>
        {next !== null ? (
          <span className="szv2-tier-next">próx. {brlCompact(next)}</span>
        ) : (
          <span className="szv2-tier-next szv2-tier-next--top">meta atingida 🦅</span>
        )}
      </div>
    </div>
  )
}
