// BE の API を呼ぶ（design/api/openapi.yaml）。アクセストークンを付け、401 をサインインに渡す。
import { getAuth, markVerified, onUnauthorized } from './auth';
import type { Node } from './model';

export class Problem extends Error {
  constructor(public status: number, public code: string, public detail: string) {
    super(`${code}: ${detail}`);
  }
}

/** BE に要求を送る。アクセストークンを付け、401 をサインインに渡し、ほかのエラーは Problem にする。 */
export async function send(path: string, init: RequestInit = {}): Promise<Response> {
  const a = getAuth();
  if (a.kind !== 'signed-in') throw new Problem(401, 'unauthenticated', 'サインインしていない');
  const r = await fetch('/v1' + path, { ...init, headers: { ...(init.headers ?? {}), Authorization: `Bearer ${a.token}` } });
  if (r.status === 401) {
    onUnauthorized();
    throw new Problem(401, 'unauthenticated', '');
  }
  if (!r.ok) {
    let code = 'internal', detail = '';
    try { const p = await r.json(); code = p.code ?? code; detail = p.detail ?? ''; } catch { /* 本文がない */ }
    throw new Problem(r.status, code, detail);
  }
  markVerified();
  return r;
}

export const call = async <T>(path: string, init: RequestInit = {}): Promise<T> => (await (await send(path, init)).json()) as T;

export interface Repository {
  name: string;
  git_url: string;
  root_hypothesis_id: string;
  root_statement: string | null;
  root_value: boolean | null;
}

interface Page<T> { items: T[]; next_cursor: string | null }

/** 一覧を、next_cursor が null になるまで読む。 */
export async function listAll<T>(path: string): Promise<T[]> {
  const out: T[] = [];
  let cursor: string | null = null;
  do {
    const sep = path.includes('?') ? '&' : '?';
    const p: Page<T> = await call<Page<T>>(path + sep + 'limit=200' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''));
    out.push(...p.items);
    cursor = p.next_cursor;
  } while (cursor);
  return out;
}

export const listRepositories = () => listAll<Repository>('/repositories');
export const getRepository = (repo: string) => call<Repository>(`/repositories/${encodeURIComponent(repo)}`);

// --- スナップショットツリー（design/api/openapi.yaml の listNodes、getNode、getNodeHistory） ---

export interface NodeList { commit: string; nodes: Node[] }
export interface Artifact { kind: 'file' | 'link'; name: string; sha256?: string; uri?: string; media_type?: string }
export interface Citation { target_kind: 'artifact' | 'subtree'; sha256?: string; node_id?: string; commit?: string }
export interface Verdict {
  id: string; hypothesis_id: string; value: boolean; report_body: string; artifacts: Artifact[]; citations: Citation[];
  supersedes?: string[]; apply_request_id?: string; submitted_commit: string; superseded_commit?: string | null;
}
export interface NodeDetail { commit: string; node: Node; children: Node[]; verdict?: Verdict | null; cited_by: string[] }
export interface HistoryEntry { commit: string; committed_at: string; apply_request_id: string | null; session_ids: string[]; reflected: boolean }

const enc = encodeURIComponent;
/** 木を読む時点。どちらもなければ今の main。commit は main かブランチのコミット、apply_request は反映したあとの姿。 */
export interface At { commit?: string; apply_request?: string }
const atQuery = (at: At = {}) => {
  const q = new URLSearchParams();
  if (at.commit) q.set('commit', at.commit);
  if (at.apply_request) q.set('apply_request', at.apply_request);
  const s = q.toString();
  return s ? '?' + s : '';
};
export const listNodes = (repo: string, at?: At) => call<NodeList>(`/repositories/${enc(repo)}/tree/nodes${atQuery(at)}`);
export const getNode = (repo: string, id: string, signal?: AbortSignal, at?: At) => call<NodeDetail>(`/repositories/${enc(repo)}/nodes/${enc(id)}${atQuery(at)}`, { signal });
export const getNodeHistory = (repo: string, id: string, signal?: AbortSignal) =>
  call<{ items: HistoryEntry[]; next_cursor: string | null }>(`/repositories/${enc(repo)}/nodes/${enc(id)}/history?limit=20`, { signal });

/** artifact のファイルを読む。保存させるだけにする（design/webui/security.md の「ファイルの開き方」）。 */
export async function fetchArtifact(repo: string, sha256: string): Promise<Blob> {
  return (await send(`/repositories/${enc(repo)}/artifacts/${enc(sha256)}`)).blob();
}

/** 取り込み中（snapshot_pending）のあいだ読み直す（design/webui/snapshot-tree-viewer.md の「取り込み中の読み直し」）。
 * 2 秒から始め、読み直すたびに 2 倍、最大 30 秒の間隔。5 分たったら諦める。 */
export async function withPendingRetry<T>(f: () => Promise<T>, onPending: (waiting: boolean) => void, now = () => Date.now(), sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))): Promise<T> {
  const start = now();
  let wait = 2000;
  for (;;) {
    try {
      const v = await f();
      onPending(false);
      return v;
    } catch (e) {
      if (!(e instanceof Problem && e.code === 'snapshot_pending')) throw e;
      if (now() - start >= 5 * 60_000) { onPending(false); throw new Problem(503, 'snapshot_pending_timeout', ''); }
      onPending(true);
      await sleep(wait);
      wait = Math.min(wait * 2, 30_000);
    }
  }
}

/** parent でやめたときと、ms たったときに、やめる signal。done で時計を止める。
 * AbortSignal.any（Safari 17.4 から）と AbortSignal.timeout（Safari 16 から）は使わない。古い iOS でも止まるように。 */
