// Hub Sistema — abas horizontais. Acessível por ícone (engrenagem) no topbar,
// fora do menu esquerdo. "Configurações" removida (funções já existem em outras
// telas). Templates PWA + Push Técnico incorporados como abas aqui.
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import GroupedTabs from '../components/GroupedTabs'
import MaintenanceMode from './MaintenanceMode'
import CronStatus from './CronStatus'
import PwaConfig from './PwaConfig'
import NotificacoesPWA from './NotificacoesPWA'
import PushTecnico from './PushTecnico'
import CapabilitiesGuard from './CapabilitiesGuard'
import AuditoriaHub from './AuditoriaHub'
import ApiDocs from './ApiDocs'
import Logs from './Logs'

type TabKey =
  | 'manutencao' | 'crons' | 'pwa' | 'templates-pwa' | 'push-tecnico'
  | 'capabilities' | 'auditoria' | 'api-docs' | 'logs'

// AUDIT-2026-06-19 — 11 abas sub-agrupadas em 3 blocos com separador visual
// (não esconder atrás de "Avançado"; só dar estrutura ao tab-strip). "Financeiro"
// vem PRIMEIRO — Auditoria deixa de ficar enterrada no fim de "Sistema" e fica
// perto do financeiro (rota /audit redireciona p/ ?tab=auditoria, mantida).
type TabDef = { key: TabKey; label: string; group: string }

const TABS: TabDef[] = [
  // Financeiro (primeiro — alvo do drill-down de "split divergente"). UMA aba só:
  // AUDIT-2026-06-23 — "Alertas" + "Auditoria" + "Ferramentas" + "Log Auditoria"
  // (antes 4 abas soltas) UNIFICADAS numa única aba "Auditoria" com sub-abas
  // internas (?sub=) via <AuditoriaHub>. Regra do dono (#73): "3 submenus em um
  // só, menus clicáveis sem estar tudo exposto". Default da sub-aba = Auditoria,
  // preservando /audit → /sistema?tab=auditoria (motor de auditoria financeira).
  { key: 'auditoria',     label: 'Auditoria & Diagnóstico', group: 'Financeiro' },
  // Operação
  { key: 'manutencao',    label: 'Manutenção',     group: 'Operação' },
  { key: 'crons',         label: 'Crons',          group: 'Operação' },
  { key: 'logs',          label: 'Logs',           group: 'Operação' },
  // Avançado (config tocada raramente)
  { key: 'pwa',           label: 'PWA Config',     group: 'Avançado' },
  { key: 'templates-pwa', label: 'Templates PWA',  group: 'Avançado' },
  { key: 'push-tecnico',  label: 'Push Técnico',   group: 'Avançado' },
  { key: 'capabilities',  label: 'Capabilities',   group: 'Avançado' },
  { key: 'api-docs',      label: 'API Docs',       group: 'Avançado' },
]

export default function SistemaHub() {
  const [params, setParams] = useSearchParams()
  const initial = (params.get('tab') as TabKey) || 'manutencao'
  const [tab, setTab] = useState<TabKey>(
    TABS.some(t => t.key === initial) ? initial : 'manutencao',
  )

  // AUDIT-2026-06-19 — sincroniza com ?tab= em navegação externa (Cmd-K / deep-link)
  // quando o hub já está montado. selectTab re-dispara setParams → setTab(mesmo) = no-op.
  useEffect(() => {
    const t = params.get('tab')
    if (t && TABS.some(x => x.key === t)) setTab(t as TabKey)
  }, [params])

  function selectTab(key: TabKey) {
    setTab(key)
    const next = new URLSearchParams(params)
    next.set('tab', key)
    // Ao sair de Auditoria, remove o ?sub= órfão (sub-aba do AuditoriaHub).
    if (key !== 'auditoria') next.delete('sub')
    setParams(next, { replace: true })
  }

  return (
    <div>
      <GroupedTabs tabs={TABS} active={tab} onSelect={selectTab} />

      {tab === 'manutencao'    && <MaintenanceMode />}
      {tab === 'crons'         && <CronStatus />}
      {tab === 'pwa'           && <PwaConfig />}
      {tab === 'templates-pwa' && <NotificacoesPWA />}
      {tab === 'push-tecnico'  && <PushTecnico />}
      {tab === 'capabilities'  && <CapabilitiesGuard />}
      {tab === 'auditoria'     && <AuditoriaHub />}
      {tab === 'api-docs'      && <ApiDocs />}
      {tab === 'logs'          && <Logs />}
    </div>
  )
}
