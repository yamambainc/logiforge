// ブラウザの E2E（internal/e2e/webui_test.go から動かす）。
// WebUI を開く → Forgejo にログイン → 許可 → repository の一覧 → 選ぶ → リロードしても押す操作なしに戻る → サインアウト。
// organization にいない人は「organization に入っていません」で止まる（authorize への行き来が止まらなくならない）ことも確かめる。
// 環境変数：BASE（serve の URL）、LOGIN、PASSWORD、CHROME（Chrome のパス）、OUT（スクリーンショットの置き場）、REPO、
// OUTSIDER_LOGIN、OUTSIDER_PASSWORD（organization にいない Forgejo のユーザー）、NODE_A（検証結果のある仮説）
import { mkdirSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, REPO, OUTSIDER_LOGIN, OUTSIDER_PASSWORD, NODE_A } = process.env;
mkdirSync(OUT, { recursive: true });
const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage();
await page.setViewport({ width: 1280, height: 800 });
const step = (s) => console.log('…', s);

// authorize への行き来を数える。WebUI が authorize に行くたびに、新しい state を作る。同意の画面（200）が出た回数も数える
function watch(p) {
  const w = { states: new Set(), consent: 0 };
  p.on('request', (r) => {
    if (!r.isNavigationRequest() || !r.url().includes('/login/oauth/authorize')) return;
    const st = new URL(r.url()).searchParams.get('state');
    if (st) w.states.add(st);
  });
  p.on('response', (r) => { if (r.url().includes('/login/oauth/authorize') && r.status() === 200) w.consent++; });
  w.reset = () => { w.states.clear(); w.consent = 0; };
  return w;
}
const signedIn = (p) => p.waitForFunction(() => [...document.querySelectorAll('button.link')].some((b) => b.textContent === 'サインアウト'), { timeout: 20000 });
const watched = watch(page);
try {
  step('WebUI を開くと、Forgejo のログインへ行く');
  await page.goto(BASE + '/', { waitUntil: 'networkidle0' });
  if (!page.url().includes('/user/login')) throw new Error('Forgejo のログインに行かない：' + page.url());
  await page.type('#user_name', LOGIN);
  await page.type('#password', PASSWORD);
  await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.keyboard.press('Enter')]);

  step('初めてなので、同意の画面で許可する');
  if (page.url().includes('/login/oauth/authorize')) {
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('button[name="granted"][value="true"]')]);
  }

  step('repository の一覧が出る');
  await page.waitForSelector('.repo-list', { timeout: 20000 });
  const names = await page.$$eval('.repo-name', (els) => els.map((e) => e.textContent));
  if (!names.includes(REPO)) throw new Error('一覧に ' + REPO + ' がない：' + names.join(','));
  const root = await page.$eval('.repo-root', (e) => e.textContent);
  if (!root.includes('継続率')) throw new Error('根の主文が出ない：' + root);
  if (new URL(page.url()).search.includes('code=')) throw new Error('URL に code が残っている');
  await page.screenshot({ path: OUT + '/signin-desktop.png' });

  step('repository を選ぶ');
  await page.click('.repo-list a');
  await page.waitForFunction((r) => location.pathname === `/r/${r}/tree`, {}, REPO);

  step('ビュワー：アウトラインにストーリーと仮説が出る');
  await page.waitForFunction(() => [...document.querySelectorAll('.row .text')].some((e) => e.textContent.includes('招待の導線')), { timeout: 30000 });
  const texts = await page.$$eval('.row .text', (els) => els.map((e) => e.textContent));
  if (!texts.some((t) => t.includes('成果物までの時間が半分'))) throw new Error('仮説が出ない：' + texts.join(' / '));

  step('仮説を選ぶと、詳細のパネルに検証結果と artifact が出る。URL に node が入る');
  await page.evaluate(() => [...document.querySelectorAll('.row')].find((r) => r.textContent.includes('成果物までの時間が半分')).click());
  await page.waitForFunction(() => document.querySelector('.verdict .val')?.textContent === '真', { timeout: 20000 });
  const report = await page.$eval('.verdict .report', (e) => e.textContent);
  if (!report.includes('半分になった')) throw new Error('レポートが出ない：' + report);
  if (!(await page.$$eval('.file', (els) => els.map((e) => e.textContent))).some((t) => t.includes('data.csv'))) throw new Error('artifact が出ない');
  if (!new URL(page.url()).searchParams.get('node')) throw new Error('URL に node が入らない');
  await page.screenshot({ path: OUT + '/viewer-outline.png' });

  step('検索すると、一致したノードと祖先だけが出る');
  await page.type('.search input', '継続率も');
  await page.waitForFunction(() => document.querySelector('.search .count')?.textContent === '1 / 1', { timeout: 5000 });
  await page.click('.search input', { clickCount: 3 });
  await page.keyboard.press('Escape');

  step('図に切り替える（選んだノードは同じ）。縦にもできる');
  await page.evaluate(() => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent.includes('図')).click());
  await page.waitForSelector('.gnode.sel', { timeout: 10000 });
  await page.screenshot({ path: OUT + '/viewer-graph.png' });
  await page.evaluate(() => [...document.querySelectorAll('.seg.mini button')].find((b) => b.textContent === '縦').click());
  await page.waitForFunction(() => new URL(location.href).searchParams.get('orient') === 'v');
  await page.screenshot({ path: OUT + '/viewer-graph-v.png' });

  step('リロードしても、押す操作なしに同じ画面に戻る（トークンはメモリだけ）');
  watched.reset();
  await page.reload({ waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForFunction((r) => location.pathname === `/r/${r}/tree`, { timeout: 20000 }, REPO);
  // 表示（図、縦）と選んだノードも、URL から戻る
  await page.waitForSelector('.gnode.sel', { timeout: 20000 });
  if (watched.states.size !== 1) throw new Error('リロードで、authorize に ' + watched.states.size + ' 回行った（1 回のはず）');
  if (watched.consent !== 0) throw new Error('リロードで、同意の画面が出た');
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }));
  if (/eyJ|access_token|gta_/.test(stored)) throw new Error('トークンをストレージに置いている：' + stored);

  step('新しいタブ（モバイルの幅）で開いても、押す操作なしにサインインする');
  const mobile = await browser.newPage();
  await mobile.setViewport({ width: 375, height: 812, deviceScaleFactor: 2, isMobile: true, hasTouch: true });
  await mobile.goto(BASE + '/', { waitUntil: 'networkidle0' });
  await mobile.waitForSelector('.repo-list', { timeout: 20000 });
  const overflow = await mobile.evaluate(() => document.documentElement.scrollWidth > innerWidth);
  if (overflow) throw new Error('幅 375 px で横にはみ出す');
  await mobile.screenshot({ path: OUT + '/signin-mobile.png' });

  step('モバイルのビュワー：アウトラインで仮説を押すと、詳細がボトムシートで出る');
  await mobile.goto(BASE + `/r/${REPO}/tree?node=${NODE_A}`, { waitUntil: 'networkidle0' });
  await mobile.waitForSelector('.row.sel', { timeout: 20000 });
  await mobile.screenshot({ path: OUT + '/viewer-mobile-outline.png' });
  await mobile.tap('.row.sel');
  await mobile.waitForSelector('.detail.sheet .verdict', { timeout: 20000 });
  if (await mobile.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('ビュワーが幅 375 px で横にはみ出す');
  await mobile.screenshot({ path: OUT + '/viewer-mobile-detail.png' });
  await mobile.click('.sheet-close');
  await mobile.evaluate(() => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent.includes('図')).click());
  await mobile.waitForSelector('.gnode', { timeout: 10000 });
  await mobile.screenshot({ path: OUT + '/viewer-mobile-graph.png' });
  await mobile.goto(BASE + '/', { waitUntil: 'networkidle0' });
  await mobile.waitForSelector('.repo-list', { timeout: 20000 });

  step('サインアウトすると、サインインの画面が出る');
  await mobile.click('button.link');
  await mobile.waitForSelector('.signin-box');

  step('organization にいない人は、「organization に入っていません」で止まる（authorize へは 1 回だけ）');
  const ctx = await browser.createBrowserContext();
  const out = await ctx.newPage();
  await out.setViewport({ width: 1280, height: 800 });
  const ow = watch(out);
  await out.goto(BASE + '/', { waitUntil: 'networkidle0' });
  await out.type('#user_name', OUTSIDER_LOGIN);
  await out.type('#password', OUTSIDER_PASSWORD);
  await Promise.all([out.waitForNavigation({ waitUntil: 'networkidle0' }), out.keyboard.press('Enter')]);
  if (out.url().includes('/login/oauth/authorize')) {
    await Promise.all([out.waitForNavigation({ waitUntil: 'networkidle0' }), out.click('button[name="granted"][value="true"]')]);
  }
  await out.waitForFunction(() => document.querySelector('.signin-msg')?.textContent?.includes('organization に入っていません'), { timeout: 20000 });
  await new Promise((r) => setTimeout(r, 3000)); // 行き直していないことを確かめるため、少し待つ
  if (ow.states.size !== 1) throw new Error('organization にいない人で、authorize に ' + ow.states.size + ' 回行った（1 回のはず）');
  await out.screenshot({ path: OUT + '/not-member.png' });
  await ctx.close();
  console.log('ok');
} catch (e) {
  await page.screenshot({ path: OUT + '/failure.png' }).catch(() => {});
  console.error(e.message);
  process.exitCode = 1;
} finally {
  await browser.close();
}
