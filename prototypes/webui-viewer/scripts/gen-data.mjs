// 試作のサンプルデータを作る。形は design/api/openapi.yaml の listNodes の応答（Node）に合わせる。
// 出力: public/data/{N}/main.json（今の main）、overlay-<apr>.json（apply request を反映したあと）、apply-requests.json
import { mkdirSync, writeFileSync } from 'node:fs';

function rng(seed) {
  return () => {
    seed |= 0; seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const ROOT = '新しいオンボーディングで、30 日後の継続率が 5 ポイント上がる';
const TOP_STORIES = [
  ['早く価値を体験できる', '最初のセッションで成果物ができれば、翌週も戻ってくる。'],
  ['つまずきを取り除く', '登録から最初の操作までの離脱が減れば、継続する母数が増える。'],
  ['呼び戻しが効く', '離れかけた利用者に、適切な時機で通知が届けば戻ってくる。'],
];
const TOP_HYPS = [
  ['最初のセッションで成果物を作れる人が、40% 以上になる', '最初の成果物を作った人は、翌週に戻る', '成果物までの時間が、中央値で 5 分を切る'],
  ['登録フォームの離脱が、半分になる', '初期設定を後回しにしても、使い始められる', '最初の画面で、何をすればよいかが分かる'],
  ['3 日目の通知で、戻る人が増える', '通知を切る人は、増えない', '戻った人は、その週にもう一度使う'],
];
const STORY_TITLES = [
  '比較実験で直接測る', '過去のログを分け直す', 'インタビューで理由を確かめる', '指標を分解する',
  'セグメントごとに見る', '経路を追う', '先行事例と比べる', '代わりの説明を潰す', '時系列で見る',
  '操作の記録から数える', '小さな試行で確かめる', 'アンケートで確かめる',
];
const STORY_SUMMARIES = [
  '子の仮説がすべて真なら、親の主張を直接支持できる。',
  '別の説明（季節、広告の流入）を除けば、差は施策による。',
  '数字の差に、利用者の言葉で裏づけを取る。',
  '指標を部分に分け、どこが動いたかを確かめる。',
];
const SEGMENTS = ['新規ユーザー', 'モバイル', '法人プラン', '無料プラン', '招待経由', '広告経由', '初回 7 日', '週末', 'デスクトップ', '既存ユーザー', '日本語以外', 'チーム利用'];
const CLAIMS = [
  'で、差が 2 ポイント以上ある', 'で、離脱の主な理由が変わる', 'でも、同じ向きの差が出る', 'で、効果が 2 週目も続く',
  'で、操作の回数が増える', 'で、問い合わせが減らない', 'で、最初の成果物までの時間が縮む', 'で、通知の開封率が上がる',
  'で、差は偶然では説明できない（p < 0.05）', 'で、季節の影響を除いても差が残る', 'で、ログの欠けが 1% 未満である',
  'で、比べる群の偏りがない', 'で、指標の定義が施策の前後で同じ', 'で、利用者が理由を自分の言葉で語れる',
];
const BACKGROUNDS = [
  '2026 年 9 月の A/B テスト（各群 2 万人）のログを使う。指標は design/metrics.md の定義に従う。',
  '前回の施策では、7 日目までは差が出たが、30 日目には消えた。今回は効果の持続を分けて確かめる。',
  'インタビューは 12 人。録音と書き起こしは artifact に置く。',
  '',
  '',
];

function id(r, p) {
  const cs = '0123456789abcdefghjkmnpqrstvwxyz';
  let s = p + '-';
  for (let i = 0; i < 10; i++) s += cs[Math.floor(r() * cs.length)];
  return s;
}

function build(N, seed) {
  const r = rng(seed);
  const hyps = new Map(); // id -> {id, parent, statement, background, stories:[], verdict}
  const stories = new Map();
  let count = 0;
  const mkH = (parent, statement, depth) => {
    const h = { id: id(r, 'h'), parent, statement, background: BACKGROUNDS[Math.floor(r() * BACKGROUNDS.length)], stories: [], depth, verdict: null };
    hyps.set(h.id, h); count++; return h;
  };
  const mkS = (parent, title, summary) => {
    const s = { id: id(r, 's'), parent, title, summary, hyps: [], state: 'active', reason: null };
    stories.set(s.id, s); count++; return s;
  };
  const root = mkH(null, ROOT, 0);
  root.background = '2026 年 10 月に入れた新しいオンボーディングの効果を、30 日後の継続率で判断する。比べるのは、9 月の旧オンボーディングの群である。';
  const queue = [];
  TOP_STORIES.forEach(([t, sm], i) => {
    const s = mkS(root.id, t, sm); root.stories.push(s.id);
    for (const st of TOP_HYPS[i]) { const h = mkH(s.id, st, 1); s.hyps.push(h.id); queue.push(h); }
  });
  while (queue.length && count < N) {
    const h = queue.shift();
    if (h.depth >= 2 && r() < 0.28) continue; // 早めの葉
    const ns = r() < 0.75 ? 1 : (r() < 0.8 ? 2 : 3);
    for (let i = 0; i < ns && count < N; i++) {
      const s = mkS(h.id, STORY_TITLES[Math.floor(r() * STORY_TITLES.length)], STORY_SUMMARIES[Math.floor(r() * STORY_SUMMARIES.length)]);
      h.stories.push(s.id);
      const nh = 2 + Math.floor(r() * 3);
      for (let j = 0; j < nh && count < N; j++) {
        const st = SEGMENTS[Math.floor(r() * SEGMENTS.length)] + CLAIMS[Math.floor(r() * CLAIMS.length)];
        const c = mkH(s.id, st, h.depth + 1); s.hyps.push(c.id); queue.push(c);
      }
    }
  }
  // inactive なストーリー（一部）
  for (const s of stories.values()) {
    if (s.parent !== root.id && r() < 0.05) { s.state = 'inactive'; s.reason = '指標の定義が途中で変わったので、このストーリーでは確かめられない'; }
  }
  // 葉の検証結果
  for (const h of hyps.values()) {
    if (h.stories.length === 0) {
      const x = r();
      h.verdict = x < 0.6 ? true : x < 0.625 ? false : null;
    }
  }
  return { root, hyps, stories, r };
}

// 値と状態を計算し、Node の一覧（深さ優先）を返す
function toNodes(t, claims = {}) {
  const { root, hyps, stories } = t;
  const out = [];
  const val = new Map();
  const inactiveSet = new Set();
  function markInactive(sid) {
    const s = stories.get(sid); inactiveSet.add(sid);
    for (const hid of s.hyps) { inactiveSet.add(hid); for (const c of hyps.get(hid).stories) markInactive(c); }
  }
  for (const s of stories.values()) if (s.state === 'inactive') markInactive(s.id);
  function vh(hid) {
    const h = hyps.get(hid);
    let v;
    if (h.stories.length === 0) v = h.verdict;
    else {
      const act = h.stories.filter((s) => stories.get(s).state === 'active').map(vs);
      h.stories.filter((s) => stories.get(s).state !== 'active').forEach(vs);
      v = act.some((x) => x === true) ? true : act.length && act.every((x) => x === false) ? false : null;
    }
    val.set(hid, v); return v;
  }
  function vs(sid) {
    const s = stories.get(sid);
    const cs = s.hyps.map(vh);
    const v = cs.some((x) => x === false) ? false : cs.every((x) => x === true) ? true : null;
    val.set(sid, v); return v;
  }
  vh(root.id);
  const pick = (x) => claims[x] || { ar: [], hr: [], nr: false };
  function walkH(hid) {
    const h = hyps.get(hid); const c = pick(hid);
    out.push({
      node_type: 'hypothesis', id: h.id, parent_story_id: h.parent, statement: h.statement, background: h.background,
      value: val.get(hid), valid_verdict_id: h.stories.length === 0 && h.verdict !== null ? 'v-' + h.id.slice(2) : null,
      leaf: h.stories.length === 0, needs_reconsideration: c.nr, inactive: inactiveSet.has(hid),
      open_apply_request_ids: c.ar, open_human_request_ids: c.hr,
    });
    h.stories.forEach(walkS);
  }
  function walkS(sid) {
    const s = stories.get(sid); const c = pick(sid);
    out.push({
      node_type: 'story', id: s.id, parent_hypothesis_id: s.parent, title: s.title, summary: s.summary, state: s.state, reason: s.reason,
      value: val.get(sid), inactive: inactiveSet.has(sid), open_apply_request_ids: c.ar, open_human_request_ids: c.hr,
    });
    s.hyps.forEach(walkH);
  }
  walkH(root.id);
  return out;
}

function gen(N, seed) {
  const t = build(N, seed);
  const r = t.r;
  const hs = [...t.hyps.values()];
  const leaves = hs.filter((h) => h.stories.length === 0 && h.depth >= 2);
  const nullLeaves = leaves.filter((h) => h.verdict === null);
  const claims = {};
  const claim = (x) => (claims[x] ||= { ar: [], hr: [], nr: false });
  // 要再検討（値が決まっている仮説の一部）
  hs.filter((h) => h.depth >= 1 && r() < 0.03).forEach((h) => (claim(h.id).nr = true));
  // 人への依頼
  leaves.filter(() => r() < 0.04).forEach((h, i) => claim(h.id).hr.push('hrq_' + (1000 + i)));

  // apply request 1: 未決の葉に検証結果を出し、ある仮説に新しいストーリーを足す（main 向き）
  const apr1 = 'apr_7k2m9q4x';
  const target = nullLeaves[Math.floor(nullLeaves.length * 0.2)];
  const parentForStory = hs.find((h) => h.depth === 1 && h.stories.length > 0);
  claim(target.id).ar.push(apr1); claim(parentForStory.id).ar.push(apr1);
  // apply request 2: 向き先が main でない
  const apr2 = 'apr_3t8v1n6c';
  // apply request 3: 衝突する
  const apr3 = 'apr_9d4h2w7e';
  const c3 = leaves[Math.floor(leaves.length * 0.6)]; claim(c3.id).ar.push(apr3);

  const main = toNodes(t, claims);

  // 反映したあと
  target.verdict = true;
  const ns = { id: id(r, 's'), parent: parentForStory.id, title: '招待の導線を足す', summary: '招待で入った人は、最初から相手がいるので、成果物まで早く着く。', hyps: [], state: 'active', reason: null };
  t.stories.set(ns.id, ns); parentForStory.stories.push(ns.id);
  for (const st of ['招待経由で、最初の成果物までの時間が半分になる', '招待した人の継続率も下がらない', '招待の画面を、70% 以上の人が開く']) {
    const h = { id: id(r, 'h'), parent: ns.id, statement: st, background: '', stories: [], depth: parentForStory.depth + 1, verdict: null };
    t.hyps.set(h.id, h); ns.hyps.push(h.id);
  }
  // 重複したストーリーを消す（消えるノードの表示を確かめるため）
  const leafStory = (sid) => t.stories.get(sid).hyps.every((x) => t.hyps.get(x).stories.length === 0);
  const dupParent = hs.find((h) => h.depth === 2 && h.stories.length >= 2 && leafStory(h.stories[h.stories.length - 1])) ?? hs.find((h) => h.depth === 2 && h.stories.length >= 2 && h.stories.every((sid) => t.stories.get(sid).hyps.every((x) => t.hyps.get(x).stories.length === 0)));
  const dup = dupParent.stories[dupParent.stories.length - 1];
  dupParent.stories = dupParent.stories.filter((x) => x !== dup);
  claim(dup).ar.push(apr1);
  const overlayClaims = structuredClone(claims);
  for (const k of Object.keys(overlayClaims)) overlayClaims[k].ar = overlayClaims[k].ar.filter((a) => a !== apr1);
  const overlay = toNodes(t, overlayClaims);

  const ars = [
    { id: apr1, state: 'open', branch: 'opener/invite-story', target: 'main', head_commit: 'c41e9a07d2', awaiting: 'review', description: '招待の導線のストーリーを足し、重複したストーリーを消す',
      operations: [
        { position: 0, kind: 'story.create', story_id: ns.id, parent_hypothesis_id: parentForStory.id, title: ns.title },
        { position: 1, kind: 'verdict.submit', verdict_id: 'v-' + target.id.slice(2), hypothesis_id: target.id, value: true },
      ] },
    { id: apr2, state: 'open', branch: 'opener/retry-notify', target: 'opener/invite-story', head_commit: '8b02f6e1aa', awaiting: null, description: '通知の再実験（別の apply request のブランチに向けたもの）', operations: [] },
    { id: apr3, state: 'open', branch: 'opener/redo-funnel', target: 'main', head_commit: '5f7c3d90be', awaiting: 'fix', description: 'ファネルの分け方をやり直す',
      operations: [{ position: 0, kind: 'verdict.submit', hypothesis_id: c3.id, value: false }],
      conflict: [`hypotheses/${c3.id}/verdicts/v-${c3.id.slice(2)}.md`] },
  ];
  const dir = `public/data/${N}`;
  mkdirSync(dir, { recursive: true });
  const commit = { 100: '3fa9c1e07b', 4000: 'a1b2c3d4e5' }[N] || 'deadbeef00';
  writeFileSync(`${dir}/main.json`, JSON.stringify({ commit, number: N === 100 ? 48 : 1412, nodes: main }));
  writeFileSync(`${dir}/overlay-${apr1}.json`, JSON.stringify({ commit, nodes: overlay }));
  writeFileSync(`${dir}/apply-requests.json`, JSON.stringify({ items: ars, next_cursor: null }));
  console.log(N, 'main', main.length, 'overlay', overlay.length);
}

gen(100, 7);
gen(4000, 11);
