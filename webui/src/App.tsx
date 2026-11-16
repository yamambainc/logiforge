import { useSyncExternalStore } from 'react';
import { getAuth, subscribe, signOut } from './auth';
import { match, navigate, useLocation } from './router';
import { SignIn } from './screens/SignIn';
import { RepositoryList } from './screens/RepositoryList';
import { TreeScreen } from './screens/TreeScreen';

export function App() {
  const auth = useSyncExternalStore(subscribe, getAuth);
  const loc = useLocation();
  if (auth.kind === 'starting') return <div className="loading">サインインしています…</div>;
  if (auth.kind === 'signed-out') return <SignIn reason={auth.reason} />;
  const route = match(loc.pathname);
  return (
    <div className="app">
      <header className="topbar">
        <a className="brand" href="/" onClick={(e) => { e.preventDefault(); navigate('/'); }}>Logiforge</a>
        {route.name === 'tree' && <span className="crumb-repo">{route.repo}</span>}
        <span className="spacer" />
        <button className="link" onClick={signOut}>サインアウト</button>
      </header>
      {route.name === 'tree' ? <TreeScreen repo={route.repo} /> : route.name === 'repositories' || route.name === 'callback' ? <RepositoryList /> : (
        <div className="empty-box"><div className="empty-title">この画面はありません</div><button className="btn" onClick={() => navigate('/')}>repository の一覧へ</button></div>
      )}
    </div>
  );
}
