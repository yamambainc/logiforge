// 3D のフレームレートを測る。使い方: node scripts/fps3d.mjs <puppeteer のパス> <URL の元>
const [, , pp, base] = process.argv;
const { default: puppeteer } = await import(pp);
const browser = await puppeteer.launch({ args: ['--no-sandbox'], headless: 'shell', executablePath: process.env.CHROME });
const page = await browser.newPage();
const cases = [
  ['デスクトップ 1440×900', { width: 1440, height: 900, deviceScaleFactor: 1 }, 1],
  ['モバイル 375×812（CPU 4 倍遅い）', { width: 375, height: 812, deviceScaleFactor: 2, isMobile: true, hasTouch: true }, 4],
];
for (const [name, vp, cpu] of cases) {
  for (const open of [3, 99]) {
    await page.emulateCPUThrottling(1);
    await page.setViewport(vp);
    await page.goto(`${base}?n=4000&view=3d&open=${open}`, { waitUntil: 'networkidle0' });
    await page.waitForFunction(() => window.__3d && window.__3d.frames() > 0, { timeout: 60000 });
    await page.emulateCPUThrottling(cpu);
    const r = await page.evaluate(async () => {
      window.__3d.autoRotate(true, 6);
      await new Promise((r) => setTimeout(r, 500));
      window.__3dStat = { r: 0, l: 0, n: 0 };
      window.__3dLab = { pick: 0, np: 0, move: 0, nm: 0 };
      const f0 = window.__3d.frames(), t0 = performance.now();
      await new Promise((r) => setTimeout(r, 4000));
      const fps = ((window.__3d.frames() - f0) * 1000) / (performance.now() - t0);
      window.__3d.autoRotate(false);
      const st = window.__3dStat;
      const lb = window.__3dLab;
      // 選び直しの時間を測るため、1 回ぶんの動かすだけの時間も測る（回し続けて、選び直しの間に挟む）
      return { pickMs: Math.round((lb.pick / Math.max(lb.np, 1)) * 10) / 10, fps: Math.round(fps * 10) / 10, jsMsPerFrame: Math.round(((st.r + st.l) / st.n) * 10) / 10, shown: document.querySelector('.tree-head .dim')?.textContent };
    });
    console.log(name, `open=${open}`, JSON.stringify(r));
  }
}
await browser.close();
