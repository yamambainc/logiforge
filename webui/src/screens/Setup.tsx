// セットアップの画面（docs/webui/setup.md、design/webui/screens-setup.md）。
// セットアップが終わるまで、どの URL を開いてもこの画面を出す。段は上から順に進め、前の段が終わるまで次の段は押せない。
import { useEffect, useState, useSyncExternalStore, type ReactNode } from 'react';
import { Problem } from '../api';
import { getAuth, signIn, subscribe, type SignedOutReason } from '../auth';
import { settingsPath } from '../router';
import {
  CALLBACK_PATH, continueStep, createSetupGitHubManifest, finishSetup, getSetup, handleManifestCallback, inOtherBrowser, isSetupInProgress,
  markInstalled, markRepository, progress, setSetupForgejo, setSetupGitForge, setSetupRepository, stepsFor, submitManifest,
  type GitForgeKind, type ManifestCallbackResult, type SetupState, type SetupStep,
} from '../setup';

const signedOutMessages: Partial<Record<SignedOutReason, string>> = {
  denied: 'サインインが許可されませんでした。',
  'state-mismatch': 'サインインの確かめに失敗しました。もう一度サインインしてください。',
  'exchange-failed': 'サインインのコードを交換できませんでした。もう一度サインインしてください。',
  unavailable: 'GitForge につながりませんでした。しばらくしてから、もう一度サインインしてください。',
  'not-configured': 'サインインのアプリケーションが、まだ置かれていません。前の段を終えてください。',
};

// 開発の StrictMode では effect が 2 回走る。GitHub から戻ったときの交換と、終わりの呼び出しは 1 回だけにする
let callbackOnce: Promise<ManifestCallbackResult> | null = null;
let finishOnce: Promise<SetupState> | null = null;

