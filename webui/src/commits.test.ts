import { describe, expect, it } from 'vitest';
import type { MainCommit } from './api';
import { commitPager, type CommitPage, type CommitState } from './commits';

const c = (n: number): MainCommit => ({ number: n, commit: `sha${n}`, committed_at: '2026-11-23T00:00:00Z', apply_request_id: null });
const page = (from: number, to: number, next: string | null): CommitPage => ({ items: Array.from({ length: from - to + 1 }, (_, i) => c(from - i)), next_cursor: next });

describe('時点の選択肢のコミット', () => {
  it('「さらに古いコミット…」で、続きの 50 個を読み、後ろに足す', async () => {
    const cursors: (string | null)[] = [];
    const pages: Record<string, CommitPage> = { first: page(120, 71, '70'), '70': page(70, 21, '20'), '20': page(20, 1, null) };
    let s: CommitState = { items: [], next: null };
    const p = commitPager(async (cur) => { cursors.push(cur); return pages[cur ?? 'first']; }, (x) => { s = x; });
    await p.first();
    expect(s.items.map((x) => x.number).slice(0, 2)).toEqual([120, 119]);
    expect(s.items).toHaveLength(50);
    expect(s.next).toBe('70');
    await p.more();
    expect(s.items).toHaveLength(100);
    expect(s.items[49].number).toBe(71);
    expect(s.items[50].number).toBe(70);
    await p.more();
    expect(s.items).toHaveLength(120);
    expect(s.next).toBeNull();
    // もうなければ、読まない
    await p.more();
    expect(cursors).toEqual([null, '70', '20']);
  });

  it('読み込み中にもう一度選んでも、同じ 50 個を 2 回足さない', async () => {
    let release!: (p: CommitPage) => void;
    const calls: (string | null)[] = [];
    let s: CommitState = { items: [], next: null };
    const p = commitPager((cur) => {
      calls.push(cur);
      if (cur === null) return Promise.resolve(page(100, 51, '50'));
      return new Promise<CommitPage>((r) => { release = r; });
    }, (x) => { s = x; });
    await p.first();
    const a = p.more();
    const b = p.more(); // 遅い回線で、もう一度選んだ
    release(page(50, 1, null));
    await Promise.all([a, b]);
    expect(calls).toEqual([null, '50']);
    expect(s.items).toHaveLength(100);
    expect(new Set(s.items.map((x) => x.commit)).size).toBe(100);
  });

  it('読んだページに、もうあるコミットが入っていても、重ねて足さない', async () => {
    let s: CommitState = { items: [], next: null };
    const p = commitPager(async (cur) => (cur === null ? page(10, 6, '6') : page(6, 1, null)), (x) => { s = x; });
    await p.first();
    await p.more();
    expect(s.items.map((x) => x.number)).toEqual([10, 9, 8, 7, 6, 5, 4, 3, 2, 1]);
  });
});
