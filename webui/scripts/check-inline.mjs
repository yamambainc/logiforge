// ビルドした出力に、インラインの <script> と <style>、style 属性がないことを確かめる（design/webui/security.md）。
// CSP の script-src と style-src を 'self' だけにしておくため。使い方: node scripts/check-inline.mjs dist
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const root = process.argv[2] ?? 'dist';
const bad = [];
const walk = (dir) => {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p);
    else if (p.endsWith('.html')) check(p, readFileSync(p, 'utf8'));
  }
};
const check = (file, html) => {
  for (const m of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)) {
    if (!/\bsrc\s*=/.test(m[1]) || m[2].trim() !== '') bad.push(`${file}: インラインの <script>`);
  }
  if (/<style\b/i.test(html)) bad.push(`${file}: インラインの <style>`);
  if (/\sstyle\s*=/i.test(html)) bad.push(`${file}: style 属性`);
  if (/\son[a-z]+\s*=/i.test(html)) bad.push(`${file}: イベントの属性（on…）`);
};
walk(root);
if (bad.length) {
  console.error(bad.join('\n'));
  process.exit(1);
}
console.log('インラインの script と style はない');
