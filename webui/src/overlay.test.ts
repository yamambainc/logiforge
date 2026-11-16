import { describe, expect, it } from 'vitest';
import { buildIndex, changeOrder, diff, filterNodes, withRemoved, type Node } from './model';

const h = (id: string, parent: string | null, value: boolean | null, extra: Partial<Node> = {}): Node =>
  ({ node_type: 'hypothesis', id, parent_story_id: parent, statement: id, value, leaf: true, needs_reconsideration: false, inactive: false, open_apply_request_ids: [], open_human_request_ids: [], ...extra }) as Node;
const s = (id: string, parent: string, value: boolean | null, extra: Partial<Node> = {}): Node =>
  ({ node_type: 'story', id, parent_hypothesis_id: parent, title: id, summary: '', state: 'active', value, inactive: false, open_apply_request_ids: [], open_human_request_ids: [], ...extra }) as Node;

// 比べる相手：h0 ─ s1 ─ h1(未決), h2(真)
//                 └ s2 ─ h3(偽) ─ s3 ─ h4
//                 └ s4 ─ h5
// 反映したあと：h1 に検証結果（真）が付き、s1 と h0 も真になる。s2 の部分木は消える。s4 の下に h6 が加わる。
const before = buildIndex([
  h('h0', null, null, { leaf: false }), s('s1', 'h0', null), h('h1', 's1', null), h('h2', 's1', true),
  s('s2', 'h0', false), h('h3', 's2', false, { leaf: false }), s('s3', 'h3', null), h('h4', 's3', null),
  s('s4', 'h0', null), h('h5', 's4', null),
]);
const after = buildIndex([
  h('h0', null, true, { leaf: false }), s('s1', 'h0', true), h('h1', 's1', true, { valid_verdict_id: 'v1' } as Partial<Node>), h('h2', 's1', true),
  s('s4', 'h0', null), h('h5', 's4', null), h('h6', 's4', null),
]);

describe('差分の重ね表示', () => {
  const d = diff(before, after);
  it('追加・変化・消える。値は、伝播で変わる祖先にも付く', () => {
    expect(d.changes.get('h6')).toEqual({ kind: 'added' });
    expect(d.changes.get('h1')).toMatchObject({ kind: 'changed', fields: [{ name: '値', before: '未決', after: '真' }, { name: '有効な検証結果', before: 'なし', after: 'v1' }] });
    expect(d.changes.get('s1')).toMatchObject({ kind: 'changed', fields: [{ name: '値', before: '未決', after: '真' }] });
    expect(d.changes.get('h0')).toMatchObject({ kind: 'changed' });
    expect(d.changes.has('h2')).toBe(false);
    expect(d.removed).toEqual(['s2', 'h3', 's3', 'h4']);
    expect(d.changes.get('h4')).toEqual({ kind: 'removed' });
  });
  it('消えるノードは、比べた相手での位置に、子孫ごと残す', () => {
    const ix = buildIndex(withRemoved(after, before, d.removed));
    expect(ix.nodes.map((n) => n.id)).toEqual(['h0', 's1', 'h1', 'h2', 's2', 'h3', 's3', 'h4', 's4', 'h5', 'h6']);
    expect(ix.depth.get('h4')).toBe(4);
  });
  it('変わるノードを、行の並びで', () => {
    const ix = buildIndex(withRemoved(after, before, d.removed));
    expect(changeOrder(ix, d.changes)).toEqual(['h0', 's1', 'h1', 's2', 'h3', 's3', 'h4', 'h6']);
  });
  it('絞り込みの「この AR で変わる」', () => {
    const ix = buildIndex(withRemoved(after, before, d.removed));
    const r = filterNodes(ix, '', new Set(['changed'] as const), true, d.changes);
    expect(r.matches).toEqual(['h0', 's1', 'h1', 's2', 'h3', 's3', 'h4', 'h6']);
    expect(r.include!.has('s4')).toBe(true);
  });
});
