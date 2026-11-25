// Service Worker（design/webui/pwa.md の「Service Worker」）。ビルドで dist/sw.js になる。
// 版と、控えに入れる外枠の一覧は、ビルドの最後に入る（vite.config.ts の pwaShell）。
import { CACHE_PREFIX, route } from './sw-route';

// tsconfig の lib は DOM なので、Service Worker の型は、使う分だけ書く
interface ExtendableEvent extends Event { waitUntil(p: Promise<unknown>): void }
interface FetchEvent extends ExtendableEvent { request: Request; respondWith(r: Promise<Response>): void }
interface SWGlobal {
  location: Location;
  skipWaiting(): Promise<void>;
  clients: { claim(): Promise<void> };
  addEventListener(t: 'install' | 'activate', f: (e: ExtendableEvent) => void): void;
  addEventListener(t: 'fetch', f: (e: FetchEvent) => void): void;
}
const sw = self as unknown as SWGlobal;

const VERSION: string = '__LF_VERSION__';
const SHELL: string[] = ['__LF_SHELL__'];
const CACHE = CACHE_PREFIX + VERSION;

sw.addEventListener('install', (e) => {
  // HTTP のキャッシュを通さずに読む（古い index.html を控えに入れないため）
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL.map((u) => new Request(u, { cache: 'reload' })))).then(() => sw.skipWaiting()));
});

sw.addEventListener('activate', (e) => {
  // 古い版の控えを消す
  e.waitUntil(caches.keys()
    .then((ks) => Promise.all(ks.filter((k) => k.startsWith(CACHE_PREFIX) && k !== CACHE).map((k) => caches.delete(k))))
    .then(() => sw.clients.claim()));
});

sw.addEventListener('fetch', (e) => {
  const r = e.request;
  switch (route(r, sw.location.origin)) {
    case 'page':
      e.respondWith(fetch(r).catch(async () => (await fromCache('/index.html')) ?? Response.error()));
      return;
    case 'asset':
      e.respondWith(fromCache(r).then((hit) => hit ?? fetch(r).then((res) => {
        if (res.ok) { const copy = res.clone(); void caches.open(CACHE).then((c) => c.put(r, copy)); }
        return res;
      })));
      return;
    case 'static':
      e.respondWith(fetch(r).catch(async () => (await fromCache(r)) ?? Response.error()));
      return;
  }
});

async function fromCache(r: Request | string): Promise<Response | undefined> {
  return (await caches.open(CACHE)).match(r);
}
