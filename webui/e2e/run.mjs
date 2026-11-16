// ブラウザの E2E（internal/e2e/webui_test.go から動かす）。
// WebUI を開く → Forgejo にログイン → 許可 → repository の一覧 → 選ぶ → リロードしても押す操作なしに戻る → サインアウト。
// 環境変数：BASE（serve の URL）、LOGIN、PASSWORD、CHROME（Chrome のパス）、OUT（スクリーンショットの置き場）、REPO
import { mkdirSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, REPO } = process.env;
mkdirSync(OUT, { recursive: true });
const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage();
await page.setViewport({ width: 1280, height: 800 });
const step = (s) => console.log('…', s);
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

  step('リロードしても、押す操作なしに同じ画面に戻る（トークンはメモリだけ）');
  await page.reload({ waitUntil: 'networkidle0' });
  await page.waitForFunction((r) => location.pathname === `/r/${r}/tree`, { timeout: 20000 }, REPO);
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

  step('サインアウトすると、サインインの画面が出る');
  await mobile.click('button.link');
  await mobile.waitForSelector('.signin-box');
  console.log('ok');
} catch (e) {
  await page.screenshot({ path: OUT + '/failure.png' }).catch(() => {});
  console.error(e.message);
  process.exitCode = 1;
} finally {
  await browser.close();
}
