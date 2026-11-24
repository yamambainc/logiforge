import { beforeEach, describe, expect, it, vi } from 'vitest';
import { buildIndex, type Node } from './model';
import { hiddenWhenInactiveHidden, loadShowInactive, nearestShown, saveShowInactive, SHOW_INACTIVE_DEFAULT } from './showInactive';

class MemStorage {
  private m = new Map<string, string>();
  getItem(k: string) { return this.m.get(k) ?? null; }
  setItem(k: string, v: string) { this.m.set(k, v); }
  removeItem(k: string) { this.m.delete(k); }
}

const h = (id: string, parent: string | null, extra: Partial<Node> = {}): Node =>
  ({ node_type: 'hypothesis', id, parent_story_id: parent, statement: id, value: null, leaf: true, needs_reconsideration: false, inactive: false, open_apply_request_ids: [], open_human_request_ids: [], ...extra }) as Node;
const s = (id: string, parent: string, extra: Partial<Node> = {}): Node =>
  ({ node_type: 'story', id, parent_hypothesis_id: parent, title: id, summary: '', state: 'active', value: null, inactive: false, open_apply_request_ids: [], open_human_request_ids: [], ...extra }) as Node;

// h0 ─ s1 ─ h1
//    └ s2(inactive) ─ h2 ─ s3 ─ h3
const ix = buildIndex([h('h0', null, { leaf: false }), s('s1', 'h0'), h('h1', 's1'), s('s2', 'h0', { inactive: true }), h('h2', 's2', { leaf: false }), s('s3', 'h2'), h('h3', 's3')]);

describe('inactive を示すかの保存', () => {
  beforeEach(() => { vi.stubGlobal('localStorage', new MemStorage()); });
  it('置いたものがなければ、初めの値', () => {
    expect(loadShowInactive()).toBe(SHOW_INACTIVE_DEFAULT);
  });
  it('切り替えた値を、次に読むときも返す', () => {
    saveShowInactive(false);
    expect(loadShowInactive()).toBe(false);
    saveShowInactive(true);
    expect(loadShowInactive()).toBe(true);
  });
  it('localStorage が使えなくても、初めの値を返し、置くときに落ちない', () => {
    vi.stubGlobal('localStorage', { getItem: () => { throw new Error('denied'); }, setItem: () => { throw new Error('denied'); } });
    expect(loadShowInactive()).toBe(SHOW_INACTIVE_DEFAULT);
    expect(() => saveShowInactive(false)).not.toThrow();
  });
});

describe('隠すと見えなくなるノード', () => {
  it('自分か祖先が inactive なら、隠れる', () => {
    expect(hiddenWhenInactiveHidden(ix, 's2')).toBe(true);
    expect(hiddenWhenInactiveHidden(ix, 'h3')).toBe(true);
    expect(hiddenWhenInactiveHidden(ix, 'h1')).toBe(false);
    expect(hiddenWhenInactiveHidden(ix, 'h0')).toBe(false);
  });
  it('見えている、いちばん近い祖先', () => {
    expect(nearestShown(ix, 'h3')).toBe('h0');
    expect(nearestShown(ix, 's2')).toBe('h0');
    expect(nearestShown(ix, 'h1')).toBe('h1');
  });
});
