// Helpers de formatação compartilhados entre as telas do admin.

const BRL = new Intl.NumberFormat('pt-BR', {
  style: 'currency',
  currency: 'BRL',
  minimumFractionDigits: 2,
})

// brl — formata um número como moeda brasileira (R$ 1.234,56).
// Aceita number ou string numérica; valores inválidos viram R$ 0,00.
export function brl(v: number | string | null | undefined): string {
  const n = typeof v === 'string' ? parseFloat(v) : v
  if (n == null || Number.isNaN(n)) return BRL.format(0)
  return BRL.format(n)
}

// ymd — data local no formato YYYY-MM-DD (sem conversão de timezone).
// NÃO usar toISOString() aqui — ele converte pra UTC e desloca o dia.
export function ymd(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// brDate — exibe uma data YYYY-MM-DD no formato brasileiro DD/MM/YYYY.
export function brDate(iso: string): string {
  if (!iso) return ''
  const parts = iso.slice(0, 10).split('-')
  if (parts.length !== 3) return iso
  return `${parts[2]}/${parts[1]}/${parts[0]}`
}
