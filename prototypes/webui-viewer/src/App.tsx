import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { listApplyRequests, listNodes, Problem, type ApplyRequestSummary } from './api';
import { buildIndex, diff, textOf, visibleRows, withAncestors, ancestors, withRemoved, type Index, type Change } from './model';
import { TreePane } from './TreePane';
import { DetailPane } from './DetailPane';

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

  useEffect(() => {
    (async () => {
      const r = await listNodes(N);
      const ix = buildIndex(r.nodes);
      const depthOpen = Number(params.get('open') ?? 2);
      const open = new Set(r.nodes.filter((n) => (ix.depth.get(n.id) ?? 0) < depthOpen).map((n) => n.id));
      setMain({ ix, commit: r.commit, number: r.number });
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
      // 比べる相手は、重ねた向き先のコミット（r.commit）の main。手元の main と違えば読み直す（試作では同じ）
      const after = buildIndex(r.nodes);
      const d = diff(base, after);
      const ix = d.removed.length ? buildIndex(withRemoved(after, base, d.removed)) : after;
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

  const toggle = useCallback((id: string) => setExpanded((e) => { const s = new Set(e); if (s.has(id)) s.delete(id); else s.add(id); return s; }), []);
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

  const toggleFilter = (k: Filter) => setFilters((f) => { const g = new Set(f); if (g.has(k)) g.delete(k); else g.add(k); return g; });
  const total = ix.nodes.length;
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
        <div className="view">
          <div className="seg">
            <button className={overlay.state === 'none' ? 'on' : ''} onClick={() => openOverlay(null)}>
              main <span className="mono dim">#{main.number} · {main.commit.slice(0, 7)}</span>
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
        <div className="chips">
          {FILTERS.filter((f) => !f.overlayOnly || overlay.state === 'ok').map((f) => (
            <button key={f.key} className={`chip chip-${f.key} ${filters.has(f.key) ? 'on' : ''}`} onClick={() => toggleFilter(f.key)}>
              {f.label}
            </button>
          ))}
          <label className="check">
            <input type="checkbox" checked={showInactive} onChange={(e) => setShowInactive(e.target.checked)} /> inactive
          </label>
        </div>
      </div>

      {overlay.state === 'ok' && (
        <div className="banner overlay">
          <span className="banner-title">反映したあとの姿</span>
          <span>{overlay.ar.description}</span>
          <span className="mono dim">{overlay.ar.id} · {overlay.ar.branch} → main · 比べた main {overlay.base.slice(0, 7)}</span>
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
          <TreePane
            ix={ix}
            rows={rows}
            selected={selected}
            onSelect={setSelected}
            onToggle={toggle}
            query={query.trim()}
            matches={matchSet}
            changes={changes}
            filtered={include !== null}
            onExpandAll={() => setExpanded(new Set(ix.nodes.filter((n) => ix.children.has(n.id)).map((n) => n.id)))}
            onCollapseAll={() => setExpanded(new Set([ix.rootId]))}
            summary={{ total, rootValue: root.value }}
          />
        )}
        {!blocked && <DetailPane
          ix={ix}
          id={selected}
          change={selected && changes ? changes.get(selected) ?? null : null}
          onReveal={reveal}
        />}
      </main>
    </div>
  );
}
