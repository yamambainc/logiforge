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
/** 設定の画面のタブ（docs/webui/settings.md の「タブ」） */
export type SettingsTab = 'repository' | 'gitforge';
export const SETTINGS_TABS: SettingsTab[] = ['repository', 'gitforge'];
/**
 * 設定の repository のタブで、足す流れを開くか（docs/webui/settings.md の「repository を足す」）。
 * new：1 段目（repository）から。first：最初の問いがまだの repository があれば 2 段目から、なければ 1 段目から（セットアップのあと）。
 */
export type AddParam = 'new' | 'first';
export type Route = { name: 'repositories' } | { name: RepoScreen; repo: string } | { name: 'settings'; tab: SettingsTab; add?: AddParam } | { name: 'callback' } | { name: 'unknown' };

const SCREENS: Record<string, RepoScreen> = { '': 'dashboard', tree: 'tree', 'human-requests': 'human-requests', 'apply-requests': 'apply-requests' };

export function match(path: string, search = ''): Route {
  // セットアップが終わったあとの /setup は、repository の一覧を出す（design/webui/screens-setup.md）
  if (path === '/' || path === '' || path === '/setup') return { name: 'repositories' };
  if (path === '/oauth/callback') return { name: 'callback' };
  // タブは ?tab= に持つ。ないか、知らない値なら repository のタブ
  if (path === '/settings' || path === '/settings/') {
    const q = new URLSearchParams(search);
    const t = q.get('tab') as SettingsTab | null;
    const tab = t && SETTINGS_TABS.includes(t) ? t : 'repository';
    const add = q.get('add');
    return tab === 'repository' && (add === 'new' || add === 'first') ? { name: 'settings', tab, add } : { name: 'settings', tab };
  }
  const m = path.match(/^\/r\/([a-z0-9][a-z0-9-]{0,62})(?:\/([a-z-]+))?\/?$/);
  if (m && (m[2] ?? '') in SCREENS) return { name: SCREENS[m[2] ?? ''], repo: m[1] };
  return { name: 'unknown' };
}

export const screenPath = (repo: string, s: RepoScreen) => `/r/${repo}${s === 'dashboard' ? '' : '/' + s}`;
export const settingsPath = (tab: SettingsTab, add?: AddParam) => `/settings?tab=${tab}${add ? `&add=${add}` : ''}`;
