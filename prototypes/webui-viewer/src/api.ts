// 試作の API。本物の BE のかわりに、public/data のサンプルを読む。
// 関数は openapi.yaml の operationId に合わせる。本物にするときは、中身を fetch('/v1/...') に替えるだけにする。
import type { Node, Hypothesis } from './model';

export interface ApplyRequestSummary {
  id: string;
  state: 'open' | 'merged' | 'closed';
  branch: string;
  target: string;
  head_commit: string;
  awaiting: 'review' | 'fix' | null;
  description: string;
  operations: { position: number; kind: string; [k: string]: unknown }[];
  conflict?: string[]; // 試作だけ：overlay_conflict の detail のかわり
}

export class Problem extends Error {
  constructor(public code: string, public detail: string) { super(code); }
}

const base = (n: number) => `./data/${n}`;
const delay = (ms: number) => new Promise((r) => setTimeout(r, ms));

export async function listNodes(n: number, applyRequest?: ApplyRequestSummary): Promise<{ commit: string; number?: number; nodes: Node[] }> {
  if (applyRequest) {
    if (applyRequest.target !== 'main') throw new Problem('overlay_unsupported', '');
    if (applyRequest.conflict) throw new Problem('overlay_conflict', applyRequest.conflict.join('\n'));
    const r = await fetch(`${base(n)}/overlay-${applyRequest.id}.json`);
    return r.json();
  }
  const r = await fetch(`${base(n)}/main.json`);
  return r.json();
}

export async function listApplyRequests(n: number): Promise<ApplyRequestSummary[]> {
  const r = await fetch(`${base(n)}/apply-requests.json`);
  return (await r.json()).items;
}

// getNode の応答のうち、一覧にないもの（検証結果、引用、履歴）を、識別子から決まった形で作る。
function hash(s: string) {
  let h = 2166136261;
  for (const c of s) h = Math.imul(h ^ c.charCodeAt(0), 16777619);
  return h >>> 0;
}

export interface NodeDetailExtra {
  verdict: null | {
    id: string;
    value: boolean;
    report_body: string;
    artifacts: { kind: 'file' | 'link'; name: string; size?: string; uri?: string }[];
    citations: { node_id: string; commit: string }[];
    submitted_commit: string;
    apply_request_id: string;
  };
  cited_by: string[];
  history: { commit: string; committed_at: string; apply_request_id: string | null; reflected: boolean; summary: string }[];
}

export async function getNodeExtra(nodes: Node[], node: Node): Promise<NodeDetailExtra> {
  await delay(60);
  const h = hash(node.id);
  const pick = <T,>(xs: T[], k: number) => xs[(h >>> k) % xs.length];
  const hyps = nodes.filter((x): x is Hypothesis => x.node_type === 'hypothesis' && x.id !== node.id);
  const verdict = node.node_type === 'hypothesis' && node.valid_verdict_id && node.value !== null
    ? {
        id: node.valid_verdict_id,
        value: node.value,
        report_body: node.value
          ? '比較実験（各群 2 万人、9/1〜9/30）で、新しい群の差は +2.8 ポイント（95% 信頼区間 +1.9〜+3.7）。季節の影響を除いた回帰でも、差は +2.5 ポイント残った。'
          : '比較実験（各群 2 万人）で、差は +0.3 ポイント（95% 信頼区間 −0.6〜+1.2）。主張した差（2 ポイント以上）は、区間の外にある。',
        artifacts: [
          { kind: 'file' as const, name: 'analysis.py', size: '4.2 KB' },
          { kind: 'file' as const, name: 'result.csv', size: '128 KB' },
          ...(h % 3 === 0 ? [{ kind: 'link' as const, name: 'ダッシュボード', uri: 'https://metrics.example/retention/30d' }] : []),
        ],
        citations: h % 2 === 0 ? [{ node_id: pick(hyps, 3).id, commit: '9e1d2c4' }] : [],
        submitted_commit: (h.toString(16) + '0000000').slice(0, 7),
        apply_request_id: 'apr_' + (h.toString(36) + 'xxxxxxxx').slice(0, 8),
      }
    : null;
  const cited_by = h % 5 === 0 ? ['v-' + pick(hyps, 7).id.slice(2)] : [];
  const n = 2 + (h % 3);
  const history = Array.from({ length: n }, (_, i) => {
    const day = 14 - i * 2 - (h % 2);
    return {
      commit: ((h >>> i) .toString(16) + 'abcdef0').slice(0, 7),
      committed_at: `2026-11-${String(Math.max(day, 1)).padStart(2, '0')} ${String(9 + ((h >>> i) % 9)).padStart(2, '0')}:${String((h >>> (i + 3)) % 60).padStart(2, '0')}`,
      apply_request_id: i === n - 1 ? null : 'apr_' + ((h >>> i).toString(36) + 'yyyyyyyy').slice(0, 8),
      reflected: i !== n - 1,
      summary: i === n - 1 ? 'ノードを作った' : i === 0 && verdict ? '検証結果を出した' : '背景情報を書き足した',
    };
  });
  return { verdict, cited_by, history };
}
