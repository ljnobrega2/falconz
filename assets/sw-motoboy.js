// Senderzz — Service Worker do PWA Motoboy
//
// ONDE ESTE ARQUIVO É SERVIDO:
//   Caminho físico:  assets/sw-motoboy.js  (TPC_PATH . 'assets/sw-motoboy.js')
//   URL pública:     /sw-motoboy.js        (escopo "/")
//
//   O WordPress NÃO serve este arquivo direto. Ele é entregue via PHP por
//   includes/motoboy/routes.php -> sz_motoboy_output_service_worker(), que faz
//   readfile() deste arquivo quando ele existe. A entrega ocorre por dois caminhos:
//     1) Hook 'init' (prioridade 0): se REQUEST_URI === '/sw-motoboy.js', faz echo + exit.
//     2) Rewrite '^sw-motoboy\.js$' -> index.php?sz_motoboy_sw=1 (fallback de permalink).
//   Em ambos os casos os headers corretos são enviados:
//     Content-Type: application/javascript / Service-Worker-Allowed: / (escopo raiz).
//
//   O app registra com: navigator.serviceWorker.register('/sw-motoboy.js', { scope: '/' })
//   (ver templates/motoboy/pwa.php, função szMbRegisterPush). Portanto o SW controla
//   TODA a origem — por isso o fetch handler é deliberadamente conservador e só
//   intercepta o que pertence ao app motoboy. Tudo de wp-admin/REST/wp-content
//   passa direto (ver CLAUDE.md: o SW nunca pode quebrar o admin/OL).

'use strict';

// Versão do cache. Bump aqui para invalidar o shell antigo em todos os dispositivos.
const CACHE = 'sz-mb-v1';

// URL base da casca (shell) do app servida pelo WordPress.
const APP_SHELL_PATH = '/motoboy-app/';

// Assets estáticos do app (cache-first). A casca em si é HTML gerado pelo PHP com
// CSS/JS inline, então os únicos arquivos externos relevantes são os ícones.
const STATIC_ASSETS = [
  '/wp-content/plugins/senderzz-logistics/assets/icon-192.png',
  '/wp-content/plugins/senderzz-logistics/assets/icon-512.png',
];

// Prefixo das chamadas REST do módulo motoboy (network-first).
const API_PREFIX = '/wp-json/sz-motoboy/v1/';

// HTML mínimo exibido quando a casca é solicitada sem rede e sem cache.
const OFFLINE_HTML =
  '<!doctype html><html lang="pt-BR"><head><meta charset="utf-8">' +
  '<meta name="viewport" content="width=device-width,initial-scale=1">' +
  '<title>Senderzz — Offline</title>' +
  '<style>body{margin:0;font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;' +
  'background:#0f172a;color:#e2e8f0;display:flex;min-height:100vh;align-items:center;' +
  'justify-content:center;text-align:center;padding:24px}' +
  '.box{max-width:340px}h1{font-size:20px;margin:0 0 8px}p{color:#94a3b8;line-height:1.5}' +
  'button{margin-top:18px;padding:10px 22px;border:0;border-radius:10px;background:#E8650A;' +
  'color:#fff;font-size:15px;cursor:pointer}</style></head>' +
  '<body><div class="box"><div style="font-size:48px">📶</div>' +
  '<h1>Sem conexão</h1>' +
  '<p>Você está offline. Reconecte para carregar suas entregas.</p>' +
  '<button onclick="location.reload()">Tentar novamente</button></div></body></html>';

// ── Install: pré-carrega a casca estática (shell) ────────────────────────────────
self.addEventListener('install', function (event) {
  event.waitUntil(
    caches.open(CACHE).then(function (cache) {
      // addAll falha tudo-ou-nada; usamos add individual com catch para não
      // travar a instalação caso um ícone esteja ausente em algum ambiente.
      return Promise.all(
        STATIC_ASSETS.map(function (url) {
          return cache.add(url).catch(function () { /* ignora asset ausente */ });
        })
      );
    })
  );
  // Ativa a nova versão imediatamente, sem esperar fechar as abas antigas.
  self.skipWaiting();
});

// ── Activate: remove caches de versões anteriores ────────────────────────────────
self.addEventListener('activate', function (event) {
  event.waitUntil(
    caches.keys().then(function (keys) {
      return Promise.all(
        keys.map(function (key) {
          // Mantém apenas o cache da versão atual; apaga sz-mb-v* antigos.
          if (key !== CACHE && key.indexOf('sz-mb-') === 0) {
            return caches.delete(key);
          }
          return null;
        })
      );
    }).then(function () {
      // Assume o controle das abas já abertas imediatamente.
      return self.clients.claim();
    })
  );
});

// ── Estratégias de cache ─────────────────────────────────────────────────────────

