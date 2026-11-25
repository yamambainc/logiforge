// サインインの流れの分かれ道を、ブラウザなしで確かめる（location、history、sessionStorage、fetch を差し替える）。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

class MemStorage {
  m = new Map<string, string>();
  get length() { return this.m.size; }
  key(i: number) { return [...this.m.keys()][i] ?? null; }
  getItem(k: string) { return this.m.get(k) ?? null; }
  setItem(k: string, v: string) { this.m.set(k, v); }
  removeItem(k: string) { this.m.delete(k); }
}

let assigned: string[];
// POST /v1/oauth/session の応答（セッションの cookie があるときの BE）。既定はセッションなし（401）
let session: () => Response;
let sessionCalls: number;
function env(path: string) {
  session = () => new Response('{}', { status: 401 });
  sessionCalls = 0;
  const u = new URL(path, 'https://lf.example');
  assigned = [];
  const loc = { pathname: u.pathname, search: u.search, origin: u.origin, assign: (x: string) => assigned.push(x) };
  vi.stubGlobal('location', loc);
  vi.stubGlobal('history', { replaceState: (_: unknown, __: string, to: string) => { const n = new URL(to, u.origin); loc.pathname = n.pathname; loc.search = n.search; } });
  vi.stubGlobal('sessionStorage', new MemStorage());
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/v1/oauth/config') return new Response(JSON.stringify({ authorize_url: 'https://forge.example/login/oauth/authorize', client_id: 'c', redirect_uri: 'https://lf.example/oauth/callback', scope: 's' }));
    if (url === '/v1/oauth/token') return new Response(JSON.stringify({ access_token: 'at', token_type: 'bearer', expires_in: 3600 }));
    if (url === '/v1/oauth/session') { sessionCalls++; return session(); }
    if (url === '/v1/oauth/signout') return new Response(null, { status: 204 });
    return new Response('{}', { status: 404 });
  }));
  return loc;
}

async function load() {
  vi.resetModules();
  return import('./auth');
}

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals(); });

const tokenResp = (t: string, sec = 3600) => () => new Response(JSON.stringify({ access_token: t, token_type: 'bearer', expires_in: sec }));

describe('起動', () => {
  it('セッションがあれば、authorize に行かずに、そのアクセストークンでサインインする（再読み込み、新しいタブ）', async () => {
    env('/r/q/tree?node=h-1');
    session = tokenResp('at-s');
    const a = await load();
    await a.start();
    expect(a.getAuth()).toMatchObject({ kind: 'signed-in', token: 'at-s' });
    expect(assigned).toHaveLength(0);
    expect(location.pathname + location.search).toBe('/r/q/tree?node=h-1');
  });

  it('BE が答えなければ、authorize に行かずに、しばらくしてからと示す', async () => {
    env('/');
    session = () => new Response('{}', { status: 503 });
    const a = await load();
    await a.start();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'unavailable' });
    expect(assigned).toHaveLength(0);
  });

  it('端末がつながっていなければ、つながっていないと示し、つながったら、もう一度始める', async () => {
    env('/r/q/tree');
    const on: (() => void)[] = [];
    vi.stubGlobal('window', { addEventListener: (t: string, f: () => void) => { if (t === 'online') on.push(f); } });
    const nav = { onLine: false };
    vi.stubGlobal('navigator', nav);
    session = () => { throw new TypeError('Failed to fetch'); };
    const a = await load();
    await a.start();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'offline' });
    expect(assigned).toHaveLength(0);
    nav.onLine = true;
    session = tokenResp('at-on');
    on.forEach((f) => f());
    await vi.waitFor(() => expect(a.getAuth()).toMatchObject({ kind: 'signed-in', token: 'at-on' }));
  });

  it('つながった直後に要求が通らなくても、間をあけてやり直す。ずっと通らなければ unavailable', async () => {
    env('/');
    const on: (() => void)[] = [];
    vi.stubGlobal('window', { addEventListener: (t: string, f: () => void) => { if (t === 'online') on.push(f); } });
    const nav = { onLine: false };
    vi.stubGlobal('navigator', nav);
    session = () => { throw new TypeError('Failed to fetch'); };
    const a = await load();
    await a.start();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'offline' });
    // つながったが、まだ通らない（onLine は true）
    nav.onLine = true;
    on.forEach((f) => f());
    await vi.advanceTimersByTimeAsync(0);
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'offline' });
    await vi.advanceTimersByTimeAsync(1_000);
    session = tokenResp('at-late');
    await vi.advanceTimersByTimeAsync(2_000);
    expect(a.getAuth()).toMatchObject({ kind: 'signed-in', token: 'at-late' });
    expect(assigned).toHaveLength(0);

    // ずっと通らなければ、1+2+4+8 秒のあとに unavailable（ボタンで押し直せる）
    env('/');
    on.length = 0;
    vi.stubGlobal('window', { addEventListener: (t: string, f: () => void) => { if (t === 'online') on.push(f); } });
    nav.onLine = false;
    vi.stubGlobal('navigator', nav);
    session = () => { throw new TypeError('Failed to fetch'); };
    const b = await load();
    await b.start();
    nav.onLine = true;
    on.forEach((f) => f());
    await vi.advanceTimersByTimeAsync(14_999);
    expect(b.getAuth()).toEqual({ kind: 'signed-out', reason: 'offline' });
    await vi.advanceTimersByTimeAsync(1);
    expect(b.getAuth()).toEqual({ kind: 'signed-out', reason: 'unavailable' });
  });

  it('つながっているのに BE が答えなければ（fetch が失敗）、unavailable', async () => {
    env('/');
    vi.stubGlobal('navigator', { onLine: true });
    session = () => { throw new TypeError('Failed to fetch'); };
    const a = await load();
    await a.start();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'unavailable' });
  });

  it('サインアウトしたタブでは、セッションを見ずにサインインの画面', async () => {
    env('/');
    sessionStorage.setItem('lf-signed-out', '1');
    const a = await load();
    await a.start();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'signed-out' });
    expect(sessionCalls).toBe(0);
  });

  it('セッションがなければ（401）、今の URL を戻り先にして authorize に行く。残っていた lf-oauth:* は消す', async () => {
    env('/r/q/tree?node=h-1');
    sessionStorage.setItem('lf-oauth:old', '{}');
    const a = await load();
    await a.start();
    expect(assigned).toHaveLength(1);
    const st = new URL(assigned[0]).searchParams.get('state')!;
    expect(sessionStorage.getItem('lf-oauth:old')).toBeNull();
    expect(JSON.parse(sessionStorage.getItem('lf-oauth:' + st)!).returnTo).toBe('/r/q/tree?node=h-1');
  });
});

