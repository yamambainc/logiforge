// 初回のセットアップの API（design/api/endpoints/setup.md）と、GitHub App の manifest flow（design/settings/setup.md）。
// - セットアップを始めたブラウザだけが続けられる。構成を選ぶ呼び出しで始まり、BE がセットアップのトークンを返す。
//   ほかの書く呼び出しには、そのトークンを Logiforge-Setup-Token のヘッダーで付ける。ないか違えば setup_in_progress で断られる。
//   終わったあとは setup_done で断られる。
// - トークンは localStorage に置く。ページを閉じても、GitHub やサインインへ移動して戻っても、続けられるように。
// - 画面の進み具合と manifest flow の state は、sessionStorage に置く。GitHub や Forgejo へ移動して戻っても、続きから進めるように。

import { Problem } from './api';
import { getAuth, randomString } from './auth';

export type GitForgeKind = 'github' | 'forgejo';
export interface SetupGitHubApp { slug: string; install_url: string }
export interface SetupState {
  done: boolean;
  /** だれかがセットアップを始めたか。 */
  started?: boolean;
  step: 'gitforge' | 'github_app' | 'forgejo' | 'signin' | null;
  gitforge: GitForgeKind | null;
  github_app: SetupGitHubApp | null;
  /** セットアップを始めた応答（構成を選ぶ呼び出し）にだけ入る。 */
  setup_token?: string;
}
export interface ManifestRequest { owner: string; owner_kind: 'organization' | 'user'; webui_url: string; public: boolean }
export interface ManifestResponse { manifest: Record<string, unknown>; post_url: string }
export interface SetupRepository { name: string; gitforge_ref: string; directory?: string }
export interface ForgejoInput {
  url: string; public_url: string; organization: string; client_id: string; client_secret: string; worker_user: string; worker_token: string;
}

const MANIFEST_STATE = 'lf-setup-manifest-state';
const TOKEN = 'lf-setup-token';
export const TOKEN_HEADER = 'Logiforge-Setup-Token';
// 画面だけで覚える進み具合。getSetup の step では分からないもの（install を開いたか、repository を登録したか）
const INSTALLED = 'lf-setup-installed';
const REPOSITORY = 'lf-setup-repository';

export const CALLBACK_PATH = '/setup/github/callback';

export const markInstalled = () => sessionStorage.setItem(INSTALLED, '1');
export const markRepository = () => sessionStorage.setItem(REPOSITORY, '1');
export const progress = () => ({ installed: !!sessionStorage.getItem(INSTALLED), repository: !!sessionStorage.getItem(REPOSITORY) });

const setupToken = () => localStorage.getItem(TOKEN);

/** セットアップが終わったら、置いたもの（sessionStorage の進み具合と、localStorage のトークン）を消す。 */
export function clearSetupStorage() {
  for (const k of [MANIFEST_STATE, INSTALLED, REPOSITORY]) sessionStorage.removeItem(k);
  localStorage.removeItem(TOKEN);
}

/** 別のブラウザがセットアップを進めているか。始まっていて、このブラウザにトークンがなければそう。 */
export const inOtherBrowser = (s: SetupState) => !s.done && !!s.started && !setupToken();

/** 呼び出しが、別のブラウザで進行中のために断られたか。 */
export const isSetupInProgress = (e: unknown) => e instanceof Problem && e.code === 'setup_in_progress';

async function problem(r: Response): Promise<Problem> {
  let code = 'internal', detail = '';
  try { const p = await r.json(); code = p.code ?? code; detail = p.detail ?? ''; } catch { /* 本文がない */ }
  return new Problem(r.status, code, detail);
}

/** セットアップが終わったか、どの段まで進んだか。 */
export async function getSetup(): Promise<SetupState> {
  const r = await fetch('/v1/setup');
  if (!r.ok) throw await problem(r);
  return (await r.json()) as SetupState;
}

