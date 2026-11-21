// apply request と履歴（docs/webui/history.md、design/webui/screens.md の「apply request と履歴」）。
// apply request の一覧と詳細（差分、先頭のコミット、レビュー）と、main のコミットの一覧（過去の時点）。
// 状態（見るもの、状態、選んだ apply request、作業の絞り込み）は URL に持つ。
// open の apply request は、その repository に Write がある利用者が、ここでレビューできる（approve なら反映まで）。
import { useEffect, useState } from 'react';
import { getApplyRequest, getReviewMaterial, listApplyRequestsPage, listCommits, listOpenApplyRequests, Problem, withPendingRetry, workSubjects, type ApplyRequest, type MainCommit, type ReviewKind, type ReviewMaterial } from '../api';
import { arTitle, AWAITING, byTime, FAILURE, filterBySubjects, opLabel, REVIEW, STATE } from '../ar';
import { reauthorize, remainingMs } from '../auth';
import { clearDraft, loadDraft, saveDraft } from '../files';
import { useMobile } from '../hooks';
import { textOf, type Node } from '../model';
import { approveSummary, canSendApprove, ownNotice, submitReview } from '../review';
import { navigate, useLocation } from '../router';
import { Markdown } from '../MarkdownLazy';

const fmt = (iso?: string | null) => (iso ? new Date(iso).toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' }) : '');
// 作業としての絞り込み（ダッシュボードの件数と同じもの。listWork の subject_id）
const WORK: Record<string, string> = { review_waiting: 'レビュー待ち（作業）', fix_waiting: '修正待ち（作業）' };
// レビューを送る直前に、アクセストークンの残りがこれより少なければ、先に authorize に行く（design/webui/auth.md）
const MIN_TOKEN_MS = 5 * 60_000;

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
      // 作業としての絞り込み：作業の一覧と、open の apply request の一覧を 1 回ずつ読み、識別子で絞る。
      // 1 つずつは読まない（design/backend/list-reads.md）
      Promise.all([workSubjects(repo, work), listOpenApplyRequests(repo)])
        .then(([ids, all]) => live() && setItems(filterBySubjects(all, ids)))
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
  // レビューを送ったあとの言葉（反映したか、反映できなかった理由など）。反映して open でなくなっても示す
  const [notice, setNotice] = useState<string | null>(null);
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
                {/* 検証結果のレポートは、折りたたみで、Markdown として示す（design/webui/markdown.md） */}
                {op.kind === 'verdict.submit' && <details className="op-report"><summary>レポート</summary><Markdown className="report" text={op.report_body} /></details>}
              </li>
            );
          })}
        </ul>
      )}

      <h3 className="sec">先頭のコミット</h3>
      <ol className="commits">
        {(a.head_commits ?? []).map((c) => (
          <li key={c.commit}><span className="mono">{c.commit.slice(0, 7)}</span><span className="dim">{c.seen_at ? fmt(c.seen_at) : '取り込み中'}</span></li>
        ))}
      </ol>

      <h3 className="sec">レビュー</h3>
      {(a.reviews ?? []).length === 0 ? <p className="dim">レビューはまだありません。</p> : (
        <ol className="reviews">
          {byTime(a.reviews!).map((r) => (
            <li key={r.id} className={r.valid ? '' : 'stale'}>
              <div className="r-head"><span className={`kind r-${r.kind}`}>{REVIEW[r.kind]}</span>{r.reviewer && <b className="r-who">{r.reviewer}</b>}<span className="dim">{fmt(r.created_at)}</span><span className="mono dim">{r.commit.slice(0, 7)}</span>{!r.valid && <span className="dim">（前の先頭へのレビュー）</span>}</div>
              {/* 本文は Markdown として示す。生の HTML は通さない（design/webui/markdown.md） */}
              {r.body && <Markdown className="report" text={r.body} />}
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

      {notice && <p className="note" role="status">{notice}</p>}
      {a.state === 'open' && <ReviewPane repo={repo} a={a} onChanged={(r, m) => { if (r) setA(r); setNotice(m); }} />}
    </section>
  );
}

const VALUE = (v: boolean | null) => (v === true ? '真' : v === false ? '偽' : '未決');
const CHANGE: Record<string, string> = { added: '加わる', modified: '変わる', deleted: '消える' };
const KINDS: { k: ReviewKind; label: string }[] = [{ k: 'comment', label: 'コメント' }, { k: 'request_changes', label: 'request_changes' }, { k: 'approve', label: 'approve' }];

// レビューの欄（docs/webui/history.md の「apply request の閲覧」）。
// CLI の reviewer と同じレビューの材料（getReviewMaterial）を示す。説明欄は示さない（材料に含めないものを、判断の横に置かないため）。
function ReviewPane({ repo, a, onChanged }: { repo: string; a: ApplyRequest; onChanged: (a: ApplyRequest | null, notice: string | null) => void }) {
  const [material, setMaterial] = useState<ReviewMaterial | null>(null);
  const [mError, setMError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [kind, setKind] = useState<ReviewKind>('comment');
  const [body, setBody] = useState(() => loadDraft(repo, a.id)?.body ?? '');
  // approve を確かめた先頭のコミット。示す先頭のコミットが変わったら、確かめ直させる
  const [confirmFor, setConfirmFor] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<string | null>(null);
  // 書きかけ（トークンの期限で authorize に行き直しても失わない）。サインアウトで消える
  useEffect(() => { saveDraft(repo, a.id, { body, fileNames: [], links: [] }); }, [repo, a.id, body]);

  const main = a.target === 'main';
  useEffect(() => {
    let live = true;
    setMaterial(null); setMError(null);
    // main 向きでなければ、Logiforge は検査しないので、材料はない（overlay_unsupported）
    if (!main) return;
    withPendingRetry(() => getReviewMaterial(repo, a.id), (w) => { if (live) setPending(w); })
      .then((m) => { if (live) setMaterial(m); })
      .catch((e) => {
        if (!live || (e instanceof Problem && e.status === 401)) return;
        setMError(e instanceof Problem && e.code === 'snapshot_pending_timeout' ? '先頭のコミットを、まだ取り込んでいます。少し待って、読み直してください。' : e instanceof Problem ? e.detail || e.code : String(e));
      });
    return () => { live = false; };
  }, [repo, a.id, a.head_commit, main]);

  // 示す先頭のコミット。材料があれば、材料を作ったコミット（approve で送るのは、確かめたこのコミット）
  const shown = material?.commit ?? a.head_commit;
  const confirming = canSendApprove(confirmFor, shown);
  const own = ownNotice(a);
  const summary = approveSummary(a, shown, (material?.operations ?? a.operations ?? []).length);
  const tree = `/r/${repo}/tree?apply_request=${a.id}`;

  const send = async (k: ReviewKind, commit: string) => {
    // 残りが少なければ、書きかけを置いて先に authorize に行く（送っている途中で期限が来ないように）
    if (remainingMs() < MIN_TOKEN_MS && !(await reauthorize())) return;
    setBusy(true); setError(null); setConflict(null);
    const r = await submitReview(repo, a.id, k, commit, body);
    setBusy(false);
    switch (r.kind) {
      case 'sent': clearDraft(repo, a.id); setBody(''); setConfirmFor(null); onChanged(r.ar, r.message); break;
      // 新しい先頭のコミットを読み直して示し、もう一度確かめさせる。書きかけは残す
      case 'conflict': setConfirmFor(null); setConflict(r.message); if (r.ar) onChanged(r.ar, null); break;
      case 'not_open': clearDraft(repo, a.id); setConfirmFor(null); onChanged(r.ar, r.message); break;
      case 'error': setError(r.message); break;
      case 'unauthenticated': break;
    }
  };
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (kind !== 'approve' && !body.trim()) { setError('本文を書いてください。'); return; }
    if (kind === 'approve') {
      if (main && !material) { setError('レビューの材料を読み込んでから approve してください。'); return; }
      // 押したら、先頭のコミットと反映したあとの姿を示して、確かめてから送る
      setError(null); setConfirmFor(shown); return;
    }
    void send(kind, shown);
  };

  return (
    <div className="review-pane">
      <h3 className="sec">レビューする</h3>
      <p className="dim small">レビューできるのは、この repository に Write がある人だけです。レビューする先頭のコミット：<span className="mono">{shown.slice(0, 7)}</span></p>
      {conflict && <p className="note" role="status">{conflict}</p>}
      <h4 className="sub">レビューの材料</h4>
      {!main ? <p className="dim">main 向きではないので、レビューの材料はありません。</p>
        : mError ? <p className="dim">レビューの材料を読めませんでした：{mError}</p>
        : !material ? <div className="loading">{pending ? '先頭のコミットを取り込んでいます。読み直しています…' : '読み込み中…'}</div>
        : <MaterialView repo={repo} m={material} />}
      <form className="answer-form" onSubmit={submit}>
        <div className="seg" role="radiogroup" aria-label="レビューの種類">
          {KINDS.map(({ k, label }) => <button key={k} type="button" role="radio" aria-checked={kind === k} className={kind === k ? 'on' : ''} onClick={() => { setKind(k); setConfirmFor(null); }}>{label}</button>)}
        </div>
        {kind === 'approve' && own && <p className="note" role="status">{own}</p>}
        <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={5} aria-label="レビューの本文" placeholder={kind === 'approve' ? '本文（なくてもよい）' : '本文を書く'} />
        {error && <p className="form-error" role="alert">{error}</p>}
        {kind === 'approve' && confirming ? (
          <div className="confirm" role="dialog" aria-label="approve の確かめ">
            <p><b>この先頭のコミットを approve します。</b></p>
            <p className="mono">{summary.commit}</p>
            <p>{summary.lines.join('')}</p>
            {summary.own && <p className="note">{summary.own}</p>}
            <p><a href={tree} target="_blank" rel="noopener">反映したあとの姿を見る</a>（別のタブで開く）</p>
            <div className="form-actions">
              <button type="button" className="btn primary" disabled={busy} onClick={() => void send('approve', confirmFor!)}>{busy ? '送っています…' : 'approve を送る'}</button>
              <button type="button" className="btn" disabled={busy} onClick={() => setConfirmFor(null)}>やめる</button>
            </div>
          </div>
        ) : (
          <div className="form-actions"><button className="btn primary" type="submit" disabled={busy}>{busy ? '送っています…' : kind === 'approve' ? 'approve する' : kind === 'comment' ? 'コメントを送る' : 'request_changes を送る'}</button></div>
        )}
      </form>
    </div>
  );
}

// レビューの材料（docs/apply-request/review.md の「レビューの材料」）。差分、対象ノードの今の値、根までの仮説、兄弟ストーリー、引用
function MaterialView({ repo, m }: { repo: string; m: ReviewMaterial }) {
  const node = (id: string) => {
    const to = `/r/${repo}/tree?node=${id}`;
    return <a className="mono" href={to} onClick={(e) => { e.preventDefault(); navigate(to); }}>{id}</a>;
  };
  const line = (n: Node) => <li key={n.id}>{node(n.id)}<span>{textOf(n)}</span><span className="dim">{VALUE(n.value)}{n.inactive ? '・inactive' : ''}</span></li>;
  return (
    <div className="material">
      {m.repair && (
        <>
          <h5>直すための変化（{m.repair.kind}）</h5>
          <ul className="ops">{m.repair.changes.map((c) => <li key={c.path}><span className="mono">{c.path}</span><span className="dim">{CHANGE[c.status] ?? c.status}</span></li>)}</ul>
        </>
      )}
      <h5>差分</h5>
      {m.operations.length === 0 ? <p className="dim">差分の操作はありません。</p> : (
        <ul className="ops">{m.operations.map((op) => { const l = opLabel(op); return <li key={op.position}><span>{l.what}</span>{node(l.target)}</li>; })}</ul>
      )}
      <h5>対象ノードの今の値</h5>
      {m.targets.length === 0 ? <p className="dim">ありません。</p> : <ul className="ops">{m.targets.map(line)}</ul>}
      <h5>親の仮説から根まで</h5>
      {m.ancestors.length === 0 ? <p className="dim">ありません。</p> : <ol className="ops">{m.ancestors.map(line)}</ol>}
      <h5>兄弟ストーリー</h5>
      {m.siblings.length === 0 ? <p className="dim">ありません。</p> : (
        <ul className="ops">{m.siblings.map((s) => <li key={s.id}>{node(s.id)}<span>{s.title}</span><span className="dim">{VALUE(s.value)}{s.inactive ? '・inactive' : ''}</span>{s.summary && <span className="dim small-block">{s.summary}</span>}</li>)}</ul>
      )}
      <h5>引用</h5>
      {m.citations.length === 0 ? <p className="dim">ありません。</p> : (
        <ul className="ops">
          {m.citations.map((c, i) => c.citation.target_kind === 'artifact'
            ? <li key={i}><span>artifact</span><span>{c.artifact?.name ?? ''}</span><span className="mono dim">{(c.citation.sha256 ?? '').slice(0, 12)}</span></li>
            : (
              <li key={i}>
                <span>部分木</span><span className="mono">{c.citation.node_id}</span><span className="dim">先頭のコミットの木</span>
                {(c.nodes ?? []).length > 0 && <details><summary>{c.nodes!.length} 個のノード</summary><ul className="ops">{c.nodes!.map((n) => <li key={n.id}><span className="mono">{n.id}</span><span>{textOf(n)}</span><span className="dim">{VALUE(n.value)}</span></li>)}</ul></details>}
              </li>
            ))}
        </ul>
      )}
    </div>
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
