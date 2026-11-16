import { describe, expect, it } from 'vitest';
import { layoutCone, RING_R } from './cone';
import { buildIndex, visibleRows, type Node } from './model';

const h = (id: string, parent: string | null): Node =>
  ({ node_type: 'hypothesis', id, parent_story_id: parent, statement: id, value: null, leaf: true, needs_reconsideration: false, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] }) as Node;
const s = (id: string, parent: string): Node =>
  ({ node_type: 'story', id, parent_hypothesis_id: parent, title: id, summary: '', state: 'active', value: null, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] }) as Node;

// h0 ─ s1 ─ h1, h2, h3
//    └ s2 ─ h4
const ix = buildIndex([h('h0', null), s('s1', 'h0'), h('h1', 's1'), h('h2', 's1'), h('h3', 's1'), s('s2', 'h0'), h('h4', 's2')]);

describe('3D の置き方（層ごとの円錐）', () => {
  const all = new Set(['h0', 's1', 's2']);
  const p = layoutCone(visibleRows(ix, all, null, true), ix);
  it('根を頂点に、深さごとに下の層へ', () => {
    const [x, y, z] = p.get('h0')!.pos.toArray();
    expect([x, y, z].map((v) => Math.abs(v))).toEqual([0, 0, 0]);
    expect(p.get('s1')!.pos.y).toBeLessThan(0);
    expect(p.get('h1')!.pos.y).toBeLessThan(p.get('s1')!.pos.y);
    expect(p.get('h1')!.pos.y).toBe(p.get('h4')!.pos.y);
  });
  it('各層は、根の真下を中心にした円の上。半径は深さ × 90 から', () => {
    const r = (id: string) => Math.hypot(p.get(id)!.pos.x, p.get(id)!.pos.z);
    expect(r('s1')).toBeCloseTo(RING_R);
    expect(r('h4')).toBeCloseTo(2 * RING_R);
  });
  it('角度は、見えている葉の数に比例して分ける（s1 は 3/4、s2 は 1/4）', () => {
    const ang = (id: string) => (Math.atan2(p.get(id)!.pos.z, p.get(id)!.pos.x) + 2 * Math.PI) % (2 * Math.PI);
    expect(ang('s1')).toBeCloseTo((Math.PI * 2 * 3) / 4 / 2);
    expect(ang('s2')).toBeCloseTo(Math.PI * 2 * (3 / 4 + 1 / 8));
  });
  it('閉じた部分木は、ノード数に応じて大きく描く', () => {
    const q = layoutCone(visibleRows(ix, new Set(['h0']), null, true), ix);
    expect(q.get('s1')!.size).toBeCloseTo(1 + Math.log10(4) * 0.55);
    expect(q.get('h0')!.size).toBe(1);
  });
  it('ここから見る：表示の根を頂点に置く', () => {
    const q = layoutCone(visibleRows(ix, new Set(['s1']), null, true, 's1'), ix);
    expect(Math.abs(q.get('s1')!.pos.y)).toBe(0);
    expect(q.has('h0')).toBe(false);
  });
});
