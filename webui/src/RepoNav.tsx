// repository の中の画面の切り替え（design/webui/mobile.md の「すべての画面の決まり」）。
// デスクトップは上の帯のタブ、モバイルは下のタブ（片手の親指で届く位置）。
import { navigate, screenPath, type RepoScreen } from './router';

const TABS: { key: RepoScreen; label: string; short: string; icon: React.ReactNode }[] = [
  { key: 'tree', label: 'スナップショットツリー', short: 'ツリー', icon: <svg viewBox="0 0 20 20" width="20" height="20" aria-hidden><rect x="2" y="8" width="5" height="4" rx="1" /><rect x="13" y="3" width="5" height="4" rx="1" /><rect x="13" y="13" width="5" height="4" rx="1" /><path d="M7 10h3M10 5v10M10 5h3M10 15h3" fill="none" /></svg> },
  { key: 'dashboard', label: 'ダッシュボード', short: '状況', icon: <svg viewBox="0 0 20 20" width="20" height="20" aria-hidden><path d="M3 17V9M8 17V4M13 17v-6M17 17V7" fill="none" /></svg> },
];

function go(e: React.MouseEvent, to: string) { e.preventDefault(); navigate(to); }

export function TopTabs({ repo, on }: { repo: string; on: RepoScreen }) {
  return (
    <nav className="tabs" aria-label="画面">
      {TABS.map((t) => (
        <a key={t.key} className={`tab ${t.key === on ? 'on' : ''}`} href={screenPath(repo, t.key)} aria-current={t.key === on ? 'page' : undefined} onClick={(e) => go(e, screenPath(repo, t.key))}>{t.label}</a>
      ))}
    </nav>
  );
}

export function BottomNav({ repo, on }: { repo: string; on: RepoScreen }) {
  return (
    <nav className="bottom-nav" aria-label="画面">
      {TABS.map((t) => (
        <a key={t.key} className={t.key === on ? 'on' : ''} href={screenPath(repo, t.key)} aria-current={t.key === on ? 'page' : undefined} onClick={(e) => go(e, screenPath(repo, t.key))}>{t.icon}{t.short}</a>
      ))}
    </nav>
  );
}
