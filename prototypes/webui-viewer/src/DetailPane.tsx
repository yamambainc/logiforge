import { useEffect, useState } from 'react';
import { getNodeExtra, type NodeDetailExtra } from './api';
import { ancestors, textOf, vLabel, type Change, type Index, type Node, type TruthValue } from './model';

interface Props { ix: Index; id: string | null; change: Change | null; onReveal: (id: string) => void; mobile: boolean; onClose: () => void }

const vc = (v: TruthValue) => (v === true ? 'v-true' : v === false ? 'v-false' : 'v-null');

export function DetailPane({ ix, id, change, onReveal, mobile, onClose }: Props) {
  const n = id ? ix.byId.get(id) : undefined;
  const [extra, setExtra] = useState<NodeDetailExtra | null>(null);
  useEffect(() => {
    setExtra(null);
    if (!n) return;
    let live = true;
    getNodeExtra(ix.nodes, n).then((x) => live && setExtra(x));
    return () => { live = false; };
  }, [n, ix]);

  if (!n) return <aside className="detail empty"><p className="dim">ノードを選ぶと、ここに詳細が出ます。</p></aside>;

  const isH = n.node_type === 'hypothesis';
  const path = ancestors(ix, n.id);
  const kids = (ix.children.get(n.id) ?? []).map((c) => ix.byId.get(c)!);
  const st = ix.stats.get(n.id)!;

  return (
    <aside className={`detail ${mobile ? 'sheet' : ''}`} aria-label="ノードの詳細">
      {mobile && (
        <div className="sheet-bar">
          <span className="grab" />
          <button className="sheet-close" onClick={onClose} aria-label="閉じる">
            <svg viewBox="0 0 16 16" width="16" height="16"><path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" /></svg>
          </button>
        </div>
      )}
      <div className="crumbs">
        {path.map((a) => (
          <button key={a} className="crumb" onClick={() => onReveal(a)} title={textOf(ix.byId.get(a)!)}>
            <i className={`glyph ${ix.byId.get(a)!.node_type === 'hypothesis' ? 'h' : 's'} ${vc(ix.byId.get(a)!.value)}`} />
            {short(textOf(ix.byId.get(a)!))}
          </button>
        ))}
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

      {change && (
        <Section title="この AR での変化" tone="chg">
          {change.kind === 'added' ? (
            <p>この AR で加わるノードです。</p>
          ) : change.kind === 'removed' ? (
            <p>この AR を反映すると消えるノードです。値と詳細は、比べた main のものです。</p>
          ) : (
            <table className="kv">
              <tbody>
                {change.fields.map((f) => (
                  <tr key={f.name}><th>{f.name}</th><td><span className="dim">{f.before}</span> → <b>{f.after}</b></td></tr>
                ))}
              </tbody>
            </table>
          )}
        </Section>
      )}

      {isH ? (
        n.background ? <Section title="背景情報"><p className="prose">{n.background}</p></Section> : null
      ) : (
        <Section title="概要">
          <p className="prose">{n.summary}</p>
          {n.state === 'inactive' && n.reason && <p className="note">inactive にした理由：{n.reason}</p>}
        </Section>
      )}

      {isH && n.leaf && (
        <Section title="検証結果">
          {!extra ? <Skeleton /> : extra.verdict ? (
            <div className="verdict">
              <div className="v-row">
                <span className={`val ${vc(extra.verdict.value)}`}>{vLabel(extra.verdict.value)}</span>
                <span className="mono dim">{extra.verdict.id}</span>
                <span className="dim">· {extra.verdict.apply_request_id} で反映 · {extra.verdict.submitted_commit}</span>
              </div>
              <p className="prose">{extra.verdict.report_body}</p>
              <div className="files">
                {extra.verdict.artifacts.map((a) => (
                  <a key={a.name} className="file" href={a.uri ?? '#'} onClick={(e) => e.preventDefault()}>
                    <FileIcon link={a.kind === 'link'} />
                    <span>{a.name}</span>
                    <span className="dim">{a.size ?? '外部リンク'}</span>
                  </a>
                ))}
              </div>
              {extra.verdict.citations.length > 0 && (
                <div className="cites">
                  <span className="dim">引用</span>
                  {extra.verdict.citations.map((c) => (
                    <NodeLink key={c.node_id} ix={ix} id={c.node_id} onReveal={onReveal} suffix={`の部分木 @ ${c.commit}`} />
                  ))}
                </div>
              )}
            </div>
          ) : (
            <p className="dim">まだ検証結果がありません（未決の葉）。</p>
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

      {extra && extra.cited_by.length > 0 && (
        <Section title="引用されている">
          {extra.cited_by.map((v) => <div key={v} className="mono">{v}</div>)}
        </Section>
      )}

      <Section title="履歴">
        {change?.kind === 'added' ? <p className="dim">まだ main にありません。反映されたコミットが、最初の履歴になります。</p> : !extra ? <Skeleton /> : (
          <ol className="history">
            {extra.history.map((h) => (
              <li key={h.commit}>
                <span className="dot" />
                <span>{h.summary}</span>
                <span className="dim">{h.apply_request_id ?? '最初のコミット'}</span>
                <span className="spacer" />
                <span className="mono dim">{h.commit}</span>
                <span className="dim">{h.committed_at}</span>
              </li>
            ))}
          </ol>
        )}
      </Section>
    </aside>
  );
}

function Section({ title, children, aside, tone }: { title: string; children: React.ReactNode; aside?: React.ReactNode; tone?: string }) {
  return (
    <section className={`d-sec ${tone ?? ''}`}>
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

function Skeleton() {
  return <div className="skel"><span /><span /></div>;
}

function FileIcon({ link }: { link: boolean }) {
  return link ? (
    <svg viewBox="0 0 16 16" width="14" height="14"><path d="M6.5 9.5l3-3M7 4.5l1-1a2.5 2.5 0 013.5 3.5l-1 1M9 11.5l-1 1A2.5 2.5 0 014.5 9l1-1" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" /></svg>
  ) : (
    <svg viewBox="0 0 16 16" width="14" height="14"><path d="M4 1.5h5l3 3v10H4z M9 1.5v3h3" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" /></svg>
  );
}
