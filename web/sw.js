const CACHE_NAME = 'nginxplorer-v2';
const STATIC_ASSETS = [
    '/',
    '/index.html',
    '/styles.css',
    '/app.js',
    '/manifest.json',
    '/icon.svg'
];

// Install: pre-cache static application shell
self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE_NAME).then((cache) => {
            return cache.addAll(STATIC_ASSETS);
        }).then(() => self.skipWaiting())
    );
});

// Activate: clean up outdated caches and claim clients
self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys().then((keys) => {
            return Promise.all(
                keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
            );
        }).then(() => self.clients.claim())
    );
});

// Fetch: serve dynamic API & SSE requests directly; cache-first or stale-while-revalidate for assets
self.addEventListener('fetch', (event) => {
    const url = new URL(event.request.url);

    // Bypass caching completely for API endpoints and SSE stream
    if (url.pathname.startsWith('/api/')) {
        return;
    }

    // Navigation requests: try network first, fallback to cached index.html
    if (event.request.mode === 'navigate') {
        event.respondWith(
            fetch(event.request).catch(() => {
                return caches.match('/index.html');
            })
        );
        return;
    }

    // Static assets & CDN libraries: Stale-While-Revalidate
    event.respondWith(
        caches.match(event.request).then((cachedResponse) => {
            const fetchPromise = fetch(event.request).then((networkResponse) => {
                if (networkResponse && networkResponse.status === 200) {
                    const responseToCache = networkResponse.clone();
                    caches.open(CACHE_NAME).then((cache) => {
                        cache.put(event.request, responseToCache);
                    });
                }
                return networkResponse;
            }).catch(() => {
                // Return cached response if network fails
                return cachedResponse;
            });

            return cachedResponse || fetchPromise;
        })
    );
});
