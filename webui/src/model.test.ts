import { describe, expect, it } from 'vitest';
import { buildIndex, visibleRows, withAncestors, type Node } from './model';

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
