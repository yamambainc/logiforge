// 詳細のパネル（design/webui/snapshot-tree-viewer.md の「詳細のパネル」、docs/webui/tree-viewer.md の「ノードの詳細」）。
// listNodes で分かる項目は、すぐに示す。getNode と getNodeHistory の応答が来たら、検証結果や履歴を示す。
import { useEffect, useState } from 'react';
import { abortAfter, fetchArtifact, getNode, getNodeHistory, type Artifact, type HistoryEntry, type NodeDetail } from './api';
import { ancestors, textOf, vLabel, type Index, type Node, type TruthValue } from './model';

interface Props { repo: string; ix: Index; id: string | null; onReveal: (id: string) => void; mobile: boolean; onClose: () => void }

const vc = (v: TruthValue) => (v === true ? 'v-true' : v === false ? 'v-false' : 'v-null');
const DETAIL_DELAY_MS = 150;
const DETAIL_TIMEOUT_MS = 20_000;
const IMAGE = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);

export function DetailPane({ repo, ix, id, onReveal, mobile, onClose }: Props) {
  const n = id ? ix.byId.get(id) : undefined;
  const [detail, setDetail] = useState<NodeDetail | null>(null);
  const [history, setHistory] = useState<HistoryEntry[] | null>(null);
  const [failed, setFailed] = useState(false);
  // 選ぶノードが続けて変わる（矢印のキーを押し続ける、検索の語を打つ）ときに、1 つごとに要求を送らない。
  // 150 ms 止まってから送り、別のノードを選んだら前の要求をやめる。20 秒で答えがなければ、読めなかったと示す。
  useEffect(() => {
    setDetail(null); setHistory(null); setFailed(false);
    if (!id) return;
    const ctrl = new AbortController();
    const t = setTimeout(() => {
      const { signal, done } = abortAfter(ctrl.signal, DETAIL_TIMEOUT_MS);
      const live = () => !ctrl.signal.aborted;
      Promise.allSettled([
        getNode(repo, id, signal).then((d) => live() && setDetail(d)).catch(() => live() && setFailed(true)),
        getNodeHistory(repo, id, signal).then((h) => live() && setHistory(h.items)).catch(() => live() && setHistory([])),
      ]).then(done);
    }, DETAIL_DELAY_MS);
    return () => { clearTimeout(t); ctrl.abort(); };
  }, [repo, id]);

  if (!n) return <aside className={`detail empty ${mobile ? 'sheet' : ''}`}><p className="dim">ノードを選ぶと、ここに詳細が出ます。</p></aside>;

  const isH = n.node_type === 'hypothesis';
  const path = ancestors(ix, n.id);
  const kids = (ix.children.get(n.id) ?? []).map((c) => ix.byId.get(c)!);
  const st = ix.stats.get(n.id)!;
  const verdict = detail?.verdict ?? null;

  return (
    <aside className={`detail ${mobile ? 'sheet' : ''}`} aria-label="ノードの詳細">
      {mobile && (
        <div className="sheet-bar">
          <span className="grab" />
          <button className="sheet-close" onClick={onClose} aria-label="閉じる">
            <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden><path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" /></svg>
          </button>
        </div>
      )}
      <div className="crumbs">
        {path.map((a) => {
          const an = ix.byId.get(a)!;
          return (
            <button key={a} className="crumb" onClick={() => onReveal(a)} title={textOf(an)}>
              <i className={`glyph ${an.node_type === 'hypothesis' ? 'h' : 's'} ${vc(an.value)}`} />
              {short(textOf(an))}
            </button>
          );
        })}
      </div>

      <div className="d-head">
        <span className="kind">{isH ? (n.parent_story_id === null ? '根の仮説' : n.leaf ? '仮説（葉）' : '仮説') : 'ストーリー'}</span>
        <span className="mono dim">{n.id}</span>
        <span className="spacer" />
        <span className={`val big ${vc(n.value)}`}>{vLabel(n.value)}</span>
      </div>
      <h2 className="d-title">{textOf(n)}</h2>
      <div className="d-tags">
        {n.inactive && <span className="tag t-inactive">inactive</span>}
        {isH && n.needs_reconsideration && <span className="tag t-nr">要再検討</span>}
        {n.open_apply_request_ids.map((a) => <span key={a} className="tag t-ar">審査中 {a}</span>)}
        {n.open_human_request_ids.map((h) => <span key={h} className="tag t-hr">依頼 {h}</span>)}
      </div>

      {isH ? (
        n.background ? <Section title="背景情報"><p className="prose">{n.background}</p></Section> : null
      ) : (
        <Section title="概要">
          <p className="prose">{n.summary}</p>
          {n.state === 'inactive' && n.reason && <p className="note">inactive にした理由：{n.reason}</p>}
        </Section>
      )}

      {isH && (n.leaf || n.valid_verdict_id) && (
        <Section title="検証結果">
          {failed ? <p className="dim">詳細を読めませんでした。</p> : !detail ? <Skeleton /> : verdict ? (
            <div className="verdict">
              <div className="v-row">
                <span className={`val ${vc(verdict.value)}`}>{vLabel(verdict.value)}</span>
                <span className="mono dim">{verdict.id}</span>
                {verdict.apply_request_id && <span className="dim" title={verdict.apply_request_id}>{verdict.apply_request_id.slice(0, 12)}… で反映</span>}
                <span className="mono dim">{verdict.submitted_commit.slice(0, 7)}</span>
              </div>
              {/* レポートは Markdown を整形せず、文字列として示す（design/webui/security.md） */}
              <p className="prose report">{verdict.report_body}</p>
              {verdict.artifacts.length > 0 && (
                <div className="files">{verdict.artifacts.map((a) => <ArtifactItem key={a.name + (a.sha256 ?? a.uri)} repo={repo} a={a} />)}</div>
              )}
              {verdict.citations.length > 0 && (
                <div className="cites">
                  <span className="dim">引用</span>
                  {verdict.citations.map((c, i) => c.target_kind === 'subtree' && c.node_id
                    ? <NodeLink key={i} ix={ix} id={c.node_id} onReveal={onReveal} suffix={`の部分木 @ ${(c.commit ?? '').slice(0, 7)}`} />
                    : <span key={i} className="mono dim">artifact {(c.sha256 ?? '').slice(0, 10)}</span>)}
                </div>
              )}
            </div>
          ) : (
            <p className="dim">まだ検証結果がありません。</p>
          )}
        </Section>
      )}

      {kids.length > 0 && (
        <Section title={isH ? `ストーリー ${kids.length}` : `子の仮説 ${kids.length}`} aside={<span className="dim">部分木 {st.total - 1} ノード</span>}>
          <ul className="kids">
            {kids.map((k) => (
              <li key={k.id}>
                <button onClick={() => onReveal(k.id)} className={k.inactive ? 'inactive' : ''}>
                  <i className={`glyph ${k.node_type === 'hypothesis' ? 'h' : 's'} ${vc(k.value)}`} />
                  <span className="text">{textOf(k)}</span>
                  <span className={`val ${vc(k.value)}`}>{vLabel(k.value)}</span>
                </button>
              </li>
            ))}
          </ul>
        </Section>
      )}

      {detail && detail.cited_by.length > 0 && (
        <Section title="引用されている">
          {detail.cited_by.map((v) => <div key={v} className="mono">{v}</div>)}
        </Section>
      )}

      <Section title="履歴">
        {!history ? <Skeleton /> : history.length === 0 ? <p className="dim">履歴がありません。</p> : (
          <ol className="history">
            {history.map((h) => (
              <li key={h.commit}>
                <span className="dot" />
                <span className="h-ar mono" title={h.apply_request_id ?? undefined}>{h.apply_request_id ?? (h.reflected ? '' : '反映の手順によらない変更')}</span>
                <span className="spacer" />
                <span className="mono dim">{h.commit.slice(0, 7)}</span>
                <span className="dim">{fmtTime(h.committed_at)}</span>
              </li>
            ))}
          </ol>
        )}
      </Section>
    </aside>
  );
}

