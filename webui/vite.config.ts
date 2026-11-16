import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 出力は dist/。logiforge serve が embed して配る（design/webui/stack.md）。
// 開発では、API を手元の serve（127.0.0.1:8080）に渡す。
export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: false, assetsInlineLimit: 0 },
  server: { proxy: { '/v1': 'http://127.0.0.1:8080' } },
  test: { environment: 'node', include: ['src/**/*.test.ts'] },
});
