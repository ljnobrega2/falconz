// Senderzz Push Service Worker v33
self.addEventListener('push', function(event) {
    if (!event.data) return;
    var data = {};
    try { data = event.data.json(); } catch(e) { data = { title: 'Pedidos COD', body: event.data.text() }; }

    var title   = data.title || 'Pedidos COD';
    var options = {
        body:  data.body  || '',
        icon:  data.icon  || (self.registration.scope.replace('/wp-json/', '/').split('/wp-content/')[0] + '/wp-content/plugins/senderzz/assets/images/senderzz-raio-192.png'),
        badge: data.badge || '',
        data:  data.data  || {},
        vibrate: [200, 100, 200],
        requireInteraction: false,
        tag: 'senderzz-' + (data.data && data.data.order_id ? data.data.order_id : Date.now())
    };

    event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', function(event) {
    event.notification.close();
    var url = (event.notification.data && event.notification.data.url) || '/meus-pedidos/';
    event.waitUntil(clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function(cls) {
        for (var i = 0; i < cls.length; i++) {
            if (cls[i].url.indexOf('/meus-pedidos') !== -1 && 'focus' in cls[i]) return cls[i].focus();
        }
        if (clients.openWindow) return clients.openWindow(url);
    }));
});

self.addEventListener('install', function() { self.skipWaiting(); });
self.addEventListener('activate', function(e) { e.waitUntil(clients.claim()); });
