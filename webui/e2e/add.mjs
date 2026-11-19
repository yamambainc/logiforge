// ブラウザの E2E（internal/e2e/webui_add_test.go から動かす）。設定の画面で、ディレクトリの repository を足す流れ（docs/webui/settings.md）。
//   - スナップショットツリーがもうあるディレクトリ（OLD）を登録すると、最初の問いを飛ばし「もうある」と示す。
//   - 新しいディレクトリ（FRESH）は、2 段目を「あとで入れる」で閉じても、一覧の「最初の問いを入れる」から続けられる。
// 環境変数：BASE、LOGIN、PASSWORD、CHROME、OUT、PROJECT（owner/name）、OLD、FRESH
import { mkdirSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, PROJECT, OLD, FRESH } = process.env;
mkdirSync(OUT, { recursive: true });
const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage();
await page.setViewport({ width: 1280, height: 800 });
page.on('pageerror', (e) => console.error('pageerror:', e.message));
const step = (s) => console.log('…', s);
const click = (sel, text) => page.evaluate((sel, text) => [...document.querySelectorAll(sel)].find((b) => b.textContent === text).click(), sel, text);
const waitText = (sel, text, timeout = 20000) => page.waitForFunction((sel, text) => [...document.querySelectorAll(sel)].some((e) => e.textContent.includes(text)), { timeout }, sel, text);
const hasStart = (name) => page.evaluate((n) => [...document.querySelectorAll('.settings-list li')].some((li) => li.querySelector('.strong').textContent === n && li.textContent.includes('最初の問いを入れる')), name);
// 足す流れを開き、ディレクトリの repository を登録する
async function addDirectory(name, dir) {
  await click('button', '足す');
  await page.waitForSelector('.add-flow input[placeholder="question-a"]', { timeout: 5000 });
  await page.evaluate(() => [...document.querySelectorAll('.add-flow label')].find((l) => l.textContent.includes('ディレクトリを使う')).querySelector('input').click());
  await page.type('.add-flow input[placeholder="question-a"]', name);
  await page.type('.add-flow input[placeholder="my-org/app"]', PROJECT);
  await page.type('.add-flow input[placeholder="logiforge/question-a"]', dir);
  await page.evaluate(() => document.querySelector('.add-flow button[type="submit"]').click());
}
try {
  step('サインインして、設定の repository のタブを開く');
  await page.goto(BASE + '/settings?tab=repository', { waitUntil: 'networkidle0' });
  await page.type('#user_name', LOGIN);
  await page.type('#password', PASSWORD);
  await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.keyboard.press('Enter')]);
  if (page.url().includes('/login/oauth/authorize')) {
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('button[name="granted"][value="true"]')]);
  }
  if (new URL(page.url()).pathname !== '/settings') await page.goto(BASE + '/settings?tab=repository', { waitUntil: 'networkidle0' });
  await page.waitForFunction(() => [...document.querySelectorAll('button')].some((b) => b.textContent === '足す'), { timeout: 20000 });

  step('スナップショットツリーがもうあるディレクトリを登録すると、最初の問いを飛ばす');
  await addDirectory(OLD, 'logiforge/' + OLD);
  await waitText('.add-flow [role="status"]', 'スナップショットツリーがもうあります');
  if (await page.$('.add-flow input[placeholder="ある究極の問いの主文"]')) throw new Error('もうあるのに、最初の問いの欄が出た');
  await waitText('.flow-steps li', '（もうある）', 5000);
  await waitText('.settings-list .strong', OLD);
  await page.screenshot({ path: OUT + '/add-4-exists.png' });
  await click('.add-flow button', '閉じる');

  step('新しいディレクトリ：2 段目を閉じても、一覧の「最初の問いを入れる」から続けられる');
  await addDirectory(FRESH, 'logiforge/' + FRESH);
  await page.waitForSelector('.add-flow input[placeholder="ある究極の問いの主文"]', { timeout: 20000 });
  await click('.add-flow button', 'あとで入れる');
  await page.waitForFunction(() => !document.querySelector('.add-flow'), { timeout: 5000 });
  await page.waitForFunction((n) => [...document.querySelectorAll('.settings-list li')].some((li) => li.textContent.includes(n) && li.textContent.includes('最初の問いを入れる')), { timeout: 20000 }, FRESH);
  if (await hasStart(OLD)) throw new Error(OLD + ' に「最初の問いを入れる」が出た（もうあるのに）');
  await page.screenshot({ path: OUT + '/add-5-list.png' });
  await page.evaluate((n) => [...document.querySelectorAll('.settings-list li')].find((li) => li.querySelector('.strong').textContent === n).querySelector('button.primary').click(), FRESH);
  await page.waitForSelector('.add-flow input[placeholder="ある究極の問いの主文"]', { timeout: 5000 });
  await page.type('.add-flow input[placeholder="ある究極の問いの主文"]', 'ディレクトリの問い');
  await page.evaluate(() => document.querySelector('.add-flow button[type="submit"]').click());
  await waitText('.add-flow [role="status"]', FRESH + ' を用意しました', 30000);
  await page.waitForFunction((n) => ![...document.querySelectorAll('.settings-list li')].some((li) => li.textContent.includes(n) && li.textContent.includes('最初の問いを入れる')), { timeout: 20000 }, FRESH);
  console.log('ok');
} catch (e) {
  await page.screenshot({ path: OUT + '/add-failed.png' }).catch(() => {});
  throw e;
} finally {
  await browser.close();
}
