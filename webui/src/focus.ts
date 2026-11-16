// 最初に開くノードと、選ぶノードを決める（design/webui/snapshot-tree-viewer.md の「折りたたみ」）。
// 決め方は、ビュワーから分けておく（「ここから見る」、node= のまわり、変更のまわり）。
import { ancestors, changeOrder, parentOf, type Change, type Index } from './model';

export interface Focus {
  /** 表示の根。「ここから見る」で選んだノード（design/webui/snapshot-tree-viewer.md の「ここから見る」）。なければスナップショットツリーの根 */
  root: string;
  /** 最初に開くノード */
  expanded: Set<string>;
  /** 最初に選ぶノード */
  selected: string;
  /** 図で、最初に収めるノード（まわり）。null なら全体（design/webui/graph-navigation.md の「まわりを収める」） */
  around: Set<string> | null;
}

/** id が、root の部分木にあるか（root そのものを含む） */
export function inSubtree(ix: Index, id: string, root: string): boolean {
  return id === root || ancestors(ix, id).includes(root);
}

/** 表示の根から 2 段（根のストーリーと、その子の仮説）までを開く。
 * URL の node があれば、そのまわり（親・兄弟・1 段下）を示す：祖先と node を開いて選び、図ではまわりを収める。
 * root（URL の root）がなければ、または node がその部分木の外なら、スナップショットツリーの根から示す。 */
export function initialFocus(ix: Index, node: string | null, root: string | null = null): Focus {
  const want = node && ix.byId.has(node) ? node : null;
  let r = root && ix.byId.has(root) ? root : ix.rootId;
  if (want && !inSubtree(ix, want, r)) r = ix.rootId;
  const base = ix.depth.get(r) ?? 0;
  const expanded = new Set(ix.nodes.filter((n) => (ix.depth.get(n.id) ?? 0) - base < 2 && inSubtree(ix, n.id, r)).map((n) => n.id));
  if (!want) return { root: r, expanded, selected: r, around: null };
  for (const a of ancestors(ix, want)) expanded.add(a);
  expanded.add(want);
  return { root: r, expanded, selected: want, around: neighborhood(ix, want) };
}

/** ノードのまわり：親、兄弟、自分、1 段下の子 */
export function neighborhood(ix: Index, id: string): Set<string> {
  const out = new Set<string>([id, ...(ix.children.get(id) ?? [])]);
  const p = parentOf(ix.byId.get(id)!);
  if (p) { out.add(p); for (const sib of ix.children.get(p) ?? []) out.add(sib); }
  return out;
}

/** apply request を重ねたとき（design/webui/overlay.md）。直接に変わるノードの祖先を開き、最初に直接に変わるノードを選ぶ（URL の node があれば、それ）。
 * 図では、直接に変わるノードとその親のまわりを最初に収める。伝播だけで変わる祖先は、まわりに入れない。 */
export function overlayFocus(ix: Index, changes: Map<string, Change>, node: string | null, root: string | null = null): Focus {
  const f = initialFocus(ix, node, root);
  // 「ここから見る」では、表示の根の部分木の中の変化だけ
  const order = changeOrder(ix, changes).filter((id) => inSubtree(ix, id, f.root));
  if (order.length === 0) return f;
  const around = new Set<string>();
  for (const id of order) {
    for (const a of ancestors(ix, id)) f.expanded.add(a);
    around.add(id);
    const p = parentOf(ix.byId.get(id)!);
    if (p) around.add(p);
  }
  // URL の node があれば、それを選び、そのまわりを収める
  if (f.around) return f;
  f.selected = order[0];
  return { ...f, around };
}

/** 縦にしたときに開く段（デスクトップ 4 段、モバイル 3 段。design/webui/graph-navigation.md） */
export function openLevels(ix: Index, levels: number, base: Set<string>): Set<string> {
  const s = new Set(base);
  for (const n of ix.nodes) if ((ix.depth.get(n.id) ?? 0) < levels - 1) s.add(n.id);
  return s;
}
