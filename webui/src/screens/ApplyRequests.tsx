// apply request の画面（docs/webui/history.md、design/webui/screens.md の「apply request の画面」）。
// apply request の一覧と、選んだ apply request を反映したあとの木（ビュワーをそのまま使う）。過去の時点は、ビュワーの「時点」の欄で選ぶ。
// 右側は「木」（既定）と「レビュー」のタブ。approve と merge は、木を見たまま押せるよう、タブの外に置く。
// 状態（状態、選んだ apply request、作業の絞り込み、右側のタブ）は URL に持つ。
import { useEffect, useRef, useState } from 'react';
import { getApplyRequest, getReviewMaterial, listApplyRequestsPage, listOpenApplyRequests, Problem, withPendingRetry, workSubjects, type ApplyRequest, type ReviewKind, type ReviewMaterial } from '../api';
import { arTitle, AWAITING, byTime, DECISION, filterBySubjects, loadFailure, oncePer, opLabel, outsideSummary, REVIEW, STATE } from '../ar';
import { reauthorize, remainingMs } from '../auth';
import { clearDraft, loadDraft, saveDraft } from '../files';
import { useMobile } from '../hooks';
import { textOf, type Node } from '../model';
import { approveSummary, canMerge, canSendApprove, ownNotice, submitMerge, submitReview } from '../review';
import { navigate, useLocation } from '../router';
import { Markdown } from '../MarkdownLazy';
import type { Source } from '../source';
import { TreeScreen } from './TreeScreen';

