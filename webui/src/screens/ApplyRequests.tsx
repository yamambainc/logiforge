// apply request と履歴（docs/webui/history.md、design/webui/screens.md の「apply request と履歴」）。
// apply request の一覧と詳細（差分、先頭のコミット、レビュー）と、main のコミットの一覧（過去の時点）。
// 状態（見るもの、状態、選んだ apply request、作業の絞り込み）は URL に持つ。WebUI からは審査しない。
import { useEffect, useState } from 'react';
import { getApplyRequest, listApplyRequestsPage, listCommits, Problem, workSubjects, type ApplyRequest, type MainCommit } from '../api';
import { arTitle, AWAITING, byTime, FAILURE, opLabel, REVIEW, STATE } from '../ar';
import { useMobile } from '../hooks';
import { navigate, useLocation } from '../router';

const fmt = (iso?: string | null) => (iso ? new Date(iso).toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' }) : '');
// 作業としての絞り込み（ダッシュボードの件数と同じもの。listWork の subject_id）
const WORK: Record<string, string> = { review_waiting: 'レビュー待ち（作業）', fix_waiting: '修正待ち（作業）' };

export function ApplyRequests({ repo }: { repo: string }) {
  const loc = useLocation();
  const view = loc.searchParams.get('view') === 'commits' ? 'commits' : 'ars';
  const state = loc.searchParams.get('state') ?? 'open';
  const work = loc.searchParams.get('work') ?? '';
  const id = loc.searchParams.get('id');
  const mobile = useMobile();

  const go = (p: Record<string, string | null>, push = false) => {
    const q = new URLSearchParams(loc.search);
    for (const [k, v] of Object.entries(p)) { if (v) q.set(k, v); else q.delete(k); }
    const s = q.toString();
    navigate(`/r/${repo}/apply-requests${s ? '?' + s : ''}`, !push);
  };

  const tabs = (
    <div className="seg list-tabs" role="tablist" aria-label="見るもの">
      <button className={view === 'ars' ? 'on' : ''} onClick={() => go({ view: null, id: null }, true)}>apply request</button>
      <button className={view === 'commits' ? 'on' : ''} onClick={() => go({ view: 'commits', id: null, work: null }, true)}>main の履歴</button>
    </div>
  );
  const list = view === 'commits'
    ? <CommitList repo={repo} tabs={tabs} />
    : <ArList repo={repo} tabs={tabs} state={state} work={work} id={id} go={go} />;
  const detail = view === 'ars' && id
    ? <ArDetail key={id} repo={repo} id={id} mobile={mobile} onBack={() => go({ id: null }, true)} />
    : view === 'ars' && !mobile ? <section className="item-pane empty"><p className="dim">apply request を選ぶと、ここに差分とレビューが出ます。</p></section> : null;
  if (mobile) return <main className="split one">{detail ?? list}</main>;
  return <main className={`split ${view === 'commits' ? 'one' : ''}`}>{list}{detail}</main>;
}

function ArList({ repo, tabs, state, work, id, go }: { repo: string; tabs: React.ReactNode; state: string; work: string; id: string | null; go: (p: Record<string, string | null>, push?: boolean) => void }) {
  const [items, setItems] = useState<ApplyRequest[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const fail = (e: unknown) => { if (!(e instanceof Problem && e.status === 401)) setError(String(e)); };
  // 50 個ずつ読み、「もっと読む」で続きを読む
  const more = (cursor: string | null, live = () => true) => listApplyRequestsPage(repo, state === 'all' ? '' : state, cursor)
    .then((p) => { if (!live()) return; setItems((x) => [...(cursor ? x ?? [] : []), ...p.items]); setNext(p.next_cursor); })
    .catch((e) => live() && fail(e));
  useEffect(() => {
    let alive = true;
    const live = () => alive;
    setItems(null); setNext(null); setError(null);
    if (work) {
      // 作業としての絞り込み：作業の対象の apply request だけを、1 つずつ読む（数は件数と同じで、多くない）
      workSubjects(repo, work)
        .then((ids) => Promise.all([...ids].map((i) => getApplyRequest(repo, i))))
        .then((r) => live() && setItems(r))
        .catch((e) => live() && fail(e));
    } else void more(null, live);
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [repo, state, work]);
  return (
    <section className="list-pane" aria-label="apply request の一覧">
      {tabs}
      <div className="list-filters">
        {work ? (
          <span className="chip on">{WORK[work] ?? work}<button className="link" onClick={() => go({ work: null, id: null })} aria-label="作業の絞り込みを外す">×</button></span>
        ) : (
          <label>状態
            <select value={state} onChange={(e) => go({ state: e.target.value === 'open' ? null : e.target.value, id: null })}>
              <option value="open">open</option><option value="merged">反映済み</option><option value="closed">閉じた</option><option value="all">すべて</option>
            </select>
          </label>
        )}
      </div>
      {error ? <p className="dim pad">一覧を読めませんでした：{error}</p> : !items ? <div className="loading">読み込み中…</div> : items.length === 0 ? <p className="dim pad">apply request はありません。</p> : (
        <ul className="items">
          {items.map((a) => (
            <li key={a.id}>
              <a href={`?id=${a.id}`} className={a.id === id ? 'on' : ''} onClick={(e) => { e.preventDefault(); go({ id: a.id }, true); }}>
                <span className="item-text">{arTitle(a)}<span className="dim mono small-block">{a.branch} → {a.target}</span></span>
                {a.awaiting && <span className="kind">{AWAITING[a.awaiting]}</span>}
                <span className={`state s-${a.state}`}>{STATE[a.state]}</span>
              </a>
            </li>
          ))}
        </ul>
      )}
      {items && next && <button className="btn more" onClick={() => void more(next)}>もっと読む</button>}
    </section>
  );
}

function ArDetail({ repo, id, mobile, onBack }: { repo: string; id: string; mobile: boolean; onBack: () => void }) {
  const [a, setA] = useState<ApplyRequest | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    getApplyRequest(repo, id).then((r) => live && setA(r)).catch((e) => {
      if (live && !(e instanceof Problem && e.status === 401)) setError(e instanceof Problem && e.code === 'not_found' ? 'この apply request はありません' : String(e));
    });
    return () => { live = false; };
  }, [repo, id]);
  const back = mobile && <button className="link back" onClick={onBack}>← 一覧</button>;
  if (error) return <section className="item-pane">{back}<p className="dim">{error}</p></section>;
  if (!a) return <section className="item-pane">{back}<div className="loading">読み込み中…</div></section>;
  const open = (to: string) => (e: React.MouseEvent) => { e.preventDefault(); navigate(to); };
  const tree = (q: string) => `/r/${repo}/tree?${q}`;
  return (
    <section className="item-pane" aria-label="apply request">
      {back}
      <div className="d-head">
        <span className={`state s-${a.state}`}>{STATE[a.state]}</span>
        {a.awaiting && <span className="kind">{AWAITING[a.awaiting]}</span>}
        <span className="mono dim">{a.id}</span>
      </div>
      <h2 className="d-title">{arTitle(a)}</h2>
      <p className="dim mono">{a.branch} → {a.target} · 先頭 {a.head_commit.slice(0, 7)}</p>
      <div className="actions">
        {a.state === 'open' && <a className="btn" href={tree(`apply_request=${a.id}`)} onClick={open(tree(`apply_request=${a.id}`))}>反映したあとの姿を見る</a>}
        {a.state === 'merged' && a.merged_commit && <a className="btn" href={tree(`commit=${a.merged_commit}`)} onClick={open(tree(`commit=${a.merged_commit}`))}>反映した時点を見る</a>}
      </div>
      {a.target !== 'main' && <p className="note">main 向きではないので、Logiforge は検査も反映もしません。</p>}
      {a.closed_reason && <p className="note">閉じた理由：{a.closed_reason}</p>}
      {(a.warnings ?? []).map((w) => <p key={w} className="note">{w}</p>)}

      <h3 className="sec">差分</h3>
      {(a.operations ?? []).length === 0 ? <p className="dim">差分の操作はありません。</p> : (
        <ul className="ops">
          {a.operations!.map((op) => {
            const l = opLabel(op);
            return (
              <li key={op.position}>
                <span>{l.what}</span>
                <a className="mono" href={tree(`node=${l.target}`)} onClick={open(tree(`node=${l.target}`))}>{l.target}</a>
                {/* 検証結果のレポートは、折りたたみで、文字列のまま示す（design/webui/security.md） */}
                {op.kind === 'verdict.submit' && <details className="op-report"><summary>レポート</summary><p className="prose report">{op.report_body}</p></details>}
              </li>
            );
          })}
        </ul>
      )}

      <h3 className="sec">先頭のコミット</h3>
      <ol className="commits">
        {(a.head_commits ?? []).map((c) => (
          <li key={c.commit}><span className="mono">{c.commit.slice(0, 7)}</span><span className="dim">{c.seen_at ? fmt(c.seen_at) : '取り込み中'}</span>{c.author_session_id && <span className="dim mono">{c.author_session_id}</span>}</li>
        ))}
      </ol>

      <h3 className="sec">レビュー</h3>
      {(a.reviews ?? []).length === 0 ? <p className="dim">レビューはまだありません。</p> : (
        <ol className="reviews">
          {byTime(a.reviews!).map((r) => (
            <li key={r.id} className={r.valid ? '' : 'stale'}>
              <div className="r-head"><span className={`kind r-${r.kind}`}>{REVIEW[r.kind]}</span>{r.reviewer && <b className="r-who">{r.reviewer}</b>}<span className="dim">{fmt(r.created_at)}</span><span className="mono dim">{r.reviewer_session_id}</span><span className="mono dim">{r.commit.slice(0, 7)}</span>{!r.valid && <span className="dim">（前の先頭へのレビュー）</span>}</div>
              {/* 本文は Markdown を整形せず、文字列として示す（design/webui/security.md） */}
              {r.body && <p className="prose report">{r.body}</p>}
            </li>
          ))}
        </ol>
      )}

      {(a.merge_failures ?? []).length > 0 && (
        <>
          <h3 className="sec">反映できなかったこと</h3>
          <ul className="ops">{a.merge_failures!.map((f, i) => <li key={i}><span>{FAILURE[f.reason] ?? f.reason}</span><span className="dim">{f.detail}</span><span className="mono dim">{f.commit.slice(0, 7)}</span></li>)}</ul>
        </>
      )}
    </section>
  );
}

// main のコミットの一覧（listCommits）。押すと、その時点のビュワーを開く
function CommitList({ repo, tabs }: { repo: string; tabs: React.ReactNode }) {
  const [items, setItems] = useState<MainCommit[]>([]);
  const [next, setNext] = useState<string | null | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const more = (cursor: string | null) => listCommits(repo, cursor)
    .then((p) => { setItems((x) => [...x, ...p.items]); setNext(p.next_cursor); })
    .catch((e) => { if (!(e instanceof Problem && e.status === 401)) setError(String(e)); });
  useEffect(() => { void more(null); /* 最初の 50 個 */ }, [repo]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <section className="list-pane wide" aria-label="main の履歴">
      {tabs}
      {error ? <p className="dim pad">履歴を読めませんでした：{error}</p> : next === undefined ? <div className="loading">読み込み中…</div> : (
        <>
          <ul className="items">
            {items.map((c) => {
              const to = `/r/${repo}/tree?commit=${c.commit}`;
              return (
                <li key={c.commit}>
                  <a href={to} onClick={(e) => { e.preventDefault(); navigate(to); }}>
                    <span className="mono">#{c.number}</span>
                    <span className="item-text">{c.apply_request_id ? <span className="mono">{c.apply_request_id}</span> : <span className="dim">反映によらないコミット</span>}</span>
                    <span className="dim">{fmt(c.committed_at)}</span>
                    <span className="mono dim">{c.commit.slice(0, 7)}</span>
                  </a>
                </li>
              );
            })}
          </ul>
          {next && <button className="btn more" onClick={() => void more(next)}>もっと読む</button>}
        </>
      )}
    </section>
  );
}
