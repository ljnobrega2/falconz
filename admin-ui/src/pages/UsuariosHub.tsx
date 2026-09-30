// Hub de Usuários — abas horizontais (szv2-tabs) que renderizam os
// componentes de página existentes. Cada aba monta o componente sob demanda
// (cada página faz seu próprio fetch no mount), sincronizada com ?tab=.
//
// AUDIT-2026-06-23 — a aba "Afiliados" passou a CONCENTRAR todo o programa de
// afiliados (antes fatiado no menu "Afiliados $" / rota /affiliates-wallet).
//
// AUDIT-2026-07-11 — as sub-abas "Lista" e "Carteira" eram 2 fontes de dados
// diferentes (identidade vs saldos) que o operador tinha que cruzar
// mentalmente. Fundidas em <AffiliatesUnified> — uma tabela só, um drawer só
// (abas Perfil/Carteira dentro dele). "Regras" continua separada (é
// configuração, não listagem de pessoas).
// Sub-abas internas (?sub=geral|regras):
//   • Geral  → <AffiliatesUnified> (lista + carteira fundidas, drawer único)
//   • Regras → <AffiliateRules>    (regras/penalidades/taxas + recompensa)
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import AffiliatesUnified from './AffiliatesUnified'
import Producers from './Producers'
import Clientes from './Clientes'
import OperatorsUsers from './OperatorsUsers'
import DocumentChanges from './DocumentChanges'

// AUDIT-2026-07-11 — a aba "Comissões" saiu daqui (Usuários é gestão de
// pessoas, não financeiro) e virou tela própria em /commissions.
// AUDIT-2026-07-14 — aba "Admin" REMOVIDA (gestão de admins não é usuário de
// plataforma). Sub-aba "Regras" (afiliados) e "Relatório Checkouts"
// (produtores) migradas p/ Taxas & Config (config/relatório, não pessoas).
type TabKey = 'afiliados' | 'produtores' | 'clientes' | 'operadores' | 'doc-changes'

const TABS: { key: TabKey; label: string }[] = [
  { key: 'afiliados',  label: 'Afiliados' },
  { key: 'produtores', label: 'Produtores' },
  { key: 'clientes',   label: 'Clientes' },
  { key: 'operadores', label: 'Operadores Logísticos' },
  { key: 'doc-changes', label: 'Trocas CPF/CNPJ' },
]

export default function UsuariosHub() {
  const [params, setParams] = useSearchParams()
  const initial = (params.get('tab') as TabKey) || 'afiliados'
  const [tab, setTab] = useState<TabKey>(
    TABS.some(t => t.key === initial) ? initial : 'afiliados',
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
    next.delete('sub')
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

      {tab === 'afiliados'  && <AffiliatesUnified />}
      {tab === 'produtores' && <Producers />}
      {tab === 'clientes'   && <Clientes />}
      {tab === 'operadores' && <OperatorsUsers />}
      {tab === 'doc-changes' && <DocumentChanges />}
    </div>
  )
}
