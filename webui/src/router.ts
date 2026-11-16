// 画面の状態は URL に持つ（design/webui/screens.md の「URL」）。history API だけで切り替える。
import { useSyncExternalStore } from 'react';

const listeners = new Set<() => void>();
const notify = () => listeners.forEach((f) => f());
// 単体テスト（node）では window がない
if (typeof window !== 'undefined') window.addEventListener('popstate', notify);

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

/** repository の中の画面（design/webui/screens.md の「URL」） */
export type RepoScreen = 'dashboard' | 'tree' | 'human-requests' | 'apply-requests';
export type Route = { name: 'repositories' } | { name: RepoScreen; repo: string } | { name: 'callback' } | { name: 'unknown' };

const SCREENS: Record<string, RepoScreen> = { '': 'dashboard', tree: 'tree', 'human-requests': 'human-requests', 'apply-requests': 'apply-requests' };

export function match(path: string): Route {
  if (path === '/' || path === '') return { name: 'repositories' };
  if (path === '/oauth/callback') return { name: 'callback' };
  const m = path.match(/^\/r\/([a-z0-9][a-z0-9-]{0,62})(?:\/([a-z-]+))?\/?$/);
  if (m && (m[2] ?? '') in SCREENS) return { name: SCREENS[m[2] ?? ''], repo: m[1] };
  return { name: 'unknown' };
}

export const screenPath = (repo: string, s: RepoScreen) => `/r/${repo}${s === 'dashboard' ? '' : '/' + s}`;
