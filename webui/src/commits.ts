// ビュワーの「時点」の選択肢に出す main のコミット（design/webui/overlay.md の「過去の時点」）。
// 新しい 50 個を読み、「さらに古いコミット…」で next_cursor から続きを読んで後ろに足す。
import type { MainCommit } from './api';

export interface CommitPage { items: MainCommit[]; next_cursor: string | null }
export interface CommitState { items: MainCommit[]; next: string | null }

/**
 * 続きを読むあいだは、もう一度読まない（遅い回線で 2 回選んでも、同じ 50 個を 2 回足さない）。
 * 読んだものに同じコミットがあれば、足さない。
 */
export function commitPager(load: (cursor: string | null) => Promise<CommitPage>, set: (s: CommitState) => void) {
  let items: MainCommit[] = [];
  let next: string | null = null;
  let loading = false;
  const apply = (p: CommitPage, append: boolean) => {
    const seen = new Set(items.map((c) => c.commit));
    items = append ? [...items, ...p.items.filter((c) => !seen.has(c.commit))] : p.items;
    next = p.next_cursor;
    set({ items, next });
  };
  return {
    first: () => load(null).then((p) => apply(p, false)),
    more: async () => {
      if (!next || loading) return;
      loading = true;
      try { apply(await load(next), true); } finally { loading = false; }
    },
  };
}
