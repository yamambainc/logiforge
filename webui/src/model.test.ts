import { describe, expect, it } from 'vitest';
import { buildIndex, filterNodes, rowPositions, rowsToDraw, visibleRows, withAncestors, type Filter, type Node } from './model';

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
    // 深さは、表示の根からの深さ
    expect(visibleRows(ix, new Set(['s1']), null, true, 's1').map((r) => r.depth)).toEqual([0, 1, 1]);
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

describe('描く行', () => {
  it('見えている範囲と、範囲の外の選んだ行', () => {
    expect(rowsToDraw(10, 13, 11)).toEqual([10, 11, 12]);
    expect(rowsToDraw(10, 13, 40)).toEqual([10, 11, 12, 40]);
    expect(rowsToDraw(10, 13, 2)).toEqual([10, 11, 12, 2]);
    expect(rowsToDraw(10, 13, -1)).toEqual([10, 11, 12]);
  });
});

describe('未決の葉の絞り込み', () => {
  it('葉の仮説で、値が未決。inactive は除く', () => {
    // h0（葉でない）、h3（未決の葉だが inactive）
    expect(filterNodes(buildIndex(nodes), '', new Set(['leaf'] as Filter[]), true).matches).toEqual([]);
    const ix2 = buildIndex([...nodes, h('h4', 's1', null)]);
    expect(filterNodes(ix2, '', new Set(['leaf'] as Filter[]), true).matches).toEqual(['h4']);
  });
});

describe('作業としての絞り込み（ダッシュボードの件数と同じ）', () => {
  it('AR が対象にしているノードと、依頼のあるノードは出さない', () => {
    const ix = buildIndex([
      h('h0', null, null, { leaf: false }), s('s1', 'h0', null),
      h('a', 's1', null), h('b', 's1', null, { open_apply_request_ids: ['apr_1'] }), h('c', 's1', null, { open_human_request_ids: ['hrq_1'] }),
      h('d', 's1', null, { needs_reconsideration: true } as Partial<Node>), h('e', 's1', null, { needs_reconsideration: true, open_apply_request_ids: ['apr_2'] } as Partial<Node>),
      h('f', 's1', null, { needs_reconsideration: true, open_human_request_ids: ['hrq_2'] } as Partial<Node>),
    ]);
    expect(filterNodes(ix, '', new Set(['leaf'] as Filter[]), true).matches).toEqual(['a', 'd']);
    expect(filterNodes(ix, '', new Set(['nr-work'] as Filter[]), true).matches).toEqual(['d']);
    // ふだんの要再検討は、そのまま
    expect(filterNodes(ix, '', new Set(['nr'] as Filter[]), true).matches).toEqual(['d', 'e', 'f']);
  });
});
