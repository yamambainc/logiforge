import { useEffect, useState } from 'react';
import { listRepositories, Problem, type Repository } from '../api';
import { navigate } from '../router';

const vLabel = (v: boolean | null) => (v === true ? '真' : v === false ? '偽' : '未決');
const vClass = (v: boolean | null) => (v === true ? 'v-true' : v === false ? 'v-false' : 'v-null');

// repository の一覧（docs/webui/repositories.md、design/webui/screens.md）。根の主文と根の値を一緒に示す。
export function RepositoryList() {
  const [repos, setRepos] = useState<Repository[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    listRepositories().then(setRepos).catch((e) => { if (!(e instanceof Problem && e.status === 401)) setError(String(e)); });
  }, []);
  if (error) return <div className="empty-box"><div className="empty-title">一覧を読めませんでした</div><p className="dim">{error}</p></div>;
  if (!repos) return <div className="loading">読み込み中…</div>;
  return (
    <main className="repos">
      <h1 className="page-title">repository</h1>
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
