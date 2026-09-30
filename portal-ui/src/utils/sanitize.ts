// Sanitização de HTML rico vindo do PRODUTOR (descrição da vitrine).
//
// CONTEXTO DE SEGURANÇA: a descrição da vitrine é texto livre que o produtor edita
// e que é renderizado para AFILIADOS e para o próprio produtor (Vitrine + Products).
// Renderizar esse HTML cru seria XSS armazenado. Toda a renderização de HTML do campo
// passa OBRIGATORIAMENTE por sanitizeProductHtml() — é o único portão.
//
// Modelo de ameaça coberto:
//   - <script>, on*=, javascript:/data: URIs ............ DOMPurify (defaults pinados)
//   - <iframe>/<object>/<embed>/<form>/<svg>/<math> ..... fora do ALLOWED_TAGS
//   - CSS clickjacking (position:fixed/z-index/transform
//     cobrindo a viewport, fora do container do modal) .. ALLOWLIST de props CSS abaixo
//   - url()/expression()/comentário CSS no style ........ filtro de valor no hook
//   - tabnabbing (target=_blank) ........................ rel=noopener forçado no hook
//
// Resíduo aceito (baixo): <img src=https://...> permite um pixel de tracking/IP-log do
// viewer. Não é XSS; não bloqueia o produto. Documentado de propósito.
import DOMPurify from 'dompurify'

// Tags de layout/texto/mídia que a descrição pode usar. ALLOWLIST estrita: qualquer
// tag fora daqui é removida (mais seguro que uma lista de proibidas).
const ALLOWED_TAGS = [
  'div', 'span', 'p', 'br', 'hr',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'b', 'strong', 'i', 'em', 'u', 's', 'small', 'mark', 'sub', 'sup',
  'ul', 'ol', 'li',
  'a', 'img',
  'table', 'thead', 'tbody', 'tr', 'th', 'td',
  'blockquote', 'figure', 'figcaption',
]

// Atributos permitidos. style/class p/ o card; href/src/alt p/ link e imagem;
// target/rel tratados no hook; colspan/rowspan/width/height p/ tabela/imagem.
const ALLOWED_ATTR = [
  'style', 'class', 'title',
  'href', 'target', 'rel',
  'src', 'alt', 'width', 'height',
  'colspan', 'rowspan',
]

// ── ALLOWLIST de propriedades CSS ─────────────────────────────────────────────
// SÓ estas propriedades sobrevivem no atributo style. Tudo que não está aqui é
// descartado — incluindo position, z-index, transform, top/right/bottom/left,
// inset, filter, clip-path, etc. (vetores de overlay/clickjacking). O card
// DATALAPROX usa apenas propriedades visuais/de layout, então nada é perdido.
const ALLOWED_CSS_PROPS = new Set([
  'color', 'background', 'background-color', 'background-image', 'background-clip',
  '-webkit-background-clip', '-webkit-text-fill-color',
  'border', 'border-top', 'border-right', 'border-bottom', 'border-left',
  'border-width', 'border-style', 'border-color', 'border-radius',
  'border-top-left-radius', 'border-top-right-radius',
  'border-bottom-left-radius', 'border-bottom-right-radius',
  'box-shadow', 'text-shadow', 'outline',
  'margin', 'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
  'padding', 'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
  'width', 'min-width', 'max-width', 'height', 'min-height', 'max-height',
  'box-sizing', 'overflow', 'overflow-x', 'overflow-y',
  'display', 'flex', 'flex-direction', 'flex-wrap', 'flex-grow', 'flex-shrink',
  'flex-basis', 'gap', 'row-gap', 'column-gap', 'justify-content', 'align-items',
  'align-self', 'align-content', 'order',
  'font', 'font-family', 'font-size', 'font-weight', 'font-style', 'font-variant',
  'line-height', 'letter-spacing', 'word-spacing', 'text-align', 'text-transform',
  'text-decoration', 'text-overflow', 'white-space', 'word-break', 'overflow-wrap',
  'list-style', 'list-style-type', 'vertical-align',
  'opacity', 'visibility',
])

