// スナップショットツリーの図。開いている部分（rows）だけを、左から右へ並べて SVG で描く。
// レイアウトは自前（葉に縦の位置を順に割り当て、親は子の真ん中に置く）。ズームとパンもポインタのイベントで自前に書く。
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { textOf, vLabel, type Change, type Index, type Row, type Stats } from './model';

interface Props {
  ix: Index;
  rows: Row[];
  selected: string | null;
  onSelect: (id: string) => void;
  onToggle: (id: string) => void;
  matches: Set<string>;
  changes: Map<string, Change> | null;
  filtered: boolean;
  mobile: boolean;
  head: React.ReactNode;
  searching: boolean;
  fitKey: string; // 検索と絞り込み。変わったら収め直す
  fitPending: boolean; // 検索の入力の途中（収め直しを待っている）
  vertical: boolean; // 上から下（縦）。false なら左から右（横）
}

interface Placed { id: string; x: number; y: number; parent: string | null; row: Row }
interface View { k: number; x: number; y: number }

const MIN_K = 0.12;
const MAX_K = 2.5;
const LOD_K = 0.45; // これより小さいと文字を省く

export function GraphView(p: Props) {
  // col は深さの間隔、row は葉の間隔
  const V = p.vertical;
  const size = V
    ? (p.mobile ? { w: 168, h: 70, col: 118, row: 182 } : { w: 208, h: 72, col: 128, row: 224 })
    : (p.mobile ? { w: 212, h: 70, col: 260, row: 82 } : { w: 264, h: 72, col: 324, row: 86 });

  // レイアウト：rows（深さ優先）から、見えている親子を作り、葉に順に縦の位置を割り当てる
  const layout = useMemo(() => {
    const kids = new Map<string, string[]>();
    const parent = new Map<string, string | null>();
    const stack: Row[] = [];
    for (const r of p.rows) {
      while (stack.length && stack[stack.length - 1].depth >= r.depth) stack.pop();
      const par = stack.length ? stack[stack.length - 1].id : null;
      parent.set(r.id, par);
      if (par) { const k = kids.get(par); if (k) k.push(r.id); else kids.set(par, [r.id]); }
      stack.push(r);
    }
    const pos = new Map<string, Placed>();
    const byId = new Map(p.rows.map((r) => [r.id, r]));
    let slot = 0;
    const place = (id: string): number => {
      const r = byId.get(id)!;
      const cs = kids.get(id);
      let y: number;
      if (!cs) y = slot++ * size.row;
      else {
        const ys = cs.map(place);
        y = (ys[0] + ys[ys.length - 1]) / 2;
      }
      // 横：深さが x、葉の順が y。縦：深さが y、葉の順が x
      pos.set(id, V ? { id, x: y, y: r.depth * size.col, parent: parent.get(id) ?? null, row: r } : { id, x: r.depth * size.col, y, parent: parent.get(id) ?? null, row: r });
      return y;
    };
    if (p.rows.length) place(p.rows[0].id);
    const maxDepth = p.rows.reduce((m, r) => Math.max(m, r.depth), 0);
    const across = Math.max(slot - 1, 0) * size.row, along = maxDepth * size.col;
    return V ? { pos, width: across + size.w, height: along + size.h } : { pos, width: along + size.w, height: across + size.h };
  }, [p.rows, V, size.col, size.row, size.w, size.h]);

  const ref = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState({ w: 0, h: 0 });
  const [view, setView] = useState<View>({ k: 1, x: 40, y: 40 });
  const viewRef = useRef(view);
  viewRef.current = view;

  useLayoutEffect(() => {
    const el = ref.current!;
    const ro = new ResizeObserver(() => setBox({ w: el.clientWidth, h: el.clientHeight }));
    ro.observe(el);
    setBox({ w: el.clientWidth, h: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  // なめらかに動かす
  const anim = useRef<number | null>(null);
  const animateTo = useCallback((to: View) => {
    if (anim.current) cancelAnimationFrame(anim.current);
    // 動きを減らす設定のときは、動かさずにすぐ移る
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) { setView(to); return; }
    const from = viewRef.current;
    const t0 = performance.now();
    const step = (t: number) => {
      const a = Math.min(1, (t - t0) / 220);
      const e = 1 - Math.pow(1 - a, 3);
      setView({ k: from.k + (to.k - from.k) * e, x: from.x + (to.x - from.x) * e, y: from.y + (to.y - from.y) * e });
      if (a < 1) anim.current = requestAnimationFrame(step);
    };
    anim.current = requestAnimationFrame(step);
  }, []);

  const centerOn = useCallback((id: string, k = viewRef.current.k) => {
    const n = layout.pos.get(id);
    if (!n) return;
    animateTo({ k, x: box.w / 2 - k * (n.x + size.w / 2), y: box.h / 2 - k * (n.y + size.h / 2) });
  }, [layout, box, animateTo, size.w, size.h]);

  // 根を左端に寄せ、開いている部分が収まるまで縮める（下限 lower）
  // モバイルは、根と 1 段目だけを収め、縮めるのは 70% まで
  const frame = useCallback((lower: number): View => {
    const pad = 16;
    if (V) {
      // 縦：根を上に寄せ、開いている部分が収まるまで縮める。ただし最初の表示では 70% より縮めない（兄弟が横に並ぶので、幅に収めるとすぐ読めない大きさになる）。
      // 幅に入らない分は、横のパンで見る。横に収まらなければ、根を横の真ん中に置く
      const k = Math.max(lower === LOD_K ? 0.7 : lower, Math.min(1, (box.w - pad * 2) / layout.width, (box.h - pad * 2) / layout.height));
      const root = p.rows.length ? layout.pos.get(p.rows[0].id) : undefined;
      const x = k * layout.width <= box.w - pad * 2 ? (box.w - k * layout.width) / 2 : box.w / 2 - k * ((root?.x ?? 0) + size.w / 2);
      return { k, x, y: pad };
    }
    if (p.mobile && lower === LOD_K) {
      const k = Math.max(0.7, Math.min(1, (box.w - pad * 2) / (size.col + size.w)));
      const root = p.rows.length ? layout.pos.get(p.rows[0].id) : undefined;
      return { k, x: pad, y: box.h / 2 - k * ((root?.y ?? 0) + size.h / 2) };
    }
    const k = Math.max(lower, Math.min(1, (box.w - pad * 2) / layout.width, (box.h - pad * 2) / layout.height));
    const root = p.rows.length ? layout.pos.get(p.rows[0].id) : undefined;
    const y = k * layout.height <= box.h - pad * 2 ? (box.h - k * layout.height) / 2 : box.h / 2 - k * ((root?.y ?? 0) + size.h / 2);
    return { k, x: pad, y };
  }, [box, layout, p.rows, p.mobile, V, size.h, size.col, size.w]);
  const fit = useCallback(() => animateTo(frame(MIN_K)), [animateTo, frame]);

  // 最初の表示：収める（下限 45%）。選んだノードへのフォーカスは効かせない
  const ready = useRef(false);
  const prevSel = useRef<string | null>(null);
  const quietUntil = useRef(0);
  useEffect(() => {
    if (ready.current || !p.rows.length || box.w < 50) return;
    ready.current = true;
    prevSel.current = p.selected;
    setView(frame(LOD_K));
  }, [box, p.rows, frame, p.selected]);

  // 検索や絞り込みを変えたら、収め直す
  const lastFitKey = useRef(p.fitKey);
  useEffect(() => {
    if (!ready.current || lastFitKey.current === p.fitKey) return;
    lastFitKey.current = p.fitKey;
    quietUntil.current = performance.now() + 400; // 続いて起きる「最初の一致を選ぶ」でフォーカスしない
    animateTo(frame(LOD_K));
  }, [p.fitKey, frame, animateTo]);

  // 選んだノードが画面の外にあれば、そこへ動かす（図の中で押したときは、もう見えているので動かない）
  useEffect(() => {
    if (!ready.current || prevSel.current === p.selected) return;
    prevSel.current = p.selected;
    if (!p.selected || p.fitPending || performance.now() < quietUntil.current) return;
    const n = layout.pos.get(p.selected);
    if (!n) return;
    const v = viewRef.current;
    const sx = v.x + v.k * n.x, sy = v.y + v.k * n.y;
    if (sx < 0 || sy < 0 || sx + v.k * size.w > box.w || sy + v.k * size.h > box.h) centerOn(p.selected, Math.max(v.k, 0.85));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [p.selected, layout]);

  // ズームとパン（ホイール、ドラッグ、2 本の指）
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef<{ moved: boolean; dist?: number; mid?: { x: number; y: number } }>({ moved: false });

  const zoomAt = (k2: number, cx: number, cy: number) => {
    const v = viewRef.current;
    const k = Math.min(MAX_K, Math.max(MIN_K, k2));
    setView({ k, x: cx - (cx - v.x) * (k / v.k), y: cy - (cy - v.y) * (k / v.k) });
  };

  useEffect(() => {
    const el = ref.current!;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const r = el.getBoundingClientRect();
      // ホイールとトラックパッドの移動はパン。Ctrl＋ホイールと、トラックパッドのピンチ（ctrlKey が付く）はズーム
      if (e.ctrlKey || e.metaKey) {
        zoomAt(viewRef.current.k * Math.exp(-e.deltaY * 0.01), e.clientX - r.left, e.clientY - r.top);
      } else {
        const v = viewRef.current;
        const dx = e.shiftKey && !e.deltaX ? e.deltaY : e.deltaX;
        const dy = e.shiftKey && !e.deltaX ? 0 : e.deltaY;
        setView({ ...v, x: v.x - dx, y: v.y - dy });
      }
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);

  const onPointerDown = (e: React.PointerEvent) => {
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    gesture.current = { moved: false };
    if (anim.current) cancelAnimationFrame(anim.current);
  };
  const onPointerMove = (e: React.PointerEvent) => {
    const ps = pointers.current;
    const prev = ps.get(e.pointerId);
    if (!prev) return;
    const r = ref.current!.getBoundingClientRect();
    ps.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (ps.size === 1) {
      const dx = e.clientX - prev.x, dy = e.clientY - prev.y;
      if (Math.abs(dx) + Math.abs(dy) > 0) {
        if (!gesture.current.moved && Math.hypot(dx, dy) < 3) return;
        gesture.current.moved = true;
        const v = viewRef.current;
        setView({ ...v, x: v.x + dx, y: v.y + dy });
      }
    } else if (ps.size === 2) {
      const [a, b] = [...ps.values()];
      const dist = Math.hypot(a.x - b.x, a.y - b.y);
      const mid = { x: (a.x + b.x) / 2 - r.left, y: (a.y + b.y) / 2 - r.top };
      const g = gesture.current;
      if (g.dist && g.mid) {
        const v = viewRef.current;
        const k = Math.min(MAX_K, Math.max(MIN_K, v.k * (dist / g.dist)));
        const x = mid.x - (g.mid.x - v.x) * (k / v.k);
        const y = mid.y - (g.mid.y - v.y) * (k / v.k);
        setView({ k, x, y });
      }
      g.moved = true; g.dist = dist; g.mid = mid;
    }
  };
  const onPointerUp = (e: React.PointerEvent) => {
    pointers.current.delete(e.pointerId);
    if (pointers.current.size < 2) { gesture.current.dist = undefined; gesture.current.mid = undefined; }
  };

  // 画面に入るノードだけを描く
  const { k, x, y } = view;
  const margin = 200;
  const vis = useMemo(() => {
    const out: Placed[] = [];
    const x0 = (-x - margin) / k, x1 = (box.w - x + margin) / k;
    const y0 = (-y - margin) / k, y1 = (box.h - y + margin) / k;
    for (const n of layout.pos.values()) {
      const par = n.parent ? layout.pos.get(n.parent)! : null;
      // ノードと、親からの線を含む範囲が、画面に入るか
      const lx = Math.min(n.x, par ? par.x : n.x), hx = Math.max(n.x, par ? par.x : n.x);
      const ly = Math.min(n.y, par ? par.y : n.y), hy = Math.max(n.y, par ? par.y : n.y);
      if (hx + size.w < x0 || lx > x1 || hy + size.h < y0 || ly > y1) continue;
      out.push(n);
    }
    return out;
  }, [layout, x, y, k, box, size.w, size.h]);

  const click = (fn: () => void) => () => { if (!gesture.current.moved) fn(); };

  // キーボード：アウトラインと同じく、行の並び（深さ優先）で動く
  const onKey = (e: React.KeyboardEvent) => {
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
  };
  const lod = k < LOD_K - 0.001; // 引いたときは、文字を省いて形と色だけにする

  return (
    <section className="graph-pane">
      {p.head}
      <div
        className="graph"
        ref={ref}
        tabIndex={0}
        onKeyDown={onKey}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
      >
        <svg width={box.w} height={box.h} role="tree" aria-label="スナップショットツリーの図">
          <g transform={`translate(${x},${y}) scale(${k})`}>
            {vis.map((n) => n.parent && <Edge key={'e' + n.id} n={n} par={layout.pos.get(n.parent)!} w={size.w} h={size.h} vertical={V} change={p.changes?.get(n.id) ?? null} />)}
            {vis.map((n) => (
              <NodeCard
                key={n.id}
                n={n}
                ix={p.ix}
                w={size.w}
                h={size.h}
                selected={n.id === p.selected}
                match={p.searching && p.matches.has(n.id)}
                dim={p.filtered && !p.matches.has(n.id)}
                change={p.changes?.get(n.id) ?? null}
                lod={lod}
                onSelect={click(() => p.onSelect(n.id))}
                onToggle={click(() => { if (!p.filtered) p.onToggle(n.id); })}
                filtered={p.filtered}
                vertical={V}
              />
            ))}
          </g>
        </svg>
        <div className="graph-ctl" onPointerDown={(e) => e.stopPropagation()}>
          {!p.mobile && <button title="拡大" onClick={() => zoomAt(k * 1.25, box.w / 2, box.h / 2)}>＋</button>}
          {!p.mobile && <button title="縮小" onClick={() => zoomAt(k / 1.25, box.w / 2, box.h / 2)}>－</button>}
          <button title="全体を表示" onClick={fit}>
            <svg viewBox="0 0 16 16" width="14" height="14"><path d="M2 6V2h4M10 2h4v4M14 10v4h-4M6 14H2v-4" fill="none" stroke="currentColor" strokeWidth="1.6" /></svg>
          </button>
          <button title="選んだノードへ" disabled={!p.selected} onClick={() => p.selected && centerOn(p.selected, Math.max(k, 0.9))}>
            <svg viewBox="0 0 16 16" width="14" height="14"><circle cx="8" cy="8" r="2.4" fill="currentColor" /><circle cx="8" cy="8" r="5.6" fill="none" stroke="currentColor" strokeWidth="1.4" /></svg>
          </button>
          <span className="zoom">{Math.round(k * 100)}%</span>
        </div>
      </div>
    </section>
  );
}

function Edge({ n, par, w, h, vertical, change }: { n: Placed; par: Placed; w: number; h: number; vertical: boolean; change: Change | null }) {
  if (vertical) {
    const x1 = par.x + w / 2, y1 = par.y + h + 10;
    const x2 = n.x + w / 2, y2 = n.y;
    const my = (y1 + y2) / 2;
    return <path className={`edge ${change ? 'e-' + change.kind : ''}`} d={`M${x1},${y1} C${x1},${my} ${x2},${my} ${x2},${y2}`} />;
  }
  const x1 = par.x + w + 10, y1 = par.y + h / 2;
  const x2 = n.x, y2 = n.y + h / 2;
  const mx = (x1 + x2) / 2;
  return <path className={`edge ${change ? 'e-' + change.kind : ''}`} d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`} />;
}

const NodeCard = memo(function NodeCard(props: {
  n: Placed; ix: Index; w: number; h: number; selected: boolean; match: boolean; dim: boolean; change: Change | null; lod: boolean;
  onSelect: () => void; onToggle: () => void; filtered: boolean; vertical: boolean;
}) {
  const { n, ix, w, h, selected, match, dim, change, lod } = props;
  const node = ix.byId.get(n.id)!;
  const v = node.value;
  const vc = v === true ? 'v-true' : v === false ? 'v-false' : 'v-null';
  const isH = node.node_type === 'hypothesis';
  const text = textOf(node);
  const n1 = Math.floor((w - 84) / 13.2);
  const n2 = Math.floor((w - 44) / 13.2);
  const line1 = text.slice(0, n1);
  const rest = text.slice(n1);
  const line2 = rest.length > n2 ? rest.slice(0, n2 - 1) + '…' : rest;
  const st = ix.stats.get(n.id)!;
  const valueChange = change?.kind === 'changed' ? change.fields.find((f) => f.name === '値') : undefined;
  const badges: { c: string; t: string }[] = [];
  if (change?.kind === 'added') badges.push({ c: 'b-added', t: '追加' });
  if (change?.kind === 'removed') badges.push({ c: 'b-removed', t: '消える' });
  if (valueChange) badges.push({ c: 'b-changed', t: `${valueChange.before}→${valueChange.after}` });
  else if (change?.kind === 'changed') badges.push({ c: 'b-changed', t: '変化' });
  if (isH && node.needs_reconsideration) badges.push({ c: 'b-nr', t: '要再検討' });
  if (node.open_apply_request_ids.length) badges.push({ c: 'b-ar', t: 'AR' });
  if (node.open_human_request_ids.length) badges.push({ c: 'b-hr', t: '依頼' });
  return (
    <g
      transform={`translate(${n.x},${n.y})`}
      className={`gnode ${vc} ${selected ? 'sel' : ''} ${match ? 'match' : ''} ${dim ? 'ctx' : ''} ${node.inactive ? 'inactive' : ''} ${change ? 'g-' + change.kind : ''} ${isH ? 'h' : 's'}`}
      role="treeitem"
      aria-selected={selected}
    >
      <rect className="card" width={w} height={h} rx={isH ? 10 : 4} onClick={props.onSelect} />
      <rect className="stripe" width={4} height={h - 12} x={0} y={6} rx={2} />
      {isH ? <circle className="glyph-g" cx={18} cy={lod ? h / 2 : 18} r={5.5} /> : <rect className="glyph-g" x={13} y={lod ? h / 2 - 5 : 13} width={10} height={10} transform={`rotate(45 18 ${lod ? h / 2 : 18})`} />}
      {!lod && (
        <>
          <text className="g-text" x={32} y={23} onClick={props.onSelect}>{line1}</text>
          {line2 && <text className="g-text" x={32} y={42} onClick={props.onSelect}>{line2}</text>}
          <text className="g-sub" x={32} y={h - 10} onClick={props.onSelect}>
            <tspan className="g-id">{n.id.slice(0, 6)}</tspan>
            {badges.map((b) => <tspan key={b.t} className={`g-badge ${b.c}`} dx={8}>{b.t}</tspan>)}
          </text>
        </>
      )}
      <g transform={`translate(${w - 44},${lod ? h / 2 - 10 : 9})`}>
        <rect className={`g-val ${vc}`} width={36} height={20} rx={5} />
        <text className="g-val-t" x={18} y={14}>{vLabel(v)}</text>
      </g>
      {!lod && n.row.hasChildren && !n.row.open && <Collapsed s={st} x={w - 52} y={h - 15} />}
      {n.row.hasChildren && !props.filtered && (
        <g className="toggle" transform={props.vertical ? `translate(${w / 2},${h + 10})` : `translate(${w + 10},${h / 2})`} onClick={props.onToggle}>
          <circle r={11} />
          <path d={n.row.open ? 'M-4.5,0 H4.5' : 'M-4.5,0 H4.5 M0,-4.5 V4.5'} />
          <circle className="hit" r={20} />
        </g>
      )}
    </g>
  );
});

function Collapsed({ s, x, y }: { s: Stats; x: number; y: number }) {
  const leaves = s.t + s.f + s.u;
  const W = 40;
  const wt = leaves ? (s.t / leaves) * W : 0, wf = leaves ? (s.f / leaves) * W : 0;
  return (
    <g transform={`translate(${x},${y})`} className="g-collapsed">
      <text x={-4} y={4} textAnchor="end">+{s.total - 1}</text>
      <rect width={W} height={4} rx={2} className="b-u" />
      <rect width={wt} height={4} className="b-t" />
      <rect x={wt} width={wf} height={4} className="b-f" />
    </g>
  );
}