export function abortAfter(parent: AbortSignal, ms: number): { signal: AbortSignal; done: () => void } {
  const ctrl = new AbortController();
  const stop = () => { clearTimeout(t); ctrl.abort(); };
  const t = setTimeout(stop, ms);
  if (parent.aborted) stop();
  else parent.addEventListener('abort', stop, { once: true });
  return { signal: ctrl.signal, done: () => { clearTimeout(t); parent.removeEventListener('abort', stop); } };
}

// --- apply request と、main のコミット（design/webui/overlay.md） ---

export type Operation =
  | { kind: 'story.create'; position: number; story_id: string; parent_hypothesis_id: string; title: string; summary: string; hypotheses: { id: string; statement: string }[] }
  | { kind: 'story.inactivate'; position: number; story_id: string; reason: string }
  | { kind: 'verdict.submit'; position: number; verdict_id: string; hypothesis_id: string; value: boolean; report_body: string }
  | { kind: 'node.edit'; position: number; hypothesis_id: string; append_background: string };
export interface ApplyRequest {
  id: string; state: 'open' | 'merged' | 'closed'; branch: string; target: string; description?: string; head_commit: string;
  head_commits?: { commit: string; base_commit: string; seen_at: string | null; author_session_id: string | null }[];
  operations?: Operation[];
  reviews?: { id: string; kind: 'approve' | 'request_changes' | 'comment'; commit: string; body?: string; reviewer_session_id: string; valid: boolean }[];
  merge_failures?: { commit: string; reason: string; detail: string }[];
  awaiting?: 'review' | 'fix' | null; closed_reason?: string | null; warnings?: string[]; merged_commit?: string | null;
}
export const listOpenApplyRequests = (repo: string) => listAll<ApplyRequest>(`/repositories/${enc(repo)}/apply-requests?state=open`);
export const listApplyRequests = (repo: string, state: string) => listAll<ApplyRequest>(`/repositories/${enc(repo)}/apply-requests${state ? '?state=' + enc(state) : ''}`);
export const getApplyRequest = (repo: string, id: string) => call<ApplyRequest>(`/repositories/${enc(repo)}/apply-requests/${enc(id)}`);

export interface MainCommit { number: number; commit: string; committed_at: string; apply_request_id: string | null }
export const listCommits = (repo: string, cursor?: string | null) =>
  call<{ items: MainCommit[]; next_cursor: string | null }>(`/repositories/${enc(repo)}/commits?limit=50${cursor ? '&cursor=' + enc(cursor) : ''}`);

// --- repository の作成、ダッシュボード（design/webui/screens.md） ---

/** repository を作る。応答の Logiforge-Pending-Commit があれば、スナップショットツリーはまだ取り込み中である。 */
export async function createRepository(name: string, statement: string, background: string): Promise<{ repo: Repository; pending: boolean }> {
  const r = await send('/repositories', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(background ? { name, statement, background } : { name, statement }),
  });
  return { repo: (await r.json()) as Repository, pending: r.headers.has('Logiforge-Pending-Commit') };
}

export interface Completion { commit: string; root_value: boolean | null; decided: boolean; remaining_work: number; done: boolean }
export const getCompletion = (repo: string) => call<Completion>(`/repositories/${enc(repo)}/tree/completion`);
/** 着手できる作業の、種類ごとの件数（ページに関わらず全体。listWork の counts） */
export const workCounts = (repo: string) => call<{ counts: Record<string, number> }>(`/repositories/${enc(repo)}/work?limit=1`).then((r) => r.counts);

// --- 人への依頼（design/webui/screens.md の「人への依頼」） ---

export interface HumanRequest {
  id: string; node_id: string; kind: 'pvr' | 'decision'; body: string; state: 'open' | 'answered';
  answer?: { body: string; attachments: Artifact[]; answered_at: string } | null;
}
export type AttachmentInput = { kind: 'file'; name: string; media_type?: string; content_base64: string } | { kind: 'link'; name: string; uri: string };

export function listHumanRequests(repo: string, kind: string, state: string) {
  const q = new URLSearchParams();
  if (kind) q.set('kind', kind);
  if (state) q.set('state', state);
  const s = q.toString();
  return listAll<HumanRequest>(`/repositories/${enc(repo)}/human-requests${s ? '?' + s : ''}`);
}
export const getHumanRequest = (repo: string, id: string) => call<HumanRequest>(`/repositories/${enc(repo)}/human-requests/${enc(id)}`);
export const answerHumanRequest = (repo: string, id: string, body: string, attachments: AttachmentInput[]) =>
  call<HumanRequest>(`/repositories/${enc(repo)}/human-requests/${enc(id)}/answer`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(attachments.length ? { body, attachments } : { body }),
  });
export async function fetchAnswerAttachment(repo: string, id: string, sha256: string): Promise<Blob> {
  return (await send(`/repositories/${enc(repo)}/human-requests/${enc(id)}/attachments/${enc(sha256)}`)).blob();
}

/** 作業として残っているものの対象（listWork の kind で絞り、subject_id を集める）。ダッシュボードの件数と同じ数になる */
export async function workSubjects(repo: string, kind: string): Promise<Set<string>> {
  return new Set((await workItems(repo, kind)).map((i) => i.subject_id));
}
/** 作業として残っているものの対象のノード（node_id） */
export async function workNodes(repo: string, kind: string): Promise<Set<string>> {
  return new Set((await workItems(repo, kind)).flatMap((i) => (i.node_id ? [i.node_id] : [])));
}
const workItems = (repo: string, kind: string) => listAll<{ kind: string; subject_id: string; node_id?: string }>(`/repositories/${enc(repo)}/work?kind=${enc(kind)}`);
