// Mermaid を描く文書（/mermaid.html）。sandbox の iframe（allow-same-origin なし）の中で動く（design/webui/markdown.md）。
// Mermaid は、描く途中でインラインの style を使うので、この文書だけ CSP で style-src 'unsafe-inline' を許す。
// 親から { id, code } を受け取り、描いた SVG の文字列を { id, svg } で返す。描けなければ { id, error }。
// 親は、SVG を <img> で示す（SVG の中のスクリプトは動かない）。この文書は、アプリのオリジンにも、トークンにも触れない。
import mermaid from 'mermaid';

// ラベルは HTML（foreignObject）にせず、SVG の文字にする。<img> で示す SVG は XML でなければ読めず、HTML のラベルは XML にならないことがあるため
mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: 'default', fontFamily: 'sans-serif', htmlLabels: false, flowchart: { htmlLabels: false } });

let n = 0;
window.addEventListener('message', async (e: MessageEvent) => {
  // 親（この iframe を置いたページ）からの要求だけを受け取る
  if (e.source !== window.parent) return;
  const { id, code } = (e.data ?? {}) as { id?: unknown; code?: unknown };
  if (typeof id !== 'string' || typeof code !== 'string') return;
  try {
    const { svg } = await mermaid.render(`m${++n}`, code, document.getElementById('draw') ?? undefined);
    window.parent.postMessage({ id, svg }, '*');
  } catch (err) {
    window.parent.postMessage({ id, error: String((err as Error)?.message ?? err).slice(0, 300) }, '*');
  }
});
// 準備ができたことを知らせる
window.parent.postMessage({ ready: true }, '*');
