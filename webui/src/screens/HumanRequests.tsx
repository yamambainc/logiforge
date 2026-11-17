// 人への依頼（docs/webui/human-requests.md、design/webui/screens.md の「人への依頼」）。
// 一覧（種類と状態で絞り込む）と本文と回答。状態（種類、状態、選んだ依頼）は URL に持つ。
import { useEffect, useState } from 'react';
import { answerHumanRequest, fetchAnswerAttachment, getHumanRequest, listHumanRequests, Problem, workSubjects, type AttachmentInput, type HumanRequest } from '../api';
import { reauthorize, remainingMs } from '../auth';
import { checkAttachments, clearDraft, HEAVY_ON_MOBILE, isWebLink, loadDraft, saveBlob, saveDraft, toBase64 } from '../files';
import { useMobile } from '../hooks';
import { navigate, useLocation } from '../router';
import { Markdown } from '../MarkdownLazy';

const KIND: Record<string, string> = { pvr: 'PVR', decision: '判断' };
const STATE: Record<string, string> = { open: 'open', answered: '回答済み' };
// 回答を送る直前に、アクセストークンの残りがこれより少なければ、先に authorize に行く（design/webui/auth.md）
const MIN_TOKEN_MS = 5 * 60_000;

export function HumanRequests({ repo }: { repo: string }) {
  const loc = useLocation();
  const kind = loc.searchParams.get('kind') ?? '';
  const state = loc.searchParams.get('state') ?? 'open';
  const id = loc.searchParams.get('id');
  // 作業として残っているものだけ（ダッシュボードの「回答済みの依頼」の件数と同じもの。listWork の human_request_answered）
  const work = loc.searchParams.get('work') === '1';
  const mobile = useMobile();
  const [items, setItems] = useState<HumanRequest[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    let live = true;
    setItems(null); setError(null);
    Promise.all([listHumanRequests(repo, kind, state === 'all' ? '' : state), work ? workSubjects(repo, 'human_request_answered') : Promise.resolve(null)])
      .then(([r, w]) => live && setItems(w ? r.filter((h) => w.has(h.id)) : r))
      .catch((e) => { if (live && !(e instanceof Problem && e.status === 401)) setError(String(e)); });
    return () => { live = false; };
  }, [repo, kind, state, work, reload]);

  // URL を変える。選んだ依頼は履歴に積み、絞り込みは置き換える
  const go = (p: Record<string, string | null>, push = false) => {
    const q = new URLSearchParams(loc.search);
    for (const [k, v] of Object.entries(p)) { if (v) q.set(k, v); else q.delete(k); }
    const s = q.toString();
    navigate(`/r/${repo}/human-requests${s ? '?' + s : ''}`, !push);
  };

  const list = (
    <section className="list-pane" aria-label="人への依頼の一覧">
      <div className="list-filters">
        <label>種類
          <select value={kind} onChange={(e) => go({ kind: e.target.value || null, id: null })}>
            <option value="">すべて</option><option value="pvr">PVR</option><option value="decision">判断</option>
          </select>
        </label>
        <label>状態
          <select value={state} onChange={(e) => go({ state: e.target.value === 'open' ? null : e.target.value, id: null })}>
            <option value="open">open</option><option value="answered">回答済み</option><option value="all">すべて</option>
          </select>
        </label>
        <label className="check"><input type="checkbox" checked={work} onChange={(e) => go({ work: e.target.checked ? '1' : null, id: null })} /> 作業として残っているもの</label>
      </div>
      {error ? <p className="dim">一覧を読めませんでした：{error}</p> : !items ? <div className="loading">読み込み中…</div> : items.length === 0 ? <p className="dim pad">人への依頼はありません。</p> : (
        <ul className="items">
          {items.map((h) => (
            <li key={h.id}>
              <a href={`?id=${h.id}`} className={h.id === id ? 'on' : ''} onClick={(e) => { e.preventDefault(); go({ id: h.id }, true); }}>
                <span className={`kind k-${h.kind}`}>{KIND[h.kind]}</span>
                <span className="item-text">{firstLine(h.body)}</span>
                <span className={`state s-${h.state}`}>{STATE[h.state]}</span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
  const detail = id
    ? <RequestDetail key={id} repo={repo} id={id} mobile={mobile} onBack={() => go({ id: null }, true)} onAnswered={() => setReload((x) => x + 1)} />
    : <section className="item-pane empty"><p className="dim">依頼を選ぶと、ここに本文が出ます。</p></section>;
  // モバイルは、一覧から選んだものを画面全体で示し、左上に戻るボタンを置く（design/webui/mobile.md）
  if (mobile) return <main className="split one">{id ? detail : list}</main>;
  return <main className="split">{list}{detail}</main>;
}

const firstLine = (s: string) => s.split('\n').find((l) => l.trim())?.replace(/^#+\s*/, '') ?? '';

function RequestDetail({ repo, id, mobile, onBack, onAnswered }: { repo: string; id: string; mobile: boolean; onBack: () => void; onAnswered: () => void }) {
  const [h, setH] = useState<HumanRequest | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [already, setAlready] = useState(false);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    getHumanRequest(repo, id).then((r) => live && setH(r)).catch((e) => {
      if (live && !(e instanceof Problem && e.status === 401)) setError(e instanceof Problem && e.code === 'not_found' ? 'この依頼はありません' : String(e));
    });
    return () => { live = false; };
  }, [repo, id, tick]);
  const back = mobile && <button className="link back" onClick={onBack}>← 一覧</button>;
  if (error) return <section className="item-pane">{back}<p className="dim">{error}</p></section>;
  if (!h) return <section className="item-pane">{back}<div className="loading">読み込み中…</div></section>;
  const node = `/r/${repo}/tree?node=${h.node_id}`;
  return (
    <section className="item-pane" aria-label="依頼">
      {back}
      <div className="d-head">
        <span className={`kind k-${h.kind}`}>{KIND[h.kind]}</span>
        <span className="mono dim">{h.id}</span>
        <span className="spacer" />
        <span className={`state s-${h.state}`}>{STATE[h.state]}</span>
      </div>
      <p className="target">対象：<a href={node} onClick={(e) => { e.preventDefault(); navigate(node); }} className="mono">{h.node_id}</a>（ビュワーで開く）</p>
      <h3 className="sec">{h.kind === 'pvr' ? '検証手順' : '問い'}</h3>
      {/* 本文は Markdown として示す。生の HTML は通さない（design/webui/markdown.md） */}
      <Markdown className="report" text={h.body} />
      {already && <p className="note" role="status">この依頼は、もう回答されています。</p>}
      {h.answer ? <Answer repo={repo} h={h} /> : <AnswerForm repo={repo} h={h} onDone={(r) => { setH(r); onAnswered(); }} onAlready={() => { setAlready(true); setTick((x) => x + 1); onAnswered(); }} />}
    </section>
  );
}

function Answer({ repo, h }: { repo: string; h: HumanRequest }) {
  const a = h.answer!;
  return (
    <div className="answer">
      <h3 className="sec">回答 <span className="dim">{new Date(a.answered_at).toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' })}</span></h3>
      <Markdown className="report" text={a.body} />
      {a.attachments.length > 0 && (
        <div className="files">
          {a.attachments.map((f) => f.kind === 'link'
            ? <div key={f.name + f.uri} className="file">{isWebLink(f.uri) ? <a href={f.uri} target="_blank" rel="noreferrer noopener">{f.name}</a> : <span>{f.name}</span>}<span className="dim">外部リンク</span></div>
            : <button key={f.name + f.sha256} className="file" onClick={async () => f.sha256 && saveBlob(await fetchAnswerAttachment(repo, h.id, f.sha256), f.name)}><span>{f.name}</span><span className="dim">保存</span></button>)}
        </div>
      )}
    </div>
  );
}

const ANSWER_ERRORS: Record<string, string> = {
  forbidden: '回答できるのは、利用者だけです。',
  too_large: '添付が大きすぎます。リンクで添えてください。',
};

function AnswerForm({ repo, h, onDone, onAlready }: { repo: string; h: HumanRequest; onDone: (r: HumanRequest) => void; onAlready: () => void }) {
  // 書きかけ（トークンの期限で authorize に行き直しても失わない）
  const [draft] = useState(() => loadDraft(repo, h.id));
  const [body, setBody] = useState(draft?.body ?? '');
  const [files, setFiles] = useState<File[]>([]);
  const [links, setLinks] = useState(draft?.links ?? []);
  const [lost, setLost] = useState<string[]>(draft?.fileNames ?? []);
  const [linkName, setLinkName] = useState('');
  const [linkUri, setLinkUri] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => { saveDraft(repo, h.id, { body, fileNames: [...files.map((f) => f.name), ...lost], links }); }, [repo, h.id, body, files, links, lost]);

  const addLink = () => {
    if (!linkName.trim() || !linkUri.trim()) return;
    setLinks((l) => [...l, { name: linkName.trim(), uri: linkUri.trim() }]);
    setLinkName(''); setLinkUri('');
  };
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!body.trim()) { setError('回答を書いてください。'); return; }
    const bad = checkAttachments(body, files, links);
    if (bad) { setError(bad); return; }
    // 残りが少なければ、書きかけを置いて先に authorize に行く（送っている途中で期限が来ないように）
    if (remainingMs() < MIN_TOKEN_MS) { await reauthorize(); return; }
    setBusy(true); setError(null);
    try {
      const atts: AttachmentInput[] = [];
      for (const f of files) atts.push({ kind: 'file', name: f.name, ...(f.type ? { media_type: f.type } : {}), content_base64: toBase64(await f.arrayBuffer()) });
      for (const l of links) atts.push({ kind: 'link', name: l.name, uri: l.uri });
      const r = await answerHumanRequest(repo, h.id, body, atts);
      clearDraft(repo, h.id);
      onDone(r);
    } catch (err) {
      setBusy(false);
      if (err instanceof Problem && err.status === 401) return;
      // ほかのタブや人が先に回答した。書きかけを消し、読み直して回答を示す
      if (err instanceof Problem && err.code === 'not_open') { clearDraft(repo, h.id); onAlready(); return; }
      setError(err instanceof Problem ? ANSWER_ERRORS[err.code] ?? (err.detail || err.code) : String(err));
    }
  };
  return (
    <form className="answer-form" onSubmit={submit}>
      <h3 className="sec">回答する</h3>
      <p className="dim small">回答は 1 回だけで、あとから変えられません。</p>
      <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={8} aria-label="回答" placeholder="回答を書く" />
      {lost.length > 0 && (
        <p className="note" role="status">サインインし直したため、添付のファイル（{lost.join('、')}）を選び直してください。<button type="button" className="link" onClick={() => setLost([])}>閉じる</button></p>
      )}
      <div className="attach">
        <label className="btn file-pick">ファイルを添付<input type="file" multiple onChange={(e) => { const fs = [...(e.target.files ?? [])]; setFiles((x) => [...x, ...fs]); setLost((l) => l.filter((n) => !fs.some((f) => f.name === n))); e.target.value = ''; }} /></label>
        <span className="dim small">1 つ 10 MiB まで。大きなものはリンクで添える</span>
      </div>
      {files.reduce((n, f) => n + f.size, 0) > HEAVY_ON_MOBILE && files.length > 1 && (
        <p className="note small">ファイルの合計が大きいので、モバイルでは送るのに時間がかかることがあります。</p>
      )}
      {(files.length > 0 || links.length > 0) && (
        <ul className="attached">
          {files.map((f, i) => <li key={'f' + i}><span>{f.name}</span><span className="dim">{fmtSize(f.size)}</span><button type="button" className="link" onClick={() => setFiles((x) => x.filter((_, j) => j !== i))}>外す</button></li>)}
          {links.map((l, i) => <li key={'l' + i}><span>{l.name}</span><span className="dim">{l.uri}</span><button type="button" className="link" onClick={() => setLinks((x) => x.filter((_, j) => j !== i))}>外す</button></li>)}
        </ul>
      )}
      <div className="link-add">
        <input value={linkName} onChange={(e) => setLinkName(e.target.value)} placeholder="リンクの名前" aria-label="リンクの名前" />
        <input value={linkUri} onChange={(e) => setLinkUri(e.target.value)} placeholder="https://…" aria-label="リンクの URL" />
        <button type="button" className="btn" onClick={addLink}>リンクを添える</button>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="form-actions"><button className="btn primary" type="submit" disabled={busy}>{busy ? '送っています…' : '回答する'}</button></div>
    </form>
  );
}

const fmtSize = (n: number) => (n < 1024 ? `${n} B` : n < 1024 * 1024 ? `${(n / 1024).toFixed(0)} KiB` : `${(n / 1024 / 1024).toFixed(1)} MiB`);
