// 初回のセットアップの API（design/api/endpoints/setup.md）と、GitHub App の manifest flow（design/settings/setup.md）。
// - getSetup のほかは、Logiforge-Setup-Code ヘッダーでコードを付ける。
// - コードは、セットアップのあいだ sessionStorage に置く。GitHub や Forgejo へ移動して戻っても、入れ直さなくてよいように。
// - コードを求める呼び出しが unauthenticated を返したら、コードを消す。画面はコードの段に戻す。

import { Problem } from './api';
import { getAuth, randomString } from './auth';

export type GitForgeKind = 'github' | 'forgejo';
export interface SetupGitHubApp { slug: string; install_url: string }
export interface SetupState {
  done: boolean;
  step: 'gitforge' | 'github_app' | 'forgejo' | 'signin' | null;
  gitforge: GitForgeKind | null;
  github_app: SetupGitHubApp | null;
}
export interface ManifestRequest { owner: string; owner_kind: 'organization' | 'user'; webui_url: string; public: boolean }
export interface ManifestResponse { manifest: Record<string, unknown>; post_url: string }
export interface SetupRepository { name: string; gitforge_ref: string; directory?: string }
export interface ForgejoInput {
  url: string; public_url: string; organization: string; client_id: string; client_secret: string; worker_user: string; worker_token: string;
}

const CODE = 'lf-setup-code';
const MANIFEST_STATE = 'lf-setup-manifest-state';
// 画面だけで覚える進み具合。getSetup の step では分からないもの（install を開いたか、repository を登録したか）
const INSTALLED = 'lf-setup-installed';
const REPOSITORY = 'lf-setup-repository';

export const CALLBACK_PATH = '/setup/github/callback';

export const getCode = () => sessionStorage.getItem(CODE);
export const setCode = (c: string) => sessionStorage.setItem(CODE, c);
export const clearCode = () => sessionStorage.removeItem(CODE);

export const markInstalled = () => sessionStorage.setItem(INSTALLED, '1');
export const markRepository = () => sessionStorage.setItem(REPOSITORY, '1');
export const progress = () => ({ installed: !!sessionStorage.getItem(INSTALLED), repository: !!sessionStorage.getItem(REPOSITORY) });

/** セットアップが終わったら、sessionStorage に置いたものを消す。 */
export function clearSetupStorage() {
  for (const k of [CODE, MANIFEST_STATE, INSTALLED, REPOSITORY]) sessionStorage.removeItem(k);
}

async function problem(r: Response): Promise<Problem> {
  let code = 'internal', detail = '';
  try { const p = await r.json(); code = p.code ?? code; detail = p.detail ?? ''; } catch { /* 本文がない */ }
  return new Problem(r.status, code, detail);
}

/** セットアップが終わったか、どの段まで進んだか。コードを求めない。 */
export async function getSetup(): Promise<SetupState> {
  const r = await fetch('/v1/setup');
  if (!r.ok) throw await problem(r);
  return (await r.json()) as SetupState;
}

async function send(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<Response> {
  const code = getCode();
  if (!code) throw new Problem(401, 'unauthenticated', '');
  const h: Record<string, string> = { ...headers, 'Logiforge-Setup-Code': code };
  if (body !== undefined) h['Content-Type'] = 'application/json';
  const r = await fetch('/v1' + path, { method, headers: h, body: body === undefined ? undefined : JSON.stringify(body) });
  if (!r.ok) {
    const p = await problem(r);
    // コードが違うか、サーバが起動し直してコードが変わった。呼びすぎで断られたときは、コードを残す
    if ((p.code === 'unauthenticated' || r.status === 401) && !isRateLimited(p)) clearCode();
    if (p.code === 'setup_done') clearSetupStorage();
    throw p;
  }
  return r;
}

/** 送り元の IP ごとの回数の上限（1 分に 10 回）で断られたか。BE は unauthenticated を返し、detail に書く。 */
export const isRateLimited = (p: Problem) => p.status === 401 && /1\s*分に\s*10\s*回/.test(p.detail);

const json = async <T>(r: Response) => (await r.json()) as T;

export const checkSetupCode = async () => { await send('POST', '/setup/code'); };
// webui_url は、サインインの redirect_uri の元になる。どちらの構成でも、今の WebUI のオリジンを送る
export const setSetupGitForge = async (gitforge: GitForgeKind, webuiUrl = location.origin) =>
  json<SetupState>(await send('PUT', '/setup/gitforge', { gitforge, webui_url: webuiUrl }));
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

export type SetupStep = 'code' | 'gitforge' | 'github_app' | 'install' | 'repository' | 'forgejo' | 'signin';

/** どの段から続けるか。getSetup の応答と、画面が覚えている進み具合から決める。 */
export function continueStep(s: SetupState, hasCode: boolean, p = { installed: false, repository: false }): SetupStep {
  if (!hasCode) return 'code';
  if (s.step === 'gitforge' || !s.gitforge) return 'gitforge';
  if (s.gitforge === 'forgejo') return s.step === 'signin' ? 'signin' : 'forgejo';
  if (s.step === 'github_app' || !s.github_app) return 'github_app';
  if (!p.installed) return 'install';
  if (!p.repository) return 'repository';
  return 'signin';
}

/** 構成ごとの段の並び。 */
export const stepsFor = (g: GitForgeKind): SetupStep[] =>
  g === 'github' ? ['code', 'gitforge', 'github_app', 'install', 'repository', 'signin'] : ['code', 'gitforge', 'forgejo', 'signin'];

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
