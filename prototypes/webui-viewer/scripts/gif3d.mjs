// 3D を回す様子を GIF にする。使い方: node scripts/gif3d.mjs <puppeteer のパス> <URL> <出力先.gif> [幅] [高さ] [mobile]
import { writeFileSync } from 'node:fs';
import { PNG } from 'pngjs';
import gifenc from 'gifenc';
const { GIFEncoder, quantize, applyPalette } = gifenc;
const [, , pp, url, out, w = '900', h = '640', mob] = process.argv;
const { default: puppeteer } = await import(pp);
const browser = await puppeteer.launch({ args: ['--no-sandbox'], headless: 'shell', executablePath: process.env.CHROME });
const page = await browser.newPage();
await page.setViewport({ width: +w, height: +h, deviceScaleFactor: 1, isMobile: !!mob, hasTouch: !!mob });
await page.goto(url, { waitUntil: 'networkidle0' });
await page.waitForFunction(() => window.__3d && window.__3d.frames() > 0, { timeout: 60000 });
await new Promise((r) => setTimeout(r, 1200));
const el = await page.$('.three');
const enc = GIFEncoder();
const N = 48;
for (let i = 0; i < N; i++) {
  await page.evaluate((d) => window.__3d.orbit(d), 360 / N);
  await new Promise((r) => setTimeout(r, 180));
  const png = PNG.sync.read(await el.screenshot({ type: 'png' }));
  const palette = quantize(png.data, 128);
  enc.writeFrame(applyPalette(png.data, palette), png.width, png.height, { palette, delay: 70 });
}
enc.finish();
writeFileSync(out, enc.bytes());
await browser.close();
console.log(out, enc.bytes().length, 'bytes');
