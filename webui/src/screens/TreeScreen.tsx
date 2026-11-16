// スナップショットツリーのビュワー（design/webui/snapshot-tree-viewer.md）。アウトラインと図（横と縦）を切り替える。
// 状態（選んだノード、開いたノード、検索、絞り込み）は 2 つの表示で共通にする。表示と選んだノードと検索の語は URL に持つ。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { listNodes, Problem, withPendingRetry } from '../api';
import { DetailPane } from '../DetailPane';
import { initialFocus, openLevels } from '../focus';
import { GraphView } from '../GraphView';
import { isMobileNow, useMobile } from '../hooks';
import { buildIndex, filterNodes, visibleRows, type Filter, type Index } from '../model';
import { TreePane } from '../TreePane';

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'true', label: '真' },
  { key: 'false', label: '偽' },
  { key: 'null', label: '未決' },
  { key: 'nr', label: '要再検討' },
  { key: 'ar', label: '審査中の AR' },
  { key: 'hr', label: 'open の依頼' },
];

type View = 'outline' | 'graph';

// repository が変わったら、作り直す（App で key={repo} にする）
export function TreeScreen({ repo }: { repo: string }) {
  const params = useRef(new URLSearchParams(location.search)).current;
  const [data, setData] = useState<{ ix: Index; commit: string } | null>(null);
  // 表示の根。読み込んだデータとは別に持つ（「ここから見る」で差し替えるため）
  const [root, setRoot] = useState<string | null>(null);
  // URL の node が、スナップショットツリーになかった
  const [missing, setMissing] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<string | null>(null);
  const [query, setQuery] = useState(params.get('q') ?? '');
  const [filters, setFilters] = useState<Set<Filter>>(new Set());
  const [showInactive, setShowInactive] = useState(true);
  const [view, setViewRaw] = useState<View>(params.get('view') === 'graph' ? 'graph' : 'outline');
  const [vertical, setVerticalRaw] = useState(params.get('orient') === 'v');
  // 表示と向きを利用者が変えたら、履歴に積む（戻るで前の表示に戻る）。ほかは置き換える
  const pushNext = useRef(false);
  const mobile = useMobile();
  const [sheet, setSheet] = useState(false);
  const [filterSheet, setFilterSheet] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
  // 利用者が開いたノードを変えたか（変えたあとは、縦にしても段を開かない）
  const touched = useRef(false);
  // 検索の語は、入力が止まって 300 ms たってから図を収め直す
  const [settledQuery, setSettledQuery] = useState(query);
  useEffect(() => { const t = setTimeout(() => setSettledQuery(query), 300); return () => clearTimeout(t); }, [query]);

  // 読む。取り込み中なら読み直す
  useEffect(() => {
    let live = true;
    setData(null); setError(null);
    withPendingRetry(() => listNodes(repo), (w) => live && setPending(w))
      .then((r) => {
        if (!live) return;
        const ix = buildIndex(r.nodes);
        const f = initialFocus(ix, params.get('node'));
        let exp = f.expanded;
        if (params.get('orient') === 'v') exp = openLevels(ix, isMobileNow() ? 3 : 4, exp);
        setExpanded(exp);
        setSelected(f.selected);
        setRoot(f.root);
        const want = params.get('node');
        if (want && !ix.byId.has(want)) setMissing(want);
        setData({ ix, commit: r.commit });
      })
      .catch((e) => {
        if (!live || (e instanceof Problem && e.status === 401)) return;
        setError(e instanceof Problem && e.code === 'snapshot_pending_timeout' ? 'pending-timeout' : e instanceof Problem && e.code === 'not_found' ? 'not-found' : String(e));
      });
    return () => { live = false; };
  }, [repo, params]);

  // URL に、表示、向き、選んだノード、検索の語を持つ（design/webui/screens.md の「URL」）
  useEffect(() => {
    if (!data) return;
    const q = new URLSearchParams();
    if (view !== 'outline') q.set('view', view);
    if (vertical) q.set('orient', 'v');
    if (selected && selected !== data.ix.rootId) q.set('node', selected);
    if (settledQuery.trim()) q.set('q', settledQuery.trim());
    const s = q.toString();
    const next = location.pathname + (s ? '?' + s : '');
    if (next === location.pathname + location.search) return;
    if (pushNext.current) history.pushState(null, '', next);
    else history.replaceState(null, '', next);
    pushNext.current = false;
  }, [data, view, vertical, selected, settledQuery]);
  // 戻る・進むで、表示と向きを URL に合わせる
  useEffect(() => {
    const onPop = () => {
      const q = new URLSearchParams(location.search);
      setViewRaw(q.get('view') === 'graph' ? 'graph' : 'outline');
      setVerticalRaw(q.get('orient') === 'v');
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  const ix = data?.ix;

  // 検索と絞り込みで、出すノードを決める（一致したノードと、その祖先）
  const { include, matches } = useMemo(
    () => (ix ? filterNodes(ix, query, filters, showInactive) : { include: null, matches: [] as string[] }),
    [ix, query, filters, showInactive],
  );

  const rows = useMemo(() => (ix && data ? visibleRows(ix, expanded, include, showInactive, root ?? ix.rootId) : []), [ix, data, root, expanded, include, showInactive]);
  const matchSet = useMemo(() => new Set(matches), [matches]);
  // 検索した語が変わったら、最初の一致を選ぶ
  useEffect(() => {
    if (query.trim() && matches.length && !(selected && matchSet.has(selected))) setSelected(matches[0]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, matches]);

  // 利用者が選んだとき（モバイルでは詳細のボトムシートを開く）
  const pick = useCallback((id: string) => { setSelected(id); if (isMobileNow()) setSheet(true); }, []);
  const toggle = useCallback((id: string) => {
    touched.current = true;
    setExpanded((e) => { const s = new Set(e); if (s.has(id)) s.delete(id); else s.add(id); return s; });
  }, []);
  const reveal = useCallback((id: string) => {
    if (!ix) return;
    setExpanded((e) => { const s = new Set(e); let p = ix.byId.get(id); while (p) { const par = p.node_type === 'hypothesis' ? p.parent_story_id : p.parent_hypothesis_id; if (!par) break; s.add(par); p = ix.byId.get(par); } return s; });
    setSelected(id);
  }, [ix]);
  const stepMatch = (dir: 1 | -1) => {
    if (!matches.length) return;
    const i = selected ? matches.indexOf(selected) : -1;
    setSelected(matches[(i + dir + matches.length) % matches.length]);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === '/' && document.activeElement !== searchRef.current) { e.preventDefault(); searchRef.current?.focus(); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  if (error === 'not-found') return <Empty title="この repository はありません" />;
  if (error === 'pending-timeout') return <Empty title="スナップショットツリーが出ません" body="worker が止まっている可能性があります。" retry />;
  if (error) return <Empty title="スナップショットツリーを読めませんでした" body={error} retry />;
  if (!data || !ix) return <div className="loading">{pending ? 'スナップショットツリーを取り込み中です。出しだい表示します…' : '読み込み中…'}</div>;

  const userSetExpanded = (e: Set<string>) => { touched.current = true; setExpanded(e); };
  const setView = (v: View) => { if (v !== view) pushNext.current = true; setViewRaw(v); };
  const setVertical = (v: boolean) => {
    if (v !== vertical) pushNext.current = true;
    setVerticalRaw(v);
    if (v && !touched.current) setExpanded((e) => openLevels(ix, isMobileNow() ? 3 : 4, e));
  };
  const toggleFilter = (k: Filter) => setFilters((f) => { const g = new Set(f); if (g.has(k)) g.delete(k); else g.add(k); return g; });
  const total = ix.nodes.length;
  const head = (
    <div className="tree-head">
      <span className="dim">
        {total.toLocaleString()} ノード
        {include !== null && <> · 一致 <b>{matchSet.size.toLocaleString()}</b></>}
        {' '}· {rows.length.toLocaleString()} 個を表示
      </span>
      <span className="spacer" />
      {view === 'graph' && (
        <span className="seg mini" role="group" aria-label="図の向き">
          <button className={!vertical ? 'on' : ''} onClick={() => setVertical(false)} title="左から右">横</button>
          <button className={vertical ? 'on' : ''} onClick={() => setVertical(true)} title="上から下">縦</button>
        </span>
      )}
      {include === null && (
        <>
          <button className="link" onClick={() => userSetExpanded(new Set(ix.nodes.filter((n) => ix.children.has(n.id)).map((n) => n.id)))}>すべて開く</button>
          <button className="link" onClick={() => userSetExpanded(new Set([root ?? ix.rootId]))}>閉じる</button>
        </>
      )}
    </div>
  );

  return (
    <div className="viewer">
      <div className="toolbar">
        <div className="seg mode" role="tablist" aria-label="表示">
          <button className={view === 'outline' ? 'on' : ''} onClick={() => setView('outline')}>
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden><path d="M2 3.5h12M5 8h9M8 12.5h6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>
            <span className="full">アウトライン</span><span className="short">一覧</span>
          </button>
          <button className={view === 'graph' ? 'on' : ''} onClick={() => setView('graph')}>
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden><rect x="1" y="6" width="4" height="4" rx="1" fill="currentColor" /><rect x="11" y="2" width="4" height="4" rx="1" fill="currentColor" /><rect x="11" y="10" width="4" height="4" rx="1" fill="currentColor" /><path d="M5 8h3M8 4v8M8 4h3M8 12h3" stroke="currentColor" strokeWidth="1.3" fill="none" /></svg>
            図
          </button>
        </div>
        <div className="view">
          <div className="seg">
            <span className="seg-label">main <span className="mono dim">{data.commit.slice(0, 7)}</span></span>
          </div>
        </div>
        <div className="search">
          <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden><circle cx="7" cy="7" r="5" fill="none" stroke="currentColor" strokeWidth="1.6" /><path d="M11 11l3.5 3.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>
          <input
            ref={searchRef}
            value={query}
            placeholder="主文・タイトル・識別子で探す"
            aria-label="検索"
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') stepMatch(e.shiftKey ? -1 : 1);
              if (e.key === 'Escape') { setQuery(''); (e.target as HTMLInputElement).blur(); }
            }}
          />
          {query ? (
            <span className="count">{matches.length ? `${selected && matchSet.has(selected) ? matches.indexOf(selected) + 1 : 0} / ${matches.length}` : '0 件'}</span>
          ) : (
            <kbd>/</kbd>
          )}
        </div>
        {mobile && (
          <button className={`filter-btn ${filters.size || !showInactive ? 'on' : ''}`} onClick={() => setFilterSheet(true)}>
            <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden><path d="M2 4h12M4.5 8h7M7 12h2" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" /></svg>
            絞り込み{filters.size ? ` ${filters.size}` : ''}
          </button>
        )}
        {(!mobile || filterSheet) && (
          <div className={`chips ${mobile ? 'filter-sheet' : ''}`}>
            {mobile && <div className="fs-head"><b>絞り込み</b><button className="link" onClick={() => setFilterSheet(false)}>閉じる</button></div>}
            {FILTERS.map((f) => (
              <button key={f.key} className={`chip chip-${f.key} ${filters.has(f.key) ? 'on' : ''}`} onClick={() => toggleFilter(f.key)}>{f.label}</button>
            ))}
            <label className="check">
              <input type="checkbox" checked={showInactive} onChange={(e) => setShowInactive(e.target.checked)} /> inactive
            </label>
          </div>
        )}
      </div>

      {missing && (
        <div className="notice" role="status">
          <span>このノードはありません（<span className="mono">{missing}</span>）。根を示しています。</span>
          <button className="link" onClick={() => setMissing(null)}>閉じる</button>
        </div>
      )}
      <main className="body">
        {view === 'graph' ? (
          <GraphView
            ix={ix}
            rows={rows}
            selected={selected}
            onSelect={pick}
            onToggle={toggle}
            matches={matchSet}
            changes={null}
            filtered={include !== null}
            mobile={mobile}
            searching={query.trim() !== ''}
            fitPending={settledQuery !== query}
            vertical={vertical}
            fitKey={`${vertical ? 'v' : 'h'}|${settledQuery.trim()}|${[...filters].sort().join(',')}|${showInactive}`}
            head={head}
          />
        ) : (
          <TreePane
            ix={ix}
            rows={rows}
            mobile={mobile}
            selected={selected}
            onSelect={pick}
            onToggle={toggle}
            query={query.trim()}
            matches={matchSet}
            changes={null}
            filtered={include !== null}
            onExpandAll={() => userSetExpanded(new Set(ix.nodes.filter((n) => ix.children.has(n.id)).map((n) => n.id)))}
            onCollapseAll={() => userSetExpanded(new Set([root ?? ix.rootId]))}
            summary={{ total, rootValue: ix.byId.get(ix.rootId)!.value }}
          />
        )}
        {(!mobile || sheet) && <DetailPane repo={repo} ix={ix} id={selected} mobile={mobile} onClose={() => setSheet(false)} onReveal={reveal} />}
      </main>
    </div>
  );
}

function Empty({ title, body, retry }: { title: string; body?: string; retry?: boolean }) {
  return (
    <div className="empty-box">
      <div className="empty-title">{title}</div>
      {body && <p className="dim">{body}</p>}
      {retry && <button className="btn" onClick={() => location.reload()}>読み直す</button>}
    </div>
  );
}
