import { Navigate, Route, Routes } from 'react-router-dom'
import { getToken } from './api'
import Layout from './components/Layout'
import ErrorBoundary from './components/ErrorBoundary'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Users from './pages/Users'
import Wallet from './pages/Wallet'
import Pix from './pages/Pix'
import Cds from './pages/Cds'
import Zonas from './pages/Zonas'
import Affiliates from './pages/Affiliates'
import Commissions from './pages/Commissions'
import Motoboys from './pages/Motoboys'
import MotoboysDay from './pages/MotoboysDay'
import Orders from './pages/Orders'
import Labels from './pages/Labels'
import Logs from './pages/Logs'
import Tools from './pages/Tools'
import Settings from './pages/Settings'
import AuditEngine from './pages/AuditEngine'
import CodLivro from './pages/CodLivro'
import CodSaques from './pages/CodSaques'
import CodTaxasEntrega from './pages/CodTaxasEntrega'
import TpcClientes from './pages/TpcClientes'
import AffiliateWallet from './pages/AffiliateWallet'
import OnboardingSetup from './pages/OnboardingSetup'
import OnboardingRequests from './pages/OnboardingRequests'
import OrderDetail from './pages/OrderDetail'
import MotoboyConfig from './pages/MotoboyConfig'
import MotoboyDashboard from './pages/MotoboyDashboard'
import MotoboyCarteira from './pages/MotoboyCarteira'
import MotoboyFechamento from './pages/MotoboyFechamento'
import TpcTransacoes from './pages/TpcTransacoes'
import TpcConfiguracoes from './pages/TpcConfiguracoes'
import MaintenanceMode from './pages/MaintenanceMode'
import CronStatus from './pages/CronStatus'
import AuditLogViewer from './pages/AuditLogViewer'
import AffiliateRules from './pages/AffiliateRules'
import ExpedicaoIntegracoes from './pages/ExpedicaoIntegracoes'
import ExpedicaoWebhooks from './pages/ExpedicaoWebhooks'
import NotificacoesPWA from './pages/NotificacoesPWA'
import MotoboyEtiquetas from './pages/MotoboyEtiquetas'
import MotoboyComprovantes from './pages/MotoboyComprovantes'
import MotoboySaques from './pages/MotoboySaques'
import MotoboyCustodia from './pages/MotoboyCustodia'
import MotoboyConciliacao from './pages/MotoboyConciliacao'
import CodWalletProducer from './pages/CodWalletProducer'
import CodWalletTransactions from './pages/CodWalletTransactions'
import TrackingBrand from './pages/TrackingBrand'
import ApiDocs from './pages/ApiDocs'
import PushTecnico from './pages/PushTecnico'
import CapabilitiesGuard from './pages/CapabilitiesGuard'
import OrderMetaNormalization from './pages/OrderMetaNormalization'
import PwaConfig from './pages/PwaConfig'
import BulkActions from './pages/BulkActions'
import MotoboyMapa from './pages/MotoboyMapa'
import Products from './pages/Products'
import CheckoutLinks from './pages/CheckoutLinks'

function Protected({ children }: { children: JSX.Element }) {
  return getToken() ? children : <Navigate to="/login" replace />
}

export default function App() {
  return (
    <ErrorBoundary>
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/setup" element={<OnboardingSetup />} />
      <Route element={<Protected><Layout /></Protected>}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/users" element={<Users />} />
        <Route path="/affiliates" element={<Affiliates />} />
        <Route path="/commissions" element={<Commissions />} />
        <Route path="/motoboys" element={<Motoboys />} />
        <Route path="/motoboys-dia" element={<MotoboysDay />} />
        <Route path="/orders" element={<Orders />} />
        <Route path="/cds" element={<Cds />} />
        <Route path="/zonas" element={<Zonas />} />
        <Route path="/wallet" element={<Wallet />} />
        <Route path="/pix" element={<Pix />} />
        <Route path="/labels" element={<Labels />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="/logs" element={<Logs />} />
        <Route path="/tools" element={<Tools />} />
        <Route path="/audit" element={<AuditEngine />} />
        <Route path="/cod-livro" element={<CodLivro />} />
        <Route path="/cod-saques" element={<CodSaques />} />
        <Route path="/cod-taxas" element={<CodTaxasEntrega />} />
        <Route path="/tpc-clientes" element={<TpcClientes />} />
        <Route path="/affiliates-wallet" element={<AffiliateWallet />} />
        <Route path="/onboarding-requests" element={<OnboardingRequests />} />
        <Route path="/orders/:id" element={<OrderDetail />} />
        <Route path="/motoboy-config" element={<MotoboyConfig />} />
        <Route path="/motoboy-dashboard" element={<MotoboyDashboard />} />
        <Route path="/motoboy-carteira" element={<MotoboyCarteira />} />
        <Route path="/motoboy-fechamento" element={<MotoboyFechamento />} />
        <Route path="/tpc-transacoes" element={<TpcTransacoes />} />
        <Route path="/tpc-config" element={<TpcConfiguracoes />} />
        <Route path="/maintenance" element={<MaintenanceMode />} />
        <Route path="/crons" element={<CronStatus />} />
        <Route path="/audit-log" element={<AuditLogViewer />} />
        <Route path="/affiliate-rules" element={<AffiliateRules />} />
        <Route path="/expedicao-integracoes" element={<ExpedicaoIntegracoes />} />
        <Route path="/expedicao-webhooks" element={<ExpedicaoWebhooks />} />
        <Route path="/notificacoes-pwa" element={<NotificacoesPWA />} />
        <Route path="/motoboy-etiquetas" element={<MotoboyEtiquetas />} />
        <Route path="/motoboy-comprovantes" element={<MotoboyComprovantes />} />
        <Route path="/motoboy-saques" element={<MotoboySaques />} />
        <Route path="/motoboy-custodia" element={<MotoboyCustodia />} />
        <Route path="/motoboy-conciliacao" element={<MotoboyConciliacao />} />
        <Route path="/cod-wallet-producer" element={<CodWalletProducer />} />
        <Route path="/cod-wallet-transactions" element={<CodWalletTransactions />} />
        <Route path="/tracking-brand" element={<TrackingBrand />} />
        <Route path="/api-docs" element={<ApiDocs />} />
        <Route path="/push-tecnico" element={<PushTecnico />} />
        <Route path="/capabilities" element={<CapabilitiesGuard />} />
        <Route path="/order-meta-normalization" element={<OrderMetaNormalization />} />
        <Route path="/pwa-config" element={<PwaConfig />} />
        <Route path="/bulk-actions" element={<BulkActions />} />
        <Route path="/motoboy-mapa" element={<MotoboyMapa />} />
        <Route path="/products" element={<Products />} />
        <Route path="/checkout-links" element={<CheckoutLinks />} />
      </Route>
    </Routes>
    </ErrorBoundary>
  )
}
