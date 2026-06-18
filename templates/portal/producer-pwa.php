<?php
/**
 * Senderzz — PWA do Produtor
 * Rota: /produtor-app/
 * Instalável no iOS e Android. Auth via cookie senderzz_portal_session.
 */
if ( ! defined( 'ABSPATH' ) ) exit;

// Redireciona para portal desktop se não for mobile e não for instalado
// (apenas envia os headers corretos — a detecção fica no JS)
header( 'Content-Type: text/html; charset=utf-8' );
header( 'Cache-Control: no-store' );

$plugin_url = TPC_URL;
$sw_url     = $plugin_url . 'assets/js/sz-producer-sw.js';
$logo_url   = esc_url( $plugin_url . 'assets/images/senderzz-logo.png' );
$push_icon_url = esc_url( $plugin_url . 'assets/images/senderzz-raio-192.png?v=243' );
$portal_url = esc_url( home_url( '/meus-pedidos/' ) );
$rest_base  = esc_js( rest_url( 'senderzz/v1' ) );
$ajax_url   = esc_js( admin_url( 'admin-ajax.php' ) );
?>
<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, ">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="default">
<meta name="apple-mobile-web-app-title" content="Pedidos COD">
<meta name="theme-color" content="#E8650A">
<meta name="description" content="Gerencie seus pedidos Senderzz">
<link rel="apple-touch-icon" href="<?php echo $push_icon_url; ?>">
<link rel="icon" type="image/png" sizes="192x192" href="<?php echo $push_icon_url; ?>">
<title>Senderzz — Produtor</title>
<style>
*{box-sizing:border-box;margin:0;padding:0;-webkit-tap-highlight-color:transparent}
:root{
  --ac:#E8650A;--ac2:#f05a00;--bg:#f6f7f9;--surface:#fff;--surface2:#f9fafb;
  --bd:#e5e7eb;--tx:#111827;--tx2:#6b7280;--tx3:#9ca3af;
  --r:14px;--rs:10px;
  --safe-top:env(safe-area-inset-top,0px);
  --safe-bot:env(safe-area-inset-bottom,0px);
}
html,body{height:100%;background:var(--bg);color:var(--tx);font-family:var(--sz-font);overscroll-behavior:none}
.app{display:flex;flex-direction:column;height:100%;height:100dvh;overflow:hidden}

