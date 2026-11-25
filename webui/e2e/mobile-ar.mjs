// スマホの幅で apply request の画面を開く（internal/e2e/webui_mobile_test.go から動かす）。
// タイトルが長くても、木の上をなぞると見出しが流れて、反映したあとの木が画面いっぱいに見え、最後の行まで読めるかを確かめる。
// 環境変数：BASE、LOGIN、PASSWORD、CHROME、OUT、REPO、AR
import { mkdirSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, REPO, AR } = process.env;
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
} finally {
  await browser.close();
}
