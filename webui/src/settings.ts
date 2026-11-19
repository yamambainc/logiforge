// 設定の API（design/api/endpoints/settings.md）と、設定の画面が使う決まり（design/webui/screens-setup.md の「設定」）。
// - どれも、主体が user であることを求める。user でなければ forbidden。
// - 秘密は返らない。置いたかどうかと、最後に変えた人と時刻だけが返る。
import { useSyncExternalStore } from 'react';
import { call, Problem, send } from './api';
import type { GitForgeKind } from './setup';

export interface SecretState { set: boolean; updated_by?: string | null; updated_at?: string | null }
export interface IntervalSettings {
  patrol_seconds?: number; list_seconds?: number; records_seconds?: number; git_timeout_seconds?: number; write_wait_seconds?: number;
}
export interface GitHubSettings {
  app_id?: number; client_id?: string; slug?: string; api_url?: string; web_url?: string;
  private_key?: SecretState; client_secret?: SecretState;
}
export interface ForgejoSettings {
  url?: string; public_url?: string; organization?: string; client_id?: string; worker_user?: string;
  client_secret?: SecretState; worker_token?: SecretState;
}
export interface Settings {
  gitforge: GitForgeKind;
  webui_url: string;
  github?: GitHubSettings | null;
  forgejo?: ForgejoSettings | null;
  intervals: IntervalSettings;
  updated_by: string;
  updated_at: string;
}
export interface GitHubSettingsInput {
  app_id?: number; client_id?: string; slug?: string; private_key?: string; client_secret?: string; api_url?: string; web_url?: string;
}
export interface ForgejoSettingsInput {
  url?: string; public_url?: string; organization?: string; client_id?: string; client_secret?: string; worker_user?: string; worker_token?: string;
}
export interface GitForgePatch { github?: GitHubSettingsInput; forgejo?: ForgejoSettingsInput; intervals?: IntervalSettings }
/** prepared：スナップショットツリーがもうあるか。BE が GitForge に聞けなかったときは入らない（docs/webui/settings.md の「repository を足す」）。 */
export interface RegisteredRepository { name: string; gitforge_ref: string; directory?: string; prepared?: boolean }
export interface RecordKey { key_id: string; public_key: string; created_at: string; primary: boolean }

const enc = encodeURIComponent;
const jsonInit = (method: string, body?: unknown): RequestInit =>
  body === undefined ? { method } : { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };

export const getSettings = () => call<Settings>('/settings');
export const updateGitForgeSettings = (p: GitForgePatch) => call<Settings>('/settings/gitforge', jsonInit('PATCH', p));
export const listRegisteredRepositories = () => call<{ items: RegisteredRepository[] }>('/settings/repositories').then((r) => r.items);
/** directory が空なら送らない（git リポジトリ全体）。 */
export const registerRepository = (name: string, gitforgeRef: string, directory = '') =>
  call<RegisteredRepository>(`/settings/repositories/${enc(name)}`, jsonInit('PUT', directory ? { gitforge_ref: gitforgeRef, directory } : { gitforge_ref: gitforgeRef }));
export const unregisterRepository = async (name: string) => { await send(`/settings/repositories/${enc(name)}`, { method: 'DELETE' }); };
export const listRecordKeys = () => call<{ items: RecordKey[] }>('/settings/record-keys').then((r) => r.items);
export const createRecordKey = () => call<RecordKey>('/settings/record-keys', { method: 'POST' });

// --- メニューに出すか ---

/** 設定を開けるか。サインインしたあとに getSettings を呼んで決める。forbidden なら 'no'。 */
export type Access = 'unknown' | 'yes' | 'no';
let access: Access = 'unknown';
const listeners = new Set<() => void>();
const setAccess = (a: Access) => { access = a; listeners.forEach((f) => f()); };
export const getAccess = () => access;
export const useSettingsAccess = () => useSyncExternalStore((f) => { listeners.add(f); return () => { listeners.delete(f); }; }, getAccess);

