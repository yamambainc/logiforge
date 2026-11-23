// ディレクトリの repository の apply request の画面（internal/e2e/directory_test.go から動かす）。
// GitForge の pull request の画面へのリンクと、ディレクトリの外の変更を確かめる。
// 環境変数：BASE、LOGIN、PASSWORD、CHROME、OUT、REPO、AR（open の apply request）、EXPECT（外の変更の行の文）、PATHS（外のパス。カンマで並べる。なければ空）
import { mkdirSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, REPO, AR, EXPECT, PATHS = '' } = process.env;
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
  await page.waitForSelector('.ar-head .outside', { timeout: 30000 });
  const link = await page.$eval('.ar-head .d-head a', (a) => [a.textContent, a.getAttribute('href'), a.target]);
  if (link[0] !== `pull request #${AR}` || !link[1].endsWith(`/pulls/${AR}`) || link[2] !== '_blank') throw new Error('pull request へのリンクが違う：' + link);
  const line = await page.$eval('.ar-head .outside', (e) => (e.querySelector('summary') ?? e).textContent);
  if (line !== EXPECT) throw new Error(`外の変更の行が違う：${line}（${EXPECT} のはず）`);
  const paths = await page.$$eval('.ar-head .outside li', (ls) => ls.map((l) => l.textContent).join(','));
  if (paths !== PATHS) throw new Error(`外のパスが違う：${paths}（${PATHS} のはず）`);
  await page.waitForSelector('.ar-pane .banner.overlay', { timeout: 30000 });
  await page.screenshot({ path: `${OUT}/outside-${AR}.png` });
  console.log('… 外の変更：', line, paths);
} finally {
  await browser.close();
}