// ファイルは保存させるだけにする。その場で示すのは、png・jpeg・gif・webp の画像だけ（design/webui/security.md）。
function ArtifactItem({ repo, a }: { repo: string; a: Artifact }) {
  const [img, setImg] = useState<string | null>(null);
  useEffect(() => {
    if (a.kind !== 'file' || !a.sha256 || !a.media_type || !IMAGE.has(a.media_type)) return;
    let url = '';
    fetchArtifact(repo, a.sha256).then((b) => { url = URL.createObjectURL(new Blob([b], { type: a.media_type })); setImg(url); }).catch(() => {});
    return () => { if (url) URL.revokeObjectURL(url); };
  }, [repo, a]);
  if (a.kind === 'link') {
    const ok = !!a.uri && /^https?:\/\//.test(a.uri);
    return (
      <div className="file">
        <FileIcon link />
        {ok ? <a href={a.uri} target="_blank" rel="noreferrer noopener">{a.name}</a> : <span>{a.name}</span>}
        <span className="dim">{ok ? '外部リンク' : a.uri}</span>
      </div>
    );
  }
  const save = async () => {
    if (!a.sha256) return;
    const b = await fetchArtifact(repo, a.sha256);
    const url = URL.createObjectURL(new Blob([b], { type: 'application/octet-stream' }));
    const link = document.createElement('a');
    link.href = url; link.download = a.name;
    document.body.appendChild(link); link.click(); link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return (
    <div className="file-wrap">
      <button className="file" onClick={() => void save()}>
        <FileIcon link={false} />
        <span>{a.name}</span>
        <span className="dim">保存</span>
      </button>
      {img && <img className="artifact-img" src={img} alt={a.name} />}
    </div>
  );
}

function Section({ title, children, aside }: { title: string; children: React.ReactNode; aside?: React.ReactNode }) {
  return (
    <section className="d-sec">
      <h3>{title}{aside && <span className="sec-aside">{aside}</span>}</h3>
      {children}
    </section>
  );
}

function NodeLink({ ix, id, onReveal, suffix }: { ix: Index; id: string; onReveal: (id: string) => void; suffix?: string }) {
  const n: Node | undefined = ix.byId.get(id);
  if (!n) return <span className="mono">{id}</span>;
  return (
    <button className="nodelink" onClick={() => onReveal(id)}>
      <i className={`glyph ${n.node_type === 'hypothesis' ? 'h' : 's'} ${vc(n.value)}`} />
      {short(textOf(n), 28)}
      {suffix && <span className="dim"> {suffix}</span>}
    </button>
  );
}

const short = (s: string, k = 14) => (s.length > k ? s.slice(0, k) + '…' : s);
const fmtTime = (iso: string) => { const d = new Date(iso); return isNaN(+d) ? iso : d.toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' }); };

function Skeleton() {
  return <div className="skel"><span /><span /></div>;
}

function FileIcon({ link }: { link: boolean }) {
  return link ? (
    <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden><path d="M6.5 9.5l3-3M7 4.5l1-1a2.5 2.5 0 013.5 3.5l-1 1M9 11.5l-1 1A2.5 2.5 0 014.5 9l1-1" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" /></svg>
  ) : (
    <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden><path d="M4 1.5h5l3 3v10H4z M9 1.5v3h3" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" /></svg>
  );
}