/** getSettings を呼び、メニューに出すかを決める。読めたときだけ出す。forbidden のほかのエラーでは、決めずに残す。 */
export async function loadAccess(): Promise<Access> {
  try {
    await getSettings();
    setAccess('yes');
  } catch (e) {
    if (e instanceof Problem && e.code === 'forbidden') setAccess('no');
  }
  return access;
}
/** 設定の画面で getSettings を呼んだ結果を、メニューにも伝える。 */
export const noteAccess = (ok: boolean) => setAccess(ok ? 'yes' : 'no');

// --- install の URL ---

/** registerRepository の validation_failed の detail から、install の URL を取り出す。http(s) のものだけ。 */
export function installUrlFrom(detail: string): string | null {
  for (const m of detail.matchAll(/https?:\/\/[^\s"'<>（）「」、。]+/g)) {
    const raw = m[0].replace(/[.,;:)\]]+$/, '');
    try {
      const u = new URL(raw);
      if (u.protocol === 'https:' || u.protocol === 'http:') return u.toString();
    } catch { /* URL でない */ }
  }
  return null;
}

// --- GitForge のフォーム ---

/** フォームの値。どれも文字列で持つ。秘密は、入れたときだけ値がある。 */
export type GitHubForm = Record<'app_id' | 'client_id' | 'slug' | 'api_url' | 'web_url' | 'private_key' | 'client_secret', string>;
export type ForgejoForm = Record<'url' | 'public_url' | 'organization' | 'client_id' | 'worker_user' | 'client_secret' | 'worker_token', string>;
export type IntervalForm = Record<keyof Required<IntervalSettings>, string>;

export const INTERVAL_KEYS: (keyof IntervalForm)[] = ['patrol_seconds', 'list_seconds', 'records_seconds', 'git_timeout_seconds', 'write_wait_seconds'];
const GITHUB_PLAIN = ['client_id', 'slug', 'api_url', 'web_url'] as const;
const GITHUB_SECRETS = ['private_key', 'client_secret'] as const;
const FORGEJO_PLAIN = ['url', 'public_url', 'organization', 'client_id', 'worker_user'] as const;
const FORGEJO_SECRETS = ['client_secret', 'worker_token'] as const;

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v));

/** 今の設定から、フォームの最初の値を作る。秘密は空にする。 */
export function formsFrom(s: Settings): { github: GitHubForm; forgejo: ForgejoForm; intervals: IntervalForm } {
  const g = s.github ?? {}, f = s.forgejo ?? {};
  return {
    github: { app_id: str(g.app_id), client_id: str(g.client_id), slug: str(g.slug), api_url: str(g.api_url), web_url: str(g.web_url), private_key: '', client_secret: '' },
    forgejo: { url: str(f.url), public_url: str(f.public_url), organization: str(f.organization), client_id: str(f.client_id), worker_user: str(f.worker_user), client_secret: '', worker_token: '' },
    intervals: Object.fromEntries(INTERVAL_KEYS.map((k) => [k, str(s.intervals?.[k])])) as IntervalForm,
  };
}

/** 送る中身。変えた項目だけを入れる。秘密は、入れたときだけ入れる。構成は変えない（今の構成の側だけを送る）。
 * 何も変えていなければ、空の object を返す。数が正しくなければ、error を返す。 */
