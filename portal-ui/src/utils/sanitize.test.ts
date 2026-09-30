// @vitest-environment jsdom
//
// Bateria de XSS adversarial contra o portão REAL (sanitizeProductHtml). Não argumenta
// sobre segurança — EXECUTA cada payload e prova que o vetor é neutralizado. Roda em
// jsdom (DOMPurify precisa de DOM). `npm run test:sanitize`.
import { describe, it, expect } from 'vitest'
import { sanitizeProductHtml } from './sanitize'

// Helper: nenhum desses tokens pode sobreviver, qualquer caixa.
function assertNeutralized(out: string) {
  const low = out.toLowerCase()
  expect(low).not.toContain('<script')
  expect(low).not.toContain('<iframe')
  expect(low).not.toContain('<object')
  expect(low).not.toContain('<embed')
  expect(low).not.toContain('<form')
  expect(low).not.toContain('<svg')
  expect(low).not.toContain('<math')
  expect(low).not.toContain('onerror')
  expect(low).not.toContain('onload')
  expect(low).not.toContain('onclick')
  expect(low).not.toContain('onmouseover')
  expect(low).not.toContain('javascript:')
  expect(low).not.toContain('vbscript:')
  // CSS clickjacking / overlay props nunca passam.
  expect(low).not.toContain('position')
  expect(low).not.toContain('z-index')
  expect(low).not.toContain('transform')
  // url() em CSS (tracking/SSRF) nunca passa.
  expect(low).not.toContain('url(')
  expect(low).not.toContain('@import')
  expect(low).not.toContain('expression(')
}

