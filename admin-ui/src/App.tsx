// AUDIT-2026-06-18 Onda3 — ToastProvider envolve toda a árvore autenticada
import { Navigate, Route, Routes } from 'react-router-dom'
import { getToken } from './api'
import Layout from './components/Layout'
import ErrorBoundary from './components/ErrorBoundary'
import { ToastProvider } from './hooks/useToast'
import Login from './pages/Login'
import ResetPassword from './pages/ResetPassword'
import Dashboard from './pages/Dashboard'
import Faturamento from './pages/Faturamento'
import VisaoGeral from './pages/VisaoGeral'
import EtiquetasLote from './pages/EtiquetasLote'
import Affiliates from './pages/Affiliates'
import Motoboys from './pages/Motoboys'
import MotoboysDay from './pages/MotoboysDay'
import Orders from './pages/Orders'
import ExpedicaoOrders from './pages/ExpedicaoOrders'
import OrdersHub from './pages/OrdersHub'
import Logs from './pages/Logs'
import Tools from './pages/Tools'
import AffiliateWallet from './pages/AffiliateWallet'
import OnboardingSetup from './pages/OnboardingSetup'
import OnboardingRequests from './pages/OnboardingRequests'
import DocumentChanges from './pages/DocumentChanges'
import MotoboyConfig from './pages/MotoboyConfig'
import MotoboyDashboard from './pages/MotoboyDashboard'
import MotoboyFechamento from './pages/MotoboyFechamento'
import MaintenanceMode from './pages/MaintenanceMode'
import CronStatus from './pages/CronStatus'
import AuditLogViewer from './pages/AuditLogViewer'
import NotificacoesPWA from './pages/NotificacoesPWA'
import MotoboyComprovantes from './pages/MotoboyComprovantes'
import MotoboyCustodia from './pages/MotoboyCustodia'
import MotoboyConciliacao from './pages/MotoboyConciliacao'
import ApiDocs from './pages/ApiDocs'
import PushTecnico from './pages/PushTecnico'
import CapabilitiesGuard from './pages/CapabilitiesGuard'
import PwaConfig from './pages/PwaConfig'
import MotoboyMapa from './pages/MotoboyMapa'
import ProdutosHub from './pages/ProdutosHub'
import ExpedicaoMEHub from './pages/ExpedicaoMEHub'
import LogisticaHub from './pages/LogisticaHub'
import UsuariosHub from './pages/UsuariosHub'
import AdministracaoHub from './pages/AdministracaoHub'
import Carteiras from './pages/Carteiras'
import TaxasConfigPage from './pages/TaxasConfig'
import Saques from './pages/Saques'
import SistemaHub from './pages/SistemaHub'
import Support from './pages/Support'

function Protected({ children }: { children: JSX.Element }) {
  return getToken() ? children : <Navigate to="/login" replace />
}

