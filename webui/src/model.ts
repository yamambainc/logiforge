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

const yesNo = (b: boolean) => (b ? 'はい' : 'いいえ');

/** 比べる相手（before）と、反映したあと（after）のノードを、識別子で突き合わせる（design/webui/overlay.md）。
 * 値は計算したあとのものを比べるので、伝播で値が変わる祖先も「変化」になる。 */
export function diff(before: Index, after: Index): { changes: Map<string, Change>; removed: string[] } {
  const changes = new Map<string, Change>();
  for (const n of after.nodes) {
    const b = before.byId.get(n.id);
    if (!b) { changes.set(n.id, { kind: 'added' }); continue; }
    const fields: { name: string; before: string; after: string }[] = [];
    if (b.value !== n.value) fields.push({ name: '値', before: vLabel(b.value), after: vLabel(n.value) });
    if (textOf(b) !== textOf(n)) fields.push({ name: n.node_type === 'hypothesis' ? '主文' : 'タイトル', before: textOf(b), after: textOf(n) });
    if (b.inactive !== n.inactive) fields.push({ name: 'inactive', before: yesNo(b.inactive), after: yesNo(n.inactive) });
    if (b.node_type === 'hypothesis' && n.node_type === 'hypothesis') {
      if (b.needs_reconsideration !== n.needs_reconsideration) fields.push({ name: '要再検討', before: yesNo(b.needs_reconsideration), after: yesNo(n.needs_reconsideration) });
      if ((b.background ?? '') !== (n.background ?? '')) fields.push({ name: '背景情報', before: b.background ? 'あり' : 'なし', after: n.background ? '書き換え・追記あり' : 'なし' });
      if (b.valid_verdict_id !== n.valid_verdict_id) fields.push({ name: '有効な検証結果', before: b.valid_verdict_id ?? 'なし', after: n.valid_verdict_id ?? 'なし' });
    }
    if (b.node_type === 'story' && n.node_type === 'story' && b.summary !== n.summary) fields.push({ name: '概要', before: b.summary, after: n.summary });
    if (fields.length) changes.set(n.id, { kind: 'changed', fields });
  }
  const removed = before.nodes.filter((n) => !after.byId.has(n.id)).map((n) => n.id);
  for (const id of removed) changes.set(id, { kind: 'removed' });
  return { changes, removed };
}

/** 反映すると消えるノードを、比べた相手での親と兄弟の並びの位置に差し込んだノードの一覧を返す。消える部分木は、子孫ごと残す。 */
export function withRemoved(after: Index, before: Index, removed: string[]): Node[] {
  if (removed.length === 0) return after.nodes;
  const gone = new Set(removed);
  const kids = new Map<string, string[]>();
  for (const [p, cs] of after.children) kids.set(p, [...cs]);
  for (const id of removed) {
    const p = parentOf(before.byId.get(id)!);
    if (!p) continue;
    // 消える部分木の中は、比べた相手の並びのまま
    if (gone.has(p)) { kids.set(p, [...(before.children.get(p) ?? [])]); continue; }
    const list = kids.get(p) ?? [];
    let at = 0;
    for (const sib of before.children.get(p) ?? []) {
      if (sib === id) break;
      const i = list.indexOf(sib);
      if (i >= 0) at = i + 1;
    }
    list.splice(at, 0, id);
    kids.set(p, list);
  }
  // buildIndex は深さ優先の並びを前提にするので、深さ優先で並べ直す
  const out: Node[] = [];
  const walk = (id: string) => {
    out.push(after.byId.get(id) ?? before.byId.get(id)!);
    for (const c of kids.get(id) ?? []) walk(c);
  };
  walk(after.rootId);
  return out;
}

/** 変わるノード（変化の印があるノード）を、行の並び（深さ優先）で返す。「次の変更」「前の変更」で使う。 */
export function changeOrder(ix: Index, changes: Map<string, Change>): string[] {
  const out: string[] = [];
  const walk = (id: string) => { if (changes.has(id)) out.push(id); for (const c of ix.children.get(id) ?? []) walk(c); };
  walk(ix.rootId);
  return out;
}

const vLabel = (v: TruthValue) => (v === true ? '真' : v === false ? '偽' : '未決');

export { vLabel };

export type Filter = 'true' | 'false' | 'null' | 'nr' | 'ar' | 'hr' | 'changed';

/** 検索と絞り込みで、出すノードを決める（design/webui/snapshot-tree-viewer.md の「検索と絞り込み」）。
 * 検索は主文・タイトル・識別子の部分一致（大文字と小文字を区別しない）。値どうしは「または」、ほかとは「かつ」。
 * 検索も絞り込みもなければ include は null（すべて）。あれば、一致したノード（matches）と、その祖先（include）。 */
export function filterNodes(ix: Index, query: string, filters: Set<Filter>, showInactive: boolean, changes: Map<string, Change> | null = null): { include: Set<string> | null; matches: string[] } {
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
    if (filters.has('changed') && !changes?.has(n.id)) continue;
    m.push(n.id);
  }
  return { include: withAncestors(ix, m), matches: m };
}
