// サインイン（design/webui/auth.md、design/webui/session.md）。GitForge の OAuth2（認可コードと PKCE）。
// - state、code_verifier、戻り先の URL は sessionStorage に置き、戻ってきたら消す。
// - アクセストークンはメモリだけに持つ。リフレッシュトークンは受け取らない（BE が持ち、ブラウザには HttpOnly の cookie だけが来る）。
// - トークンがない・期限が近い・401（1 度は成功したあと）のときは、まず POST /v1/oauth/session で黙ってもらう。
//   セッションがないか切れていたときだけ、authorize に行く。

import { clearAllDrafts } from './files';
import { isOffline, onceOnline } from './online';

export interface OAuthConfig {
  authorize_url: string;
  client_id: string;
  redirect_uri: string;
  scope: string;
  gitforge?: 'forgejo' | 'github';
}

export type AuthState =
  | { kind: 'starting' }
  | { kind: 'signed-in'; token: string; expiresAt: number }
  | { kind: 'signed-out'; reason: SignedOutReason };

export type SignedOutReason = 'signed-out' | 'denied' | 'state-mismatch' | 'exchange-failed' | 'unavailable' | 'offline' | 'not-member' | 'not-configured';

const PENDING = 'lf-oauth:'; // + state → { verifier, returnTo }
const SIGNED_OUT = 'lf-signed-out';

let state: AuthState = { kind: 'starting' };
let verified = false; // このアクセストークンで、API が 1 度は成功したか
let retryTo: string | null = null; // サインインの画面のボタンで戻る先（コールバックで分かった、もとの URL）
// 期限のどれだけ前に、新しいアクセストークンをもらうか（送っている途中の要求が 401 にならないように）
export const EXPIRY_MARGIN_MS = 60_000;
// セッションの BE が答えなかったときに、もらい直すまで待つ時間
export const RENEW_RETRY_MS = 30_000;
const listeners = new Set<() => void>();

export const getAuth = () => state;
export const subscribe = (f: () => void) => { listeners.add(f); return () => { listeners.delete(f); }; };
function set(s: AuthState) { state = s; listeners.forEach((f) => f()); }

// --- PKCE と state ---

export function base64url(bytes: Uint8Array): string {
  let s = '';
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function randomString(n = 32): string {
  return base64url(crypto.getRandomValues(new Uint8Array(n)));
}

export async function codeChallenge(verifier: string): Promise<string> {
  const d = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier));
  return base64url(new Uint8Array(d));
}

export function authorizeURL(cfg: OAuthConfig, st: string, challenge: string): string {
  const u = new URL(cfg.authorize_url);
  u.search = new URLSearchParams({
    response_type: 'code', client_id: cfg.client_id, redirect_uri: cfg.redirect_uri, scope: cfg.scope,
    state: st, code_challenge: challenge, code_challenge_method: 'S256',
  }).toString();
  return u.toString();
}

// 戻り先は、同じオリジンのパスだけにする（外の URL に飛ばされないように）
export function safeReturnTo(v: string | undefined): string {
  if (!v || !v.startsWith('/') || v.startsWith('//') || v.startsWith('/oauth/callback')) return '/';
  return v;
}

// --- 流れ ---

let config: OAuthConfig | null = null;

async function loadConfig(): Promise<OAuthConfig | null> {
  if (config) return config;
  const r = await fetch('/v1/oauth/config');
  if (!r.ok) return null;
  config = (await r.json()) as OAuthConfig;
  return config;
}

/** authorize に行く。戻り先は今の URL。 */
export async function signIn(returnTo = location.pathname + location.search): Promise<void> {
  sessionStorage.removeItem(SIGNED_OUT);
  const cfg = await loadConfig();
  if (!cfg) { set({ kind: 'signed-out', reason: 'not-configured' }); return; }
  const st = randomString();
  const verifier = randomString(48);
  sessionStorage.setItem(PENDING + st, JSON.stringify({ verifier, returnTo: safeReturnTo(returnTo) }));
  location.assign(authorizeURL(cfg, st, await codeChallenge(verifier)));
}

