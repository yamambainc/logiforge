import { useSyncExternalStore } from 'react';
import { getAuth, subscribe, signOut } from './auth';
import { match, navigate, useLocation } from './router';
import { SignIn } from './screens/SignIn';
import { useMobile } from './hooks';
import { BottomNav, TopTabs } from './RepoNav';
import { Dashboard } from './screens/Dashboard';
import { HumanRequests } from './screens/HumanRequests';
import { ApplyRequests } from './screens/ApplyRequests';
import { RepositoryList } from './screens/RepositoryList';
import { TreeScreen } from './screens/TreeScreen';
import { BrokenBanner } from './BrokenBanner';

export function App() {
  const auth = useSyncExternalStore(subscribe, getAuth);
  const loc = useLocation();
  const mobile = useMobile();
  if (auth.kind === 'starting') return <div className="loading">サインインしています…</div>;
  if (auth.kind === 'signed-out') return <SignIn reason={auth.reason} />;
  const route = match(loc.pathname);
  return (
    <div className="app">
      <header className="topbar">
        <a className="brand" href="/" onClick={(e) => { e.preventDefault(); navigate('/'); }}>Logiforge</a>
        {'repo' in route && <span className="crumb-repo">{route.repo}</span>}
        {'repo' in route && <TopTabs repo={route.repo} on={route.name} />}
        <span className="spacer" />
        <button className="link" onClick={signOut}>サインアウト</button>
      </header>
      {'repo' in route && <BrokenBanner key={route.repo} repo={route.repo} />}
      {route.name === 'tree' ? <TreeScreen key={route.repo} repo={route.repo} />
        : route.name === 'dashboard' ? <Dashboard key={route.repo} repo={route.repo} />
        : route.name === 'human-requests' ? <HumanRequests key={route.repo} repo={route.repo} />
        : route.name === 'apply-requests' ? <ApplyRequests key={route.repo} repo={route.repo} />
        : route.name === 'repositories' || route.name === 'callback' ? <RepositoryList /> : (
        <div className="empty-box"><div className="empty-title">この画面はありません</div><button className="btn" onClick={() => navigate('/')}>repository の一覧へ</button></div>
      )}
      {mobile && 'repo' in route && <BottomNav repo={route.repo} on={route.name} />}
    </div>
  );
}
