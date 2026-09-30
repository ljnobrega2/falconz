import { useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import Orders from '../admin-orders/Orders'
import Expedicao from './Expedicao'
import type { PortalMe } from '../components/Layout'

type OrdersHubTab = 'pedidos' | 'expedicao'

const TAB_LABEL: Record<OrdersHubTab, string> = {
  pedidos: 'Cash on Delivery',
  expedicao: 'Expedição',
}

// Produtor com expedição ATIVA reclamava de não ver os pedidos de frete sem ficar
// clicando manualmente no item de menu separado "Expedição" — o menu lateral tinha
// DOIS links (Cash on Delivery / Expedição) e nada os unia. Aba local (sem trocar de
// rota) dentro de /orders: mesmo gate `expedicaoAtiva` já usado em Layout.tsx, tudo
// que é de expedição fica visível junto do Pedidos, sem ação manual extra.
//
// Título/subtítulo + switcher espelham o padrão de Dashboard.tsx (h2.szv2-page-title
// + p muted + .szv2-dash-switcher/.szv2-dash-tab) — mesma "copy e mini copy" da Visão
// geral, pedido do dono pra padronizar a tela.
export default function OrdersHub() {
  const { me } = useOutletContext<{ me: PortalMe | null }>() ?? { me: null }
  const [tab, setTab] = useState<OrdersHubTab>('pedidos')
  const role = (me?.role || 'cliente').toLowerCase()
  const isProducer = role === 'produtor' || role === 'producer'
  const isOperator = role === 'operator' || role === 'operador'
  const expFlag = me?.settings?.expedicao_ativa
  // OL sempre vê Expedição (nav já a expõe sem gate de flag — ver Layout.tsx);
  // produtor segue atrás do flag expedicao_ativa.
  const expedicaoAtiva = isOperator || (isProducer && (expFlag === true || expFlag === 'true'))

  const tabs: OrdersHubTab[] = expedicaoAtiva ? ['pedidos', 'expedicao'] : ['pedidos']
  const activeTab = tabs.includes(tab) ? tab : 'pedidos'

  return (
    <section id="sec-orders-hub" className="sz-sec">
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Pedidos
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Acompanhe e gerencie os pedidos Cash on Delivery e de expedição.
        </p>
      </div>

      {tabs.length > 1 && (
        <div className="szv2-dash-switcher" role="tablist" aria-label="Tipo de pedido" style={{ marginBottom: 16 }}>
          {tabs.map(t => (
            <button
              key={t}
              type="button"
              className={`szv2-dash-tab${activeTab === t ? ' szv2-dash-tab--active' : ''}`}
              role="tab"
              aria-selected={activeTab === t}
              onClick={() => setTab(t)}
            >
              {t === 'pedidos' ? (
                <svg viewBox="0 0 20 20" aria-hidden="true">
                  <path d="M5 14a2.5 2.5 0 1 0 0 .01zM15 14a2.5 2.5 0 1 0 0 .01zM11 5h3l3 4v4h-2a3 3 0 0 0-6 0H8a3 3 0 0 0-5.4-1.8L2 9l4-1 2-3h3z" />
                </svg>
              ) : (
                <svg viewBox="0 0 20 20" aria-hidden="true">
                  <path d="M10 2 3 5.5v9L10 18l7-3.5v-9L10 2zm0 2.2 4.6 2.3L10 8.8 5.4 6.5 10 4.2zM5 8.1l4 2v5.3l-4-2V8.1zm10 0v5.3l-4 2v-5.3l4-2z" />
                </svg>
              )}
              {TAB_LABEL[t]}
            </button>
          ))}
        </div>
      )}
      {activeTab === 'pedidos' && <Orders />}
      {activeTab === 'expedicao' && <Expedicao />}
    </section>
  )
}
