// ダッシュボード（docs/webui/dashboard.md、design/webui/screens.md）。根の値、作業の終わりの判定、着手できる作業の件数を示す。
import { useEffect, useState } from 'react';
import { getCompletion, getRepository, Problem, withPendingRetry, workCounts, type Completion, type Repository } from '../api';
import { vLabel } from '../model';
import { navigate } from '../router';

// 作業の種類（docs/work/README.md）。並びは、作業の一覧の種類の順。
// to は、件数を押したときに行く、その作業の一覧（docs/webui/dashboard.md）。ほかの画面ができたら、つなぐ
const KINDS: { key: string; label: string; to?: (repo: string) => string }[] = [
  { key: 'undecided_leaf', label: '未決の葉', to: (r) => `/r/${r}/tree?f=leaf` },
  { key: 'needs_reconsideration', label: '要再検討', to: (r) => `/r/${r}/tree?f=nr-work` },
  { key: 'review_waiting', label: 'レビュー待ち', to: (r) => `/r/${r}/apply-requests?work=review_waiting` },
  { key: 'fix_waiting', label: '修正待ち', to: (r) => `/r/${r}/apply-requests?work=fix_waiting` },
  { key: 'human_request_answered', label: '回答済みの依頼', to: (r) => `/r/${r}/human-requests?state=answered&work=1` },
  { key: 'citation_changed', label: '引用元が変化した検証結果', to: (r) => `/r/${r}/tree?f=cited` },
];
const vc = (v: boolean | null) => (v === true ? 'v-true' : v === false ? 'v-false' : 'v-null');

export function Dashboard({ repo }: { repo: string }) {
  const [data, setData] = useState<{ r: Repository; c: Completion; counts: Record<string, number> } | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    // 取り込み中のあいだは、作業の終わりの判定だけを待つ。出たら、ほかを読む
    withPendingRetry(() => getCompletion(repo), (w) => live && setPending(w))
      .then((c) => Promise.all([getRepository(repo), workCounts(repo)]).then(([r, counts]) => live && setData({ r, c, counts })))
      .catch((e) => { if (live && !(e instanceof Problem && e.status === 401)) setError(e instanceof Problem && e.code === 'not_found' ? 'この repository はありません' : String(e)); });
    return () => { live = false; };
  }, [repo]);
  if (error) return <div className="empty-box"><div className="empty-title">{error}</div></div>;
  if (!data) return <div className="loading">{pending ? 'スナップショットツリーを取り込み中です。出しだい表示します…' : '読み込み中…'}</div>;
  const { r, c, counts } = data;
  const tree = `/r/${repo}/tree`;
  return (
    <main className="page dashboard">
      <section className="card root-card">
        <span className="dim">根</span>
        <a className="root-text" href={tree} onClick={(e) => { e.preventDefault(); navigate(tree); }}>{r.root_statement ?? r.root_hypothesis_id}</a>
        <span className={`val big ${vc(c.root_value)}`}>{vLabel(c.root_value)}</span>
      </section>
      <section className="card">
        <h2>作業の終わりの判定</h2>
        <table className="judge">
          <tbody>
            <tr><th>根の値が決まっている</th><td className={c.decided ? 'ok' : 'ng'}>{c.decided ? 'はい' : 'いいえ'}</td></tr>
            <tr><th>根拠部分木で着手できる作業</th>{c.decided ? <td className={c.remaining_work === 0 ? 'ok' : 'ng'}>{c.remaining_work} 件</td> : <td className="dim note-cell">根の値が決まってから数えます</td>}</tr>
            <tr><th>作業の終わり</th><td className={c.done ? 'ok' : 'ng'}>{c.done ? '終わった' : 'まだ'}</td></tr>
          </tbody>
        </table>
      </section>
      <section className="card">
        <h2>着手できる作業</h2>
        <ul className="counts">
          {KINDS.map((k) => (
            <li key={k.key} className={counts[k.key] ? '' : 'zero'}>
              {k.to ? (
                <a href={k.to(repo)} onClick={(e) => { e.preventDefault(); navigate(k.to!(repo)); }}><span>{k.label}</span><b>{counts[k.key] ?? 0}<small> 件</small></b></a>
              ) : <><span>{k.label}</span><b>{counts[k.key] ?? 0}<small> 件</small></b></>}
            </li>
          ))}
        </ul>
        <p className="dim mono small">main {c.commit.slice(0, 7)}</p>
      </section>
    </main>
  );
}
