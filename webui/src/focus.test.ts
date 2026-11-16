import { describe, expect, it } from 'vitest';
import { initialFocus, openLevels } from './focus';
import { buildIndex, type Node } from './model';

const n = (id: string, parent: string | null, type: 'hypothesis' | 'story') =>
  (type === 'hypothesis'
    ? { node_type: 'hypothesis', id, parent_story_id: parent, statement: id, value: null, leaf: false, needs_reconsideration: false, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] }
    : { node_type: 'story', id, parent_hypothesis_id: parent, title: id, summary: '', state: 'active', value: null, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] }) as Node;
// 深さ 0..5 の 1 本道
const ix = buildIndex([n('h0', null, 'hypothesis'), n('s1', 'h0', 'story'), n('h1', 's1', 'hypothesis'), n('s2', 'h1', 'story'), n('h2', 's2', 'hypothesis'), n('s3', 'h2', 'story')]);

describe('最初の表示', () => {
  it('根から 2 段までを開き、根を選ぶ', () => {
    const f = initialFocus(ix, null);
    expect([...f.expanded].sort()).toEqual(['h0', 's1']);
    expect(f.selected).toBe('h0');
    expect(f.root).toBe('h0');
  });
  it('URL の node があれば、その祖先を開いて選ぶ', () => {
    const f = initialFocus(ix, 's3');
    expect(f.selected).toBe('s3');
    for (const a of ['h0', 's1', 'h1', 's2', 'h2']) expect(f.expanded.has(a)).toBe(true);
  });
  it('知らない node は無視する', () => {
    expect(initialFocus(ix, 'h-nothing').selected).toBe('h0');
  });
  it('縦では、画面の高さに入る段まで開く', () => {
    expect([...openLevels(ix, 4, new Set())].sort()).toEqual(['h0', 'h1', 's1']);
  });
});

describe('重ねたときの最初の表示', () => {
  it('変わるノードの祖先を開き、最初に変わるノードを選び、そのまわりを収める', async () => {
    const { buildIndex } = await import('./model');
    const { overlayFocus } = await import('./focus');
    const n = (id: string, p: string | null, t: 'hypothesis' | 'story') => (t === 'hypothesis'
      ? { node_type: 'hypothesis', id, parent_story_id: p, statement: id, value: null, leaf: true, needs_reconsideration: false, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] }
      : { node_type: 'story', id, parent_hypothesis_id: p, title: id, summary: '', state: 'active', value: null, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] });
    // h0 ─ s1 ─ h1 ─ s2 ─ h2（変わる）
    const ix = buildIndex([n('h0', null, 'hypothesis'), n('s1', 'h0', 'story'), n('h1', 's1', 'hypothesis'), n('s2', 'h1', 'story'), n('h2', 's2', 'hypothesis')] as never);
    const f = overlayFocus(ix, new Map([['h2', { kind: 'added' as const }]]), null);
    expect(f.selected).toBe('h2');
    expect([...f.expanded]).toEqual(expect.arrayContaining(['h0', 's1', 'h1', 's2']));
    expect([...f.around!].sort()).toEqual(['h2', 's2']);
    expect(overlayFocus(ix, new Map([['h2', { kind: 'added' as const }]]), 's1').selected).toBe('s1');
  });
});
