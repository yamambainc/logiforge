// Mermaid を描く文書（/mermaid.html）。sandbox の iframe（allow-same-origin なし）の中で動く（design/webui/markdown.md）。
// Mermaid は、描く途中でインラインの style を使うので、この文書だけ CSP で style-src 'unsafe-inline' を許す。
// CSP の sandbox allow-scripts で、この URL を直接開かれても、オパークなオリジンで動く。
// やりとり：ready を親に送り、親から MessagePort を 1 つだけ受け取る。以後、図の文 { id, code } と、SVG の文字列 { id, svg } は、ポートだけでやりとりする。
// 親は、SVG を <img> で示す（SVG の中のスクリプトは動かない）。この文書は、アプリのオリジンにも、トークンにも触れない。
import mermaid from 'mermaid';

// ラベルは HTML（foreignObject）にせず、SVG の文字にする。<img> で示す SVG は XML でなければ読めず、HTML のラベルは XML にならないことがあるため
// 文の長さと線の数に上限を置く（大きな図で、端末を止めないように）
mermaid.initialize({
  startOnLoad: false, securityLevel: 'strict', theme: 'default', fontFamily: 'sans-serif',
  htmlLabels: false, flowchart: { htmlLabels: false }, maxTextSize: 20_000, maxEdges: 500,
});

let n = 0;
let port: MessagePort | null = null;

window.addEventListener('message', (e: MessageEvent) => {
  // 親からの、最初のポートだけを受け取る
  if (port || e.source !== window.parent || !(e.data as { port?: unknown })?.port || e.ports.length !== 1) return;
  port = e.ports[0];
  port.onmessage = async (m: MessageEvent) => {
    const { id, code } = (m.data ?? {}) as { id?: unknown; code?: unknown };
    if (typeof id !== 'string' || typeof code !== 'string') return;
    try {
      const { svg } = await mermaid.render(`m${++n}`, code, document.getElementById('draw') ?? undefined);
      port!.postMessage({ id, svg });
    } catch (err) {
      port!.postMessage({ id, error: String((err as Error)?.message ?? err).slice(0, 300) });
    }
  };
});

// 準備ができたことを、親に知らせる。宛先は、この文書の URL のオリジン（親と同じ）。中身は ready だけ
window.parent.postMessage({ ready: true }, new URL(location.href).origin);
