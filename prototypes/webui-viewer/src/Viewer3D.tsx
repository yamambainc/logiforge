// スナップショットツリーの 3D の表示。開いている部分（rows）だけを、層ごとの円錐に置いて、three.js で描く。
// 置き方：根を頂点に、深さごとに下の層へ。各層では、根の真下を中心にした円の上に、葉の数に比例した角度の範囲で並べる。
// このファイルは、3D を開いたときだけ読み込む（App.tsx の lazy）。
import { useEffect, useMemo, useRef, useState } from 'react';
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { textOf, vLabel, type Change, type Index, type Row } from './model';

interface Props {
  ix: Index;
  rows: Row[];
  selected: string | null;
  onSelect: (id: string) => void;
  onToggle: (id: string) => void;
  matches: Set<string>;
  changes: Map<string, Change> | null;
  filtered: boolean;
  searching: boolean;
  mobile: boolean;
  head: React.ReactNode;
  fitKey: string;
  onFallback: () => void;
}

const LAYER_H = 70; // 層の間隔（高さ）
const RING_R = 90; // 1 段ごとに広がる半径
const COL = { t: new THREE.Color('#17a34a'), f: new THREE.Color('#e0393e'), u: new THREE.Color('#9aa3b2'), inactive: new THREE.Color('#d5d9e1'), white: new THREE.Color('#ffffff') };
const HALO = { added: new THREE.Color('#17a34a'), changed: new THREE.Color('#e3a008'), removed: new THREE.Color('#e0393e'), match: new THREE.Color('#ffd43b') };

interface Placed { id: string; pos: THREE.Vector3; parent: string | null; row: Row; size: number }

