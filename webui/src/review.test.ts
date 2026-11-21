// WebUI からのレビューの送り方を、ブラウザなしで確かめる（セッション ID、要求の形、commit_conflict、エラーの言葉、結果の言葉）。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ApplyRequest } from './api';

vi.mock('./auth', () => ({ getAuth: () => ({ kind: 'signed-in', token: 'at' }), markVerified: () => {}, onUnauthorized: () => {} }));

class MemStorage {
  m = new Map<string, string>();
  get length() { return this.m.size; }
  key(i: number) { return [...this.m.keys()][i] ?? null; }
  getItem(k: string) { return this.m.get(k) ?? null; }
  setItem(k: string, v: string) { this.m.set(k, v); }
  removeItem(k: string) { this.m.delete(k); }
}

type Handler = (url: string, init?: RequestInit) => Response;
const ok = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } });
const problem = (status: number, code: string, detail = '') =>
  new Response(JSON.stringify({ code, detail }), { status, headers: { 'Content-Type': 'application/problem+json' } });
const stubFetch = (h: Handler) => vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => h(url, init)));
const callOf = (i = 0) => { const [url, init] = vi.mocked(fetch).mock.calls[i] as [string, RequestInit | undefined]; return { url, init: init ?? {}, body: init?.body ? JSON.parse(init.body as string) : undefined }; };

const ar = (p: Partial<ApplyRequest> = {}): ApplyRequest => ({ id: 'apr_1', state: 'open', branch: 'b', target: 'main', head_commit: 'c1', ...p });

async function load() {
  vi.resetModules();
  return import('./review');
}

beforeEach(() => { vi.stubGlobal('sessionStorage', new MemStorage()); stubFetch(() => ok({})); });
afterEach(() => { vi.unstubAllGlobals(); });

describe('レビューを送る', () => {
  it('kind と commit と body を送る。prompt_revision は送らない', async () => {
    stubFetch(() => ok({ review: { id: 'rev_1' }, apply_request: ar() }, 201));
    const { submitReview } = await load();
    const r = await submitReview('demo', 'apr_1', 'comment', 'c1', '  見ました  ');
    expect(r).toEqual({ kind: 'sent', ar: ar(), message: 'コメントを付けました。' });
    const c = callOf();
    expect(c.url).toBe('/v1/repositories/demo/apply-requests/apr_1/reviews');
    expect(c.init.method).toBe('POST');
    expect(c.init.headers as Record<string, string>).not.toHaveProperty('Logiforge-Session-Id');
    expect(c.body).toEqual({ kind: 'comment', commit: 'c1', body: '見ました' });
    expect(c.body).not.toHaveProperty('prompt_revision');
  });
  it('本文のない approve は、body を送らない。approve は merge しない', async () => {
    stubFetch(() => ok({ review: {}, apply_request: ar() }, 201));
    const { submitReview } = await load();
    const r = await submitReview('demo', 'apr_1', 'approve', 'c1', '');
    expect(callOf().body).toEqual({ kind: 'approve', commit: 'c1' });
    expect(r).toMatchObject({ kind: 'sent', message: 'approve を付けました。merge は、別に行います。' });
  });
  it('今の main での確かめに通らなければ、approve を出さなかった理由を示し、apply request を読み直す', async () => {
    stubFetch((url, init) => (init?.method === 'POST' ? problem(409, 'stale_target', 'd') : ok(ar())));
    const { submitReview } = await load();
    const r = await submitReview('demo', 'apr_1', 'approve', 'c1', '');
    expect(r).toMatchObject({ kind: 'refused', message: 'approve を出しませんでした：ブランチを今の main に合わせる必要がある（d）' });
    expect(vi.mocked(fetch).mock.calls).toHaveLength(2);
  });
  it('approve のあとに示す言葉', async () => {
    const { outcome } = await load();
    expect(outcome('approve', ar({ target: 'feature' }))).toContain('main 向きではない');
    expect(outcome('request_changes', ar())).toBe('request_changes を付けました。');
  });
});

describe('commit_conflict', () => {
  it('新しい先頭のコミットを読み直して返し、確かめ直させる', async () => {
    stubFetch((url, init) => (init?.method === 'POST' ? problem(409, 'commit_conflict') : ok(ar({ head_commit: 'c2' }))));
    const { submitReview, canSendApprove, CONFLICT_MESSAGE } = await load();
    const shown = 'c1';
    expect(canSendApprove(shown, shown)).toBe(true);
    const r = await submitReview('demo', 'apr_1', 'approve', shown, '');
    expect(r).toEqual({ kind: 'conflict', ar: ar({ head_commit: 'c2' }), message: CONFLICT_MESSAGE });
    expect(callOf(1).url).toBe('/v1/repositories/demo/apply-requests/apr_1');
    // 示す先頭が c2 になったので、c1 で確かめたままでは送れない
    expect(canSendApprove(shown, 'c2')).toBe(false);
    expect(canSendApprove(null, 'c2')).toBe(false);
    expect(canSendApprove('c2', 'c2')).toBe(true);
  });
  it('not_open なら、読み直した apply request と言葉を返す', async () => {
    stubFetch((url, init) => (init?.method === 'POST' ? problem(409, 'not_open', 'close して作り直す') : ok(ar({ state: 'merged' }))));
    const { submitReview } = await load();
    const r = await submitReview('demo', 'apr_1', 'comment', 'c1', 'x');
    expect(r).toMatchObject({ kind: 'not_open', ar: { state: 'merged' } });
    expect((r as { message: string }).message).toContain('close して作り直す');
  });
});

