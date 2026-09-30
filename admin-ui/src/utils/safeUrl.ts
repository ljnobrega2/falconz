// AUDIT-2026-06-21 #13: safeUrl — allowlist de esquemas para href dinâmico.
// React 18 NÃO bloqueia href="javascript:..." nem "data:..." — não confiar nele.
// Regra: URL sem esquema (relativa) é navegação interna → permitida.
//        URL com esquema só passa se for http/https/tel/mailto, senão vira '#'.
// O navegador remove caracteres de controle/espaço antes de avaliar o esquema
// (ex.: "java\tscript:" e "JavaScript:" disparam), então detectamos sobre uma
// cópia limpa+minúscula e retornamos o ORIGINAL intacto só quando o veredito é seguro.

const SCHEMES_PERMITIDOS = ['http', 'https', 'tel', 'mailto']

export function safeUrl(url: string | null | undefined): string {
  if (!url) return '#'
  const original = String(url)
  // Remove controles (0x00-0x20) e DEL (0x7f) que o browser ignora ao parsear o esquema.
  const limpo = original.replace(/[\x00-\x20\x7f]+/g, '')
  const m = limpo.match(/^([a-z][a-z0-9+.-]*):/i)
  if (!m) return original // sem esquema → relativa (navegação interna), permite
  return SCHEMES_PERMITIDOS.includes(m[1].toLowerCase()) ? original : '#'
}
