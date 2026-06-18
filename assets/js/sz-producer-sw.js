// Senderzz Producer PWA — Service Worker v1
const CACHE = 'sz-producer-v1';
const OFFLINE_URL = '/produtor-app/';

// Arquivos que ficam em cache para funcionar offline
const PRECACHE = [
    '/produtor-app/',
    '/wp-content/plugins/senderzz_v46/assets/img/senderzz-logo.png',
];

self.addEventListener('install', e => {
    self.skipWaiting();
    e.waitUntil(
        caches.open(CACHE).then(c => c.addAll(PRECACHE.map(u => new Request(u, { credentials: 'same-origin' }))))
        .catch(() => {}) // não falha se offline na instalação
    );
});

self.addEventListener('activate', e => {
    e.waitUntil(
        caches.keys().then(keys =>
            Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k)))
        ).then(() => clients.claim())
    );
});

self.addEventListener('fetch', e => {
    const url = new URL(e.request.url);
    // API calls: sempre network-first, sem cache
    if (url.pathname.startsWith('/wp-json/')) return;
    // Navegação: tenta network, cai em cache
    if (e.request.mode === 'navigate') {
        e.respondWith(
            fetch(e.request).catch(() => caches.match(OFFLINE_URL))
        );
        return;
    }
    // Assets: cache-first
    e.respondWith(
        caches.match(e.request).then(cached => cached || fetch(e.request))
    );
});

// Push notifications
self.addEventListener('push', e => {
    if (!e.data) return;
    let data = {};
    try { data = e.data.json(); } catch { data = { title: 'Senderzz', body: e.data.text() }; }
    e.waitUntil(self.registration.showNotification(data.title || 'Senderzz', {
        body:    data.body  || '',
        icon:    data.icon  || '/wp-content/plugins/senderzz_v46/assets/img/senderzz-logo.png',
        badge:   '/wp-content/plugins/senderzz_v46/assets/img/senderzz-logo.png',
        data:    data.data  || {},
        vibrate: [200, 100, 200],
        tag:     'sz-producer-' + (data.data?.order_id || Date.now()),
        requireInteraction: data.data?.urgent || false,
    }));
});

self.addEventListener('notificationclick', e => {
    e.notification.close();
    const url = e.notification.data?.url || '/produtor-app/';
    e.waitUntil(
        clients.matchAll({ type: 'window', includeUncontrolled: true }).then(cls => {
            for (const c of cls) {
                if (c.url.includes('/produtor-app') && 'focus' in c) return c.focus();
            }
            if (clients.openWindow) return clients.openWindow(url);
        })
    );
});
