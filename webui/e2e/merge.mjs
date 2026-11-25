// merge したあとに、apply request の一覧が変わるか（internal/e2e/directory_test.go から動かす）。
//   MODE=ui：WebUI の merge ボタンで merge すると、open の一覧から消える。
//   MODE=forge：画面を開いたまま GitForge（Forgejo）で直接 merge し、タブに戻ると、open の一覧から消え、反映済みの一覧に出る。
// 環境変数：BASE、LOGIN、PASSWORD、CHROME、OUT、REPO、AR、MODE、FORGEJO_API（…/api/v1）、FORGEJO_TOKEN、FORGEJO_REPO（owner/name）
import { mkdirSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, REPO, AR, MODE, FORGEJO_API, FORGEJO_TOKEN, FORGEJO_REPO } = process.env;
mkdirSync(OUT, { recursive: true });
const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage();
page.on('pageerror', (e) => console.error('pageerror:', e.message));
await page.setViewport({ width: 1280, height: 800 });
const step = (s) => console.log('…', s);
const listIds = () => page.$$eval('ul.items a', (as) => as.map((a) => new URL(a.href).searchParams.get('id')));
// 一覧を読み終えていて（空なら「ありません」）、AR があるかが want か
const listHas = (want) => page.waitForFunction((id, w) => (!!document.querySelector('.list-pane ul.items') || document.querySelector('.list-pane p.dim')?.textContent === 'apply request はありません。') && [...document.querySelectorAll('ul.items a')].some((a) => new URL(a.href).searchParams.get('id') === id) === w, { timeout: 30000 }, AR, want);

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
  step('open の一覧に ' + AR + ' がある');
  await listHas(true);
  await page.waitForSelector('.ar-head .d-head .s-open', { timeout: 30000 });

  if (MODE === 'ui') {
    step('merge ボタンで merge する');
    await page.waitForFunction(() => [...document.querySelectorAll('.ar-head button')].some((b) => b.textContent === 'merge する'), { timeout: 30000 });
    await page.evaluate(() => [...document.querySelectorAll('.ar-head button')].find((b) => b.textContent === 'merge する').click());
    await page.waitForFunction(() => [...document.querySelectorAll('.ar-head button')].some((b) => b.textContent === 'merge を送る'), { timeout: 10000 });
    await page.evaluate(() => [...document.querySelectorAll('.ar-head button')].find((b) => b.textContent === 'merge を送る').click());
    await page.waitForSelector('.ar-head .d-head .s-merged', { timeout: 60000 });
  } else {
    step('GitForge で直接 merge する');
    let res;
    for (let i = 0; i < 30; i++) {
      res = await fetch(`${FORGEJO_API}/repos/${FORGEJO_REPO}/pulls/${AR}/merge`, { method: 'POST', headers: { Authorization: 'token ' + FORGEJO_TOKEN, 'Content-Type': 'application/json' }, body: JSON.stringify({ Do: 'merge' }) });
      if (res.ok) break;
      await new Promise((r) => setTimeout(r, 1000));
    }
    if (!res.ok) throw new Error('Forgejo で merge できない：' + res.status + ' ' + (await res.text()));
    // 少し待っても、タブに戻るまでは読み直さない（一覧は前のまま）
    if (!(await listIds()).includes(AR)) throw new Error('タブに戻る前に一覧が変わった');
    step('タブに戻る');
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    await page.waitForSelector('.ar-head .d-head .s-merged', { timeout: 30000 });
  }
  step('open の一覧から消える');
  await listHas(false);
  await page.screenshot({ path: `${OUT}/merge-${MODE}.png` });
  step('反映済みの一覧に出る');
  await page.evaluate(() => { const s = document.querySelector('.list-filters select'); s.value = 'merged'; s.dispatchEvent(new Event('change', { bubbles: true })); });
  await listHas(true);
} finally {
  await browser.close();
}
