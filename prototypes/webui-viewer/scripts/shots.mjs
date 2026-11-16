// スクリーンショットを撮る。使い方: node scripts/shots.mjs <puppeteer のパス> <URL の元> <出力先>
const [, , pp, base, out] = process.argv;
const { default: puppeteer } = await import(pp);
const M = { width: 375, height: 812, deviceScaleFactor: 2, isMobile: true, hasTouch: true };
const shots = [
  ['01-n100-detail', '?n=100&open=3&pick=verdict'],
  ['02-n4000-overview', '?n=4000&open=2'],
  ['02b-n4000-deep', '?n=4000&open=9'],
  ['03-n4000-search', '?n=4000&q=' + encodeURIComponent('招待経由')],
  ['04-n4000-filter-nr', '?n=4000&f=nr'],
  ['05-n4000-overlay', '?n=4000&ar=apr_7k2m9q4x'],
  ['06-n4000-overlay-changed', '?n=4000&ar=apr_7k2m9q4x&f=changed'],
  ['07-not-main', '?n=100&ar=apr_3t8v1n6c'],
  ['08-conflict', '?n=100&ar=apr_9d4h2w7e'],
  ['10-graph-n100', '?n=100&view=graph&open=3&pick=verdict'],
  ['11-graph-n4000', '?n=4000&view=graph&open=3'],
  ['12-graph-n4000-overlay', '?n=4000&view=graph&ar=apr_7k2m9q4x&f=changed'],
  ['13-graph-n100-search', '?n=100&view=graph&q=' + encodeURIComponent('招待')],
  ['20-mobile-outline', '?n=4000&open=3', M],
  ['21-mobile-graph', '?n=4000&view=graph&open=2', M],
  ['22-mobile-detail', '?n=100&open=3&pick=verdict&sheet=1', M],
  ['23-mobile-overlay', '?n=4000&ar=apr_7k2m9q4x&f=changed', M],
  ['24-mobile-filter-sheet', '?n=4000&fsheet=1&f=nr', M],
];
const browser = await puppeteer.launch({ args: ['--no-sandbox'], headless: 'shell', executablePath: process.env.CHROME });
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 2 });
const D = { width: 1440, height: 900, deviceScaleFactor: 2 };
for (const [name, q, vp] of shots) {
  await page.setViewport(vp ?? D);
  const t0 = Date.now();
  await page.goto(base + q, { waitUntil: 'networkidle0' });
  await page.waitForSelector('.row, .empty-box, .gnode');
  await new Promise((r) => setTimeout(r, 400));
  await page.screenshot({ path: `${out}/${name}.png` });
  console.log(name, Date.now() - t0, 'ms');
}
// 計る: 4,000 ノードを読み込んで最初の行が出るまで、すべて開く、検索の 1 文字ごと
await page.goto(base + '?n=4000', { waitUntil: 'networkidle0' });
const perf = await page.evaluate(async () => {
  const t = () => performance.now();
  const frame = () => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
  const out = { firstRow: performance.getEntriesByType('navigation')[0].duration };
  let a = t();
  [...document.querySelectorAll('.link')].find((b) => b.textContent === 'すべて開く').click();
  await frame(); out.expandAll = t() - a;
  out.rowsAfterExpand = document.querySelector('.tree-head .dim').textContent;
  const input = document.querySelector('.search input');
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
  a = t(); setter.call(input, '招'); input.dispatchEvent(new Event('input', { bubbles: true })); await frame(); out.search1 = t() - a;
  a = t(); setter.call(input, '招待経由'); input.dispatchEvent(new Event('input', { bubbles: true })); await frame(); out.search4 = t() - a;
  out.domRows = document.querySelectorAll('.row').length;
  return out;
});
console.log('perf', JSON.stringify(perf));
// 図: 開いた段数ごとに、描く行数と、描き終わるまでの時間
await page.setViewport(D);
for (const open of [3, 5, 99]) {
  await page.goto(base + `?n=4000&view=graph&open=${open}`, { waitUntil: 'networkidle0' });
  const g = await page.evaluate(async () => {
    const frame = () => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
    const t = performance.now();
    // 図とアウトラインを行き来して、図を描き直す時間を測る
    [...document.querySelectorAll('.seg.mode button')][0].click(); await frame();
    const a = performance.now();
    [...document.querySelectorAll('.seg.mode button')][1].click(); await frame();
    const draw = performance.now() - a;
    const drawn = document.querySelectorAll('.gnode').length;
    // パン（100 回）
    const el = document.querySelector('.graph');
    const b = performance.now();
    for (let i = 0; i < 30; i++) { el.dispatchEvent(new WheelEvent('wheel', { deltaX: 40, deltaY: 0, bubbles: true, cancelable: true })); await frame(); }
    const pan = (performance.now() - b) / 30;
    return { draw, panPerFrame: pan, laidOut: document.querySelector('.tree-head .dim').textContent, drawnNodes: drawn };
  });
  console.log('graph', open, JSON.stringify(g));
}
// モバイル相当（CPU を 4 倍遅く、幅 375px）で、読み込み・パース・索引・図の描画を測る
await page.setViewport(M);
await page.emulateCPUThrottling(4);
for (let i = 0; i < 2; i++) {
  await page.goto(base + '?n=4000&view=graph&open=3', { waitUntil: 'networkidle0' });
  await page.waitForSelector('.gnode');
  const m = await page.evaluate(async () => {
    const frame = () => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
    const lf = window.__lf;
    const btn = () => [...document.querySelectorAll('.seg.mode button')];
    btn()[0].click(); await frame();
    let a = performance.now(); btn()[1].click(); await frame(); const graph = performance.now() - a;
    btn()[0].click(); await frame();
    a = performance.now(); [...document.querySelectorAll('.link')].find((b) => b.textContent === 'すべて開く').click(); await frame(); const expandAll = performance.now() - a;
    return { bytes: lf.bytes, loadAndParse: lf.load, parse: lf.parse, index: lf.index, graphDraw: graph, outlineExpandAll: expandAll };
  });
  console.log('mobile-cpu4x', JSON.stringify(m));
}
await page.emulateCPUThrottling(1);
await browser.close();
