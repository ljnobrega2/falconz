// Estilos compartilhados das abas dentro dos drawers laterais de detalhe.
// Mesmo padrão visual das abas do drawer de Pedidos (Orders.tsx): linha de
// botões com indicador inferior laranja/azul da marca quando ativa.
// var(--szv2-brand) = #1E6FF2 (NUNCA verde).
import type { CSSProperties } from 'react'

export const drawerTabsStyle: CSSProperties = {
  display: 'flex',
  gap: 4,
  borderBottom: '1px solid var(--szv2-divider)',
}

export function drawerTabBtnStyle(active: boolean): CSSProperties {
  return {
    background: 'transparent',
    border: 0,
    borderBottom: active ? '2px solid var(--szv2-brand)' : '2px solid transparent',
    color: active ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
    fontWeight: active ? 700 : 500,
    fontSize: 13,
    padding: '8px 10px',
    cursor: 'pointer',
  }
}
