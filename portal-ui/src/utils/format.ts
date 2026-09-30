// Formatadores canônicos BR — usar em TODAS as telas (evita cada página reimplementar `fmt`).
// AUDIT-2026-06-18: moeda R$ + datas d/m/Y consistentes. null/undefined → "—".

const _brl = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' })
const _num = new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 0, maximumFractionDigits: 0 })
const _num2 = new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

/** "R$ 1.234,56". Aceita number | string numérica | null. */
export function brl(v: number | string | null | undefined): string {
  if (v === null || v === undefined || v === '') return '—'
  const n = typeof v === 'string' ? parseFloat(v) : v
  return Number.isFinite(n) ? _brl.format(n) : '—'
}

/** Número com 2 casas no padrão BR (1.234,56), SEM o "R$". */
export function num2(v: number | string | null | undefined): string {
  if (v === null || v === undefined || v === '') return '—'
  const n = typeof v === 'string' ? parseFloat(v) : v
  return Number.isFinite(n) ? _num2.format(n) : '—'
}

/** Inteiro no padrão BR (1.234). */
export function num(v: number | string | null | undefined): string {
  if (v === null || v === undefined || v === '') return '—'
  const n = typeof v === 'string' ? parseFloat(v) : v
  return Number.isFinite(n) ? _num.format(n) : '—'
}

function _parse(s: string | number | null | undefined): Date | null {
  if (s === null || s === undefined || s === '') return null
  // aceita "2026-06-18", "2026-06-18 14:30:00", ISO com T/Z, ou epoch
  let str = typeof s === 'number' ? new Date(s).toISOString() : String(s).trim()
  if (str.startsWith('0000')) return null
  if (str.includes(' ') && !str.includes('T')) str = str.replace(' ', 'T')
  // BUG-FIX 2026-07-23: timestamptz do Postgres em texto vem como
  // "2026-07-23 15:52:10.450502-03" — offset de 2 dígitos SEM minutos
  // ("-03", não "-03:00"). Isso não é ISO 8601 válido; alguns motores JS
  // (Date nativo) retornam Invalid Date silenciosamente, e a coluna some da
  // tela (mostra "—") mesmo com o dado correto vindo do backend. Normaliza
  // o offset pra "+HH:mm"/"-HH:mm" antes de parsear.
  str = str.replace(/([+-]\d{2})$/, '$1:00')
  const d = new Date(str)
  return Number.isNaN(d.getTime()) ? null : d
}

/** "18/06/2026" */
export function dt(s: string | number | null | undefined): string {
  const d = _parse(s)
  return d ? d.toLocaleDateString('pt-BR', { timeZone: 'America/Sao_Paulo' }) : '—'
}

/** "18/06/2026 14:30" */
export function dtTime(s: string | number | null | undefined): string {
  const d = _parse(s)
  return d
    ? d.toLocaleString('pt-BR', { timeZone: 'America/Sao_Paulo', day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
    : '—'
}

// AUDIT-2026-06-21 #15: neutraliza CSV/Formula Injection. Nome do comprador é texto
// livre; um valor começando com = + - @ TAB ou CR vira fórmula ao abrir no Excel/Sheets
// (=HYPERLINK/=WEBSERVICE → exfiltração). Prefixamos apóstrofo para forçar texto literal.
// Aplicar SEMPRE antes do quote-escape do campo (esc/csvField) nos sinks de export.
const _CSV_FORMULA_RE = /^[=+\-@\t\r]/
/** Prefixa "'" se o valor começa com um gatilho de fórmula (= + - @ TAB CR). */
export function csvSafe(v: unknown): string {
  const s = v === null || v === undefined ? '' : String(v)
  return _CSV_FORMULA_RE.test(s) ? "'" + s : s
}
