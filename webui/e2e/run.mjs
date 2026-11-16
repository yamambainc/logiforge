// ブラウザの E2E（internal/e2e/webui_test.go から動かす）。
// WebUI を開く → Forgejo にログイン → 許可 → repository の一覧 → 選ぶ → リロードしても押す操作なしに戻る → サインアウト。
// organization にいない人は「organization に入っていません」で止まる（authorize への行き来が止まらなくならない）ことも確かめる。
// 環境変数：BASE（serve の URL）、LOGIN、PASSWORD、CHROME（Chrome のパス）、OUT（スクリーンショットの置き場）、REPO、
// OUTSIDER_LOGIN、OUTSIDER_PASSWORD（organization にいない Forgejo のユーザー）、NODE_A（検証結果のある仮説）、NODE_B（検証結果のない葉）。
// スナップショットツリーには、inactive のストーリー「広告の導線」もある。
// HR（open の PVR）。AR_MAIN（main 向きで open。NODE_B に検証結果（偽）を出す）、AR_DEV（向き先が develop。NODE_A の背景情報を足す）。
import { mkdirSync, writeFileSync } from 'node:fs';
import puppeteer from 'puppeteer-core';

const { BASE, LOGIN, PASSWORD, CHROME, OUT, REPO, OUTSIDER_LOGIN, OUTSIDER_PASSWORD, NODE_A, NODE_B, AR_MAIN, AR_DEV, HR } = process.env;
mkdirSync(OUT, { recursive: true });
const browser = await puppeteer.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage();
await page.setViewport({ width: 1280, height: 800 });
const step = (s) => console.log('…', s);
// ページの中の誤りは、ログに出す（白い画面になったときに、理由が分かるように）
const logErrors = (p) => p.on('pageerror', (e) => console.error('pageerror:', e.message));
logErrors(page);

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
// 詳細のパネルを読み終える（撮る前に、読み込み中の表示がなくなるのを待つ）
const detailLoaded = (p) => p.waitForFunction(() => document.querySelector('.detail') && !document.querySelector('.detail .skel'), { timeout: 20000 });
const rowTexts = (p) => p.$$eval('.row .text', (els) => els.map((e) => e.textContent));
const selText = (p) => p.$eval('.row.sel .text', (e) => e.textContent);
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

  step('repository を作る：名前と主文を書くと、ビュワーが開き、取り込まれしだい根が出る');
  await page.evaluate(() => [...document.querySelectorAll('button')].find((b) => b.textContent === '新しい repository').click());
  await page.type('form.create input[placeholder="question-a"]', 'made');
  await page.type('form.create input[placeholder="ある究極の問いの主文"]', 'あ'.repeat(51));
  await page.evaluate(() => document.querySelector('form.create button[type="submit"]').click());
  await page.waitForFunction(() => document.querySelector('.form-error')?.textContent.includes('50 字以下'), { timeout: 5000 });
  await page.screenshot({ path: OUT + '/create.png' });
  await page.$eval('form.create input[placeholder="ある究極の問いの主文"]', (e) => { e.select(); });
  await page.keyboard.press('Backspace');
  await page.type('form.create input[placeholder="ある究極の問いの主文"]', 'ブラウザから作った問い');
  await page.evaluate(() => document.querySelector('form.create button[type="submit"]').click());
  await page.waitForFunction(() => location.pathname === '/r/made/tree', { timeout: 30000 });
  await page.waitForFunction(() => [...document.querySelectorAll('.row .text')].some((e) => e.textContent === 'ブラウザから作った問い'), { timeout: 60000 });
  await page.goto(BASE + '/', { waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForSelector('.repo-list', { timeout: 20000 });

  step('repository を選ぶ');
  await page.evaluate((r) => [...document.querySelectorAll('.repo-list a')].find((a) => a.querySelector('.repo-name').textContent === r).click(), REPO);
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
  if (new URL(page.url()).searchParams.get('node') !== NODE_A) throw new Error('URL に node が入らない');
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/viewer-outline.png' });

  step('読み上げ：アウトラインは tree、選んだ行は aria-activedescendant で示す。図は img');
  const a11y = await page.evaluate(() => {
    const t = document.querySelector('.tree-scroll'), sel = document.querySelector('.row.sel');
    return { role: t.getAttribute('role'), label: t.getAttribute('aria-label'), active: t.getAttribute('aria-activedescendant'), id: sel.id,
      level: sel.getAttribute('aria-level'), size: sel.getAttribute('aria-setsize'), pos: sel.getAttribute('aria-posinset') };
  });
  if (a11y.role !== 'tree' || !a11y.label || a11y.active !== a11y.id || a11y.level !== '3' || a11y.size !== '2' || !['1', '2'].includes(a11y.pos)) throw new Error('読み上げの属性：' + JSON.stringify(a11y));

  step('キーボード：↓（または ↑）で隣の行（検証結果のない葉）、← で親、← で閉じ、→ で開く');
  await page.focus('.tree-scroll');
  // 兄弟の並びは識別子の順なので、検証結果のない葉は上にも下にもありうる
  await page.keyboard.press(a11y.pos === '1' ? 'ArrowDown' : 'ArrowUp');
  await page.waitForFunction((b) => new URL(location.href).searchParams.get('node') === b, { timeout: 5000 }, NODE_B);
  await page.waitForFunction(() => document.querySelector('.d-sec .dim')?.textContent === 'まだ検証結果がありません。' || [...document.querySelectorAll('.d-sec p.dim')].some((e) => e.textContent === 'まだ検証結果がありません。'), { timeout: 20000 });
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/viewer-leaf-unverified.png' });
  await page.keyboard.press('ArrowLeft');
  await page.waitForFunction(() => document.querySelector('.row.sel .text')?.textContent === '招待の導線', { timeout: 5000 });
  await page.keyboard.press('ArrowLeft');
  await page.waitForFunction(() => document.querySelector('.row.sel')?.getAttribute('aria-expanded') === 'false', { timeout: 5000 });
  if ((await rowTexts(page)).some((t) => t.includes('継続率も'))) throw new Error('← で閉じない');
  await page.keyboard.press('ArrowRight');
  await page.waitForFunction(() => document.querySelector('.row.sel')?.getAttribute('aria-expanded') === 'true', { timeout: 5000 });

  step('/ で検索に移り、Esc で語を消す');
  await page.keyboard.press('/');
  if (!(await page.evaluate(() => document.activeElement === document.querySelector('.search input')))) throw new Error('/ で検索に移らない');
  await page.keyboard.type('継続');
  await page.keyboard.press('Escape');
  if ((await page.$eval('.search input', (e) => e.value)) !== '') throw new Error('Esc で語が消えない');

  step('絞り込み：真なら検証結果のある仮説だけ、未決なら検証結果のない葉も出る');
  const chip = (label) => page.evaluate((l) => [...document.querySelectorAll('.chip')].find((b) => b.textContent === l).click(), label);
  await chip('真');
  await page.waitForFunction(() => ![...document.querySelectorAll('.row .text')].some((e) => e.textContent.includes('継続率も')), { timeout: 5000 });
  if (!(await rowTexts(page)).some((t) => t.includes('成果物までの時間が半分'))) throw new Error('真で絞り込むと、真の仮説が消える');
  await chip('真');
  await chip('未決');
  await page.waitForFunction(() => [...document.querySelectorAll('.row .text')].some((e) => e.textContent.includes('継続率も')), { timeout: 5000 });
  if ((await rowTexts(page)).some((t) => t.includes('成果物までの時間が半分'))) throw new Error('未決で絞り込んでも、真の仮説が出る');
  await chip('未決');

  step('inactive のストーリーは、印つきで出る。隠すこともできる');
  await page.waitForFunction(() => [...document.querySelectorAll('.row.inactive .text')].some((e) => e.textContent === '広告の導線'), { timeout: 5000 });
  await page.click('.chips .check input');
  await page.waitForFunction(() => ![...document.querySelectorAll('.row .text')].some((e) => e.textContent === '広告の導線'), { timeout: 5000 });
  await page.click('.chips .check input');

  step('デスクトップで ?node= を開くと、そのノードを選ぶ。ないノードなら、そう示す');
  await page.goto(BASE + `/r/${REPO}/tree?node=${NODE_B}`, { waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForSelector('.row.sel', { timeout: 20000 });
  if (!(await selText(page)).includes('継続率も')) throw new Error('?node= のノードを選ばない：' + (await selText(page)));
  await page.goto(BASE + `/r/${REPO}/tree?node=h-0000000000`, { waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForFunction(() => document.querySelector('.notice')?.textContent.includes('このノードはありません'), { timeout: 20000 });
  await page.goto(BASE + `/r/${REPO}/tree?node=${NODE_A}`, { waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForSelector('.row.sel', { timeout: 20000 });

  step('AR を重ねる：変わるノード（伝播で変わる祖先も）に印。最初の直接の変更を選び、n と p でたどる（伝播だけの祖先はたどらない）。帯に件数');
  await page.select('select[aria-label="AR を重ねる"]', AR_MAIN);
  await page.waitForSelector('.banner.overlay', { timeout: 30000 });
  if (new URL(page.url()).searchParams.get('apply_request') !== AR_MAIN) throw new Error('URL に apply_request が入らない');
  await page.waitForSelector('.row.chg-changed', { timeout: 10000 });
  const changed = await page.$$eval('.row.chg-changed .text', (els) => els.map((e) => e.textContent));
  if (!changed.some((t) => t.includes('継続率も')) || !changed.includes('招待の導線')) throw new Error('変わるノード（と伝播で変わる祖先）に印がない：' + changed.join(' / '));
  if (!(await selText(page)).includes('継続率も')) throw new Error('最初の直接の変更を選ばない：' + (await selText(page)));
  const stepText = () => page.$eval('.banner .steps .dim', (e) => e.textContent);
  if ((await stepText()) !== '1 / 1') throw new Error('直接の変更の数：' + (await stepText()));
  // 伝播だけの祖先（ストーリー）を選んでから n を押すと、直接の変更に戻る
  await page.evaluate(() => [...document.querySelectorAll('.row')].find((r) => r.querySelector('.text').textContent === '招待の導線').click());
  await page.waitForFunction(() => document.querySelector('.banner .steps .dim')?.textContent === '0 / 1', { timeout: 5000 });
  await page.evaluate(() => document.activeElement?.blur());
  await page.keyboard.press('n');
  await page.waitForFunction(() => document.querySelector('.banner .steps .dim')?.textContent === '1 / 1' && document.querySelector('.row.sel .text')?.textContent.includes('継続率も'), { timeout: 5000 });
  await page.keyboard.press('p');
  await page.waitForFunction(() => document.querySelector('.banner .steps .dim')?.textContent === '1 / 1', { timeout: 5000 });
  await page.evaluate(() => [...document.querySelectorAll('.row')].find((r) => r.textContent.includes('継続率も')).click());
  await page.waitForFunction(() => [...document.querySelectorAll('.d-sec h3')].some((h) => h.textContent.startsWith('この AR での変化')) && document.querySelector('table.changes')?.textContent.includes('値'), { timeout: 10000 });
  await page.waitForFunction(() => document.querySelector('.verdict .val')?.textContent === '偽', { timeout: 20000 });
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/overlay-outline.png' });
  await page.evaluate(() => [...document.querySelectorAll('.chip')].find((b) => b.textContent === 'この AR で変わる').click());
  if ((await rowTexts(page)).some((t) => t === '広告の導線')) throw new Error('「この AR で変わる」で、変わらないノードが出る');
  await page.evaluate(() => [...document.querySelectorAll('.chip')].find((b) => b.textContent === 'この AR で変わる').click());
  await page.evaluate(() => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent.includes('図')).click());
  await page.waitForSelector('.gnode.g-changed', { timeout: 10000 });
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/overlay-graph.png' });
  await page.evaluate(() => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent.includes('アウトライン')).click());

  step('向き先が main でない AR は、黄の帯で「検査していません」と示す。戻ると前の AR');
  await page.select('select[aria-label="AR を重ねる"]', AR_DEV);
  await page.waitForFunction(() => document.querySelector('.banner.overlay.unchecked')?.textContent.includes('main 向きではないので、検査していません'), { timeout: 30000 });
  await page.waitForSelector('.row.chg-changed', { timeout: 10000 });
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/overlay-unchecked.png' });
  await page.goBack();
  await page.waitForFunction((a) => new URL(location.href).searchParams.get('apply_request') === a && document.querySelector('.banner.overlay:not(.unchecked)'), { timeout: 30000 }, AR_MAIN);
  await page.goForward();
  await page.waitForFunction((a) => new URL(location.href).searchParams.get('apply_request') === a && document.querySelector('.banner.overlay.unchecked'), { timeout: 30000 }, AR_DEV);
  await page.goBack();
  await page.waitForSelector('.banner.overlay:not(.unchecked)', { timeout: 30000 });

  step('過去の時点：main の古いコミットを選ぶと、その時点の姿。審査中の AR の札は出さない。今の main に戻れる');
  const oldest = await page.$$eval('select[aria-label="時点"] option', (os) => os[os.length - 1].value);
  await page.select('select[aria-label="時点"]', oldest);
  await page.waitForSelector('.banner.past', { timeout: 30000 });
  await page.waitForSelector('.row', { timeout: 10000 });
  if ((await rowTexts(page)).some((t) => t.includes('招待の導線'))) throw new Error('最初のコミットに、あとで足したストーリーが出る');
  if (await page.$('.row .t-ar')) throw new Error('過去の時点に、審査中の AR の札が出る');
  if ((await page.$$eval('.chip', (cs) => cs.map((c) => c.textContent))).some((t) => t === '審査中の AR' || t === 'open の依頼')) throw new Error('過去の時点に、審査中の AR と依頼の絞り込みが出る');
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/past.png' });
  // 戻る・進むで、時点も戻る
  await page.goBack();
  await page.waitForFunction(() => !document.querySelector('.banner.past') && document.querySelector('.banner.overlay'), { timeout: 30000 });
  await page.goForward();
  await page.waitForSelector('.banner.past', { timeout: 30000 });
  await page.evaluate(() => [...document.querySelectorAll('.banner.past button')].find((b) => b.textContent === '今の main に戻る').click());
  await page.waitForFunction(() => !document.querySelector('.banner') && !new URL(location.href).searchParams.get('commit'), { timeout: 30000 });
  await page.waitForSelector('.row', { timeout: 10000 });

  step('ここから見る：ストーリーを根にして、その下だけを示す。パンくずで上へ戻る。戻るでも戻る');
  await page.evaluate(() => [...document.querySelectorAll('.row')].find((r) => r.querySelector('.text').textContent === '招待の導線').click());
  await page.waitForSelector('.scope-btn', { timeout: 10000 });
  await page.click('.scope-btn');
  await page.waitForSelector('.scope', { timeout: 5000 });
  if (!new URL(page.url()).searchParams.get('root')) throw new Error('URL に root が入らない');
  const scoped = await rowTexts(page);
  if (scoped[0] !== '招待の導線' || scoped.some((t) => t.includes('新しい導線') || t === '広告の導線')) throw new Error('ここから見るで、根の部分木の外が出る：' + scoped.join(' / '));
  if (!(await page.$eval('.tree-head .dim', (e) => e.textContent)).startsWith('3 ノード')) throw new Error('ここから見るのノード数が、部分木の数でない');
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/scope.png' });
  await page.goBack();
  await page.waitForFunction(() => !document.querySelector('.scope') && !new URL(location.href).searchParams.get('root'), { timeout: 5000 });
  await page.goForward();
  await page.waitForSelector('.scope', { timeout: 5000 });
  await page.evaluate(() => [...document.querySelectorAll('.scope button')].find((b) => b.textContent === '全体を見る').click());
  await page.waitForFunction(() => !document.querySelector('.scope'), { timeout: 5000 });

  step('node= で図を開くと、そのノードのまわり（親・兄弟）を最初に示す');
  await page.goto(BASE + `/r/${REPO}/tree?view=graph&node=${NODE_A}`, { waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForSelector('.gnode.sel', { timeout: 20000 });
  await new Promise((r) => setTimeout(r, 500));
  const seen = await page.evaluate(() => {
    const g = document.querySelector('.graph').getBoundingClientRect();
    const inView = (el) => { const b = el.getBoundingClientRect(); return b.left >= g.left && b.right <= g.right && b.top >= g.top && b.bottom <= g.bottom; };
    return [...document.querySelectorAll('.gnode')].filter(inView).map((e) => e.textContent);
  });
  if (!seen.some((t) => t.includes('成果物までの時間が半分')) || !seen.some((t) => t.includes('継続率も')) || !seen.some((t) => t.includes('招待の導線'))) throw new Error('まわりが画面にない：' + seen.join(' / '));
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/node-around.png' });
  await page.goto(BASE + `/r/${REPO}/tree`, { waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForSelector('.row', { timeout: 20000 });

  step('3D：開くと描く（URL に view=3d）。canvas は role=img。キーボードで選ぶと、詳細と読み上げの領域に出る');
  await page.evaluate(() => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent === '3D').click());
  await page.waitForFunction(() => window.__3d && window.__3d.frames() > 0, { timeout: 30000 });
  if (new URL(page.url()).searchParams.get('view') !== '3d') throw new Error('URL に view=3d が入らない');
  const canvas = await page.$eval('.three canvas', (c) => [c.getAttribute('role'), c.getAttribute('aria-label')]);
  if (canvas[0] !== 'img' || !canvas[1].includes('アウトライン')) throw new Error('3D の canvas の読み上げ：' + canvas.join(' / '));
  await page.focus('.three');
  await page.keyboard.press('ArrowDown');
  await page.waitForFunction(() => document.querySelector('.three .sr-only[aria-live]')?.textContent.includes('値は'), { timeout: 5000 });
  await page.waitForSelector('.sel3', { timeout: 5000 });
  if ((await page.$$('.l3:not([style*="display: none"])')).length === 0) throw new Error('3D のラベルが出ない');
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/3d.png' });

  step('3D：アウトラインと 20 回行き来しても描ける（片づけで、WebGL の文脈が溜まらない）');
  const modeBtn = (label) => page.evaluate((l) => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent.includes(l)).click(), label);
  for (let i = 0; i < 20; i++) {
    await modeBtn('アウトライン');
    await page.waitForSelector('.tree-scroll', { timeout: 10000 });
    await modeBtn('3D');
    await page.waitForFunction(() => window.__3d && window.__3d.frames() > 0, { timeout: 10000 });
  }

  // 止まっているあいだは描かない（requestAnimationFrame の繰り返しもやめる）
  await new Promise((r) => setTimeout(r, 1500));
  const idle = await page.evaluate(async () => { const f0 = window.__3d.frames(); await new Promise((r) => setTimeout(r, 700)); return window.__3d.frames() - f0; });
  if (idle !== 0) throw new Error('止まっているのに描いている：' + idle + ' フレーム');

  step('3D：WebGL の文脈が失われたら「3D の描画が止まりました」。「3D を作り直す」で描ける');
  await page.evaluate(() => window.__3d.loseContext());
  await page.waitForFunction(() => document.querySelector('.empty-title')?.textContent === '3D の描画が止まりました', { timeout: 10000 });
  await page.evaluate(() => [...document.querySelectorAll('.empty-box button')].find((b) => b.textContent === '3D を作り直す').click());
  await page.waitForFunction(() => window.__3d && window.__3d.frames() > 0 && document.querySelector('.three canvas'), { timeout: 10000 });

  step('3D でも AR を重ねられる');
  await page.select('select[aria-label="AR を重ねる"]', AR_MAIN);
  await page.waitForSelector('.banner.overlay', { timeout: 30000 });
  await page.waitForFunction(() => window.__3d && window.__3d.frames() > 0 && document.querySelector('.three canvas'), { timeout: 30000 });
  await new Promise((r) => setTimeout(r, 300));
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/3d-overlay.png' });
  await page.goto(BASE + `/r/${REPO}/tree`, { waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForSelector('.row', { timeout: 20000 });

  step('WebGL が使えない端末では「この端末では 3D を表示できません」と示し、図で見られる');
  const nogl = await browser.newPage();
  logErrors(nogl);
  await nogl.setViewport({ width: 1280, height: 800 });
  await nogl.evaluateOnNewDocument(() => {
    const orig = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (t, ...rest) { return /webgl/.test(t) ? null : orig.call(this, t, ...rest); };
  });
  await nogl.goto(BASE + `/r/${REPO}/tree?view=3d`, { waitUntil: 'networkidle0' });
  await nogl.waitForFunction(() => document.querySelector('.empty-title')?.textContent === 'この端末では 3D を表示できません', { timeout: 30000 });
  await nogl.evaluate(() => [...document.querySelectorAll('.empty-box button')].find((b) => b.textContent === '図で見る').click());
  await nogl.waitForSelector('.gnode', { timeout: 10000 });
  await nogl.close();

  step('3D の部分の読み込みに失敗したら、そう示す。図に移ってから 3D に戻ると、読み直して描ける');
  const flaky = await browser.newPage();
  logErrors(flaky);
  await flaky.setViewport({ width: 1280, height: 800 });
  await flaky.setRequestInterception(true);
  let failedOnce = false;
  flaky.on('request', (r) => {
    if (!failedOnce && /\/assets\/Viewer3D-.*\.js/.test(r.url())) { failedOnce = true; r.abort(); } else r.continue();
  });
  await flaky.goto(BASE + `/r/${REPO}/tree?view=3d`, { waitUntil: 'networkidle0' });
  await flaky.waitForFunction(() => document.querySelector('.empty-box p')?.textContent.includes('3D の部分を読み込めませんでした'), { timeout: 30000 });
  await flaky.evaluate(() => [...document.querySelectorAll('.empty-box button')].find((b) => b.textContent === '図で見る').click());
  await flaky.waitForSelector('.gnode', { timeout: 10000 });
  await flaky.evaluate(() => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent === '3D').click());
  await flaky.waitForFunction(() => window.__3d && window.__3d.frames() > 0, { timeout: 30000 });
  await flaky.close();

  step('ダッシュボード：上のタブで移る。根の値、作業の終わりの判定、着手できる作業の件数');
  await page.evaluate(() => [...document.querySelectorAll('.topbar .tab')].find((t) => t.textContent === 'ダッシュボード').click());
  await page.waitForFunction((r) => location.pathname === `/r/${r}`, { timeout: 5000 }, REPO);
  await page.waitForSelector('table.judge', { timeout: 20000 });
  const counts = await page.$$eval('ul.counts li', (ls) => Object.fromEntries(ls.map((l) => [l.querySelector('span').textContent, Number(l.querySelector('b').textContent)])));
  // 未決の葉は、open の AR が対象にしているので数えない（listWork の include_claimed）。レビュー待ちは、AR_MAIN
  if (counts['未決の葉'] !== 0 || !(counts['レビュー待ち'] >= 1)) throw new Error('着手できる作業の件数：' + JSON.stringify(counts));
  await page.screenshot({ path: OUT + '/dashboard.png' });
  if (!(await page.$eval('table.judge', (t) => t.textContent)).includes('根の値が決まってから数えます')) throw new Error('根の値が決まる前の文がない');
  await page.evaluate(() => [...document.querySelectorAll('.topbar .tab')].find((t) => t.textContent === 'スナップショットツリー').click());
  await page.waitForSelector('.row', { timeout: 20000 });

  step('人への依頼：タブで移り、PVR を開く。本文は文字列のまま。回答の書きかけは、リロードしても残る');
  await page.evaluate(() => [...document.querySelectorAll('.topbar .tab')].find((t) => t.textContent === '人への依頼').click());
  await page.waitForFunction((r) => location.pathname === `/r/${r}/human-requests`, { timeout: 5000 }, REPO);
  await page.waitForSelector('ul.items a', { timeout: 20000 });
  await page.evaluate((id) => [...document.querySelectorAll('ul.items a')].find((a) => a.getAttribute('href') === `?id=${id}`).click(), HR);
  await page.waitForFunction(() => document.querySelector('.item-pane .report')?.textContent.includes('# 手順'), { timeout: 20000 });
  await page.type('.answer-form textarea', '測った。継続率は下がらなかった');
  const measure = OUT + '/measure.txt';
  writeFileSync(measure, 'rate,0.42\n');
  const pick = async () => { const input = await page.$('.answer-form input[type="file"]'); await input.uploadFile(measure); await page.waitForFunction(() => document.querySelector('ul.attached')?.textContent.includes('measure.txt'), { timeout: 5000 }); };
  await pick();
  await page.reload({ waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForFunction(() => document.querySelector('.answer-form textarea')?.value.includes('下がらなかった'), { timeout: 20000 });
  await page.waitForFunction(() => document.querySelector('.answer-form .note')?.textContent.includes('measure.txt'), { timeout: 5000 });
  await pick();
  if (await page.$('.answer-form .note')) throw new Error('選び直したのに、選び直しの案内が残る');
  await page.type('input[aria-label="リンクの名前"]', '集計表');
  await page.type('input[aria-label="リンクの URL"]', 'https://example.com/sheet');
  await page.evaluate(() => [...document.querySelectorAll('.link-add button')].find((b) => b.textContent === 'リンクを添える').click());
  await page.screenshot({ path: OUT + '/hr-answer.png' });
  await page.evaluate(() => document.querySelector('.answer-form button[type="submit"]').click());
  await page.waitForSelector('.answer', { timeout: 30000 });
  const files = await page.$$eval('.answer .file', (fs) => fs.map((f) => f.textContent));
  if (!files.some((t) => t.includes('measure.txt'))) throw new Error('回答の添付が出ない：' + files.join(' / '));
  // リンクの添付は、BE が回答の本文の末尾に書く（internal/backend/writes.go の AnswerHumanRequest）
  if (!(await page.$eval('.answer .report', (e) => e.textContent)).includes('集計表：https://example.com/sheet')) throw new Error('リンクの添付が、回答の本文にない');
  if (await page.evaluate(() => Object.keys(sessionStorage).some((k) => k.startsWith('lf-draft:')))) throw new Error('回答したのに、書きかけが残る');
  await page.screenshot({ path: OUT + '/hr-answered.png' });

  step('ダッシュボードの「回答済みの依頼」を押すと、回答済みの依頼の一覧へ行く');
  await page.evaluate(() => [...document.querySelectorAll('.topbar .tab')].find((t) => t.textContent === 'ダッシュボード').click());
  await page.waitForSelector('ul.counts', { timeout: 20000 });
  await page.evaluate(() => [...document.querySelectorAll('ul.counts a')].find((a) => a.textContent.includes('回答済みの依頼')).click());
  await page.waitForFunction(() => new URL(location.href).searchParams.get('state') === 'answered' && document.querySelector('ul.items a'), { timeout: 20000 });
  await page.goto(BASE + `/r/${REPO}/tree`, { waitUntil: 'networkidle0' });
  await signedIn(page);
  await page.waitForSelector('.row', { timeout: 20000 });

  step('検索すると、一致したノードと祖先だけが出る');
  await page.type('.search input', '継続率も');
  await page.waitForFunction(() => document.querySelector('.search .count')?.textContent === '1 / 1', { timeout: 5000 });
  await page.click('.search input', { clickCount: 3 });
  await page.keyboard.press('Escape');

  step('図に切り替える（選んだノードは同じ）。縦にもできる');
  await page.evaluate(() => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent.includes('図')).click());
  await page.waitForSelector('.gnode.sel', { timeout: 10000 });
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/viewer-graph.png' });
  await page.evaluate(() => [...document.querySelectorAll('.seg.mini button')].find((b) => b.textContent === '縦').click());
  await page.waitForFunction(() => new URL(location.href).searchParams.get('orient') === 'v');
  await detailLoaded(page);
  await page.screenshot({ path: OUT + '/viewer-graph-v.png' });

  step('表示と向きは履歴に積む。戻ると横、もう 1 度戻るとアウトライン');
  await page.goBack();
  await page.waitForFunction(() => [...document.querySelectorAll('.seg.mini button.on')].some((b) => b.textContent === '横'), { timeout: 5000 });
  await page.goBack();
  await page.waitForSelector('.tree-scroll', { timeout: 5000 });
  await page.goForward();
  await page.goForward();
  await page.waitForFunction(() => [...document.querySelectorAll('.seg.mini button.on')].some((b) => b.textContent === '縦'), { timeout: 5000 });

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
  await detailLoaded(mobile);
  if (await mobile.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('ビュワーが幅 375 px で横にはみ出す');
  await mobile.screenshot({ path: OUT + '/viewer-mobile-detail.png' });
  await mobile.click('.sheet-close');
  await mobile.evaluate(() => [...document.querySelectorAll('.seg.mode button')].find((b) => b.textContent.includes('図')).click());
  await mobile.waitForSelector('.gnode', { timeout: 10000 });
  await mobile.screenshot({ path: OUT + '/viewer-mobile-graph.png' });

  step('モバイルの 3D：描けて、はみ出さない');
  await mobile.goto(BASE + `/r/${REPO}/tree?view=3d`, { waitUntil: 'networkidle0' });
  await mobile.waitForFunction(() => window.__3d && window.__3d.frames() > 0, { timeout: 30000 });
  await new Promise((r) => setTimeout(r, 300));
  if (await mobile.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('3D が幅 375 px で横にはみ出す');
  await mobile.screenshot({ path: OUT + '/3d-mobile.png' });

  step('モバイル：下のタブで、ダッシュボードに移る。はみ出さない');
  await mobile.waitForSelector('.bottom-nav', { timeout: 5000 });
  await mobile.evaluate(() => [...document.querySelectorAll('.bottom-nav a')].find((a) => a.textContent.includes('状況')).click());
  await mobile.waitForSelector('table.judge', { timeout: 20000 });
  if (await mobile.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('ダッシュボードが幅 375 px で横にはみ出す');
  await mobile.screenshot({ path: OUT + '/dashboard-mobile.png' });

  step('モバイルの人への依頼：一覧から選ぶと画面全体で示し、左上の「← 一覧」で戻る');
  await mobile.evaluate(() => [...document.querySelectorAll('.bottom-nav a')].find((a) => a.textContent.includes('依頼')).click());
  await mobile.waitForSelector('.list-filters', { timeout: 20000 });
  await mobile.select('.list-filters label:nth-child(2) select', 'answered');
  await mobile.waitForSelector('ul.items a', { timeout: 20000 });
  await mobile.tap('ul.items a');
  await mobile.waitForSelector('.item-pane .answer', { timeout: 20000 });
  if (await mobile.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('人への依頼が幅 375 px で横にはみ出す');
  await mobile.screenshot({ path: OUT + '/hr-mobile.png' });
  await mobile.evaluate(() => [...document.querySelectorAll('.item-pane .back')][0].click());
  await mobile.waitForSelector('ul.items a', { timeout: 10000 });

  step('モバイルで AR を重ねる：帯が出て、はみ出さない');
  await mobile.goto(BASE + `/r/${REPO}/tree?apply_request=${AR_MAIN}`, { waitUntil: 'networkidle0' });
  await mobile.waitForSelector('.banner.overlay', { timeout: 30000 });
  await mobile.waitForSelector('.row.chg-changed', { timeout: 10000 });
  if (await mobile.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('重ねたビュワーが幅 375 px で横にはみ出す');
  await mobile.screenshot({ path: OUT + '/overlay-mobile.png' });
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
