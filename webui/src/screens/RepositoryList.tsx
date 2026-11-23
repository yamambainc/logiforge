import { useEffect, useState } from 'react';
import { listRepositories, Problem, type Repository } from '../api';
import { navigate, settingsPath } from '../router';
import { useSettingsAccess } from '../settings';

const vLabel = (v: boolean | null) => (v === true ? '真' : v === false ? '偽' : '未決');
const vClass = (v: boolean | null) => (v === true ? 'v-true' : v === false ? 'v-false' : 'v-null');

// repository の一覧（docs/webui/repositories.md、design/webui/screens.md）。根の主文と根の値を一緒に示す。
// 新しい repository は、設定の画面で足す（docs/webui/settings.md の「repository を足す」）。設定を開ける利用者にだけ、入口を示す。
export function RepositoryList() {
  const [repos, setRepos] = useState<Repository[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const access = useSettingsAccess();
  useEffect(() => {
    listRepositories().then(setRepos).catch((e) => { if (!(e instanceof Problem && e.status === 401)) setError(String(e)); });
  }, []);
  if (error) return <div className="empty-box"><div className="empty-title">一覧を読めませんでした</div><p className="dim">{error}</p></div>;
  if (!repos) return <div className="loading">読み込み中…</div>;
  const add = settingsPath('repository', 'new');
  return (
    <main className="repos">
      <div className="page-head">
        <h1 className="page-title">repository</h1>
        <span className="spacer" />
        {access === 'yes' && <a className="btn primary" href={add} onClick={(e) => { e.preventDefault(); navigate(add); }}>新しい repository</a>}
      </div>
      {repos.length === 0 && <p className="dim">repository がまだありません。</p>}
      <ul className="repo-list">
        {repos.map((r) => (
          <li key={r.name}>
            <a href={`/r/${r.name}/tree`} onClick={(e) => { e.preventDefault(); navigate(`/r/${r.name}/tree`); }}>
              <span className="repo-name mono">{r.name}</span>
              <span className="repo-root">{r.root_statement ?? 'スナップショットツリーを取り込み中'}</span>
              <span className={`val ${vClass(r.root_value)}`}>{vLabel(r.root_value)}</span>
            </a>
          </li>
        ))}
      </ul>
    </main>
  );
}