/* ── Header ── */
.sz-header{background:var(--ac);color:#fff;padding:calc(14px + var(--safe-top)) 16px 14px;display:flex;align-items:center;gap:12px;flex-shrink:0}
.sz-header-logo{height:28px;filter:brightness(0) invert(1)}
.sz-header-title{font-size:var(--sz-text-lg);font-weight:700;letter-spacing:-.015em;flex:1}
.sz-header-badge{background:rgba(255,255,255,.2);border-radius:99px;padding:3px 10px;font-size:var(--sz-text-sm);font-weight:700}
.sz-header-btn{background:none;border:none;color:#fff;cursor:pointer;padding:4px;display:flex;align-items:center;justify-content:center;border-radius:8px}
.sz-header-btn:active{background:rgba(255,255,255,.15)}

/* ── Content area ── */
.sz-content{flex:1;overflow-y:auto;-webkit-overflow-scrolling:touch}
.sz-screen{display:none;padding:16px;min-height:100%}
.sz-screen.active{display:block}

/* ── Bottom nav ── */
.sz-bottom-nav{background:var(--surface);border-top:1px solid var(--bd);display:flex;flex-shrink:0;padding-bottom:var(--safe-bot)}
.sz-nav-btn{flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:10px 4px 8px;gap:3px;font-size:var(--sz-text-xs);font-weight:600;color:var(--tx3);cursor:pointer;border:none;background:none;transition:color .15s}
.sz-nav-btn.active{color:var(--ac)}
.sz-nav-btn svg{width:22px;height:22px;stroke-width:2}

/* ── Cards ── */
.sz-card{background:var(--surface);border:1px solid var(--bd);border-radius:var(--r);padding:16px;margin-bottom:10px}
.sz-card-title{font-size:var(--sz-text-lg);font-weight:700;color:var(--tx);margin-bottom:4px}
.sz-card-sub{font-size:var(--sz-text-meta);color:var(--tx2);line-height:1.4}

/* ── KPI strip ── */
.sz-kpi-strip{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-bottom:14px}
.sz-kpi{background:var(--surface);border:1px solid var(--bd);border-radius:var(--rs);padding:12px 8px;text-align:center}
.sz-kpi-val{font-size:var(--sz-text-xl);font-weight:700;color:var(--ac);letter-spacing:-.02em;line-height:1}
.sz-kpi-lbl{font-size:var(--sz-text-xs);color:var(--tx3);margin-top:4px;font-weight:700;text-transform:none;letter-spacing:0}

/* ── Order list ── */
.sz-order{background:var(--surface);border:1px solid var(--bd);border-radius:var(--r);padding:14px 16px;margin-bottom:8px;display:flex;align-items:center;gap:12px;cursor:pointer;transition:background .1s}
.sz-order:active{background:var(--surface2)}
.sz-order-main{flex:1;min-width:0}
.sz-order-id{font-size:var(--sz-text-base);font-weight:700;color:var(--tx)}
.sz-order-info{font-size:var(--sz-text-meta);color:var(--tx2);margin-top:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.sz-order-right{text-align:right;flex-shrink:0}
.sz-order-val{font-size:var(--sz-text-base);font-weight:700;color:var(--tx)}
.sz-chevron{color:var(--tx3);margin-left:4px}

/* ── Badges ── */
.sz-badge{display:inline-flex;align-items:center;padding:3px 9px;border-radius:99px;font-size:var(--sz-text-sm);font-weight:700;text-transform:capitalize}
.sz-badge-onhold,.sz-badge-aguardando{background:#fef9c3;color:#854d0e}
.sz-badge-aprovado,.sz-badge-approved{background:#dcfce7;color:#166534}
.sz-badge-enviado,.sz-badge-shipped{background:#dbeafe;color:#1e40af}
.sz-badge-entregue,.sz-badge-completed{background:#d1fae5;color:#065f46}
.sz-badge-cancelled,.sz-badge-cancelado{background:#fee2e2;color:#991b1b}
.sz-badge-erro{background:#fee2e2;color:#991b1b}
.sz-badge-embalado{background:#ede9fe;color:#5b21b6}
.sz-badge-separado{background:#e0f2fe;color:#0369a1}
.sz-badge-default{background:#f3f4f6;color:#374151}

/* ── Wallet ── */
.sz-wallet-hero{background:linear-gradient(135deg,#1a1410,#2d1f14);color:#fff;border-radius:var(--r);padding:22px 20px;margin-bottom:14px}
.sz-wallet-label{font-size:var(--sz-text-sm);font-weight:700;opacity:.6;text-transform:none;letter-spacing:0;margin-bottom:6px}
.sz-wallet-amount{font-size:var(--sz-text-hero);font-weight:700;letter-spacing:-.02em;color:#fff}
.sz-wallet-sub{font-size:var(--sz-text-meta);opacity:.55;margin-top:4px}
.sz-pix-btn{display:block;width:100%;margin-top:16px;padding:13px;background:var(--ac);color:#fff;border:none;border-radius:var(--rs);font-size:var(--sz-text-lg);font-weight:700;cursor:pointer;text-align:center}
.sz-pix-res{background:var(--surface);border:1px solid var(--bd);border-radius:var(--rs);padding:14px;margin-top:12px;display:none}
.sz-pix-code{font-family:var(--sz-font);font-size:var(--sz-text-sm);word-break:break-all;background:var(--surface2);padding:10px;border-radius:8px;margin:8px 0;color:var(--tx);user-select:all}
.sz-copy-btn{width:100%;padding:10px;background:var(--tx);color:#fff;border:none;border-radius:var(--rs);font-weight:700;font-size:var(--sz-text-base);cursor:pointer;margin-top:6px}
.sz-tx-row{display:flex;justify-content:space-between;align-items:center;padding:10px 0;border-bottom:1px solid var(--bd);font-size:var(--sz-text-base)}
.sz-tx-row:last-child{border:none}
.sz-tx-desc{color:var(--tx2);flex:1;padding-right:8px}
.sz-tx-val{font-weight:700;white-space:nowrap}
.sz-tx-val.pos{color:#16a34a}.sz-tx-val.neg{color:#dc2626}

/* ── Order detail sheet ── */
.sz-sheet{position:fixed;inset:0;z-index:200;display:none;flex-direction:column}
.sz-sheet.open{display:flex}
.sz-sheet-overlay{flex:1;background:rgba(0,0,0,.4);backdrop-filter:blur(2px)}
.sz-sheet-body{background:var(--surface);border-radius:22px 22px 0 0;padding:0 0 calc(20px + var(--safe-bot));max-height:85dvh;overflow-y:auto;flex-shrink:0}
.sz-sheet-handle{width:40px;height:4px;background:var(--bd);border-radius:99px;margin:12px auto 16px}
.sz-sheet-content{padding:0 20px 20px}
.sz-sheet-title{font-size:var(--sz-text-lg);font-weight:700;color:var(--tx);margin-bottom:4px}
.sz-sheet-sub{font-size:var(--sz-text-base);color:var(--tx2);margin-bottom:16px}
.sz-detail-row{display:flex;justify-content:space-between;padding:9px 0;border-bottom:1px solid var(--bd);font-size:var(--sz-text-base)}
.sz-detail-row:last-child{border:none}
.sz-detail-label{color:var(--tx2)}
.sz-detail-val{font-weight:700;color:var(--tx);text-align:right;max-width:60%}
.sz-action-btns{display:grid;gap:8px;margin-top:16px}

/* ── Buttons ── */
.sz-btn{width:100%;padding:14px;border:none;border-radius:var(--rs);font-size:var(--sz-text-lg);font-weight:700;cursor:pointer;transition:opacity .15s}
.sz-btn:active{opacity:.8}
.sz-btn-primary{background:var(--ac);color:#fff}
.sz-btn-danger{background:#dc2626;color:#fff}
.sz-btn-ghost{background:var(--surface2);color:var(--tx);border:1px solid var(--bd)}

/* ── States ── */
.sz-loading{text-align:center;padding:40px 20px;color:var(--tx3)}
.sz-loading-spin{width:32px;height:32px;border:3px solid var(--bd);border-top-color:var(--ac);border-radius:50%;animation:spin .7s linear infinite;margin:0 auto 12px}
@keyframes spin{to{transform:rotate(360deg)}}
.sz-empty{text-align:center;padding:48px 20px;color:var(--tx3)}
.sz-empty-icon{font-size:var(--sz-text-hero);margin-bottom:12px}
.sz-empty strong{display:block;color:var(--tx2);font-size:var(--sz-text-lg);margin-bottom:4px}

/* ── Toast ── */
.sz-toast{position:fixed;bottom:calc(80px + var(--safe-bot));left:50%;transform:translateX(-50%) translateY(20px);background:#111827;color:#fff;padding:11px 18px;border-radius:12px;font-size:var(--sz-text-base);font-weight:700;z-index:999;opacity:0;transition:all .25s;pointer-events:none;white-space:nowrap;max-width:calc(100% - 32px);text-align:center}
.sz-toast.show{opacity:1;transform:translateX(-50%) translateY(0)}

/* ── Install banner ── */
.sz-install-banner{background:var(--ac);color:#fff;padding:12px 16px;display:flex;align-items:center;gap:10px;flex-shrink:0}
.sz-install-banner p{flex:1;font-size:var(--sz-text-base);font-weight:700;line-height:1.3}
.sz-install-banner button{background:#fff;color:var(--ac);border:none;border-radius:8px;padding:8px 14px;font-size:var(--sz-text-base);font-weight:700;cursor:pointer;flex-shrink:0}
.sz-install-banner .sz-close-banner{background:none;color:rgba(255,255,255,.7);font-size:var(--sz-text-xl);padding:0 4px;flex-shrink:0}

/* ── Filtros ── */
.sz-filter-row{display:flex;gap:6px;overflow-x:auto;padding-bottom:4px;margin-bottom:12px;-ms-overflow-style:none;scrollbar-width:none}
.sz-filter-row::-webkit-scrollbar{display:none}
.sz-filter-chip{flex-shrink:0;padding:6px 14px;border-radius:99px;border:1.5px solid var(--bd);background:var(--surface);font-size:var(--sz-text-meta);font-weight:700;color:var(--tx2);cursor:pointer;white-space:nowrap}
.sz-filter-chip.active{background:var(--ac);border-color:var(--ac);color:#fff}

/* ── Login ── */
.sz-login-wrap{display:flex;align-items:center;justify-content:center;min-height:100dvh;padding:24px;background:var(--bg)}
.sz-login-card{background:var(--surface);border:1px solid var(--bd);border-radius:22px;padding:32px 28px;width:100%;max-width:380px}
.sz-login-logo{height:34px;margin-bottom:20px}
.sz-login-title{font-size:var(--sz-text-xl);font-weight:700;color:var(--tx);margin-bottom:6px;letter-spacing:-.02em}
.sz-login-sub{font-size:var(--sz-text-md);color:var(--tx2);margin-bottom:24px;line-height:1.5}
.sz-field{margin-bottom:14px}
.sz-field label{display:block;font-size:var(--sz-text-sm);font-weight:700;color:var(--tx3);text-transform:none;letter-spacing:0;margin-bottom:6px}
.sz-field input{width:100%;height:46px;border:1.5px solid var(--bd);border-radius:var(--rs);padding:0 14px;font-size:var(--sz-text-lg);color:var(--tx);background:var(--surface);outline:none;-webkit-appearance:none}
.sz-field input:focus{border-color:var(--ac);box-shadow:0 0 0 4px rgba(232,101,10,.09)}
.sz-login-btn{width:100%;height:48px;background:var(--ac);color:#fff;border:none;border-radius:var(--rs);font-size:var(--sz-text-lg);font-weight:700;cursor:pointer;margin-top:6px}
.sz-login-err{color:#dc2626;font-size:var(--sz-text-base);font-weight:700;margin-bottom:12px;display:none}
</style>
</head>
<body>

<!-- Toast -->
<div class="sz-toast" id="sz-toast"></div>

<!-- Login screen (mostrada antes do app carregar) -->
<div class="sz-login-wrap" id="sz-login-wrap" style="display:none">
  <div class="sz-login-card">
    <img src="<?php echo $logo_url; ?>" class="sz-login-logo" alt="Senderzz">
    <h2 class="sz-login-title">Entrar</h2>
    <p class="sz-login-sub">Acesse o portal de pedidos Senderzz.</p>
    <div class="sz-login-err" id="sz-login-err"></div>
    <div class="sz-field"><label>E-mail</label><input type="email" id="sz-email" autocomplete="email" placeholder="seu@email.com"></div>
    <div class="sz-field"><label>Senha</label><input type="password" id="sz-pass" autocomplete="current-password" placeholder="••••••••"></div>
    <button class="sz-login-btn" onclick="szLogin()">Entrar</button>
  </div>
</div>

<!-- Install banner (iOS) -->
<div class="sz-install-banner" id="sz-install-banner" style="display:none">
  <p>Instale o app: toque em <strong>Compartilhar</strong> → <strong>Adicionar à tela de início</strong></p>
  <button class="sz-close-banner" onclick="document.getElementById('sz-install-banner').style.display='none'">✕</button>
</div>

<!-- Main app -->
<div class="app" id="sz-app" style="display:none">

  <!-- Header -->
  <div class="sz-header">
    <img src="<?php echo $logo_url; ?>" class="sz-header-logo" alt="">
    <span class="sz-header-title" id="sz-header-title">Pedidos</span>
    <span class="sz-header-badge" id="sz-saldo-badge" style="display:none"></span>
    <button class="sz-header-btn" onclick="szRefresh()" title="Atualizar">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" width="20" height="20"><path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/></svg>
    </button>
  </div>

  <!-- Content -->
  <div class="sz-content">

    <!-- Pedidos -->
    <div class="sz-screen active" id="sz-screen-orders">
      <div class="sz-filter-row" id="sz-filter-row">
        <button class="sz-filter-chip active" onclick="szFilter('',this)">Todos</button>
        <button class="sz-filter-chip" onclick="szFilter('on-hold',this)">Aguardando</button>
        <button class="sz-filter-chip" onclick="szFilter('aprovado',this)">Aprovados</button>
        <button class="sz-filter-chip" onclick="szFilter('enviado',this)">Enviados</button>
        <button class="sz-filter-chip" onclick="szFilter('entregue',this)">Entregues</button>
      </div>
      <div class="sz-kpi-strip" id="sz-kpi-strip">
        <div class="sz-kpi"><div class="sz-kpi-val" id="sz-kpi-total">—</div><div class="sz-kpi-lbl">Pedidos</div></div>
        <div class="sz-kpi"><div class="sz-kpi-val" id="sz-kpi-aguard">—</div><div class="sz-kpi-lbl">Aguardando</div></div>
        <div class="sz-kpi"><div class="sz-kpi-val" id="sz-kpi-enviado">—</div><div class="sz-kpi-lbl">Em trânsito</div></div>
      </div>
      <div id="sz-order-list"><div class="sz-loading"><div class="sz-loading-spin"></div>Carregando pedidos…</div></div>
    </div>

    <!-- Carteira -->
    <div class="sz-screen" id="sz-screen-wallet">
      <div class="sz-wallet-hero">
        <div class="sz-wallet-label">Saldo de expedição</div>
        <div class="sz-wallet-amount" id="sz-wallet-amount">—</div>
        <div class="sz-wallet-sub">Disponível para envios</div>
      </div>
      <?php if ( true ) : // sempre mostra PIX ?>
      <div class="sz-card">
        <div class="sz-card-title">Recarregar via PIX</div>
        <div class="sz-card-sub" style="margin-bottom:12px">Crédito em até 5 minutos após o pagamento.</div>
        <div class="sz-field"><label>Valor (mín. R$ 10,00)</label><input type="number" id="sz-pix-amt" min="10" step="0.01" placeholder="0,00"></div>
        <button class="sz-pix-btn" onclick="szGenPix()">Gerar PIX</button>
        <div class="sz-pix-res" id="sz-pix-res">
          <div style="font-size:var(--sz-text-meta);font-weight:700;color:var(--tx3);margin-bottom:6px">PIX Copia e Cola</div>
          <div class="sz-pix-code" id="sz-pix-code"></div>
          <button class="sz-copy-btn" onclick="szCopyPix()">Copiar código PIX</button>
          <button class="sz-pix-btn" style="margin-top:8px;background:var(--surface2);color:var(--tx);font-size:var(--sz-text-base);padding:10px" onclick="szPixConfirm()">✓ Já paguei — confirmar</button>
          <div id="sz-pix-paid-msg" style="display:none;font-size:var(--sz-text-meta);font-weight:700;padding:8px 0;text-align:center"></div>
        </div>
      </div>
      <?php endif; ?>
      <div class="sz-card">
        <div class="sz-card-title" style="margin-bottom:12px">Últimas movimentações</div>
        <div id="sz-wallet-txs"><div class="sz-loading"><div class="sz-loading-spin"></div></div></div>
      </div>
    </div>

    <!-- Perfil -->
    <div class="sz-screen" id="sz-screen-profile">
      <div class="sz-card" style="margin-bottom:10px">
        <div class="sz-card-title" id="sz-profile-name">—</div>
        <div class="sz-card-sub" id="sz-profile-email">—</div>
      </div>
      <button class="sz-btn sz-btn-ghost" style="margin-top:10px" onclick="szLogout()">Sair da conta</button>
    </div>

  </div>

  <!-- Bottom nav -->
  <div class="sz-bottom-nav">
    <button class="sz-nav-btn active" onclick="szNav('orders',this)">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></svg>
      Pedidos
    </button>
    <button class="sz-nav-btn" onclick="szNav('wallet',this)">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="1" y="4" width="22" height="16" rx="2"/><path d="M1 10h22"/></svg>
      Carteira
    </button>
    <button class="sz-nav-btn" onclick="szNav('profile',this)">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="8" r="4"/><path d="M4 20c0-4 3.6-7 8-7s8 3 8 7"/></svg>
      Perfil
    </button>
  </div>
</div>

<!-- Order detail sheet -->
<div class="sz-sheet" id="sz-sheet">
  <div class="sz-sheet-overlay" onclick="szSheetClose()"></div>
  <div class="sz-sheet-body">
    <div class="sz-sheet-handle"></div>
    <div class="sz-sheet-content" id="sz-sheet-content"></div>
  </div>
</div>

<script>
// ── Config ──────────────────────────────────────────────────────────────────
var REST  = '<?php echo $rest_base; ?>';
var AJAX  = '<?php echo $ajax_url; ?>';
var SZ    = { user: null, orders: [], filterStatus: '', nonce: '' };

// ── Utils ────────────────────────────────────────────────────────────────────
function fmt(v){ return 'R$ '+parseFloat(v||0).toLocaleString('pt-BR',{minimumFractionDigits:2,maximumFractionDigits:2}); }
function esc(s){ var d=document.createElement('div'); d.textContent=s||''; return d.innerHTML; }
function toast(msg, ok){
  var t=document.getElementById('sz-toast');
  t.textContent=msg; t.className='sz-toast show'+(ok===false?' err':'');
  clearTimeout(t._t); t._t=setTimeout(function(){t.className='sz-toast';},2800);
}
function badgeCls(s){
  var map={'on-hold':'onhold','aguardando':'aguardando','aprovado':'aprovado','approved':'aprovado',
    'enviado':'enviado','shipped':'enviado','entregue':'entregue','completed':'entregue',
    'cancelled':'cancelled','cancelado':'cancelado','erro':'erro','embalado':'embalado','separado':'separado'};
  return 'sz-badge sz-badge-'+(map[s]||'default');
}
function badgeLbl(s){
  var map={'on-hold':'Aguardando','aguardando':'Aguardando','aprovado':'Aprovado','approved':'Aprovado',
    'enviado':'Enviado','shipped':'Enviado','entregue':'Entregue','completed':'Entregue',
    'cancelled':'Cancelado','cancelado':'Cancelado','erro':'Erro','embalado':'Embalado','separado':'Separado'};
  return map[s]||s;
}

// ── Auth ─────────────────────────────────────────────────────────────────────
function szLogin(){
  var email=document.getElementById('sz-email').value.trim();
  var pass=document.getElementById('sz-pass').value;
  var err=document.getElementById('sz-login-err');
  if(!email||!pass){err.textContent='Preencha e-mail e senha.';err.style.display='block';return;}
  err.style.display='none';
  fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
    body:'action=sz_producer_pwa&szaction=login&email='+encodeURIComponent(email)+'&password='+encodeURIComponent(pass)+'&_ajax_nonce='+SZ.nonce})
  .then(function(r){return r.json()})
  .then(function(d){
    if(d.success){szBoot();}
    else{err.textContent=d.data&&d.data.message?d.data.message:'E-mail ou senha incorretos.';err.style.display='block';}
  }).catch(function(){err.textContent='Erro de conexão. Tente novamente.';err.style.display='block';});
}

function szLogout(){
  fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
    body:'action=sz_producer_pwa&szaction=logout&_ajax_nonce='+SZ.nonce})
  .finally(function(){(window.szReloadSameSection?window.szReloadSameSection():location.reload());});
}

// ── Boot ─────────────────────────────────────────────────────────────────────
function szBoot(){
  fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
    body:'action=sz_producer_pwa&szaction=get_current_user&_ajax_nonce='+SZ.nonce})
  .then(function(r){return r.json()})
  .then(function(d){
    if(d.success&&d.data){
      SZ.user=d.data;
      document.getElementById('sz-login-wrap').style.display='none';
      document.getElementById('sz-app').style.display='flex';
      document.getElementById('sz-profile-name').textContent=SZ.user.name||'Produtor';
      document.getElementById('sz-profile-email').textContent=SZ.user.email||'';
      szLoadOrders();
      szLoadWallet();
    } else {
      document.getElementById('sz-login-wrap').style.display='flex';
      document.getElementById('sz-app').style.display='none';
    }
  }).catch(function(){
    document.getElementById('sz-login-wrap').style.display='flex';
    document.getElementById('sz-app').style.display='none';
  });
}

// ── Orders ────────────────────────────────────────────────────────────────────
function szLoadOrders(){
  fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
    body:'action=sz_producer_pwa&szaction=get_orders&limit=60&_ajax_nonce='+SZ.nonce})
  .then(function(r){return r.json()})
  .then(function(d){
    if(d.success&&d.data){
      SZ.orders=d.data.orders||d.data||[];
      szRenderOrders();
      szUpdateKpis();
      // Atualiza badge de saldo se disponível
      if(d.data.saldo!==undefined){
        var b=document.getElementById('sz-saldo-badge');
        b.textContent=fmt(d.data.saldo); b.style.display='';
      }
    }
  }).catch(function(){
    document.getElementById('sz-order-list').innerHTML='<div class="sz-empty"><div class="sz-empty-icon">⚠️</div><strong>Erro ao carregar</strong><span>Verifique sua conexão.</span></div>';
  });
}

function szUpdateKpis(){
  var orders=SZ.orders;
  var aguard=orders.filter(function(o){return o.status==='on-hold'||o.status==='aguardando';}).length;
  var enviado=orders.filter(function(o){return ['enviado','shipped','embalado','separado'].indexOf(o.status)>=0;}).length;
  document.getElementById('sz-kpi-total').textContent=orders.length;
  document.getElementById('sz-kpi-aguard').textContent=aguard;
  document.getElementById('sz-kpi-enviado').textContent=enviado;
}

function szFilter(status, btn){
  SZ.filterStatus=status;
  document.querySelectorAll('.sz-filter-chip').forEach(function(c){c.classList.remove('active');});
  if(btn)btn.classList.add('active');
  szRenderOrders();
}

function szRenderOrders(){
  var list=document.getElementById('sz-order-list');
  var orders=SZ.filterStatus?SZ.orders.filter(function(o){return o.status===SZ.filterStatus;}):SZ.orders;
  if(!orders.length){
    list.innerHTML='<div class="sz-empty"><div class="sz-empty-icon">📦</div><strong>Nenhum pedido</strong><span>Nenhum pedido neste filtro.</span></div>';
    return;
  }
  list.innerHTML=orders.map(function(o){
    return '<div class="sz-order" onclick="szOrderDetail('+esc(JSON.stringify(o))+')">'
      +'<div class="sz-order-main">'
      +'<div class="sz-order-id">#'+esc(o.order_number||o.id)+' <span class="'+badgeCls(o.status)+'">'+esc(badgeLbl(o.status))+'</span></div>'
      +'<div class="sz-order-info">'+esc(o.customer_name||o.customer_email||'Cliente')+(o.shipping_name?' · '+esc(o.shipping_name):'')+'</div>'
      +'</div>'
      +'<div class="sz-order-right">'
      +'<div class="sz-order-val">'+fmt(o.total||0)+'</div>'
      +'<svg class="sz-chevron" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="m9 18 6-6-6-6"/></svg>'
      +'</div>'
      +'</div>';
  }).join('');
}

// ── Order detail sheet ────────────────────────────────────────────────────────
function szOrderDetail(o){
  var canApprove=o.status==='on-hold';
  var canCancel=['on-hold','aprovado'].indexOf(o.status)>=0;
  var html='<div class="sz-sheet-title">Pedido #'+esc(o.order_number||o.id)+'</div>'
    +'<div class="sz-sheet-sub">'+esc(badgeLbl(o.status))+'</div>'
    +'<div class="sz-detail-row"><span class="sz-detail-label">Cliente</span><span class="sz-detail-val">'+esc(o.customer_name||'—')+'</span></div>'
    +'<div class="sz-detail-row"><span class="sz-detail-label">Total</span><span class="sz-detail-val">'+fmt(o.total||0)+'</span></div>'
    +'<div class="sz-detail-row"><span class="sz-detail-label">Frete</span><span class="sz-detail-val">'+fmt(o.shipping_total||0)+'</span></div>'
    +'<div class="sz-detail-row"><span class="sz-detail-label">Transportadora</span><span class="sz-detail-val">'+esc(o.shipping_name||'—')+'</span></div>'
    +(o.tracking_code?'<div class="sz-detail-row"><span class="sz-detail-label">Rastreio</span><span class="sz-detail-val">'+esc(o.tracking_code)+'</span></div>':'')
    +'<div class="sz-action-btns">'
    +(canApprove?'<button class="sz-btn sz-btn-primary" onclick="szApproveOrder('+o.id+')">✓ Aprovar pedido</button>':'')
    +(canCancel?'<button class="sz-btn sz-btn-danger" onclick="szCancelOrder('+o.id+')">✕ Cancelar pedido</button>':'')
    +'<button class="sz-btn sz-btn-ghost" onclick="szSheetClose()">Fechar</button>'
    +'</div>';
  document.getElementById('sz-sheet-content').innerHTML=html;
  document.getElementById('sz-sheet').classList.add('open');
}
function szSheetClose(){ document.getElementById('sz-sheet').classList.remove('open'); }

function szApproveOrder(id){
  if(!confirm('Aprovar o pedido #'+id+'?'))return;
  szSheetClose();
  fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
    body:'action=sz_producer_pwa&szaction=approve_order&order_id='+id+'&_ajax_nonce='+SZ.nonce})
  .then(function(r){return r.json()})
  .then(function(d){toast(d.data&&d.data.message?d.data.message:(d.success?'Pedido aprovado!':'Erro ao aprovar'),d.success);if(d.success)setTimeout(szLoadOrders,1200);});
}
function szCancelOrder(id){
  if(!confirm('Cancelar o pedido #'+id+'? Esta ação não pode ser desfeita.'))return;
  szSheetClose();
  fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
    body:'action=sz_producer_pwa&szaction=cancel_order&order_id='+id+'&_ajax_nonce='+SZ.nonce})
  .then(function(r){return r.json()})
  .then(function(d){toast(d.data&&d.data.message?d.data.message:(d.success?'Pedido cancelado.':'Erro ao cancelar'),d.success);if(d.success)setTimeout(szLoadOrders,1200);});
}

// ── Wallet ────────────────────────────────────────────────────────────────────
function szLoadWallet(){
  fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
    body:'action=sz_producer_pwa&szaction=get_wallet&_ajax_nonce='+SZ.nonce})
  .then(function(r){return r.json()})
  .then(function(d){
    if(d.success&&d.data){
      document.getElementById('sz-wallet-amount').textContent=fmt(d.data.saldo||0);
      var b=document.getElementById('sz-saldo-badge');
      b.textContent=fmt(d.data.saldo||0); b.style.display='';
    }
  });
  // Carregar transações
  fetch(window.location.origin+'/wp-json/sz-portal/v1/wallet/history?period=30d&n='+SZ.nonce,{credentials:'same-origin'})
  .then(function(r){return r.json()})
  .then(function(d){
    var el=document.getElementById('sz-wallet-txs');
    if(!d.success||!d.data||!d.data.length){el.innerHTML='<p style="color:var(--tx3);font-size:var(--sz-text-base)">Nenhuma movimentação nos últimos 30 dias.</p>';return;}
    el.innerHTML=d.data.slice(0,10).map(function(r){
      var v=parseFloat(r.value||0);
      var isPos=r.movement==='Crédito'||r.movement==='Recarga PIX'||r.movement==='Estorno';
      return '<div class="sz-tx-row"><span class="sz-tx-desc">'+esc(r.description.substring(0,36))+'<br><small>'+esc(r.date)+'</small></span>'
        +'<span class="sz-tx-val '+(isPos?'pos':'neg')+'">'+(isPos?'+':'-')+fmt(Math.abs(v))+'</span></div>';
    }).join('');
  });
}

function szGenPix(){
  var amt=parseFloat(document.getElementById('sz-pix-amt').value);
  if(!amt||amt<10){toast('Valor mínimo: R$ 10,00',false);return;}
  fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
    body:'action=sz_producer_pwa&szaction=gerar_pix&valor='+amt+'&_ajax_nonce='+SZ.nonce})
  .then(function(r){return r.json()})
  .then(function(d){
    if(d.success&&d.data&&d.data.qr_code){
      document.getElementById('sz-pix-code').textContent=d.data.qr_code;
      document.getElementById('sz-pix-res').style.display='block';
      document.getElementById('sz-pix-paid-msg').style.display='none';
    } else {toast(d.data&&d.data.message?d.data.message:'Erro ao gerar PIX',false);}
  });
}
function szCopyPix(){
  var code=document.getElementById('sz-pix-code').textContent;
  if(navigator.clipboard){navigator.clipboard.writeText(code).then(function(){toast('Código PIX copiado!');});}
  else{var a=document.createElement('textarea');a.value=code;document.body.appendChild(a);a.select();document.execCommand('copy');a.remove();toast('Código copiado!');}
}
function szPixConfirm(){
  var msg=document.getElementById('sz-pix-paid-msg');
  msg.textContent='Verificando…'; msg.style.display='block'; msg.style.color='var(--tx2)';
  fetch(window.location.origin+'/wp-json/sz-portal/v1/pix/confirm',{method:'POST',credentials:'same-origin',
    headers:{'Content-Type':'application/json'},body:JSON.stringify({n:SZ.nonce})})
  .then(function(r){return r.json()})
  .then(function(d){
    msg.textContent=d.message||''; msg.style.color=d.success?'#16a34a':'var(--ac)';
    if(d.success){setTimeout(szLoadWallet,1000);}
  });
}

// ── Navigation ────────────────────────────────────────────────────────────────
function szNav(screen, btn){
  document.querySelectorAll('.sz-screen').forEach(function(s){s.classList.remove('active');});
  document.querySelectorAll('.sz-nav-btn').forEach(function(b){b.classList.remove('active');});
  document.getElementById('sz-screen-'+screen).classList.add('active');
  if(btn)btn.classList.add('active');
  var titles={orders:'Pedidos',wallet:'Carteira',profile:'Perfil'};
  document.getElementById('sz-header-title').textContent=titles[screen]||'Senderzz';
  var saldoBadge=document.getElementById('sz-saldo-badge');
  saldoBadge.style.display=screen==='wallet'?'none':'';
}

function szRefresh(){
  szLoadOrders();
  if(document.getElementById('sz-screen-wallet').classList.contains('active'))szLoadWallet();
  toast('Atualizado!');
}

// ── Service Worker + Install ─────────────────────────────────────────────────
if('serviceWorker' in navigator){
  navigator.serviceWorker.register('<?php echo esc_js( $plugin_url . 'assets/js/sz-producer-sw.js' ); ?>', {scope:'/produtor-app/'})
    .catch(function(){});
}
// iOS install hint
var isIOS=/iphone|ipad|ipod/i.test(navigator.userAgent);
var isStandalone=window.navigator.standalone===true;
if(isIOS&&!isStandalone&&!sessionStorage.getItem('sz_install_dismissed')){
  document.getElementById('sz-install-banner').style.display='flex';
  document.querySelector('.sz-close-banner').addEventListener('click',function(){
    sessionStorage.setItem('sz_install_dismissed','1');
  });
}
// Android install prompt
var deferredPrompt=null;
window.addEventListener('beforeinstallprompt',function(e){
  e.preventDefault(); deferredPrompt=e;
  var b=document.getElementById('sz-install-banner');
  b.innerHTML='<p>Instale o app Senderzz para acesso rápido!</p><button onclick="szInstall()">Instalar</button><button class="sz-close-banner" onclick="this.parentElement.style.display=\'none\'">✕</button>';
  b.style.display='flex';
});
function szInstall(){if(deferredPrompt){deferredPrompt.prompt();deferredPrompt.userChoice.then(function(){deferredPrompt=null;document.getElementById('sz-install-banner').style.display='none';});}}

// ── Init ──────────────────────────────────────────────────────────────────────
// Pega nonce via endpoint público (não expõe dados, só o nonce de AJAX)
fetch(AJAX,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/x-www-form-urlencoded'},
  body:'action=sz_producer_pwa_nonce'})
.then(function(r){return r.json()})
.then(function(d){SZ.nonce=d.nonce||'';szBoot();})
.catch(function(){szBoot();});
</script>
</body>
</html>
<?php
