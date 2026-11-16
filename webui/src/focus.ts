// 最初に開くノードと、選ぶノードを決める（design/webui/snapshot-tree-viewer.md の「折りたたみ」）。
// 決め方を差し替えやすいように、ビュワーから分けておく（「ここから見る」や、変更のまわりを最初に示す決め方を足すため）。
import { ancestors, changeOrder, parentOf, type Change, type Index } from './model';

export interface Focus {
  /** 表示の根。今は、いつもスナップショットツリーの根。 */
  root: string;
  /** 最初に開くノード */
  expanded: Set<string>;
  /** 最初に選ぶノード */
  selected: string;
  /** 図で、最初に収めるノード（まわり）。null なら全体（design/webui/graph-navigation.md の「まわりを収める」） */
  around: Set<string> | null;
}

/** 根から 2 段（根のストーリーと、その子の仮説）までを開く。URL の node があれば、その祖先も開いて選ぶ。 */
export function initialFocus(ix: Index, node: string | null): Focus {
  const expanded = new Set(ix.nodes.filter((n) => (ix.depth.get(n.id) ?? 0) < 2).map((n) => n.id));
  let selected = ix.rootId;
  if (node && ix.byId.has(node)) {
    for (const a of ancestors(ix, node)) expanded.add(a);
    selected = node;
  }
  return { root: ix.rootId, expanded, selected, around: null };
}

/** apply request を重ねたとき（design/webui/overlay.md）。直接に変わるノードの祖先を開き、最初に直接に変わるノードを選ぶ（URL の node があれば、それ）。
 * 図では、直接に変わるノードとその親のまわりを最初に収める。伝播だけで変わる祖先は、まわりに入れない。 */
export function overlayFocus(ix: Index, changes: Map<string, Change>, node: string | null): Focus {
  const f = initialFocus(ix, node);
  const order = changeOrder(ix, changes);
  if (order.length === 0) return f;
  const around = new Set<string>();
  for (const id of order) {
    for (const a of ancestors(ix, id)) f.expanded.add(a);
    around.add(id);
    const p = parentOf(ix.byId.get(id)!);
    if (p) around.add(p);
  }
  if (!(node && ix.byId.has(node))) f.selected = order[0];
  return { ...f, around };
}

/** 縦にしたときに開く段（デスクトップ 4 段、モバイル 3 段。design/webui/graph-navigation.md） */
export function openLevels(ix: Index, levels: number, base: Set<string>): Set<string> {
  const s = new Set(base);
  for (const n of ix.nodes) if ((ix.depth.get(n.id) ?? 0) < levels - 1) s.add(n.id);
  return s;
}
