import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import { SHELL_STATIC } from './src/sw-route';

// 出力は dist/。logiforge serve が embed して配る（design/webui/stack.md）。
// 開発では、API を手元の serve（127.0.0.1:8080）に渡す。
export default defineConfig({
  plugins: [react(), pwaShell()],
  // mermaid.html は、Mermaid を描く sandbox の iframe の文書（design/webui/markdown.md）
  // sw は Service Worker。スコープを / にするため、ハッシュを付けずに dist/sw.js に置く（design/webui/pwa.md）
  build: {
    outDir: 'dist', emptyOutDir: false, assetsInlineLimit: 0,
    rollupOptions: {
      input: { main: 'index.html', mermaid: 'mermaid.html', sw: 'src/sw.ts' },
      output: { entryFileNames: (c) => (c.name === 'sw' ? 'sw.js' : 'assets/[name]-[hash].js') },
    },
  },
  server: { proxy: { '/v1': 'http://127.0.0.1:8080' } },
  test: { environment: 'node', include: ['src/**/*.test.ts'] },
});

/**
 * ビルドの最後に、dist/sw.js に、版と、控えに入れる外枠の一覧を入れる（design/webui/pwa.md の「更新」）。
 * 外枠は、index.html と、index.html が読む JS と CSS と、public/ のアイコンと manifest である。使うときだけ読む部分（3D、Mermaid）は入れない。
 * 版は、外枠の中身から作る。どれかが変われば sw.js が変わり、ブラウザが新しい Service Worker を入れる。
 */
function pwaShell(): Plugin {
  let outDir = 'dist';
  return {
    name: 'logiforge-pwa-shell',
    apply: 'build',
    configResolved(c) { outDir = c.build.outDir; },
    writeBundle() {
      const html = readFileSync(join(outDir, 'index.html'), 'utf8');
      const refs = [...html.matchAll(/\s(?:src|href)="(\/assets\/[^"]+)"/g)].map((m) => m[1]);
      const shell = ['/index.html', ...new Set(refs), ...SHELL_STATIC];
      const h = createHash('sha256');
      for (const u of shell) h.update(u).update('\0').update(readFileSync(join(outDir, u === '/index.html' ? 'index.html' : u.slice(1))));
      const version = h.digest('hex').slice(0, 16);
      const file = join(outDir, 'sw.js');
      let sw = readFileSync(file, 'utf8');
      const before = sw;
      sw = sw.replace(/\[\s*(["'`])__LF_SHELL__\1\s*\]/, () => JSON.stringify(shell)).replaceAll('__LF_VERSION__', version);
      if (sw === before || sw.includes('__LF_')) throw new Error('sw.js に、版か外枠の一覧を入れられなかった');
      writeFileSync(file, sw);
    },
  };
}
