// Hub Carteira Expedição — abas horizontais (Transações, PIX, REST API).
//
// "Carteira Expedição" (saldos por cliente) consolidada em /carteiras;
// "Config Expedição" consolidada em /taxas-config (AUDIT-2026-07-11).
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import TpcTransacoes from './TpcTransacoes'
import Pix from './Pix'
import TpcRestApi from './TpcRestApi'

type TabKey = 'transacoes' | 'pix' | 'restapi'

const TABS: { key: TabKey; label: string }[] = [
  { key: 'transacoes', label: 'Transações Expedição' },
  { key: 'pix',        label: 'PIX' },
  { key: 'restapi',    label: 'REST API' },
]

export default function CarteiraExpedicaoHub() {
  const [params, setParams] = useSearchParams()
  const initial = (params.get('tab') as TabKey) || 'transacoes'
  const [tab, setTab] = useState<TabKey>(
    TABS.some(t => t.key === initial) ? initial : 'transacoes',
  )

  // AUDIT-2026-06-19 — sincroniza com ?tab= em navegação externa (Cmd-K / deep-link).
  useEffect(() => {
    const t = params.get('tab')
    if (t && TABS.some(x => x.key === t)) setTab(t as TabKey)
  }, [params])

  function selectTab(key: TabKey) {
    setTab(key)
    const next = new URLSearchParams(params)
    next.set('tab', key)
    setParams(next, { replace: true })
  }

  return (
    <div>
      <div className="szv2-tabs">
        {TABS.map(t => (
          <button
            key={t.key}
            className="szv2-tab"
            aria-selected={tab === t.key}
            onClick={() => selectTab(t.key)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'transacoes' && <TpcTransacoes />}
      {tab === 'pix'        && <Pix />}
      {tab === 'restapi'    && <TpcRestApi />}
    </div>
  )
}
