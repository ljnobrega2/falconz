const PRINTABLE_EXPEDITION_STATUSES = new Set(['processing', 'em_separacao', 'embalado'])

function normalizeExpeditionStatus(status: string): string {
  return (status || '').trim().toLowerCase().replace(/^wc-/, '')
}

/**
 * Impressão de etiqueta/declaração é uma ação operacional global: somente o
 * operador logístico a recebe no portal. Produtores continuam limitados a
 * aprovar/cancelar os próprios pedidos.
 */
export function canPrintExpeditionOrder(isOperator: boolean, status: string, hasLabel: boolean): boolean {
  return isOperator && hasLabel && PRINTABLE_EXPEDITION_STATUSES.has(normalizeExpeditionStatus(status))
}