export function Setup({ initial }: { initial: SetupState }) {
  const [s, setS] = useState(initial);
  const [prog, setProg] = useState(progress);
  const [errors, setErrors] = useState<Partial<Record<SetupStep, ReactNode>>>({});
  const [busy, setBusy] = useState<SetupStep | null>(null);
  // 別のブラウザがセットアップを進めている。段は出さない
  const [other, setOther] = useState(() => inOtherBrowser(initial));
  const auth = useSyncExternalStore(subscribe, getAuth);

  const gitforge: GitForgeKind = s.gitforge ?? 'github';
  const steps = stepsFor(gitforge);
  const current = continueStep(s, prog);
  const at = steps.indexOf(current);
  const enabled = (st: SetupStep) => steps.indexOf(st) <= at;
  const done = (st: SetupStep) => steps.indexOf(st) < at;
  const setError = (st: SetupStep, e: ReactNode) => setErrors((m) => ({ ...m, [st]: e }));

  /** 段に共通のエラーの扱い。扱ったら true。 */
  function common(e: unknown, st: SetupStep): boolean {
    // 終わりの呼び出しで、サインインのトークンが受け入れられなかった。detail を示し、サインインし直せるようにする
    if (e instanceof Problem && e.code === 'unauthenticated') {
      setError(st, <>サインインを確かめられませんでした。もう一度サインインしてください。{e.detail && <><br />{e.detail}</>}</>);
      return true;
    }
    if (e instanceof Problem && e.code === 'setup_done') { location.assign('/'); return true; }
    if (isSetupInProgress(e)) { setOther(true); return true; }
    if (!(e instanceof Problem)) { setError(st, 'BE につながりませんでした。しばらくしてから、もう一度押してください。'); return true; }
    return false;
  }

  /** 段の呼び出し。うまくいけば true。 */
  async function run(st: SetupStep, f: () => Promise<void>, onProblem?: (p: Problem) => ReactNode): Promise<boolean> {
    setBusy(st);
    setError(st, null);
    try { await f(); return true; } catch (e) {
      if (!common(e, st)) {
        const p = e as Problem;
        setError(st, onProblem?.(p) ?? (p.detail || p.code));
      }
      return false;
    } finally { setBusy(null); }
  }

  // GitHub で App を作って戻ったとき
  useEffect(() => {
    if (location.pathname !== CALLBACK_PATH && !callbackOnce) return;
    callbackOnce ??= handleManifestCallback();
    void run('github_app', async () => {
      const r = await callbackOnce!;
      if (!r.ok) {
        setError('github_app', r.reason === 'state-mismatch'
          ? 'GitHub から戻ったときの確かめに失敗しました。もう一度「GitHub で App を作る」を押してください。'
          : 'GitHub が App を作りませんでした。もう一度「GitHub で App を作る」を押してください。');
        return;
      }
      setS(await getSetup());
    }, (p) => p.code === 'validation_failed'
      ? <>App を作れませんでした。code が切れたかもしれません。もう一度「GitHub で App を作る」を押してください。{p.detail && <><br />{p.detail}</>}</>
      : undefined);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // サインインから /setup に戻ったら、最初の利用者にして、セットアップを終える
  useEffect(() => {
    if (auth.kind !== 'signed-in') return;
    finishOnce ??= finishSetup();
    void run('signin', async () => {
      try { await finishOnce!; } catch (e) { finishOnce = null; throw e; }
      // 続けて、設定の画面で最初の問いを入れる（docs/webui/setup.md）
      location.assign(settingsPath('repository', 'first'));
    }, (p) => {
      if (p.code === 'forbidden') {
        const m = gitforge === 'github'
          ? 'サインインした人は、登録した repository に書けません（Write 以上が要ります）。書ける人でサインインし直してください。'
          : 'サインインした人は、organization の team で write 以上がありません。write の team に入っている人でサインインし直してください。';
        return <>{m}{p.detail && <><br />{p.detail}</>}</>;
      }
      return undefined;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.kind]);

  if (other) {
    return (
      <div className="setup">
        <div className="setup-box">
          <h1 className="setup-title">Logiforge のセットアップ</h1>
          <p role="alert">セットアップは別のブラウザで進行中です。最初からやり直すときは、設定の DB を消して起動し直してください。</p>
        </div>
      </div>
    );
  }

  return (
    <div className="setup">
      <div className="setup-box">
        <h1 className="setup-title">Logiforge のセットアップ</h1>

        <Step n={1} title="構成" on={enabled('gitforge')} done={done('gitforge')} error={errors.gitforge}>
          <GitForgeStep value={s.gitforge} disabled={!enabled('gitforge') || busy === 'gitforge'} onSubmit={(g) =>
            void run('gitforge', async () => { setS(await setSetupGitForge(g)); })} />
        </Step>

        {gitforge === 'github' ? <>
          <Step n={2} title="GitHub App" on={enabled('github_app')} done={done('github_app')} error={errors.github_app}>
            {s.github_app && <p className="dim small">App「{s.github_app.slug}」を作りました。作り直すときだけ、もう一度押してください。</p>}
            <GitHubAppStep disabled={!enabled('github_app') || busy === 'github_app'} onSubmit={(req) =>
              void run('github_app', async () => { submitManifest(await createSetupGitHubManifest(req)); })} />
          </Step>

          <Step n={3} title="install" on={enabled('install')} done={done('install')}>
            {s.github_app ? (
              <a className="btn" href={s.github_app.install_url} target="_blank" rel="noreferrer"
                onClick={() => { markInstalled(); setProg(progress()); }}>GitHub で install する</a>
            ) : <button className="btn" disabled>GitHub で install する</button>}
            <p className="dim small">登録する GitHub リポジトリの owner に install してください。GitHub の画面が、別のタブで開きます。</p>
          </Step>

          <Step n={4} title="repository" on={enabled('repository')} done={done('repository')} error={errors.repository}>
            <RepositoryStep disabled={!enabled('repository') || busy === 'repository'} onSubmit={(repo) =>
              void run('repository', async () => { setS(await setSetupRepository(repo)); markRepository(); setProg(progress()); }, (p) =>
                p.code === 'validation_failed' ? <>
                  GitHub リポジトリを読めませんでした。App が、その owner に install されていないかもしれません。
                  {s.github_app && <> <a href={s.github_app.install_url} target="_blank" rel="noreferrer">GitHub で install する</a></>}
                  {p.detail && <><br />{p.detail}</>}
                </> : undefined)} />
          </Step>
        </> : (
          <Step n={2} title="Forgejo" on={enabled('forgejo')} done={done('forgejo')} error={errors.forgejo}>
            <ForgejoStep saved={done('forgejo')} disabled={!enabled('forgejo') || busy === 'forgejo'} onSubmit={(f) =>
              run('forgejo', async () => { setS(await setSetupForgejo(f)); })} />
          </Step>
        )}

        <Step n={gitforge === 'github' ? 5 : 3} title="サインイン" on={enabled('signin')} done={false}
          error={errors.signin ?? (auth.kind === 'signed-out' ? signedOutMessages[auth.reason] : null)}>
          <button className="btn primary" disabled={!enabled('signin') || busy === 'signin' || (auth.kind === 'signed-in' && !errors.signin)}
            onClick={() => void signIn('/setup')}>
            {gitforge === 'github' ? 'GitHub' : 'Forgejo'} でサインインして、利用者になる
          </button>
          <p className="dim small">サインインした人が、最初の利用者になります。
            {gitforge === 'github' ? '4 の repository に書ける人でサインインしてください。' : 'organization の write の team に入っている人でサインインしてください。'}
          </p>
        </Step>
      </div>
    </div>
  );
}

function Step({ n, title, on, done, error, children }: { n: number; title: string; on: boolean; done: boolean; error?: ReactNode; children: ReactNode }) {
  return (
    <section className={`setup-step ${on ? '' : 'off'} ${done ? 'done' : ''}`} aria-disabled={!on}>
      <h2>{n}. {title}{done && <span className="setup-done">済み</span>}</h2>
      <fieldset disabled={!on}>{children}</fieldset>
      {error && <p className="form-error" role="alert">{error}</p>}
    </section>
  );
}

function GitForgeStep({ value, disabled, onSubmit }: { value: GitForgeKind | null; disabled: boolean; onSubmit: (g: GitForgeKind) => void }) {
  const [g, setG] = useState<GitForgeKind>(value ?? 'github');
  return (
    <form className="setup-row" onSubmit={(e) => { e.preventDefault(); onSubmit(g); }}>
      <label><input type="radio" name="gitforge" checked={g === 'github'} onChange={() => setG('github')} /> GitHub</label>
      <label><input type="radio" name="gitforge" checked={g === 'forgejo'} onChange={() => setG('forgejo')} /> Forgejo</label>
      <button className="btn" disabled={disabled}>{value ? '選び直す' : '次へ'}</button>
    </form>
  );
}

function GitHubAppStep({ disabled, onSubmit }: { disabled: boolean; onSubmit: (r: { owner: string; owner_kind: 'organization' | 'user'; webui_url: string; public: boolean }) => void }) {
  const [owner, setOwner] = useState('');
  const [kind, setKind] = useState<'organization' | 'user'>('organization');
  const [webui, setWebui] = useState(location.origin);
  const [pub, setPub] = useState(false);
  return (
    <form className="setup-form" onSubmit={(e) => { e.preventDefault(); onSubmit({ owner: owner.trim(), owner_kind: kind, webui_url: webui.trim(), public: pub }); }}>
      <label>owner<input value={owner} onChange={(e) => setOwner(e.target.value)} placeholder="my-org" required /></label>
      <div className="setup-row">
        <label><input type="radio" name="owner_kind" checked={kind === 'organization'} onChange={() => setKind('organization')} /> organization</label>
        <label><input type="radio" name="owner_kind" checked={kind === 'user'} onChange={() => setKind('user')} /> アカウント</label>
      </div>
      <label>WebUI の URL<input type="url" value={webui} onChange={(e) => setWebui(e.target.value)} required /></label>
      <label className="check"><input type="checkbox" checked={pub} onChange={(e) => setPub(e.target.checked)} /> ほかの owner にも install する</label>
      <p className="dim small">GitHub の画面で App を作ります。App を作る owner の admin の権限（organization なら organization の owner）が要ります。</p>
      <div className="form-actions"><button className="btn" disabled={disabled || !owner.trim()}>GitHub で App を作る</button></div>
    </form>
  );
}

function RepositoryStep({ disabled, onSubmit }: { disabled: boolean; onSubmit: (r: { name: string; gitforge_ref: string; directory?: string }) => void }) {
  const [name, setName] = useState('');
  const [ref, setRef] = useState('');
  const [dir, setDir] = useState('');
  return (
    <form className="setup-form" onSubmit={(e) => {
      e.preventDefault();
      const d = dir.trim();
      onSubmit(d ? { name: name.trim(), gitforge_ref: ref.trim(), directory: d } : { name: name.trim(), gitforge_ref: ref.trim() });
    }}>
      <label>名前<input value={name} onChange={(e) => setName(e.target.value)} placeholder="question-a" pattern="[a-z0-9][a-z0-9\-]{0,62}" required /></label>
      <label>owner/name<input value={ref} onChange={(e) => setRef(e.target.value)} placeholder="my-org/question-a" required /></label>
      <label>ディレクトリのパス（ディレクトリの repository のときだけ）<input value={dir} onChange={(e) => setDir(e.target.value)} placeholder="logiforge/question-a" /></label>
      <div className="form-actions"><button className="btn" disabled={disabled || !name.trim() || !ref.trim()}>登録する</button></div>
    </form>
  );
}

const emptyForgejo = { url: '', public_url: '', organization: '', client_id: '', client_secret: '', worker_user: '', worker_token: '' };

function ForgejoStep({ saved, disabled, onSubmit }: { saved: boolean; disabled: boolean; onSubmit: (f: typeof emptyForgejo) => Promise<boolean> }) {
  const [f, setF] = useState(emptyForgejo);
  const [editing, setEditing] = useState(false);
  const field = (k: keyof typeof emptyForgejo) => ({ value: f[k], onChange: (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value }) });
  // 置いた秘密は、画面に戻さない
  if (saved && !editing) {
    return <div className="setup-row"><span>置きました。</span><button type="button" className="btn" onClick={() => { setF(emptyForgejo); setEditing(true); }}>入れ直す</button></div>;
  }
  const redirect = location.origin + '/oauth/callback';
  return (
    <form className="setup-form" onSubmit={(e) => {
      e.preventDefault();
      const t = Object.fromEntries(Object.entries(f).map(([k, v]) => [k, v.trim()])) as typeof emptyForgejo;
      // 置けたら、入れた秘密を消す。置けなければ、直せるように残す
      void onSubmit({ ...t, public_url: t.public_url || t.url }).then((ok) => { if (ok) { setF(emptyForgejo); setEditing(false); } });
    }}>
      <label>URL（BE から Forgejo へ）<input type="url" {...field('url')} placeholder="https://forgejo.example" required /></label>
      <label>ブラウザからの URL（BE からと同じなら空）<input type="url" {...field('public_url')} /></label>
      <label>organization<input {...field('organization')} placeholder="my-org" required /></label>
      <p className="dim small">Forgejo で OAuth2 のアプリケーション（confidential client）を作り、redirect_uri に次を登録してください。<br /><code>{redirect}</code></p>
      <label>client_id<input {...field('client_id')} autoComplete="off" required /></label>
      <label>client secret<input type="password" {...field('client_secret')} autoComplete="new-password" required /></label>
      <p className="dim small">worker のユーザーは、organization の読むだけの team にだけ入れてください。トークンのスコープは read:repository、read:organization、read:issue です。</p>
      <label>worker のユーザー<input {...field('worker_user')} autoComplete="off" required /></label>
      <label>worker のトークン<input type="password" {...field('worker_token')} autoComplete="new-password" required /></label>
      <div className="form-actions"><button className="btn" disabled={disabled}>確かめる</button></div>
    </form>
  );
}
