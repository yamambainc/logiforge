import { useEffect, useSyncExternalStore } from 'react';
import { getAuth, isVerified, onVerified, subscribe, signOut } from './auth';
import { match, navigate, settingsPath, useLocation } from './router';
import { SignIn } from './screens/SignIn';
import { useMobile } from './hooks';
import { BottomNav, TopTabs } from './RepoNav';
import { Dashboard } from './screens/Dashboard';
import { HumanRequests } from './screens/HumanRequests';
import { ApplyRequests } from './screens/ApplyRequests';
import { RepositoryList } from './screens/RepositoryList';
import { TreeScreen } from './screens/TreeScreen';
import { BrokenBanner } from './BrokenBanner';
import { Settings } from './screens/Settings';
import { loadAccess, useSettingsAccess } from './settings';

export function App() {
  const auth = useSyncExternalStore(subscribe, getAuth);
  const loc = useLocation();
  const mobile = useMobile();
  const access = useSettingsAccess();
  // サインインしたら、設定を開けるかを確かめる。forbidden ならメニューに出さない（design/webui/screens-setup.md の「設定」）。
  // API がトークンを 1 度受け付けてから確かめる。なぜ：organization にいない人などで、最初の要求と並べて 401 をもう 1 つ出さないため。
  useEffect(() => {
    if (auth.kind !== 'signed-in') return;
    if (isVerified()) { void loadAccess(); return; }
    return onVerified(() => { void loadAccess(); });
  }, [auth.kind]);
  if (auth.kind === 'starting') return <div className="loading">サインインしています…</div>;
  if (auth.kind === 'signed-out') return <SignIn reason={auth.reason} />;
  const route = match(loc.pathname, loc.search);
  return (
    <div className="app">
      <header className="topbar">
        <a className="brand" href="/" onClick={(e) => { e.preventDefault(); navigate('/'); }}>Logiforge</a>
        {'repo' in route && <span className="crumb-repo">{route.repo}</span>}
        {'repo' in route && <TopTabs repo={route.repo} on={route.name} />}
        <span className="spacer" />
        {access === 'yes' && <a className={`tab ${route.name === 'settings' ? 'on' : ''}`} href={settingsPath('repository')}
          onClick={(e) => { e.preventDefault(); navigate(settingsPath('repository')); }}>設定</a>}
        <button className="link" onClick={() => void signOut().then((ok) => { if (!ok) window.alert('サインアウトできませんでした。サーバにつながるか確かめて、もう一度押してください。'); })}>サインアウト</button>
      </header>
      {'repo' in route && <BrokenBanner key={route.repo} repo={route.repo} />}
      {route.name === 'tree' ? <TreeScreen key={route.repo} repo={route.repo} />
        : route.name === 'dashboard' ? <Dashboard key={route.repo} repo={route.repo} />
        : route.name === 'human-requests' ? <HumanRequests key={route.repo} repo={route.repo} />
        : route.name === 'apply-requests' ? <ApplyRequests key={route.repo} repo={route.repo} />
        : route.name === 'settings' ? <Settings tab={route.tab} add={route.add} />
        : route.name === 'repositories' || route.name === 'callback' ? <RepositoryList /> : (
        <div className="empty-box"><div className="empty-title">この画面はありません</div><button className="btn" onClick={() => navigate('/')}>repository の一覧へ</button></div>
      )}
      {mobile && 'repo' in route && <BottomNav repo={route.repo} on={route.name} />}
    </div>
  );
}
