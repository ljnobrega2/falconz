// Hub Expedição (Melhor Envio) — unifica 4 telas de ME em abas (Etiquetas ME,
// Markup/Integrações, Webhooks, Tracking Brand). Compõe os componentes existentes;
// cada aba só monta quando ativa. Rotas antigas (/labels, /expedicao-integracoes,
// /expedicao-webhooks, /tracking-brand) abrem direto a aba correspondente.
import { useState } from 'react'
import Labels from './Labels'
import ExpedicaoIntegracoes from './ExpedicaoIntegracoes'
import ExpedicaoWebhooks from './ExpedicaoWebhooks'
import OrderWebhooksLog from './OrderWebhooksLog'
import TrackingBrand from './TrackingBrand'
import ExpedicaoCarriers from './ExpedicaoCarriers'

type Tab = 'etiquetas' | 'markup' | 'webhooks' | 'webhooks-status' | 'tracking' | 'transportadoras'

const tabsStyle = { display: 'flex', gap: 4, marginBottom: 16, borderBottom: '1px solid var(--szv2-divider)', flexWrap: 'wrap' as const }

export default function ExpedicaoMEHub({ initialTab = 'etiquetas' }: { initialTab?: Tab }) {
  const [tab, setTab] = useState<Tab>(initialTab)
  return (
    <div>
      <div className="szv2-tabs" role="tablist" style={tabsStyle}>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'etiquetas'} onClick={() => setTab('etiquetas')}>Etiquetas ME</button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'markup'} onClick={() => setTab('markup')}>Markup / Integrações</button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'webhooks'} onClick={() => setTab('webhooks')}>Webhooks</button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'webhooks-status'} onClick={() => setTab('webhooks-status')}>Histórico Webhooks (Pedidos)</button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'tracking'} onClick={() => setTab('tracking')}>Tracking Brand</button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'transportadoras'} onClick={() => setTab('transportadoras')}>Transportadoras</button>
      </div>
      {tab === 'etiquetas' && <Labels />}
      {tab === 'markup' && <ExpedicaoIntegracoes />}
      {tab === 'webhooks' && <ExpedicaoWebhooks />}
      {tab === 'webhooks-status' && <OrderWebhooksLog />}
      {tab === 'tracking' && <TrackingBrand />}
      {tab === 'transportadoras' && <ExpedicaoCarriers />}
    </div>
  )
}