const fmt = (iso?: string | null) => (iso ? new Date(iso).toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' }) : '');
// 作業としての絞り込み（ダッシュボードの件数と同じもの。listWork の subject_id）
const WORK: Record<string, string> = { review_waiting: 'レビュー待ち（作業）', fix_waiting: '修正待ち（作業）', approved: '承認済み（作業）' };
// レビューを送る直前に、アクセストークンの残りがこれより少なければ、先に authorize に行く（design/webui/auth.md）
const MIN_TOKEN_MS = 5 * 60_000;

export function ApplyRequests({ repo }: { repo: string }) {
  const loc = useLocation();
  const state = loc.searchParams.get('state') ?? 'open';
  const work = loc.searchParams.get('work') ?? '';
  const id = loc.searchParams.get('id');
  const tab = loc.searchParams.get('tab') === 'review' ? 'review' : 'tree';
  const mobile = useMobile();
  // 読み直しの回数。一覧は merge とレビューのあとにも、詳細と一覧はタブに戻ったときにも読み直す
  // （GitForge で直接 merge して戻ったときにも、今の状態を示すため）
  const [listRev, setListRev] = useState(0);
  const [detailRev, setDetailRev] = useState(0);
  useEffect(() => {
    const reload = oncePer(1000, () => { setListRev((n) => n + 1); setDetailRev((n) => n + 1); });
    const again = () => { if (document.visibilityState === 'visible') reload(); };
    window.addEventListener('focus', again);
    document.addEventListener('visibilitychange', again);
    return () => { window.removeEventListener('focus', again); document.removeEventListener('visibilitychange', again); };
  }, []);

  const go = (p: Record<string, string | null>, push = false) => {
    const q = new URLSearchParams(loc.search);
    for (const [k, v] of Object.entries(p)) { if (v) q.set(k, v); else q.delete(k); }
    const s = q.toString();
    navigate(`/r/${repo}/apply-requests${s ? '?' + s : ''}`, !push);
  };

  const list = <ArList repo={repo} state={state} work={work} id={id} go={go} rev={listRev} />;
  const detail = id
    ? <ArDetail key={id} repo={repo} id={id} rev={detailRev} onChanged={() => setListRev((n) => n + 1)} tab={tab} onTab={(t) => go({ tab: t === 'tree' ? null : t }, true)} mobile={mobile} onBack={() => go({ id: null, tab: null }, true)} />
    : !mobile ? <section className="item-pane empty"><p className="dim">apply request を選んでください。</p></section> : null;
  if (mobile) return <main className="split one">{detail ?? list}</main>;
  return <main className="split">{list}{detail}</main>;
}

function ArList({ repo, state, work, id, go, rev }: { repo: string; state: string; work: string; id: string | null; go: (p: Record<string, string | null>, push?: boolean) => void; rev: number }) {
  const [items, setItems] = useState<ApplyRequest[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  // 読み直しに失敗したら、今の一覧を残す
  const loaded = useRef(false);
  const fail = (e: unknown) => { const f = loadFailure(e, loaded.current); setError(f.error); setNote(f.note); };
  // 50 個ずつ読み、「もっと読む」で続きを読む
  const more = (cursor: string | null, live = () => true) => listApplyRequestsPage(repo, state === 'all' ? '' : state, cursor)
    .then((p) => { if (!live()) return; loaded.current = true; setNote(null); setItems((x) => [...(cursor ? x ?? [] : []), ...p.items]); setNext(p.next_cursor); })
    .catch((e) => live() && fail(e));
  // 読み直しでは、読み終えるまで今の一覧を示したままにする（絞り込みを変えたときだけ空にする）
  const shown = useRef('');
  useEffect(() => {
    let alive = true;
    const live = () => alive;
    const key = `${repo}\n${state}\n${work}`;
    if (shown.current !== key) { shown.current = key; loaded.current = false; setItems(null); setNext(null); setNote(null); }
    setError(null);
    if (work) {
      // 作業としての絞り込み：作業の一覧と、open の apply request の一覧を 1 回ずつ読み、識別子で絞る。
      // 1 つずつは読まない（design/backend/list-reads.md）
      Promise.all([workSubjects(repo, work), listOpenApplyRequests(repo)])
        .then(([ids, all]) => { if (!live()) return; loaded.current = true; setNote(null); setItems(filterBySubjects(all, ids)); })
        .catch((e) => live() && fail(e));
    } else void more(null, live);
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [repo, state, work, rev]);
  return (
    <section className="list-pane" aria-label="apply request の一覧">
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
      {note && <p className="note">{note}</p>}
      {error ? <p className="dim pad">一覧を読めませんでした：{error}</p> : !items ? <div className="loading">読み込み中…</div> : items.length === 0 ? <p className="dim pad">apply request はありません。</p> : (
        <ul className="items">
          {items.map((a) => (
            <li key={a.id}>
              <a href={`?id=${a.id}`} className={a.id === id ? 'on' : ''} onClick={(e) => { e.preventDefault(); go({ id: a.id, tab: null }, true); }}>
                <span className="item-text">{arTitle(a)}<span className="dim mono small-block">{a.branch} → {a.target}</span></span>
                {a.awaiting && <span className="kind">{AWAITING[a.awaiting]}</span>}
                {a.review_decision && <span className="kind">{DECISION[a.review_decision]}</span>}
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

type Tab = 'tree' | 'review';

function ArDetail({ repo, id, rev, onChanged, tab, onTab, mobile, onBack }: { repo: string; id: string; rev: number; onChanged: () => void; tab: Tab; onTab: (t: Tab) => void; mobile: boolean; onBack: () => void }) {
  const [a, setA] = useState<ApplyRequest | null>(null);
  const [error, setError] = useState<string | null>(null);
  // レビューを送ったあとの言葉（反映したか、反映できなかった理由など）。反映して open でなくなっても示す
  const [notice, setNotice] = useState<string | null>(null);
  // レビューの材料（CLI の reviewer と同じもの）。approve で送る先頭のコミットは、材料を作ったコミット
  const [material, setMaterial] = useState<ReviewMaterial | null>(null);
  const [mError, setMError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  // レビューのタブでノードを押したら、木のタブでそのノードを選ぶ
  const [focus, setFocus] = useState<string | null>(null);
  const [reloadNote, setReloadNote] = useState<string | null>(null);
  const loaded = useRef(false);
  loaded.current = a !== null;
  const showNode = (n: string) => { setFocus(n); onTab('tree'); };
  useEffect(() => {
    let live = true;
    getApplyRequest(repo, id).then((r) => { if (live) { setA(r); setError(null); setReloadNote(null); } }).catch((e) => {
      if (!live) return;
      // 読み直し（もう示している）の失敗は、示しているものを残す
      const f = loadFailure(e, loaded.current, 'この apply request はありません');
      setError(f.error); setReloadNote(f.note);
    });
    return () => { live = false; };
  }, [repo, id, rev]);
  // main 向きでなければ、Logiforge は検査しないので、材料はない（overlay_unsupported）
  const wantMaterial = !!a && a.state === 'open' && a.target === 'main';
  useEffect(() => {
    let live = true;
    setMaterial(null); setMError(null);
    if (!wantMaterial) return;
    withPendingRetry(() => getReviewMaterial(repo, id), (w) => { if (live) setPending(w); })
      .then((m) => { if (live) setMaterial(m); })
      .catch((e) => {
        if (!live || (e instanceof Problem && e.status === 401)) return;
        setMError(e instanceof Problem && e.code === 'snapshot_pending_timeout' ? '先頭のコミットを、まだ取り込んでいます。少し待って、読み直してください。' : e instanceof Problem ? e.detail || e.code : String(e));
      });
    return () => { live = false; };
  }, [repo, id, a?.head_commit, wantMaterial]);
  const back = mobile && <button className="link back" onClick={onBack}>← 一覧</button>;
  if (error) return <section className="item-pane">{back}<p className="dim">{error}</p></section>;
  if (!a) return <section className="item-pane">{back}<div className="loading">読み込み中…</div></section>;
  // merge とレビューで状態とラベルが変わるので、一覧も読み直す
  const changed = (r: ApplyRequest | null, m: string | null) => { if (r) setA(r); setNotice(m); onChanged(); };
  // 示す木：open なら反映したあと、反映済みなら反映したコミット。閉じたものには木はない
  const outside = outsideSummary(a);
  const source: Source | null = a.state === 'open' ? { kind: 'ar', id: a.id } : a.state === 'merged' && a.merged_commit ? { kind: 'commit', commit: a.merged_commit } : null;
  return (
    <section className="ar-pane" aria-label="apply request">
      <div className="ar-head">
        {back}
        <div className="d-head">
          <span className={`state s-${a.state}`}>{STATE[a.state]}</span>
          {a.awaiting && <span className="kind">{AWAITING[a.awaiting]}</span>}
          {a.url ? <a className="mono" href={a.url} target="_blank" rel="noopener noreferrer">pull request #{a.id}</a> : <span className="mono dim">{a.id}</span>}
          <span className="d-title">{arTitle(a)}</span>
          <span className="dim mono">{a.branch} → {a.target} · 先頭 {a.head_commit.slice(0, 7)}</span>
        </div>
        {(a.warnings ?? []).map((w) => <p key={w} className="note">{w}</p>)}
        {outside && (a.outside_paths!.length === 0 ? <p className="dim outside">{outside}</p> : (
          <details open className="note warn-note outside"><summary>{outside}</summary><ul className="mono">{a.outside_paths!.map((p) => <li key={p}>{p}</li>)}</ul></details>
        ))}
        {notice && <p className="note" role="status">{notice}</p>}
        {reloadNote && <p className="note">{reloadNote}</p>}
        <div className="ar-bar">
          <div className="seg ar-tabs" role="tablist" aria-label="右側">
            <button role="tab" aria-selected={tab === 'tree'} className={tab === 'tree' ? 'on' : ''} onClick={() => onTab('tree')}>木</button>
            <button role="tab" aria-selected={tab === 'review'} className={tab === 'review' ? 'on' : ''} onClick={() => onTab('review')}>レビュー</button>
          </div>
          <span className="spacer" />
          {a.state === 'open' && (
            <div className="ar-actions">
              <ApproveBar repo={repo} a={a} material={material} mError={mError} onChanged={changed} />
              {canMerge(a) && <MergeBar repo={repo} a={a} onChanged={changed} />}
            </div>
          )}
        </div>
      </div>
      {tab === 'tree' ? (
        source ? <TreeScreen key={a.head_commit + (a.merged_commit ?? '') + (focus ?? '')} repo={repo} fixed={source} node={focus} /> : <div className="item-pane empty"><p className="dim">閉じた apply request なので、反映したあとの木はありません。</p></div>
      ) : (
        <div className="item-pane">
          <ReviewTab repo={repo} a={a} material={material} mError={mError} pending={pending} onChanged={changed} onNode={showNode} />
        </div>
      )}
    </section>
  );
}

const VALUE = (v: boolean | null) => (v === true ? '真' : v === false ? '偽' : '未決');
const KINDS: { k: ReviewKind; label: string }[] = [{ k: 'comment', label: 'コメント' }, { k: 'request_changes', label: 'request_changes' }];
type Changed = (a: ApplyRequest | null, notice: string | null) => void;

// レビューを送る（approve も、コメントも）。送ったあとの言葉は onChanged で示す。書きかけを消すかを返す
async function sendReview(repo: string, a: ApplyRequest, k: ReviewKind, commit: string, body: string, onChanged: Changed, setError: (m: string | null) => void): Promise<boolean> {
  // 残りが少なければ、書きかけを置いて先に authorize に行く（送っている途中で期限が来ないように）
  if (remainingMs() < MIN_TOKEN_MS && !(await reauthorize())) return false;
  setError(null);
  const r = await submitReview(repo, a.id, k, commit, body);
  switch (r.kind) {
    case 'sent': onChanged(r.ar, r.message); return true;
    // 新しい先頭のコミットを読み直して示し、もう一度確かめさせる。書きかけは残す
    case 'conflict': onChanged(r.ar ?? null, r.message); return false;
    case 'not_open': onChanged(r.ar, r.message); return true;
    // 確かめに通らず、approve を出さなかった。書きかけは残す
    case 'refused': setError(r.message); if (r.ar) onChanged(r.ar, null); return false;
    case 'error': setError(r.message); return false;
    case 'unauthenticated': return false;
  }
}

// approve（docs/apply-request/review.md）。押したら、先頭のコミットを示して確かめてから送る
function ApproveBar({ repo, a, material, mError, onChanged }: { repo: string; a: ApplyRequest; material: ReviewMaterial | null; mError: string | null; onChanged: Changed }) {
  const main = a.target === 'main';
  // 示す先頭のコミット。材料があれば、材料を作ったコミット（approve で送るのは、確かめたこのコミット）
  const shown = material?.commit ?? a.head_commit;
  // approve を確かめた先頭のコミット。示す先頭のコミットが変わったら、確かめ直させる
  const [confirmFor, setConfirmFor] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const own = ownNotice(a);
  const summary = approveSummary(a, shown, (material?.operations ?? a.operations ?? []).length);
  const start = () => {
    if (main && !material) { setError(mError ? `レビューの材料を読めませんでした：${mError}` : 'レビューの材料を読み込んでいます。少し待ってから押してください。'); return; }
    setError(null); setConfirmFor(shown);
  };
  const send = async () => {
    setBusy(true);
    await sendReview(repo, a, 'approve', confirmFor!, '', onChanged, setError);
    setBusy(false); setConfirmFor(null);
  };
  return (
    <div className="ar-action">
      {own && <p className="note" role="status">{own}</p>}
      {error && <p className="form-error" role="alert">{error}</p>}
      {canSendApprove(confirmFor, shown) ? (
        <div className="confirm" role="dialog" aria-label="approve の確かめ">
          <p><b>この先頭のコミットを approve します。</b> <span className="mono">{summary.commit}</span></p>
          <p>{summary.lines.join('')}</p>
          <div className="form-actions">
            <button type="button" className="btn primary" disabled={busy} onClick={() => void send()}>{busy ? '送っています…' : 'approve を送る'}</button>
            <button type="button" className="btn" disabled={busy} onClick={() => setConfirmFor(null)}>やめる</button>
          </div>
        </div>
      ) : (
        <div className="form-actions"><button type="button" className="btn primary" onClick={start}>approve する</button></div>
      )}
    </div>
  );
}

// merge（docs/apply-request/review.md の「merge」）。approve された apply request を、サインインした人のトークンで merge する。
// 押したら確かめてから送る。バックエンドが、直前に今の main で反映の条件を確かめ直す。
function MergeBar({ repo, a, onChanged }: { repo: string; a: ApplyRequest; onChanged: Changed }) {
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const send = async () => {
    if (remainingMs() < MIN_TOKEN_MS && !(await reauthorize())) return;
    setBusy(true); setError(null);
    const r = await submitMerge(repo, a.id);
    setBusy(false); setConfirm(false);
    switch (r.kind) {
      case 'sent': onChanged(r.ar, r.message); break;
      case 'refused': setError(r.message); if (r.ar) onChanged(r.ar, null); break;
      case 'conflict': case 'not_open': case 'error': setError(r.message); break;
      case 'unauthenticated': break;
    }
  };
  return (
    <div className="ar-action">
      {error && <p className="form-error" role="alert">{error}</p>}
      {confirm ? (
        <div className="confirm" role="dialog" aria-label="merge の確かめ">
          <p><b>この先頭のコミットを、main に squash merge します。</b> <span className="mono">{a.head_commit.slice(0, 7)}</span></p>
          <div className="form-actions">
            <button type="button" className="btn primary" disabled={busy} onClick={() => void send()}>{busy ? '送っています…' : 'merge を送る'}</button>
            <button type="button" className="btn" disabled={busy} onClick={() => setConfirm(false)}>やめる</button>
          </div>
        </div>
      ) : (
        <div className="form-actions"><button type="button" className="btn primary" onClick={() => setConfirm(true)}>merge する</button></div>
      )}
    </div>
  );
}

// レビューのタブ：差分、先頭のコミット、レビューとコメントの一覧、コメントを書く欄
function ReviewTab({ repo, a, material, mError, pending, onChanged, onNode }: { repo: string; a: ApplyRequest; material: ReviewMaterial | null; mError: string | null; pending: boolean; onChanged: Changed; onNode: (id: string) => void }) {
  return (
    <>
      <h3 className="sec">差分</h3>
      {(a.operations ?? []).length === 0 ? <p className="dim">差分の操作はありません。</p> : (
        <ul className="ops">
          {a.operations!.map((op) => {
            const l = opLabel(op);
            return (
              <li key={op.position}>
                <span>{l.what}</span>
                <NodeLink id={l.target} onNode={onNode} />
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

      {(a.comments ?? []).length > 0 && (
        <>
          {/* pull request のコメントを、そのまま示す。反映の失敗と close の理由も、ここに人が読む文で入る */}
          <h3 className="sec">コメント</h3>
          <ol className="reviews">
            {a.comments!.map((c, i) => (
              <li key={i}>
                <div className="r-head"><b className="r-who">{c.author}</b><span className="dim">{fmt(c.created_at)}</span></div>
                <Markdown className="report" text={c.body} />
              </li>
            ))}
          </ol>
        </>
      )}

      {a.state === 'open' && <ReviewPane repo={repo} a={a} material={material} mError={mError} pending={pending} onChanged={onChanged} onNode={onNode} />}
    </>
  );
}

// コメントと request_changes を書く欄（docs/webui/history.md の「apply request の閲覧」）。
// CLI の reviewer と同じレビューの材料（getReviewMaterial）を示す。説明欄は示さない（材料に含めないものを、判断の横に置かないため）。
function ReviewPane({ repo, a, material, mError, pending, onChanged, onNode }: { repo: string; a: ApplyRequest; material: ReviewMaterial | null; mError: string | null; pending: boolean; onChanged: Changed; onNode: (id: string) => void }) {
  const [kind, setKind] = useState<ReviewKind>('comment');
  const [body, setBody] = useState(() => loadDraft(repo, a.id)?.body ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // 書きかけ（トークンの期限で authorize に行き直しても失わない）。サインアウトで消える
  useEffect(() => { saveDraft(repo, a.id, { body, fileNames: [], links: [] }); }, [repo, a.id, body]);
  const main = a.target === 'main';
  const shown = material?.commit ?? a.head_commit;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!body.trim()) { setError('本文を書いてください。'); return; }
    setBusy(true);
    if (await sendReview(repo, a, kind, shown, body, onChanged, setError)) { clearDraft(repo, a.id); setBody(''); }
    setBusy(false);
  };
  return (
    <div className="review-pane">
      <h3 className="sec">レビューする</h3>
      <h4 className="sub">レビューの材料</h4>
      {!main ? <p className="dim">main 向きではないので、レビューの材料はありません。</p>
        : mError ? <p className="dim">レビューの材料を読めませんでした：{mError}</p>
        : !material ? <div className="loading">{pending ? '先頭のコミットを取り込んでいます。読み直しています…' : '読み込み中…'}</div>
        : <MaterialView m={material} onNode={onNode} />}
      <form className="answer-form" onSubmit={(e) => void submit(e)}>
        <div className="seg" role="radiogroup" aria-label="レビューの種類">
          {KINDS.map(({ k, label }) => <button key={k} type="button" role="radio" aria-checked={kind === k} className={kind === k ? 'on' : ''} onClick={() => setKind(k)}>{label}</button>)}
        </div>
        <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={5} aria-label="レビューの本文" placeholder="本文を書く" />
        {error && <p className="form-error" role="alert">{error}</p>}
        <div className="form-actions"><button className="btn primary" type="submit" disabled={busy}>{busy ? '送っています…' : kind === 'comment' ? 'コメントを送る' : 'request_changes を送る'}</button></div>
      </form>
    </div>
  );
}

// レビューの材料（docs/apply-request/review.md の「レビューの材料」）。差分、対象ノードの今の値、根までの仮説、兄弟ストーリー、引用
function MaterialView({ m, onNode }: { m: ReviewMaterial; onNode: (id: string) => void }) {
  const node = (id: string) => <NodeLink id={id} onNode={onNode} />;
  const line = (n: Node) => <li key={n.id}>{node(n.id)}<span>{textOf(n)}</span><span className="dim">{VALUE(n.value)}{n.inactive ? '・inactive' : ''}</span></li>;
  return (
    <div className="material">
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

// ノードへのリンク。押すと、右側の木のタブで、そのノードを選ぶ（加わるノードは今の main にないので、/tree には飛ばない）
function NodeLink({ id, onNode }: { id: string; onNode: (id: string) => void }) {
  return <button type="button" className="link mono" onClick={() => onNode(id)}>{id}</button>;
}
