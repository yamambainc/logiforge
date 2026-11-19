// 設定の API の呼び方と、送る中身の作り方と、メニューに出すかを、ブラウザなしで確かめる。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { match, settingsPath } from './router';
import type { Settings } from './settings';
import { afterRegister, firstStep } from './settings';

vi.mock('./auth', () => ({ getAuth: () => ({ kind: 'signed-in', token: 'at' }), markVerified: () => {}, onUnauthorized: () => {} }));

type Handler = (url: string, init?: RequestInit) => Response;
const ok = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } });
const problem = (status: number, code: string, detail = '') =>
  new Response(JSON.stringify({ code, detail }), { status, headers: { 'Content-Type': 'application/problem+json' } });
const stubFetch = (h: Handler) => vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => h(url, init)));
const callOf = (i = 0) => { const [url, init] = vi.mocked(fetch).mock.calls[i] as [string, RequestInit | undefined]; return { url, init: init ?? {}, body: init?.body ? JSON.parse(init.body as string) : undefined }; };

async function load() {
  vi.resetModules();
  return import('./settings');
}

const github: Settings = {
  gitforge: 'github', webui_url: 'https://lf.example',
  github: { app_id: 12, client_id: 'Iv1.abc', slug: 'lf', api_url: 'https://api.github.com', web_url: 'https://github.com', private_key: { set: true, updated_by: 'alice', updated_at: '2026-11-18T12:00:00Z' }, client_secret: { set: true } },
  forgejo: null,
  intervals: { patrol_seconds: 1, list_seconds: 60, records_seconds: 5, git_timeout_seconds: 300, write_wait_seconds: 10 },
  updated_by: 'alice', updated_at: '2026-11-18T12:00:00Z',
};
const forgejo: Settings = {
  ...github, gitforge: 'forgejo', github: null,
  forgejo: { url: 'http://forgejo:3000', public_url: 'https://forgejo.example', organization: 'org', client_id: 'cid', worker_user: 'lf-worker', client_secret: { set: true }, worker_token: { set: true } },
};

beforeEach(() => { stubFetch(() => ok({})); });
afterEach(() => { vi.unstubAllGlobals(); });

describe('URL', () => {
  it('/settings の tab。ないか知らない値なら repository', () => {
    expect(match('/settings', '?tab=gitforge')).toEqual({ name: 'settings', tab: 'gitforge' });
    expect(match('/settings', '?tab=record-keys')).toEqual({ name: 'settings', tab: 'record-keys' });
    expect(match('/settings', '?tab=repository')).toEqual({ name: 'settings', tab: 'repository' });
    expect(match('/settings', '')).toEqual({ name: 'settings', tab: 'repository' });
    expect(match('/settings', '?tab=nothing')).toEqual({ name: 'settings', tab: 'repository' });
    expect(match('/settings')).toEqual({ name: 'settings', tab: 'repository' });
    expect(match('/settings', '?tab=repository&add=new')).toEqual({ name: 'settings', tab: 'repository', add: 'new' });
    expect(match('/settings', '?tab=repository&add=first')).toEqual({ name: 'settings', tab: 'repository', add: 'first' });
    expect(match('/settings', '?tab=repository&add=x')).toEqual({ name: 'settings', tab: 'repository' });
    expect(match('/settings', '?tab=gitforge&add=new')).toEqual({ name: 'settings', tab: 'gitforge' });
    expect(settingsPath('repository', 'new')).toBe('/settings?tab=repository&add=new');
    expect(settingsPath('record-keys')).toBe('/settings?tab=record-keys');
  });
});

describe('送る中身', () => {
  it('何も変えなければ、何も送らない', async () => {
    const s = await load();
    expect(s.buildPatch(github, s.formsFrom(github))).toEqual({ patch: {}, error: null });
    expect(s.buildPatch(forgejo, s.formsFrom(forgejo))).toEqual({ patch: {}, error: null });
  });

  it('変えた項目だけを送る。秘密は、入れたときだけ送る', async () => {
    const s = await load();
    const f = s.formsFrom(github);
    expect(f.github.private_key).toBe('');
    expect(f.github.client_secret).toBe('');
    f.github.slug = ' lf2 ';
    f.github.app_id = '13';
    f.intervals.patrol_seconds = '2.5';
    expect(s.buildPatch(github, f)).toEqual({ patch: { github: { slug: 'lf2', app_id: 13 }, intervals: { patrol_seconds: 2.5 } }, error: null });
    f.github.client_secret = 'sec';
    f.github.private_key = '-----BEGIN RSA PRIVATE KEY-----\nx\n-----END RSA PRIVATE KEY-----\n';
    const p = s.buildPatch(github, f).patch;
    expect(p.github).toMatchObject({ client_secret: 'sec', private_key: f.github.private_key });
  });

  it('Forgejo では forgejo の側だけを送る。構成は送らない', async () => {
    const s = await load();
    const f = s.formsFrom(forgejo);
    f.forgejo.worker_token = 'tok';
    f.github.slug = 'ignored';
    expect(s.buildPatch(forgejo, f)).toEqual({ patch: { forgejo: { worker_token: 'tok' } }, error: null });
  });

  it('正しくない数は送らない', async () => {
    const s = await load();
    const f = s.formsFrom(github);
    f.intervals.list_seconds = '0';
    expect(s.buildPatch(github, f).error).not.toBeNull();
    const g = s.formsFrom(github);
    g.github.app_id = 'abc';
    expect(s.buildPatch(github, g).error).not.toBeNull();
  });
});

