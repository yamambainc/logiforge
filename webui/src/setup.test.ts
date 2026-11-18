// セットアップの API の呼び方と、manifest flow と、続きの段の決め方を、ブラウザなしで確かめる。
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { SetupState } from './setup';

class MemStorage {
  m = new Map<string, string>();
  get length() { return this.m.size; }
  key(i: number) { return [...this.m.keys()][i] ?? null; }
  getItem(k: string) { return this.m.get(k) ?? null; }
  setItem(k: string, v: string) { this.m.set(k, v); }
  removeItem(k: string) { this.m.delete(k); }
}

type Handler = (url: string, init?: RequestInit) => Response | Promise<Response>;
let events: string[];
function env(path: string, handler: Handler = () => new Response('{}')) {
  const u = new URL(path, 'https://lf.example');
  events = [];
  const loc = { pathname: u.pathname, search: u.search, origin: u.origin, assign: vi.fn() };
  vi.stubGlobal('location', loc);
  vi.stubGlobal('history', { replaceState: (_: unknown, __: string, to: string) => {
    events.push('replace ' + to);
    const n = new URL(to, u.origin); loc.pathname = n.pathname; loc.search = n.search;
  } });
  vi.stubGlobal('sessionStorage', new MemStorage());
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => { events.push('fetch ' + url); return handler(url, init); }));
  return loc;
}
const problem = (status: number, code: string, detail = '') =>
  new Response(JSON.stringify({ code, detail }), { status, headers: { 'Content-Type': 'application/problem+json' } });
const headersOf = (i: number) => new Headers((vi.mocked(fetch).mock.calls[i][1] as RequestInit | undefined)?.headers);

async function load() {
  vi.resetModules();
  return import('./setup');
}

afterEach(() => { vi.unstubAllGlobals(); });

