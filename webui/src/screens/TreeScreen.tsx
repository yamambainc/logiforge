// スナップショットツリーのビュワー（design/webui/snapshot-tree-viewer.md）。アウトラインと図（横と縦）を切り替える。
// 状態（選んだノード、開いたノード、検索、絞り込み）は 2 つの表示で共通にする。表示と選んだノードと検索の語は URL に持つ。
// 示す時点（今の main、過去の時点、apply request を重ねた姿）も URL に持つ（design/webui/overlay.md）。
import { Component, lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { listCommits, listOpenApplyRequests, Problem, workNodes, type ApplyRequest, type At, type MainCommit } from '../api';
import { DetailPane } from '../DetailPane';
import { initialFocus, inSubtree, openLevels, overlayFocus } from '../focus';
import { GraphView } from '../GraphView';
import { isMobileNow, useMobile } from '../hooks';
import { ancestors, changeOrder, FILTER_KEYS, filterNodes, textOf, visibleRows, type Filter } from '../model';
import { isBlocked, loadSource, sourceFromURL, sourceToURL, type Blocked, type Loaded, type Source } from '../source';
import { TreePane } from '../TreePane';

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'true', label: '真' },
  { key: 'false', label: '偽' },
  { key: 'null', label: '未決' },
  { key: 'leaf', label: '未決の葉（作業）' },
  { key: 'nr', label: '要再検討' },
  { key: 'nr-work', label: '要再検討（作業）' },
  { key: 'cited', label: '引用元が変化（作業）' },
  { key: 'ar', label: '審査中の AR' },
  { key: 'hr', label: 'open の依頼' },
  { key: 'changed', label: 'この AR で変わる' },
];

const short = (t: string, k = 16) => (t.length > k ? t.slice(0, k) + '…' : t);
const sourceKey = (s: Source) => (s.kind === 'ar' ? 'ar:' + s.id : s.kind === 'commit' ? 'commit:' + s.commit : 'main');
const fmtTime = (iso?: string) => { if (!iso) return ''; const d = new Date(iso); return isNaN(+d) ? iso : d.toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' }); };

type View = 'outline' | 'graph' | '3d';
const viewOf = (v: string | null): View => (v === 'graph' || v === '3d' ? v : 'outline');

// 3D の部分（three.js を含む）は、3D を開いたときだけ読み込む（design/webui/viewer-3d.md の「描き方」）
const load3d = () => import('../Viewer3D');
let Viewer3D = lazy(load3d);

// 3D の部分の読み込みに失敗したとき（回線など）に、図へ戻れるようにする。
// React の lazy は失敗を覚えるので、作り直す。3D に戻ると（LoadGuard を作り直すと）、読み直す
class LoadGuard extends Component<{ onFallback: () => void; children: React.ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch() { Viewer3D = lazy(load3d); }
  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <section className="graph-pane empty-3d">
        <div className="empty-box">
          <div className="empty-title">この端末では 3D を表示できません</div>
          <p>3D の部分を読み込めませんでした。図かアウトラインで見てください。</p>
          <button className="btn" onClick={this.props.onFallback}>図で見る</button>
        </div>
      </section>
    );
  }
}

