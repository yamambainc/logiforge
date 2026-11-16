// 3D の置き方（design/webui/viewer-3d.md の「置き方」）。開いている部分（rows）を、層ごとの円錐に置く。
// 根を頂点に、深さごとに下の層へ。各層では、根の真下を中心にした円の上に、見えている葉の数に比例した角度の範囲で並べる。
import * as THREE from 'three';
import type { Index, Row } from './model';

export const LAYER_H = 70; // 層の間隔（高さ）
export const RING_R = 90; // 1 段ごとに広がる半径
export const MIN_GAP = 22; // 同じ層のノードどうしの、円周の上の最小の間隔

export interface Placed { id: string; pos: THREE.Vector3; parent: string | null; row: Row; size: number }

export function layoutCone(rows: Row[], ix: Index): Map<string, Placed> {
  const kids = new Map<string, string[]>();
  const parent = new Map<string, string | null>();
  const stack: Row[] = [];
  for (const r of rows) {
    while (stack.length && stack[stack.length - 1].depth >= r.depth) stack.pop();
    const par = stack.length ? stack[stack.length - 1].id : null;
    parent.set(r.id, par);
    if (par) { const k = kids.get(par); if (k) k.push(r.id); else kids.set(par, [r.id]); }
    stack.push(r);
  }
  const byId = new Map(rows.map((r) => [r.id, r]));
  const weight = new Map<string, number>();
  const w = (id: string): number => {
    const cs = kids.get(id);
    const v = cs ? cs.reduce((a, c) => a + w(c), 0) : 1;
    weight.set(id, v);
    return v;
  };
  const out = new Map<string, Placed>();
  if (!rows.length) return out;
  w(rows[0].id);
  // 層の半径は max(深さ × 90, その層のノード数 × 最小の間隔 ÷ 2π)。深い層にノードが詰まらないようにする
  const perDepth = new Map<number, number>();
  for (const r of rows) perDepth.set(r.depth, (perDepth.get(r.depth) ?? 0) + 1);
  // 根（深さ 0）は、円錐の頂点に置く
  const radius = (d: number) => (d === 0 ? 0 : Math.max(d * RING_R, ((perDepth.get(d) ?? 0) * MIN_GAP) / (2 * Math.PI)));
  // 円が広がったら、層の間隔も広げて、円錐の形（高さと半径の比）を保つ
  const maxD = Math.max(...perDepth.keys());
  let maxR = 0;
  for (let d = 0; d <= maxD; d++) maxR = Math.max(maxR, radius(d));
  const layerH = Math.max(LAYER_H, maxD ? (maxR * 0.8) / maxD : LAYER_H);
  const place = (id: string, a0: number, a1: number) => {
    const r = byId.get(id)!;
    const th = (a0 + a1) / 2;
    const rad = radius(r.depth);
    const st = ix.stats.get(id)!;
    // 閉じた部分木は、ノード数に応じて大きく描く
    const size = r.hasChildren && !r.open ? 1 + Math.log10(st.total) * 0.55 : 1;
    out.set(id, { id, pos: new THREE.Vector3(Math.cos(th) * rad, -r.depth * layerH, Math.sin(th) * rad), parent: parent.get(id) ?? null, row: r, size });
    const cs = kids.get(id);
    if (!cs) return;
    const total = weight.get(id)!;
    let a = a0;
    for (const c of cs) {
      const span = ((a1 - a0) * weight.get(c)!) / total;
      place(c, a, a + span);
      a += span;
    }
  };
  place(rows[0].id, 0, Math.PI * 2);
  return out;
}
