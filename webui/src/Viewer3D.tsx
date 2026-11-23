// スナップショットツリーの 3D の表示。開いている部分（rows）だけを、層ごとの円錐に置いて、three.js で描く。
// 置き方：根を頂点に、深さごとに下の層へ。各層では、根の真下を中心にした円の上に、葉の数に比例した角度の範囲で並べる。
// このファイルは、3D を開いたときだけ読み込む（screens/TreeScreen.tsx の lazy）。置き方は cone.ts。
import { useEffect, useMemo, useRef, useState } from 'react';
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { layoutCone } from './cone';
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
  onRetry: () => void;
}

const COL = { t: new THREE.Color('#17a34a'), f: new THREE.Color('#e0393e'), u: new THREE.Color('#9aa3b2'), inactive: new THREE.Color('#d5d9e1'), white: new THREE.Color('#ffffff') };
const HALO = { added: new THREE.Color('#17a34a'), changed: new THREE.Color('#e3a008'), removed: new THREE.Color('#e0393e'), match: new THREE.Color('#ffd43b') };

type R = { x: number; y: number; w: number; h: number };
type Side = 'right' | 'left' | 'above' | 'below';
const SIDES: Side[] = ['right', 'left', 'above', 'below'];
const LABEL_H = 20;
const hit = (a: R, b: R) => a.x < b.x + b.w + 4 && b.x < a.x + a.w + 4 && a.y < b.y + b.h + 2 && b.y < a.y + a.h + 2;

/** ラベルの文字。16 字までにし、長いものは「…」で切る */
function labelText(ix: Index, id: string) {
  const t = textOf(ix.byId.get(id)!);
  return t.length > 16 ? t.slice(0, 15) + '…' : t;
}

// ラベルの幅は、ラベルと同じフォントで実際に測る（.l3 の 12px と左右の余白 6px、枠 1px）
let measureCtx: CanvasRenderingContext2D | null = null;
const widths = new Map<string, number>();
function labelWidth(text: string): number {
  let v = widths.get(text);
  if (v !== undefined) return v;
  if (!measureCtx) {
    measureCtx = document.createElement('canvas').getContext('2d');
    if (measureCtx) measureCtx.font = `12px ${getComputedStyle(document.body).fontFamily}`;
  }
  v = Math.ceil((measureCtx ? measureCtx.measureText(text).width : text.length * 12.5) + 14);
  widths.set(text, v);
  return v;
}

/** ノードの画面の位置 (x, y) に対して、向き side に置いたラベルの場所。画面の中に収める */
function labelRect(text: string, x: number, y: number, side: Side, w: number, h: number): R {
  const lw = labelWidth(text);
  const at = side === 'right' ? { x: x + 12, y: y - LABEL_H / 2 }
    : side === 'left' ? { x: x - 12 - lw, y: y - LABEL_H / 2 }
    : side === 'above' ? { x: x - lw / 2, y: y - 12 - LABEL_H }
    : { x: x - lw / 2, y: y + 12 };
  return { x: Math.min(Math.max(at.x, 4), w - lw - 4), y: Math.min(Math.max(at.y, 4), h - LABEL_H - 4), w: lw, h: LABEL_H };
}

// 選んだノードの道具（値と「開く」「閉じる」）の場所。ラベルのすぐ下に、画面の中に収めて置く
function toolsRect(label: { x: number; y: number; h: number }, w: number, mobile: boolean): R {
  const tw = mobile ? 170 : 150, th = mobile ? 44 : 34;
  return { x: Math.min(Math.max(label.x, 4), w - tw - 4), y: label.y + label.h + 4, w: tw, h: th };
}

