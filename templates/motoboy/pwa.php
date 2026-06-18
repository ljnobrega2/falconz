<?php
/**
 * Senderzz — PWA do Motoboy
 * Acessível em: /motoboy-app/
 * Registrado via add_rewrite_rule no módulo motoboy
 */
if ( ! defined( 'ABSPATH' ) ) exit;

// Página standalone — sem header/footer WP
header('Content-Type: text/html; charset=utf-8');
// Desabilita cache do LiteSpeed/CDN — PWA é dinâmico e personalizado por motoboy
header('X-LiteSpeed-Cache-Control: no-cache');
header('Cache-Control: no-store, no-cache, must-revalidate, max-age=0');
header('Pragma: no-cache');
?>
<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="default">
<meta name="apple-mobile-web-app-title" content="Senderzz Motoboy">
<meta name="theme-color" content="#E8650A">
<title>Senderzz — Motoboy</title>
<meta name="senderzz-motoboy-build" content="v323-pwa-em-rota-no-cache-confirmacao">
<style>
  :root{--sz-font:-apple-system, BlinkMacSystemFont, "SF Pro Text", "SF Pro Display", system-ui, sans-serif;--sz-orange:#E8650A;--sz-orange-hover:#C94F06;--sz-bg:#F8FAFC;--sz-card:#FFFFFF;--sz-border:#E6EAF0;--sz-text:#111827;--sz-muted:#64748B;--sz-radius-md:12px;--sz-radius-lg:16px;--sz-shadow-focus:0 0 0 3px rgba(232,101,10,.14)}
  *{box-sizing:border-box;margin:0;padding:0;-webkit-tap-highlight-color:transparent}
  body{font-family:var(--sz-font);background:#f9fafb;color:#111827;min-height:100dvh}
  .app{display:none;flex-direction:column;min-height:100dvh}
  .app.visible{display:flex}

  /* Header */
  .header{background:#E8650A;color:#fff;padding:14px 16px;display:flex;align-items:center;gap:10px;position:sticky;top:0;z-index:101;box-shadow:0 2px 8px rgba(0,0,0,.15)}
  .header-logo{font-size:var(--sz-text-lg);font-weight:700;letter-spacing:-.015em}
  .header-sub{font-size:var(--sz-text-meta);opacity:.85}
  .header-info{margin-left:auto;text-align:right;font-size:var(--sz-text-meta);opacity:.9}

  /* Bottom nav */
  .bottom-nav{background:#fff;border-top:1px solid #e5e7eb;display:flex;position:sticky;bottom:0;z-index:100}
  .nav-btn{flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:10px 4px;gap:3px;font-size:var(--sz-text-xs);font-weight:600;color:#9ca3af;cursor:pointer;border:none;background:none;transition:color .2s}
  .nav-btn.active{color:#E8650A}
  .nav-btn svg{width:22px;height:22px}

  /* Screens */
  .screen{display:none;flex:1;padding:16px;overflow-y:auto}
  .screen.active{display:block}

  /* Cards */
  .card{background:#fff;border-radius:14px;padding:16px;margin-bottom:12px;box-shadow:0 1px 4px rgba(0,0,0,.06);border:1px solid #f3f4f6}
  .card-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:10px}
  .card-title{font-weight:700;font-size:var(--sz-text-lg)}
  .card-sub{font-size:var(--sz-text-meta);color:#6b7280;margin-top:2px}

  /* Status badges */
  .badge{display:inline-block;padding:4px 10px;border-radius:999px;font-size:var(--sz-text-sm);font-weight:700}
  .badge-aprovado{background:#dcfce7;color:#166534}
  .badge-embalado{background:#ede9fe;color:#5b21b6}
  .badge-em_rota{background:#dbeafe;color:#1e40af}
  .badge-entregue{background:#d1fae5;color:#065f46}
  .badge-frustrado{background:#fee2e2;color:#991b1b}

  /* Buttons */
  .btn{width:100%;padding:13px;border-radius:12px;border:none;font-size:var(--sz-text-lg);font-weight:700;cursor:pointer;margin-top:8px;transition:opacity .2s}
  .btn:active{opacity:.8}
  .btn-primary{background:#E8650A;color:#fff}
  .btn-success{background:#EA580C;color:#fff}
  .btn-danger{background:#dc2626;color:#fff}
  .btn-outline{background:#fff;color:#374151;border:1.5px solid #d1d5db}
  .btn-sm{padding:8px 14px;font-size:var(--sz-text-base);width:auto;margin:0}

  /* Forms */
  .form-group{margin-bottom:14px}
  .form-label{display:block;font-size:var(--sz-text-base);font-weight:600;color:#374151;margin-bottom:6px}
  .form-input{width:100%;padding:11px 12px;border:1.5px solid #d1d5db;border-radius:10px;font-size:var(--sz-text-lg);outline:none}
  .form-input:focus{border-color:#E8650A}

  /* Stats filtro */
  .stats-grid{display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin-bottom:16px}
  .stat-box{background:#fff;border-radius:12px;padding:10px 6px;text-align:center;border:2px solid #f3f4f6;box-shadow:0 1px 3px rgba(0,0,0,.04);cursor:pointer;transition:all .18s;user-select:none;-webkit-user-select:none}
  .stat-box:active{transform:scale(.96)}
  .stat-box.active{border-color:#E8650A;background:#fff7ed}
  .stat-box.active .stat-num{color:#E8650A}
  .stat-box.active .stat-label{color:#c2410c}
  .stat-num{font-size:var(--sz-text-xl);font-weight:700;color:#374151;line-height:1}
  .stat-num.green{color:#16a34a}
  .stat-num.red{color:#dc2626}
  .stat-num.purple{color:#7c3aed}
  .stat-num.blue{color:#2563eb}
  .stat-label{font-size:var(--sz-text-xs);font-weight:700;color:#9ca3af;margin-top:3px;text-transform:none;letter-spacing:0;line-height:1.2}

  /* Card pedido redesign */
  .order-card{background:#fff;border-radius:16px;margin-bottom:10px;box-shadow:0 1px 6px rgba(0,0,0,.07);border:1px solid #f0f0f0;overflow:hidden}
  .order-card-top{padding:14px 14px 10px;display:flex;align-items:flex-start;gap:10px}
  .order-num{background:#f9fafb;border:1px solid #e5e7eb;border-radius:8px;padding:3px 8px;font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;white-space:nowrap;flex-shrink:0;margin-top:1px}
  .order-info{flex:1;min-width:0}
  .order-name{font-size:var(--sz-text-lg);font-weight:700;color:#111827;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .order-addr{font-size:var(--sz-text-meta);color:#6b7280;margin-top:2px;overflow:hidden;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical}
  .order-product{font-size:var(--sz-text-meta);color:#E8650A;font-weight:700;margin-top:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .order-meta{display:flex;align-items:center;gap:8px;margin-top:6px;flex-wrap:wrap}
  .order-valor{font-size:var(--sz-text-lg);font-weight:700;color:#111827;letter-spacing:-.02em}
  .order-qty{font-size:var(--sz-text-sm);font-weight:700;color:#6b7280;background:#f3f4f6;border-radius:6px;padding:2px 7px}
  .order-status-pill{font-size:var(--sz-text-xs);font-weight:700;padding:3px 9px;border-radius:999px;white-space:nowrap}
  .pill-embalado{background:#ede9fe;color:#5b21b6}
  .pill-em_rota{background:#dbeafe;color:#1e40af}
  .pill-a_caminho{background:#e0f2fe;color:#0369a1}
  .pill-entregue{background:#dcfce7;color:#166534}
  .pill-frustrado{background:#fee2e2;color:#991b1b}
  .order-card-actions{border-top:1px solid #f3f4f6;padding:10px 12px;display:flex;gap:8px}
  .act-btn{flex:1;height:44px;min-height:44px;border:none;border-radius:10px;font-size:var(--sz-text-base);font-weight:700;cursor:pointer;display:flex;align-items:center;justify-content:center;gap:5px;transition:opacity .15s}
  .act-btn:active{opacity:.75}
  .act-wa{background:#25d366;color:#fff}
  .act-ok{background:#EA580C;color:#fff}
  .act-fail{background:#dc2626;color:#fff}
  .act-btn svg{width:16px;height:16px}

  /* Bipar QR btn */
  .btn-iniciar{width:100%;padding:15px;border-radius:14px;border:none;background:linear-gradient(135deg,#E8650A,#f59e0b);color:#fff;font-size:var(--sz-text-lg);font-weight:700;cursor:pointer;margin-bottom:14px;box-shadow:0 4px 14px rgba(232,101,10,.3);letter-spacing:-.015em;transition:opacity .2s}
  .btn-iniciar:active{opacity:.85}

  /* Seleção em massa */
  .sel-bar{display:none;align-items:center;justify-content:space-between;background:#fff;border:1.5px solid #E8650A;border-radius:12px;padding:10px 14px;margin-bottom:10px;gap:10px}
  .sel-bar.visible{display:flex}
  .sel-bar-left{display:flex;align-items:center;gap:10px;font-size:var(--sz-text-base);font-weight:700;color:#374151}
  .sel-all-btn{background:none;border:1.5px solid #d1d5db;border-radius:8px;padding:4px 10px;font-size:var(--sz-text-meta);font-weight:700;color:#374151;cursor:pointer;white-space:nowrap}
  .sel-all-btn:active{opacity:.7}
  .sel-count{font-size:var(--sz-text-base);font-weight:700;color:#E8650A}
  .btn-iniciar-sel{background:#E8650A;color:#fff;border:none;border-radius:10px;padding:9px 18px;font-size:var(--sz-text-base);font-weight:700;cursor:pointer;white-space:nowrap;transition:opacity .2s}
  .btn-iniciar-sel:active{opacity:.8}
  .btn-iniciar-sel:disabled{opacity:.4;cursor:not-allowed}

  /* Checkbox no card */
  .order-check-wrap{display:none;align-items:center;justify-content:center;width:28px;height:28px;flex-shrink:0}
  .order-check-wrap.show{display:flex}
  .order-check{width:22px;height:22px;border:2px solid #d1d5db;border-radius:6px;background:#fff;cursor:pointer;display:flex;align-items:center;justify-content:center;transition:all .15s;flex-shrink:0}
  .order-check.checked{background:#E8650A;border-color:#E8650A}
  .order-check.checked::after{content:'✓';color:#fff;font-size:var(--sz-text-base);font-weight:700}
  .order-card.selected{border-color:#E8650A;box-shadow:0 0 0 2px rgba(232,101,10,.15)}

  /* Btn individual rota */
  .act-rota{background:#7c3aed;color:#fff}

  /* Filter label */
  .filter-label{font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;margin-bottom:10px;display:flex;align-items:center;gap:6px}
  .filter-label span{background:#E8650A;color:#fff;border-radius:99px;padding:1px 8px;font-size:var(--sz-text-xs)}

  /* Login */
  .login-screen{display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:100dvh;padding:32px 24px;background:linear-gradient(135deg,#E8650A 0%,#c8540a 100%)}
  .login-logo{font-size:var(--sz-text-hero);font-weight:700;color:#fff;letter-spacing:-.015em;margin-bottom:4px}
  .login-sub{color:rgba(255,255,255,.8);font-size:var(--sz-text-md);margin-bottom:32px}
  .login-card{background:#fff;border-radius:20px;padding:24px;width:100%;max-width:380px;box-shadow:0 20px 40px rgba(0,0,0,.2)}
  .login-title{font-size:var(--sz-text-lg);font-weight:700;margin-bottom:20px;color:#111827}

  /* Assinatura */
  .sig-wrap{position:relative}
  .sig-clear{position:absolute;top:8px;right:8px;background:#fee2e2;color:#991b1b;border:none;border-radius:8px;padding:4px 10px;font-size:var(--sz-text-meta);font-weight:700;cursor:pointer}

  /* Foto */
  .foto-preview{width:100%;border-radius:12px;object-fit:cover;max-height:200px;display:none;margin-top:8px}

  /* Loading */
  .loading{display:flex;align-items:center;justify-content:center;padding:40px;color:#9ca3af;font-size:var(--sz-text-md)}
  .spinner{width:20px;height:20px;border:2px solid #e5e7eb;border-top-color:#E8650A;border-radius:50%;animation:spin .7s linear infinite;margin-right:8px}
  @keyframes spin{to{transform:rotate(360deg)}}

  /* Toast */
  .toast{position:fixed;top:80px;left:50%;transform:translateX(-50%);background:#111827;color:#fff;padding:10px 20px;border-radius:999px;font-size:var(--sz-text-base);font-weight:600;z-index:999;opacity:0;transition:opacity .3s;white-space:nowrap}
  .toast.show{opacity:1}

  /* Modal */
  .modal-backdrop{display:none;position:fixed;inset:0;background:rgba(0,0,0,.5);z-index:200;align-items:flex-end}
  .modal-backdrop.open{display:flex}
  .modal{background:#fff;border-radius:20px 20px 0 0;padding:24px;width:100%;max-height:90dvh;overflow-y:auto}
  .modal-title{font-size:var(--sz-text-lg);font-weight:700;margin-bottom:16px}
  .modal-close{float:right;background:none;border:none;font-size:var(--sz-text-xl);cursor:pointer;color:#6b7280}
  .qr-route-hint{background:#fff7ed;border:1px solid #fed7aa;color:#9a3412;border-radius:14px;padding:12px 14px;margin:0 0 12px;font-size:var(--sz-text-sm);font-weight:700;line-height:1.35}
  .qr-video-wrap{background:#111827;border-radius:14px;overflow:hidden;min-height:260px;display:flex;align-items:center;justify-content:center;margin:12px 0;border:1px solid #1f2937}
  .qr-video-wrap video{width:100%;max-height:340px;object-fit:cover;display:block}
  .qr-help{font-size:var(--sz-text-sm);color:#6b7280;line-height:1.45;margin:8px 0 12px}
  .qr-actions{display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:10px}

  /* Pagamento split */
  .pgto-row{display:grid;grid-template-columns:auto 1fr;gap:10px;align-items:center;margin-bottom:10px}
  .pgto-label{font-size:var(--sz-text-base);font-weight:600;color:#374151;white-space:nowrap}

  .delivery-total{background:#fff7ed;border:1px solid #fed7aa;color:#9a3412;border-radius:12px;padding:10px 12px;font-size:var(--sz-text-base);font-weight:700;margin-bottom:12px;display:flex;justify-content:space-between;gap:12px}
  .delivery-total small{display:block;color:#9ca3af;font-weight:700;margin-top:2px}
  .form-help{font-size:var(--sz-text-meta);color:#6b7280;margin-top:5px}

  /* GPS indicator */
  .gps-dot{width:10px;height:10px;border-radius:50%;background:#16a34a;display:inline-block;margin-right:6px;animation:pulse 2s infinite}
  @keyframes pulse{0%,100%{opacity:1}50%{opacity:.4}}
  .gps-off{background:#ef4444;animation:none}

  .empty-state{text-align:center;padding:48px 24px;color:#9ca3af}
  .empty-icon{font-size:var(--sz-text-hero);margin-bottom:12px}
  .empty-text{font-size:var(--sz-text-lg);font-weight:600;margin-bottom:6px;color:#6b7280}


  /* Senderzz v386 — PWA visual canônico no próprio PWA, sem stylesheet de remendo */
  body.sz-pwa-only .header,.header{background:var(--sz-orange)!important;box-shadow:none!important;border-bottom:1px solid rgba(255,255,255,.16)}
  .card{background:var(--sz-card)!important;border:1px solid var(--sz-border)!important;border-radius:16px!important;box-shadow:none!important}
  .btn,button{background-image:none!important;box-shadow:none!important;border-radius:12px!important;font-weight:600!important}
  .btn.primary,button.primary,.btn.active{background:var(--sz-orange)!important;color:#fff!important;border-color:var(--sz-orange)!important}
  input,select,textarea{font-family:var(--sz-font)!important;border:1px solid var(--sz-border)!important;border-radius:12px!important;box-shadow:none!important}
  input:focus,select:focus,textarea:focus{border-color:var(--sz-orange)!important;box-shadow:var(--sz-shadow-focus)!important;outline:none!important}



/* Senderzz v405 — fonte padrão Apple/SF nativa */
:root,
.sz-root,
#sz-admin-wrap,
.sz-admin-wrap,
.sz-pwa,
.sz-pwa-only,
body.sz-pwa-only,
.sz-checkout,
.sz-mb-date-checkout{--sz-font:-apple-system, BlinkMacSystemFont, "SF Pro Text", "SF Pro Display", system-ui, sans-serif}
html body #sz-admin-wrap,
html body #sz-admin-wrap *:not(.dashicons):not([class^="dashicons"]):not([class*=" dashicons"]):not(.fa):not(.fas):not(.far):not(.fab):not(.material-icons),
html body .sz-root,
html body .sz-root *:not(.dashicons):not([class^="dashicons"]):not([class*=" dashicons"]):not(.fa):not(.fas):not(.far):not(.fab):not(.material-icons),
html body .sz-pwa,
html body .sz-pwa *:not(.dashicons):not([class^="dashicons"]):not([class*=" dashicons"]):not(.fa):not(.fas):not(.far):not(.fab):not(.material-icons),
html body.sz-pwa-only,
html body.sz-pwa-only *:not(.dashicons):not([class^="dashicons"]):not([class*=" dashicons"]):not(.fa):not(.fas):not(.far):not(.fab):not(.material-icons),
html body .sz-checkout,
html body .sz-checkout *:not(.dashicons):not([class^="dashicons"]):not([class*=" dashicons"]):not(.fa):not(.fas):not(.far):not(.fab):not(.material-icons),
html body .sz-mb-date-checkout,
html body .sz-mb-date-checkout *:not(.dashicons):not([class^="dashicons"]):not([class*=" dashicons"]):not(.fa):not(.fas):not(.far):not(.fab):not(.material-icons){font-family:var(--sz-font)!important}

</style>
</head>
<body>
<?php $vapid_pub = get_option('sz_notif_vapid_public',''); ?>
<?php if($vapid_pub): ?><input type="hidden" id="sz-vapid-key" value="<?php echo esc_attr($vapid_pub); ?>"><?php endif; ?>

<!-- LOGIN -->
<div class="login-screen" id="loginScreen">
  <div class="login-logo">🏍️ Senderzz</div>
  <div class="login-sub">Painel do Entregador</div>

  <!-- Passo 1: telefone -->
  <div class="login-card" id="loginStep1">
    <div class="login-title">Entrar</div>
    <div class="form-group">
      <label class="form-label">Celular com DDD</label>
      <input class="form-input" type="tel" id="loginTelefone" placeholder="11999999999"
             inputmode="numeric" autocomplete="tel"
             onkeydown="if(event.key==='Enter')verificarTelefone()">
    </div>
    <button class="btn btn-primary" id="btnVerificarTel" onclick="verificarTelefone()">Continuar</button>
    <div id="loginError" style="color:#dc2626;font-size:var(--sz-text-base);margin-top:10px;display:none"></div>
  </div>

  <!-- Passo 2a: definir senha (primeiro acesso) -->
  <div class="login-card" id="loginStepDefinir" style="display:none">
    <div class="login-title">Criar senha</div>
    <div style="font-size:var(--sz-text-base);color:#6b7280;margin-bottom:14px">
      Primeiro acesso. Defina sua senha de acesso.
    </div>
    <div class="form-group">
      <label class="form-label">Nova senha</label>
      <input class="form-input" type="password" id="loginSenhaNova" placeholder="Mínimo 4 caracteres"
             autocomplete="new-password">
    </div>
    <div class="form-group">
      <label class="form-label">Confirmar senha</label>
      <input class="form-input" type="password" id="loginSenhaConfirm" placeholder="Repita a senha"
             autocomplete="new-password"
             onkeydown="if(event.key==='Enter')definirSenha()">
    </div>
    <button class="btn btn-primary" id="btnDefinirSenha" onclick="definirSenha()">Salvar e entrar</button>
    <button class="btn btn-outline" onclick="voltarStep1()" style="margin-top:8px;width:100%">← Voltar</button>
    <div id="loginDefinirError" style="color:#dc2626;font-size:var(--sz-text-base);margin-top:10px;display:none"></div>
  </div>

  <!-- Passo 2b: senha já existe -->
  <div class="login-card" id="loginStepSenha" style="display:none">
    <div class="login-title">Entrar</div>
    <div style="font-size:var(--sz-text-base);color:#6b7280;margin-bottom:14px" id="loginNomeMb"></div>
    <div class="form-group">
      <label class="form-label">Senha</label>
      <input class="form-input" type="password" id="loginSenha" placeholder="Sua senha"
             autocomplete="current-password"
             onkeydown="if(event.key==='Enter')confirmarSenha()">
    </div>
    <button class="btn btn-primary" id="btnConfirmarSenha" onclick="confirmarSenha()">Entrar</button>
    <button class="btn btn-outline" onclick="voltarStep1()" style="margin-top:8px;width:100%">← Voltar</button>
    <div id="loginSenhaError" style="color:#dc2626;font-size:var(--sz-text-base);margin-top:10px;display:none"></div>
  </div>
</div>

<!-- APP -->
<div class="app" id="app">
  <!-- Header -->
  <div class="header">
    <div>
      <div class="header-logo">🏍️ Senderzz</div>
      <div class="header-sub" id="headerNome">Carregando...</div>
    </div>
    <div class="header-info">
      <span class="gps-dot" id="gpsDot"></span><span id="gpsStatus">GPS</span>
    </div>
  </div>

  <!-- Screens -->
  <div class="screen active" id="screenLote">
    <div class="stats-grid" id="statsGrid">
      <div class="stat-box" id="statBoxEmbalado" onclick="setFiltro('embalado')">
        <div class="stat-num purple" id="statEmbalado">-</div>
        <div class="stat-label">Embalados</div>
      </div>
      <div class="stat-box" id="statBoxEmRota" onclick="setFiltro('em_rota')">
        <div class="stat-num blue" id="statEmRota">-</div>
        <div class="stat-label">Em Rota</div>
      </div>
      <div class="stat-box" id="statBoxEntregue" onclick="setFiltro('entregue')">
        <div class="stat-num green" id="statEntregues">-</div>
        <div class="stat-label">Entregues</div>
      </div>
      <div class="stat-box" id="statBoxFrustrado" onclick="setFiltro('frustrado')">
        <div class="stat-num red" id="statFrustrados">-</div>
        <div class="stat-label">Frustrados</div>
      </div>
    </div>
    <div class="qr-route-hint" id="qrRouteHint" style="display:none">
      📷 Para sair para entrega, abra cada pedido embalado e bipe o QR Code da etiqueta.
    </div>
    <div id="loteList"><div class="loading"><div class="spinner"></div>Carregando...</div></div>
  </div>

  <div class="screen" id="screenFechamento">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
      <span style="font-size:var(--sz-text-meta);color:#9ca3af" id="fechamentoTs"></span>
      <button onclick="loadFechamento(true)" style="background:none;border:1px solid #e5e7eb;border-radius:8px;padding:5px 12px;font-size:var(--sz-text-meta);font-weight:700;color:#374151;cursor:pointer">↻ Atualizar</button>
    </div>
    <div id="fechamentoContent"><div class="loading"><div class="spinner"></div>Carregando...</div></div>
  </div>

  <!-- Carteira -->
  <div class="screen" id="screenCarteira">
    <div id="walletContent">
      <div class="loading"><div class="spinner"></div>Carregando carteira...</div>
    </div>
  </div>

  <!-- Perfil -->
  <div class="screen" id="screenPerfil">
    <div class="card">
      <div class="card-title">💳 Dados bancários</div>
      <p style="font-size:var(--sz-text-base);color:#6b7280;margin:6px 0 14px">Seus pagamentos são realizados toda sexta-feira para a chave PIX cadastrada.</p>
      <div id="perfilMsg" style="display:none;margin-bottom:12px;padding:10px 12px;border-radius:8px;font-size:var(--sz-text-base);font-weight:700"></div>
      <div style="display:flex;flex-direction:column;gap:12px">
        <div>
          <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Nome completo do titular</label>
          <input id="pf-holder" type="text" placeholder="Nome completo" style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none">
        </div>
        <div>
          <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">CPF do titular</label>
          <input id="pf-cpf" type="tel" placeholder="000.000.000-00" maxlength="14" style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none" oninput="mbCpfMask(this)">
        </div>
        <div>
          <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Banco</label>
          <input id="pf-bank" type="text" placeholder="Ex: Nubank, Itaú, Bradesco..." style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none">
        </div>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
          <div>
            <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Agência</label>
            <input id="pf-agency" type="tel" placeholder="0001" style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none">
          </div>
          <div>
            <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Conta</label>
            <input id="pf-account" type="tel" placeholder="00000-0" style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none">
          </div>
        </div>
        <div>
          <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Tipo de chave PIX</label>
          <select id="pf-pix-type" style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none;background:#fff" onchange="mbUpdatePixPlaceholder()">
            <option value="cpf">CPF</option>
            <option value="cnpj">CNPJ</option>
            <option value="email">E-mail</option>
            <option value="telefone">Telefone</option>
            <option value="aleatoria">Chave aleatória</option>
          </select>
        </div>
        <div>
          <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Chave PIX</label>
          <input id="pf-pix-key" type="text" placeholder="Informe sua chave PIX" style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none">
        </div>
        <button class="btn btn-primary" onclick="mbSalvarBancario()" style="margin-top:4px">Salvar dados bancários</button>
      </div>
    </div>
    <div class="card" style="margin-top:0">
      <div class="card-title" style="color:#9ca3af;font-size:var(--sz-text-base)">ℹ️ Sobre os pagamentos</div>
      <p style="font-size:var(--sz-text-base);color:#6b7280;line-height:1.55;margin-top:6px">Seus ganhos são acumulados e pagos toda <strong>sexta-feira</strong>. Não há taxa de saque ou antecipação disponível. O valor é creditado diretamente na chave PIX cadastrada.</p>
    </div>

    <!-- Card trocar senha -->
    <div class="card" style="margin-top:0">
      <div class="card-title">🔒 Alterar senha</div>
      <div id="perfilSenhaMsg" style="display:none;margin-bottom:12px;padding:10px 12px;border-radius:8px;font-size:var(--sz-text-base);font-weight:700"></div>
      <div style="display:flex;flex-direction:column;gap:12px">
        <div>
          <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Senha atual</label>
          <input id="pf-senha-atual" type="password" placeholder="Senha atual" autocomplete="current-password"
                 style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none">
        </div>
        <div>
          <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Nova senha</label>
          <input id="pf-senha-nova" type="password" placeholder="Mínimo 4 caracteres" autocomplete="new-password"
                 style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none">
        </div>
        <div>
          <label style="font-size:var(--sz-text-sm);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;display:block;margin-bottom:6px">Confirmar nova senha</label>
          <input id="pf-senha-confirm" type="password" placeholder="Repita a nova senha" autocomplete="new-password"
                 style="width:100%;height:44px;border:1.5px solid #e5e7eb;border-radius:10px;padding:0 12px;font-size:var(--sz-text-md);font-family:var(--sz-font);outline:none">
        </div>
        <button class="btn btn-primary" onclick="mbTrocarSenha()" style="margin-top:4px">Salvar nova senha</button>
      </div>
    </div>
  </div>

  <!-- Bottom Nav -->
  <div class="bottom-nav">
    <button class="nav-btn active" onclick="switchTab('lote',this)">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="18" rx="2"/><path d="M8 10h8M8 14h5"/></svg>
      Pedidos
    </button>
    <button class="nav-btn" onclick="switchTab('fechamento',this)">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg>
      Fechamento
    </button>
    <button class="nav-btn" onclick="switchTab('carteira',this)">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 7V6a3 3 0 0 0-3-3H5a3 3 0 0 0-3 3v12a3 3 0 0 0 3 3h15a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2Z"/><path d="M16 14h.01"/><path d="M2 7h18"/></svg>
      Carteira
    </button>
    <button class="nav-btn" onclick="switchTab('perfil',this)">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="8" r="4"/><path d="M4 20c0-4 3.6-7 8-7s8 3 8 7"/></svg>
      Perfil
    </button>
  </div>
</div>

<!-- Modal QR Etiqueta -->
<div class="modal-backdrop" id="modalQrScan">
  <div class="modal">
    <button class="modal-close" onclick="szQrClose(null)">✕</button>
    <div class="modal-title">📷 Bipar QR da etiqueta</div>
    <div class="qr-help" id="qrHelp">Aponte a câmera para o QR Code da etiqueta do pacote. Se a câmera não abrir, cole o código impresso.</div>
    <div class="qr-video-wrap"><video id="qrVideo" playsinline muted></video></div>
    <div class="form-group">
      <label class="form-label">Código da etiqueta</label>
      <input class="form-input" id="qrManualInput" placeholder="SZ-1234-99-ABCDEF123456" autocomplete="off" autocorrect="off" autocapitalize="characters" spellcheck="false">
    </div>
    <div class="qr-actions">
      <button class="btn btn-outline" onclick="szQrClose(null)">Cancelar</button>
      <button class="btn btn-primary" onclick="szQrUseManual()">Confirmar</button>
    </div>
  </div>
</div>

<!-- Modal Entregar -->
<div class="modal-backdrop" id="modalEntregar">
  <div class="modal">
    <button class="modal-close" onclick="closeModal('modalEntregar')">✕</button>
    <div class="modal-title">✅ Confirmar Entrega</div>
    <input type="hidden" id="entregaPedidoId">
    <input type="hidden" id="entregaValorPedido" value="0">
    <div class="delivery-total">
      <span>Valor do pedido <small>O total recebido precisa bater exatamente.</small></span>
      <strong id="entregaValorPedidoLabel">R$ 0,00</strong>
    </div>
    <div id="entregaValorCCWrap" style="display:none;background:#f0f9ff;border:1px solid #bae6fd;border-radius:8px;padding:8px 12px;margin-bottom:8px;font-size:var(--sz-text-base)">
      <span style="color:#0369a1">💳 Cartão (com taxa de <?php echo esc_js( number_format( (float) get_option('sz_motoboy_cc_fee_pct', 0), 1 ) ); ?>%): </span>
      <strong id="entregaValorCCLabel" style="color:#0369a1">R$ 0,00</strong>
    </div>

    <!-- Toggle cliente / terceiro -->
    <div class="form-group">
      <label class="form-label">Quem está recebendo?</label>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:4px">
        <button type="button" id="btnRecCliente"
          onclick="setRecebedorTipo('cliente')"
          style="padding:10px;border-radius:10px;border:2px solid #E8650A;background:#E8650A;color:#fff;font-weight:700;font-size:var(--sz-text-base);cursor:pointer">
          👤 O próprio cliente
        </button>
        <button type="button" id="btnRecTerceiro"
          onclick="setRecebedorTipo('terceiro')"
          style="padding:10px;border-radius:10px;border:2px solid #e5e7eb;background:#fff;color:#374151;font-weight:700;font-size:var(--sz-text-base);cursor:pointer">
          👥 Terceiro
        </button>
      </div>
    </div>

    <!-- Nome — só aparece para terceiro -->
    <div class="form-group" id="grupoNome" style="display:none">
      <label class="form-label">Nome completo do Recebedor</label>
      <input class="form-input" type="text" id="entregaNome" placeholder="Nome e sobrenome" autocomplete="name">
    </div>

    <div class="form-group">
      <label class="form-label">CPF do Recebedor</label>
      <input class="form-input" type="text" id="entregaCpf" placeholder="000.000.000-00" maxlength="14" inputmode="numeric" autocomplete="off">
      <div class="form-help">CPF real, com dígitos válidos.</div>
    </div>

    <!-- Seleção de método(s) de pagamento -->
    <div class="form-group">
      <label class="form-label">Método(s) de pagamento recebido(s) <span style="color:#dc2626">*</span></label>
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-top:4px" id="pgtoMetodosWrap">
        <button type="button" id="btnPgtoDinheiro" onclick="togglePgtoMetodo('dinheiro')"
          style="padding:10px 4px;border-radius:10px;border:2px solid #e5e7eb;background:#fff;color:#374151;font-weight:700;font-size:var(--sz-text-meta);cursor:pointer;line-height:1.3">
          💵<br>Dinheiro
        </button>
        <button type="button" id="btnPgtoPix" onclick="togglePgtoMetodo('pix')"
          style="padding:10px 4px;border-radius:10px;border:2px solid #e5e7eb;background:#fff;color:#374151;font-weight:700;font-size:var(--sz-text-meta);cursor:pointer;line-height:1.3">
          📱<br>PIX
        </button>
        <button type="button" id="btnPgtoCartao" onclick="togglePgtoMetodo('cartao')"
          style="padding:10px 4px;border-radius:10px;border:2px solid #e5e7eb;background:#fff;color:#374151;font-weight:700;font-size:var(--sz-text-meta);cursor:pointer;line-height:1.3">
          💳<br>Cartão
        </button>
      </div>
    </div>

    <!-- Campos de valor — aparecem conforme seleção -->
    <div class="form-group" id="pgtoValoresWrap" style="display:none">
      <label class="form-label">Valor recebido por método</label>
      <div id="pgtoDinheiroRow" class="pgto-row" style="display:none">
        <span class="pgto-label">💵 Dinheiro R$</span>
        <input class="form-input" type="text" inputmode="decimal" id="pgtoDinheiro" placeholder="0,00" value="0" oninput="updateEntregaTotal()">
      </div>
      <div id="pgtoPixRow" class="pgto-row" style="display:none">
        <span class="pgto-label">📱 PIX R$</span>
        <input class="form-input" type="text" inputmode="decimal" id="pgtoPix" placeholder="0,00" value="0" oninput="updateEntregaTotal()">
      </div>
      <div id="pgtoCartaoRow" class="pgto-row" style="display:none">
        <span class="pgto-label">💳 Cartão R$</span>
        <input class="form-input" type="text" inputmode="decimal" id="pgtoCartao" placeholder="0,00" value="0" oninput="updateEntregaTotal()">
      </div>
      <div class="delivery-total" style="margin-top:10px;background:#f8fafc;border-color:#e5e7eb;color:#111827">
        <span>Total informado</span>
        <strong id="entregaTotalRecebido">R$ 0,00</strong>
      </div>
    </div>

    <div class="form-group" id="comprovantesWrap" style="display:none">
      <label class="form-label">📸 Comprovante(s) de pagamento <span style="color:#dc2626">*</span></label>
      <div style="font-size:var(--sz-text-sm);color:#6b7280;margin-bottom:8px">Foto do dinheiro, print do PIX ou recibo do cartão. Adicione uma foto por forma de pagamento usada.</div>
      <div id="comprovantes-preview" style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:8px"></div>
      <div id="comprovantes-btns" style="display:flex;gap:8px;flex-wrap:wrap"></div>
      <div id="comprovantes-count" style="font-size:var(--sz-text-sm);color:#9ca3af;margin-top:6px">0 foto(s) adicionada(s)</div>
    </div>

    <button class="btn btn-success" onclick="confirmarEntrega()">✅ Confirmar Entrega</button>
  </div>
</div>

<!-- Modal Frustrar -->
<div class="modal-backdrop" id="modalFrustrar">
  <div class="modal">
    <button class="modal-close" onclick="closeModal('modalFrustrar')">✕</button>
    <div class="modal-title">❌ Registrar Tentativa Frustrada</div>
    <input type="hidden" id="frustrarPedidoId">

    <div class="form-group">
      <label class="form-label">Motivo</label>
      <select class="form-input" id="frustrarMotivo">
        <option value="Ausente">Cliente ausente</option>
        <option value="Recusou">Cliente recusou receber</option>
        <option value="Endereço incorreto">Endereço incorreto</option>
        <option value="Portaria não autorizou">Portaria não autorizou</option>
        <option value="Outro">Outro</option>
      </select>
    </div>

    <div class="form-group">
      <label class="form-label">Descreva o motivo</label>
      <textarea class="form-input" id="frustrarObservacao" rows="3" maxlength="500" placeholder="Ex.: cliente não atendeu, portaria informou ausência, endereço sem número..."></textarea>
      <div class="form-help">Informe detalhes curtos para auditoria da tentativa.</div>
    </div>

    <div class="form-group">
      <label class="form-label">📸 Foto obrigatória</label>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-bottom:8px">
        <button type="button" class="btn btn-outline" onclick="document.getElementById('fotoInputGaleria').click()" style="font-size:var(--sz-text-base)">🖼️ Galeria</button>
        <button type="button" class="btn btn-outline" onclick="document.getElementById('fotoInputCamera').click()" style="font-size:var(--sz-text-base)">📷 Câmera</button>
      </div>
      <input type="file" id="fotoInputGaleria" accept="image/*" style="display:none" onchange="previewFoto(this)">
      <input type="file" id="fotoInputCamera" accept="image/*" capture="environment" style="display:none" onchange="previewFoto(this)">
      <div class="form-help">A frustração só envia com GPS ativo, data/hora e coordenadas gravadas.</div>
      <img id="fotoPreview" class="foto-preview">
    </div>

    <button class="btn btn-danger" onclick="confirmarFrustrado()">❌ Confirmar Frustração</button>
  </div>
</div>

<!-- Toast -->
<div class="toast" id="toast"></div>

<script>
const API = '<?php echo esc_js( set_url_scheme( rest_url('sz-motoboy/v1'), 'https' ) ); ?>';
const SZ_CC_FEE_PCT = <?php echo (float) get_option( 'sz_motoboy_cc_fee_pct', 0 ); ?>;
let SESSION = JSON.parse(localStorage.getItem('sz_mb_session') || 'null');
let loteData = [];
let gpsLat = null, gpsLng = null, gpsAccuracy = null, gpsUpdatedAt = 0;
let fotoBase64 = null;
let entregaAtual = null;

// ── Boot ───────────────────────────────────────────────────────────────────────
let szPrevLoteCount = 0;

async function szMbRegisterPush() {
    if (!('serviceWorker' in navigator) || !('PushManager' in window)) return;
    const vapidKey = document.getElementById('sz-vapid-key')?.value;
    if (!vapidKey) return;
    function urlB64ToUint8(b) {
        const pad = '='.repeat((4 - b.length % 4) % 4);
        const raw = atob((b+pad).replace(/-/g,'+').replace(/_/g,'/'));
        return Uint8Array.from([...raw].map(c=>c.charCodeAt(0)));
    }
    try {
        // Tenta registrar o SW via PHP (/sw-motoboy.js)
        // Se der 404 (permalinks não salvos ainda), não bloqueia o app
        let reg;
        try {
          reg = await navigator.serviceWorker.register('/sw-motoboy.js', { scope: '/' });
        } catch(swErr) {
          console.warn('[Senderzz SW] falha no registro — push desativado:', swErr.message);
          return; // Push não disponível, mas o app funciona normalmente
        }
        const existing = await reg.pushManager.getSubscription();
        if (existing) return;
        const perm = await Notification.requestPermission();
        if (perm !== 'granted') return;
        const sub = await reg.pushManager.subscribe({
            userVisibleOnly: true,
            applicationServerKey: urlB64ToUint8(vapidKey)
        });
        await api('POST', '/motoboy/push-subscribe', sub.toJSON());
    } catch(e) { console.log('Push:', e); }
}

let _loteIntervalId = null;

function boot() {
  if (!SESSION) {
    document.getElementById('loginScreen').style.display = 'flex';
    return;
  }
  document.getElementById('loginScreen').style.display = 'none';
  document.getElementById('app').classList.add('visible');
  document.getElementById('headerNome').textContent = SESSION.nome;
  initGPS();
  loadLote();
  loadFechamento();
  // Garante que só existe um intervalo ativo mesmo se boot() for chamado mais de uma vez
  if (_loteIntervalId) clearInterval(_loteIntervalId);
  _loteIntervalId = setInterval(loadLote, 30000);
  szMbRegisterPush();
}

// ── Login ──────────────────────────────────────────────────────────────────────
// ── Login ──────────────────────────────────────────────────────────────────────
let _loginTelefone = '';

async function verificarTelefone() {
  const tel = document.getElementById('loginTelefone').value.replace(/\D/g,'');
  if (!tel) { showLoginError('Informe o celular com DDD.'); return; }
  _loginTelefone = tel;
  const btn = document.getElementById('btnVerificarTel');
  if (btn) { btn.disabled = true; btn.textContent = 'Verificando...'; }
  try {
    const r = await fetch(API + '/login/verificar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ telefone: tel })
    }).then(r => r.json());
    if (!r.ok) { showLoginError(r.erro || 'Número não cadastrado.'); return; }
    document.getElementById('loginStep1').style.display = 'none';
    if (r.tem_senha) {
      // Já tem senha — vai para o step de login normal
      document.getElementById('loginNomeMb').textContent = 'Olá, ' + r.nome + '! 👋';
      document.getElementById('loginSenha').value = '';
      document.getElementById('loginSenhaError').style.display = 'none';
      document.getElementById('loginStepSenha').style.display = '';
      setTimeout(() => document.getElementById('loginSenha').focus(), 100);
    } else {
      // Primeiro acesso — define a senha
      document.getElementById('loginSenhaNova').value = '';
      document.getElementById('loginSenhaConfirm').value = '';
      document.getElementById('loginDefinirError').style.display = 'none';
      document.getElementById('loginStepDefinir').style.display = '';
      setTimeout(() => document.getElementById('loginSenhaNova').focus(), 100);
    }
  } catch(e) { showLoginError('Erro de conexão. Tente novamente.'); }
  finally { if (btn) { btn.disabled = false; btn.textContent = 'Continuar'; } }
}

async function definirSenha() {
  const nova    = document.getElementById('loginSenhaNova').value;
  const confirm = document.getElementById('loginSenhaConfirm').value;
  if (nova.length < 4)      { showDefinirError('Senha deve ter ao menos 4 caracteres.'); return; }
  if (nova !== confirm)     { showDefinirError('As senhas não coincidem.'); return; }
  const btn = document.getElementById('btnDefinirSenha');
  if (btn) { btn.disabled = true; btn.textContent = 'Salvando...'; }
  try {
    const token = _gerarToken();
    const r = await fetch(API + '/login/definir-senha', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ telefone: _loginTelefone, senha: nova, token_app: token })
    }).then(r => r.json());
    if (!r.ok) { showDefinirError(r.erro || 'Erro ao salvar senha.'); return; }
    SESSION = { token, nome: r.motoboy.nome, id: r.motoboy.id };
    localStorage.setItem('sz_mb_session', JSON.stringify(SESSION));
    boot();
  } catch(e) { showDefinirError('Erro de conexão.'); }
  finally { if (btn) { btn.disabled = false; btn.textContent = 'Salvar e entrar'; } }
}

async function confirmarSenha() {
  const senha = document.getElementById('loginSenha').value;
  if (!senha) { showSenhaError('Informe sua senha.'); return; }
  const btn = document.getElementById('btnConfirmarSenha');
  if (btn) { btn.disabled = true; btn.textContent = 'Entrando...'; }
  try {
    const token = _gerarToken();
    const r = await fetch(API + '/login/autenticar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ telefone: _loginTelefone, senha, token_app: token })
    }).then(r => r.json());
    if (!r.ok) { showSenhaError(r.erro || 'Senha incorreta.'); return; }
    SESSION = { token, nome: r.motoboy.nome, id: r.motoboy.id };
    localStorage.setItem('sz_mb_session', JSON.stringify(SESSION));
    boot();
  } catch(e) { showSenhaError('Erro de conexão.'); }
  finally { if (btn) { btn.disabled = false; btn.textContent = 'Entrar'; } }
}

function voltarStep1() {
  ['loginStepDefinir','loginStepSenha','loginStep2'].forEach(id => {
    const el = document.getElementById(id);
    if (el) el.style.display = 'none';
  });
  document.getElementById('loginStep1').style.display = '';
  document.getElementById('loginError').style.display = 'none';
}

function _gerarToken() {
  const buf = new Uint8Array(24);
  crypto.getRandomValues(buf);
  return 'mb_' + Array.from(buf).map(b => b.toString(16).padStart(2,'0')).join('');
}

function showLoginError(msg)   { const e = document.getElementById('loginError');       if(e){e.textContent=msg;e.style.display='block';} }
function showDefinirError(msg) { const e = document.getElementById('loginDefinirError');if(e){e.textContent=msg;e.style.display='block';} }
function showSenhaError(msg)   { const e = document.getElementById('loginSenhaError');  if(e){e.textContent=msg;e.style.display='block';} }

// ── GPS ────────────────────────────────────────────────────────────────────────
let _gpsWatchId = null;

function initGPS() {
  if (!navigator.geolocation) return;
  // Cancela watcher anterior antes de criar novo — evita múltiplos watchers em boot() repetido
  if (_gpsWatchId !== null) {
    navigator.geolocation.clearWatch(_gpsWatchId);
    _gpsWatchId = null;
  }
  _gpsWatchId = navigator.geolocation.watchPosition(pos => {
    gpsLat = pos.coords.latitude;
    gpsLng = pos.coords.longitude;
    gpsAccuracy = pos.coords.accuracy || null;
    gpsUpdatedAt = Date.now();
    document.getElementById('gpsDot').classList.remove('gps-off');
    document.getElementById('gpsStatus').textContent = gpsAccuracy ? `GPS ativo (${Math.round(gpsAccuracy)}m)` : 'GPS ativo';
    sendPing();
  }, () => {
    document.getElementById('gpsDot').classList.add('gps-off');
    document.getElementById('gpsStatus').textContent = 'Sem GPS';
  }, { enableHighAccuracy: true });
}

async function sendPing() {
  if (!gpsLat || !SESSION) return;
  try { await api('POST', '/motoboy/ping', { lat: gpsLat, lng: gpsLng }); } catch(e){}
}

// ── Lote ───────────────────────────────────────────────────────────────────────
async function loadLote() {
  const list = document.getElementById('loteList');
  const needsHistorico = filtroAtivo === 'entregue' || filtroAtivo === 'frustrado';
  const endpoint = needsHistorico ? '/motoboy/lote?incluir=historico' : '/motoboy/lote';
  try {
    const r = await api('GET', endpoint);
    if (!r.ok) {
      if (list && list.innerHTML.includes('spinner')) {
        list.innerHTML = '<div class="empty-state"><div class="empty-icon">⚠️</div><div class="empty-text">Erro ao carregar</div><button onclick="loadLote()" style="margin-top:12px;padding:8px 18px;background:#E8650A;color:#fff;border:0;border-radius:8px;font-size:var(--sz-text-md);cursor:pointer">Tentar novamente</button></div>';
      }
      return;
    }
    loteData = r.pedidos || [];
    pedidosSelecionados.clear(); // reseta seleção ao recarregar

    // Detectar pedido novo e notificar
    if (szPrevLoteCount > 0 && loteData.length > szPrevLoteCount) {
      const novos = loteData.length - szPrevLoteCount;
      toast('📦 ' + novos + ' novo(s) pedido(s) chegaram!', 4000);
      if ('Notification' in window && Notification.permission === 'granted') {
        new Notification('Senderzz', {
          body: novos + ' novo(s) pedido(s) no seu lote.',
          icon: '/wp-content/plugins/senderzz-logistics/assets/icon-192.png'
        });
      }
    }
    szPrevLoteCount = loteData.length;
    renderLote();
  } catch(e) {
    if (list && list.innerHTML.includes('spinner')) {
      const errMsg = e?.message || String(e);
      const isTimeout = errMsg.includes('abort') || errMsg.includes('timeout');
      const isHtml    = errMsg.includes('invalid_response');
      const label  = isTimeout ? '⏱️ Timeout' : isHtml ? '⚠️ Erro servidor' : '📶 Sem conexão';
      const detail = isHtml ? 'Resposta inesperada do servidor. Abra F12 > Console para detalhes.'
                   : isTimeout ? 'Servidor demorou demais.' : 'Verifique sua internet.';
      list.innerHTML = `<div class="empty-state">
        <div class="empty-icon">${isHtml?'⚠️':'📶'}</div>
        <div class="empty-text">${label}</div>
        <div style="font-size:var(--sz-text-meta);color:#9ca3af;margin-top:4px;padding:0 16px;text-align:center">${detail}</div>
        <div style="font-size:var(--sz-text-xs);color:#d1d5db;margin-top:4px;padding:0 16px;word-break:break-all">${errMsg.substring(0,100)}</div>
        <button onclick="loadLote()" style="margin-top:12px;padding:8px 18px;background:#E8650A;color:#fff;border:0;border-radius:8px;font-size:var(--sz-text-md);cursor:pointer">Tentar novamente</button>
      </div>`;
    }
  }
}

var filtroAtivo = null;

function setFiltro(status) {
  filtroAtivo = filtroAtivo === status ? null : status;
  const keyMap = { embalado:'Embalado', em_rota:'EmRota', entregue:'Entregue', frustrado:'Frustrado' };
  Object.keys(keyMap).forEach(s => {
    const box = document.getElementById('statBox' + keyMap[s]);
    if (box) box.classList.toggle('active', filtroAtivo === s);
  });
  // Se mudou para histórico, recarrega da API para incluir entregue/frustrado do dia
  if (filtroAtivo === 'entregue' || filtroAtivo === 'frustrado') {
    loadLote();
  } else {
    renderLote();
  }
}

function renderLote() {
  const list = document.getElementById('loteList');
  const cnt = s => loteData.filter(p => p.status === s).length;
  const emb = cnt('embalado'), rot = cnt('em_rota') + cnt('a_caminho');
  const ent = cnt('entregue'), fru = cnt('frustrado');

  const se = document.getElementById('statEmbalado');   if (se) se.textContent = emb;
  const sr = document.getElementById('statEmRota');     if (sr) sr.textContent = rot;
  const sv = document.getElementById('statEntregues');  if (sv) sv.textContent = ent;
  const sf = document.getElementById('statFrustrados'); if (sf) sf.textContent = fru;

  // Determina lista visível ANTES de usar — declaração precisa vir antes do uso
  let visivel;
  if (filtroAtivo === 'entregue')       visivel = loteData.filter(p => p.status === 'entregue');
  else if (filtroAtivo === 'frustrado') visivel = loteData.filter(p => p.status === 'frustrado');
  else if (filtroAtivo === 'embalado')  visivel = loteData.filter(p => p.status === 'embalado');
  else if (filtroAtivo === 'em_rota')   visivel = loteData.filter(p => ['em_rota','a_caminho'].includes(p.status));
  else                                  visivel = loteData.filter(p => ['embalado','em_rota','a_caminho'].includes(p.status));

  // Sem botão manual de iniciar rota: saída somente por QR individual da etiqueta.
  const hint = document.getElementById('qrRouteHint');
  if (hint) hint.style.display = (emb > 0 && (!filtroAtivo || filtroAtivo === 'embalado')) ? 'block' : 'none';
  loadRepassesPendentes();

  const FILTRO_LABEL = { embalado:'📦 Embalados', em_rota:'🔵 Em Rota', entregue:'✅ Entregues', frustrado:'❌ Frustrados' };
  const EMPTY_ICON   = { embalado:'📭', em_rota:'🛵', entregue:'🎉', frustrado:'😅', default:'📭' };
  const EMPTY_MSG    = { embalado:'Nenhum pedido embalado.', em_rota:'Nenhum pedido em rota.', entregue:'Nenhuma entrega hoje.', frustrado:'Nenhuma tentativa frustrada.', default:'Nenhum pedido ativo.' };

  if (!visivel.length) {
    const ico = EMPTY_ICON[filtroAtivo] || EMPTY_ICON.default;
    const msg = EMPTY_MSG[filtroAtivo]  || EMPTY_MSG.default;
    list.innerHTML = `<div class="empty-state"><div class="empty-icon">${ico}</div><div class="empty-text">${msg}</div></div>`;
    return;
  }

  const fmtVal = v => 'R\u00a0' + Number(v||0).toLocaleString('pt-BR',{minimumFractionDigits:2,maximumFractionDigits:2});
  const PILL = { embalado:'pill-embalado', em_rota:'pill-em_rota', a_caminho:'pill-a_caminho', entregue:'pill-entregue', frustrado:'pill-frustrado' };
  const PLBL = { embalado:'📦 Embalado', em_rota:'🔵 Em Rota', a_caminho:'🛵 A Caminho', entregue:'✅ Entregue', frustrado:'❌ Frustrado' };

  const filterLabel = filtroAtivo
    ? `<div class="filter-label">${FILTRO_LABEL[filtroAtivo]}<span>${visivel.length}</span></div>`
    : '';

  list.innerHTML = filterLabel + visivel.map(p => {
    const canAction  = ['em_rota','a_caminho'].includes(p.status);
    const isHistoric = ['entregue','frustrado'].includes(p.status);
    const isEmbalado = p.status === 'embalado';
    const isFrustrado = p.status === 'frustrado';
    // Regra Senderzz: quantidade + nome, uma vez só. Pedidos antigos podem ter
    // vindo com vários itens separados por vírgula ("1x A, 1x B") — exibimos só o
    // primeiro segmento (item principal).
    const prod  = (p.dest_produto || '').split(',')[0].trim();
    // Extrai qtd do dest_produto se tiver formato "Nome xN" — ex: "Creme x2"
    const qtyMatch = prod.match(/\sx(\d+)$/i);
    const qty = qtyMatch
      ? `<span class="order-qty">${qtyMatch[1]}x</span>`
      : (p.quantidade ? `<span class="order-qty">${p.quantidade}x</span>` : '');
    const _vPed = parseFloat(p.valor_pedido || 0);
    const _vCC  = SZ_CC_FEE_PCT > 0 ? _vPed * (1 + SZ_CC_FEE_PCT / 100) : 0;
    const valor = _vPed > 0 ? `<span class="order-valor">${fmtVal(_vPed)}</span>${_vCC > 0 ? `<span class="order-valor" style="font-size:.75em;color:#9ca3af;text-decoration:line-through"> / ${fmtVal(_vCC)}💳</span>` : ''}` : '';
    // Endereço: rua + número + complemento (linha 1) | bairro · cidade (linha 2)
    const numComp = [p.dest_numero, p.dest_complemento].filter(Boolean).join(' ');
    const linha1  = [p.dest_endereco, numComp].filter(Boolean).join(', ');
    const linha2  = [p.dest_bairro, p.dest_cidade].filter(Boolean).join(' · ');
    const endShort = [linha1, linha2].filter(Boolean).join(' — ');
    const waMsg = encodeURIComponent('Olá ' + (p.dest_nome||'') + '! Sou o entregador Senderzz do seu pedido #' + p.wc_order_id + '. Estou a caminho! 🛵');
    const waBtn = p.dest_telefone
      ? `<a href="https://wa.me/${p.dest_telefone.replace(/\D/g,'')}?text=${waMsg}" target="_blank" class="act-btn act-wa"><svg viewBox="0 0 24 24" fill="currentColor" style="width:16px;height:16px;flex-shrink:0"><path d="M17.472 14.382c-.297-.149-1.758-.867-2.03-.967-.273-.099-.471-.148-.67.15-.197.297-.767.966-.94 1.164-.173.199-.347.223-.644.075-.297-.15-1.255-.463-2.39-1.475-.883-.788-1.48-1.761-1.653-2.059-.173-.297-.018-.458.13-.606.134-.133.298-.347.446-.52.149-.174.198-.298.298-.497.099-.198.05-.371-.025-.52-.075-.149-.669-1.612-.916-2.207-.242-.579-.487-.5-.669-.51-.173-.008-.371-.01-.57-.01-.198 0-.52.074-.792.372-.272.297-1.04 1.016-1.04 2.479 0 1.462 1.065 2.875 1.213 3.074.149.198 2.096 3.2 5.077 4.487.709.306 1.262.489 1.694.625.712.227 1.36.195 1.871.118.571-.085 1.758-.719 2.006-1.413.248-.694.248-1.289.173-1.413-.074-.124-.272-.198-.57-.347m-5.421 7.403h-.004a9.87 9.87 0 01-5.031-1.378l-.361-.214-3.741.982.998-3.648-.235-.374a9.86 9.86 0 01-1.51-5.26c.001-5.45 4.436-9.884 9.888-9.884 2.64 0 5.122 1.03 6.988 2.898a9.825 9.825 0 012.893 6.994c-.003 5.45-4.437 9.884-9.885 9.884m8.413-18.297A11.815 11.815 0 0012.05 0C5.495 0 .16 5.335.157 11.892c0 2.096.547 4.142 1.588 5.945L.057 24l6.305-1.654a11.882 11.882 0 005.683 1.448h.005c6.554 0 11.89-5.335 11.893-11.893a11.821 11.821 0 00-3.48-8.413z"/></svg> WhatsApp</a>`
      : '';

    const actions = canAction
      ? `${waBtn}<button class="act-btn act-ok" onclick="openEntregar(${p.id})">✅ Entregue</button><button class="act-btn act-fail" onclick="openFrustrar(${p.id})">✕ Frustrado</button>`
      : (!isHistoric ? waBtn : '');
    const retornoAction = isFrustrado
      ? (Number(p.return_declared||0) === 1 || p.custody_status === 'return_declared'
          ? `<div class="order-card-actions"><button class="act-btn act-rota" disabled>⏳ Aguardando OL</button></div>`
          : `<div class="order-card-actions"><button class="act-btn act-rota" onclick="declararDevolucao(${p.id}, event)">📷 Bipar devolução</button></div>`)
      : '';

    return `<div class="order-card" id="card-${p.id}">
      <div class="order-card-top">
        <div class="order-num">#${p.wc_order_id}</div>
        <div class="order-info">
          <div class="order-name">${p.dest_nome||'Destinatário'}</div>
          <div class="order-addr">${endShort}</div>
          ${prod ? `<div class="order-product">📦 ${prod}</div>` : ''}
          <div class="order-meta">${valor}${qty}<span class="order-status-pill ${PILL[p.status]||'pill-embalado'}">${PLBL[p.status]||p.status}</span></div>
        </div>
      </div>
      ${isEmbalado ? `<div class="order-card-actions"><button class="act-btn act-rota" onclick="iniciarRotaIndividual(${p.id}, event)">📷 Bipar saída</button></div>` : ''}
      ${!isEmbalado && actions ? `<div class="order-card-actions">${actions}</div>` : ''}
      ${retornoAction}
    </div>`;
  }).join('');
}

// ── Seleção de pedidos embalados ───────────────────────────────────────────────
let pedidosSelecionados = new Set(); // IDs de sz_motoboy_pedidos selecionados

function toggleSelect(pedidoId, e) {
  if (e) e.stopPropagation();
  const chk = document.getElementById('chk-' + pedidoId);
  const card = document.getElementById('card-' + pedidoId);
  if (!chk) return;
  if (pedidosSelecionados.has(pedidoId)) {
    pedidosSelecionados.delete(pedidoId);
    chk.classList.remove('checked');
    if (card) card.classList.remove('selected');
  } else {
    pedidosSelecionados.add(pedidoId);
    chk.classList.add('checked');
    if (card) card.classList.add('selected');
  }
  atualizarSelCount();
}

function toggleSelecionarTodos() {
  const embalados = loteData.filter(p => p.status === 'embalado');
  const todosSel  = embalados.every(p => pedidosSelecionados.has(parseInt(p.id)));
  embalados.forEach(p => {
    const id = parseInt(p.id);
    const chk  = document.getElementById('chk-' + id);
    const card = document.getElementById('card-' + id);
    if (todosSel) {
      pedidosSelecionados.delete(id);
      if (chk)  chk.classList.remove('checked');
      if (card) card.classList.remove('selected');
    } else {
      pedidosSelecionados.add(id);
      if (chk)  chk.classList.add('checked');
      if (card) card.classList.add('selected');
    }
  });
  atualizarSelCount();
}

function atualizarSelCount() {
  const n   = pedidosSelecionados.size;
  const cnt = document.getElementById('selCount');
  const btn = document.getElementById('btnIniciarSel');
  const all = document.getElementById('selAllBtn');
  const emb = loteData.filter(p => p.status === 'embalado').length;
  if (cnt) cnt.textContent = n > 0 ? n + ' selecionado(s)' : 'Nenhum selecionado';
  if (btn) btn.disabled = n === 0;
  if (all) all.textContent = n === emb && emb > 0 ? 'Desmarcar todos' : 'Selecionar todos';
}

let szQrStream = null;
let szQrResolver = null;
let szQrAnimation = null;
function szQrStopCamera(){ if(szQrAnimation){cancelAnimationFrame(szQrAnimation);szQrAnimation=null;} if(szQrStream){szQrStream.getTracks().forEach(t=>t.stop());szQrStream=null;} const v=document.getElementById('qrVideo'); if(v) v.srcObject=null; }
function szQrClose(value){ szQrStopCamera(); const m=document.getElementById('modalQrScan'); if(m)m.classList.remove('open'); if(szQrResolver){ const r=szQrResolver; szQrResolver=null; r(value||''); } }
function szQrUseManual(){ const i=document.getElementById('qrManualInput'); szQrClose((i&&i.value?i.value:'').trim()); }
async function lerQrEtiqueta(pedido) {
  const ref = pedido && pedido.wc_order_id ? (' do pedido #' + pedido.wc_order_id) : '';
  const fallbackPrompt = () => (prompt('Bipe/cole o QR Code da etiqueta' + ref + '.\n\nEx.: SZ-1234-99-ABCDEF123456') || '').trim();
  if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia || !('BarcodeDetector' in window)) return fallbackPrompt();
  return await new Promise(async (resolve)=>{
    szQrResolver=resolve; const modal=document.getElementById('modalQrScan'), input=document.getElementById('qrManualInput'), help=document.getElementById('qrHelp'), video=document.getElementById('qrVideo');
    if(input) input.value=''; if(help) help.textContent='Aponte a câmera para o QR Code da etiqueta' + ref + '. Se preferir, cole o código impresso.'; if(modal) modal.classList.add('open');
    let detector; try{ detector=new BarcodeDetector({formats:['qr_code']}); szQrStream=await navigator.mediaDevices.getUserMedia({video:{facingMode:{ideal:'environment'}},audio:false}); video.srcObject=szQrStream; await video.play(); }catch(e){ szQrClose(fallbackPrompt()); return; }
    const scan=async()=>{ if(!szQrResolver)return; try{ const codes=await detector.detect(video); if(codes&&codes.length&&codes[0].rawValue){ szQrClose(String(codes[0].rawValue).trim()); return; } }catch(e){} szQrAnimation=requestAnimationFrame(scan); }; scan();
  });
}

async function iniciarRotaIndividual(pedidoId, e) {
  if (e) e.stopPropagation();
  const btn = e?.currentTarget;
  if (btn) { btn.disabled = true; btn.textContent = '⏳'; }
  const pedido = loteData.find(p => parseInt(p.id) === pedidoId);
  if (pedido) {
    const hoje = new Date(); hoje.setHours(0,0,0,0);
    if (pedido.delivery_date) {
      const dt = new Date(pedido.delivery_date + 'T12:00:00');
      if (dt.getTime() !== hoje.getTime()) {
        const ok = confirm('⚠️ Pedido #' + pedido.wc_order_id + ' tem data de entrega ' + pedido.delivery_date + '.\nIniciar mesmo assim?');
        if (!ok) { if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar saída'; } return; }
      }
    }
  }
  const packageCode = await lerQrEtiqueta(pedido || {id:pedidoId});
  if (!packageCode) { if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar saída'; } return; }
  try {
    const r = await api('POST', '/motoboy/iniciar-rota', { pedido_id: pedidoId, package_code: packageCode });
    if (!r.ok || Number(r.em_rota||0) < 1) {
      const msg = r.erro || 'Nenhum pedido foi atualizado. Atualize o lote e tente novamente.';
      toast('⚠️ ' + msg, 5000);
      if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar saída'; }
      await loadLote();
      return;
    }
    toast('🚀 Pedido em rota!');
    pedidosSelecionados.delete(pedidoId);
    loadLote();
  } catch(e) {
    console.error('[Senderzz Motoboy] erro iniciar individual:', e);
    toast('Erro ao iniciar: ' + ((e && e.message) ? e.message : 'verifique conexão'), 5000);
    if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar saída'; }
  }
}

async function declararDevolucao(pedidoId, e) {
  if (e) e.stopPropagation();
  const btn = e?.currentTarget;
  if (btn) { btn.disabled = true; btn.textContent = '⏳'; }
  const pedido = loteData.find(p => parseInt(p.id) === pedidoId);
  const packageCode = await lerQrEtiqueta(pedido || {id:pedidoId});
  if (!packageCode) { if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar devolução'; } return; }
  try {
    const r = await api('POST', '/motoboy/devolver-qr', { pedido_id: pedidoId, package_code: packageCode });
    if (!r.ok) {
      toast('⚠️ ' + (r.erro || 'Não foi possível declarar devolução.'), 5000);
      if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar devolução'; }
      await loadLote();
      return;
    }
    toast('✅ Devolução declarada. Aguardando confirmação do OL.', 5000);
    loadLote();
  } catch(e) {
    console.error('[Senderzz Motoboy] erro declarar devolução:', e);
    toast('Erro ao declarar devolução: ' + ((e && e.message) ? e.message : 'verifique conexão'), 5000);
    if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar devolução'; }
  }
}


async function iniciarRotaSelecionados() {
  toast('Cada pacote precisa ser bipado individualmente pelo QR da etiqueta.', 5000);
  return;
  const ids = [...pedidosSelecionados];
  if (!ids.length) { toast('Selecione ao menos um pedido.'); return; }
  const fora = loteData.filter(p => ids.includes(parseInt(p.id)) && p.delivery_date && (() => {
    const hoje = new Date(); hoje.setHours(0,0,0,0);
    return new Date(p.delivery_date + 'T12:00:00').getTime() !== hoje.getTime();
  })());
  if (fora.length > 0) {
    const nomes = fora.map(p => '#' + p.wc_order_id + ' (' + p.delivery_date + ')').join('\n');
    const ok = confirm('⚠️ ' + fora.length + ' pedido(s) com data diferente de hoje:\n' + nomes + '\n\nIniciar mesmo assim?');
    if (!ok) return;
  }
  const btn = document.getElementById('btnIniciarSel');
  if (btn) { btn.disabled = true; btn.textContent = '⏳ Iniciando...'; }
  try {
    const r = await api('POST', '/motoboy/iniciar-rota', { pedido_ids: ids });
    if (!r.ok || Number(r.em_rota||0) < 1) {
      const msg = r.erro || 'Nenhum pedido selecionado foi atualizado. Atualize o lote e tente novamente.';
      toast('⚠️ ' + msg, 5000);
      if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar selecionados'; }
      await loadLote();
      return;
    }
    toast('🚀 ' + r.em_rota + ' pedido(s) em rota!');
    pedidosSelecionados.clear();
    loadLote();
  } catch(e) {
    console.error('[Senderzz Motoboy] erro iniciar selecionados:', e);
    toast('Erro ao iniciar rota: ' + ((e && e.message) ? e.message : 'verifique conexão'), 5000);
    if (btn) { btn.disabled = false; btn.textContent = '📷 Bipar selecionados'; }
  }
}

// ── Bipar QR ───────────────────────────────────────────────────────────────
function checkDataPedidos() {
  const hoje = new Date(); hoje.setHours(0,0,0,0);
  return loteData.filter(p => {
    if (p.status !== 'embalado') return false;
    if (!p.delivery_date) return false;
    const dt = new Date(p.delivery_date + 'T12:00:00');
    return dt.getTime() !== hoje.getTime();
  });
}

async function iniciarRotaTodos() {
  toast('Para iniciar rota, abra o pedido embalado e bipe o QR da etiqueta.', 5000);
  return;
  const fora = checkDataPedidos();
  if (fora.length > 0) {
    const nomes = fora.map(p => '#' + p.wc_order_id + ' (entrega: ' + p.delivery_date + ')').join('\n');
    const ok = confirm(
      '⚠️ Atenção!\n\n' + fora.length + ' pedido(s) com data diferente de hoje:\n' + nomes +
      '\n\nDeseja iniciar a rota mesmo assim?'
    );
    if (!ok) return;
  }
  try {
    const r = await api('POST', '/motoboy/iniciar-rota');
    if (!r.ok || Number(r.em_rota||0) < 1) {
      toast('⚠️ ' + (r.erro || 'Nenhum pedido embalado encontrado para iniciar rota.'), 5000);
      await loadLote();
      return;
    }
    toast(`🚀 Rota iniciada! ${r.em_rota} pedido(s) em rota.`);
    loadLote();
  } catch(e) {
    console.error('[Senderzz Motoboy] erro iniciar rota todos:', e);
    toast('Erro ao iniciar rota: ' + ((e && e.message) ? e.message : 'verifique conexão'), 5000);
  }
}

// ── Repasses pendentes de confirmação ─────────────────────────────────────────
async function loadRepassesPendentes() {
  try {
    const r = await api('GET', '/motoboy/pendentes-confirmacao');
    if (!r.ok || !r.pendentes || !r.pendentes.length) {
      const el = document.getElementById('sz-repasses-pendentes');
      if (el) el.style.display = 'none';
      return;
    }
    let html = '<div style="background:#fff7ed;border:2px solid #f59e0b;border-radius:12px;padding:14px;margin-bottom:12px">'
      + '<div style="font-weight:700;font-size:var(--sz-text-md);color:#92400e;margin-bottom:8px">⚠️ Confirme os repasses em caixa</div>'
      + '<div style="font-size:var(--sz-text-meta);color:#78350f;margin-bottom:10px">O operador deu baixa nos pedidos abaixo. Confirme que recebeu o dinheiro.</div>';
    r.pendentes.forEach(p => {
      html += '<div style="background:#fff;border-radius:8px;padding:10px 12px;margin-bottom:6px;display:flex;justify-content:space-between;align-items:center">'
        + '<div><div style="font-weight:700;font-size:var(--sz-text-base)">#' + p.number + ' — ' + p.valor_fmt + '</div>'
        + '<div style="font-size:var(--sz-text-sm);color:#6b7280">Baixa por: ' + (p.baixa_admin||'Admin') + ' · ' + (p.baixa_at||'') + '</div></div>'
        + '<button onclick="confirmarRepasse(' + p.order_id + ',this)" '
        + 'style="background:#16a34a;color:#fff;border:0;border-radius:8px;padding:6px 12px;font-size:var(--sz-text-meta);font-weight:700;cursor:pointer">✅ Confirmar</button>'
        + '</div>';
    });
    html += '</div>';
    let el = document.getElementById('sz-repasses-pendentes');
    if (!el) {
      el = document.createElement('div');
      el.id = 'sz-repasses-pendentes';
      const list = document.getElementById('loteList');
      if (list) list.insertAdjacentElement('beforebegin', el);
    }
    el.innerHTML = html;
    el.style.display = 'block';
  } catch(e) {}
}

async function confirmarRepasse(orderId, btn) {
  if (btn) { btn.disabled=true; btn.textContent='…'; }
  try {
    const r = await api('POST', '/motoboy/confirmar-repasse', { order_id: orderId });
    if (r.ok) {
      toast('✅ Repasse confirmado!');
      loadRepassesPendentes();
    } else {
      toast('Erro: ' + (r.erro||'falha'));
      if (btn) { btn.disabled=false; btn.textContent='✅ Confirmar'; }
    }
  } catch(e) {
    toast('Erro de conexão.');
    if (btn) { btn.disabled=false; btn.textContent='✅ Confirmar'; }
  }
}

// ── Modal Entregar ─────────────────────────────────────────────────────────────
function money(v) {
  if (typeof v === 'number') return Math.round(v * 100) / 100;
  v = String(v || '').trim().replace(/R\$/g,'').replace(/\s/g,'');
  // Formato brasileiro: 1.250,00 → remove ponto de milhar, troca vírgula por ponto
  // Formato americano: 250.00 → mantém como está
  if (v.includes(',')) {
    v = v.replace(/\./g,'').replace(',', '.'); // 1.250,00 → 1250.00
  }
  // se não tem vírgula, o ponto já é decimal (formato do PHP/WC) — não mexe
  const n = parseFloat(v);
  return Number.isFinite(n) ? Math.max(0, Math.round(n * 100) / 100) : 0;
}
function fmtMoney(n) { return 'R$ ' + (money(n)).toFixed(2).replace('.', ','); }
function onlyDigits(v){ return String(v||'').replace(/\D/g,''); }
function validarCPF(cpf) {
  cpf = onlyDigits(cpf);
  if (cpf.length !== 11 || /^(\d)\1{10}$/.test(cpf)) return false;
  for (let t=9; t<11; t++) {
    let soma = 0;
    for (let i=0; i<t; i++) soma += parseInt(cpf[i],10) * ((t + 1) - i);
    let d = (soma * 10) % 11;
    if (d === 10) d = 0;
    if (parseInt(cpf[t],10) !== d) return false;
  }
  return true;
}
function maskCPF(v) {
  const d = onlyDigits(v).slice(0,11);
  return d.replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d{1,2})$/,'$1-$2');
}
function updateEntregaTotal() {
  const total = money(document.getElementById('pgtoDinheiro').value) + money(document.getElementById('pgtoPix').value) + money(document.getElementById('pgtoCartao').value);
  const esperado = money(document.getElementById('entregaValorPedido').value);
  const el = document.getElementById('entregaTotalRecebido');
  el.textContent = fmtMoney(total);
  el.style.color = Math.abs(total - esperado) < 0.01 ? '#16a34a' : (total > esperado ? '#dc2626' : '#E8650A');
}
var recebedorTipo = 'cliente'; // 'cliente' ou 'terceiro'
var pgtoMetodosSelecionados = new Set(); // 'dinheiro', 'pix', 'cartao'

function togglePgtoMetodo(tipo) {
  const BTN_ON  = { background:'#E8650A', color:'#fff', borderColor:'#E8650A' };
  const BTN_OFF = { background:'#fff',    color:'#374151', borderColor:'#e5e7eb' };
  const btn = document.getElementById('btnPgto' + tipo.charAt(0).toUpperCase() + tipo.slice(1));
  if (pgtoMetodosSelecionados.has(tipo)) {
    pgtoMetodosSelecionados.delete(tipo);
    Object.assign(btn.style, BTN_OFF);
  } else {
    pgtoMetodosSelecionados.add(tipo);
    Object.assign(btn.style, BTN_ON);
  }

  // Mostrar/ocultar rows de valor e seção de comprovantes
  const valoresWrap = document.getElementById('pgtoValoresWrap');
  const compWrap    = document.getElementById('comprovantesWrap');
  const hasSel      = pgtoMetodosSelecionados.size > 0;
  valoresWrap.style.display = hasSel ? '' : 'none';
  compWrap.style.display    = hasSel ? '' : 'none';

  // Mostra só os rows dos métodos selecionados
  ['dinheiro','pix','cartao'].forEach(t => {
    const row = document.getElementById('pgto' + t.charAt(0).toUpperCase() + t.slice(1) + 'Row');
    const sel = pgtoMetodosSelecionados.has(t);
    if (row) row.style.display = sel ? '' : 'none';
    // Zera valor ao desmarcar
    const inp = document.getElementById('pgto' + t.charAt(0).toUpperCase() + t.slice(1));
    if (!sel && inp) inp.value = '0';
  });

  // Pré-preenche com valor do pedido se método único
  const esperado = money(document.getElementById('entregaValorPedido').value);
  if (pgtoMetodosSelecionados.size === 1) {
    const unico = [...pgtoMetodosSelecionados][0];
    const inp = document.getElementById('pgto' + unico.charAt(0).toUpperCase() + unico.slice(1));
    if (inp) inp.value = String(esperado).replace('.', ',');
  } else {
    // limpa pré-fill para o motoboy informar manualmente
    pgtoMetodosSelecionados.forEach(t => {
      const inp = document.getElementById('pgto' + t.charAt(0).toUpperCase() + t.slice(1));
      if (inp && money(inp.value) === esperado) inp.value = '0';
    });
  }

  // Reconstrói botões de comprovante
  _renderComprovanteBtns();
  updateEntregaTotal();
}

function _renderComprovanteBtns() {
  const wrap = document.getElementById('comprovantes-btns');
  if (!wrap) return;
  const EMOJI = { dinheiro:'💵', pix:'📱', cartao:'💳' };
  const LABEL = { dinheiro:'Dinheiro', pix:'PIX', cartao:'Cartão' };
  wrap.innerHTML = [...pgtoMetodosSelecionados].map(tipo => `
    <div style="flex:1;min-width:90px">
      <div style="font-size:var(--sz-text-xs);font-weight:700;color:#6b7280;text-align:center;margin-bottom:4px">${EMOJI[tipo]} ${LABEL[tipo]}</div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:4px">
        <label style="height:38px;background:#f3f4f6;border:2px dashed #d1d5db;border-radius:8px;display:flex;align-items:center;justify-content:center;cursor:pointer;font-size:var(--sz-text-xs);font-weight:700;color:#374151">
          🖼️<input type="file" accept="image/*" onchange="addComprovante(this,'${tipo}')" style="display:none">
        </label>
        <label style="height:38px;background:#f3f4f6;border:2px dashed #d1d5db;border-radius:8px;display:flex;align-items:center;justify-content:center;cursor:pointer;font-size:var(--sz-text-xs);font-weight:700;color:#374151">
          📷<input type="file" accept="image/*" capture="environment" onchange="addComprovante(this,'${tipo}')" style="display:none">
        </label>
      </div>
    </div>
  `).join('');
}

function setRecebedorTipo(tipo) {
  recebedorTipo = tipo;
  const btnC = document.getElementById('btnRecCliente');
  const btnT = document.getElementById('btnRecTerceiro');
  const grupoNome = document.getElementById('grupoNome');
  if (tipo === 'cliente') {
    btnC.style.background = '#E8650A'; btnC.style.color = '#fff'; btnC.style.borderColor = '#E8650A';
    btnT.style.background = '#fff'; btnT.style.color = '#374151'; btnT.style.borderColor = '#e5e7eb';
    grupoNome.style.display = 'none';
    document.getElementById('entregaNome').value = '';
  } else {
    btnT.style.background = '#E8650A'; btnT.style.color = '#fff'; btnT.style.borderColor = '#E8650A';
    btnC.style.background = '#fff'; btnC.style.color = '#374151'; btnC.style.borderColor = '#e5e7eb';
    grupoNome.style.display = '';
  }
}

var comprovantes = []; // [{tipo, base64}]
var szEntregaEnviando = false;

function addComprovante(input, tipo) {
  const file = input.files[0];
  if (!file) return;
  if (comprovantes.length >= 5) { toast('Máximo de 5 comprovantes.'); return; }
  const reader = new FileReader();
  reader.onload = e => {
    const b64 = e.target.result;
    comprovantes.push({ tipo, base64: b64 });
    const prev = document.getElementById('comprovantes-preview');
    const cnt  = document.getElementById('comprovantes-count');
    if (prev) {
      const EMOJI = { dinheiro:'💵', pix:'📱', cartao:'💳' };
      const div = document.createElement('div');
      div.style.cssText = 'position:relative;width:60px;height:60px';
      div.innerHTML = '<img src="'+b64+'" style="width:60px;height:60px;object-fit:cover;border-radius:8px;border:2px solid #e5e7eb">'
        + '<div style="position:absolute;top:-6px;right:-6px;background:#E8650A;color:#fff;border-radius:99px;width:18px;height:18px;font-size:var(--sz-text-xs);font-weight:700;display:flex;align-items:center;justify-content:center">'+(EMOJI[tipo]||tipo.charAt(0).toUpperCase())+'</div>'
        + '<button onclick="removeComprovante('+(comprovantes.length-1)+',this.parentNode)" style="position:absolute;bottom:-6px;right:-6px;background:#dc2626;color:#fff;border:0;border-radius:99px;width:18px;height:18px;font-size:var(--sz-text-xs);cursor:pointer">✕</button>';
      prev.appendChild(div);
    }
    if (cnt) cnt.textContent = comprovantes.length + ' foto(s) adicionada(s)';
  };
  reader.readAsDataURL(file);
  input.value = ''; // permite re-selecionar o mesmo arquivo
}

function removeComprovante(idx, el) {
  comprovantes.splice(idx, 1);
  if (el) el.remove();
  const cnt = document.getElementById('comprovantes-count');
  if (cnt) cnt.textContent = comprovantes.length + ' foto(s) adicionada(s)';
}

function openEntregar(pedidoId) {
  entregaAtual = loteData.find(p => parseInt(p.id) === parseInt(pedidoId)) || null;
  const valor = money(entregaAtual?.valor_pedido || 0);
  document.getElementById('entregaPedidoId').value = pedidoId;
  document.getElementById('entregaValorPedido').value = valor;
  document.getElementById('entregaValorPedidoLabel').textContent = fmtMoney(valor);
  // Segundo valor: com taxa de cartão
  const _ccWrap = document.getElementById('entregaValorCCWrap');
  const _ccLbl  = document.getElementById('entregaValorCCLabel');
  const _vPedNum = parseFloat(entregaAtual?.valor_pedido || 0);
  if (_ccWrap && SZ_CC_FEE_PCT > 0 && _vPedNum > 0) {
    const _vCC = _vPedNum * (1 + SZ_CC_FEE_PCT / 100);
    if (_ccLbl) _ccLbl.textContent = 'R$ ' + _vCC.toFixed(2).replace('.', ',');
    _ccWrap.style.display = '';
  } else if (_ccWrap) { _ccWrap.style.display = 'none'; }
  document.getElementById('entregaNome').value = '';
  document.getElementById('entregaCpf').value = '';
  document.getElementById('pgtoDinheiro').value = '0';
  document.getElementById('pgtoPix').value = '0';
  document.getElementById('pgtoCartao').value = '0';
  comprovantes = [];
  const prev = document.getElementById('comprovantes-preview');
  if (prev) prev.innerHTML = '';
  const cnt = document.getElementById('comprovantes-count');
  if (cnt) cnt.textContent = '0 foto(s) adicionada(s)';
  // Reset seleção de métodos
  pgtoMetodosSelecionados = new Set();
  ['Dinheiro','Pix','Cartao'].forEach(t => {
    const b = document.getElementById('btnPgto'+t);
    if (b) { b.style.background='#fff'; b.style.color='#374151'; b.style.borderColor='#e5e7eb'; }
  });
  document.getElementById('pgtoValoresWrap').style.display = 'none';
  document.getElementById('comprovantesWrap').style.display = 'none';
  ['dinheiro','pix','cartao'].forEach(t => {
    const r = document.getElementById('pgto'+t.charAt(0).toUpperCase()+t.slice(1)+'Row');
    if (r) r.style.display = 'none';
  });
  _renderComprovanteBtns();
  setRecebedorTipo('cliente');
  updateEntregaTotal();
  openModal('modalEntregar');
}

document.addEventListener('input', function(e){
  if (e.target && e.target.id === 'entregaCpf') e.target.value = maskCPF(e.target.value);
  if (e.target && e.target.id === 'pf-pix-key') mbMaskPixKey(e.target);
});

function gpsOperacionalOk() {
  if (!gpsLat || !gpsLng) return false;
  if (gpsUpdatedAt && (Date.now() - gpsUpdatedAt) > 120000) return false;
  if (gpsAccuracy && gpsAccuracy > 150) return false;
  return true;
}

async function confirmarEntrega() {
  if (szEntregaEnviando) return;
  const pedidoId  = document.getElementById('entregaPedidoId').value;
  const nome      = document.getElementById('entregaNome').value.trim();
  const cpf       = document.getElementById('entregaCpf').value;

  // Alerta se data de entrega é diferente de hoje
  if (entregaAtual && entregaAtual.delivery_date) {
    const hoje = new Date(); hoje.setHours(0,0,0,0);
    const dtPedido = new Date(entregaAtual.delivery_date + 'T12:00:00');
    if (dtPedido.getTime() !== hoje.getTime()) {
      const ok = confirm(
        '⚠️ Atenção!\n\nEste pedido tem data de entrega: ' + entregaAtual.delivery_date +
        '\nHoje é: ' + hoje.toLocaleDateString('pt-BR') +
        '\n\nDeseja confirmar a entrega mesmo assim?'
      );
      if (!ok) return;
    }
  }

  if (pgtoMetodosSelecionados.size === 0) { toast('Selecione pelo menos 1 método de pagamento.'); return; }

  // Nome obrigatório apenas para terceiro
  if (recebedorTipo === 'terceiro' && !nome) { toast('Informe o nome do recebedor.'); return; }
  // Se cliente, usa nome do destinatário
  const nomeEnvio = recebedorTipo === 'cliente' ? (entregaAtual?.dest_nome || 'Cliente') : nome;
  const dinheiro  = money(document.getElementById('pgtoDinheiro').value);
  const pix       = money(document.getElementById('pgtoPix').value);
  const cartao    = money(document.getElementById('pgtoCartao').value);
  const esperado  = money(document.getElementById('entregaValorPedido').value);
  const total     = Math.round((dinheiro + pix + cartao) * 100) / 100;

  if (!validarCPF(cpf)) { toast('CPF inválido.'); return; }
  if (!gpsOperacionalOk()) { toast('GPS obrigatório e com boa precisão para confirmar entrega.'); return; }
  if (total > esperado + 0.009) { toast('Valor recebido não pode exceder o pedido.'); return; }
  if (Math.abs(total - esperado) > 0.009) { toast('Total recebido precisa bater com o valor do pedido.'); return; }

  if (comprovantes.length === 0) { toast('📸 Adicione pelo menos 1 foto do comprovante.'); return; }

  szEntregaEnviando = true;
  try {
    const r = await api('POST', '/motoboy/entregar', {
      pedido_id: parseInt(pedidoId), recebedor_nome: nomeEnvio, cpf, recebedor_tipo: recebedorTipo,
      pgto_dinheiro: dinheiro, pgto_pix: pix, pgto_cartao: cartao, lat: gpsLat, lng: gpsLng, accuracy: gpsAccuracy
    });
    if (!r.ok) { szEntregaEnviando = false; toast('Erro: ' + (r.erro || 'falha na entrega')); return; }

    // Upload comprovantes por tipo — com retry automático em caso de falha de rede
    const tipos = [...new Set(comprovantes.map(c=>c.tipo))];
    let uploadOk = true;
    for (const tipo of tipos) {
      const fotos = comprovantes.filter(c=>c.tipo===tipo).map(c=>c.base64);
      try {
        const ur = await api('POST', '/motoboy/comprovante', {
          pedido_id: parseInt(pedidoId), tipo_pgto: tipo, fotos, baixa_por: 'motoboy'
        });
        if (!ur.ok) uploadOk = false;
      } catch(e) {
        uploadOk = false;
        // Persiste para retry no próximo boot
        szMbSalvarComprovantePendente(parseInt(pedidoId), tipo, fotos);
      }
    }

    closeModal('modalEntregar');
    toast(uploadOk ? '✅ Entrega confirmada!' : '✅ Entrega registrada! Enviando comprovantes...');
    // Remove imediatamente da lista — não reaparece na tela do motoboy
    loteData = loteData.filter(p => parseInt(p.id) !== parseInt(pedidoId));
    renderLote();
    loadFechamento();
    szEntregaEnviando = false;
  } catch(e) { szEntregaEnviando = false; toast('Erro ao confirmar: ' + ((e && e.message) ? e.message : 'verifique conexão')); }
}

// ── Modal Frustrar ─────────────────────────────────────────────────────────────
function openFrustrar(pedidoId) {
  document.getElementById('frustrarPedidoId').value = pedidoId;
  const obs = document.getElementById('frustrarObservacao');
  if (obs) obs.value = '';
  const gi = document.getElementById('fotoInputGaleria');
  const ci = document.getElementById('fotoInputCamera');
  if (gi) gi.value = '';
  if (ci) ci.value = '';
  fotoBase64 = null;
  document.getElementById('fotoPreview').style.display = 'none';
  openModal('modalFrustrar');
}

function previewFoto(input) {
  const file = input.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = e => {
    fotoBase64 = e.target.result;
    const img = document.getElementById('fotoPreview');
    img.src = fotoBase64;
    img.style.display = 'block';
  };
  reader.readAsDataURL(file);
}

async function confirmarFrustrado() {
  const pedidoId = document.getElementById('frustrarPedidoId').value;
  const motivo   = document.getElementById('frustrarMotivo').value;
  const observacao = (document.getElementById('frustrarObservacao')?.value || '').trim();
  if (!motivo) { toast('Informe o motivo.'); return; }
  if (!observacao) { toast('Descreva o motivo da frustração.'); return; }
  if (!fotoBase64) { toast('Foto obrigatória.'); return; }

  // Alerta se data de entrega é diferente de hoje
  const pedidoFrustr = loteData.find(p => parseInt(p.id) === parseInt(pedidoId));
  if (pedidoFrustr && pedidoFrustr.delivery_date) {
    const hoje = new Date(); hoje.setHours(0,0,0,0);
    const dtP = new Date(pedidoFrustr.delivery_date + 'T12:00:00');
    if (dtP.getTime() !== hoje.getTime()) {
      const ok = confirm(
        '⚠️ Atenção!\n\nEste pedido tem data de entrega: ' + pedidoFrustr.delivery_date +
        '\nHoje é: ' + hoje.toLocaleDateString('pt-BR') +
        '\n\nDeseja frustrar mesmo assim?'
      );
      if (!ok) return;
    }
  }
  if (!gpsOperacionalOk()) { toast('GPS obrigatório e com boa precisão para frustrar.'); return; }

  try {
    const r = await api('POST', '/motoboy/frustrar', {
      pedido_id: parseInt(pedidoId), motivo, observacao, foto_base64: fotoBase64,
      lat: gpsLat, lng: gpsLng, accuracy: gpsAccuracy
    });
    if (!r.ok) { szEntregaEnviando = false; toast('Erro: ' + (r.erro || 'falha na entrega')); return; }
    closeModal('modalFrustrar');
    const msg = r.isento ? '✅ Frustração registrada (isenta — 1ª tentativa).' : `✅ Frustração registrada. Taxa R$ ${r.taxa}.`;
    toast(msg, 4000);
    // Remove imediatamente da lista — não reaparece na tela do motoboy
    loteData = loteData.filter(p => parseInt(p.id) !== parseInt(pedidoId));
    renderLote();
    loadFechamento();
  } catch(e) { toast('Erro ao registrar.'); }
}

// ── Fechamento ─────────────────────────────────────────────────────────────────
async function loadFechamento(forceRefresh) {
  if (forceRefresh) {
    const el = document.getElementById('fechamentoContent');
    if (el) el.innerHTML = '<div class="loading"><div class="spinner"></div>Atualizando...</div>';
  }
  try {
    const r = await api('GET', '/motoboy/fechamento');
    renderFechamento(r.fechamento);
    const ts = document.getElementById('fechamentoTs');
    if (ts) ts.textContent = 'Atualizado às ' + new Date().toLocaleTimeString('pt-BR', {hour:'2-digit',minute:'2-digit'});
  } catch(e){}
}

function renderFechamento(f) {
  const el = document.getElementById('fechamentoContent');
  if (!f) { el.innerHTML = '<div class="empty-state"><div class="empty-icon">📊</div><div class="empty-text">Nenhum dado hoje</div></div>'; return; }

  const repasse = parseFloat(f.total_a_repassar) || 0;
  const confirmadoMb = f.repasse_confirmado === '1' || f.repasse_confirmado === 1;
  const confirmadoAdmin = f.alan_confirmou === '1' || f.alan_confirmou === 1;
  const fmtR = v => 'R$ ' + parseFloat(v||0).toLocaleString('pt-BR',{minimumFractionDigits:2});

  // Status do dinheiro
  let dinheiroStatus = '';
  if (repasse <= 0) {
    dinheiroStatus = '';
  } else if (confirmadoAdmin) {
    dinheiroStatus = `<div style="margin-top:10px;padding:10px 12px;border-radius:10px;background:#f0fdf4;border:1px solid #bbf7d0;font-size:var(--sz-text-base);color:#166534;font-weight:700">✅ Repasse confirmado pela Senderzz</div>`;
  } else if (confirmadoMb) {
    dinheiroStatus = `<div style="margin-top:10px;padding:10px 12px;border-radius:10px;background:#fff7ed;border:1px solid #fed7aa;font-size:var(--sz-text-base);color:#9a3412;font-weight:700">⏳ Aguardando confirmação da Senderzz</div>`;
  } else {
    dinheiroStatus = `<div style="margin-top:10px">
      <div style="font-size:var(--sz-text-meta);color:#6b7280;margin-bottom:8px">Envie PIX de <strong>${fmtR(repasse)}</strong> para a conta Senderzz e confirme abaixo.</div>
      <button class="btn btn-primary" onclick="confirmarRepasse()" style="width:100%">✅ Confirmo que enviei o PIX</button>
    </div>`;
  }

  el.innerHTML = `
    <div class="card">
      <div class="card-title" style="margin-bottom:12px">📊 Fechamento de Hoje</div>
      <div class="stats-grid">
        <div class="stat-box"><div class="stat-num">${f.total_entregues}</div><div class="stat-label">Entregues</div></div>
        <div class="stat-box"><div class="stat-num" style="color:#dc2626">${f.total_frustrados}</div><div class="stat-label">Frustrados</div></div>
        <div class="stat-box"><div class="stat-num">${f.total_pedidos}</div><div class="stat-label">Total</div></div>
      </div>
    </div>
    <div class="card">
      <div class="card-title" style="margin-bottom:12px">💰 Recebimentos coletados</div>
      <div style="display:flex;flex-direction:column;gap:10px">
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span>📱 PIX <span style="font-size:var(--sz-text-sm);color:#6b7280">(já na conta Senderzz)</span></span>
          <strong style="color:#16a34a">${fmtR(f.total_pix)}</strong>
        </div>
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span>💳 Cartão <span style="font-size:var(--sz-text-sm);color:#6b7280">(já na conta Senderzz)</span></span>
          <strong style="color:#16a34a">${fmtR(f.total_cartao)}</strong>
        </div>
        <div style="display:flex;justify-content:space-between;align-items:center;border-top:1px solid #f3f4f6;padding-top:10px">
          <div>
            <div>💵 <strong>Dinheiro a repassar</strong></div>
            <div style="font-size:var(--sz-text-sm);color:#9ca3af;margin-top:2px">Pendente de confirmação pela Senderzz</div>
          </div>
          <strong style="color:${repasse>0?'#E8650A':'#6b7280'};font-size:var(--sz-text-lg)">${fmtR(repasse)}</strong>
        </div>
      </div>
      ${dinheiroStatus}
    </div>`;
}

async function confirmarRepasse() {
  try {
    const r = await api('POST', '/motoboy/confirmar-repasse');
    if (!r.ok) { toast('Erro.'); return; }
    toast('✅ Repasse confirmado!');
    loadFechamento();
  } catch(e) { toast('Erro.'); }
}

// ── Helpers ────────────────────────────────────────────────────────────────────
async function api(method, path, body = null) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 15000);
  const token = SESSION?.token || '';
  const sep = path.includes('?') ? '&' : '?';
  const auth = token ? sep + '_szt=' + encodeURIComponent(token) : '';
  const sep2 = (path + auth).includes('?') ? '&' : '?';
  const url = API + path + auth + sep2 + '_ts=' + Date.now();
  const opts = {
    method,
    signal: controller.signal,
    cache: 'no-store',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-Motoboy-Token': token,
      'Cache-Control': 'no-cache',
      'Pragma': 'no-cache'
    }
  };
  if (body !== null && body !== undefined) opts.body = JSON.stringify(body);
  try {
    const res = await fetch(url, opts);
    clearTimeout(timer);
    // APENAS 401 = sessão/token realmente inválido → desloga.
    // 403 e outros status são erros de NEGÓCIO (ex.: "fechamento pendente",
    // "fora do horário", etc.) e NÃO devem deslogar o motoboy. Antes, qualquer
    // 403 era tratado como "session_expired", deslogando indevidamente — esse era
    // o bug do "Iniciar Rota".
    if (res.status === 401) {
      if (_loteIntervalId) { clearInterval(_loteIntervalId); _loteIntervalId = null; }
      SESSION = null;
      localStorage.removeItem('sz_mb_session');
      window.location.reload();
      throw new Error('session_expired');
    }
    // Verifica content-type antes de parsear — servidor pode retornar HTML (cache/redirect)
    const ct = res.headers.get('content-type') || '';
    if (!ct.includes('application/json') && !ct.includes('text/json')) {
      const txt = await res.text().catch(() => '');
      console.error('[Senderzz] resposta nao-JSON:', res.status, url, txt.substring(0, 300));
      throw new Error('invalid_response:' + res.status);
    }
    // Devolve o JSON mesmo em respostas não-2xx (ex.: 403/409/422/500): os callers
    // já tratam r.ok === false e exibem r.erro num toast, sem deslogar.
    return res.json();
  } catch(e) {
    clearTimeout(timer);
    throw e;
  }
}

function switchTab(name, btn) {
  document.querySelectorAll('.screen').forEach(s => s.classList.remove('active'));
  document.querySelectorAll('.nav-btn').forEach(b => b.classList.remove('active'));
  document.getElementById('screen' + name.charAt(0).toUpperCase() + name.slice(1)).classList.add('active');
  btn.classList.add('active');
  if (name === 'carteira') loadCarteira();
  if (name === 'perfil') loadPerfilBancario();
  if (name === 'fechamento') loadFechamento();
}

function openModal(id)  { document.getElementById(id).classList.add('open'); }
function closeModal(id) { document.getElementById(id).classList.remove('open'); }

function toast(msg, dur = 2500) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.classList.add('show');
  setTimeout(() => t.classList.remove('show'), dur);
}

// ── Carteira ──────────────────────────────────────────────────────────────────
async function loadCarteira() {
  const box = document.getElementById('walletContent');
  box.innerHTML = '<div class="loading"><div class="spinner"></div>Carregando...</div>';
  try {
    const [saldoRes, histRes] = await Promise.all([
      api('GET', '/wallet/saldo'),
      api('GET', '/wallet/historico'),
    ]);
    const s = saldoRes;
    const fmtR = v => 'R$ ' + Number(v||0).toLocaleString('pt-BR', {minimumFractionDigits:2});
    const tipoLabel = t => t === 'entrega' ? '✅ Entrega' : '❌ Frustrado';
    const statusBg  = s => s === 'pago' ? '#dcfce7' : s === 'pendente' ? '#fff7ed' : '#f3f4f6';
    const statusClr = s => s === 'pago' ? '#166534' : s === 'pendente' ? '#9a3412' : '#374151';

    const hist = (histRes.historico || []).map(g => `
      <div style="display:flex;align-items:center;justify-content:space-between;padding:10px 0;border-bottom:1px solid #f3f4f6;gap:8px">
        <div>
          <div style="font-weight:700;font-size:var(--sz-text-base)">${tipoLabel(g.tipo)} <span style="color:#6b7280;font-weight:500">${g.pedido}</span></div>
          <div style="font-size:var(--sz-text-sm);color:#9ca3af;margin-top:2px">${g.data}</div>
        </div>
        <div style="text-align:right;flex-shrink:0">
          <div style="font-weight:700;font-size:var(--sz-text-lg);color:${g.tipo==='entrega'?'#16a34a':'#dc2626'}">${g.valor}</div>
          <div style="font-size:var(--sz-text-xs);font-weight:700;padding:2px 8px;border-radius:999px;background:${statusBg(g.status)};color:${statusClr(g.status)};margin-top:3px;display:inline-block">${g.status}</div>
        </div>
      </div>`).join('') || '<p style="color:#9ca3af;font-size:var(--sz-text-base);padding:16px 0;text-align:center">Nenhum ganho registrado ainda.</p>';

    box.innerHTML = `
      <div class="card">
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-bottom:14px">
          <div style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:12px;padding:14px">
            <div style="font-size:var(--sz-text-xs);font-weight:700;color:#6b7280;text-transform:none;letter-spacing:0;margin-bottom:4px">A receber</div>
            <div style="font-size:var(--sz-text-xl);font-weight:700;color:#16a34a;letter-spacing:-.02em">${fmtR(s.pendente)}</div>
            <div style="font-size:var(--sz-text-sm);color:#6b7280;margin-top:3px">${s.qtd_entregas} entregas · ${s.qtd_frustrados} frustrados</div>
          </div>
          <div style="background:#fff7ed;border:1px solid #fed7aa;border-radius:12px;padding:14px">
            <div style="font-size:var(--sz-text-xs);font-weight:700;color:#6b7280;text-transform:none;letter-spacing:0;margin-bottom:4px">Próximo pagamento</div>
            <div style="font-size:var(--sz-text-lg);font-weight:700;color:#E8650A;letter-spacing:-.015em">${s.sexta}</div>
            <div style="font-size:var(--sz-text-sm);color:#6b7280;margin-top:3px">Toda sexta-feira via PIX</div>
          </div>
        </div>
        <div style="font-size:var(--sz-text-xs);font-weight:700;color:#9ca3af;text-transform:none;letter-spacing:0;margin-bottom:8px">Histórico de ganhos</div>
        ${hist}
      </div>`;
  } catch(e) {
    box.innerHTML = '<div class="card"><p style="color:#dc2626">Erro ao carregar carteira.</p></div>';
  }
}

// ── Perfil bancário ───────────────────────────────────────────────────────────
async function loadPerfilBancario() {
  try {
    const r = await api('GET', '/wallet/bancario');
    if (r.bancario) {
      const b = r.bancario;
      document.getElementById('pf-holder').value   = b.holder_name  || '';
      document.getElementById('pf-cpf').value      = mbFormatCpf(b.holder_cpf || '');
      document.getElementById('pf-bank').value     = b.bank_name    || '';
      document.getElementById('pf-agency').value   = b.agency       || '';
      document.getElementById('pf-account').value  = b.account_number || '';
      const pt = document.getElementById('pf-pix-type');
      if (pt) pt.value = b.pix_type || 'cpf';
      document.getElementById('pf-pix-key').value  = b.pix_key      || '';
    }
  } catch(e) {}
}

async function mbSalvarBancario() {
  const msg = document.getElementById('perfilMsg');
  const body = {
    holder_name:    document.getElementById('pf-holder').value.trim(),
    holder_cpf:     document.getElementById('pf-cpf').value.replace(/\D/g,''),
    bank_name:      document.getElementById('pf-bank').value.trim(),
    agency:         document.getElementById('pf-agency').value.trim(),
    account_number: document.getElementById('pf-account').value.trim(),
    account_type:   'corrente',
    pix_type:       document.getElementById('pf-pix-type').value,
    pix_key:        document.getElementById('pf-pix-key').value.trim(),
  };
  if (!body.holder_name) { mbPerfilMsg('Informe o nome completo.', false); return; }
  if (body.holder_cpf.length !== 11) { mbPerfilMsg('CPF inválido.', false); return; }
  if (!body.pix_key) { mbPerfilMsg('Informe a chave PIX.', false); return; }
  try {
    const r = await api('POST', '/wallet/bancario', body);
    mbPerfilMsg(r.msg || (r.ok ? 'Salvo!' : 'Erro.'), !!r.ok);
  } catch(e) { mbPerfilMsg('Erro de conexão.', false); }
}

function mbPerfilMsg(txt, ok) {
  const el = document.getElementById('perfilMsg');
  el.textContent = txt;
  el.style.display = 'block';
  el.style.background = ok ? '#dcfce7' : '#fee2e2';
  el.style.color = ok ? '#166534' : '#991b1b';
}

function mbCpfMask(el) {
  let v = el.value.replace(/\D/g,'').slice(0,11);
  v = v.replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d{1,2})$/,'$1-$2');
  el.value = v;
}
function mbFormatCpf(v) {
  v = (v||'').replace(/\D/g,'').slice(0,11);
  return v.replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d{1,2})$/,'$1-$2');
}
function mbFormatCnpj(v) {
  v = (v||'').replace(/\D/g,'').slice(0,14);
  return v.replace(/(\d{2})(\d)/,'$1.$2').replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d)/,'$1/$2').replace(/(\d{4})(\d{1,2})$/,'$1-$2');
}
function mbMaskPixKey(el) {
  const type = document.getElementById('pf-pix-type')?.value || '';
  if (type === 'cpf') el.value = mbFormatCpf(el.value);
  if (type === 'cnpj') el.value = mbFormatCnpj(el.value);
}
function mbUpdatePixPlaceholder() {
  const type = document.getElementById('pf-pix-type').value;
  const ph = {cpf:'000.000.000-00',cnpj:'00.000.000/0000-00',email:'seu@email.com',telefone:'+55 (11) 99999-9999',aleatoria:'Chave aleatória (UUID)'};
  const key = document.getElementById('pf-pix-key');
  key.placeholder = ph[type]||'';
  mbMaskPixKey(key);
}

// ── Retry de comprovantes pendentes ───────────────────────────────────────────
const SZ_COMP_PENDING_KEY = 'sz_mb_comp_pending';

function szMbSalvarComprovantePendente(pedidoId, tipo, fotos) {
  try {
    const lista = JSON.parse(localStorage.getItem(SZ_COMP_PENDING_KEY) || '[]');
    lista.push({ pedidoId, tipo, fotos, ts: Date.now() });
    localStorage.setItem(SZ_COMP_PENDING_KEY, JSON.stringify(lista.slice(-20)));
  } catch(e) {}
}

async function szMbRetryComprovantes() {
  let lista;
  try { lista = JSON.parse(localStorage.getItem(SZ_COMP_PENDING_KEY) || '[]'); }
  catch(e) { return; }
  if (!lista.length) return;
  const sobrou = [];
  for (const item of lista) {
    if (Date.now() - (item.ts||0) > 48*3600*1000) continue; // descarta >48h
    try {
      const r = await api('POST', '/motoboy/comprovante', {
        pedido_id: item.pedidoId, tipo_pgto: item.tipo, fotos: item.fotos, baixa_por: 'motoboy'
      });
      if (!r.ok) sobrou.push(item);
    } catch(e) { sobrou.push(item); }
  }
  try {
    sobrou.length
      ? localStorage.setItem(SZ_COMP_PENDING_KEY, JSON.stringify(sobrou))
      : localStorage.removeItem(SZ_COMP_PENDING_KEY);
  } catch(e) {}
}

async function mbTrocarSenha() {
  const atual   = document.getElementById('pf-senha-atual').value;
  const nova    = document.getElementById('pf-senha-nova').value;
  const confirm = document.getElementById('pf-senha-confirm').value;
  const msg     = document.getElementById('perfilSenhaMsg');
  const showMsg = (txt, ok) => {
    if (!msg) return;
    msg.textContent = txt;
    msg.style.background = ok ? '#dcfce7' : '#fee2e2';
    msg.style.color      = ok ? '#166534' : '#991b1b';
    msg.style.display    = 'block';
  };
  if (!atual)           { showMsg('Informe a senha atual.', false); return; }
  if (nova.length < 4)  { showMsg('Nova senha deve ter ao menos 4 caracteres.', false); return; }
  if (nova !== confirm)  { showMsg('As senhas não coincidem.', false); return; }
  try {
    const r = await api('POST', '/motoboy/trocar-senha', { senha_atual: atual, senha_nova: nova });
    if (!r.ok) { showMsg(r.erro || 'Senha atual incorreta.', false); return; }
    showMsg('✅ Senha alterada com sucesso!', true);
    document.getElementById('pf-senha-atual').value   = '';
    document.getElementById('pf-senha-nova').value    = '';
    document.getElementById('pf-senha-confirm').value = '';
  } catch(e) { showMsg('Erro ao salvar. Tente novamente.', false); }
}

boot();
// Retry de comprovantes pendentes de sessões anteriores
setTimeout(szMbRetryComprovantes, 3000);

</script>
</body>
</html>