describe('コールバック', () => {
  it('error が付いて戻ったら、交換せずにサインインの画面（自動で行き直さない）', async () => {
    env('/oauth/callback?error=access_denied&state=x');
    sessionStorage.setItem('lf-oauth:x', JSON.stringify({ verifier: 'v', returnTo: '/r/q/tree' }));
    const a = await load();
    await a.start();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'denied' });
    expect(assigned).toHaveLength(0);
    expect(fetch).not.toHaveBeenCalledWith('/v1/oauth/token', expect.anything());
    expect(location.search).toBe('');
    // ボタンで戻る先は、もとの URL
    expect(a.retryTarget()).toBe('/r/q/tree');
  });

  it('state が合わなければ、交換せずにサインインの画面', async () => {
    env('/oauth/callback?code=c&state=unknown');
    const a = await load();
    await a.start();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'state-mismatch' });
    expect(assigned).toHaveLength(0);
    expect(fetch).not.toHaveBeenCalledWith('/v1/oauth/token', expect.anything());
  });

  it('交換できたら、メモリにトークンを置き、戻り先に戻る。URL から code を消す', async () => {
    env('/oauth/callback?code=c&state=s1');
    sessionStorage.setItem('lf-oauth:s1', JSON.stringify({ verifier: 'v', returnTo: '/r/q/tree?node=h-1' }));
    const a = await load();
    await a.start();
    expect(a.getAuth().kind).toBe('signed-in');
    expect(location.pathname + location.search).toBe('/r/q/tree?node=h-1');
    expect(sessionStorage.length).toBe(0);
  });

  it('BE が 503 なら、しばらくしてからと示す', async () => {
    env('/oauth/callback?code=c&state=s1');
    sessionStorage.setItem('lf-oauth:s1', JSON.stringify({ verifier: 'v', returnTo: '/' }));
    vi.mocked(fetch).mockImplementation(async () => new Response('{}', { status: 503 }));
    const a = await load();
    await a.start();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'unavailable' });
  });
});

