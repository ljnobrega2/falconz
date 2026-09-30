import { useState } from 'react'
import Orders from './Orders'
import ExpedicaoOrders from './ExpedicaoOrders'

type OrdersHubTab = 'pedidos' | 'expedicao'

const TAB_LABEL: Record<OrdersHubTab, string> = {
  pedidos: 'Cash on Delivery',
  expedicao: 'Expedição',
}

// Mesmo padrão de portal-ui/src/pages/OrdersHub.tsx (pedido do dono, 2026-07-27):
// admin via só Cash on Delivery em /orders, Expedição ficava escondida atrás de
// item de menu separado (/expedicao-pedidos) e "não aparecia nada" na prática —
// aba local reaproveita as DUAS telas já existentes (Orders/ExpedicaoOrders),
// sem recriar rota nem duplicar lógica.
export default function OrdersHub() {
  const [tab, setTab] = useState<OrdersHubTab>('pedidos')
  const tabs: OrdersHubTab[] = ['pedidos', 'expedicao']

  return (
    <section id="sec-admin-orders-hub">
      <div className="szv2-dash-switcher" role="tablist" aria-label="Tipo de pedido" style={{ marginBottom: 16 }}>
        {tabs.map(t => (
          <button
            key={t}
            type="button"
            className={`szv2-dash-tab${tab === t ? ' szv2-dash-tab--active' : ''}`}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
          >
            {TAB_LABEL[t]}
          </button>
        ))}
      </div>
      {tab === 'pedidos' && <Orders />}
      {tab === 'expedicao' && <ExpedicaoOrders />}
    </section>
  )
}
