import { useEffect, useState, type ReactNode } from 'react';
import { createRepository, Problem } from '../api';
import { navigate, settingsPath, type AddParam, type SettingsTab } from '../router';
import {
  afterRegister, buildPatch, charCount, firstStep, MAX_STATEMENT, validateName, validateStatement, type AddStep, formsFrom, getSettings, installUrlFrom, INTERVAL_KEYS,
  listRegisteredRepositories, noteAccess, registerRepository, unregisterConfirm, unregisterRepository, updateGitForgeSettings,
  type ForgejoForm, type GitHubForm, type IntervalForm, type RegisteredRepository, type SecretState, type Settings as SettingsValue,
} from '../settings';

const fmt = (iso?: string | null) => { if (!iso) return ''; const d = new Date(iso); return isNaN(+d) ? iso : d.toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' }); };
const TAB_LABELS: Record<SettingsTab, string> = { repository: 'repository', gitforge: 'GitForge' };
/** エラーを文にする。401 はサインインに任せるので、何も示さない。 */
const message = (e: unknown): string | null =>
  e instanceof Problem ? (e.status === 401 ? null : e.code === 'forbidden' ? '設定を変えられるのは、利用者だけです。' : e.detail || e.code) : String(e);

// 設定の画面（docs/webui/settings.md、design/webui/screens-setup.md の「設定」）。利用者だけが開ける。
export function Settings({ tab, add }: { tab: SettingsTab; add?: AddParam }) {
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
      <p>利用者に足してもらうよう、ほかの利用者に頼んでください。</p>
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
      {tab === 'repository' ? <RepositoryTab settings={s} add={add} onChanged={() => void reload()} />
        : <GitForgeTab settings={s} onSaved={setS} />}
      <p className="settings-updated dim">最後に変えた人：{s.updated_by || '—'}　{fmt(s.updated_at)}</p>
    </main>
  );
}


// --- repository ---