function layoutCone(rows: Row[], ix: Index): Map<string, Placed> {
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
  const place = (id: string, a0: number, a1: number) => {
    const r = byId.get(id)!;
    const th = (a0 + a1) / 2;
    const rad = r.depth * RING_R;
    const st = ix.stats.get(id)!;
    // 閉じた部分木は、ノード数に応じて大きく描く
    const size = r.hasChildren && !r.open ? 1 + Math.log10(st.total) * 0.55 : 1;
    out.set(id, { id, pos: new THREE.Vector3(Math.cos(th) * rad, -r.depth * LAYER_H, Math.sin(th) * rad), parent: parent.get(id) ?? null, row: r, size });
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

export default function Viewer3D(p: Props) {
  const host = useRef<HTMLDivElement>(null);
  const labelsRef = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState(false);
  const [selScreen, setSelScreen] = useState<{ x: number; y: number } | null>(null);
  const placed = useMemo(() => layoutCone(p.rows, p.ix), [p.rows, p.ix]);

  // three.js の部品は 1 回だけ作り、ref に持つ
  const g = useRef<{
    renderer: THREE.WebGLRenderer; scene: THREE.Scene; camera: THREE.PerspectiveCamera; controls: OrbitControls;
    hyp: THREE.InstancedMesh; story: THREE.InstancedMesh; halo: THREE.InstancedMesh; edges: THREE.LineSegments; sel: THREE.Mesh;
    ids: { hyp: string[]; story: string[] }; dirty: boolean; frames: number;
  } | null>(null);

  // 作る
  useEffect(() => {
    const el = host.current!;
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: new URLSearchParams(location.search).get('aa') !== '0', powerPreference: 'high-performance' });
    } catch {
      setFailed(true);
      return;
    }
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.setClearColor('#fafbfc');
    el.prepend(renderer.domElement);
    const scene = new THREE.Scene();
    scene.add(new THREE.HemisphereLight('#ffffff', '#c9d1e0', 2.2));
    const sun = new THREE.DirectionalLight('#ffffff', 1.2);
    sun.position.set(1, 2, 1.5);
    scene.add(sun);
    const camera = new THREE.PerspectiveCamera(50, 1, 1, 50000);
    const controls = new OrbitControls(camera, renderer.domElement);
    controls.enableDamping = true;
    controls.dampingFactor = 0.12;
    controls.touches = { ONE: THREE.TOUCH.ROTATE, TWO: THREE.TOUCH.DOLLY_PAN };
    controls.screenSpacePanning = true;
    const MAX = 8000;
    const mat = new THREE.MeshLambertMaterial();
    // 三角形を少なくする（球は 80 面、菱形は 8 面）。ノード 4,000 個でも、端末の GPU の負担を小さくするため
    const hyp = new THREE.InstancedMesh(new THREE.IcosahedronGeometry(6, 1), mat, MAX);
    const story = new THREE.InstancedMesh(new THREE.OctahedronGeometry(7), mat, MAX);
    const halo = new THREE.InstancedMesh(new THREE.IcosahedronGeometry(12, 1), new THREE.MeshBasicMaterial({ transparent: true, opacity: 0.32, depthWrite: false }), MAX);
    for (const m of [hyp, story, halo]) { m.count = 0; m.frustumCulled = false; scene.add(m); }
    const edges = new THREE.LineSegments(new THREE.BufferGeometry(), new THREE.LineBasicMaterial({ vertexColors: true, transparent: true, opacity: 0.85 }));
    edges.frustumCulled = false;
    scene.add(edges);
    const sel = new THREE.Mesh(new THREE.TorusGeometry(13, 1.6, 8, 40), new THREE.MeshBasicMaterial({ color: '#3563e9' }));
    sel.visible = false;
    scene.add(sel);
    g.current = { renderer, scene, camera, controls, hyp, story, halo, edges, sel, ids: { hyp: [], story: [] }, dirty: true, frames: 0 };
    controls.addEventListener('change', () => { if (g.current) g.current.dirty = true; });

    const resize = () => {
      const w = el.clientWidth, h = el.clientHeight;
      renderer.setSize(w, h);
      camera.aspect = w / Math.max(h, 1);
      camera.updateProjectionMatrix();
      if (g.current) g.current.dirty = true;
    };
    const ro = new ResizeObserver(resize);
    ro.observe(el);
    resize();

    let raf = 0;
    const loop = () => {
      raf = requestAnimationFrame(loop);
      const s = g.current!;
      const moved = controls.update();
      // 動いたときだけ描く（止まっているあいだは描かない）
      if (moved || s.dirty || controls.autoRotate) {
        s.sel.quaternion.copy(camera.quaternion);
        const t0 = performance.now();
        renderer.render(scene, camera);
        const t1 = performance.now();
        s.frames++;
        updateLabels();
        const st = ((window as unknown as { __3dStat?: { r: number; l: number; n: number } }).__3dStat ??= { r: 0, l: 0, n: 0 });
        st.r += t1 - t0; st.l += performance.now() - t1; st.n++;
        s.dirty = false;
      }
    };
    raf = requestAnimationFrame(loop);
    // 計測とスクリーンショットのための口
    (window as unknown as { __3d: unknown }).__3d = {
      autoRotate: (on: boolean, speed = 2) => { controls.autoRotate = on; controls.autoRotateSpeed = speed; },
      frames: () => g.current?.frames ?? 0,
      dolly: (f: number) => { camera.position.sub(controls.target).multiplyScalar(f).add(controls.target); controls.update(); g.current!.dirty = true; },
      orbit: (deg: number) => { const off = camera.position.clone().sub(controls.target); off.applyAxisAngle(new THREE.Vector3(0, 1, 0), (deg * Math.PI) / 180); camera.position.copy(controls.target).add(off); g.current!.dirty = true; },
    };
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
      controls.dispose();
      renderer.dispose();
      renderer.domElement.remove();
      g.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 置き直す：ノード、色、輪、線
  useEffect(() => {
    const s = g.current;
    if (!s) return;
    const m4 = new THREE.Matrix4(), q = new THREE.Quaternion(), sc = new THREE.Vector3(), col = new THREE.Color();
    s.ids = { hyp: [], story: [] };
    let nh = 0, ns = 0, nhalo = 0;
    const segs: number[] = [], cols: number[] = [];
    for (const n of placed.values()) {
      const node = p.ix.byId.get(n.id)!;
      const ch = p.changes?.get(n.id);
      const isMatch = p.searching && p.matches.has(n.id);
      const ctx = p.filtered && !p.matches.has(n.id);
      col.copy(node.inactive ? COL.inactive : node.value === true ? COL.t : node.value === false ? COL.f : COL.u);
      if (ctx || ch?.kind === 'removed') col.lerp(COL.white, 0.6);
      sc.setScalar(n.size);
      m4.compose(n.pos, q, sc);
      if (node.node_type === 'hypothesis') { s.hyp.setMatrixAt(nh, m4); s.hyp.setColorAt(nh, col); s.ids.hyp.push(n.id); nh++; }
      else { s.story.setMatrixAt(ns, m4); s.story.setColorAt(ns, col); s.ids.story.push(n.id); ns++; }
      const hc = ch ? HALO[ch.kind] : isMatch ? HALO.match : null;
      if (hc) { sc.setScalar(n.size); m4.compose(n.pos, q, sc); s.halo.setMatrixAt(nhalo, m4); s.halo.setColorAt(nhalo, hc); nhalo++; }
      if (n.parent) {
        const a = placed.get(n.parent)!.pos;
        segs.push(a.x, a.y, a.z, n.pos.x, n.pos.y, n.pos.z);
        const ec = ch?.kind === 'added' ? HALO.added : ch?.kind === 'removed' ? HALO.removed : ctx ? new THREE.Color('#e6e9ef') : new THREE.Color('#bcc3cf');
        cols.push(ec.r, ec.g, ec.b, ec.r, ec.g, ec.b);
      }
    }
    for (const [m, c] of [[s.hyp, nh], [s.story, ns], [s.halo, nhalo]] as const) {
      m.count = c;
      m.instanceMatrix.needsUpdate = true;
      if (m.instanceColor) m.instanceColor.needsUpdate = true;
    }
    s.edges.geometry.dispose();
    const geo = new THREE.BufferGeometry();
    geo.setAttribute('position', new THREE.Float32BufferAttribute(segs, 3));
    geo.setAttribute('color', new THREE.Float32BufferAttribute(cols, 3));
    s.edges.geometry = geo;
    s.dirty = true;
  }, [placed, p.ix, p.changes, p.matches, p.filtered, p.searching]);

  // 収める：最初と、検索・絞り込みを変えたとき
  const fitted = useRef<string | null>(null);
  useEffect(() => {
    const s = g.current;
    if (!s || !placed.size || fitted.current === p.fitKey) return;
    fitted.current = p.fitKey;
    const box = new THREE.Box3();
    for (const n of placed.values()) box.expandByPoint(n.pos);
    const sphere = box.getBoundingSphere(new THREE.Sphere());
    const r = Math.max(sphere.radius, 120);
    const dist = r / Math.sin(((s.camera.fov / 2) * Math.PI) / 180) * (p.mobile ? 1.15 : 0.9);
    // 見下ろすので、目標を少し下げて、全体を画面の真ん中に寄せる
    const target = sphere.center.clone().add(new THREE.Vector3(0, -0.22 * r, 0));
    s.controls.target.copy(target);
    // 少し上から見下ろす（層が重ならずに見える）
    s.camera.position.copy(target).add(new THREE.Vector3(0.7, 0.32, 0.7).normalize().multiplyScalar(dist));
    s.controls.update();
    s.dirty = true;
  }, [placed, p.fitKey, p.mobile]);

  // 選んだノード：輪を付け、見えなければそちらへ向ける（最初の表示では向けない）
  const prevSel = useRef<string | null | undefined>(undefined);
  useEffect(() => {
    const s = g.current;
    if (!s) return;
    const n = p.selected ? placed.get(p.selected) : undefined;
    s.sel.visible = !!n;
    const first = prevSel.current === undefined;
    const changed = prevSel.current !== p.selected;
    prevSel.current = p.selected;
    if (n) {
      s.sel.position.copy(n.pos);
      s.sel.scale.setScalar(n.size);
    }
    if (n && !first && changed) {
      s.camera.updateMatrixWorld();
      const v = n.pos.clone().project(s.camera);
      if (Math.abs(v.x) > 1 || Math.abs(v.y) > 1 || v.z > 1) {
        const off = s.camera.position.clone().sub(s.controls.target);
        s.controls.target.copy(n.pos);
        s.camera.position.copy(n.pos).add(off);
        s.controls.update();
      }
    }
    s.dirty = true;
  }, [p.selected, placed]);

  // 押して選ぶ（ドラッグとは、動いた量で見分ける）
  useEffect(() => {
    const s = g.current;
    if (!s) return;
    const el = s.renderer.domElement;
    let down: { x: number; y: number; t: number } | null = null;
    const ray = new THREE.Raycaster();
    const pick = (e: PointerEvent) => {
      const r = el.getBoundingClientRect();
      const ndc = new THREE.Vector2(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1);
      ray.setFromCamera(ndc, s.camera);
      const hits = ray.intersectObjects([s.hyp, s.story], false);
      const h = hits[0];
      if (!h || h.instanceId === undefined) return null;
      return (h.object === s.hyp ? s.ids.hyp : s.ids.story)[h.instanceId];
    };
    const onDown = (e: PointerEvent) => { down = { x: e.clientX, y: e.clientY, t: performance.now() }; };
    const onUp = (e: PointerEvent) => {
      if (!down || Math.hypot(e.clientX - down.x, e.clientY - down.y) > 5) { down = null; return; }
      down = null;
      const id = pick(e);
      if (id) p.onSelect(id);
    };
    const onDbl = (e: MouseEvent) => { const id = pick(e as PointerEvent); if (id && !p.filtered) p.onToggle(id); };
    el.addEventListener('pointerdown', onDown);
    el.addEventListener('pointerup', onUp);
    el.addEventListener('dblclick', onDbl);
    return () => { el.removeEventListener('pointerdown', onDown); el.removeEventListener('pointerup', onUp); el.removeEventListener('dblclick', onDbl); };
  }, [p.onSelect, p.onToggle, p.filtered]);

  // ラベル：近いものだけを出す（デスクトップ 28、モバイル 12）。選んだノードと根はいつも出す
  const latest = useRef({ placed, p });
  latest.current = { placed, p };
  function updateLabels() {
    const s = g.current, box = labelsRef.current;
    if (!s || !box) return;
    const { placed: pl, p: pp } = latest.current;
    const w = box.clientWidth, h = box.clientHeight;
    const limit = pp.mobile ? 12 : 28;
    const cand: { id: string; d: number; x: number; y: number }[] = [];
    const v = new THREE.Vector3();
    const camPos = s.camera.position;
    for (const n of pl.values()) {
      v.copy(n.pos).project(s.camera);
      if (v.z > 1 || Math.abs(v.x) > 1.05 || Math.abs(v.y) > 1.05) continue;
      cand.push({ id: n.id, d: n.pos.distanceTo(camPos), x: (v.x * 0.5 + 0.5) * w, y: (-v.y * 0.5 + 0.5) * h });
    }
    cand.sort((a, b) => a.d - b.d);
    const show = new Map<string, { x: number; y: number }>();
    const root = pp.rows[0]?.id;
    for (const c of cand) if (c.id === pp.selected || c.id === root) show.set(c.id, c);
    // 重なるラベルは出さない（ラベルの幅は、文字数から見積もる）
    const wOf = (id: string) => Math.min(textOf(pp.ix.byId.get(id)!).length, 16) * 12.5 + 14;
    const taken: { x: number; y: number; w: number }[] = [...show.entries()].map(([id, c]) => ({ x: c.x + 12, y: c.y - 10, w: wOf(id) }));
    for (const c of cand) {
      if (show.size >= limit) break;
      if (show.has(c.id)) continue;
      const r = { x: c.x + 12, y: c.y - 10, w: wOf(c.id) };
      if (taken.some((t) => r.x < t.x + t.w + 4 && t.x < r.x + r.w + 4 && Math.abs(t.y - r.y) < 22)) continue;
      show.set(c.id, c); taken.push(r);
    }
    // 使い回す div の数を合わせる
    while (box.children.length < limit + 2) { const d = document.createElement('div'); d.className = 'l3'; box.appendChild(d); }
    let i = 0;
    for (const [id, c] of show) {
      const d = box.children[i++] as HTMLDivElement;
      const node = pp.ix.byId.get(id)!;
      const t = textOf(node);
      const txt = (t.length > 16 ? t.slice(0, 15) + '…' : t);
      const key = `${id}|${txt}|${id === pp.selected}`;
      if (d.dataset.k !== key) {
        d.dataset.k = key;
        d.textContent = txt;
        d.className = `l3 ${id === pp.selected ? 'sel' : ''} ${node.value === true ? 'v-true' : node.value === false ? 'v-false' : 'v-null'}`;
      }
      d.style.transform = `translate(${Math.round(c.x + 12)}px, ${Math.round(c.y - 10)}px)`;
      d.style.display = '';
    }
    for (; i < box.children.length; i++) (box.children[i] as HTMLDivElement).style.display = 'none';
    const sc = pp.selected ? show.get(pp.selected) : undefined;
    setSelScreen((prev) => (sc ? (prev && Math.abs(prev.x - sc.x) < 1 && Math.abs(prev.y - sc.y) < 1 ? prev : { x: sc.x, y: sc.y }) : null));
  }

  const selNode = p.selected ? p.ix.byId.get(p.selected) : undefined;
  const selRow = p.selected ? placed.get(p.selected)?.row : undefined;

  if (failed) {
    return (
      <section className="graph-pane empty-3d">
        {p.head}
        <div className="empty-box">
          <div className="empty-title">この端末では 3D を表示できません</div>
          <p>WebGL が使えないため、3D の表示を作れませんでした。図かアウトラインで見てください。</p>
          <button className="btn" onClick={p.onFallback}>図で見る</button>
        </div>
      </section>
    );
  }

  return (
    <section className="graph-pane">
      {p.head}
      <div className="three" ref={host}>
        <div className="labels3" ref={labelsRef} />
        {selNode && selScreen && (
          <div className="sel3" style={{ transform: `translate(${Math.round(selScreen.x + 12)}px, ${Math.round(selScreen.y + 14)}px)` }}>
            <span className={`val ${selNode.value === true ? 'v-true' : selNode.value === false ? 'v-false' : 'v-null'}`}>{vLabel(selNode.value)}</span>
            {selRow?.hasChildren && !p.filtered && (
              <button className="btn3" onClick={() => p.onToggle(selNode.id)}>{selRow.open ? '閉じる' : `開く（${(p.ix.stats.get(selNode.id)?.total ?? 1) - 1}）`}</button>
            )}
          </div>
        )}
        <div className="hint3">{p.mobile ? '1 本の指で回す・2 本の指でズームと移動・押して選ぶ' : 'ドラッグで回す・ホイールでズーム・右ドラッグで移動・押して選ぶ・ダブルクリックで開閉'}</div>
      </div>
    </section>
  );
}
