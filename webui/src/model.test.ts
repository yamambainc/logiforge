import { describe, expect, it } from 'vitest';
import { buildIndex, filterNodes, rowPositions, visibleRows, withAncestors, type Filter, type Node } from './model';

const h = (id: string, parent: string | null, value: boolean | null, extra: Partial<Node> = {}): Node =>
  ({ node_type: 'hypothesis', id, parent_story_id: parent, statement: id, value, leaf: true, needs_reconsideration: false, inactive: false, open_apply_request_ids: [], open_human_request_ids: [], ...extra }) as Node;
const s = (id: string, parent: string, value: boolean | null, extra: Partial<Node> = {}): Node =>
  ({ node_type: 'story', id, parent_hypothesis_id: parent, title: id, summary: '', state: 'active', value, inactive: false, open_apply_request_ids: [], open_human_request_ids: [], ...extra }) as Node;

// h0 ─ s1 ─ h1(真), h2(偽)
//    └ s2 ─ h3(未決, inactive)
const nodes = [h('h0', null, null, { leaf: false }), s('s1', 'h0', false), h('h1', 's1', true), h('h2', 's1', false), s('s2', 'h0', null, { inactive: true }), h('h3', 's2', null, { inactive: true })];

describe('索引', () => {
  const ix = buildIndex(nodes);
  it('根、子の並び、深さ', () => {
    expect(ix.rootId).toBe('h0');
    expect(ix.children.get('h0')).toEqual(['s1', 's2']);
    expect(ix.depth.get('h3')).toBe(2);
  });
  it('部分木の葉の値の内訳（inactive な葉は数えない）とノード数', () => {
    expect(ix.stats.get('h0')).toEqual({ t: 1, f: 1, u: 0, total: 6 });
    expect(ix.stats.get('s1')).toEqual({ t: 1, f: 1, u: 0, total: 3 });
  });
});

describe('表示する行', () => {
  const ix = buildIndex(nodes);
  it('開いたノードの子だけを出す', () => {
    expect(visibleRows(ix, new Set(['h0']), null, true).map((r) => r.id)).toEqual(['h0', 's1', 's2']);
  });
  it('inactive を隠す', () => {
    expect(visibleRows(ix, new Set(['h0', 's1', 's2']), null, false).map((r) => r.id)).toEqual(['h0', 's1', 'h1', 'h2']);
  });
  it('絞り込みでは、一致したノードと祖先を、開いて出す', () => {
    const inc = withAncestors(ix, ['h2']);
    expect(visibleRows(ix, new Set(), inc, true).map((r) => r.id)).toEqual(['h0', 's1', 'h2']);
  });
  it('表示の根を変えられる', () => {
    expect(visibleRows(ix, new Set(['s1']), null, true, 's1').map((r) => r.id)).toEqual(['s1', 'h1', 'h2']);
  });
});

describe('検索と絞り込み', () => {
  const ix = buildIndex(nodes);
  it('なにもなければ、すべて', () => {
    expect(filterNodes(ix, '  ', new Set(), true)).toEqual({ include: null, matches: [] });
  });
  it('検索は、主文と識別子の部分一致で、大文字と小文字を区別しない', () => {
    expect(filterNodes(ix, 'H2', new Set(), true).matches).toEqual(['h2']);
    expect([...filterNodes(ix, 'h2', new Set(), true).include!].sort()).toEqual(['h0', 'h2', 's1']);
  });
  it('値どうしは「または」', () => {
    expect(filterNodes(ix, '', new Set(['true', 'false'] as Filter[]), true).matches).toEqual(['s1', 'h1', 'h2']);
  });
  it('値とほかは「かつ」、検索とも「かつ」', () => {
    expect(filterNodes(ix, 'h1', new Set(['false'] as Filter[]), true).matches).toEqual([]);
  });
  it('inactive を隠す', () => {
    expect(filterNodes(ix, '', new Set(['null'] as Filter[]), false).matches).toEqual(['h0']);
    expect(filterNodes(ix, '', new Set(['null'] as Filter[]), true).matches).toEqual(['h0', 's2', 'h3']);
  });
});

describe('読み上げの位置', () => {
  it('段と、兄弟の数と、兄弟の中の位置', () => {
    const rows = [0, 1, 2, 2, 1, 2, 1].map((depth, i) => ({ id: 'r' + i, depth, hasChildren: false, open: false }));
    expect(rowPositions(rows).map((p) => `${p.level}:${p.posinset}/${p.setsize}`)).toEqual(['1:1/1', '2:1/3', '3:1/2', '3:2/2', '2:2/3', '3:1/1', '2:3/3']);
  });
});