function RepositoryTab({ settings, add, onChanged }: { settings: SettingsValue; add?: AddParam; onChanged: () => void }) {
  const [items, setItems] = useState<RegisteredRepository[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [flow, setFlow] = useState<AddStep | null>(add === 'new' ? { at: 'repository' } : null);
  const [busy, setBusy] = useState<string | null>(null);
  const load = () => listRegisteredRepositories().then((v) => { setItems(v); return v; }).catch((e) => { setError(message(e)); return null; });
  useEffect(() => {
    void load().then((v) => { if (add === 'first' && v) setFlow(firstStep(v)); });
    // 開いたら、URL から add を消す。読み直したときに、また開かないように
    if (add) navigate(settingsPath('repository'), true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
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
        {!flow && <button className="btn" onClick={() => setFlow({ at: 'repository' })}>足す</button>}
      </div>
      {flow && <AddFlow github={github} step={flow} onStep={setFlow} onClose={() => setFlow(null)} onRegistered={() => { void load(); onChanged(); }} />}
      {error && <p className="form-error" role="alert">{error}</p>}
      {!items ? <p className="dim">読み込み中…</p> : items.length === 0 ? <p className="dim">登録した repository はありません。</p> : (
        <ul className="settings-list">
          {items.map((r) => (
            <li key={r.name}>
              <span className="mono strong">{r.name}</span>
              <span className="mono">{r.gitforge_ref}{r.directory ? ` : ${r.directory}` : ''}</span>
              <span className="spacer" />
              {/* prepared は、worker が取り込んだか（latest があるか）で決まるので、登録した直後は false に見える（design/webui/screens-setup.md） */}
              {r.prepared === false && <button className="btn primary" disabled={!!flow} onClick={() => setFlow({ at: 'question', name: r.name })}>最初の問いを入れる</button>}
              <button className="btn" disabled={busy === r.name} onClick={() => void remove(r.name)}>外す</button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// 足す流れ：1. repository → 2. 最初の問い → 完了。トップの画面には移らない（docs/webui/settings.md の「repository を足す」）。
function AddFlow({ github, step, onStep, onClose, onRegistered }: {
  github: boolean; step: AddStep; onStep: (s: AddStep) => void; onClose: () => void; onRegistered: () => void;
}) {
  const second = step.at === 'repository' ? '' : step.at === 'question' ? 'on' : 'done';
  return (
    <div className="add-flow">
      <ol className="flow-steps" aria-label="足す段">
        <li className={step.at === 'repository' ? 'on' : 'done'}>1. repository</li>
        <li className={second}>2. 最初の問い{step.at === 'exists' && '（もうある）'}</li>
      </ol>
      {step.at === 'repository' ? <RepositoryStep github={github} onNext={onStep} onCancel={onClose} onRegistered={onRegistered} />
        : step.at === 'question' ? <QuestionStep name={step.name} onDone={(exists) => { onStep({ at: exists ? 'exists' : 'done', name: step.name }); onRegistered(); }} onCancel={onClose} />
        : <Finished name={step.name} exists={step.at === 'exists'} onClose={onClose} />}
    </div>
  );
}

function RepositoryStep({ github, onNext, onCancel, onRegistered }: { github: boolean; onNext: (s: AddStep) => void; onCancel: () => void; onRegistered: () => void }) {
  // Forgejo では、ふつうの repository は Forgejo に作る（登録しない）。ディレクトリの repository は登録する。
  const [kind, setKind] = useState<'create' | 'directory'>(github ? 'directory' : 'create');
  const [name, setName] = useState('');
  const [ref, setRef] = useState('');
  const [dir, setDir] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ReactNode>(null);
  const register = !github && kind === 'create' ? false : true;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const n = name.trim(), r = ref.trim(), d = dir.trim().replace(/^\/+|\/+$/g, '');
    const v = validateName(n);
    if (v) { setError(v); return; }
    if (!register) { onNext({ at: 'question', name: n }); return; }
    if (!/^[^/\s]+\/[^/\s]+$/.test(r)) { setError('git リポジトリは owner/name の形で入れてください。'); return; }
    if (!github && !d) { setError('ディレクトリのパスを入れてください。'); return; }
    setBusy(true); setError(null);
    try {
      onNext(afterRegister(await registerRepository(n, r, d)));
      onRegistered();
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
      {!github && (
        <div className="setup-row" role="radiogroup" aria-label="repository の種類">
          <label><input type="radio" name="kind" checked={kind === 'create'} onChange={() => setKind('create')} />Forgejo に新しく作る</label>
          <label><input type="radio" name="kind" checked={kind === 'directory'} onChange={() => setKind('directory')} />git リポジトリの中のディレクトリを使う</label>
        </div>
      )}
      <label>名前<input value={name} onChange={(e) => setName(e.target.value)} placeholder="question-a" autoFocus required /></label>
      {register && <>
        <label>{github ? 'GitHub リポジトリ（owner/name）' : 'git リポジトリ（owner/name）'}<input value={ref} onChange={(e) => setRef(e.target.value)} placeholder="my-org/app" autoComplete="off" required /></label>
        <label>{github ? 'ディレクトリのパス（git リポジトリ全体なら空）' : 'ディレクトリのパス'}
          <input value={dir} onChange={(e) => setDir(e.target.value)} placeholder="logiforge/question-a" autoComplete="off" required={!github} />
        </label>
      </>}
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="form-actions">
        <button className="btn primary" type="submit" disabled={busy}>{busy ? '確かめています…' : register ? '登録して次へ' : '次へ'}</button>
        <button className="btn" type="button" onClick={onCancel} disabled={busy}>やめる</button>
      </div>
    </form>
  );
}

const CREATE_ERRORS: Record<string, string> = {
  forbidden: 'repository を作れるのは、利用者だけです。',
  repository_not_registered: 'その名前の repository は、登録されていません。',
};

function QuestionStep({ name, onDone, onCancel }: { name: string; onDone: (exists: boolean) => void; onCancel: () => void }) {
  const [statement, setStatement] = useState('');
  const [background, setBackground] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const n = charCount(statement);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const v = validateStatement(statement);
    if (v) { setError(v); return; }
    setBusy(true); setError(null);
    try {
      // スナップショットツリーがまだ出ていなくても（Logiforge-Pending-Commit）、用意は済んでいる。ビュワーが「取り込み中」と示して読み直す
      await createRepository(name, statement.trim(), background.trim());
      onDone(false);
    } catch (err) {
      setBusy(false);
      if (err instanceof Problem && err.status === 401) return;
      // 取り込み待ちで prepared が false に見えていた repository は、用意が済んでいる（design/webui/screens-setup.md）
      if (err instanceof Problem && err.code === 'repository_exists') { onDone(true); return; }
      setError(err instanceof Problem ? CREATE_ERRORS[err.code] ?? (err.detail || err.code) : String(err));
    }
  };
  return (
    <form className="setup-form settings-form" onSubmit={submit}>
      <label>根の仮説の主文 <span className={`dim ${n > MAX_STATEMENT ? 'over' : ''}`}>{n} / {MAX_STATEMENT}</span>
        <input value={statement} onChange={(e) => { setStatement(e.target.value); setError(null); }} placeholder="ある究極の問いの主文" autoFocus required />
      </label>
      <label>背景情報（なくてもよい）<textarea value={background} onChange={(e) => setBackground(e.target.value)} rows={3} /></label>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="form-actions">
        <button className="btn primary" type="submit" disabled={busy}>{busy ? '用意しています…' : '用意する'}</button>
        <button className="btn" type="button" onClick={onCancel} disabled={busy}>あとで入れる</button>
      </div>
    </form>
  );
}

function Finished({ name, exists, onClose }: { name: string; exists: boolean; onClose: () => void }) {
  return (
    <div className="setup-form settings-form" role="status">
      <p>{exists
        ? <><span className="mono strong">{name}</span> には、スナップショットツリーがもうあります。最初の問いは要りません。</>
        : <><span className="mono strong">{name}</span> を用意しました。</>}</p>
      <div className="form-actions">
        <button className="btn primary" onClick={() => navigate(`/r/${name}/tree`)}>スナップショットツリーを開く</button>
        <button className="btn" onClick={onClose}>閉じる</button>
      </div>
    </div>
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
  patrol_seconds: '見回りの間隔', list_seconds: 'repository の一覧を読み直す間隔',
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
  return (
    <form className="card setup-form settings-form" onSubmit={submit}>
      <h2>{settings.gitforge === 'github' ? 'GitHub App' : 'Forgejo'}</h2>
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
      <p className="dim small">入れたものだけを置き換えます。新しいものを保存してから、古いものを消してください。</p>
      {settings.gitforge === 'github' ? <>
        <Secret label="秘密鍵（.pem）" state={g.private_key}>
          <input type="file" accept=".pem,application/x-pem-file" aria-label="秘密鍵のファイル" onChange={(e) => void readPem(e.target.files?.[0])} />
          {pemName && <span className="dim small">{pemName}</span>}
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
      {INTERVAL_KEYS.map((k) => (
        <label key={k}>{INTERVAL_LABELS[k]}<input type="number" min="0" step="any" {...iv(k)} /></label>
      ))}

      {error && <p className="form-error" role="alert">{error}</p>}
      {saved && <p className="setup-done" role="status">保存しました。</p>}
      <div className="form-actions"><button className="btn primary" type="submit" disabled={busy}>{busy ? '確かめています…' : '保存する'}</button></div>
    </form>
  );
}

