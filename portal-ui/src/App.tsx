// Portal V2 (PRODUTOR, AFILIADO e OL/operator). Rotas espelham as seções de
// templates/portal/v2/dashboard-v2.php. O menu é escopado por papel no Layout
// (buildNavGroups): cada persona vê só o que age (UX-AUDIT §3.1). As rotas aqui
// permanecem completas — itens removidos do menu (ex.: reports, links, wallet-expedition)
// continuam acessíveis por deep-link, sem virar link morto na sidebar.
import { Navigate, Route, Routes } from 'react-router-dom'
import { getToken } from './api'
import Layout from './components/Layout'
import ErrorBoundary from './components/ErrorBoundary'
import CookieConsent from './components/CookieConsent'
import { ToastProvider } from './hooks/useToast'

import Dashboard from './pages/Dashboard'
// NAV UNIFICADO — a tela canônica de Pedidos agora é a cópia self-contained da tela
// admin (admin-orders/), idêntica ao painel admin, escopada por papel no servidor.
// A antiga pages/Orders.tsx foi removida. As rotas /orders e /motoboy renderizam esta.
// NAV UNIFICADO: a antiga tela Motoboy (Cash On Delivery) foi consolidada na tela
// canônica de Pedidos (Orders, role-aware: lista /portal/motoboy p/ produtor/operator
// e /portal/orders p/ afiliado). O arquivo Motoboy.tsx permanece órfão (não importado);
// a rota /motoboy aponta p/ Orders p/ preservar deep-links antigos.
import MotoboysDia from './pages/MotoboysDia'
import Expedicao from './pages/Expedicao'
import OrdersHub from './pages/OrdersHub'
import Products from './pages/Products'
import Stock from './pages/Stock'
import Vitrine from './pages/Vitrine'
import Affiliates from './pages/Affiliates'
import Links from './pages/Links'
import FinanceiroHub from './pages/FinanceiroHub'
import Saques from './pages/Saques'
import Reports from './pages/Reports'
import PlataformaHub from './pages/PlataformaHub'
import Support from './pages/Support'
import ResetPassword from './pages/ResetPassword'

// LOGIN-UNICO (FEAT-RBAC) — o portal NÃO tem mais tela de login própria. A página
// única de login vive na RAIZ da origem (/), servida pelo admin-ui. Quando não há
// token, redirecionamos o BROWSER para '/' (hard nav fora do basename /portal — um
// react-router <Navigate> ficaria preso em /portal e causaria loop). Renderizamos
// null enquanto o browser navega.
function RedirectToRootLogin() {
  // assign('/') sai do basename /portal/ e abre a página única de login na raiz.
  if (typeof window !== 'undefined') window.location.assign('/')
  return null
}

function Protected({ children }: { children: JSX.Element }) {
  return getToken() ? children : <RedirectToRootLogin />
}

export default function App() {
  return (
    <ToastProvider>
      <ErrorBoundary>
        {/* Banner LGPD de cookies/dados — global (login + app), 1ª visita. */}
        <CookieConsent />
        <Routes>
          {/* LOGIN-UNICO — /portal/login não existe mais como tela própria: qualquer
              acesso direto é redirecionado para a página única de login na raiz (/). */}
          <Route path="/login" element={<RedirectToRootLogin />} />
          {/* AUDIT-2026-07-31 (dono: reset de senha do Gabriel) — ResetPassword.tsx
              existia, funcional, mas NUNCA foi registrado aqui: link do e-mail de
              reset (enviarEmailReset em auth.go, /reset-password?token=...) sempre
              caía fora de rota, perdendo o token. Fora de <Protected> — precisa
              funcionar SEM sessão ativa (é assim que o usuário chega, deslogado). */}
          <Route path="/reset-password" element={<ResetPassword />} />
          <Route element={<Protected><Layout /></Protected>}>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<Dashboard />} />
            <Route path="/orders" element={<OrdersHub />} />
            {/* /motoboy → tela unificada de Pedidos (Orders role-aware). Deep-link legado. */}
            <Route path="/motoboy" element={<OrdersHub />} />
            <Route path="/motoboys-dia" element={<MotoboysDia />} />
            <Route path="/expedicao" element={<Expedicao />} />
            <Route path="/products" element={<Products />} />
            <Route path="/stock" element={<Stock />} />
            <Route path="/vitrine" element={<Vitrine />} />
            <Route path="/affiliates" element={<Affiliates />} />
            <Route path="/links" element={<Links />} />
            <Route path="/wallet" element={<FinanceiroHub initialTab="carteira" />} />
            <Route path="/saques" element={<Saques />} />
            <Route path="/wallet-expedition" element={<FinanceiroHub initialTab="expedicao" />} />
            <Route path="/webhooks" element={<PlataformaHub initialTab="webhooks" />} />
            <Route path="/reports" element={<Reports />} />
            <Route path="/integrations" element={<PlataformaHub initialTab="integracoes" />} />
            <Route path="/freight" element={<PlataformaHub initialTab="freight" />} />
            <Route path="/localidades" element={<PlataformaHub initialTab="localidades" />} />
            <Route path="/users" element={<PlataformaHub initialTab="users" />} />
            <Route path="/settings" element={<PlataformaHub initialTab="settings" />} />
            <Route path="/support" element={<Support />} />
            <Route path="/label-credits" element={<FinanceiroHub initialTab="creditos" />} />
            <Route path="*" element={<Navigate to="/dashboard" replace />} />
          </Route>
        </Routes>
      </ErrorBoundary>
    </ToastProvider>
  )
}
