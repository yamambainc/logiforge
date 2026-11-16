// ダッシュボード（docs/webui/dashboard.md、design/webui/screens.md）。根の値、作業の終わりの判定、着手できる作業の件数を示す。
import { useEffect, useState } from 'react';
import { getCompletion, getRepository, Problem, withPendingRetry, workCounts, type Completion, type Repository } from '../api';
import { vLabel } from '../model';
import { navigate } from '../router';

// 作業の種類（docs/work/README.md）。並びは、作業の一覧の種類の順
const KINDS: { key: string; label: string }[] = [
  { key: 'undecided_leaf', label: '未決の葉' },
  { key: 'needs_reconsideration', label: '要再検討' },
  { key: 'review_waiting', label: 'レビュー待ち' },
  { key: 'fix_waiting', label: '修正待ち' },
  { key: 'human_request_answered', label: '回答済みの依頼' },
  { key: 'citation_changed', label: '引用元が変化した検証結果' },
];
const vc = (v: boolean | null) => (v === true ? 'v-true' : v === false ? 'v-false' : 'v-null');

export function Dashboard({ repo }: { repo: string }) {
  const [data, setData] = useState<{ r: Repository; c: Completion; counts: Record<string, number> } | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    withPendingRetry(() => Promise.all([getRepository(repo), getCompletion(repo), workCounts(repo)]), (w) => live && setPending(w))
      .then(([r, c, counts]) => live && setData({ r, c, counts }))
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
            <tr><th>根拠部分木で着手できる作業</th><td className={c.decided ? (c.remaining_work === 0 ? 'ok' : 'ng') : 'dim'}>{c.decided ? `${c.remaining_work} 件` : '-'}</td></tr>
            <tr><th>作業の終わり</th><td className={c.done ? 'ok' : 'ng'}>{c.done ? '終わった' : 'まだ'}</td></tr>
          </tbody>
        </table>
      </section>
      <section className="card">
        <h2>着手できる作業</h2>
        <ul className="counts">
          {KINDS.map((k) => (
            <li key={k.key} className={counts[k.key] ? '' : 'zero'}><span>{k.label}</span><b>{counts[k.key] ?? 0}</b></li>
          ))}
        </ul>
        <p className="dim mono small">main {c.commit.slice(0, 7)}</p>
      </section>
    </main>
  );
}
