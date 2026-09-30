// Hub Logística — CDs + Zonas/CEPs numa tela só (são infra motoboy relacionada:
// zonas pertencem a CDs). Compõe os componentes existentes em abas. Rotas antigas
// /cds e /zonas abrem direto a aba correspondente.
import { useState } from 'react'
import Cds from './Cds'
import Zonas from './Zonas'

type Tab = 'cds' | 'zonas'

const tabsStyle = { display: 'flex', gap: 4, marginBottom: 16, borderBottom: '1px solid var(--szv2-divider)' }

export default function LogisticaHub({ initialTab = 'cds' }: { initialTab?: Tab }) {
  const [tab, setTab] = useState<Tab>(initialTab)
  return (
    <div>
      <div className="szv2-tabs" role="tablist" style={tabsStyle}>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'cds'} onClick={() => setTab('cds')}>CDs</button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'zonas'} onClick={() => setTab('zonas')}>Zonas / CEPs</button>
      </div>
      {tab === 'cds' ? <Cds /> : <Zonas />}
    </div>
  )
}
