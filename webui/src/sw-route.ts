// Service Worker が、要求をどう扱うかを決める（design/webui/pwa.md の「Service Worker」）。
// Service Worker が持つのは、アプリの外枠（index.html、JS、CSS、アイコン）だけである。API の応答とサインインの行き来は持たない。

export type Route =
  | 'pass' // 触れない（ブラウザがそのままネットに送る）
  | 'page' // 画面の遷移。ネットを先に使い、つながらないときだけ、控えの index.html を返す
  | 'asset' // ハッシュ付きの JS と CSS。中身が変わらないので、控えを先に使う
  | 'static'; // アイコンと manifest。ネットを先に使い、つながらないときだけ控え

export interface Req {
  url: string;
  method: string;
  mode: string; // Request.mode
  destination: string; // Request.destination
}

/** origin は、Service Worker のオリジン（WebUI と API のオリジン）。 */
export function route(r: Req, origin: string): Route {
  if (r.method !== 'GET') return 'pass';
  const u = new URL(r.url);
  if (u.origin !== origin) return 'pass';
  const p = u.pathname;
  // API（サインインの中継 /v1/oauth/ を含む）と、Service Worker 自身には触れない
  if (p === '/v1' || p.startsWith('/v1/') || p === '/healthz' || p === '/sw.js') return 'pass';
  if (r.mode === 'navigate') {
    // Mermaid の iframe（mermaid.html）などの、ページの中の文書は、控えの index.html に置き換えない
    return r.destination === 'document' ? 'page' : 'pass';
  }
  if (p.startsWith('/assets/')) return 'asset';
  if (SHELL_STATIC.includes(p)) return 'static';
  return 'pass';
}

/** 外枠のうち、ハッシュの付かないファイル（public/ のもの）。ビルドで控えに入れる。 */
export const SHELL_STATIC = ['/manifest.webmanifest', '/icon.svg', '/icon-192.png', '/icon-512.png', '/icon-maskable-512.png', '/apple-touch-icon.png'];

/** Service Worker の控えの名前の頭。版ごとに `${CACHE_PREFIX}${版}` にする。 */
export const CACHE_PREFIX = 'logiforge-shell-';
