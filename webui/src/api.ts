// BE の API を呼ぶ（design/api/openapi.yaml）。アクセストークンを付け、401 をサインインに渡す。
import { getAuth, markVerified, onUnauthorized } from './auth';
import type { Node } from './model';

export class Problem extends Error {
  constructor(public status: number, public code: string, public detail: string) {
    super(`${code}: ${detail}`);
  }
}

export async function call<T>(path: string, init: RequestInit = {}): Promise<T> {
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
  return (await r.json()) as T;
}

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
export const listNodes = (repo: string) => call<NodeList>(`/repositories/${enc(repo)}/tree/nodes`);
export const getNode = (repo: string, id: string, signal?: AbortSignal) => call<NodeDetail>(`/repositories/${enc(repo)}/nodes/${enc(id)}`, { signal });
export const getNodeHistory = (repo: string, id: string, signal?: AbortSignal) =>
  call<{ items: HistoryEntry[]; next_cursor: string | null }>(`/repositories/${enc(repo)}/nodes/${enc(id)}/history?limit=20`, { signal });

/** artifact のファイルを読む。保存させるだけにする（design/webui/security.md の「ファイルの開き方」）。 */
export async function fetchArtifact(repo: string, sha256: string): Promise<Blob> {
  const a = getAuth();
  if (a.kind !== 'signed-in') throw new Problem(401, 'unauthenticated', '');
  const r = await fetch(`/v1/repositories/${enc(repo)}/artifacts/${enc(sha256)}`, { headers: { Authorization: `Bearer ${a.token}` } });
  if (r.status === 401) { onUnauthorized(); throw new Problem(401, 'unauthenticated', ''); }
  if (!r.ok) throw new Problem(r.status, 'not_found', '');
  markVerified();
  return r.blob();
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
