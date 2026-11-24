// ビュワーで inactive なノードを示すか（design/webui/viewer-search.md の「inactive を示す」）。
// - 利用者が切り替えたら localStorage に置き、次に開いたときも同じにする。repository と、/tree・apply request の木とで共通。
// - 置いたものがないときは SHOW_INACTIVE_DEFAULT にする。
import type { Index } from './model';
import { hiddenWhenInactiveHidden, parentOf } from './model';

export { hiddenWhenInactiveHidden };

/** 置いたものがないときに示すか。初めて開いたときの見え方は、ここだけで決める */
export const SHOW_INACTIVE_DEFAULT = false;
const KEY = 'lf-show-inactive';

export function loadShowInactive(): boolean {
  try {
    const v = localStorage.getItem(KEY);
    return v === null ? SHOW_INACTIVE_DEFAULT : v === '1';
  } catch {
    return SHOW_INACTIVE_DEFAULT;
  }
}

export function saveShowInactive(v: boolean) {
  try { localStorage.setItem(KEY, v ? '1' : '0'); } catch { /* 置けなくても、いまの画面では切り替わる */ }
}

/** inactive を隠しても見えている、いちばん近い祖先（自分が見えていれば自分）。根まで見えなければ null */
export function nearestShown(ix: Index, id: string): string | null {
  let top: string | null = null; // いちばん上の inactive
  for (let n = ix.byId.get(id); n; ) {
    if (n.inactive) top = n.id;
    const p = parentOf(n);
    n = p === null ? undefined : ix.byId.get(p);
  }
  if (top === null) return id;
  const t = ix.byId.get(top)!;
  return parentOf(t);
}
