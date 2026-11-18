// WebUI からのレビュー（docs/webui/history.md の「apply request の閲覧」、design/webui/screens.md の「apply request と履歴」、
// design/settings/users.md の「WebUI からのレビュー」）。画面に依らない部分：セッション ID、送り方、エラーの言葉、結果の言葉。
import { getApplyRequest, Problem, reviewApplyRequest, type ApplyRequest, type ReviewKind } from './api';
import { FAILURE } from './ar';

const SESSION_KEY = 'lf-session-id';

/** このタブの Logiforge-Session-Id。タブごとに乱数で 1 度だけ作り、sessionStorage に置いて使い回す。
 * ブラウザでタブを「複製」すると sessionStorage も写るので、2 つのタブが同じ値になる。どちらも本人なので、受け入れる */
export function sessionId(): string {
  const v = sessionStorage.getItem(SESSION_KEY);
  if (v) return v;
  const b = crypto.getRandomValues(new Uint8Array(16));
  const id = 'webui-' + [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
  sessionStorage.setItem(SESSION_KEY, id);
  return id;
}

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

export function reviewErrorMessage(e: unknown): string | null {
  if (e instanceof Problem) {
    if (e.status === 401) return null;
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
  | { kind: 'error'; message: string }
  | { kind: 'unauthenticated' };

export const CONFLICT_MESSAGE = '先頭のコミットが新しくなりました。新しい先頭のコミットと差分を確かめて、もう一度送ってください。';

/** レビューを送る。commit は、画面で示した（確かめた）先頭のコミット */
export async function submitReview(repo: string, id: string, kind: ReviewKind, commit: string, body: string): Promise<SendResult> {
  try {
    const r = await reviewApplyRequest(repo, id, sessionId(), { kind, commit, body: body.trim() });
    return { kind: 'sent', ar: r.apply_request, message: outcome(kind, r.apply_request, commit) };
  } catch (e) {
    if (e instanceof Problem && e.status === 401) return { kind: 'unauthenticated' };
    if (e instanceof Problem && (e.code === 'commit_conflict' || e.code === 'not_open')) {
      const ar = await getApplyRequest(repo, id).catch(() => null);
      return e.code === 'commit_conflict'
        ? { kind: 'conflict', ar, message: CONFLICT_MESSAGE }
        : { kind: 'not_open', ar, message: reviewErrorMessage(e)! };
    }
    return { kind: 'error', message: reviewErrorMessage(e) ?? '' };
  }
}

/** 送ったあとに示す言葉。approve なら、反映したか、反映できなかった理由（apply_request.merge_failures） */
export function outcome(kind: ReviewKind, ar: ApplyRequest, commit: string): string {
  if (kind !== 'approve') return kind === 'comment' ? 'コメントを付けました。' : 'request_changes を付けました。';
  if (ar.state === 'merged') return 'approve を付け、反映しました。';
  if (ar.target !== 'main') return 'approve を付けました。main 向きではないので、Logiforge は反映しません。';
  const fails = (ar.merge_failures ?? []).filter((f) => f.commit === commit);
  if (fails.length > 0) {
    const f = fails[fails.length - 1];
    return `approve を付けましたが、反映できませんでした：${FAILURE[f.reason] ?? f.reason}${f.detail ? `（${f.detail}）` : ''}`;
  }
  return 'approve を付けました。反映の条件をまだ満たしていないので、反映していません。';
}

export const OWN_AR = 'あなたが作った apply request です。';
/** approve の前に示すこと：自分が作った apply request なら OWN_AR（opened_by_caller） */
export const ownNotice = (a: ApplyRequest): string | null => (a.opened_by_caller ? OWN_AR : null);

/** approve を確かめる欄に示すこと：レビューする先頭のコミット、差分の操作の数、反映するか、自分が作ったか */
export function approveSummary(a: ApplyRequest, commit: string, operations: number): { commit: string; lines: string[]; own: string | null } {
  return {
    commit,
    lines: [`差分の操作 ${operations} 個。`, a.target === 'main' ? '反映の条件を満たしたら、そのまま main に反映します。' : 'main 向きではないので、Logiforge は反映しません。'],
    own: ownNotice(a),
  };
}