describe('sanitizeProductHtml — bateria XSS', () => {
  const PAYLOADS: Array<[string, string]> = [
    ['script tag', '<script>alert(1)</script>'],
    ['img onerror', '<img src=x onerror=alert(1)>'],
    ['svg onload', '<svg onload=alert(1)></svg>'],
    ['a javascript:', '<a href="javascript:alert(1)">x</a>'],
    ['iframe js', '<iframe src="javascript:alert(1)"></iframe>'],
    ['uppercase onclick', '<DIV ONCLICK=alert(1)>x</DIV>'],
    ['onmouseover', '<p onmouseover=alert(1)>hover</p>'],
    ['overlay position:fixed', '<div style="position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:99999;background:#000">over</div>'],
    ['absolute z-index', '<div style="position:absolute;z-index:2147483647">x</div>'],
    ['transform cover', '<div style="transform:scale(50);transform-origin:0 0">x</div>'],
    ['css url javascript', '<div style="background:url(javascript:alert(1))">x</div>'],
    ['css url tracking', '<div style="background:url(https://evil.example/pixel.png)">x</div>'],
    ['css expression', '<div style="width:expression(alert(1))">x</div>'],
    ['css import', '<style>@import url(//evil.example/x.css)</style>'],
    ['css comment bypass', '<div style="background:re/**/d;position:fixed">x</div>'],
    ['data uri img', '<img src="data:text/html,<script>alert(1)</script>">'],
    ['form formaction', '<form><button formaction="javascript:alert(1)">go</button></form>'],
    ['object data', '<object data="javascript:alert(1)"></object>'],
    ['embed', '<embed src="javascript:alert(1)">'],
    ['base href', '<base href="javascript:alert(1)//">'],
    ['mxss math glyph', '<math><mtext><table><mglyph><style><img src=x onerror=alert(1)>'],
    ['mxss noscript', '<noscript><p title="</noscript><img src=x onerror=alert(1)>">'],
    ['svg foreignobject', '<svg><foreignObject><img src=x onerror=alert(1)></foreignObject></svg>'],
    ['meta refresh', '<meta http-equiv="refresh" content="0;url=javascript:alert(1)">'],
  ]

  for (const [name, payload] of PAYLOADS) {
    it(`neutraliza: ${name}`, () => {
      assertNeutralized(sanitizeProductHtml(payload))
    })
  }

  // Regressão dos bypasses confirmados pelo review adversarial (escape CSS + clickjacking).
  const ADVERSARIAL: Array<[string, string, (out: string) => void]> = [
    ['escape \\75 rl()', '<div style="background:\\75 rl(//evil/x)">x</div>',
      o => { expect(o).not.toContain('\\'); expect(o.toLowerCase()).not.toContain('rl(') }],
    ['escape ur\\6c()', '<div style="background:ur\\6c(//evil/x)">x</div>',
      o => { expect(o).not.toContain('\\'); expect(o.toLowerCase()).not.toContain('//evil') }],
    ['escape parens url\\28', '<div style="background:url\\28 //evil/x\\29">x</div>',
      o => expect(o).not.toContain('\\')],
    ['escape 6-hex', '<div style="background:\\000075\\000072\\00006c(//evil/x)">x</div>',
      o => expect(o).not.toContain('\\')],
    ['escape vbscript', '<div style="background:\\76 bscript:x">x</div>',
      o => { expect(o).not.toContain('\\'); expect(o.toLowerCase()).not.toContain('bscript') }],
    ['escape expression', '<div style="width:expressio\\6e(alert(1))">x</div>',
      o => expect(o).not.toContain('\\')],
    ['escape on background-image', '<div style="background-image:\\75 rl(//evil/x)">x</div>',
      o => expect(o).not.toContain('\\')],
    ['clickjack 100vw/vh', '<a href="https://x" style="display:block;width:100vw;height:100vh;margin-top:-3000px;opacity:0;background:#000">h</a>',
      o => { const l = o.toLowerCase(); expect(l).not.toContain('100vw'); expect(l).not.toContain('100vh'); expect(l).not.toContain('margin-top:-') }],
    ['clickjack neg-margin huge', '<a href="https://x" style="display:block;width:9999px;height:9999px;margin-left:-5000px;margin-top:-5000px;opacity:0">h</a>',
      o => { const l = o.toLowerCase(); expect(l).not.toContain('-5000'); expect(l).not.toContain('margin-left:-'); expect(l).not.toContain('margin-top:-') }],
    ['clickjack vw+padding', '<a href="https://x" style="display:block;width:100vw;padding-bottom:5000px;opacity:0;margin-top:-2500px">h</a>',
      o => { const l = o.toLowerCase(); expect(l).not.toContain('100vw'); expect(l).not.toContain('margin-top:-') }],
  ]
  for (const [name, payload, assertFn] of ADVERSARIAL) {
    it(`regressão adversarial: ${name}`, () => {
      const out = sanitizeProductHtml(payload)
      assertFn(out)
    })
  }

  it('força rel=noopener em target=_blank', () => {
    const out = sanitizeProductHtml('<a href="https://x.example" target="_blank">link</a>')
    expect(out.toLowerCase()).toContain('rel="noopener noreferrer"')
  })

  it('entrada vazia/nula → string vazia', () => {
    expect(sanitizeProductHtml('')).toBe('')
    expect(sanitizeProductHtml(null)).toBe('')
    expect(sanitizeProductHtml(undefined)).toBe('')
  })

  // PROVA que o card legítimo SOBREVIVE (sanitização não é "remove tudo").
  it('preserva o card DATALAPROX (gradient/shadow/flex/badges)', () => {
    const card = '<div style="max-width:680px;background:#0d0d0d;border-radius:16px;box-shadow:0 10px 40px rgba(0,0,0,.5);overflow:hidden">' +
      '<div style="background:linear-gradient(135deg,#1a0000 0%,#7a0000 100%);padding:34px 26px;text-align:center">' +
      '<h1 style="font-size:34px;color:#fff;text-shadow:0 2px 8px rgba(255,42,42,.6)">DATALAPROX</h1></div>' +
      '<div style="display:flex;flex-wrap:wrap;gap:12px">' +
      '<div style="flex:1 1 45%;border-left:4px solid #ff2a2a;padding:14px 16px"><b style="color:#fff">Energia</b></div></div></div>'
    const out = sanitizeProductHtml(card)
    expect(out).toContain('linear-gradient')
    expect(out).toContain('box-shadow')
    expect(out).toContain('text-shadow')
    expect(out).toContain('display:flex')
    expect(out).toContain('border-left:4px solid #ff2a2a')
    expect(out).toContain('DATALAPROX')
    expect(out).toContain('<h1')
    expect(out).toContain('<b')
  })
})
