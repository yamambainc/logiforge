// スマホの幅で apply request の画面を開く（internal/e2e/webui_mobile_test.go から動かす）。
// タイトルが長くても、木の上をなぞると見出しが流れて、反映したあとの木が画面いっぱいに見え、最後の行まで読めるかを確かめる。
// 長いタイトルは 1 行で「…」で省き、▸ とタイトルを押して全文を開く・閉じる。1 行に収まるタイトル（AR_SHORT）には ▸ を出さない（スマホと PC の幅で）。
// 環境変数：BASE、LOGIN、PASSWORD、CHROME、OUT、REPO、AR、AR_SHORT
import { mkdirSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, REPO, AR, AR_SHORT } = process.env;

// 見出しのタイトルの高さ（行数）と、▸ があるか・開いているか
const titleState = (page) => page.evaluate(() => {
  const t = document.querySelector('.ar-head .d-title');
  const b = document.querySelector('.ar-head .title-toggle');
  const lh = parseFloat(getComputedStyle(t).lineHeight);
  return { lines: Math.round(t.getBoundingClientRect().height / lh), ellipsis: t.scrollWidth > t.clientWidth + 1, toggle: !!b, expanded: b?.getAttribute('aria-expanded') ?? null };
});
// 長いタイトル：閉じて 1 行 → ▸ で開いて何行にも → タイトルを押しても閉じない → ▾ で閉じる → タイトルを押して開く（開いたまま終える）。width は画面の名前（スクショ）
async function longTitle(page, width) {
  let t = await titleState(page);
  console.log(`… ${width} 長いタイトル（閉じ）：`, JSON.stringify(t));
  if (t.lines !== 1 || !t.ellipsis || !t.toggle || t.expanded !== 'false') throw new Error(`${width}：長いタイトルが 1 行で省かれていない、または ▸ がない`);
  await page.screenshot({ path: `${OUT}/${width}-title-closed.png` });
  await page.click('.ar-head .title-toggle');
  t = await titleState(page);
  console.log(`… ${width} 長いタイトル（開き）：`, JSON.stringify(t));
  if (t.lines < 2 || t.ellipsis || t.expanded !== 'true') throw new Error(`${width}：▸ を押しても全文が開かない`);
  if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error(`${width}：タイトルを開くと横にはみ出す`);
  await page.screenshot({ path: `${OUT}/${width}-title-open.png` });
  // 開いている間は、タイトルを押しても閉じない（文字を選べるように）
  await page.click('.ar-head .d-title');
  t = await titleState(page);
  if (t.lines < 2 || t.expanded !== 'true') throw new Error(`${width}：開いているタイトルを押すと閉じる`);
  await page.click('.ar-head .title-toggle');
  t = await titleState(page);
  if (t.lines !== 1 || t.expanded !== 'false') throw new Error(`${width}：▾ を押しても閉じない`);
  // タイトルを押しても開く
  await page.click('.ar-head .d-title');
  t = await titleState(page);
  if (t.lines < 2 || t.expanded !== 'true') throw new Error(`${width}：タイトルを押しても開かない`);
}
async function shortTitle(page, width) {
  const t = await titleState(page);
  console.log(`… ${width} 短いタイトル：`, JSON.stringify(t));
  if (t.lines !== 1 || t.ellipsis || t.toggle) throw new Error(`${width}：1 行に収まるタイトルに ▸ が出る`);
}
const openAr = async (page, id) => {
  await page.goto(BASE + `/r/${REPO}/apply-requests?id=${id}`, { waitUntil: 'networkidle0' });
  await page.waitForSelector('.ar-head .d-title', { timeout: 30000 });
};

mkdirSync(OUT, { recursive: true });
const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage();
page.on('pageerror', (e) => console.error('pageerror:', e.message));
await page.setViewport({ width: 1280, height: 800 });

