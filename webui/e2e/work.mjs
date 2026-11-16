// ダッシュボードの作業の件数を押して開いた先の数が、件数と同じかを確かめる（internal/e2e/webui_work_test.go から動かす）。
// 環境変数：BASE、LOGIN、PASSWORD、CHROME、OUT、REPO、EXPECT（「種類>=1」「種類=0」をカンマで並べる）、PHASE（スクリーンショットの名前）
import { mkdirSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, REPO, EXPECT, PHASE = '1' } = process.env;
mkdirSync(OUT, { recursive: true });
const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage();
page.on('pageerror', (e) => console.error('pageerror:', e.message));
await page.setViewport({ width: 1280, height: 800 });

// 作業の種類、ダッシュボードの名前、開いた先で数えるもの
const KINDS = [
  ['undecided_leaf', '未決の葉', 'viewer'],
  ['needs_reconsideration', '要再検討', 'viewer'],
  ['review_waiting', 'レビュー待ち', 'list'],
  ['fix_waiting', '修正待ち', 'list'],
  ['human_request_answered', '回答済みの依頼', 'list'],
  ['citation_changed', '引用元が変化した検証結果', 'viewer'],
];
const signedIn = () => page.waitForFunction(() => [...document.querySelectorAll('button.link')].some((b) => b.textContent === 'サインアウト'), { timeout: 20000 });

try {
  await page.goto(BASE + `/r/${REPO}`, { waitUntil: 'networkidle0' });
  if (page.url().includes('/user/login')) {
    await page.type('#user_name', LOGIN);
    await page.type('#password', PASSWORD);
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.keyboard.press('Enter')]);
    if (page.url().includes('/login/oauth/authorize')) {
      await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('button[name="granted"][value="true"]')]);
    }
  }
  await signedIn();
  const got = {};
  for (const [kind, label, where] of KINDS) {
    await page.goto(BASE + `/r/${REPO}`, { waitUntil: 'networkidle0' });
    await signedIn();
    await page.waitForSelector('ul.counts a', { timeout: 30000 });
    if (kind === 'undecided_leaf') await page.screenshot({ path: `${OUT}/work-dashboard-${PHASE}.png` });
    const n = await page.evaluate((l) => parseInt([...document.querySelectorAll('ul.counts a')].find((a) => a.querySelector('span').textContent === l).querySelector('b').textContent, 10), label);
    await page.evaluate((l) => [...document.querySelectorAll('ul.counts a')].find((a) => a.querySelector('span').textContent === l).click(), label);
    let m;
    if (where === 'list') {
      await page.waitForFunction(() => new URL(location.href).searchParams.get('work') && document.querySelector('.list-pane') && !document.querySelector('.list-pane .loading'), { timeout: 30000 });
      m = await page.$$eval('ul.items a', (as) => as.length);
    } else if (kind === 'citation_changed') {
      // 件数は検証結果の数。ビュワーの帯に、検証結果の数と仮説の数を出す。仮説の数は、ビュワーの一致の数と同じ
      await page.waitForFunction(() => new URL(location.href).searchParams.get('f') === 'cited' && document.querySelector('.cited-items') && document.querySelector('.tree-head b'), { timeout: 30000 });
      m = await page.$eval('.cited-items', (b) => Number(b.textContent));
      const [hyps, shown] = await page.evaluate(() => [Number(document.querySelector('.notice').textContent.match(/仮説 (\d+) 個/)[1]), Number(document.querySelector('.tree-head b').textContent)]);
      if (hyps !== shown) throw new Error(`引用元の変化：帯の仮説 ${hyps} 個と、ビュワーの一致 ${shown} が違う`);
    } else {
      await page.waitForFunction(() => new URL(location.href).searchParams.get('f') && document.querySelector('.tree-head b'), { timeout: 30000 });
      m = await page.$eval('.tree-head b', (b) => Number(b.textContent));
    }
    console.log(`… ${label}：ダッシュボード ${n}、開いた先 ${m}`);
    if (n !== m) throw new Error(`${label}：ダッシュボードの件数 ${n} と、開いた先の数 ${m} が違う`);
    if (n > 0) await page.screenshot({ path: `${OUT}/work-${kind}-${PHASE}.png` });
    got[kind] = n;
  }
  // この段で、決まった数になっているか（1 件以上の場面を、ちゃんと作れたか）
  for (const e of (EXPECT ?? '').split(',').filter(Boolean)) {
    const [, kind, op, v] = e.match(/^([a-z_]+)(>=|=)(\d+)$/);
    const ok = op === '>=' ? got[kind] >= Number(v) : got[kind] === Number(v);
    if (!ok) throw new Error(`${kind} が ${got[kind]}（${op}${v} のはず）`);
  }
  console.log('ok');
} catch (e) {
  await page.screenshot({ path: `${OUT}/work-failure-${PHASE}.png` }).catch(() => {});
  console.error(e.message);
  process.exitCode = 1;
} finally {
  await browser.close();
}