describe('エラーの言葉', () => {
  const cases: [number, string, string][] = [
    [403, 'forbidden', 'レビューできるのは、この repository に Write がある人だけです。'],
    [403, 'same_session', 'このタブのセッションは、差分を書いたセッションと同じです。差分を書いたセッションからは、レビューできません。'],
    [503, 'snapshot_pending', '先頭のコミットを、まだ取り込んでいるところです。少し待ってから、もう一度送ってください。'],
    [422, 'validation_failed', '送った中身を受け付けませんでした。'],
  ];
  for (const [status, code, msg] of cases) {
    it(code, async () => {
      stubFetch(() => problem(status, code));
      const { submitReview } = await load();
      expect(await submitReview('demo', 'apr_1', 'comment', 'c1', 'x')).toEqual({ kind: 'error', message: msg });
    });
  }
  it('detail があれば添える', async () => {
    stubFetch(() => problem(422, 'validation_failed', '先頭のコミットに Logiforge-Session-Id: がない'));
    const { submitReview } = await load();
    expect(await submitReview('demo', 'apr_1', 'approve', 'c1', '')).toEqual({ kind: 'error', message: '送った中身を受け付けませんでした。（先頭のコミットに Logiforge-Session-Id: がない）' });
  });
  it('401 は、サインインに任せて、言葉を出さない', async () => {
    stubFetch(() => new Response('', { status: 401 }));
    const { submitReview } = await load();
    expect(await submitReview('demo', 'apr_1', 'comment', 'c1', 'x')).toEqual({ kind: 'unauthenticated' });
  });
});

describe('approve の前に示すこと', () => {
  it('自分が作った apply request なら「あなたが作った apply request です」', async () => {
    const { ownNotice, approveSummary, OWN_AR } = await load();
    expect(OWN_AR).toBe('あなたが作った apply request です。');
    expect(ownNotice(ar({ opened_by_caller: true }))).toBe(OWN_AR);
    expect(ownNotice(ar({ opened_by_caller: false }))).toBeNull();
    expect(ownNotice(ar())).toBeNull();
    expect(approveSummary(ar({ opened_by_caller: true }), 'c1', 3)).toEqual({ commit: 'c1', lines: ['差分の操作 3 個。', '今の main で確かめ、通ったときだけ approve を出します。merge は、別に行います。'], own: OWN_AR });
    expect(approveSummary(ar({ target: 'feature' }), 'c1', 0).lines[1]).toBe('main 向きではないので、Logiforge は確かめません。');
  });
});

describe('merge', () => {
  const rv = (kind: 'approve' | 'request_changes' | 'comment', commit: string) => ({ id: 'r', kind, commit, valid: true });
  it('最後の決めるレビューが、今の先頭のコミットへの approve のときだけ、merge できる', async () => {
    const { canMerge } = await load();
    expect(canMerge(ar({ reviews: [rv('approve', 'c1')] }))).toBe(true);
    expect(canMerge(ar({ reviews: [rv('approve', 'c1'), rv('comment', 'c1')] }))).toBe(true);
    expect(canMerge(ar({ reviews: [rv('approve', 'c0')] }))).toBe(false);
    expect(canMerge(ar({ reviews: [rv('approve', 'c1'), rv('request_changes', 'c1')] }))).toBe(false);
    expect(canMerge(ar({ reviews: [rv('approve', 'c1')], target: 'feature' }))).toBe(false);
    expect(canMerge(ar({ reviews: [rv('approve', 'c1')], state: 'merged' }))).toBe(false);
  });
  it('merge を送る', async () => {
    stubFetch(() => ok(ar({ state: 'merged' })));
    const { submitMerge } = await load();
    const r = await submitMerge('demo', 'apr_1');
    expect(callOf().url).toBe('/v1/repositories/demo/apply-requests/apr_1/merge');
    expect(callOf().init.method).toBe('POST');
    expect(r).toMatchObject({ kind: 'sent', message: 'merge しました。' });
  });
  it('merge が断られたら、理由を示す', async () => {
    stubFetch((url, init) => (init?.method === 'POST' ? problem(403, 'merge_forbidden', 'protected') : ok(ar())));
    const { submitMerge } = await load();
    expect(await submitMerge('demo', 'apr_1')).toMatchObject({ kind: 'refused', message: 'merge しませんでした：GitForge で merge する権限がなく、GitForge が merge を断った（protected）' });
  });
});
