import { describe, expect, it } from 'vitest';
import { buildIndex, changeLabel, changeOrder, diff, filterNodes, withRemoved, type Node } from './model';
import { overlayFocus } from './focus';

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
    expect(d.changes.get('h0')).toMatchObject({ kind: 'changed', direct: false });
    expect(d.changes.get('s1')).toMatchObject({ direct: false });
    expect(d.changes.get('h1')).toMatchObject({ direct: true });
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
    // 伝播だけで変わる祖先（h0、s1）は、たどらない
    expect(changeOrder(ix, d.changes)).toEqual(['h1', 's2', 'h3', 's3', 'h4', 'h6']);
  });
  it('絞り込みの「この AR で変わる」', () => {
    const ix = buildIndex(withRemoved(after, before, d.removed));
    const r = filterNodes(ix, '', new Set(['changed'] as const), true, d.changes);
    expect(r.matches).toEqual(['h0', 's1', 'h1', 's2', 'h3', 's3', 'h4', 'h6']);
    expect(r.include!.has('s4')).toBe(true);
  });
});

describe('差分の重ね表示の細部', () => {
  it('消えるノードは、部分木のノード数と内訳に数えない', () => {
    const d = diff(before, after);
    const ix = buildIndex(withRemoved(after, before, d.removed), new Set(d.removed));
    expect(ix.stats.get('h0')!.total).toBe(7);
    expect(ix.stats.get('s2')!.total).toBe(0);
  });
  it('前の兄弟も消えるとき、残る兄弟の前に、もとの順で置く', () => {
    // 比べる相手：h0 ─ sa, sb, sc。反映したあと：sc だけ
    const b = buildIndex([h('h0', null, null, { leaf: false }), s('sa', 'h0', null), s('sb', 'h0', null), s('sc', 'h0', null)]);
    const a = buildIndex([h('h0', null, null, { leaf: false }), s('sc', 'h0', null)]);
    const d = diff(b, a);
    expect(buildIndex(withRemoved(a, b, d.removed)).children.get('h0')).toEqual(['sa', 'sb', 'sc']);
  });
  it('親の子がすべて消えるとき、もとの順で残す', () => {
    const b = buildIndex([h('h0', null, null, { leaf: false }), s('s1', 'h0', null), h('h1', 's1', null), h('h2', 's1', null)]);
    const a = buildIndex([h('h0', null, null, { leaf: false }), s('s1', 'h0', null)]);
    const d = diff(b, a);
    expect(buildIndex(withRemoved(a, b, d.removed)).children.get('s1')).toEqual(['h1', 'h2']);
  });
  it('札：値、要再検討だけ、ほか', () => {
    expect(changeLabel({ kind: 'changed', direct: false, fields: [{ name: '値', before: '未決', after: '偽' }] })).toBe('未決 → 偽');
    expect(changeLabel({ kind: 'changed', direct: false, fields: [{ name: '要再検討', before: 'いいえ', after: 'はい' }] })).toBe('→ 要再検討');
    expect(changeLabel({ kind: 'changed', direct: true, fields: [{ name: '主文', before: 'a', after: 'b' }] })).toBe('変化');
    expect(changeLabel({ kind: 'added' })).toBeNull();
  });
  it('重ねたときの最初の表示は、直接の変化だけを使う', () => {
    const d = diff(before, after);
    const ix = buildIndex(withRemoved(after, before, d.removed));
    const f = overlayFocus(ix, d.changes, null);
    expect(f.selected).toBe('h1');
    expect(f.around!.has('h0')).toBe(true); // s2 の親として
    expect(f.around!.has('s1')).toBe(true); // h1 の親として
    expect([...f.around!].sort()).toEqual(['h0', 'h1', 'h3', 'h4', 'h6', 's1', 's2', 's3', 's4']);
    // URL の node があれば、それを選ぶ
    expect(overlayFocus(ix, d.changes, 'h5').selected).toBe('h5');
  });
});
