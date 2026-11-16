// 最初に開くノードと、選ぶノードを決める（design/webui/snapshot-tree-viewer.md の「折りたたみ」）。
// 決め方を差し替えやすいように、ビュワーから分けておく（「ここから見る」や、変更のまわりを最初に示す決め方を足すため）。
import { ancestors, type Index } from './model';

export interface Focus {
  /** 表示の根。今は、いつもスナップショットツリーの根。 */
  root: string;
  /** 最初に開くノード */
  expanded: Set<string>;
  /** 最初に選ぶノード */
  selected: string;
}

/** 根から 2 段（根のストーリーと、その子の仮説）までを開く。URL の node があれば、その祖先も開いて選ぶ。 */
export function initialFocus(ix: Index, node: string | null): Focus {
  const expanded = new Set(ix.nodes.filter((n) => (ix.depth.get(n.id) ?? 0) < 2).map((n) => n.id));
  let selected = ix.rootId;
  if (node && ix.byId.has(node)) {
    for (const a of ancestors(ix, node)) expanded.add(a);
    selected = node;
  }
  return { root: ix.rootId, expanded, selected };
}

/** 縦にしたときに開く段（デスクトップ 4 段、モバイル 3 段。design/webui/graph-navigation.md） */
export function openLevels(ix: Index, levels: number, base: Set<string>): Set<string> {
  const s = new Set(base);
  for (const n of ix.nodes) if ((ix.depth.get(n.id) ?? 0) < levels - 1) s.add(n.id);
  return s;
}