describe('API の呼び方', () => {
  it('updateGitForgeSettings は PATCH /v1/settings/gitforge に、トークンを付けて送る', async () => {
    stubFetch(() => ok(github));
    const s = await load();
    await s.updateGitForgeSettings({ intervals: { patrol_seconds: 2 } });
    const c = callOf();
    expect(c.url).toBe('/v1/settings/gitforge');
    expect(c.init.method).toBe('PATCH');
    expect(new Headers(c.init.headers).get('Authorization')).toBe('Bearer at');
    expect(new Headers(c.init.headers).get('Content-Type')).toBe('application/json');
    expect(c.body).toEqual({ intervals: { patrol_seconds: 2 } });
  });

  it('registerRepository は PUT。ディレクトリが空なら送らない', async () => {
    stubFetch(() => ok({ name: 'q', gitforge_ref: 'o/r' }));
    const s = await load();
    await s.registerRepository('q', 'o/r');
    await s.registerRepository('q2', 'o/app', 'logiforge/q2');
    expect(callOf(0)).toMatchObject({ url: '/v1/settings/repositories/q', init: { method: 'PUT' }, body: { gitforge_ref: 'o/r' } });
    expect(callOf(1)).toMatchObject({ url: '/v1/settings/repositories/q2', body: { gitforge_ref: 'o/app', directory: 'logiforge/q2' } });
  });

  it('unregisterRepository は DELETE で、204 を受ける', async () => {
    stubFetch(() => new Response(null, { status: 204 }));
    const s = await load();
    await s.unregisterRepository('q');
    expect(callOf()).toMatchObject({ url: '/v1/settings/repositories/q', init: { method: 'DELETE' } });
  });

  it('一覧は items を返す。鍵は POST で作る', async () => {
    stubFetch((url, init) => url === '/v1/settings/record-keys' && init?.method === 'POST'
      ? ok({ key_id: 'k2', public_key: 'p', created_at: '2026-11-18T12:00:00Z', primary: true }, 201)
      : ok({ items: [{ key_id: 'k1', public_key: 'p', created_at: '2026-11-18T12:00:00Z', primary: true }] }));
    const s = await load();
    expect((await s.listRecordKeys()).map((k) => k.key_id)).toEqual(['k1']);
    expect((await s.createRecordKey()).key_id).toBe('k2');
    expect(callOf(1).init.method).toBe('POST');
  });

  it('validation_failed の detail は Problem に入る', async () => {
    stubFetch(() => problem(422, 'validation_failed', 'App が install されていない。https://github.com/apps/lf/installations/new から install する'));
    const s = await load();
    const e = await s.registerRepository('q', 'o/r').catch((x) => x);
    expect(e).toMatchObject({ code: 'validation_failed' });
    expect(s.installUrlFrom(e.detail)).toBe('https://github.com/apps/lf/installations/new');
  });
});

describe('install の URL', () => {
  it('detail から URL を取り出す。句読点と括弧は含めない', async () => {
    const s = await load();
    expect(s.installUrlFrom('install してください：https://github.com/apps/lf/installations/new。')).toBe('https://github.com/apps/lf/installations/new');
    expect(s.installUrlFrom('（https://github.com/apps/lf/installations/new）')).toBe('https://github.com/apps/lf/installations/new');
    expect(s.installUrlFrom('see https://github.com/apps/lf/installations/new.')).toBe('https://github.com/apps/lf/installations/new');
    expect(s.installUrlFrom('リポジトリがない')).toBeNull();
    expect(s.installUrlFrom('javascript:alert(1)')).toBeNull();
  });
});

describe('メニューに出すか', () => {
  it('getSettings が読めたら出す', async () => {
    stubFetch(() => ok(github));
    const s = await load();
    expect(s.getAccess()).toBe('unknown');
    expect(await s.loadAccess()).toBe('yes');
    expect(callOf().url).toBe('/v1/settings');
  });
  it('forbidden なら出さない', async () => {
    stubFetch(() => problem(403, 'forbidden'));
    const s = await load();
    expect(await s.loadAccess()).toBe('no');
  });
  it('ほかのエラーでは決めない（出さない）', async () => {
    stubFetch(() => problem(500, 'internal'));
    const s = await load();
    expect(await s.loadAccess()).toBe('unknown');
  });
});

describe('repository を足す流れの段', () => {
  it('登録したあと：スナップショットツリーがもうあれば最初の問いを飛ばす。分からなければ最初の問いへ', () => {
    expect(afterRegister({ name: 'a', gitforge_ref: 'o/a', prepared: true })).toEqual({ at: 'exists', name: 'a' });
    expect(afterRegister({ name: 'a', gitforge_ref: 'o/a', prepared: false })).toEqual({ at: 'question', name: 'a' });
    expect(afterRegister({ name: 'a', gitforge_ref: 'o/a' })).toEqual({ at: 'question', name: 'a' });
  });
  it('セットアップのあと：最初の問いがまだの repository があれば、その 2 段目から', () => {
    expect(firstStep([{ name: 'a', gitforge_ref: 'o/a', prepared: true }, { name: 'b', gitforge_ref: 'o/b', prepared: false }])).toEqual({ at: 'question', name: 'b' });
    expect(firstStep([{ name: 'a', gitforge_ref: 'o/a' }])).toEqual({ at: 'repository' });
    expect(firstStep([])).toEqual({ at: 'repository' });
  });
});
