// =============================================================================
// Validacao e mascaras PT-BR (CPF, CEP, telefone) + ViaCEP.
// Validacao basica no front; a verdade financeira e a zona de entrega ("fora de
// area") sao decididas no servidor.
// =============================================================================

const onlyDigits = (s: string): string => (s || '').replace(/\D+/g, '')

// ---------------------------------------------------------------------------
// CPF — FEAT-FRETE (2026-06-18): VOLTA SOMENTE para ofertas tipo correio.
// Os Correios exigem o CPF do destinatario; motoboy continua SEM CPF.
//
// O backend so valida `len(cpf) == 11` — o FRONT e o unico gate real de validade
// (digitos verificadores + rejeicao de CPFs com todos os digitos iguais, que
// passam na aritmetica do modulo mas sao invalidos: 00000000000, 11111111111...).
// ---------------------------------------------------------------------------
export function maskCPF(v: string): string {
  const d = onlyDigits(v).slice(0, 11)
  return d
    .replace(/(\d{3})(\d)/, '$1.$2')
    .replace(/(\d{3})(\d)/, '$1.$2')
    .replace(/(\d{3})(\d{1,2})$/, '$1-$2')
}

export function isValidCPF(v: string): boolean {
  const d = onlyDigits(v)
  if (d.length !== 11) return false
  // Rejeita sequencias de digito unico (000..., 111...) — invalidas apesar do modulo.
  if (/^(\d)\1{10}$/.test(d)) return false

  // Calcula um digito verificador sobre os primeiros `len` digitos.
  const calcDigit = (len: number): number => {
    let sum = 0
    for (let i = 0; i < len; i++) {
      sum += Number(d[i]) * (len + 1 - i)
    }
    const r = (sum * 10) % 11
    return r === 10 ? 0 : r
  }

  return calcDigit(9) === Number(d[9]) && calcDigit(10) === Number(d[10])
}

// ---------------------------------------------------------------------------
// CEP
// ---------------------------------------------------------------------------
export function maskCEP(v: string): string {
  const d = onlyDigits(v).slice(0, 8)
  return d.replace(/(\d{5})(\d)/, '$1-$2')
}

export function isValidCEP(v: string): boolean {
  return onlyDigits(v).length === 8
}

// ---------------------------------------------------------------------------
// Telefone (PT-BR, 10-11 digitos)
// ---------------------------------------------------------------------------
export function maskPhone(v: string): string {
  let d = onlyDigits(v)
  // remove DDI 55 se colado (12-13 digitos comecando com 55)
  if ((d.length === 12 || d.length === 13) && d.startsWith('55')) d = d.slice(2)
  d = d.slice(0, 11)
  if (d.length <= 10) {
    return d
      .replace(/(\d{2})(\d)/, '($1) $2')
      .replace(/(\d{4})(\d)/, '$1-$2')
  }
  return d
    .replace(/(\d{2})(\d)/, '($1) $2')
    .replace(/(\d{5})(\d)/, '$1-$2')
}

export function isValidPhone(v: string): boolean {
  const d = onlyDigits(v)
  return d.length === 10 || d.length === 11
}

// ---------------------------------------------------------------------------
// ViaCEP
// ---------------------------------------------------------------------------
export interface ViaCepResult {
  logradouro: string
  bairro: string
  localidade: string // cidade
  uf: string
}

/**
 * Consulta ViaCEP. Retorna null em CEP inexistente (erro:true) ou falha de rede.
 * NAO decide "fora de area" — isso e verdict do servidor no POST do pedido.
 */
export async function lookupCEP(cep: string): Promise<ViaCepResult | null> {
  const d = onlyDigits(cep)
  if (d.length !== 8) return null
  try {
    const res = await fetch(`https://viacep.com.br/ws/${d}/json/`)
    if (!res.ok) return null
    const data = await res.json()
    if (!data || data.erro) return null
    return {
      logradouro: data.logradouro || '',
      bairro: data.bairro || '',
      localidade: data.localidade || '',
      uf: data.uf || '',
    }
  } catch {
    return null
  }
}

export { onlyDigits }