/** 起動したとき。コールバックなら交換する。そうでなければ、セッションからアクセストークンをもらい、なければ authorize に行く。 */
export async function start(): Promise<void> {
  if (location.pathname === '/oauth/callback') { await callback(); return; }
  // authorize に行ったまま戻らなかったときの、使われずに残ったものを消す
  clearPending();
  if (sessionStorage.getItem(SIGNED_OUT)) { set({ kind: 'signed-out', reason: 'signed-out' }); return; }
  verified = false;
  const r = await renew();
  if (r === 'none') await signIn();
  else if (r === 'unavailable') set({ kind: 'signed-out', reason: 'unavailable' });
  else if (r === 'offline') {
    // ホーム画面から、つながらないところで開いたとき。つながったら、もう一度始める（design/webui/pwa.md）
    set({ kind: 'signed-out', reason: 'offline' });
    onceOnline(() => { if (state.kind === 'signed-out' && state.reason === 'offline') { set({ kind: 'starting' }); void start(); } });
  }
}

const postJSON = (path: string) => fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}', credentials: 'same-origin' });

/**
 * セッション（HttpOnly の cookie）から、アクセストークンをもらう（design/webui/session.md）。
 * none：セッションがないか切れた（authorize に行く）。unavailable：BE が答えない。offline：端末がつながっていない。
 */
async function renew(): Promise<'ok' | 'none' | 'unavailable' | 'offline'> {
  let r: Response;
  try { r = await postJSON('/v1/oauth/session'); } catch { return isOffline() ? 'offline' : 'unavailable'; }
  // 403 は、設定の WebUI の URL と違うホストで開いたとき（Origin が合わない）。authorize に行けば、設定のホストに戻る
  if (r.status === 401 || r.status === 403) return 'none';
  if (!r.ok) return 'unavailable';
  const t = (await r.json()) as { access_token: string; expires_in: number };
  set({ kind: 'signed-in', token: t.access_token, expiresAt: Date.now() + t.expires_in * 1000 });
  scheduleExpiry();
  return 'ok';
}

async function callback(): Promise<void> {
  const q = new URLSearchParams(location.search);
  const code = q.get('code'), st = q.get('state'), error = q.get('error');
  // 交換の前に、URL から code と state を消す（履歴や画面の共有に残さない）
  history.replaceState(null, '', '/oauth/callback');
  const pending = st ? sessionStorage.getItem(PENDING + st) : null;
  clearPending();
  const parsed = pending ? (JSON.parse(pending) as { verifier: string; returnTo: string }) : null;
  retryTo = parsed ? safeReturnTo(parsed.returnTo) : null;
  // 自動で行き直さない（行き来が止まらなくなるため）
  if (error) { set({ kind: 'signed-out', reason: 'denied' }); return; }
  if (!code || !parsed) { set({ kind: 'signed-out', reason: 'state-mismatch' }); return; }
  const { verifier, returnTo } = parsed;
  const r = await fetch('/v1/oauth/token', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, code_verifier: verifier }),
  });
  if (!r.ok) { set({ kind: 'signed-out', reason: r.status === 503 ? 'unavailable' : 'exchange-failed' }); return; }
  const t = (await r.json()) as { access_token: string; expires_in: number };
  verified = false;
  history.replaceState(null, '', safeReturnTo(returnTo));
  set({ kind: 'signed-in', token: t.access_token, expiresAt: Date.now() + t.expires_in * 1000 });
  scheduleExpiry();
}

let expiryTimer: ReturnType<typeof setTimeout> | undefined;
function scheduleExpiry() {
  clearTimeout(expiryTimer);
  if (state.kind !== 'signed-in') return;
  // 期限の前に、セッションから新しいアクセストークンをもらう。セッションが切れていたら、authorize に行き、今の URL に戻る
  expiryTimer = setTimeout(() => { void renewOrRetry(); }, Math.max(0, state.expiresAt - EXPIRY_MARGIN_MS - Date.now()));
}

