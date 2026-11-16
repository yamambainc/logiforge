import { useEffect, useState } from 'react';
import { createRepository, listRepositories, Problem, type Repository } from '../api';
import { navigate } from '../router';

const vLabel = (v: boolean | null) => (v === true ? '真' : v === false ? '偽' : '未決');
const vClass = (v: boolean | null) => (v === true ? 'v-true' : v === false ? 'v-false' : 'v-null');

// repository の一覧と作成（docs/webui/repositories.md、design/webui/screens.md）。根の主文と根の値を一緒に示す。
export function RepositoryList() {
  const [repos, setRepos] = useState<Repository[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  useEffect(() => {
    listRepositories().then(setRepos).catch((e) => { if (!(e instanceof Problem && e.status === 401)) setError(String(e)); });
  }, []);
  if (error) return <div className="empty-box"><div className="empty-title">一覧を読めませんでした</div><p className="dim">{error}</p></div>;
  if (!repos) return <div className="loading">読み込み中…</div>;
  return (
    <main className="repos">
      <div className="page-head">
        <h1 className="page-title">repository</h1>
        <span className="spacer" />
        {!creating && <button className="btn primary" onClick={() => setCreating(true)}>新しい repository</button>}
      </div>
      {creating && <CreateForm onCancel={() => setCreating(false)} />}
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

// 名前と主文の決まり（openapi の RepositoryNameValue、docs/tree/README.md の「仮説」）。
// BE と同じ決まりである：主文は 50 字以下（internal/tree/check.go の MaxStatementChars）、
// 文字数は NFC に正規化したあとのコードポイント数（同じく CharCount。docs/tree/README.md）。BE が最後に確かめるので、ここは送る前の案内である
export const NAME_RE = /^[a-z0-9][a-z0-9-]{0,62}$/;
export const MAX_STATEMENT = 50;
/** 文字数は、NFC に正規化したあとのコードポイント数で数える */
export const charCount = (s: string) => [...s.normalize('NFC')].length;

export function validateNew(name: string, statement: string): string | null {
  if (!NAME_RE.test(name)) return '名前は、英小文字・数字・ハイフンで、63 字まで（先頭はハイフン以外）にしてください。';
  if (!statement.trim()) return '根の仮説の主文を書いてください。';
  if (charCount(statement) > MAX_STATEMENT) return `主文は ${MAX_STATEMENT} 字以下にしてください。`;
  return null;
}

const CREATE_ERRORS: Record<string, string> = {
  repository_exists: 'その名前の repository は、もうあります。',
  forbidden: 'repository を作れるのは、利用者だけです。',
  repository_not_registered: 'GitForge に、その repository を用意できませんでした。',
};

function CreateForm({ onCancel }: { onCancel: () => void }) {
  const [name, setName] = useState('');
  const [statement, setStatement] = useState('');
  const [background, setBackground] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const n = charCount(statement);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const v = validateNew(name, statement);
    if (v) { setError(v); return; }
    setBusy(true); setError(null);
    try {
      // 作ったら、ビュワーを開く。スナップショットツリーがまだ出ていなければ、ビュワーが「取り込み中」と示して読み直す
      const r = await createRepository(name, statement.trim(), background.trim());
      navigate(`/r/${r.repo.name}/tree`);
    } catch (err) {
      setBusy(false);
      if (err instanceof Problem && err.status === 401) return;
      setError(err instanceof Problem ? CREATE_ERRORS[err.code] ?? (err.detail || err.code) : String(err));
    }
  };
  return (
    <form className="card create" onSubmit={submit}>
      <h2>新しい repository</h2>
      <label>名前<input value={name} onChange={(e) => setName(e.target.value)} placeholder="question-a" autoFocus pattern="[a-z0-9][a-z0-9\-]{0,62}" required /></label>
      <label>根の仮説の主文 <span className={`dim ${n > MAX_STATEMENT ? 'over' : ''}`}>{n} / {MAX_STATEMENT}</span>
        <input value={statement} onChange={(e) => setStatement(e.target.value)} placeholder="ある究極の問いの主文" required />
      </label>
      <label>背景情報（なくてもよい）<textarea value={background} onChange={(e) => setBackground(e.target.value)} rows={3} /></label>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="form-actions">
        <button className="btn primary" type="submit" disabled={busy}>{busy ? '作っています…' : '作る'}</button>
        <button className="btn" type="button" onClick={onCancel} disabled={busy}>やめる</button>
      </div>
    </form>
  );
}
