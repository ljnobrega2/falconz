// Senderzz Motoboy Service Worker
// fetch passthrough: garante que requisições REST não sejam dropadas quando o SW está ativo
self.addEventListener('fetch', function(e) {
    const url = new URL(e.request.url);

    // Nunca intercepta wp-admin, REST, AJAX, assets do WordPress ou requisições não-GET.
    // Isso evita que o Service Worker do app motoboy quebre o admin/OL quando uma
    // requisição de rede falha ou quando o navegador tenta pré-carregar a raiz do site.
    if (e.request.method !== 'GET' ||
        url.pathname.indexOf('/wp-admin/') === 0 ||
        url.pathname.indexOf('/wp-json/') === 0 ||
        url.pathname.indexOf('/wp-content/') === 0 ||
        url.pathname.indexOf('/wp-includes/') === 0 ||
        url.pathname === '/wp-admin/admin-ajax.php') {
        return;
    }

    e.respondWith(
        fetch(e.request).catch(function() {
            return new Response('', { status: 204, statusText: 'Offline' });
        })
    );
});

self.addEventListener('push', function(e) {
    let data = { title: 'Senderzz', body: 'Nova notificação.' };
    try { data = e.data.json(); } catch(err) {}
    e.waitUntil(
        self.registration.showNotification(data.title, {
            body:  data.body,
            icon:  '/wp-content/plugins/senderzz-logistics/assets/icon-192.png',
            badge: '/wp-content/plugins/senderzz-logistics/assets/icon-192.png',
            vibrate: [200, 100, 200],
            tag:   'sz-motoboy-push',
            renotify: true,
        })
    );
});

self.addEventListener('notificationclick', function(e) {
    e.notification.close();
    e.waitUntil(clients.openWindow('/motoboy-app/'));
});
