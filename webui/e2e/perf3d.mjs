// 3D のフレームレートを測る（design/webui/viewer-3d-limits.md の「性能」）。
// dist を配り、BE の API とサインインは、ブラウザの要求を横取りして、作ったノード 4,000 個の木で答える。
// 使い方：npm run build のあと、CHROME=<Chrome のパス> node e2e/perf3d.mjs
import { readFileSync, existsSync } from 'node:fs';
import { createServer } from 'node:http';
import { extname, join } from 'node:path';
import puppeteer from 'puppeteer-core';

const N = Number(process.env.NODES ?? 4000);
const DIST = new URL('../dist/', import.meta.url).pathname;
const TYPES = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' };

// ノード：根の仮説の下に、ストーリー 1 つと仮説 3 つずつを、幅優先で足す
function nodes() {
  const base = { needs_reconsideration: false, inactive: false, open_apply_request_ids: [], open_human_request_ids: [] };
  const out = [{ ...base, node_type: 'hypothesis', id: 'h-0000000000', parent_story_id: null, statement: '根の仮説', value: null, leaf: false }];
  const leaves = ['h-0000000000'];
  for (let i = 1; out.length < N; i++) {
    const p = leaves.shift();
    const sid = `s-${String(i).padStart(10, '0')}`;
    out.push({ ...base, node_type: 'story', id: sid, parent_hypothesis_id: p, title: `ストーリー ${i}`, summary: '', state: 'active', value: null });
    for (let j = 0; j < 3; j++) {
      const hid = `h-${String(i).padStart(8, '0')}${j}0`;
      out.push({ ...base, node_type: 'hypothesis', id: hid, parent_story_id: sid, statement: `仮説 ${i}-${j}`, value: [true, false, null][(i + j) % 3], leaf: true });
      leaves.push(hid);
    }
  }
  // 子を持つ仮説は葉でない。深さ優先の並びにする（listNodes の応答の順）
  const kids = new Map();
  for (const n of out) { const p = n.parent_story_id ?? n.parent_hypothesis_id; if (p) { if (!kids.has(p)) kids.set(p, []); kids.get(p).push(n); } }
  for (const n of out) if (n.node_type === 'hypothesis' && kids.has(n.id)) n.leaf = false;
  const dfs = [];
  const walk = (n) => { dfs.push(n); for (const c of kids.get(n.id) ?? []) walk(c); };
  walk(out[0]);
  return dfs;
}
const all = nodes();
const tree = JSON.stringify({ commit: 'c'.repeat(40), nodes: all });

const server = createServer((req, res) => {
  const path = new URL(req.url, 'http://x').pathname;
  const file = join(DIST, path);
  const ok = path !== '/' && existsSync(file) && !path.endsWith('/');
  res.setHeader('Content-Type', TYPES[extname(ok ? file : 'index.html')] ?? 'application/octet-stream');
  res.end(readFileSync(ok ? file : join(DIST, 'index.html')));
}).listen(0);
const BASE = `http://127.0.0.1:${server.address().port}`;

function answer(r) {
  const u = new URL(r.url());
  const json = (body, status = 200) => r.respond({ status, contentType: 'application/json', body: typeof body === 'string' ? body : JSON.stringify(body) });
  if (u.pathname === '/fake-authorize') return r.respond({ status: 302, headers: { Location: `${BASE}/oauth/callback?code=c&state=${u.searchParams.get('state')}` } });
  if (!u.pathname.startsWith('/v1/')) return r.continue();
  if (u.pathname === '/v1/oauth/config') return json({ authorize_url: `${BASE}/fake-authorize`, client_id: 'x', redirect_uri: `${BASE}/oauth/callback`, scope: '' });
  if (u.pathname === '/v1/oauth/token') return json({ access_token: 't', expires_in: 3600 });
  if (u.pathname === '/v1/repositories/big/tree/nodes') return json(tree);
  if (u.pathname.endsWith('/history')) return json({ items: [], next_cursor: null });
  if (u.pathname.includes('/nodes/')) return json({ code: 'not_found', detail: '' }, 404);
  return json({ items: [], next_cursor: null });
}

const browser = await puppeteer.launch({ executablePath: process.env.CHROME, headless: true, args: ['--no-sandbox'] });
const cases = [
  ['デスクトップ（1440 × 900）', { width: 1440, height: 900, deviceScaleFactor: 1 }, 1],
  ['モバイル（375 × 812、CPU を 4 倍遅く）', { width: 375, height: 812, deviceScaleFactor: 2, isMobile: true, hasTouch: true }, 4],
];
try {
  for (const [name, vp, cpu] of cases) {
    for (const openAll of [false, true]) {
      const page = await browser.newPage();
      await page.setRequestInterception(true);
      page.on('request', answer);
      await page.setViewport(vp);
      await page.goto(`${BASE}/r/big/tree?view=3d`, { waitUntil: 'networkidle0' });
      await page.waitForFunction(() => window.__3d && window.__3d.frames() > 0, { timeout: 60000 });
      if (openAll) {
        await page.evaluate(() => [...document.querySelectorAll('button.link')].find((b) => b.textContent === 'すべて開く').click());
        await page.waitForFunction((n) => document.querySelector('.tree-head .dim')?.textContent.includes(`${n.toLocaleString()} 個を表示`), { timeout: 60000 }, all.length);
      }
      await page.emulateCPUThrottling(cpu);
      const r = await page.evaluate(async () => {
        window.__3d.autoRotate(true, 6);
        await new Promise((res) => setTimeout(res, 500));
        const f0 = window.__3d.frames(), m0 = window.__3d.ms(), t0 = performance.now();
        await new Promise((res) => setTimeout(res, 4000));
        const frames = window.__3d.frames() - f0;
        window.__3d.autoRotate(false);
        return { fps: Math.round(((frames * 1000) / (performance.now() - t0)) * 10) / 10, msPerFrame: Math.round(((window.__3d.ms() - m0) / Math.max(frames, 1)) * 10) / 10, shown: document.querySelector('.tree-head .dim')?.textContent };
      });
      console.log(`| ${name} | ${openAll ? 'すべて' : '最初の表示'}（${r.shown}） | 約 ${r.fps} fps | 約 ${r.msPerFrame} ms |`);
      await page.close();
    }
  }
} finally {
  await browser.close();
  server.close();
}