async function send(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<Response> {
  const h: Record<string, string> = { ...headers };
  const t = setupToken();
  if (t) h[TOKEN_HEADER] = t;
  if (body !== undefined) h['Content-Type'] = 'application/json';
  const r = await fetch('/v1' + path, { method, headers: h, body: body === undefined ? undefined : JSON.stringify(body) });
  if (!r.ok) {
    const p = await problem(r);
    if (p.code === 'setup_done') clearSetupStorage();
    // トークンが受け入れられなかった。持っていても続けられないので消す
    else if (p.code === 'setup_in_progress') localStorage.removeItem(TOKEN);
    throw p;
  }
  return r;
}

const json = async <T>(r: Response) => (await r.json()) as T;

// webui_url は、サインインの redirect_uri の元になる。どちらの構成でも、今の WebUI のオリジンを送る
// セットアップを始める呼び出し。応答にトークンが入っていれば置く
export async function setSetupGitForge(gitforge: GitForgeKind, webuiUrl = location.origin): Promise<SetupState> {
  const { setup_token, ...s } = await json<SetupState>(await send('PUT', '/setup/gitforge', { gitforge, webui_url: webuiUrl }));
  if (setup_token) localStorage.setItem(TOKEN, setup_token);
  return s;
}
export const createSetupGitHubManifest = async (req: ManifestRequest) => json<ManifestResponse>(await send('POST', '/setup/github/manifest', req));
export const convertSetupGitHubManifest = async (code: string) => json<SetupGitHubApp>(await send('POST', '/setup/github/conversion', { code }));
export const setSetupRepository = async (repo: SetupRepository) => json<SetupState>(await send('PUT', '/setup/repository', repo));
export const setSetupForgejo = async (f: ForgejoInput) => json<SetupState>(await send('PUT', '/setup/forgejo', f));

/** サインインで得たトークンを付けて、セットアップを終える。終わったら、置いたものを消す。 */
export async function finishSetup(): Promise<SetupState> {
  const a = getAuth();
  if (a.kind !== 'signed-in') throw new Problem(401, 'unauthenticated', 'サインインしていない');
  const s = await json<SetupState>(await send('POST', '/setup/finish', undefined, { Authorization: `Bearer ${a.token}` }));
  clearSetupStorage();
  return s;
}

// --- 画面の段 ---

export type SetupStep = 'gitforge' | 'github_app' | 'install' | 'repository' | 'forgejo' | 'signin';

/** どの段から続けるか。getSetup の応答と、画面が覚えている進み具合から決める。 */
export function continueStep(s: SetupState, p = { installed: false, repository: false }): SetupStep {
  if (s.step === 'gitforge' || !s.gitforge) return 'gitforge';
  if (s.gitforge === 'forgejo') return s.step === 'signin' ? 'signin' : 'forgejo';
  if (s.step === 'github_app' || !s.github_app) return 'github_app';
  if (!p.installed) return 'install';
  if (!p.repository) return 'repository';
  return 'signin';
}

/** 構成ごとの段の並び。 */
export const stepsFor = (g: GitForgeKind): SetupStep[] =>
  g === 'github' ? ['gitforge', 'github_app', 'install', 'repository', 'signin'] : ['gitforge', 'forgejo', 'signin'];

// --- manifest flow ---

/** manifest を送るフォームの送り先。state を付ける。 */
export function manifestAction(postUrl: string, state: string): string {
  const u = new URL(postUrl);
  u.searchParams.set('state', state);
  return u.toString();
}

/** manifest をフォームで GitHub に送る。state は乱数にし、sessionStorage に置く。 */
export function submitManifest(m: ManifestResponse, doc: Document = document): void {
  const st = randomString();
  sessionStorage.setItem(MANIFEST_STATE, st);
  const form = doc.createElement('form');
  form.method = 'post';
  form.action = manifestAction(m.post_url, st);
  form.style.display = 'none';
  const input = doc.createElement('input');
  input.type = 'hidden';
  input.name = 'manifest';
  input.value = JSON.stringify(m.manifest);
  form.appendChild(input);
  doc.body.appendChild(form);
  form.submit();
}

export type ManifestCallbackResult = { ok: true; app: SetupGitHubApp } | { ok: false; reason: 'state-mismatch' | 'no-code' };

/** GitHub から /setup/github/callback に戻ったとき。交換の前に URL から code を消し、state を照らし合わせてから code を送る。 */
export async function handleManifestCallback(): Promise<ManifestCallbackResult> {
  const q = new URLSearchParams(location.search);
  const code = q.get('code'), st = q.get('state');
  history.replaceState(null, '', '/setup');
  const want = sessionStorage.getItem(MANIFEST_STATE);
  sessionStorage.removeItem(MANIFEST_STATE);
  if (!st || !want || st !== want) return { ok: false, reason: 'state-mismatch' };
  if (!code) return { ok: false, reason: 'no-code' };
  return { ok: true, app: await convertSetupGitHubManifest(code) };
}