try {
  await page.goto(BASE + `/r/${REPO}/apply-requests?id=${AR}`, { waitUntil: 'networkidle0' });
  if (page.url().includes('/user/login')) {
    await page.type('#user_name', LOGIN);
    await page.type('#password', PASSWORD);
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.keyboard.press('Enter')]);
    if (page.url().includes('/login/oauth/authorize')) {
      await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('button[name="granted"][value="true"]')]);
    }
  }
  await page.setViewport({ width: 390, height: 844, deviceScaleFactor: 2, isMobile: true, hasTouch: true });
  await page.goto(BASE + `/r/${REPO}/apply-requests?id=${AR}`, { waitUntil: 'networkidle0' });
  await page.waitForSelector('.ar-pane .tree-scroll .row', { timeout: 30000 });
  await page.screenshot({ path: `${OUT}/mobile-ar.png` });
  if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('幅 390 px で横にはみ出す');
  // タイトルの省略と開閉（スマホの幅）。1 行に収まるタイトルには ▸ を出さない。そのあと、読み込み直した（閉じた）画面で木を確かめる。
  // 切り替えて閉じに戻るかは、一覧から選べる PC の幅で確かめる
  await longTitle(page, 'mobile');
  await openAr(page, AR_SHORT);
  await shortTitle(page, 'mobile');
  await openAr(page, AR);
  await page.waitForSelector('.ar-pane .tree-scroll .row', { timeout: 30000 });

  // 木の見える高さ（.tree-scroll のうち、画面の中で、下のナビに隠れないところ）と、最後の行まで見えているか
  const look = () => page.evaluate(() => {
    const nav = document.querySelector('.bottom-nav')?.getBoundingClientRect().top ?? innerHeight;
    const el = document.querySelector('.ar-pane .tree-scroll');
    const t = el.getBoundingClientRect();
    // 木を最後まで送り、木の下の端が下のナビより上にある（最後の行が見えている）
    const last = el.scrollTop + el.clientHeight >= el.scrollHeight - 1 && t.bottom <= nav + 0.5;
    return { tree: Math.round(Math.max(0, Math.min(t.bottom, nav) - Math.max(t.top, 0))), last };
  });
  // 木の行の上（開いたときに見えている木のところ）を、指でなぞるように（ホイールで）下へ送る。
  // 内側の木が先に動くと、見出しが残ったまま狭い窓で木だけが動く。2 回で見出しが流れ、木が画面いっぱいになるはず
  let s = await look();
  const y = await page.evaluate(() => {
    const nav = document.querySelector('.bottom-nav')?.getBoundingClientRect().top ?? innerHeight;
    const t = document.querySelector('.ar-pane .tree-scroll').getBoundingClientRect();
    return Math.round((Math.max(t.top, 0) + Math.min(t.bottom, nav)) / 2);
  });
  console.log('… 開いたとき：', JSON.stringify(s), '木をなぞる高さ', y);
  if (s.tree < 20) throw new Error('開いたとき、木が少しも見えない');
  const swipe = async () => { await page.mouse.move(195, y); await page.mouse.wheel({ deltaY: 300 }); await new Promise((r) => setTimeout(r, 80)); s = await look(); };
  for (let i = 0; i < 2; i++) await swipe();
  console.log('… 木の上を 2 回送ったあと：', JSON.stringify(s));
  if (s.tree < 400) throw new Error(`木の上を 2 回送っても、木の見える高さが ${s.tree}px しかない（見出しが流れない）`);
  for (let i = 0; i < 100 && !s.last; i++) await swipe();
  await page.screenshot({ path: `${OUT}/mobile-ar-scrolled.png` });
  console.log('… スクロールしたあと：', JSON.stringify(s));
  if (!s.last) throw new Error('木の最後の行までスクロールできない');
  console.log('… 最後の行まで見える');

  // レビューのタブも、見出しといっしょに 1 枚のページとして流れ、回答の欄の最後まで送れる
  await page.evaluate(() => document.querySelector('.ar-pane').scrollTo(0, 0));
  await page.click('.ar-tabs [role="tab"]:nth-child(2)');
  await page.waitForSelector('.ar-pane .answer-form', { timeout: 30000 });
  const formSeen = () => page.evaluate(() => {
    const nav = document.querySelector('.bottom-nav')?.getBoundingClientRect().top ?? innerHeight;
    const f = document.querySelector('.ar-pane .answer-form').getBoundingClientRect();
    return f.top >= 0 && f.bottom <= nav + 0.5;
  });
  for (let i = 0; i < 60 && !(await formSeen()); i++) {
    await page.mouse.move(195, 420);
    await page.mouse.wheel({ deltaY: 200 });
    await new Promise((r) => setTimeout(r, 50));
  }
  await page.screenshot({ path: `${OUT}/mobile-ar-review.png` });
  if (!(await formSeen())) throw new Error('レビューのタブで、回答の欄までスクロールできない');
  console.log('… レビューのタブも、回答の欄まで見える');

  // PC の幅は、これまでどおり（右側は高さ固定で、木だけがスクロールする）
  await page.setViewport({ width: 1280, height: 800 });
  await page.goto(BASE + `/r/${REPO}/apply-requests?id=${AR}`, { waitUntil: 'networkidle0' });
  await page.waitForSelector('.ar-pane .tree-scroll .row', { timeout: 30000 });
  await page.screenshot({ path: `${OUT}/desktop-ar.png` });

  // タイトルの省略と開閉（PC の幅）。開いたまま、ほかの apply request（短いタイトル）を一覧から選んで戻ると、閉じに戻る
  await longTitle(page, 'desktop');
  await page.click(`.list-pane a[href="?id=${AR_SHORT}"]`);
  await page.waitForFunction((id) => document.querySelector('.ar-head .mono')?.textContent?.includes(`#${id}`), { timeout: 30000 }, AR_SHORT);
  await shortTitle(page, 'desktop');
  await page.click(`.list-pane a[href="?id=${AR}"]`);
  await page.waitForFunction((id) => document.querySelector('.ar-head .mono')?.textContent?.includes(`#${id}`), { timeout: 30000 }, AR);
  await page.waitForSelector('.ar-head .title-toggle', { timeout: 30000 });
  const back = await titleState(page);
  if (back.expanded !== 'false' || back.lines !== 1) throw new Error('apply request を切り替えて戻っても、タイトルが閉じに戻らない');
  // 一覧の長いタイトルも 1 行で省く
  const row = await page.evaluate((id) => {
    const a = document.querySelector(`.list-pane a[href="?id=${id}"]`);
    const t = a.querySelector('.item-text');
    return { h: Math.round(a.getBoundingClientRect().height), ellipsis: t.scrollWidth > t.clientWidth + 1 };
  }, AR);
  console.log('… 一覧の長いタイトル：', JSON.stringify(row));
  if (!row.ellipsis || row.h > 90) throw new Error('一覧の長いタイトルが 1 行で省かれていない');
} finally {
  await browser.close();
}
