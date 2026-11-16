// API の形（design/api/openapi.yaml の Node）と、ビュワーが使う索引（design/webui/snapshot-tree-viewer.md）。

export type TruthValue = boolean | null;

interface NodeBase {
  id: string;
  value: TruthValue;
  inactive: boolean;
  open_apply_request_ids: string[];
  open_human_request_ids: string[];
}
export interface Hypothesis extends NodeBase {
  node_type: 'hypothesis';
  parent_story_id: string | null;
  statement: string;
  background?: string;
  valid_verdict_id?: string | null;
  leaf: boolean;
  needs_reconsideration: boolean;
}
export interface Story extends NodeBase {
  node_type: 'story';
  parent_hypothesis_id: string;
  title: string;
  summary: string;
  state: 'active' | 'inactive';
  reason?: string | null;
}
export type Node = Hypothesis | Story;

export const parentOf = (n: Node) => (n.node_type === 'hypothesis' ? n.parent_story_id : n.parent_hypothesis_id);
export const textOf = (n: Node) => (n.node_type === 'hypothesis' ? n.statement : n.title);

export interface Stats { t: number; f: number; u: number; total: number }

export interface Index {
  nodes: Node[]; // 深さ優先の順（listNodes の応答の順）
  byId: Map<string, Node>;
  children: Map<string, string[]>;
  depth: Map<string, number>;
  order: Map<string, number>;
  stats: Map<string, Stats>; // 部分木の葉の仮説の値の内訳と、部分木のノード数
  rootId: string;
}

export function buildIndex(nodes: Node[]): Index {
  const byId = new Map<string, Node>();
  const children = new Map<string, string[]>();
  const depth = new Map<string, number>();
  const order = new Map<string, number>();
  let rootId = '';
  nodes.forEach((n, i) => {
    byId.set(n.id, n);
    order.set(n.id, i);
    const p = parentOf(n);
    if (p === null) rootId = n.id;
    else {
      const c = children.get(p);
      if (c) c.push(n.id);
      else children.set(p, [n.id]);
    }
  });
  for (const n of nodes) {
    const p = parentOf(n);
    depth.set(n.id, p === null ? 0 : (depth.get(p) ?? 0) + 1);
  }
  const stats = new Map<string, Stats>();
  for (let i = nodes.length - 1; i >= 0; i--) {
    const n = nodes[i];
    const s: Stats = { t: 0, f: 0, u: 0, total: 1 };
    const cs = children.get(n.id);
    if (cs) {
      for (const c of cs) {
        const x = stats.get(c)!;
        s.t += x.t; s.f += x.f; s.u += x.u; s.total += x.total;
      }
    } else if (n.node_type === 'hypothesis' && !n.inactive) {
      if (n.value === true) s.t++;
      else if (n.value === false) s.f++;
      else s.u++;
    }
    stats.set(n.id, s);
  }
  return { nodes, byId, children, depth, order, stats, rootId };
}

export function ancestors(ix: Index, id: string): string[] {
  const out: string[] = [];
  let n = ix.byId.get(id);
  while (n) {
    const p = parentOf(n);
    if (!p) break;
    out.unshift(p);
    n = ix.byId.get(p);
  }
  return out;
}

export interface Row { id: string; depth: number; hasChildren: boolean; open: boolean }

// 表示する行。include があれば、その集合のノードだけを（祖先を開いて）出す。
export function visibleRows(ix: Index, expanded: Set<string>, include: Set<string> | null, showInactive: boolean, root = ix.rootId): Row[] {
  const rows: Row[] = [];
  const walk = (id: string) => {
    const n = ix.byId.get(id)!;
    if (!showInactive && n.inactive) return;
    if (include && !include.has(id)) return;
    const cs = ix.children.get(id) ?? [];
    const open = include ? true : expanded.has(id);
    rows.push({ id, depth: ix.depth.get(id)!, hasChildren: cs.length > 0, open });
    if (open) for (const c of cs) walk(c);
  };
  if (root && ix.byId.has(root)) walk(root);
  return rows;
}

// 一致したノードと、その祖先の集合
export function withAncestors(ix: Index, ids: Iterable<string>): Set<string> {
  const s = new Set<string>();
  for (const id of ids) {
    if (s.has(id)) continue;
    s.add(id);
    for (const a of ancestors(ix, id)) s.add(a);
  }
  return s;
}

// 差分の重ね表示（2 段目）で使う、ノードの変化の印
export type Change = { kind: 'added' } | { kind: 'removed' } | { kind: 'changed'; fields: { name: string; before: string; after: string }[] };

const vLabel = (v: TruthValue) => (v === true ? '真' : v === false ? '偽' : '未決');

export { vLabel };
