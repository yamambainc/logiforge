import { describe, expect, it } from 'vitest';
import { arTitle, opLabel } from './ar';
import { buildIndex, filterNodes, type Filter, type Node } from './model';

describe('apply request の言葉', () => {
  it('操作を 1 行にする', () => {
    expect(opLabel({ kind: 'verdict.submit', position: 0, verdict_id: 'v-1', hypothesis_id: 'h-1', value: false, report_body: 'r' })).toEqual({ what: '検証結果を出す（偽）', target: 'h-1' });
    expect(opLabel({ kind: 'story.create', position: 0, story_id: 's-1', parent_hypothesis_id: 'h-0', title: '導線', summary: '', hypotheses: [{ id: 'h-2', statement: 'a' }] }).what).toBe('ストーリーを作る「導線」（仮説 1）');
    expect(opLabel({ kind: 'node.edit', position: 0, hypothesis_id: 'h-3', append_background: 'x' })).toEqual({ what: '背景情報を足す', target: 'h-3' });
    expect(opLabel({ kind: 'story.inactivate', position: 0, story_id: 's-2', reason: '出さない' }).target).toBe('s-2');
  });
  it('見出しは、説明の最初の行（# を外す）。なければブランチ', () => {
    expect(arTitle({ id: 'apr_1', state: 'open', branch: 'b3', target: 'main', head_commit: 'c', description: '\n# 検証 2\n本文' })).toBe('検証 2');
    expect(arTitle({ id: 'apr_1', state: 'open', branch: 'b3', target: 'main', head_commit: 'c' })).toBe('b3');
  });
});

describe('引用元が変化（作業）の絞り込み', () => {
  it('作業の一覧の node_id だけを出す', () => {
    const h = (id: string, p: string | null) => ({ node_type: 'hypothesis', id, parent_story_id: p, statement: id, value: null, leaf: p !== null, needs_reconsideration: false, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] }) as Node;
    const s = { node_type: 'story', id: 's1', parent_hypothesis_id: 'h0', title: 's1', summary: '', state: 'active', value: null, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] } as Node;
    const ix = buildIndex([h('h0', null), s, h('h1', 's1'), h('h2', 's1')]);
    expect(filterNodes(ix, '', new Set(['cited'] as Filter[]), true, null, new Set(['h2'])).matches).toEqual(['h2']);
    expect(filterNodes(ix, '', new Set(['cited'] as Filter[]), true, null, null).matches).toEqual([]);
  });
});
