// 画面の状態は URL に持つ（design/webui/screens.md の「URL」）。history API だけで切り替える。
import { useSyncExternalStore } from 'react';

const listeners = new Set<() => void>();
const notify = () => listeners.forEach((f) => f());
window.addEventListener('popstate', notify);

export function navigate(to: string, replace = false) {
  if (replace) history.replaceState(null, '', to);
  else history.pushState(null, '', to);
  notify();
}

const subscribe = (f: () => void) => { listeners.add(f); return () => { listeners.delete(f); }; };
const snapshot = () => location.pathname + location.search;

export function useLocation(): URL {
  const s = useSyncExternalStore(subscribe, snapshot);
  return new URL(s, location.origin);
}

export type Route = { name: 'repositories' } | { name: 'tree'; repo: string } | { name: 'callback' } | { name: 'unknown' };

export function match(path: string): Route {
  if (path === '/' || path === '') return { name: 'repositories' };
  if (path === '/oauth/callback') return { name: 'callback' };
  const m = path.match(/^\/r\/([a-z0-9][a-z0-9-]{0,62})\/tree\/?$/);
  if (m) return { name: 'tree', repo: m[1] };
  return { name: 'unknown' };
}