export function buildPatch(s: Settings, form: { github: GitHubForm; forgejo: ForgejoForm; intervals: IntervalForm }): { patch: GitForgePatch; error: string | null } {
  const patch: GitForgePatch = {};
  if (s.gitforge === 'github') {
    const cur = s.github ?? {}, f = form.github, out: GitHubSettingsInput = {};
    const id = f.app_id.trim();
    if (id !== str(cur.app_id)) {
      if (!/^[1-9][0-9]*$/.test(id)) return { patch: {}, error: 'App の ID は、正の整数にしてください。' };
      out.app_id = Number(id);
    }
    for (const k of GITHUB_PLAIN) { const v = f[k].trim(); if (v !== str(cur[k])) out[k] = v; }
    // 秘密鍵は、ファイルの中身をそのまま送る
    for (const k of GITHUB_SECRETS) { if (f[k].trim()) out[k] = k === 'private_key' ? f[k] : f[k].trim(); }
    if (Object.keys(out).length) patch.github = out;
  } else {
    const cur = s.forgejo ?? {}, f = form.forgejo, out: ForgejoSettingsInput = {};
    for (const k of FORGEJO_PLAIN) { const v = f[k].trim(); if (v !== str(cur[k])) out[k] = v; }
    for (const k of FORGEJO_SECRETS) { const v = f[k].trim(); if (v) out[k] = v; }
    if (Object.keys(out).length) patch.forgejo = out;
  }
  const iv: IntervalSettings = {};
  for (const k of INTERVAL_KEYS) {
    const v = form.intervals[k].trim();
    if (v === str(s.intervals?.[k])) continue;
    const n = Number(v);
    if (!v || !Number.isFinite(n) || n <= 0) return { patch: {}, error: '間隔と上限は、0 より大きい秒数にしてください。' };
    if (n !== s.intervals?.[k]) iv[k] = n;
  }
  if (Object.keys(iv).length) patch.intervals = iv;
  return { patch, error: null };
}

/** 外すときの確かめの文（docs/webui/settings.md の「外すときの確かめ」）。 */
export const unregisterConfirm = (name: string) => `${name} を外します。スナップショットツリーは読めなくなります。git リポジトリは消えません。`;
/** 鍵を作るときの確かめの文。 */
export const createKeyConfirm = '新しい記録の署名の鍵を作り、これから書く記録に使います。古い鍵の公開鍵は残すので、前の記録はそのまま確かめられます。';

// --- repository を足す流れ（docs/webui/settings.md の「repository を足す」） ---

// 名前と主文の決まり（openapi の RepositoryNameValue、docs/tree/README.md の「仮説」）。
// BE と同じ決まりである：主文は 50 字以下（internal/tree/check.go の MaxStatementChars）、
// 文字数は NFC に正規化したあとのコードポイント数（同じく CharCount）。BE が最後に確かめるので、ここは送る前の案内である
export const NAME_RE = /^[a-z0-9][a-z0-9-]{0,62}$/;
export const MAX_STATEMENT = 50;
/** 文字数は、NFC に正規化したあとのコードポイント数で数える */
export const charCount = (s: string) => [...s.normalize('NFC')].length;

export const validateName = (name: string): string | null =>
  NAME_RE.test(name) ? null : '名前は、英小文字・数字・ハイフンで、63 字まで（先頭はハイフン以外）にしてください。';

export function validateStatement(statement: string): string | null {
  if (!statement.trim()) return '根の仮説の主文を書いてください。';
  if (charCount(statement) > MAX_STATEMENT) return `主文は ${MAX_STATEMENT} 字以下にしてください。`;
  return null;
}

/** 足す流れの段。exists は、スナップショットツリーがもうあるので、最初の問いを飛ばしたもの。 */
export type AddStep = { at: 'repository' } | { at: 'question'; name: string } | { at: 'exists'; name: string } | { at: 'done'; name: string };

/** 登録したあとの段。prepared が分からないときは、最初の問いの段に進む（作るときに BE が確かめる）。 */
export const afterRegister = (r: RegisteredRepository): AddStep => (r.prepared ? { at: 'exists', name: r.name } : { at: 'question', name: r.name });

/** ?add=first のときの段：最初の問いがまだの repository があれば、その 2 段目。なければ 1 段目。 */
export function firstStep(items: RegisteredRepository[]): AddStep {
  const r = items.find((x) => x.prepared === false);
  return r ? { at: 'question', name: r.name } : { at: 'repository' };
}