export default function App() {
  return (
    <ToastProvider>
    <ErrorBoundary>
    <Routes>
      <Route path="/login" element={<Login />} />
      {/* AUDIT-2026-06-22 forgot-pw — pública, FORA de <Protected> (usuário deslogado
          precisa acessar o link do e-mail sem ser jogado para /login). */}
      <Route path="/reset" element={<ResetPassword />} />
      <Route path="/setup" element={<OnboardingSetup />} />
      <Route element={<Protected><Layout /></Protected>}>
        {/* Tela única: Dashboard + Faturamento em abas (VisaoGeral). '/faturamento'
            permanece p/ deep-link e abre direto na aba Faturamento. */}
        <Route path="/" element={<VisaoGeral />} />
        <Route path="/faturamento" element={<VisaoGeral initialTab="faturamento" />} />
        <Route path="/lucro-operacional" element={<VisaoGeral />} />
        {/* Hubs com abas horizontais (sidebar enxuta). Rotas antigas abaixo
            permanecem para deep-link. */}
        <Route path="/usuarios" element={<UsuariosHub />} />
        <Route path="/administracao" element={<AdministracaoHub />} />
        {/* Transações COD retirada (2026-07-11) — produtor/afiliado já veem
            extrato no próprio portal; admin não precisa de ledger bruto separado. */}
        <Route path="/carteira-cod" element={<Navigate to="/carteiras?actor=produtores" replace />} />
        {/* PIX Expedição MOVIDO p/ Taxas & Config (pedido do dono 2026-07-14). */}
        <Route path="/carteira-expedicao" element={<Navigate to="/taxas-config?sec=pix-expedicao" replace />} />
        {/* Onda "Carteiras primeiro" — consolida AffiliateWallet + CodWalletProducer
            + MotoboyCarteira + TpcClientes numa tela só (seletor de ator), sem
            fundir cálculos: cada domínio mantém suas colunas/ações/drawers. */}
        <Route path="/carteiras" element={<Carteiras />} />
        <Route path="/taxas-config" element={<TaxasConfigPage />} />
        <Route path="/saques" element={<Saques />} />
        <Route path="/sistema" element={<SistemaHub />} />
        {/* AUDIT-2026-06-19 Limpeza — rotas standalone que duplicavam abas de
            hub agora redirecionam para o hub canônico (?tab=). Evita 2ª fonte
            de verdade / quebra de contexto (mesmo destino com e sem chrome). */}
        <Route path="/users" element={<Navigate to="/usuarios" replace />} />
        <Route path="/pix" element={<Navigate to="/taxas-config?sec=pix-expedicao&tab=pix" replace />} />
        <Route path="/audit" element={<Navigate to="/sistema?tab=auditoria" replace />} />
        {/* AUDIT-2026-06-19 Limpeza — /wallet (74KB) e /settings REMOVIDAS:
            eram rotas órfãs fora da nav que reimplementavam config financeira
            (ME token / PIX / segredos / regras de carteira) já fatiada nos hubs
            → 2ª fonte de verdade perigosa. Config canônica vive nos hubs. */}
        <Route path="/producers" element={<Navigate to="/usuarios?tab=produtores" replace />} />
        <Route path="/clientes" element={<Navigate to="/usuarios?tab=clientes" replace />} />
        <Route path="/operators-users" element={<Navigate to="/usuarios?tab=operadores" replace />} />
        {/* Aba "Admin" removida de Usuários (2026-07-14) — sem tela própria hoje. */}
        <Route path="/admins" element={<Navigate to="/usuarios" replace />} />
        <Route path="/affiliates" element={<Navigate to="/usuarios?tab=afiliados" replace />} />
        {/* Comissões retirada (2026-07-11) — dado por pedido já está em Pedidos,
            saldo agregado já está em Carteiras. */}
        <Route path="/commissions" element={<Navigate to="/orders" replace />} />
        <Route path="/motoboys" element={<Motoboys />} />
        <Route path="/motoboys-dia" element={<MotoboysDay />} />
        <Route path="/orders" element={<OrdersHub />} />
        <Route path="/expedicao-pedidos" element={<Navigate to="/orders" replace />} />
        {/* Logística = CDs + Zonas em abas */}
        <Route path="/cds" element={<LogisticaHub />} />
        <Route path="/zonas" element={<LogisticaHub initialTab="zonas" />} />
        {/* Expedição ME = Etiquetas ME + Markup + Webhooks + Tracking em abas */}
        <Route path="/labels" element={<ExpedicaoMEHub />} />
        <Route path="/logs" element={<Logs />} />
        <Route path="/tools" element={<Tools />} />
        {/* Livro COD retirado (2026-07-11) — duplicava Pedidos (por pedido) e
            CodWalletProducer/carteiras (KPIs, resumo por produtor). Resumo por
            afiliado por período: usar filtro de afiliado em /orders. */}
        <Route path="/cod-livro" element={<Navigate to="/orders" replace />} />
        <Route path="/cod-saques" element={<Navigate to="/saques?fila=cod" replace />} />
        <Route path="/cod-taxas" element={<Navigate to="/taxas-config?sec=cod" replace />} />
        <Route path="/config-taxas" element={<Navigate to="/taxas-config?sec=calculo" replace />} />
        <Route path="/tpc-clientes" element={<Navigate to="/carteiras?actor=clientes" replace />} />
        {/* AUDIT-2026-06-23 — programa de afiliados unificado na aba Afiliados de
            Usuários. Rotas antigas redirecionam para as sub-abas correspondentes
            (não quebra links/Cmd-K antigos, sem dado órfão). */}
        <Route path="/affiliates-wallet" element={<Navigate to="/carteiras?actor=afiliados" replace />} />
        <Route path="/onboarding-requests" element={<OnboardingRequests />} />
        <Route path="/document-changes" element={<DocumentChanges />} />
        {/* AUDIT-2026-07-28: "pedido completo" (/orders/:id, OrderDetail.tsx)
            removida — pedido dono explícito ("pode desfazer ela por completo").
            Tudo que precisava (aprovar/imprimir/declaração/financeiro) já vive
            no drawer inline de ExpedicaoOrders.tsx/Orders.tsx. Redireciona pra
            lista em vez de 404 (não quebra link salvo/histórico de navegação). */}
        <Route path="/orders/:id" element={<Navigate to="/orders" replace />} />
        <Route path="/motoboy-config" element={<MotoboyConfig />} />
        <Route path="/motoboy-dashboard" element={<MotoboyDashboard />} />
        <Route path="/motoboy-carteira" element={<Navigate to="/administracao?tab=carteira" replace />} />
        <Route path="/motoboy-fechamento" element={<MotoboyFechamento />} />
        <Route path="/tpc-transacoes" element={<Navigate to="/taxas-config?sec=pix-expedicao&tab=transacoes" replace />} />
        <Route path="/tpc-config" element={<Navigate to="/taxas-config?sec=expedicao" replace />} />
        <Route path="/maintenance" element={<MaintenanceMode />} />
        <Route path="/crons" element={<CronStatus />} />
        <Route path="/audit-log" element={<AuditLogViewer />} />
        <Route path="/affiliate-rules" element={<Navigate to="/taxas-config?sec=regras-afiliados" replace />} />
        <Route path="/expedicao-integracoes" element={<ExpedicaoMEHub initialTab="markup" />} />
        <Route path="/expedicao-webhooks" element={<ExpedicaoMEHub initialTab="webhooks" />} />
        <Route path="/expedicao-transportadoras" element={<ExpedicaoMEHub initialTab="transportadoras" />} />
        <Route path="/notificacoes-pwa" element={<NotificacoesPWA />} />
        {/* Etiquetas/Lote migradas para o drawer de Pedidos — rotas redirecionam. */}
        <Route path="/etiquetas-lote" element={<Navigate to="/orders" replace />} />
        <Route path="/motoboy-etiquetas" element={<Navigate to="/orders" replace />} />
        <Route path="/motoboy-comprovantes" element={<MotoboyComprovantes />} />
        <Route path="/motoboy-saques" element={<Navigate to="/administracao?tab=saques" replace />} />
        <Route path="/motoboy-custodia" element={<MotoboyCustodia />} />
        <Route path="/motoboy-conciliacao" element={<MotoboyConciliacao />} />
        <Route path="/cod-wallet-producer" element={<Navigate to="/carteiras?actor=produtores" replace />} />
        <Route path="/cod-wallet-transactions" element={<Navigate to="/carteiras?actor=produtores" replace />} />
        <Route path="/tracking-brand" element={<ExpedicaoMEHub initialTab="tracking" />} />
        <Route path="/api-docs" element={<ApiDocs />} />
        <Route path="/push-tecnico" element={<PushTecnico />} />
        <Route path="/capabilities" element={<CapabilitiesGuard />} />
        <Route path="/pwa-config" element={<PwaConfig />} />
        <Route path="/bulk-actions" element={<Navigate to="/orders" replace />} />
        <Route path="/motoboy-mapa" element={<MotoboyMapa />} />
        {/* Produtos vira hub: "Aprovar produtos" é aba dentro de Produtos.
            /products-approval permanece p/ deep-link e abre direto na aba Aprovar. */}
        <Route path="/products" element={<ProdutosHub />} />
        <Route path="/products-approval" element={<ProdutosHub initialTab="aprovar" />} />
        {/* Estoque mergeado dentro de /products (tabela única produto+estoque). */}
        <Route path="/stock" element={<Navigate to="/products" replace />} />
        <Route path="/checkout-links" element={<Navigate to="/taxas-config?sec=checkouts" replace />} />
        <Route path="/support" element={<Support />} />
      </Route>
    </Routes>
    </ErrorBoundary>
    </ToastProvider>
  )
}
