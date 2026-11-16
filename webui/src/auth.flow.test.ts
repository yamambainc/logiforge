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
function env(path: string) {
  const u = new URL(path, 'https://lf.example');
  assigned = [];
  const loc = { pathname: u.pathname, search: u.search, origin: u.origin, assign: (x: string) => assigned.push(x) };
  vi.stubGlobal('location', loc);
  vi.stubGlobal('history', { replaceState: (_: unknown, __: string, to: string) => { const n = new URL(to, u.origin); loc.pathname = n.pathname; loc.search = n.search; } });
  vi.stubGlobal('sessionStorage', new MemStorage());
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/v1/oauth/config') return new Response(JSON.stringify({ authorize_url: 'https://forge.example/login/oauth/authorize', client_id: 'c', redirect_uri: 'https://lf.example/oauth/callback', scope: 's' }));
    if (url === '/v1/oauth/token') return new Response(JSON.stringify({ access_token: 'at', token_type: 'bearer', expires_in: 3600 }));
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

describe('起動', () => {
  it('トークンがなければ、今の URL を戻り先にして authorize に行く。残っていた lf-oauth:* は消す', async () => {
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

  it('1 度は成功したあとの 401 では、サインインの画面を出さずに authorize に行く', async () => {
    // authorize への移動は、PKCE の digest（本物の非同期）のあとなので、この試しだけ本物の時計で待つ。期限（59 分後）は来ない
    vi.useRealTimers();
    const a = await signedIn();
    a.markVerified();
    a.onUnauthorized();
    await vi.waitFor(() => expect(assigned).toHaveLength(1));
    await new Promise((r) => setTimeout(r, 50));
    expect(assigned).toHaveLength(1);
  });

  it('期限の 1 分前に authorize に行くように、時計を仕掛ける', async () => {
    const spy = vi.spyOn(globalThis, 'setTimeout');
    const a = await signedIn();
    const delays = spy.mock.calls.map((c) => c[1]);
    expect(delays).toContain(3600_000 - a.EXPIRY_MARGIN_MS);
  });
});