export default function Viewer3D(p: Props) {
  const host = useRef<HTMLDivElement>(null);
  const labelsRef = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState(false);
  const [lost, setLost] = useState(false);
  // 操作の案内：このタブで最初の 1 回だけ出す。閉じるか操作したら消し、? で出し直せる
  const [hint, setHint] = useState(() => sessionStorage.getItem('3d-hint') !== 'seen');
  const hideHint = () => { setHint(false); sessionStorage.setItem('3d-hint', 'seen'); };
  const hideRef = useRef(hideHint);
  hideRef.current = hideHint;
  // 選んだノードの道具。毎フレーム React で描き直さず、style.transform で直接動かす
  const toolsRef = useRef<HTMLDivElement>(null);
  const placed = useMemo(() => layoutCone(p.rows, p.ix), [p.rows, p.ix]);

  // three.js の部品は 1 回だけ作り、ref に持つ
  const g = useRef<{
    renderer: THREE.WebGLRenderer; scene: THREE.Scene; camera: THREE.PerspectiveCamera; controls: OrbitControls;
    hyp: THREE.InstancedMesh; story: THREE.InstancedMesh; halo: THREE.InstancedMesh; edges: THREE.LineSegments; sel: THREE.Mesh;
    ids: { hyp: string[]; story: string[] }; dirty: boolean; frames: number; ms: number; wake: () => void;
  } | null>(null);

  // 作る
  useEffect(() => {
    const el = host.current!;
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' });
    } catch {
      setFailed(true);
      return;
    }
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.setClearColor('#fafbfc');
    renderer.domElement.setAttribute('role', 'img');
    renderer.domElement.setAttribute('aria-label', 'スナップショットツリーの 3D。アウトラインで同じ内容を読めます');
    el.prepend(renderer.domElement);
    // 5 WebGL の文脈が失われたとき（モバイルで裏に回したときなど）は、作り直しを促す
    const onLost = (e: Event) => { e.preventDefault(); setLost(true); };
    renderer.domElement.addEventListener('webglcontextlost', onLost);
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
    // 上下に回せる角度を絞る（下から見上げて、根が下に見えないようにする）
    controls.minPolarAngle = 0.08 * Math.PI;
    controls.maxPolarAngle = 0.56 * Math.PI;
    const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduce) controls.enableDamping = false;
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
    // 描くのは、動いたときと、変わったときだけ。止まったら requestAnimationFrame の繰り返しもやめ、wake で始め直す
    let raf = 0;
    const wake = () => { if (g.current) g.current.dirty = true; if (!raf) raf = requestAnimationFrame(loop); };
    g.current = { renderer, scene, camera, controls, hyp, story, halo, edges, sel, ids: { hyp: [], story: [] }, dirty: true, frames: 0, ms: 0, wake };
    controls.addEventListener('change', wake);
    controls.addEventListener('start', wake);

    const resize = () => {
      const w = el.clientWidth, h = el.clientHeight;
      renderer.setSize(w, h);
      camera.aspect = w / Math.max(h, 1);
      camera.updateProjectionMatrix();
      wake();
    };
    const ro = new ResizeObserver(resize);
    ro.observe(el);
    resize();

    // raf は、繰り返しているあいだ 0 でない。loop の中の controls.update が change を出しても、wake が 2 本目を始めないように、止めるときだけ 0 にする
    function loop() {
      const s = g.current;
      if (!s) { raf = 0; return; }
      const moved = controls.update();
      // 動いたときだけ描く（止まっているあいだは描かず、繰り返しもやめる）
      if (!(moved || s.dirty || controls.autoRotate)) { raf = 0; return; }
      raf = requestAnimationFrame(loop);
      {
        s.sel.quaternion.copy(camera.quaternion);
        const t0 = performance.now();
        renderer.render(scene, camera);
        // どのラベルを出すかの選び直しは重いので、動いているあいだは 100 ms に 1 回にする。出しているラベルの位置は、毎フレーム動かす
        const now = performance.now();
        if (!moved || now - lastPick.current > 100) { updateLabels(); lastPick.current = now; }
        else moveLabels();
        s.frames++;
        s.ms += performance.now() - t0;
        s.dirty = false;
      }
    }
    raf = requestAnimationFrame(loop);
    // E2E と性能の計測のための口（描いたフレームの数と、その JavaScript の時間）。開発のときと、自動で動かしているブラウザ（E2E）のときだけ置く
    const probe = import.meta.env.DEV || navigator.webdriver;
    if (probe) {
      (window as unknown as { __3d?: unknown }).__3d = {
        autoRotate: (on: boolean, speed = 2) => { controls.autoRotate = on; controls.autoRotateSpeed = speed; wake(); },
        frames: () => g.current?.frames ?? 0,
        ms: () => g.current?.ms ?? 0,
        loseContext: () => renderer.getContext().getExtension('WEBGL_lose_context')?.loseContext(),
      };
    }
    return () => {
      cancelAnimationFrame(raf);
      raf = 0;
      ro.disconnect();
      controls.dispose();
      // 片づけ：ジオメトリとマテリアルを捨て、文脈の失われた知らせを外してから、文脈を手放す（行き来で文脈が溜まらないように）
      for (const m of [hyp, story, halo, edges, sel]) {
        m.geometry.dispose();
        (Array.isArray(m.material) ? m.material : [m.material]).forEach((x) => x.dispose());
      }
      renderer.domElement.removeEventListener('webglcontextlost', onLost);
      renderer.dispose();
      renderer.forceContextLoss();
      renderer.domElement.remove();
      if (probe) delete (window as unknown as { __3d?: unknown }).__3d;
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
    s.wake();
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
    // 縦と横の画角の狭いほうで収める（モバイルは縦長なので、横の画角が狭い）
    const vHalf = ((s.camera.fov / 2) * Math.PI) / 180;
    const half = Math.min(vHalf, Math.atan(Math.tan(vHalf) * s.camera.aspect));
    const dist = (r / Math.sin(half)) * (p.mobile ? 1.1 : 0.9);
    // 見下ろすので、目標を少し下げて、全体を画面の真ん中に寄せる
    const target = sphere.center.clone().add(new THREE.Vector3(0, -0.22 * r, 0));
    s.controls.target.copy(target);
    // 少し上から見下ろす（層が重ならずに見える）
    s.camera.position.copy(target).add(new THREE.Vector3(0.7, 0.32, 0.7).normalize().multiplyScalar(dist));
    s.controls.update();
    s.wake();
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
    s.wake();
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
    const onDown = (e: PointerEvent) => { down = { x: e.clientX, y: e.clientY, t: performance.now() }; if (sessionStorage.getItem('3d-hint') !== 'seen') hideRef.current(); };
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
  const lastPick = useRef(0);
  // 出しているラベルと、置いた向き（ノードの右・左・上・下）
  const shown = useRef<{ id: string; side: Side }[]>([]);

  // 出しているラベルの位置だけを、今のカメラで動かす
  function moveLabels() {
    const s = g.current, box = labelsRef.current;
    if (!s || !box) return;
    const { placed: pl, p: pp } = latest.current;
    const w = box.clientWidth, h = box.clientHeight;
    const v = new THREE.Vector3();
    shown.current.forEach(({ id, side }, i) => {
      const n = pl.get(id);
      const d = box.children[i] as HTMLDivElement | undefined;
      if (!n || !d) return;
      v.copy(n.pos).project(s.camera);
      const r = labelRect(labelText(pp.ix, id), (v.x * 0.5 + 0.5) * w, (-v.y * 0.5 + 0.5) * h, side, w, h);
      d.style.transform = `translate(${Math.round(r.x)}px, ${Math.round(r.y)}px)`;
      if (id === pp.selected) placeTools(toolsRect(r, w, pp.mobile));
    });
  }

  function placeTools(t: R | null) {
    const el = toolsRef.current;
    if (!el) return;
    if (!t) { el.style.visibility = 'hidden'; return; }
    el.style.transform = `translate(${Math.round(t.x)}px, ${Math.round(t.y)}px)`;
    el.style.visibility = 'visible';
  }

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
    const root = pp.rows[0]?.id;
    // 選んだノードと根は、いつも右に出す。ほかは、右・左・上・下の順に試し、重なるなら出さない
    const out: { id: string; side: Side; r: R }[] = [];
    const taken: R[] = [];
    for (const c of cand) {
      if (c.id !== pp.selected && c.id !== root) continue;
      const r = labelRect(labelText(pp.ix, c.id), c.x, c.y, 'right', w, h);
      out.push({ id: c.id, side: 'right', r }); taken.push(r);
    }
    const sel = out.find((o) => o.id === pp.selected);
    const tools = sel ? toolsRect(sel.r, w, pp.mobile) : null;
    if (tools) taken.push(tools);
    for (const c of cand) {
      if (out.length >= limit) break;
      if (c.id === pp.selected || c.id === root) continue;
      const text = labelText(pp.ix, c.id);
      for (const side of SIDES) {
        const r = labelRect(text, c.x, c.y, side, w, h);
        if (taken.some((t) => hit(r, t))) continue;
        out.push({ id: c.id, side, r }); taken.push(r);
        break;
      }
    }
    // 使い回す div の数を合わせる
    while (box.children.length < limit + 2) { const d = document.createElement('div'); d.className = 'l3'; box.appendChild(d); }
    let i = 0;
    for (const { id, r } of out) {
      const d = box.children[i++] as HTMLDivElement;
      const node = pp.ix.byId.get(id)!;
      const txt = labelText(pp.ix, id);
      const key = `${id}|${txt}|${id === pp.selected}`;
      if (d.dataset.k !== key) {
        d.dataset.k = key;
        d.textContent = txt;
        d.className = `l3 ${id === pp.selected ? 'sel' : ''} ${node.value === true ? 'v-true' : node.value === false ? 'v-false' : 'v-null'}`;
      }
      d.style.transform = `translate(${Math.round(r.x)}px, ${Math.round(r.y)}px)`;
      d.style.display = '';
    }
    for (; i < box.children.length; i++) (box.children[i] as HTMLDivElement).style.display = 'none';
    shown.current = out.map(({ id, side }) => ({ id, side }));
    placeTools(tools);
  }

  const selNode = p.selected ? p.ix.byId.get(p.selected) : undefined;
  const selRow = p.selected ? placed.get(p.selected)?.row : undefined;

  if (lost) {
    return (
      <section className="graph-pane empty-3d">
        {p.head}
        <div className="empty-box">
          <div className="empty-title">3D の描画が止まりました</div>
          <button className="btn" onClick={p.onRetry}>3D を作り直す</button>{' '}
          <button className="btn" onClick={p.onFallback}>図で見る</button>
        </div>
      </section>
    );
  }

  if (failed) {
    return (
      <section className="graph-pane empty-3d">
        {p.head}
        <div className="empty-box">
          <div className="empty-title">この端末では 3D を表示できません</div>
          <p>WebGL が使えません。</p>
          <button className="btn" onClick={p.onFallback}>図で見る</button>
        </div>
      </section>
    );
  }

  return (
    <section className="graph-pane">
      {p.head}
      <div className="three" ref={host} tabIndex={0} onKeyDown={(e) => {
        // キーボード：アウトラインと同じく、行の並び（深さ優先）で動く
        const i = p.selected ? p.rows.findIndex((r) => r.id === p.selected) : -1;
        if (i < 0) return;
        const r = p.rows[i];
        if (e.key === 'ArrowDown' && i < p.rows.length - 1) p.onSelect(p.rows[i + 1].id);
        else if (e.key === 'ArrowUp' && i > 0) p.onSelect(p.rows[i - 1].id);
        else if (e.key === 'ArrowRight' && r.hasChildren && !r.open) p.onToggle(r.id);
        else if (e.key === 'ArrowLeft') {
          if (r.open && r.hasChildren && !p.filtered) p.onToggle(r.id);
          else for (let j = i - 1; j >= 0; j--) if (p.rows[j].depth < r.depth) { p.onSelect(p.rows[j].id); break; }
        } else return;
        e.preventDefault();
      }}>
        <div className="labels3" ref={labelsRef} />
        {selNode && (
          <div className="sel3" ref={toolsRef}>
            <span className={`val ${selNode.value === true ? 'v-true' : selNode.value === false ? 'v-false' : 'v-null'}`}>{vLabel(selNode.value)}</span>
            {selRow?.hasChildren && !p.filtered && (
              <button className="btn3" onClick={() => p.onToggle(selNode.id)}>{selRow.open ? '閉じる' : `開く（${(p.ix.stats.get(selNode.id)?.total ?? 1) - 1}）`}</button>
            )}
          </div>
        )}
        {hint ? (
          <div className="hint3">
            <span>{p.mobile ? '1 本の指で回す・2 本の指でズームと移動・押して選ぶ' : 'ドラッグで回す・ホイールでズーム・右ドラッグで移動・押して選ぶ・ダブルクリックで開閉'}</span>
            <button className="link" onClick={hideHint} aria-label="案内を閉じる">閉じる</button>
          </div>
        ) : (
          <button className="help3" onClick={() => setHint(true)} aria-label="操作の案内を出す">?</button>
        )}
        <div className="sr-only" aria-live="polite">{selNode ? `${textOf(selNode)}。値は${vLabel(selNode.value)}` : ''}</div>
      </div>
    </section>
  );
}
