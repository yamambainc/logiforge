import { useEffect, useState, type ReactNode } from 'react';
import { Problem } from '../api';
import { navigate, settingsPath, type SettingsTab } from '../router';
import {
  buildPatch, createKeyConfirm, createRecordKey, formsFrom, getSettings, installUrlFrom, INTERVAL_KEYS, listRecordKeys,
  listRegisteredRepositories, noteAccess, registerRepository, unregisterConfirm, unregisterRepository, updateGitForgeSettings,
  type ForgejoForm, type GitHubForm, type IntervalForm, type RecordKey, type RegisteredRepository, type SecretState, type Settings as SettingsValue,
} from '../settings';
import { NAME_RE } from './RepositoryList';

const fmt = (iso?: string | null) => { if (!iso) return ''; const d = new Date(iso); return isNaN(+d) ? iso : d.toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' }); };
const TAB_LABELS: Record<SettingsTab, string> = { repository: 'repository', gitforge: 'GitForge', 'record-keys': '記録の鍵' };
/** エラーを文にする。401 はサインインに任せるので、何も示さない。 */
const message = (e: unknown): string | null =>
  e instanceof Problem ? (e.status === 401 ? null : e.code === 'forbidden' ? '設定を変えられるのは、利用者だけです。' : e.detail || e.code) : String(e);

// 設定の画面（docs/webui/settings.md、design/webui/screens-setup.md の「設定」）。利用者だけが開ける。
export function Settings({ tab }: { tab: SettingsTab }) {
  const [s, setS] = useState<SettingsValue | null>(null);
  const [error, setError] = useState<'forbidden' | string | null>(null);
  const reload = () => getSettings().then((v) => { setS(v); noteAccess(true); }).catch((e) => {
    if (e instanceof Problem && e.code === 'forbidden') { noteAccess(false); setError('forbidden'); return; }
    const m = message(e); if (m) setError(m);
  });
  useEffect(() => { void reload(); }, []);

  if (error === 'forbidden') return (
    <div className="empty-box">
      <div className="empty-title">利用者ではありません</div>
      <p>設定を開けるのは、利用者だけです。利用者になるには、ほかの利用者に頼んでください。</p>
      <button className="btn" onClick={() => navigate('/')}>repository の一覧へ</button>
    </div>
  );
  if (error) return <div className="empty-box"><div className="empty-title">設定を読めませんでした</div><p className="dim">{error}</p></div>;
  if (!s) return <div className="loading">読み込み中…</div>;
  return (
    <main className="repos settings">
      <div className="page-head">
        <h1 className="page-title">設定</h1>
        <div className="seg" role="tablist" aria-label="設定のタブ">
          {(Object.keys(TAB_LABELS) as SettingsTab[]).map((t) => (
            <button key={t} role="tab" aria-selected={t === tab} className={t === tab ? 'on' : ''} onClick={() => navigate(settingsPath(t), true)}>{TAB_LABELS[t]}</button>
          ))}
        </div>
      </div>
      {tab === 'repository' ? <RepositoryTab settings={s} onChanged={() => void reload()} />
        : tab === 'gitforge' ? <GitForgeTab settings={s} onSaved={setS} />
        : <RecordKeysTab onChanged={() => void reload()} />}
      <p className="settings-updated dim">最後に変えた人：{s.updated_by || '—'}　{fmt(s.updated_at)}</p>
    </main>
  );
}

const WorkerNotice = ({ what }: { what: string }) => <p className="dim small">{what}は、worker が次に一覧を読み直したときに効きます。</p>;

// --- repository ---

function RepositoryTab({ settings, onChanged }: { settings: SettingsValue; onChanged: () => void }) {
  const [items, setItems] = useState<RegisteredRepository[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const load = () => listRegisteredRepositories().then(setItems).catch((e) => setError(message(e)));
  useEffect(() => { void load(); }, []);
  const remove = async (name: string) => {
    if (!window.confirm(unregisterConfirm(name))) return;
    setBusy(name); setError(null);
    try { await unregisterRepository(name); await load(); onChanged(); } catch (e) { setError(message(e)); }
    setBusy(null);
  };
  const github = settings.gitforge === 'github';
  return (
    <section className="card">
      <div className="page-head">
        <h2>repository</h2>
        <span className="spacer" />
        {!adding && <button className="btn" onClick={() => setAdding(true)}>足す</button>}
      </div>
      <p className="dim small">{github
        ? 'GitHub リポジトリと、ディレクトリの repository を登録します。'
        : 'ディレクトリの repository を登録します。ふつうの repository は、repository の一覧で作ってください。'}</p>
      <WorkerNotice what="repository の一覧の変更" />
      {adding && <AddRepository github={github} onCancel={() => setAdding(false)} onAdded={() => { setAdding(false); void load(); onChanged(); }} />}
      {error && <p className="form-error" role="alert">{error}</p>}
      {!items ? <p className="dim">読み込み中…</p> : items.length === 0 ? <p className="dim">登録した repository はありません。</p> : (
        <ul className="settings-list">
          {items.map((r) => (
            <li key={r.name}>
              <span className="mono strong">{r.name}</span>
              <span className="mono">{r.gitforge_ref}{r.directory ? ` : ${r.directory}` : ''}</span>
              <span className="spacer" />
              <button className="btn" disabled={busy === r.name} onClick={() => void remove(r.name)}>外す</button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function AddRepository({ github, onCancel, onAdded }: { github: boolean; onCancel: () => void; onAdded: () => void }) {
  const [name, setName] = useState('');
  const [ref, setRef] = useState('');
  const [dir, setDir] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ReactNode>(null);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const n = name.trim(), r = ref.trim(), d = dir.trim().replace(/^\/+|\/+$/g, '');
    if (!NAME_RE.test(n)) { setError('名前は、英小文字・数字・ハイフンで、63 字まで（先頭はハイフン以外）にしてください。'); return; }
    if (!/^[^/\s]+\/[^/\s]+$/.test(r)) { setError('git リポジトリは owner/name の形で入れてください。'); return; }
    if (!github && !d) { setError('ディレクトリのパスを入れてください。'); return; }
    setBusy(true); setError(null);
    try {
      await registerRepository(n, r, d);
      onAdded();
    } catch (err) {
      setBusy(false);
      const url = err instanceof Problem && err.code === 'validation_failed' ? installUrlFrom(err.detail) : null;
      setError(url ? <>
        GitHub リポジトリを読めませんでした。App が、その owner に install されていないかもしれません。
        {' '}<a href={url} target="_blank" rel="noreferrer">GitHub で install する</a>
        {err instanceof Problem && err.detail && <><br /><span className="dim">{err.detail}</span></>}
      </> : message(err));
    }
  };
  return (
    <form className="setup-form settings-form" onSubmit={submit}>
      <label>名前<input value={name} onChange={(e) => setName(e.target.value)} placeholder="question-a" autoFocus required /></label>
      <label>{github ? 'GitHub リポジトリ（owner/name）' : 'git リポジトリ（owner/name）'}<input value={ref} onChange={(e) => setRef(e.target.value)} placeholder="my-org/app" autoComplete="off" required /></label>
      <label>{github ? 'ディレクトリのパス（git リポジトリ全体なら空）' : 'ディレクトリのパス'}
        <input value={dir} onChange={(e) => setDir(e.target.value)} placeholder="logiforge/question-a" autoComplete="off" required={!github} />
      </label>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="form-actions">
        <button className="btn primary" type="submit" disabled={busy}>{busy ? '確かめています…' : '足す'}</button>
        <button className="btn" type="button" onClick={onCancel} disabled={busy}>やめる</button>
      </div>
    </form>
  );
}

// --- GitForge ---

function Secret({ label, state, children }: { label: string; state?: SecretState; children: ReactNode }) {
  return (
    <div className="secret">
      <div className="secret-head">
        <span>{label}</span>
        <span className={state?.set ? 'setup-done' : 'dim small'}>{state?.set ? '設定済み' : '未設定'}</span>
        {state?.set && state.updated_by && <span className="dim small">{state.updated_by}　{fmt(state.updated_at)}</span>}
      </div>
      {children}
    </div>
  );
}

const INTERVAL_LABELS: Record<keyof IntervalForm, string> = {
  patrol_seconds: '見回りの間隔', list_seconds: 'repository の一覧を読み直す間隔', records_seconds: '記録を読む間隔',
  git_timeout_seconds: 'git の操作の時間の上限', write_wait_seconds: '書いたものが読む側に出るまで待つ上限',
};

function GitForgeTab({ settings, onSaved }: { settings: SettingsValue; onSaved: (s: SettingsValue) => void }) {
  const [form, setForm] = useState(() => formsFrom(settings));
  const [pemName, setPemName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const gh = <K extends keyof GitHubForm>(k: K) => ({ value: form.github[k], onChange: (e: { target: { value: string } }) => { setSaved(false); setForm({ ...form, github: { ...form.github, [k]: e.target.value } }); } });
  const fj = <K extends keyof ForgejoForm>(k: K) => ({ value: form.forgejo[k], onChange: (e: { target: { value: string } }) => { setSaved(false); setForm({ ...form, forgejo: { ...form.forgejo, [k]: e.target.value } }); } });
  const iv = (k: keyof IntervalForm) => ({ value: form.intervals[k], onChange: (e: { target: { value: string } }) => { setSaved(false); setForm({ ...form, intervals: { ...form.intervals, [k]: e.target.value } }); } });
  const readPem = async (f: File | undefined) => {
    setSaved(false);
    if (!f) { setPemName(''); setForm((cur) => ({ ...cur, github: { ...cur.github, private_key: '' } })); return; }
    const text = await f.text();
    setPemName(f.name);
    setForm((cur) => ({ ...cur, github: { ...cur.github, private_key: text } }));
  };
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const { patch, error: v } = buildPatch(settings, form);
    if (v) { setError(v); return; }
    if (!Object.keys(patch).length) { setError('変えた項目がありません。'); return; }
    setBusy(true); setError(null);
    try {
      const next = await updateGitForgeSettings(patch);
      // 置けたら、入れた秘密を消す。置けなければ、直せるように残す
      setForm(formsFrom(next)); setPemName(''); setSaved(true);
      onSaved(next);
    } catch (err) { setError(message(err)); }
    setBusy(false);
  };
  const g = settings.github ?? {}, f = settings.forgejo ?? {};
  const forge = settings.gitforge === 'github' ? 'GitHub' : 'Forgejo';
  return (
    <form className="card setup-form settings-form" onSubmit={submit}>
      <h2>{settings.gitforge === 'github' ? 'GitHub App' : 'Forgejo'}</h2>
      <p className="dim small">構成（GitHub か Forgejo か）は変えられません。</p>
      {settings.gitforge === 'github' ? <>
        <label>App の ID<input inputMode="numeric" {...gh('app_id')} autoComplete="off" /></label>
        <label>client_id<input {...gh('client_id')} autoComplete="off" /></label>
        <label>slug<input {...gh('slug')} autoComplete="off" /></label>
        <label>REST の URL<input type="url" {...gh('api_url')} placeholder="https://api.github.com" /></label>
        <label>画面の URL<input type="url" {...gh('web_url')} placeholder="https://github.com" /></label>
      </> : <>
        <label>URL（BE から Forgejo へ）<input type="url" {...fj('url')} /></label>
        <label>ブラウザからの URL<input type="url" {...fj('public_url')} /></label>
        <label>organization<input {...fj('organization')} autoComplete="off" /></label>
        <label>client_id<input {...fj('client_id')} autoComplete="off" /></label>
        <label>worker のユーザー<input {...fj('worker_user')} autoComplete="off" /></label>
      </>}

      <h2>秘密</h2>
      <p className="dim small">秘密は、画面に示しません。入れ直すことだけができます。入れたものだけを置き換えます。</p>
      <ol className="small steps">
        <li>{forge} の画面で、新しいものを作る。</li>
        <li>この画面に入れて、保存する。</li>
        <li>{forge} の画面で、古いものを消す。</li>
      </ol>
      {settings.gitforge === 'github' ? <>
        <Secret label="秘密鍵（.pem）" state={g.private_key}>
          <input type="file" accept=".pem,application/x-pem-file" aria-label="秘密鍵のファイル" onChange={(e) => void readPem(e.target.files?.[0])} />
          {pemName && <span className="dim small">{pemName} を入れ直します。</span>}
        </Secret>
        <Secret label="client secret" state={g.client_secret}>
          <input type="password" {...gh('client_secret')} autoComplete="new-password" placeholder="入れ直すときだけ" />
        </Secret>
      </> : <>
        <Secret label="client secret" state={f.client_secret}>
          <input type="password" {...fj('client_secret')} autoComplete="new-password" placeholder="入れ直すときだけ" />
        </Secret>
        <Secret label="worker のトークン" state={f.worker_token}>
          <input type="password" {...fj('worker_token')} autoComplete="new-password" placeholder="入れ直すときだけ" />
        </Secret>
      </>}

      <h2>間隔（秒）</h2>
      <WorkerNotice what="間隔と上限" />
      {INTERVAL_KEYS.map((k) => (
        <label key={k}>{INTERVAL_LABELS[k]}<input type="number" min="0" step="any" {...iv(k)} /></label>
      ))}

      {error && <p className="form-error" role="alert">{error}</p>}
      {saved && <p className="setup-done" role="status">保存しました。</p>}
      <div className="form-actions"><button className="btn primary" type="submit" disabled={busy}>{busy ? '確かめています…' : '保存する'}</button></div>
    </form>
  );
}

// --- 記録の鍵 ---

const shortKey = (k: string) => (k.length > 24 ? `${k.slice(0, 12)}…${k.slice(-8)}` : k);

function RecordKeysTab({ onChanged }: { onChanged: () => void }) {
  const [items, setItems] = useState<RecordKey[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const load = () => listRecordKeys().then(setItems).catch((e) => setError(message(e)));
  useEffect(() => { void load(); }, []);
  const create = async () => {
    if (!window.confirm(createKeyConfirm)) return;
    setBusy(true); setError(null);
    try { await createRecordKey(); await load(); onChanged(); } catch (e) { setError(message(e)); }
    setBusy(false);
  };
  return (
    <section className="card">
      <div className="page-head">
        <h2>記録の鍵</h2>
        <span className="spacer" />
        <button className="btn" disabled={busy} onClick={() => void create()}>{busy ? '作っています…' : '新しい鍵を作る'}</button>
      </div>
      <p className="dim small">記録の署名の鍵です。新しい鍵を作ると、これから書く記録に使います。古い鍵の公開鍵は残すので、前の記録はそのまま確かめられます。</p>
      {error && <p className="form-error" role="alert">{error}</p>}
      {!items ? <p className="dim">読み込み中…</p> : items.length === 0 ? <p className="dim">鍵はありません。</p> : (
        <ul className="settings-list">
          {items.map((k) => (
            <li key={k.key_id}>
              <span className="mono strong">{k.key_id}</span>
              <span className="mono" title={k.public_key}>{shortKey(k.public_key)}</span>
              <span className="dim small">{fmt(k.created_at)}</span>
              {k.primary && <span className="badge-primary">書くときの鍵</span>}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
