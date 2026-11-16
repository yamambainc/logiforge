// BE の API を呼ぶ（design/api/openapi.yaml）。アクセストークンを付け、401 をサインインに渡す。
import { getAuth, markVerified, onUnauthorized } from './auth';

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