// Valores de CSS proibidos (mesmo em propriedade permitida): url()/image-set()
// (tracking/SSRF + dados externos), expression() (IE), comentário /* */ (bypass),
// e qualquer protocolo de script. Declaração inteira cai se bater.
const FORBIDDEN_CSS_VALUE = /url\s*\(|image-set\s*\(|expression\s*\(|javascript:|vbscript:|\/\*|\*\/|@import|behavior\s*:|-moz-binding/i

// Bypass por ESCAPE CSS: `\75 rl(...)` → `url(...)` no parser do browser, e o regex
// literal acima nunca casa. Defesa: rejeitar QUALQUER backslash ou caractere de
// controle no valor — cards legítimos (cor/px/%/gradient) nunca os usam. Fecha a
// classe inteira de escapes (url/expression/vbscript ofuscados) de uma vez.
const CSS_ESCAPE_OR_CTRL = /[\\\x00-\x1f\x7f]/

// Clickjacking via props PERMITIDAS (sem position/z-index): viewport-units enchem a
// tela e margem negativa puxa pra cima dos controles. O wrapper de render já confina
// (overflow:hidden+contain), mas barramos no valor como cinto-e-suspensório.
const VIEWPORT_UNIT = /\d(?:vw|vh|vmin|vmax)\b/i

// filterStyle reescreve um atributo style mantendo só (propriedade ∈ allowlist) com
// (valor sem padrão proibido). Retorna '' se nada sobrar.
function filterStyle(style: string): string {
  const out: string[] = []
  for (const decl of style.split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    const prop = decl.slice(0, i).trim().toLowerCase()
    const value = decl.slice(i + 1).trim()
    if (!prop || !value) continue
    if (!ALLOWED_CSS_PROPS.has(prop)) continue
    if (CSS_ESCAPE_OR_CTRL.test(value)) continue
    if (FORBIDDEN_CSS_VALUE.test(value)) continue
    if (VIEWPORT_UNIT.test(value)) continue
    // Margem negativa puxa conteúdo p/ fora da caixa (overlay) — barra.
    if (prop.startsWith('margin') && /(^|[\s:])-\d/.test(value)) continue
    out.push(`${prop}:${value}`)
  }
  return out.join(';')
}

// Hook único: filtra style (allowlist de props) e blinda links target=_blank.
// Registrado uma vez no módulo (idempotente — addHook acumula, então guardamos flag).
let hooksReady = false
function ensureHooks(): void {
  if (hooksReady) return
  DOMPurify.addHook('afterSanitizeAttributes', (node) => {
    const el = node as Element
    if (el.getAttribute && el.hasAttribute('style')) {
      const filtered = filterStyle(el.getAttribute('style') || '')
      if (filtered) el.setAttribute('style', filtered)
      else el.removeAttribute('style')
    }
    // Anti-tabnabbing: todo <a target=_blank> recebe rel=noopener noreferrer.
    if (el.tagName === 'A' && el.getAttribute('target') === '_blank') {
      el.setAttribute('rel', 'noopener noreferrer')
    }
  })
  hooksReady = true
}

// Config DOMPurify. Defaults de URI MANTIDOS (não setamos ALLOW_DATA_ATTR nem
// ALLOW_UNKNOWN_PROTOCOLS → data:/javascript: continuam bloqueados). FORBID redundante
// como cinto-e-suspensório além do ALLOWED_TAGS.
const CONFIG: Parameters<typeof DOMPurify.sanitize>[1] = {
  ALLOWED_TAGS,
  ALLOWED_ATTR,
  ALLOW_DATA_ATTR: false,
  FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed', 'form', 'input',
    'button', 'textarea', 'select', 'option', 'link', 'meta', 'base', 'svg',
    'math', 'template', 'noscript'],
  FORBID_ATTR: ['srcset', 'formaction', 'xlink:href', 'ping'],
  // SEM USE_PROFILES: ele SOBRESCREVE ALLOWED_TAGS/ALLOWED_ATTR (ignora a allowlist
  // estrita acima e descarta 'target' antes do hook). A allowlist explícita já é o gate.
}

// sanitizeProductHtml devolve HTML seguro p/ dangerouslySetInnerHTML. Único portão
// de renderização de HTML do produtor. String vazia se entrada vazia/nula.
export function sanitizeProductHtml(dirty: string | null | undefined): string {
  if (!dirty) return ''
  ensureHooks()
  return DOMPurify.sanitize(dirty, CONFIG) as unknown as string
}
