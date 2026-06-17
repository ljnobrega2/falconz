import { useState, useEffect } from 'react'
import { NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import { api, clearToken, getToken } from '../api'

type NavGroup = { kicker: string; items: { to: string; label: string; icon: JSX.Element }[] }

const ICONS = {
  dashboard:   <svg viewBox="0 0 20 20"><path d="M3 3h6v8H3zM11 3h6v5h-6zM11 10h6v7h-6zM3 13h6v4H3z"/></svg>,
  users:       <svg viewBox="0 0 20 20"><path d="M10 9a3.2 3.2 0 1 0-.01-6.41A3.2 3.2 0 0 0 10 9zm0 2c-3.4 0-6 1.8-6 4v2h12v-2c0-2.2-2.6-4-6-4z"/></svg>,
  affiliate:   <svg viewBox="0 0 20 20"><path d="M7 9a3 3 0 1 0-.01-6.01A3 3 0 0 0 7 9zm6 1a2.5 2.5 0 1 0-.01-5.01A2.5 2.5 0 0 0 13 10zM2 17v-1.5C2 13.6 4.2 12 7 12s5 1.6 5 3.5V17H2zm12 0v-1.5c0-1-.4-1.9-1.1-2.6.7-.3 1.4-.4 2.1-.4 2.2 0 4 1.3 4 3V17h-5z"/></svg>,
  commissions: <svg viewBox="0 0 20 20"><path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm1 11H9v-4h2v4zm0-6H9V5h2v2z"/></svg>,
  motoboy:     <svg viewBox="0 0 20 20"><path d="M5 14a2.5 2.5 0 1 0 0 .01zM15 14a2.5 2.5 0 1 0 0 .01zM11 5h3l3 4v4h-2a3 3 0 0 0-6 0H8a3 3 0 0 0-5.4-1.8L2 9l4-1 2-3h3z"/></svg>,
  orders:      <svg viewBox="0 0 20 20"><path d="M4 3h12a1 1 0 0 1 1 1v13l-3-2-2 2-2-2-2 2-2-2-3 2V4a1 1 0 0 1 1-1zm2 4v2h8V7H6zm0 4v2h6v-2H6z"/></svg>,
  cds:         <svg viewBox="0 0 20 20"><path d="M10 2 3 5.5v9L10 18l7-3.5v-9L10 2zm0 2.2 4.6 2.3L10 8.8 5.4 6.5 10 4.2zM5 8.1l4 2v5.3l-4-2V8.1zm10 0v5.3l-4 2v-5.3l4-2z"/></svg>,
  zonas:       <svg viewBox="0 0 20 20"><path d="M10 2C6.7 2 4 4.7 4 8c0 4.5 6 10 6 10s6-5.5 6-10c0-3.3-2.7-6-6-6zm0 8a2 2 0 1 1 0-4 2 2 0 0 1 0 4z"/></svg>,
  mbday:       <svg viewBox="0 0 20 20"><path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm.5 4.5v4l3 1.8-.8 1.3L9 11.5V6.5h1.5z"/></svg>,
  wallet:      <svg viewBox="0 0 20 20"><path d="M3 5a2 2 0 0 1 2-2h10v3H5a2 2 0 0 1-2-1zm0 2.5V15a2 2 0 0 0 2 2h12V7.5H3zM14 12a1.2 1.2 0 1 1 0 .01z"/></svg>,
  pix:         <svg viewBox="0 0 20 20"><path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm0 2 3 3h-2v2.5h-2V7H7l3-3zm-3 8h2v2.5h2V12h2l-3 3-3-3z"/></svg>,
  labels:      <svg viewBox="0 0 20 20"><path d="M2 6l6-4h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H8L2 10V6zm5 2a1 1 0 1 0 0 2 1 1 0 0 0 0-2z"/></svg>,
  settings:    <svg viewBox="0 0 20 20"><path d="m11.4 2 .5 2.1c.5.2 1 .4 1.4.7l2-.9 1.4 2.4-1.6 1.5c.1.5.1 1 0 1.4l1.6 1.5-1.4 2.4-2-.9c-.4.3-.9.5-1.4.7l-.5 2.1H8.6l-.5-2.1a6 6 0 0 1-1.4-.7l-2 .9-1.4-2.4 1.6-1.5a6 6 0 0 1 0-1.4L3.3 6.3 4.7 4l2 .9c.4-.3.9-.5 1.4-.7L8.6 2h2.8zM10 7.5A2.5 2.5 0 1 0 10 12.5 2.5 2.5 0 0 0 10 7.5z"/></svg>,
  webhooks:    <svg viewBox="0 0 20 20"><path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zM8 13l-3-3 1.4-1.4L8 10.2l5.6-5.6L15 6l-7 7z"/></svg>,
  logs:        <svg viewBox="0 0 20 20"><path d="M4 16h2V8H4v8zm5 0h2V4H9v12zm5 0h2v-6h-2v6zM3 18h14v1.5H3z"/></svg>,
  tools:       <svg viewBox="0 0 20 20"><path d="M3 4h5v2H5v10h2v2H3V4zm14 0v14h-4v-2h2V6h-2V4h4zM8 9h4v2H8z"/></svg>,
  audit:       <svg viewBox="0 0 20 20"><path d="M10 1a9 9 0 1 0 0 18 9 9 0 0 0 0-18zm.9 13.4h-1.8v-1.8h1.8v1.8zm0-3.6h-1.8V5.6h1.8v5.2z"/></svg>,
  fechamento:  <svg viewBox="0 0 20 20"><path d="M4 3h12a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1zm2 4v2h8V7H6zm0 4v2h5v-2H6z"/></svg>,
  maintenance: <svg viewBox="0 0 20 20"><path d="m13 2 5 5-3 3-2-2-7 7H3v-3l7-7-2-2zm-1.4 6.4 1.5-1.5L11.7 5.5 10.2 7l1.4 1.4z"/></svg>,
  cron:        <svg viewBox="0 0 20 20"><path d="M10 2a8 8 0 1 1 0 16 8 8 0 0 1 0-16zm.6 4H9v5.4l4.3 2.6.7-1.2-3.4-2V6z"/></svg>,
  onboarding:  <svg viewBox="0 0 20 20"><path d="M10 2 3 6v4c0 4 3 7 7 8 4-1 7-4 7-8V6l-7-4zm0 5a2 2 0 1 1 0 4 2 2 0 0 1 0-4zm-4 8c0-1.6 1.7-3 4-3s4 1.4 4 3v.5H6V15z"/></svg>,
  rules:       <svg viewBox="0 0 20 20"><path d="M5 2h10v3H5zM5 7h10v3H5zM5 12h10v3H5zM5 17h10v1H5z"/></svg>,
}

const NAV_GROUPS: NavGroup[] = [
  {
    kicker: '',
    items: [{ to: '/', label: 'Dashboard', icon: ICONS.dashboard }],
  },
  {
    kicker: 'Usuários',
    items: [
      { to: '/users',       label: 'Usuários',  icon: ICONS.users },
      { to: '/affiliates',  label: 'Afiliados', icon: ICONS.affiliate },
      { to: '/commissions', label: 'Comissões', icon: ICONS.commissions },
      { to: '/products',    label: 'Produtos',  icon: ICONS.tools },
    ],
  },
  {
    kicker: 'Cash on Delivery',
    items: [
      { to: '/motoboy-dashboard',       label: 'Dashboard Motoboy',  icon: ICONS.dashboard },
      { to: '/motoboys',                label: 'Motoboys',           icon: ICONS.motoboy },
      { to: '/motoboys-dia',            label: 'Motoboys do Dia',    icon: ICONS.mbday },
      { to: '/orders',                  label: 'Pedidos',            icon: ICONS.orders },
      { to: '/motoboy-etiquetas',       label: 'Etiquetas/QR',       icon: ICONS.labels },
      { to: '/bulk-actions',            label: 'Ações em Lote',      icon: ICONS.labels },
      { to: '/motoboy-comprovantes',    label: 'Comprovantes',       icon: ICONS.labels },
      { to: '/motoboy-carteira',        label: 'Carteira Motoboy',   icon: ICONS.wallet },
      { to: '/motoboy-saques',          label: 'Saques Motoboy',     icon: ICONS.wallet },
      { to: '/motoboy-custodia',        label: 'Custódia',           icon: ICONS.cds },
      { to: '/motoboy-conciliacao',     label: 'Conciliação',        icon: ICONS.commissions },
      { to: '/motoboy-fechamento',      label: 'Fechamento',         icon: ICONS.fechamento },
      { to: '/motoboy-mapa',            label: 'Mapa Ao Vivo',       icon: ICONS.zonas },
      { to: '/motoboy-config',          label: 'Config COD',         icon: ICONS.settings },
      { to: '/cod-livro',               label: 'Livro COD',          icon: ICONS.commissions },
      { to: '/cod-saques',              label: 'Saques COD',         icon: ICONS.wallet },
      { to: '/cod-taxas',               label: 'Taxas COD',          icon: ICONS.settings },
      { to: '/cod-wallet-producer',     label: 'Wallet Produtor',    icon: ICONS.wallet },
      { to: '/cod-wallet-transactions', label: 'Transações COD',     icon: ICONS.commissions },
    ],
  },
  {
    kicker: 'Logística',
    items: [
      { to: '/cds',   label: 'CDs',          icon: ICONS.cds },
      { to: '/zonas', label: 'Zonas / CEPs', icon: ICONS.zonas },
    ],
  },
  {
    kicker: 'Expedição (Melhor Envio)',
    items: [
      { to: '/labels',                label: 'Etiquetas ME',       icon: ICONS.labels },
      { to: '/expedicao-integracoes', label: 'Markup / Integ.',    icon: ICONS.settings },
      { to: '/expedicao-webhooks',    label: 'Webhooks Expedição', icon: ICONS.webhooks },
      { to: '/tracking-brand',        label: 'Tracking Brand',     icon: ICONS.webhooks },
    ],
  },
  {
    kicker: 'Financeiro',
    items: [
      { to: '/wallet',            label: 'Carteiras',          icon: ICONS.wallet },
      { to: '/pix',               label: 'PIX',                icon: ICONS.pix },
      { to: '/tpc-clientes',      label: 'Carteira Expedição', icon: ICONS.wallet },
      { to: '/tpc-transacoes',    label: 'Transações Expedição', icon: ICONS.commissions },
      { to: '/tpc-config',        label: 'Config Expedição',   icon: ICONS.settings },
      { to: '/affiliates-wallet', label: 'Carteira Afiliados', icon: ICONS.affiliate },
      { to: '/affiliate-rules',   label: 'Regras Afiliados',   icon: ICONS.rules },
      { to: '/audit',             label: 'Auditoria',          icon: ICONS.audit },
      { to: '/audit-log',         label: 'Log Auditoria',      icon: ICONS.logs },
    ],
  },
  {
    kicker: 'Notificações',
    items: [
      { to: '/notificacoes-pwa', label: 'Templates PWA', icon: ICONS.webhooks },
      { to: '/push-tecnico',     label: 'Push Técnico',  icon: ICONS.cron },
    ],
  },
  {
    kicker: 'Sistema',
    items: [
      { to: '/settings',                 label: 'Configurações', icon: ICONS.settings },
      { to: '/maintenance',              label: 'Manutenção',    icon: ICONS.maintenance },
      { to: '/crons',                    label: 'Crons',         icon: ICONS.cron },
      { to: '/pwa-config',               label: 'PWA Config',    icon: ICONS.tools },
      { to: '/capabilities',             label: 'Capabilities',  icon: ICONS.tools },
      { to: '/order-meta-normalization', label: 'Order Meta',    icon: ICONS.tools },
      { to: '/api-docs',                 label: 'API Docs',      icon: ICONS.logs },
      { to: '/logs',                     label: 'Logs',          icon: ICONS.logs },
      { to: '/tools',                    label: 'Ferramentas',   icon: ICONS.tools },
    ],
  },
]

const PAGE_TITLES: Record<string, string> = {
  '/':             'Dashboard',
  '/users':        'Usuários',
  '/affiliates':   'Afiliados',
  '/commissions':  'Comissões',
  '/motoboys':     'Motoboys',
  '/orders':       'Pedidos',
  '/motoboys-dia': 'Motoboys do Dia',
  '/cds':          'Centros de Distribuição',
  '/zonas':        'Zonas / CEPs',
  '/wallet':       'Carteiras',
  '/pix':          'PIX / Recargas',
  '/labels':       'Etiquetas ME',
  '/settings':     'Configurações',
  '/logs':         'Logs do Sistema',
  '/tools':                'Ferramentas',
  '/audit':                'Auditoria Financeira',
  '/cod-livro':            'Livro COD',
  '/cod-saques':           'Saques',
  '/cod-taxas':            'Taxas de Entrega',
  '/tpc-clientes':         'Carteira Expedição',
  '/affiliates-wallet':    'Carteira de Afiliados',
  '/motoboy-dashboard':    'Dashboard Motoboy',
  '/motoboy-carteira':     'Carteira Motoboy',
  '/motoboy-fechamento':   'Fechamento Diário Motoboy',
  '/motoboy-config':       'Configurações COD',
  '/tpc-transacoes':       'Transações Expedição',
  '/tpc-config':           'Configurações Expedição',
  '/maintenance':          'Modo Manutenção',
  '/crons':                'Status dos Crons',
  '/audit-log':            'Log de Auditoria',
  '/affiliate-rules':         'Regras de Afiliados',
  '/expedicao-integracoes':   'Markup / Integrações',
  '/expedicao-webhooks':      'Webhooks de Expedição',
  '/notificacoes-pwa':        'Templates de Notificação PWA',
  '/motoboy-etiquetas':       'Etiquetas / QR Code',
  '/motoboy-comprovantes':    'Comprovantes de Entrega',
  '/motoboy-saques':          'Saques Motoboy',
  '/motoboy-custodia':        'Custódia / Estoque Motoboy',
  '/motoboy-conciliacao':     'Conciliação Bancária',
  '/cod-wallet-producer':     'Carteira COD — Produtor',
  '/cod-wallet-transactions':  'Transações COD Wallet',
  '/tracking-brand':           'Tracking Brand (por Classe)',
  '/api-docs':                 'Documentação da API',
  '/push-tecnico':             'Push Técnico (VAPID)',
  '/capabilities':             'Capabilities / Permissões',
  '/order-meta-normalization': 'Normalização de Order Meta',
  '/pwa-config':               'Configurações PWA',
  '/bulk-actions':             'Ações em Lote (Etiquetas)',
  '/motoboy-mapa':             'Mapa Ao Vivo — Motoboys',
  '/products':                 'Produtos',
}

const LOGO_SVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 180 36" fill="none" height="28">
  <rect x="0" y="4" width="28" height="28" rx="7" fill="#E8650A"/>
  <text x="14" y="23" text-anchor="middle" font-family="system-ui" font-size="15" font-weight="800" fill="#fff">SZ</text>
  <text x="42" y="26" font-family="system-ui" font-size="18" font-weight="700" fill="currentColor">Senderzz</text>
  <text x="138" y="20" font-family="system-ui" font-size="8" font-weight="700" fill="#E8650A">ADMIN</text>
</svg>`

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const [sidebar, setSidebar] = useState<'open' | 'collapsed'>('open')
  const [theme, setTheme] = useState<'light' | 'dark'>('light')
  const [adminName, setAdminName] = useState('Admin')
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    const defaults: Record<string, boolean> = {}
    NAV_GROUPS.forEach(g => { if (g.kicker) defaults[g.kicker] = true })
    return defaults
  })

  useEffect(() => {
    const saved = localStorage.getItem('szAdminSidebar') as 'open' | 'collapsed' | null
    if (saved) setSidebar(saved)
    const savedTheme = localStorage.getItem('szAdminTheme') as 'light' | 'dark' | null
    if (savedTheme) setTheme(savedTheme)
    const savedOpen = localStorage.getItem('szAdminMenuOpen')
    if (savedOpen) {
      try {
        const parsed = JSON.parse(savedOpen) as Record<string, boolean>
        setOpenGroups(prev => ({ ...prev, ...parsed }))
      } catch { /* ignore */ }
    }
    if (getToken()) {
      api<{ nome: string }>('/me').then(r => setAdminName(r.nome?.split(' ')[0] || 'Admin')).catch(() => {})
    }
  }, [])

  function toggleGroup(kicker: string) {
    setOpenGroups(prev => {
      const next = { ...prev, [kicker]: !prev[kicker] }
      try { localStorage.setItem('szAdminMenuOpen', JSON.stringify(next)) } catch { /* ignore */ }
      return next
    })
  }

  function toggleSidebar() {
    const next = sidebar === 'open' ? 'collapsed' : 'open'
    setSidebar(next)
    localStorage.setItem('szAdminSidebar', next)
  }

  function toggleTheme() {
    const next = theme === 'light' ? 'dark' : 'light'
    setTheme(next)
    localStorage.setItem('szAdminTheme', next)
  }

  function logout() { clearToken(); navigate('/login') }

  const title = PAGE_TITLES[location.pathname] ?? 'Admin'

  return (
    <div className="sz-root sz-dashboard-v2" data-theme={theme} data-sidebar={sidebar}>
      {/* ── Sidebar ─────────────────────────────────────────── */}
      <aside className="szv2-sidebar" aria-label="Navegação principal">
        <div className="szv2-sidebar-head">
          <span className="szv2-logo-full" dangerouslySetInnerHTML={{ __html: LOGO_SVG }} />
          <span className="szv2-logo-mark" aria-hidden="true">
            <svg viewBox="0 0 36 36" fill="none"><rect width="36" height="36" rx="9" fill="#E8650A"/><text x="18" y="24" textAnchor="middle" fontFamily="system-ui" fontSize="14" fontWeight="800" fill="#fff">SZ</text></svg>
          </span>
        </div>

        <div className="szv2-sidebar-hello">
          <strong>Olá, {adminName}</strong>
          <span>Painel administrativo</span>
        </div>

        <nav className="szv2-nav">
          {NAV_GROUPS.map((g, gi) => {
            const isOpen = g.kicker ? (openGroups[g.kicker] ?? true) : true
            return (
              <div key={gi} className="szv2-nav-group" data-open={isOpen ? '1' : '0'}>
                {g.kicker && (
                  <button
                    type="button"
                    className="szv2-nav-kicker szv2-nav-kicker-btn"
                    onClick={() => toggleGroup(g.kicker)}
                    aria-expanded={isOpen}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      width: '100%',
                      background: 'transparent',
                      border: 0,
                      padding: '6px 12px',
                      cursor: 'pointer',
                      font: 'inherit',
                      color: 'inherit',
                      textAlign: 'left',
                    }}
                  >
                    <span>{g.kicker}</span>
                    <span aria-hidden="true" style={{ fontSize: '10px', opacity: 0.7 }}>{isOpen ? '▾' : '▸'}</span>
                  </button>
                )}
                {isOpen && g.items.map(item => (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.to === '/'}
                    title={item.label}
                    className={({ isActive }) => 'sz-ni' + (isActive ? ' sz-ni-on' : '')}
                  >
                    <span className="szv2-ni-icon" aria-hidden="true">{item.icon}</span>
                    <span className="szv2-ni-label">{item.label}</span>
                  </NavLink>
                ))}
              </div>
            )
          })}
        </nav>

        <div className="szv2-sidebar-foot">
          <button type="button" className="szv2-sidebar-foot-btn" onClick={toggleTheme} title="Alternar tema">
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3a7 7 0 0 0 0 14c1.2 0 2.4-.3 3.4-.9A7 7 0 0 1 10 3z"/></svg>
            <span>Tema</span>
          </button>
          <button type="button" className="szv2-sidebar-foot-btn" onClick={logout} title="Sair">
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M8 3h5a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H8v-2h5V5H8V3zm1 5 3 2-3 2v-1.2H3V9.2h6V8z"/></svg>
            <span>Sair</span>
          </button>
        </div>
      </aside>

      {/* ── Main ────────────────────────────────────────────── */}
      <div className="szv2-main">
        <header className="szv2-topbar">
          <button type="button" className="szv2-topbar-toggle" onClick={toggleSidebar} aria-label="Recolher menu">
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 5h14v2H3zM3 9h14v2H3zM3 13h14v2H3z"/></svg>
          </button>
          <h1 className="szv2-topbar-title">{title}</h1>
          <div className="szv2-topbar-spacer" />
          <div className="szv2-topbar-actions">
            <span className="szv2-beta-pill">ADMIN</span>
            <div className="szv2-avatar" title={adminName}>{adminName.charAt(0).toUpperCase()}</div>
          </div>
        </header>

        <main className="szv2-content" id="szv2-content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
