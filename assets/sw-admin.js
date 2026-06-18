self.addEventListener('push', function(e) {
    let data = { title: 'Senderzz Ops', body: 'Nova notificação.' };
    try { data = e.data.json(); } catch(err) {}
    e.waitUntil(
        self.registration.showNotification(data.title, {
            body:  data.body,
            icon:  '/wp-content/plugins/senderzz-logistics/assets/icon-192.png',
            badge: '/wp-content/plugins/senderzz-logistics/assets/icon-192.png',
            vibrate: [100, 50, 100],
            tag:   'sz-admin-push',
            renotify: true,
            data:  data,
        })
    );
});

self.addEventListener('notificationclick', function(e) {
    e.notification.close();
    const url = '/wp-admin/admin.php?page=sz-motoboy&tab=pedidos';
    e.waitUntil(clients.openWindow(url));
});
