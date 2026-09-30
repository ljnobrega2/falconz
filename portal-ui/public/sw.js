// Service worker do PWA do portal FALK.
//
// AUDIT-2026-07-31: o portal nunca teve manifest/SW de verdade — o mecanismo
// original (includes/senderzz-app-pwa.php, servindo /app-manifest.json e
// /app-sw.js via WordPress) foi apagado no strangler-fig e nunca migrado pro
// Go/estático; o botão "Testar manifest" no admin (PwaConfig.tsx) sempre ia
// dar 404. Instalável nunca funcionou.
//
// Escopo DELIBERADAMENTE mínimo: só o necessário pra passar no critério de
// instalabilidade do Chrome/Android (precisa de um handler de 'fetch') +
// fallback offline simples. NÃO cacheia o bundle JS/CSS do app — cache
// agressivo de SPA é a causa clássica de "app preso numa versão velha depois
// do deploy" (bug real documentado no CSS do favicon deste mesmo projeto).
// Deploy de nova versão = usuário sempre pega o bundle novo.
const CACHE_VERSION = 'falk-portal-v1'
const OFFLINE_URL = '/portal/'

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_VERSION).map((k) => caches.delete(k)))
    )
  )
  self.clients.claim()
})

// Network-first, sem cache de bundle — só cai pro fallback offline se a rede
// falhar de verdade (avião/sem sinal), nunca serve um JS/CSS velho por engano.
self.addEventListener('fetch', (event) => {
  if (event.request.method !== 'GET') return
  event.respondWith(
    fetch(event.request).catch(() => caches.match(OFFLINE_URL))
  )
})