async function renewOrRetry() {
  const r = await renew();
  if (r === 'none') { await goAuthorize(); return; }
  // BE が答えなければ、しばらくして、もう 1 度
  if (r === 'unavailable' || r === 'offline') { clearTimeout(expiryTimer); expiryTimer = setTimeout(() => { void renewOrRetry(); }, RENEW_RETRY_MS); }
}

async function goAuthorize() {
  clearTimeout(expiryTimer);
  set({ kind: 'starting' });
  await signIn();
}

/** アクセストークンの残りの時間（ミリ秒）。サインインしていなければ 0 */
export function remainingMs(): number {
  return state.kind === 'signed-in' ? state.expiresAt - Date.now() : 0;
}

/**
 * 今すぐ新しいアクセストークンをもらう（回答を送る直前に、残りが少ないとき）。
 * もらえたら true。セッションが切れていたら authorize に行き（戻り先は今の URL）、false を返す。
 */
export async function reauthorize(): Promise<boolean> {
  const r = await renew();
  if (r === 'ok') return true;
  if (r === 'none') await goAuthorize();
  return false;
}

/**
 * サインアウト。BE のセッションと cookie を消してから、メモリのトークンと回答の書きかけを消す。
 * BE が消せなかったら false を返し、サインインしたままにする（押し直せるように）。
 * なぜ：セッションが残ると、共用の端末で、次の人が新しいタブを開いたときに、前の人としてサインインするから。
 */
export async function signOut(): Promise<boolean> {
  let ok = false;
  try { ok = (await postJSON('/v1/oauth/signout')).ok; } catch { /* 下で false を返す */ }
  if (!ok) return false;
  clearTimeout(expiryTimer);
  // 回答の書きかけも消す（同じタブで、ほかの人がサインインすることがあるため）
  clearAllDrafts();
  sessionStorage.setItem(SIGNED_OUT, '1');
  set({ kind: 'signed-out', reason: 'signed-out' });
  return true;
}

const verifiedListeners = new Set<() => void>();

/** API がアクセストークンを受け付けた。 */
export function markVerified() {
  if (verified) return;
  verified = true;
  verifiedListeners.forEach((f) => f());
}

/** このアクセストークンで、API が 1 度は成功したか */
export const isVerified = () => verified;

/** API が、今のアクセストークンを初めて受け付けたときに f を呼ぶ。戻り値で、やめる。 */
export function onVerified(f: () => void): () => void {
  verifiedListeners.add(f);
  return () => { verifiedListeners.delete(f); };
}

/**
 * API が 401 を返した。1 度は成功したあとなら、セッションから新しいトークンをもらう。
 * もらえないか、同じトークンしか返らなければ（GitForge で取り消されたなど）、authorize に行き直す。
 * まだ 1 度も成功していなければ、organization にいないと示す。
 */
export function onUnauthorized() {
  if (state.kind !== 'signed-in') return;
  if (verified) {
    if (renewing) return;
    const before = state.token;
    renewing = true;
    void renew().then(async (r) => {
      renewing = false;
      if (r === 'none' || (r === 'ok' && getToken() === before)) await goAuthorize();
    });
    return;
  }
  clearTimeout(expiryTimer);
  set({ kind: 'signed-out', reason: 'not-member' });
}

let renewing = false;
const getToken = () => (state.kind === 'signed-in' ? state.token : null);

function clearPending() {
  for (let i = sessionStorage.length - 1; i >= 0; i--) {
    const k = sessionStorage.key(i);
    if (k?.startsWith(PENDING)) sessionStorage.removeItem(k);
  }
}

/** サインインの画面のボタンで戻る先。コールバックで分かったもとの URL、なければ今の URL。 */
export function retryTarget(): string {
  if (retryTo) return retryTo;
  return location.pathname === '/oauth/callback' ? '/' : safeReturnTo(location.pathname + location.search);
}

export function forgejoOrigin(): string | null {
  return config ? new URL(config.authorize_url).origin : null;
}

/** サインインする GitForge の表示名。設定を読む前は Forgejo。 */
export function gitforgeName(): string {
  return config?.gitforge === 'github' ? 'GitHub' : 'Forgejo';
}
