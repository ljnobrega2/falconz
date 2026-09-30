// Hub de Administração (ex-"Operação Motoboy") — abas horizontais com TODOS os
// itens do módulo motoboy. "Pedidos" NÃO entra (item de sidebar separado).
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import GroupedTabs from '../components/GroupedTabs'
import MotoboyDashboard from './MotoboyDashboard'
import Motoboys from './Motoboys'
import MotoboysDay from './MotoboysDay'
import MotoboyComprovantes from './MotoboyComprovantes'
import MotoboyCustodia from './MotoboyCustodia'
import MotoboyConciliacao from './MotoboyConciliacao'
import MotoboyFechamento from './MotoboyFechamento'
import MotoboyMapa from './MotoboyMapa'
import MotoboySaques from './MotoboySaques'
import MotoboyCarteira from './MotoboyCarteira'

type TabKey =
  | 'dashboard' | 'motoboys' | 'motoboys-dia'
  | 'comprovantes' | 'custodia' | 'conciliacao' | 'fechamento' | 'mapa' | 'saques' | 'carteira'

// AUDIT-2026-06-19 — 11 abas sub-agrupadas em 3 blocos (Operação / Financeiro /
// Cadastro) com separador visual, em vez de um strip plano ilegível.
type TabDef = { key: TabKey; label: string; group: string }

const TABS: TabDef[] = [
  // Operação do dia — "Dashboard Motoboy" + "Motoboys do Dia" foram MERGEADOS numa
  // única aba "Painel do Dia" (regra do dono), preservando as duas funcionalidades
  // empilhadas. Deep-link ?tab=motoboys-dia cai no fallback p/ 'dashboard'.
  { key: 'dashboard',    label: 'Painel do Dia',     group: 'Operação' },
  { key: 'comprovantes', label: 'Comprovantes',      group: 'Operação' },
  { key: 'mapa',         label: 'Mapa Ao Vivo',      group: 'Operação' },
  // Financeiro / custódia
  { key: 'custodia',     label: 'Custódia',          group: 'Financeiro' },
  { key: 'conciliacao',  label: 'Conciliação',       group: 'Financeiro' },
  { key: 'fechamento',   label: 'Fechamento',        group: 'Financeiro' },
  { key: 'saques',       label: 'Saques Motoboy',    group: 'Financeiro' },
  { key: 'carteira',     label: 'Carteira Motoboy',  group: 'Financeiro' },
  // Cadastro / config
  { key: 'motoboys',     label: 'Motoboys',          group: 'Cadastro' },
]

export default function AdministracaoHub() {
  const [params, setParams] = useSearchParams()
  const initial = (params.get('tab') as TabKey) || 'dashboard'
  const [tab, setTab] = useState<TabKey>(
    TABS.some(t => t.key === initial) ? initial : 'dashboard',
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
      <GroupedTabs tabs={TABS} active={tab} onSelect={selectTab} />

      {/* Painel do Dia = merge de Dashboard Motoboy + Motoboys do Dia (empilhados,
          todas as funcionalidades preservadas). */}
      {tab === 'dashboard' && (
        <>
          <MotoboyDashboard />
          <div style={{ borderTop: '1px solid var(--szv2-divider)', margin: '24px 0' }} />
          <MotoboysDay />
        </>
      )}
      {tab === 'motoboys'     && <Motoboys />}
      {tab === 'comprovantes' && <MotoboyComprovantes />}
      {tab === 'custodia'     && <MotoboyCustodia />}
      {tab === 'conciliacao'  && <MotoboyConciliacao />}
      {tab === 'fechamento'   && <MotoboyFechamento />}
      {tab === 'mapa'         && <MotoboyMapa />}
      {tab === 'saques'       && <MotoboySaques />}
      {tab === 'carteira'     && <MotoboyCarteira />}
    </div>
  )
}
