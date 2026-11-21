// apply request の差分とレビューの言葉（docs/webui/history.md）
import type { ApplyRequest, Operation, Review } from './api';

/** レビュー（議論を含む）を、書いた時刻の順に並べる。時刻が読めないものは、もとの順のまま最後に置く */
export function byTime(rs: Review[]): Review[] {
  const t = (r: Review) => (r.created_at ? Date.parse(r.created_at) : Infinity);
  return rs.map((r, i) => ({ r, i })).sort((a, b) => t(a.r) - t(b.r) || a.i - b.i).map((x) => x.r);
}

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
  stale_target: 'ブランチを今の main に合わせる必要がある', invariant_violation: '決まりに沿わない', conflict: 'git の衝突', snapshot_pending: '取り込み中で確かめられなかった',
  merge_forbidden: 'GitForge で merge する権限がなく、GitForge が merge を断った',
};

/** 一覧の 1 行の見出し。説明の最初の行、なければブランチ */
export const arTitle = (a: ApplyRequest) => (a.description ?? '').split('\n').find((l) => l.trim())?.replace(/^#+\s*/, '') || a.branch;

/** 作業としての絞り込み：apply request の一覧から、作業の対象（識別子の集まり）だけを、一覧の順のまま残す */
export const filterBySubjects = (all: ApplyRequest[], ids: Set<string>) => all.filter((a) => ids.has(a.id));
