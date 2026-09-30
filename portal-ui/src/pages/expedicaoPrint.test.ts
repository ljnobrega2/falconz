import { describe, expect, it } from 'vitest'
import { canPrintExpeditionOrder } from './expedicaoPrint'

describe('canPrintExpeditionOrder', () => {
  it.each(['processing', 'em_separacao', 'embalado', 'wc-processing'])(
    'libera operador com etiqueta no status %s',
    status => {
      expect(canPrintExpeditionOrder(true, status, true)).toBe(true)
    },
  )

  it('não libera produtor, mesmo com etiqueta', () => {
    expect(canPrintExpeditionOrder(false, 'processing', true)).toBe(false)
  })

  it('não libera pedido sem etiqueta', () => {
    expect(canPrintExpeditionOrder(true, 'processing', false)).toBe(false)
  })

  it.each(['pending', 'em_andamento', 'coletado', 'enviado', 'entregue', 'cancelled'])(
    'não libera impressão no status %s',
    status => {
      expect(canPrintExpeditionOrder(true, status, true)).toBe(false)
    },
  )
})
