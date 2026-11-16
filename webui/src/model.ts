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

/** 読み上げのための、行ごとの段（1 から）と、兄弟の数と、兄弟の中の位置（1 から）。兄弟は、表示している行で数える。 */
export function rowPositions(rows: Row[]): { level: number; setsize: number; posinset: number }[] {
  const out = rows.map((r) => ({ level: r.depth + 1, setsize: 0, posinset: 0 }));
  // 段ごとに、今の兄弟の組（行の番号）を持つ。親が変わるたびに、深い段の組を閉じる
  const open: number[][] = [];
  const close = (g: number[]) => g.forEach((i, k) => { out[i].setsize = g.length; out[i].posinset = k + 1; });
  rows.forEach((r, i) => {
    while (open.length > r.depth + 1) close(open.pop()!);
    if (open.length === r.depth + 1) open[r.depth].push(i);
    else { while (open.length < r.depth) open.push([]); open.push([i]); }
  });
  while (open.length) close(open.pop()!);
  return out;
}

/** 描く行の番号。見えている範囲（start から end の前まで）と、選んだ行（範囲の外でも。aria-activedescendant が描いた行を指すように）。 */
export function rowsToDraw(start: number, end: number, selIndex: number): number[] {
  const out: number[] = [];
  for (let i = start; i < end; i++) out.push(i);
  if (selIndex >= 0 && (selIndex < start || selIndex >= end)) out.push(selIndex);
  return out;
}

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

export type Filter = 'true' | 'false' | 'null' | 'nr' | 'ar' | 'hr';

/** 検索と絞り込みで、出すノードを決める（design/webui/snapshot-tree-viewer.md の「検索と絞り込み」）。
 * 検索は主文・タイトル・識別子の部分一致（大文字と小文字を区別しない）。値どうしは「または」、ほかとは「かつ」。
 * 検索も絞り込みもなければ include は null（すべて）。あれば、一致したノード（matches）と、その祖先（include）。 */
export function filterNodes(ix: Index, query: string, filters: Set<Filter>, showInactive: boolean): { include: Set<string> | null; matches: string[] } {
  const q = query.trim().toLowerCase();
  if (q === '' && filters.size === 0) return { include: null, matches: [] };
  const valueF = (['true', 'false', 'null'] as const).filter((k) => filters.has(k));
  const m: string[] = [];
  for (const n of ix.nodes) {
    if (!showInactive && n.inactive) continue;
    if (q && !(textOf(n).toLowerCase().includes(q) || n.id.toLowerCase().includes(q))) continue;
    if (valueF.length && !valueF.some((k) => String(n.value) === k)) continue;
    if (filters.has('nr') && !(n.node_type === 'hypothesis' && n.needs_reconsideration)) continue;
    if (filters.has('ar') && n.open_apply_request_ids.length === 0) continue;
    if (filters.has('hr') && n.open_human_request_ids.length === 0) continue;
    m.push(n.id);
  }
  return { include: withAncestors(ix, m), matches: m };
}