// Network-first: tenta a rede; em falha (offline), cai para o cache.
// Usado nas chamadas REST do motoboy para manter os dados sempre frescos.
function networkFirst(request) {
  return fetch(request)
    .then(function (response) {
      // Guarda uma cópia das respostas OK para consulta offline posterior.
      if (response && response.ok) {
        const copy = response.clone();
        caches.open(CACHE).then(function (cache) { cache.put(request, copy); });
      }
      return response;
    })
    .catch(function () {
      return caches.match(request).then(function (cached) {
        return cached || new Response(
          JSON.stringify({ ok: false, erro: 'offline' }),
          { status: 503, headers: { 'Content-Type': 'application/json' } }
        );
      });
    });
}

// Cache-first: responde do cache se houver; senão busca na rede e armazena.
// Usado nos assets estáticos da casca (ícones).
function cacheFirst(request) {
  return caches.match(request).then(function (cached) {
    if (cached) return cached;
    return fetch(request).then(function (response) {
      if (response && response.ok) {
        const copy = response.clone();
        caches.open(CACHE).then(function (cache) { cache.put(request, copy); });
      }
      return response;
    });
  });
}

// ── Fetch: roteamento conservador ────────────────────────────────────────────────
// O SW controla toda a origem (escopo "/"); por isso só interceptamos o que é do
// app motoboy. Qualquer outra coisa (wp-admin, AJAX, REST de outros módulos,
// wp-includes, demais assets) passa direto pela rede — assim o SW nunca quebra
// o painel admin/OL.
self.addEventListener('fetch', function (event) {
  const request = event.request;
  const url = new URL(request.url);

  // Só lidamos com requisições da própria origem.
  if (url.origin !== self.location.origin) return;

  // 1) API do motoboy (GET) — network-first com fallback de cache.
  if (url.pathname.indexOf(API_PREFIX) === 0) {
    if (request.method === 'GET') {
      event.respondWith(networkFirst(request));
    }
    // POST/PUT/etc. nunca são cacheados — deixa passar direto.
    return;
  }

  // A partir daqui só tratamos GET; demais métodos passam direto.
  if (request.method !== 'GET') return;

  // 2) Navegação para a casca do app — network-first com fallback offline.
  const isShellNav =
    request.mode === 'navigate' && url.pathname.indexOf(APP_SHELL_PATH) === 0;
  if (isShellNav) {
    event.respondWith(
      fetch(request)
        .then(function (response) {
          if (response && response.ok) {
            const copy = response.clone();
            caches.open(CACHE).then(function (cache) { cache.put(request, copy); });
          }
          return response;
        })
        .catch(function () {
          return caches.match(request).then(function (cached) {
            return cached || new Response(OFFLINE_HTML, {
              status: 200,
              headers: { 'Content-Type': 'text/html; charset=utf-8' },
            });
          });
        })
    );
    return;
  }

  // 3) Assets estáticos da casca (ícones do plugin) — cache-first.
  if (STATIC_ASSETS.indexOf(url.pathname) !== -1) {
    event.respondWith(cacheFirst(request));
    return;
  }

  // 4) Resto da origem (wp-admin, wp-json de outros módulos, wp-content,
  //    wp-includes, admin-ajax, etc.) — NÃO intercepta. Passa direto pela rede.
  return;
});

// ── Push: exibe a notificação com título/corpo do payload ────────────────────────
self.addEventListener('push', function (event) {
  let data = { title: 'Senderzz', body: 'Nova notificação.' };
  // O payload chega como JSON; se não der para parsear, usa o texto cru ou o default.
  try {
    if (event.data) data = event.data.json();
  } catch (err) {
    try { data.body = event.data.text() || data.body; } catch (e2) {}
  }
  event.waitUntil(
    self.registration.showNotification(data.title || 'Senderzz', {
      body:  data.body || '',
      icon:  '/wp-content/plugins/senderzz-logistics/assets/icon-192.png',
      badge: '/wp-content/plugins/senderzz-logistics/assets/icon-192.png',
      vibrate: [200, 100, 200],
      tag:   'sz-motoboy-push',
      renotify: true,
      data:  data, // mantém o payload para uso no clique (ex.: url destino).
    })
  );
});

// ── Notificationclick: foca uma aba do app já aberta ou abre uma nova ────────────
self.addEventListener('notificationclick', function (event) {
  event.notification.close();
  // Permite que o payload indique uma URL específica; senão volta para a casca.
  const targetUrl = (event.notification.data && event.notification.data.url)
    ? event.notification.data.url
    : APP_SHELL_PATH;
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true })
      .then(function (clientList) {
        // Se já houver uma aba do motoboy aberta, foca nela.
        for (let i = 0; i < clientList.length; i++) {
          const client = clientList[i];
          if (client.url.indexOf(APP_SHELL_PATH) !== -1 && 'focus' in client) {
            return client.focus();
          }
        }
        // Caso contrário, abre uma nova aba na casca do app.
        if (self.clients.openWindow) {
          return self.clients.openWindow(targetUrl);
        }
        return null;
      })
  );
});
