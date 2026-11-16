import { Component, lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { listApplyRequests, listNodes, listNodesAt, Problem, type ApplyRequestSummary } from './api';
import { buildIndex, diff, textOf, visibleRows, withAncestors, ancestors, withRemoved, type Index, type Change } from './model';
import { TreePane } from './TreePane';
import { DetailPane } from './DetailPane';
import { GraphView } from './GraphView';
// 3D は、開いたときだけ読み込む（three.js を最初の読み込みに含めない）
const Viewer3D = lazy(() => import('./Viewer3D'));

// 3D の部分の読み込みに失敗したとき（回線など）に、図へ戻れるようにする
class LoadGuard extends Component<{ onFallback: () => void; children: React.ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
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

function useMobile() {
  const q = '(max-width: 767px)';
  const [m, setM] = useState(() => matchMedia(q).matches);
  useEffect(() => {
    const mq = matchMedia(q);
    const on = () => setM(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);
  return m;
}

const params = new URLSearchParams(location.search);
const N = Number(params.get('n') ?? 100);

type Filter = 'true' | 'false' | 'null' | 'nr' | 'ar' | 'hr' | 'changed';
const FILTERS: { key: Filter; label: string; overlayOnly?: boolean }[] = [
  { key: 'true', label: '真' },
  { key: 'false', label: '偽' },
  { key: 'null', label: '未決' },
  { key: 'nr', label: '要再検討' },
  { key: 'ar', label: '審査中の AR' },
  { key: 'hr', label: 'open の依頼' },
  { key: 'changed', label: 'この AR で変わる', overlayOnly: true },
];

type Overlay =
  | { state: 'none' }
  | { state: 'loading'; ar: ApplyRequestSummary }
  | { state: 'ok'; ar: ApplyRequestSummary; ix: Index; changes: Map<string, Change>; removed: string[]; base: string }
  | { state: 'unsupported'; ar: ApplyRequestSummary }
  | { state: 'conflict'; ar: ApplyRequestSummary; paths: string[] };

export function App() {
  const [main, setMain] = useState<{ ix: Index; commit: string; number?: number } | null>(null);
  const [ars, setArs] = useState<ApplyRequestSummary[]>([]);
  const [overlay, setOverlay] = useState<Overlay>({ state: 'none' });
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<string | null>(params.get('node'));
  const [query, setQuery] = useState(params.get('q') ?? '');
  const [filters, setFilters] = useState<Set<Filter>>(new Set((params.get('f') ?? '').split(',').filter(Boolean) as Filter[]));
  const [showInactive, setShowInactive] = useState(params.get('inactive') !== '0');
  const searchRef = useRef<HTMLInputElement>(null);
  const [view, setView] = useState<'outline' | 'graph' | '3d'>((['graph', '3d'].includes(params.get('view') ?? '') ? params.get('view') : 'outline') as 'outline' | 'graph' | '3d');
  const [retry3d, setRetry3d] = useState(0);
  // 利用者が開いたノードを変えたか（変えたあとは、縦にしても段を開かない）
  const touched = useRef(false);
  const userSetExpanded = (e: Set<string>) => { touched.current = true; setExpanded(e); };
  const [vertical, setVerticalRaw] = useState(params.get('orient') === 'v');
  // 縦にしたら、画面の高さに入る段まで開く（デスクトップ 4 段、モバイル 3 段）
  const openLevels = useCallback((ixx: Index) => {
    const lv = matchMedia('(max-width: 767px)').matches ? 3 : 4;
    setExpanded((e) => { const s = new Set(e); for (const n of ixx.nodes) if ((ixx.depth.get(n.id) ?? 0) < lv - 1) s.add(n.id); return s; });
  }, []);
  const mobile = useMobile();
  const [sheet, setSheet] = useState(params.get('sheet') === '1');
  // 検索の語は、入力が止まって 300 ms たってから図を収め直す
  const [settledQuery, setSettledQuery] = useState(query);
  useEffect(() => { const t = setTimeout(() => setSettledQuery(query), 300); return () => clearTimeout(t); }, [query]);
  const [filterSheet, setFilterSheet] = useState(params.get('fsheet') === '1');
  // 利用者が選んだとき（モバイルでは詳細のボトムシートを開く）
  const pick = useCallback((id: string) => { setSelected(id); if (matchMedia('(max-width: 767px)').matches) setSheet(true); }, []);

  useEffect(() => {
    (async () => {
      const t0 = performance.now();
      const r = await listNodes(N);
      const t1 = performance.now();
      const ix = buildIndex(r.nodes);
      const lf = (window as unknown as { __lf: Record<string, number> }).__lf;
      lf.load = t1 - t0; lf.index = performance.now() - t1;
      const depthOpen = Number(params.get('open') ?? 2);
      const open = new Set(r.nodes.filter((n) => (ix.depth.get(n.id) ?? 0) < depthOpen).map((n) => n.id));
      setMain({ ix, commit: r.commit, number: r.number });
      if (params.get('orient') === 'v' && !params.get('open')) setTimeout(() => openLevels(ix));
      if (!params.get('node')) setSelected(ix.rootId);
      // 試作のスクリーンショット用：検証結果のある深い葉を選ぶ
      if (params.get('pick') === 'verdict') {
        const leaf = r.nodes.find((n) => n.node_type === 'hypothesis' && n.leaf && n.valid_verdict_id && n.value === true && (ix.depth.get(n.id) ?? 0) >= 4);
        if (leaf) { for (const a of ancestors(ix, leaf.id)) open.add(a); setSelected(leaf.id); }
      }
      setExpanded(open);
      const a = await listApplyRequests(N);
      setArs(a);
      const want = params.get('ar');
      if (want) {
        const ar = a.find((x) => x.id === want);
        if (ar) void openOverlay(ar, ix);
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const openOverlay = useCallback(async (ar: ApplyRequestSummary | null, mainIx?: Index) => {
    if (!ar) { setOverlay({ state: 'none' }); setFilters((f) => { const g = new Set(f); g.delete('changed'); return g; }); return; }
    const base = mainIx ?? main?.ix;
    if (!base) return;
    setOverlay({ state: 'loading', ar });
    try {
      const r = await listNodes(N, ar);
      // 比べる相手は、重ねた向き先のコミット（r.commit）の木。手元の main と違えば読み直す（向き先が main でないときは、いつも読み直す）
      // 試作では、main 向きなら手元の main と同じコミットに重ねている
      const before = ar.target === 'main' ? base : buildIndex((await listNodesAt(N, r.commit)).nodes);
      const after = buildIndex(r.nodes);
      const d = diff(before, after);
      const ix = d.removed.length ? buildIndex(withRemoved(after, before, d.removed)) : after;
      for (const id of d.removed) d.changes.set(id, { kind: 'removed' });
      setOverlay({ state: 'ok', ar, ix, changes: d.changes, removed: d.removed, base: r.commit });
      // 変わるノードの祖先を開く
      setExpanded((e) => { const s = new Set(e); for (const id of d.changes.keys()) for (const a of ancestors(ix, id)) s.add(a); return s; });
      if (!params.get('node')) { const first = [...d.changes.keys()][0]; if (first) setSelected(first); }
    } catch (e) {
      if (e instanceof Problem && e.code === 'overlay_unsupported') setOverlay({ state: 'unsupported', ar });
      else if (e instanceof Problem && e.code === 'overlay_conflict') setOverlay({ state: 'conflict', ar, paths: e.detail.split('\n') });
      else throw e;
    }
  }, [main]);

  const ix = overlay.state === 'ok' ? overlay.ix : main?.ix;
  const changes = overlay.state === 'ok' ? overlay.changes : null;

  // 検索とフィルタで、出すノードを決める
  const { include, matches } = useMemo(() => {
    if (!ix) return { include: null, matches: [] as string[] };
    const q = query.trim().toLowerCase();
    const valueF = (['true', 'false', 'null'] as const).filter((k) => filters.has(k));
    const active = q !== '' || filters.size > 0;
    if (!active) return { include: null, matches: [] };
    const m: string[] = [];
    for (const n of ix.nodes) {
      if (!showInactive && n.inactive) continue;
      if (q && !(textOf(n).toLowerCase().includes(q) || n.id.includes(q))) continue;
      if (valueF.length && !valueF.some((k) => String(n.value) === k)) continue;
      if (filters.has('nr') && !(n.node_type === 'hypothesis' && n.needs_reconsideration)) continue;
      if (filters.has('ar') && n.open_apply_request_ids.length === 0) continue;
      if (filters.has('hr') && n.open_human_request_ids.length === 0) continue;
      if (filters.has('changed') && !changes?.has(n.id)) continue;
      m.push(n.id);
    }
    return { include: withAncestors(ix, m), matches: m };
  }, [ix, query, filters, showInactive, changes]);

  const rows = useMemo(() => (ix ? visibleRows(ix, expanded, include, showInactive) : []), [ix, expanded, include, showInactive]);
  const matchSet = useMemo(() => new Set(matches), [matches]);
  // 検索した語が変わったら、最初の一致を選ぶ
  useEffect(() => {
    if (query.trim() && matches.length && !(selected && matchSet.has(selected))) setSelected(matches[0]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, matches]);

  const toggle = useCallback((id: string) => (touched.current = true, setExpanded((e) => { const s = new Set(e); if (s.has(id)) s.delete(id); else s.add(id); return s; })), []);
  const reveal = useCallback((id: string) => {
    if (!ix) return;
    setExpanded((e) => { const s = new Set(e); for (const a of ancestors(ix, id)) s.add(a); return s; });
    setSelected(id);
  }, [ix]);

  const stepMatch = (dir: 1 | -1) => {
    if (!matches.length) return;
    const i = selected ? matches.indexOf(selected) : -1;
    const next = matches[(i + dir + matches.length) % matches.length];
    setSelected(next);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === '/' && document.activeElement !== searchRef.current) { e.preventDefault(); searchRef.current?.focus(); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  if (!main || !ix) return <div className="loading">読み込み中…</div>;
  const setVertical = (v: boolean) => { setVerticalRaw(v); if (v && !touched.current) openLevels(ix); };

  const total = ix.nodes.length;
  const graphHead = (
                <div className="tree-head">
                  <span className="dim">
                    {total.toLocaleString()} ノード
                    {include !== null && <> · 一致 <b>{matchSet.size.toLocaleString()}</b></>}
                    {' '}· {rows.length.toLocaleString()} 個を表示
                  </span>
                  <span className="spacer" />
                  {view === 'graph' && <span className="seg mini" role="group" aria-label="図の向き">
                    <button className={!vertical ? 'on' : ''} onClick={() => setVertical(false)} title="左から右">横</button>
                    <button className={vertical ? 'on' : ''} onClick={() => setVertical(true)} title="上から下">縦</button>
                  </span>}
                  {include === null && (
                    <>
                      <button className="link" onClick={() => userSetExpanded(new Set(ix.nodes.filter((n) => ix.children.has(n.id)).map((n) => n.id)))}>すべて開く</button>
                      <button className="link" onClick={() => userSetExpanded(new Set([ix.rootId]))}>閉じる</button>
                    </>
                  )}
                </div>
  );


  const toggleFilter = (k: Filter) => setFilters((f) => { const g = new Set(f); if (g.has(k)) g.delete(k); else g.add(k); return g; });
  const root = ix.byId.get(ix.rootId)!;
  const openArs = ars.filter((a) => a.state === 'open');
  const blocked = overlay.state === 'unsupported' || overlay.state === 'conflict';

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">Logiforge</div>
        <label className="repo">
          <span className="repo-label">repository</span>
          <select defaultValue="onboarding-retention">
            <option value="onboarding-retention">onboarding-retention</option>
          </select>
        </label>
        <nav className="tabs">
          <a className="tab on">スナップショットツリー</a>
          <a className="tab">人への依頼</a>
          <a className="tab">AR と履歴</a>
          <a className="tab">ダッシュボード</a>
        </nav>
        <div className="me" title="GitForge のユーザー">ky</div>
      </header>

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
          <button className={view === '3d' ? 'on' : ''} onClick={() => setView('3d')}>
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden><path d="M8 1.5l5.6 3.2v6.6L8 14.5l-5.6-3.2V4.7z M8 1.5v6.5m0 0l5.6-3.3M8 8L2.4 4.7M8 8v6.5" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" /></svg>
            3D
          </button>
        </div>
        <div className="view">
          <div className="seg">
            <button className={overlay.state === 'none' ? 'on' : ''} onClick={() => openOverlay(null)}>
              {overlay.state === 'ok' && overlay.ar.target !== 'main'
                ? <>{overlay.ar.target} <span className="mono dim">{overlay.base.slice(0, 7)}</span></>
                : <>main <span className="mono dim">#{main.number} · {main.commit.slice(0, 7)}</span></>}
            </button>
            <select
              className={overlay.state !== 'none' ? 'on' : ''}
              value={overlay.state === 'none' ? '' : overlay.ar.id}
              onChange={(e) => openOverlay(openArs.find((a) => a.id === e.target.value) ?? null)}
            >
              <option value="">AR を重ねる…</option>
              {openArs.map((a) => (
                <option key={a.id} value={a.id}>{a.description}（{a.id}）</option>
              ))}
            </select>
          </div>
        </div>
        <div className="search">
          <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden><circle cx="7" cy="7" r="5" fill="none" stroke="currentColor" strokeWidth="1.6" /><path d="M11 11l3.5 3.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>
          <input
            ref={searchRef}
            value={query}
            placeholder="主文・タイトル・識別子で探す"
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
          {FILTERS.filter((f) => !f.overlayOnly || overlay.state === 'ok').map((f) => (
            <button key={f.key} className={`chip chip-${f.key} ${filters.has(f.key) ? 'on' : ''}`} onClick={() => toggleFilter(f.key)}>
              {f.label}
            </button>
          ))}
          <label className="check">
            <input type="checkbox" checked={showInactive} onChange={(e) => setShowInactive(e.target.checked)} /> inactive
          </label>
        </div>
        )}
      </div>

      {overlay.state === 'ok' && (
        <div className={`banner overlay ${overlay.ar.target !== 'main' ? 'unchecked' : ''}`}>
          {overlay.ar.target !== 'main' && (
            <span className="warn">
              <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden><path d="M8 1.8l6.6 12H1.4z" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" /><path d="M8 6.2v3.6M8 11.6v.2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>
              main 向きではないので、検査していません
            </span>
          )}
          <span className="banner-title">反映したあとの姿</span>
          <span>{overlay.ar.description}</span>
          <span className="mono dim">{overlay.ar.id} · {overlay.ar.branch} → {overlay.ar.target} · 比べた {overlay.ar.target === 'main' ? 'main' : '向き先'} {overlay.base.slice(0, 7)}</span>
          <span className="spacer" />
          <span className="pill added">追加 {[...overlay.changes.values()].filter((c) => c.kind === 'added').length}</span>
          <span className="pill changed">変化 {[...overlay.changes.values()].filter((c) => c.kind === 'changed').length}</span>
          {overlay.removed.length > 0 && <span className="pill removed">消える {overlay.removed.length}</span>}
          <button className="link" onClick={() => openOverlay(null)}>重ねるのをやめる</button>
        </div>
      )}

      <main className={`body ${blocked ? 'blocked' : ''}`}>
        {blocked ? (
          <section className="tree-pane empty">
            {overlay.state === 'unsupported' ? (
              <div className="empty-box">
                <div className="empty-title">main 向きではないので、ツリーは表示できません</div>
                <p>この AR の向き先は <code>{overlay.ar.target}</code> です。Logiforge は、main に向けた AR だけを検査し、反映したあとのスナップショットツリーを作ります。</p>
                <button className="btn" onClick={() => openOverlay(null)}>main のツリーに戻る</button>
              </div>
            ) : (
              <div className="empty-box">
                <div className="empty-title">今の main に重ねられません（衝突）</div>
                <p>この AR の差分は、今の main と次のファイルで衝突しています。opener が main を取り込み直すまで、反映したあとの姿は決まりません。</p>
                <ul className="paths">{overlay.paths.map((p) => <li key={p} className="mono">{p}</li>)}</ul>
                <button className="btn" onClick={() => openOverlay(null)}>main のツリーに戻る</button>
              </div>
            )}
          </section>
        ) : (
          view === '3d' ? (
            <LoadGuard onFallback={() => setView('graph')}>
            <Suspense fallback={<section className="graph-pane"><div className="loading">3D を読み込み中…</div></section>}>
              <Viewer3D
                ix={ix}
                rows={rows}
                selected={selected}
                onSelect={pick}
                onToggle={toggle}
                matches={matchSet}
                changes={changes}
                filtered={include !== null}
                searching={query.trim() !== ''}
                mobile={mobile}
                head={graphHead}
                fitKey={`${settledQuery.trim()}|${[...filters].sort().join(',')}|${showInactive}`}
                onFallback={() => setView('graph')}
                onRetry={() => setRetry3d((x) => x + 1)}
                key={retry3d}
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
              changes={changes}
              filtered={include !== null}
              mobile={mobile}
              searching={query.trim() !== ''}
              fitPending={settledQuery !== query}
              vertical={vertical}
              fitKey={`${vertical ? 'v' : 'h'}|${settledQuery.trim()}|${[...filters].sort().join(',')}|${showInactive}`}
              head={graphHead}
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
            changes={changes}
            filtered={include !== null}
            onExpandAll={() => userSetExpanded(new Set(ix.nodes.filter((n) => ix.children.has(n.id)).map((n) => n.id)))}
            onCollapseAll={() => userSetExpanded(new Set([ix.rootId]))}
            summary={{ total, rootValue: root.value }}
          />
          )
        )}
        {!blocked && (!mobile || sheet) && <DetailPane
          ix={ix}
          mobile={mobile}
          onClose={() => setSheet(false)}
          id={selected}
          change={selected && changes ? changes.get(selected) ?? null : null}
          unchecked={overlay.state === 'ok' && overlay.ar.target !== 'main'}
          notInMain={!!selected && !main.ix.byId.has(selected)}
          onReveal={(id) => { reveal(id); }}
        />}
      </main>
      {mobile && (
        <nav className="bottom-nav">
          <a className="on"><svg viewBox="0 0 20 20" width="20" height="20"><rect x="2" y="8" width="5" height="4" rx="1" /><rect x="13" y="3" width="5" height="4" rx="1" /><rect x="13" y="13" width="5" height="4" rx="1" /><path d="M7 10h3M10 5v10M10 5h3M10 15h3" fill="none" /></svg>ツリー</a>
          <a><svg viewBox="0 0 20 20" width="20" height="20"><path d="M3 4h14v10H8l-4 3v-3H3z" fill="none" /></svg>依頼</a>
          <a><svg viewBox="0 0 20 20" width="20" height="20"><circle cx="6" cy="5" r="2" fill="none" /><circle cx="6" cy="15" r="2" fill="none" /><circle cx="14" cy="15" r="2" fill="none" /><path d="M6 7v6M14 13V9a3 3 0 00-3-3H9" fill="none" /></svg>AR</a>
          <a><svg viewBox="0 0 20 20" width="20" height="20"><path d="M3 17V9M8 17V4M13 17v-6M17 17V7" fill="none" /></svg>状況</a>
        </nav>
      )}
    </div>
  );
}