describe('401', () => {
  async function signedIn() {
    env('/oauth/callback?code=c&state=s1');
    sessionStorage.setItem('lf-oauth:s1', JSON.stringify({ verifier: 'v', returnTo: '/r/q/tree' }));
    const a = await load();
    await a.start();
    return a;
  }

  it('交換の直後（まだ API が 1 度も成功していない）の 401 では行き直さず、organization にいないと示す', async () => {
    const a = await signedIn();
    a.onUnauthorized();
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'not-member' });
    expect(assigned).toHaveLength(0);
  });

  it('1 度は成功したあとの 401 では、まずセッションから新しいトークンをもらう。もらえれば authorize に行かない', async () => {
    vi.useRealTimers();
    const a = await signedIn();
    a.markVerified();
    session = tokenResp('at-2');
    a.onUnauthorized();
    await vi.waitFor(() => expect(a.getAuth()).toMatchObject({ kind: 'signed-in', token: 'at-2' }));
    expect(assigned).toHaveLength(0);
  });

  it('1 度は成功したあとの 401 で、セッションも切れていたら、サインインの画面を出さずに authorize に行く', async () => {
    // authorize への移動は、PKCE の digest（本物の非同期）のあとなので、この試しだけ本物の時計で待つ。期限（59 分後）は来ない
    vi.useRealTimers();
    const a = await signedIn();
    a.markVerified();
    a.onUnauthorized();
    await vi.waitFor(() => expect(assigned).toHaveLength(1));
    await new Promise((r) => setTimeout(r, 50));
    expect(assigned).toHaveLength(1);
  });

  it('セッションが同じトークンしか返さなければ（GitForge で取り消された）、authorize に行く', async () => {
    vi.useRealTimers();
    const a = await signedIn();
    a.markVerified();
    session = tokenResp('at');
    a.onUnauthorized();
    await vi.waitFor(() => expect(assigned).toHaveLength(1));
  });

  it('期限の 1 分前に、セッションから黙って新しいトークンをもらう。authorize には行かない', async () => {
    const spy = vi.spyOn(globalThis, 'setTimeout');
    const a = await signedIn();
    const delays = spy.mock.calls.map((c) => c[1]);
    expect(delays).toContain(3600_000 - a.EXPIRY_MARGIN_MS);
    session = tokenResp('at-2');
    await vi.advanceTimersByTimeAsync(3600_000 - a.EXPIRY_MARGIN_MS);
    expect(a.getAuth()).toMatchObject({ kind: 'signed-in', token: 'at-2' });
    expect(assigned).toHaveLength(0);
  });

  it('期限の前に BE が答えなければ、しばらくしてもう 1 度もらう', async () => {
    const a = await signedIn();
    session = () => new Response('{}', { status: 503 });
    await vi.advanceTimersByTimeAsync(3600_000 - a.EXPIRY_MARGIN_MS);
    expect(sessionCalls).toBe(1);
    session = tokenResp('at-3');
    await vi.advanceTimersByTimeAsync(a.RENEW_RETRY_MS);
    expect(a.getAuth()).toMatchObject({ kind: 'signed-in', token: 'at-3' });
  });

  it('回答の前の reauthorize：もらえれば true（移動しない）、セッションが切れていれば authorize に行って false', async () => {
    vi.useRealTimers();
    const a = await signedIn();
    session = tokenResp('at-2');
    expect(await a.reauthorize()).toBe(true);
    expect(assigned).toHaveLength(0);
    session = () => new Response('{}', { status: 401 });
    expect(await a.reauthorize()).toBe(false);
    expect(assigned).toHaveLength(1);
  });
});

describe('サインアウト', () => {
  it('BE のセッションを消す要求を送り、サインインの画面を出す', async () => {
    env('/');
    session = tokenResp('at-s');
    const a = await load();
    await a.start();
    expect(await a.signOut()).toBe(true);
    expect(a.getAuth()).toEqual({ kind: 'signed-out', reason: 'signed-out' });
    expect(fetch).toHaveBeenCalledWith('/v1/oauth/signout', expect.objectContaining({ method: 'POST' }));
    expect(sessionStorage.getItem('lf-signed-out')).toBe('1');
  });

  it('BE がセッションを消せなければ、サインインしたままにして false（押し直せる）', async () => {
    env('/');
    session = tokenResp('at-s');
    const a = await load();
    await a.start();
    vi.mocked(fetch).mockImplementation(async () => new Response('{}', { status: 503 }));
    expect(await a.signOut()).toBe(false);
    expect(a.getAuth()).toMatchObject({ kind: 'signed-in' });
    expect(sessionStorage.getItem('lf-signed-out')).toBeNull();
  });
});
