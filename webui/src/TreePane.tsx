import { memo, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { rowPositions, rowsToDraw, textOf, vLabel, type Change, type Index, type Row, type Stats, type TruthValue } from './model';

const OVERSCAN = 12;

interface Props {
  ix: Index;
  rows: Row[];
  selected: string | null;
  onSelect: (id: string) => void;
  onToggle: (id: string) => void;
  query: string;
  matches: Set<string>;
  changes: Map<string, Change> | null;
  filtered: boolean;
  onExpandAll: () => void;
  onCollapseAll: () => void;
  summary: { total: number; rootValue: TruthValue };
  mobile: boolean;
}

export function TreePane(p: Props) {
  const ROW = p.mobile ? 40 : 30;
  const INDENT = p.mobile ? 12 : 18;
  const ref = useRef<HTMLDivElement>(null);
  const [scroll, setScroll] = useState(0);
  const [height, setHeight] = useState(600);

  useLayoutEffect(() => {
    const el = ref.current!;
    const ro = new ResizeObserver(() => setHeight(el.clientHeight));
    ro.observe(el);
    setHeight(el.clientHeight);
    return () => ro.disconnect();
  }, []);

  // 選んだ行が見えるようにする
  const selIndex = p.selected ? p.rows.findIndex((r) => r.id === p.selected) : -1;
  useEffect(() => {
    const el = ref.current;
    if (!el || selIndex < 0) return;
    const top = selIndex * ROW;
    if (top < el.scrollTop + ROW) el.scrollTop = Math.max(0, top - ROW * 3);
    else if (top + ROW > el.scrollTop + el.clientHeight - ROW) el.scrollTop = top - el.clientHeight + ROW * 4;
  }, [selIndex]);

  const onKey = (e: React.KeyboardEvent) => {
    if (selIndex < 0) return;
    const r = p.rows[selIndex];
    if (e.key === 'ArrowDown' && selIndex < p.rows.length - 1) p.onSelect(p.rows[selIndex + 1].id);
    else if (e.key === 'ArrowUp' && selIndex > 0) p.onSelect(p.rows[selIndex - 1].id);
    else if (e.key === 'ArrowRight' && r.hasChildren && !r.open) p.onToggle(r.id);
    else if (e.key === 'ArrowLeft') {
      if (r.open && r.hasChildren && !p.filtered) p.onToggle(r.id);
      else {
        for (let i = selIndex - 1; i >= 0; i--) if (p.rows[i].depth < r.depth) { p.onSelect(p.rows[i].id); break; }
      }
    } else return;
    e.preventDefault();
  };

  const start = Math.max(0, Math.floor(scroll / ROW) - OVERSCAN);
  const end = Math.min(p.rows.length, Math.ceil((scroll + height) / ROW) + OVERSCAN);
  const pos = useMemo(() => rowPositions(p.rows), [p.rows]);

  return (
    <section className="tree-pane">
      <div className="tree-head">
        <span className="dim">
          {p.summary.total.toLocaleString()} ノード
          {p.filtered && <> · 一致 <b>{p.matches.size.toLocaleString()}</b></>}
          {' '}· {p.rows.length.toLocaleString()} 行を表示
        </span>
        <span className="spacer" />
        <Legend />
        {!p.filtered && (
          <>
            <button className="link" onClick={p.onExpandAll}>すべて開く</button>
            <button className="link" onClick={p.onCollapseAll}>閉じる</button>
          </>
        )}
      </div>
      <div
        className="tree-scroll"
        ref={ref}
        tabIndex={0}
        role="tree"
        aria-label="スナップショットツリー"
        aria-activedescendant={selIndex >= 0 ? rowId(p.selected!) : undefined} onKeyDown={onKey} onScroll={(e) => setScroll(e.currentTarget.scrollTop)}>
        <div style={{ height: p.rows.length * ROW, position: 'relative' }} role="presentation">
          {rowsToDraw(start, end, selIndex).map((i) => ({ i, r: p.rows[i] })).map(({ i, r }) => (
            <TreeRow
              key={r.id}
              top={i * ROW}
              pos={pos[i]}
              row={r}
              ix={p.ix}
              selected={r.id === p.selected}
              match={p.matches.has(r.id)}
              dimmed={p.filtered && !p.matches.has(r.id)}
              query={p.query}
              change={p.changes?.get(r.id) ?? null}
              onSelect={p.onSelect}
              onToggle={p.onToggle}
              filtered={p.filtered}
              h={ROW}
              indent={INDENT}
            />
          ))}
        </div>
      </div>
    </section>
  );
}

const rowId = (id: string) => 'row-' + id;

function Legend() {
  return (
    <span className="legend">
      <span><i className="glyph h v-true" />仮説</span>
      <span><i className="glyph s v-true" />ストーリー</span>
    </span>
  );
}

interface RowProps {
  top: number; pos: { level: number; setsize: number; posinset: number }; row: Row; ix: Index; selected: boolean; match: boolean; dimmed: boolean; query: string;
  change: Change | null; onSelect: (id: string) => void; onToggle: (id: string) => void; filtered: boolean; h: number; indent: number;
}

const TreeRow = memo(function TreeRow({ top, pos, row, ix, selected, match, dimmed, query, change, onSelect, onToggle, filtered, h, indent }: RowProps) {
  const n = ix.byId.get(row.id)!;
  const v = n.value;
  const vc = v === true ? 'v-true' : v === false ? 'v-false' : 'v-null';
  const isH = n.node_type === 'hypothesis';
  const st = ix.stats.get(n.id)!;
  const valueChange = change?.kind === 'changed' ? change.fields.find((f) => f.name === '値') : undefined;
  return (
    <div
      className={`row ${selected ? 'sel' : ''} ${n.inactive ? 'inactive' : ''} ${dimmed ? 'ctx' : ''} ${change ? 'chg-' + change.kind : ''} ${isH ? '' : 'story'}`}
      style={{ top, height: h }}
      onClick={() => onSelect(n.id)}
      id={rowId(n.id)}
      role="treeitem"
      aria-level={pos.level}
      aria-setsize={pos.setsize}
      aria-posinset={pos.posinset}
      aria-selected={selected}
      aria-expanded={row.hasChildren ? row.open : undefined}
    >
      <span className="indent" style={{ width: row.depth * indent }}>
        {Array.from({ length: row.depth }, (_, i) => <span key={i} className="guide" style={{ left: i * indent + indent / 2 - 1 }} />)}
      </span>
      <span
        className={`chev ${row.hasChildren && !filtered ? '' : 'none'} ${row.open ? 'open' : ''}`}
        onClick={(e) => { e.stopPropagation(); if (row.hasChildren && !filtered) onToggle(n.id); }}
      >
        {row.hasChildren && !filtered && <svg viewBox="0 0 10 10" width="10" height="10"><path d="M3 2l4 3-4 3" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>}
      </span>
      <i className={`glyph ${isH ? 'h' : 's'} ${vc}`} />
      <span className="text">
        <Highlight text={textOf(n)} q={match ? query : ''} />
      </span>
      {change?.kind === 'added' && <span className="tag t-added">追加</span>}
      {change?.kind === 'removed' && <span className="tag t-removed">消える</span>}
      {valueChange && <span className="tag t-changed">{valueChange.before} → {valueChange.after}</span>}
      {change?.kind === 'changed' && !valueChange && <span className="tag t-changed">変化</span>}
      {isH && n.needs_reconsideration && <span className="tag t-nr">要再検討</span>}
      {n.open_apply_request_ids.length > 0 && <span className="tag t-ar">AR{n.open_apply_request_ids.length > 1 ? ` ${n.open_apply_request_ids.length}` : ''}</span>}
      {n.open_human_request_ids.length > 0 && <span className="tag t-hr">依頼</span>}
      {n.node_type === 'story' && n.state === 'inactive' && <span className="tag t-inactive">inactive</span>}
      {row.hasChildren && !row.open && <SubtreeBar s={st} />}
      <span className="mono id">{n.id.slice(0, 6)}</span>
      <span className={`val ${vc}`}>{vLabel(v)}</span>
    </div>
  );
});

function SubtreeBar({ s }: { s: Stats }) {
  const leaves = s.t + s.f + s.u;
  if (!leaves) return null;
  const w = (x: number) => `${(x / leaves) * 100}%`;
  return (
    <span className="sub" title={`部分木 ${s.total} ノード / 葉 真 ${s.t}・偽 ${s.f}・未決 ${s.u}`}>
      <span className="sub-n">{s.total - 1}</span>
      <span className="bar">
        <span className="b-t" style={{ width: w(s.t) }} />
        <span className="b-f" style={{ width: w(s.f) }} />
        <span className="b-u" style={{ width: w(s.u) }} />
      </span>
    </span>
  );
}

function Highlight({ text, q }: { text: string; q: string }) {
  if (!q) return <>{text}</>;
  const i = text.toLowerCase().indexOf(q.toLowerCase());
  if (i < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, i)}
      <mark>{text.slice(i, i + q.length)}</mark>
      {text.slice(i + q.length)}
    </>
  );
}
