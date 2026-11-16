// apply request の差分とレビューの言葉（docs/webui/history.md）
import type { ApplyRequest, Operation } from './api';

/** 操作を、1 行の言葉にする。対象は識別子で示す */
export function opLabel(op: Operation): { what: string; target: string } {
  switch (op.kind) {
    case 'story.create': return { what: `ストーリーを作る「${op.title}」（仮説 ${op.hypotheses.length}）`, target: op.parent_hypothesis_id };
    case 'story.inactivate': return { what: `ストーリーを inactive にする（${op.reason}）`, target: op.story_id };
    case 'verdict.submit': return { what: `検証結果を出す（${op.value ? '真' : '偽'}）`, target: op.hypothesis_id };
    case 'node.edit': return { what: '背景情報を足す', target: op.hypothesis_id };
  }
}

export const REVIEW: Record<string, string> = { approve: 'approve', request_changes: 'request_changes', comment: 'コメント' };
export const STATE: Record<string, string> = { open: 'open', merged: '反映済み', closed: '閉じた' };
export const AWAITING: Record<string, string> = { review: 'レビュー待ち', fix: '修正待ち' };
export const FAILURE: Record<string, string> = {
  stale_target: '対象ノードが先に変わった', invariant_violation: '決まりに沿わない', conflict: 'git の衝突', snapshot_pending: '取り込み中で確かめられなかった',
};

/** 一覧の 1 行の見出し。説明の最初の行、なければブランチ */
export const arTitle = (a: ApplyRequest) => (a.description ?? '').split('\n').find((l) => l.trim())?.replace(/^#+\s*/, '') || a.branch;