describe('ヘッダー', () => {
  it('getSetup はコードを付けない。ほかはコードを付ける', async () => {
    env('/setup', (url) => url === '/v1/setup' ? new Response(JSON.stringify({ done: false, step: 'gitforge', gitforge: null, github_app: null })) : new Response('{}'));
    const s = await load();
    s.setCode('ABC');
    await s.getSetup();
    await s.setSetupGitForge('github');
    expect(headersOf(0).has('Logiforge-Setup-Code')).toBe(false);
    expect(headersOf(1).get('Logiforge-Setup-Code')).toBe('ABC');
    expect(JSON.parse(vi.mocked(fetch).mock.calls[1][1]!.body as string)).toEqual({ gitforge: 'github', webui_url: 'https://lf.example' });
  });

  it('finishSetup は、コードとサインインのトークンの両方を付ける', async () => {
    env('/oauth/callback?code=c&state=s1', (url) => {
      if (url === '/v1/oauth/token') return new Response(JSON.stringify({ access_token: 'at', token_type: 'bearer', expires_in: 3600 }));
      return new Response(JSON.stringify({ done: true, step: null, gitforge: 'github', github_app: null }));
    });
    sessionStorage.setItem('lf-oauth:s1', JSON.stringify({ verifier: 'v', returnTo: '/setup' }));
    const s = await load();
    const auth = await import('./auth');
    await auth.start();
    expect(location.pathname).toBe('/setup');
    s.setCode('ABC');
    sessionStorage.setItem('lf-setup-repository', '1');
    await s.finishSetup();
    const h = headersOf(1);
    expect(h.get('Logiforge-Setup-Code')).toBe('ABC');
    expect(h.get('Authorization')).toBe('Bearer at');
    // 終わったら、置いたものを消す
    expect(s.getCode()).toBeNull();
    expect(sessionStorage.getItem('lf-setup-repository')).toBeNull();
  });

  it('コードがなければ、送らずに unauthenticated', async () => {
    env('/setup');
    const s = await load();
    await expect(s.checkSetupCode()).rejects.toMatchObject({ code: 'unauthenticated' });
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('エラー', () => {
  it('unauthenticated なら、コードを消す', async () => {
    env('/setup', () => problem(401, 'unauthenticated'));
    const s = await load();
    s.setCode('OLD');
    await expect(s.setSetupRepository({ name: 'q', gitforge_ref: 'o/q' })).rejects.toMatchObject({ status: 401, code: 'unauthenticated' });
    expect(s.getCode()).toBeNull();
  });

  it('1 分に 10 回を越えて断られたときは、コードを残す', async () => {
    env('/setup', () => problem(401, 'unauthenticated', '送り元の IP ごとに、1 分に 10 回までです'));
    const s = await load();
    s.setCode('ABC');
    const e = await s.checkSetupCode().catch((x) => x);
    expect(s.isRateLimited(e)).toBe(true);
    expect(s.getCode()).toBe('ABC');
  });

  it('validation_failed では、コードを残し、detail を渡す', async () => {
    env('/setup', () => problem(400, 'validation_failed', 'worker のトークンで organization を読めない'));
    const s = await load();
    s.setCode('ABC');
    await expect(s.setSetupForgejo({ url: 'u', public_url: 'u', organization: 'o', client_id: 'c', client_secret: 's', worker_user: 'w', worker_token: 't' }))
      .rejects.toMatchObject({ code: 'validation_failed', detail: 'worker のトークンで organization を読めない' });
    expect(s.getCode()).toBe('ABC');
  });

  it('setup_done なら、置いたものを消す', async () => {
    env('/setup', () => problem(409, 'setup_done'));
    const s = await load();
    s.setCode('ABC');
    await expect(s.checkSetupCode()).rejects.toMatchObject({ code: 'setup_done' });
    expect(s.getCode()).toBeNull();
  });
});

describe('manifest flow', () => {
  it('送り先に state を付け、state を sessionStorage に置く', async () => {
    env('/setup');
    const s = await load();
    expect(s.manifestAction('https://github.com/organizations/o/settings/apps/new', 'st')).toBe('https://github.com/organizations/o/settings/apps/new?state=st');
    const made: Record<string, unknown>[] = [];
    const el = (tag: string) => { const e: Record<string, unknown> = { tag, style: {}, children: [] as unknown[], appendChild(c: unknown) { (e.children as unknown[]).push(c); }, submit: vi.fn() }; made.push(e); return e; };
    const doc = { createElement: el, body: { appendChild: vi.fn() } } as unknown as Document;
    s.submitManifest({ manifest: { name: 'lf' }, post_url: 'https://github.com/settings/apps/new' }, doc);
    const [form, input] = made;
    const st = sessionStorage.getItem('lf-setup-manifest-state')!;
    expect(st).toBeTruthy();
    expect(form.method).toBe('post');
    expect(form.action).toBe('https://github.com/settings/apps/new?state=' + st);
    expect(input).toMatchObject({ type: 'hidden', name: 'manifest', value: '{"name":"lf"}' });
    expect(form.submit).toHaveBeenCalled();
  });

  it('state が合わなければ、code を送らない。URL から code は消す', async () => {
    env('/setup/github/callback?code=gh&state=other');
    const s = await load();
    s.setCode('ABC');
    sessionStorage.setItem('lf-setup-manifest-state', 'mine');
    expect(await s.handleManifestCallback()).toEqual({ ok: false, reason: 'state-mismatch' });
    expect(fetch).not.toHaveBeenCalled();
    expect(location.search).toBe('');
    expect(sessionStorage.getItem('lf-setup-manifest-state')).toBeNull();
  });

  it('state を置いていなければ、code を送らない', async () => {
    env('/setup/github/callback?code=gh&state=x');
    const s = await load();
    s.setCode('ABC');
    expect(await s.handleManifestCallback()).toEqual({ ok: false, reason: 'state-mismatch' });
    expect(fetch).not.toHaveBeenCalled();
  });

  it('state が合えば、URL から code を消してから、code を送る', async () => {
    env('/setup/github/callback?code=gh&state=mine', () => new Response(JSON.stringify({ slug: 'lf', install_url: 'https://github.com/apps/lf/installations/new' })));
    const s = await load();
    s.setCode('ABC');
    sessionStorage.setItem('lf-setup-manifest-state', 'mine');
    const r = await s.handleManifestCallback();
    expect(r).toEqual({ ok: true, app: { slug: 'lf', install_url: 'https://github.com/apps/lf/installations/new' } });
    expect(events).toEqual(['replace /setup', 'fetch /v1/setup/github/conversion']);
    expect(JSON.parse(vi.mocked(fetch).mock.calls[0][1]!.body as string)).toEqual({ code: 'gh' });
    expect(headersOf(0).get('Logiforge-Setup-Code')).toBe('ABC');
  });
});

describe('続きの段', () => {
  const st = (p: Partial<SetupState>): SetupState => ({ done: false, step: 'gitforge', gitforge: null, github_app: null, ...p });
  const app = { slug: 'lf', install_url: 'https://github.com/apps/lf/installations/new' };

  it('コードがなければ、コードの段', async () => {
    const s = await load();
    expect(s.continueStep(st({ step: 'signin', gitforge: 'github', github_app: app }), false)).toBe('code');
  });

  it('構成を選ぶ前は、構成の段', async () => {
    const s = await load();
    expect(s.continueStep(st({}), true)).toBe('gitforge');
  });

  it('GitHub：App、install、repository、サインインの順', async () => {
    const s = await load();
    expect(s.continueStep(st({ step: 'github_app', gitforge: 'github' }), true)).toBe('github_app');
    const signin = st({ step: 'signin', gitforge: 'github', github_app: app });
    expect(s.continueStep(signin, true)).toBe('install');
    expect(s.continueStep(signin, true, { installed: true, repository: false })).toBe('repository');
    expect(s.continueStep(signin, true, { installed: true, repository: true })).toBe('signin');
  });

  it('Forgejo：Forgejo の段、サインインの順', async () => {
    const s = await load();
    expect(s.continueStep(st({ step: 'forgejo', gitforge: 'forgejo' }), true)).toBe('forgejo');
    expect(s.continueStep(st({ step: 'signin', gitforge: 'forgejo' }), true)).toBe('signin');
  });
});

describe('起動', () => {
  const notDone = JSON.stringify({ done: false, step: 'gitforge', gitforge: null, github_app: null });

  it('終わっていなければ、authorize に行かず、どの URL でもセットアップの画面', async () => {
    env('/r/q/tree', () => new Response(notDone));
    const { boot } = await import('./setup-boot');
    const start = vi.fn(async () => {});
    expect((await boot(start)).kind).toBe('setup');
    expect(start).not.toHaveBeenCalled();
    expect(location.pathname).toBe('/setup');
  });

  it('終わっていなくても、サインインから戻ったときは交換する', async () => {
    env('/oauth/callback?code=c&state=s', () => new Response(notDone));
    const { boot } = await import('./setup-boot');
    const start = vi.fn(async () => {});
    expect((await boot(start)).kind).toBe('setup');
    expect(start).toHaveBeenCalled();
  });

  it('終わっていれば、ふだんどおり。getSetup がつながらなくても、ふだんどおり', async () => {
    env('/setup', () => new Response(JSON.stringify({ done: true, step: null, gitforge: 'github', github_app: null })));
    let { boot } = await import('./setup-boot');
    let start = vi.fn(async () => {});
    expect((await boot(start)).kind).toBe('app');
    expect(start).toHaveBeenCalled();
    expect(location.pathname).toBe('/setup');

    env('/', () => { throw new TypeError('network'); });
    vi.resetModules();
    ({ boot } = await import('./setup-boot'));
    start = vi.fn(async () => {});
    expect((await boot(start)).kind).toBe('app');
    expect(start).toHaveBeenCalled();
  });
});
