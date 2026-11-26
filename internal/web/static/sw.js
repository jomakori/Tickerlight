// Tickerlight console service worker.
//
// The console is server-rendered and needs the engine for everything that
// matters, so this worker deliberately caches nothing. It exists so the app is
// installable — a browser only offers "Add to Home Screen" once a service
// worker with a fetch handler controls the page — and it must never serve a
// stale arm state, caps figure or journal page from a cache.
self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("fetch", (event) => {
  if (event.request.method !== "GET") return;
  event.respondWith(fetch(event.request));
});
