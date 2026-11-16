// スクリーンショットを撮る。使い方: node scripts/shots.mjs <puppeteer のパス> <URL の元> <出力先>
const [, , pp, base, out] = process.argv;
const { default: puppeteer } = await import(pp);
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
];
const browser = await puppeteer.launch({ args: ['--no-sandbox'], headless: 'shell', executablePath: process.env.CHROME });
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 2 });
for (const [name, q] of shots) {
  const t0 = Date.now();
  await page.goto(base + q, { waitUntil: 'networkidle0' });
  await page.waitForSelector('.row, .empty-box');
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
await browser.close();
