// ビュワーが示すスナップショットツリーの時点（design/webui/overlay.md）。今の main、過去の時点（main のコミット）、apply request を重ねた姿。
import { getApplyRequest, listCommits, listNodes, Problem, withPendingRetry, type ApplyRequest, type MainCommit } from './api';
import { buildIndex, diff, withRemoved, type Change, type Index, type Node } from './model';

export type Source = { kind: 'main' } | { kind: 'commit'; commit: string } | { kind: 'ar'; id: string };

export function sourceFromURL(q: URLSearchParams): Source {
  const ar = q.get('apply_request'), commit = q.get('commit');
  if (ar) return { kind: 'ar', id: ar };
  if (commit) return { kind: 'commit', commit };
  return { kind: 'main' };
}

export function sourceToURL(s: Source, q: URLSearchParams) {
  q.delete('apply_request'); q.delete('commit');
  if (s.kind === 'ar') q.set('apply_request', s.id);
  if (s.kind === 'commit') q.set('commit', s.commit);
}

export interface Loaded {
  ix: Index;
  commit: string; // 示しているスナップショットツリーのコミット（重ねたときは、比べた向き先のコミット）
  changes: Map<string, Change> | null; // 重ねたときだけ
  removed: string[];
  ar?: ApplyRequest;
  past?: MainCommit | { commit: string; number?: undefined; committed_at?: undefined };
}

/** 重ねられないとき。示すのは理由だけ */
export interface Blocked { reason: 'conflict' | 'gone' | 'not-open'; ar?: ApplyRequest; id: string; detail: string }

export async function loadSource(repo: string, s: Source, onPending: (w: boolean) => void): Promise<Loaded | Blocked> {
  if (s.kind === 'main') {
    const r = await withPendingRetry(() => listNodes(repo), onPending);
    return { ix: buildIndex(r.nodes), commit: r.commit, changes: null, removed: [] };
  }
  if (s.kind === 'commit') {
    const r = await withPendingRetry(() => listNodes(repo, { commit: s.commit }), onPending);
    // 過去の時点では、審査中の apply request と open の人への依頼の札は、今のものなので示さない
    const nodes = r.nodes.map((n) => ({ ...n, open_apply_request_ids: [], open_human_request_ids: [] }) as Node);
    return { ix: buildIndex(nodes), commit: r.commit, changes: null, removed: [], past: (await findCommit(repo, r.commit)) ?? { commit: r.commit } };
  }
  let ar: ApplyRequest | undefined;
  try {
    ar = await getApplyRequest(repo, s.id);
    if (ar.state !== 'open') return { reason: 'not-open', ar, id: s.id, detail: '' };
    // 1. 反映したあと（と、比べる相手の候補の今の main）を読む
    const [after, main] = await Promise.all([
      withPendingRetry(() => listNodes(repo, { apply_request: s.id }), onPending),
      withPendingRetry(() => listNodes(repo), onPending),
    ]);
    // 2. 重ねた向き先のコミットが手元の main と同じなら、それと比べる。違えば（main が進んだ、向き先が main でない）読み直す
    const beforeNodes = main.commit === after.commit ? main.nodes : (await withPendingRetry(() => listNodes(repo, { commit: after.commit }), onPending)).nodes;
    const before = buildIndex(beforeNodes), afterIx = buildIndex(after.nodes);
    const d = diff(before, afterIx);
    return { ix: buildIndex(withRemoved(afterIx, before, d.removed)), commit: after.commit, changes: d.changes, removed: d.removed, ar };
  } catch (e) {
    if (e instanceof Problem && e.code === 'overlay_conflict') return { reason: 'conflict', ar, id: s.id, detail: e.detail };
    if (e instanceof Problem && e.code === 'not_open') return { reason: 'not-open', ar, id: s.id, detail: '' };
    if (e instanceof Problem && e.code === 'not_found') return { reason: 'gone', ar, id: s.id, detail: e.detail };
    throw e;
  }
}

export const isBlocked = (x: Loaded | Blocked): x is Blocked => 'reason' in x;

/** main のコミットの番号と時刻を、listCommits から探す（新しい順に、5 ページまで）。 */
async function findCommit(repo: string, sha: string): Promise<MainCommit | null> {
  let cursor: string | null = null;
  for (let i = 0; i < 5; i++) {
    const p: { items: MainCommit[]; next_cursor: string | null } = await listCommits(repo, cursor);
    const hit = p.items.find((c) => c.commit === sha);
    if (hit) return hit;
    cursor = p.next_cursor;
    if (!cursor) break;
  }
  return null;
}
