// Tela ÚNICA de Visão Geral — unifica Dashboard + Faturamento em abas (pedido do
// dono: "unificar dashboard e faturamento em uma tela única · auditar valores").
// Compõe os componentes existentes sem reescrevê-los: cada aba só monta quando
// ativa (fetch lazy, sem chamada dupla).
//
// FONTE DA VERDADE = a ROTA, não estado interno. '/' = aba Dashboard;
// '/faturamento' = aba Faturamento. Antes a aba era um useState(initialTab):
// como React Router reusa a MESMA instância de VisaoGeral entre '/' e
// '/faturamento' (mesmo elemento), navegar trocava só o prop e o useState
// ignorava — o botão "Ver faturamento completo" não fazia nada. Derivando a aba
// de useLocation + navegando ao clicar nas tabs, o <Link to="/faturamento">
// passa a realmente abrir o Faturamento.
import { useLocation, useNavigate } from 'react-router-dom'
import Dashboard from './Dashboard'
import Faturamento from './Faturamento'
import OperationalProfit from './OperationalProfit'

type Tab = 'dashboard' | 'faturamento' | 'lucro'

export default function VisaoGeral({ initialTab }: { initialTab?: Tab }) {
  const location = useLocation()
  const navigate = useNavigate()

  // Aba derivada da rota (com fallback ao prop legado, p/ deep-links que ainda
  // montem com initialTab). /faturamento → 'faturamento', resto → 'dashboard'.
  const tab: Tab = location.pathname.startsWith('/lucro-operacional')
    ? 'lucro'
    : location.pathname.startsWith('/faturamento') || initialTab === 'faturamento'
      ? 'faturamento'
      : 'dashboard'

  function selectTab(next: Tab) {
    navigate(next === 'faturamento' ? '/faturamento' : next === 'lucro' ? '/lucro-operacional' : '/')
  }

  return (
    <div>
      <div
        className="szv2-tabs"
        role="tablist"
        style={{ display: 'flex', gap: 4, marginBottom: 16, borderBottom: '1px solid var(--szv2-divider)' }}
      >
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'dashboard'} onClick={() => selectTab('dashboard')}>
          Visão geral
        </button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'faturamento'} onClick={() => selectTab('faturamento')}>
          Faturamento
        </button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'lucro'} onClick={() => selectTab('lucro')}>
          Lucro operacional
        </button>
      </div>

      {tab === 'dashboard' ? <Dashboard /> : tab === 'faturamento' ? <Faturamento /> : <OperationalProfit />}
    </div>
  )
}