// repository が変わったら、作り直す（App で key={repo} にする）
export function TreeScreen({ repo }: { repo: string }) {
  const params = useRef(new URLSearchParams(location.search)).current;
  const [source, setSourceRaw] = useState<Source>(() => sourceFromURL(params));
  const [data, setData] = useState<Loaded | null>(null);
  const [blocked, setBlocked] = useState<Blocked | null>(null);
  // 図で最初に収めるノード（まわり）。null なら全体
  const [around, setAround] = useState<Set<string> | null>(null);
  // 選べる apply request と、main のコミット（過去の時点）
  const [ars, setArs] = useState<ApplyRequest[]>([]);
  const [commits, setCommits] = useState<MainCommit[]>([]);
  // 表示の根。読み込んだデータとは別に持つ（「ここから見る」で差し替えるため）
  const [root, setRoot] = useState<string | null>(null);
  const rootRef = useRef<string | null>(null);
  rootRef.current = root;
  // URL の node が、スナップショットツリーになかった
  const [missing, setMissing] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<string | null>(null);
  const [query, setQuery] = useState(params.get('q') ?? '');
  // 絞り込みも URL（f）に持つ。ダッシュボードの件数から、絞り込んだビュワーを開くため
  const [filters, setFilters] = useState<Set<Filter>>(() => new Set((params.get('f') ?? '').split(',').filter((k): k is Filter => (FILTER_KEYS as string[]).includes(k))));
  const [showInactive, setShowInactive] = useState(true);
  const [view, setViewRaw] = useState<View>(viewOf(params.get('view')));
  // 「3D を作り直す」で、3D の部分を作り直す
  const [retry3d, setRetry3d] = useState(0);
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

  // 読む。取り込み中なら読み直す。時点を変えたら読み直す
  const firstLoad = useRef(true);
  const selectedRef = useRef<string | null>(null);
  selectedRef.current = selected;
  const key = sourceKey(source);
  useEffect(() => {
    let live = true;
    setData(null); setBlocked(null); setError(null);
    // URL の node は、開いたときだけ使う。AR を重ねたら、最初に変わるノードを選ぶ。ほかの時点に変えたら、選んでいたノードのまま
    const want = firstLoad.current ? params.get('node') : source.kind === 'ar' ? null : selectedRef.current;
    loadSource(repo, source, (w) => live && setPending(w))
      .then((r) => {
        if (!live) return;
        if (isBlocked(r)) { setBlocked(r); return; }
        const wantRoot = firstLoad.current ? params.get('root') : rootRef.current;
        const f = r.changes ? overlayFocus(r.ix, r.changes, want, wantRoot) : initialFocus(r.ix, want, wantRoot);
        let exp = f.expanded;
        if (params.get('orient') === 'v') exp = openLevels(r.ix, isMobileNow() ? 3 : 4, exp, f.root);
        setExpanded(exp);
        setSelected(f.selected);
        setRoot(f.root);
        setAround(f.around);
        if (firstLoad.current && want && !r.ix.byId.has(want)) setMissing(want);
        firstLoad.current = false;
        if (!r.changes) setFilters((x) => { if (!x.has('changed')) return x; const g = new Set(x); g.delete('changed'); return g; });
        setData(r);
      })
      .catch((e) => {
        if (!live || (e instanceof Problem && e.status === 401)) return;
        setError(e instanceof Problem && e.code === 'snapshot_pending_timeout' ? 'pending-timeout' : e instanceof Problem && e.code === 'not_found' ? 'not-found' : String(e));
      });
    return () => { live = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [repo, key]);

  // 選べる apply request（open のもの）と、main のコミット（新しい 50 個）。読めなくても、ビュワーは使える
  useEffect(() => {
    listOpenApplyRequests(repo).then(setArs).catch(() => {});
    listCommits(repo).then((p) => setCommits(p.items)).catch(() => {});
  }, [repo]);

  // URL に、表示、向き、選んだノード、検索の語を持つ（design/webui/screens.md の「URL」）
  useEffect(() => {
    if (!data) return;
    const q = new URLSearchParams();
    sourceToURL(source, q);
    if (view !== 'outline') q.set('view', view);
    if (vertical) q.set('orient', 'v');
    if (root && root !== data.ix.rootId) q.set('root', root);
    if (selected && selected !== (root ?? data.ix.rootId)) q.set('node', selected);
    if (settledQuery.trim()) q.set('q', settledQuery.trim());
    if (filters.size) q.set('f', [...filters].join(','));
    const s = q.toString();
    const next = location.pathname + (s ? '?' + s : '');
    if (next === location.pathname + location.search) return;
    if (pushNext.current) history.pushState(null, '', next);
    else history.replaceState(null, '', next);
    pushNext.current = false;
  }, [data, source, root, view, vertical, selected, settledQuery, filters]);
  // 戻る・進むで、表示と向きと時点と表示の根を URL に合わせる（根は、読み込んだデータで確かめてから）
  const popRoot = useRef<string | null>(null);
  const [popTick, setPopTick] = useState(0);
  useEffect(() => {
    const onPop = () => {
      const q = new URLSearchParams(location.search);
      setViewRaw(viewOf(q.get('view')));
      setVerticalRaw(q.get('orient') === 'v');
      setSourceRaw((cur) => { const s = sourceFromURL(q); return sourceKey(s) === sourceKey(cur) ? cur : s; });
      popRoot.current = q.get('root');
      setPopTick((t) => t + 1);
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  const ix = data?.ix;

  useEffect(() => {
    if (!ix || popTick === 0) return;
    const r = popRoot.current && ix.byId.has(popRoot.current) ? popRoot.current : ix.rootId;
    if (r !== rootRef.current) { setRoot(r); setExpanded((e) => new Set(e).add(r)); }
  }, [popTick, ix]);

  // 「引用元が変化（作業）」で絞るときは、作業の一覧から、その仮説を読む（ダッシュボードの件数と同じもの）
  const [cited, setCited] = useState<Set<string> | null>(null);
  const wantCited = filters.has('cited');
  useEffect(() => {
    if (!wantCited) { setCited(null); return; }
    let live = true;
    workNodes(repo, 'citation_changed').then((s) => live && setCited(s)).catch(() => live && setCited(new Set()));
    return () => { live = false; };
  }, [repo, wantCited]);

  // 検索と絞り込みで、出すノードを決める（一致したノードと、その祖先）。「ここから見る」では、表示の根の部分木の中だけ
  const { include, matches } = useMemo(() => {
    if (!ix) return { include: null, matches: [] as string[] };
    const r = filterNodes(ix, query, filters, showInactive, data?.changes ?? null, cited);
    if (!root || root === ix.rootId || r.include === null) return r;
    return { include: r.include, matches: r.matches.filter((id) => inSubtree(ix, id, root)) };
  }, [ix, query, filters, showInactive, data, root, cited]);

  const rows = useMemo(() => (ix && data ? visibleRows(ix, expanded, include, showInactive, root ?? ix.rootId) : []), [ix, data, root, expanded, include, showInactive]);
  const matchSet = useMemo(() => new Set(matches), [matches]);
  // 検索の語や絞り込みを変えたら、まわりを外す
  const filterKey = `${query}|${[...filters].join(',')}|${showInactive}`;
  const firstFilterKey = useRef(filterKey);
  useEffect(() => { if (filterKey !== firstFilterKey.current) setAround(null); }, [filterKey]);
  // 検索した語が変わったら、最初の一致を選ぶ
  useEffect(() => {
    if (query.trim() && matches.length && !(selected && matchSet.has(selected))) setSelected(matches[0]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, matches]);

  // 利用者が選んだとき（モバイルでは詳細のボトムシートを開く）
  // まわりを収めるのは、最初の 1 回だけ。利用者が選ぶ・開閉する・探すと、まわりを外す（graph-navigation.md の「まわりを収める」）
  const pick = useCallback((id: string) => { setAround(null); setSelected(id); if (isMobileNow()) setSheet(true); }, []);
  const toggle = useCallback((id: string) => {
    setAround(null);
    touched.current = true;
    setExpanded((e) => { const s = new Set(e); if (s.has(id)) s.delete(id); else s.add(id); return s; });
  }, []);
  const reveal = useCallback((id: string) => {
    if (!ix) return;
    // 表示の根の外のノードなら、スナップショットツリーの根から示す
    setAround(null);
    // 履歴に積み、戻るで前の表示の根に戻れるようにする
    if (rootRef.current && !inSubtree(ix, id, rootRef.current)) { pushNext.current = true; setRoot(ix.rootId); }
    setExpanded((e) => { const s = new Set(e); let p = ix.byId.get(id); while (p) { const par = p.node_type === 'hypothesis' ? p.parent_story_id : p.parent_hypothesis_id; if (!par) break; s.add(par); p = ix.byId.get(par); } return s; });
    setSelected(id);
  }, [ix]);
  // 「次の変更」「前の変更」（n、p）。変わるノードを行の並びでたどる（design/webui/graph-navigation.md）
  // 絞り込み・検索・inactive を隠しているあいだは、今の行にある変化だけをたどる。「ここから見る」では、表示の根の部分木の中だけ
  const order = useMemo(() => {
    if (!ix || !data?.changes) return [];
    const all = changeOrder(ix, data.changes);
    if (include === null && showInactive && (!root || root === ix.rootId)) return all;
    // 「ここから見る」では、表示の根の部分木の中だけ。隠しているあいだは、今の行にあるものだけ
    const shown = include === null && showInactive ? null : new Set(rows.map((r) => r.id));
    return all.filter((id) => (shown ? shown.has(id) : inSubtree(ix, id, root!)));
  }, [ix, data, include, showInactive, rows, root]);
  const stepChange = useCallback((dir: 1 | -1) => {
    if (!order.length) return;
    const i = selectedRef.current ? order.indexOf(selectedRef.current) : -1;
    reveal(order[i < 0 ? (dir === 1 ? 0 : order.length - 1) : (i + dir + order.length) % order.length]);
  }, [order, reveal]);
  const stepMatch = (dir: 1 | -1) => {
    if (!matches.length) return;
    const i = selected ? matches.indexOf(selected) : -1;
    setSelected(matches[(i + dir + matches.length) % matches.length]);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const inField = document.activeElement instanceof HTMLInputElement || document.activeElement instanceof HTMLSelectElement;
      if (e.key === '/' && document.activeElement !== searchRef.current) { e.preventDefault(); searchRef.current?.focus(); }
      else if (!inField && !e.ctrlKey && !e.metaKey && !e.altKey && (e.key === 'n' || e.key === 'p')) { e.preventDefault(); stepChange(e.key === 'n' ? 1 : -1); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [stepChange]);

  // ここから見る：あるノードを表示の根にし、その下だけを示す（履歴に積む）。子を開いて選ぶ
  const scopeTo = useCallback((id: string) => {
    if (!ix || id === rootRef.current) return;
    pushNext.current = true;
    touched.current = true;
    setRoot(id);
    // URL の root で開いたときと同じく、表示の根から 2 段を開く
    setExpanded((e) => new Set([...e, ...initialFocus(ix, null, id).expanded]));
    setSelected(id);
    setAround(null);
  }, [ix]);

  // 時点を変える（履歴に積む）
  const setSource = (s: Source) => { if (sourceKey(s) === key) return; pushNext.current = true; setSourceRaw(s); };

  if (error === 'not-found') return <Empty title="この repository はありません" />;
  if (error === 'pending-timeout') return <Empty title="スナップショットツリーが出ません" body="worker が止まっている可能性があります。" retry />;
  if (error) return <Empty title="スナップショットツリーを読めませんでした" body={error} retry />;
  if (blocked) return <BlockedView b={blocked} onBack={() => setSource({ kind: 'main' })} />;
  if (!data || !ix) return <div className="loading">{pending ? 'スナップショットツリーを取り込み中です。出しだい表示します…' : '読み込み中…'}</div>;

  const userSetExpanded = (e: Set<string>) => { touched.current = true; setExpanded(e); };
  const setView = (v: View) => { if (v !== view) pushNext.current = true; setViewRaw(v); };
  const setVertical = (v: boolean) => {
    if (v !== vertical) pushNext.current = true;
    setVerticalRaw(v);
    if (v && !touched.current) setExpanded((e) => openLevels(ix, isMobileNow() ? 3 : 4, e, root ?? ix.rootId));
  };
  const toggleFilter = (k: Filter) => setFilters((f) => { const g = new Set(f); if (g.has(k)) g.delete(k); else g.add(k); return g; });
  // 表示の根の部分木のノード数。消えるノードは数えない（buildIndex の skip）
  const total = ix.stats.get(root ?? ix.rootId)?.total ?? ix.nodes.length;
  // 過去の時点の番号と時刻。選択肢の 50 個になければ、SHA だけを示す
  const pastInfo = data.past ? commits.find((c) => c.commit === data.past!.commit) : undefined;
  // 詳細を読む時点。消えるノードは、比べた向き先のコミットで読む（apply_request を付けると not_found になるため）
  const detailAt: At | undefined = source.kind === 'commit' ? { commit: data.commit }
    : source.kind === 'ar' ? (selected && data.changes?.get(selected)?.kind === 'removed' ? { commit: data.commit } : { apply_request: source.id })
    : undefined;
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
          <button className={view === '3d' ? 'on' : ''} onClick={() => setView('3d')}>3D</button>
        </div>
        <div className="view sources">
          <select
            className="pick"
            aria-label="時点"
            value={source.kind === 'commit' ? source.commit : ''}
            onChange={(e) => setSource(e.target.value ? { kind: 'commit', commit: e.target.value } : { kind: 'main' })}
          >
            <option value="">今の main{source.kind === 'main' ? ` ${data.commit.slice(0, 7)}` : ''}</option>
            {source.kind === 'commit' && !commits.some((c) => c.commit === source.commit) && <option value={source.commit}>{source.commit.slice(0, 7)}</option>}
            {commits.map((c) => (
              <option key={c.commit} value={c.commit}>#{c.number} {fmtTime(c.committed_at)}{c.apply_request_id ? ` ${c.apply_request_id.slice(0, 12)}…` : ''}</option>
            ))}
          </select>
          <select
            className={`pick ${source.kind === 'ar' ? 'on' : ''}`}
            aria-label="AR を重ねる"
            value={source.kind === 'ar' ? source.id : ''}
            onChange={(e) => setSource(e.target.value ? { kind: 'ar', id: e.target.value } : { kind: 'main' })}
          >
            <option value="">AR を重ねる…</option>
            {source.kind === 'ar' && !ars.some((a) => a.id === source.id) && <option value={source.id}>{source.id}</option>}
            {ars.map((a) => <option key={a.id} value={a.id}>{a.branch} → {a.target}{a.description ? `：${a.description.split('\n')[0].slice(0, 30)}` : ''}</option>)}
          </select>
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
            {FILTERS.filter((f) => (f.key !== 'changed' || data.changes) && (f.key !== 'nr-work' || filters.has('nr-work')) && (f.key !== 'cited' || filters.has('cited')) && (!data.past || (f.key !== 'ar' && f.key !== 'hr'))).map((f) => (
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
      {data.ar && data.changes && (
        <div className={`banner overlay ${data.ar.target !== 'main' ? 'unchecked' : ''}`} role="status">
          {data.ar.target !== 'main' && <span className="warn">main 向きではないので、検査していません</span>}
          <span className="banner-title">反映したあとの姿</span>
          <span>{(data.ar.description ?? '').split('\n')[0]}</span>
          <span className="mono dim">{data.ar.id} · {data.ar.branch} → {data.ar.target} · 比べた向き先 {data.commit.slice(0, 7)}</span>
          <span className="spacer" />
          <span className="pill added">追加 {[...data.changes.values()].filter((c) => c.kind === 'added').length}</span>
          <span className="pill changed">変化 {[...data.changes.values()].filter((c) => c.kind === 'changed').length}</span>
          <span className="pill removed">消える {data.removed.length}</span>
          {order.length > 0 && (
            <span className="steps">
              <button className="link" onClick={() => stepChange(-1)} title="前の変更（p）">← 前の変更</button>
              <span className="dim">{selected && order.includes(selected) ? order.indexOf(selected) + 1 : 0} / {order.length}</span>
              <button className="link" onClick={() => stepChange(1)} title="次の変更（n）">次の変更 →</button>
            </span>
          )}
          <button className="link" onClick={() => setSource({ kind: 'main' })}>重ねるのをやめる</button>
        </div>
      )}
      {data.past && (
        <div className="banner past" role="status">
          <span className="banner-title">過去の時点</span>
          {pastInfo && <span>#{pastInfo.number} {fmtTime(pastInfo.committed_at)}</span>}
          <span className="mono dim">{data.past.commit.slice(0, 7)}</span>
          <span className="dim">審査中の AR と依頼の札は示しません</span>
          <span className="spacer" />
          <button className="link" onClick={() => setSource({ kind: 'main' })}>今の main に戻る</button>
        </div>
      )}
      {root && root !== ix.rootId && (
        <nav className="scope" aria-label="表示の根">
          <span className="dim">ここから見ています：</span>
          {[...ancestors(ix, root), root].map((a, i, all) => (
            <span key={a} className="scope-item">
              {i > 0 && <span className="dim">›</span>}
              {i === all.length - 1
                ? <b title={textOf(ix.byId.get(a)!)}>{short(textOf(ix.byId.get(a)!))}</b>
                : <button className="link" onClick={() => scopeTo(a)} title={textOf(ix.byId.get(a)!)}>{short(textOf(ix.byId.get(a)!))}</button>}
            </span>
          ))}
          <span className="spacer" />
          <button className="link" onClick={() => scopeTo(ix.rootId)}>全体を見る</button>
        </nav>
      )}
      <main className="body">
        {view === '3d' ? (
          <LoadGuard onFallback={() => setView('graph')}>
            <Suspense fallback={<section className="graph-pane"><div className="loading">3D を読み込み中…</div></section>}>
              <Viewer3D
                key={retry3d}
                ix={ix}
                rows={rows}
                selected={selected}
                onSelect={pick}
                onToggle={toggle}
                matches={matchSet}
                changes={data.changes}
                filtered={include !== null}
                searching={query.trim() !== ''}
                mobile={mobile}
                head={head}
                fitKey={`${key}|${root ?? ''}|${settledQuery.trim()}|${[...filters].sort().join(',')}|${showInactive}`}
                onFallback={() => setView('graph')}
                onRetry={() => setRetry3d((x) => x + 1)}
              />
            </Suspense>
          </LoadGuard>
        ) : view === 'graph' ? (
          <GraphView
            ix={ix}
            rows={rows}
            selected={selected}
            onSelect={pick}
            onToggle={toggle}
            matches={matchSet}
            changes={data.changes}
            filtered={include !== null}
            mobile={mobile}
            focus={include === null ? around : null}
            searching={query.trim() !== ''}
            fitPending={settledQuery !== query}
            vertical={vertical}
            fitKey={`${key}|${root ?? ''}|${vertical ? 'v' : 'h'}|${settledQuery.trim()}|${[...filters].sort().join(',')}|${showInactive}`}
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
            changes={data.changes}
            filtered={include !== null}
            onExpandAll={() => userSetExpanded(new Set(ix.nodes.filter((n) => ix.children.has(n.id)).map((n) => n.id)))}
            onCollapseAll={() => userSetExpanded(new Set([root ?? ix.rootId]))}
            summary={{ total, rootValue: ix.byId.get(root ?? ix.rootId)!.value }}
          />
        )}
        {(!mobile || sheet) && (
          <DetailPane
            repo={repo}
            ix={ix}
            id={selected}
            at={detailAt}
            change={selected ? data.changes?.get(selected) ?? null : null}
            unchecked={!!data.ar && data.ar.target !== 'main'}
            mobile={mobile}
            onClose={() => setSheet(false)}
            onReveal={reveal}
            onScope={selected && selected !== (root ?? ix.rootId) && ix.children.has(selected) ? () => scopeTo(selected) : undefined}
          />
        )}
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

function BlockedView({ b, onBack }: { b: Blocked; onBack: () => void }) {
  const title = b.reason === 'conflict' ? '今の main に重ねられません（衝突）'
    : b.reason === 'not-open' ? 'この AR は open ではありません'
    : '向き先のブランチか AR がありません';
  return (
    <div className="empty-box">
      <div className="empty-title">{title}</div>
      <p className="dim mono">{b.id}{b.ar ? ` · ${b.ar.branch} → ${b.ar.target}` : ''}</p>
      {b.reason === 'conflict' && <><p>この AR の差分は、今の main と次のパスで衝突しています。</p><p className="mono paths">{b.detail}</p></>}
      {b.reason === 'not-open' && <p>反映されたか、閉じられました。apply request と履歴の画面で確かめてください。</p>}
      {b.reason === 'gone' && <p>向き先のブランチが消えたか、AR がなくなりました。</p>}
      <button className="btn" onClick={onBack}>今の main に戻る</button>
    </div>
  );
}
