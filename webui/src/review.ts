// WebUI からのレビュー（docs/webui/history.md の「apply request の閲覧」、design/webui/screens.md の「apply request と履歴」、
// design/settings/users.md の「WebUI からのレビュー」）。画面に依らない部分：送り方、エラーの言葉、結果の言葉。
import { getApplyRequest, mergeApplyRequest, Problem, reviewApplyRequest, type ApplyRequest, type ReviewKind } from './api';
import { FAILURE } from './ar';

/** approve を送ってよいか。確かめたコミットが、今示している先頭のコミットと同じときだけ。
 * 先頭のコミットが変わったら（commit_conflict で読み直したときなど）、確かめ直させる */
export const canSendApprove = (confirmed: string | null, shown: string) => confirmed !== null && confirmed === shown;

/** レビューを送ったときのエラーの言葉。401 は、サインインに任せるので null */
export const REVIEW_ERRORS: Record<string, string> = {
  forbidden: 'レビューできるのは、この repository に Write がある人だけです。',
  same_session: 'このタブのセッションは、差分を書いたセッションと同じです。差分を書いたセッションからは、レビューできません。',
  snapshot_pending: '先頭のコミットを、まだ取り込んでいるところです。少し待ってから、もう一度送ってください。',
  not_open: 'この apply request には、もうレビューできません（open でないか、レビューの鎖が切れています）。',
  unavailable: 'GitForge を読めませんでした。少し待ってから、もう一度送ってください。',
  validation_failed: '送った中身を受け付けませんでした。',
  session_required: 'セッション ID を送れませんでした。ページを読み直してください。',
};

/** approve と merge を断った理由のときの言葉。what は「approve を出しませんでした」「merge しませんでした」 */
export function refusalMessage(what: string, e: Problem): string {
  return `${what}：${FAILURE[e.code] ?? e.code}${e.detail ? `（${e.detail}）` : ''}`;
}

export function reviewErrorMessage(e: unknown): string | null {
  if (e instanceof Problem) {
    if (e.status === 401) return null;
    if (FAILURE[e.code] && e.code !== 'snapshot_pending') return refusalMessage('approve を出しませんでした', e);
    const m = REVIEW_ERRORS[e.code];
    if (m) return e.detail ? `${m}（${e.detail}）` : m;
    return e.detail || e.code;
  }
  return String(e);
}

export type SendResult =
  | { kind: 'sent'; ar: ApplyRequest; message: string }
  // 先頭のコミットが変わった。読み直した apply request の新しい先頭を示し、もう一度確かめさせる
  | { kind: 'conflict'; ar: ApplyRequest | null; message: string }
  // open でなくなった（ほかの人が先に反映した、閉じた、鎖が切れた）。読み直した apply request を示す
  | { kind: 'not_open'; ar: ApplyRequest | null; message: string }
  // 今の main での確かめに通らなかった（approve を出さなかった、merge しなかった）。読み直した apply request を示す
  | { kind: 'refused'; ar: ApplyRequest | null; message: string }
  | { kind: 'error'; message: string }
  | { kind: 'unauthenticated' };

export const CONFLICT_MESSAGE = '先頭のコミットが新しくなりました。新しい先頭のコミットと差分を確かめて、もう一度送ってください。';

/** レビューを送る。commit は、画面で示した（確かめた）先頭のコミット */
export async function submitReview(repo: string, id: string, kind: ReviewKind, commit: string, body: string): Promise<SendResult> {
  try {
    const r = await reviewApplyRequest(repo, id, { kind, commit, body: body.trim() });
    return { kind: 'sent', ar: r.apply_request, message: outcome(kind, r.apply_request) };
  } catch (e) {
    if (e instanceof Problem && e.status === 401) return { kind: 'unauthenticated' };
    // 確かめに通らなければ、修正待ちになっていることがあるので、読み直す
    if (e instanceof Problem && REFUSALS.includes(e.code)) {
      const ar = await getApplyRequest(repo, id).catch(() => null);
      return { kind: 'refused', ar, message: reviewErrorMessage(e)! };
    }
    if (e instanceof Problem && (e.code === 'commit_conflict' || e.code === 'not_open')) {
      const ar = await getApplyRequest(repo, id).catch(() => null);
      return e.code === 'commit_conflict'
        ? { kind: 'conflict', ar, message: CONFLICT_MESSAGE }
        : { kind: 'not_open', ar, message: reviewErrorMessage(e)! };
    }
    return { kind: 'error', message: reviewErrorMessage(e) ?? '' };
  }
}

/** 今の main での確かめに通らなかったときの code（design/backend/merge.md）。snapshot_pending は、送り直せばよいので含めない */
const REFUSALS = ['stale_target', 'conflict', 'invariant_violation'];

/** 送ったあとに示す言葉 */
export function outcome(kind: ReviewKind, ar: ApplyRequest): string {
  if (kind !== 'approve') return kind === 'comment' ? 'コメントを付けました。' : 'request_changes を付けました。';
  if (ar.target !== 'main') return 'approve を付けました。main 向きではないので、Logiforge は確かめていません。merge は GitForge で行ってください。';
  return 'approve を付けました。merge は、別に行います。';
}

/** merge の欄を示すか：open で main 向きで、承認済みのとき（docs/apply-request/review.md の「merge」）。
 * 1 件を読んだ apply request では、承認の状態がいつも入る */
export const canMerge = (a: ApplyRequest): boolean => a.state === 'open' && a.target === 'main' && a.review_decision === 'approved';

export const MERGE_ERRORS: Record<string, string> = {
  forbidden: 'merge できるのは、この repository に Write がある人だけです。',
  commit_conflict: '先頭のコミットが新しくなりました。新しい先頭のコミットを確かめてから、merge を送り直してください（approve が外れていれば、レビューを受け直してください）。',
  snapshot_pending: REVIEW_ERRORS.snapshot_pending,
  unavailable: REVIEW_ERRORS.unavailable,
};

/** merge を送る。直前に、バックエンドが反映の条件を確かめ直す */
export async function submitMerge(repo: string, id: string): Promise<SendResult> {
  try {
    const r = await mergeApplyRequest(repo, id);
    return { kind: 'sent', ar: r, message: 'merge しました。' };
  } catch (e) {
    if (!(e instanceof Problem)) return { kind: 'error', message: String(e) };
    if (e.status === 401) return { kind: 'unauthenticated' };
    if (FAILURE[e.code] && e.code !== 'snapshot_pending') {
      const ar = await getApplyRequest(repo, id).catch(() => null);
      return { kind: 'refused', ar, message: refusalMessage('merge しませんでした', e) };
    }
    const m = MERGE_ERRORS[e.code];
    return { kind: 'error', message: m ? (e.detail ? `${m}（${e.detail}）` : m) : e.detail || e.code };
  }
}

export const OWN_AR = 'あなたが作った apply request です。';
/** approve の前に示すこと：自分が作った apply request なら OWN_AR（opened_by_caller） */
export const ownNotice = (a: ApplyRequest): string | null => (a.opened_by_caller ? OWN_AR : null);

/** approve を確かめる欄に示すこと：レビューする先頭のコミット、差分の操作の数、反映するか、自分が作ったか */
export function approveSummary(a: ApplyRequest, commit: string, operations: number): { commit: string; lines: string[]; own: string | null } {
  return {
    commit,
    lines: [`差分の操作 ${operations} 個。`, ...(a.target === 'main' ? [] : ['main 向きではないので、Logiforge は確かめません。'])],
    own: ownNotice(a),
  };
}
