// Mermaid の図を、sandbox の iframe（/mermaid.html）で描いてもらう（design/webui/markdown.md）。
// iframe は、最初の図を描くときに 1 つだけ作る（Mermaid の部分は、そのときだけ読み込む）。
// 返ってきた SVG は、文字列から DOM を作らず、Blob にして <img> で示す（Markdown.tsx）。

let frame: HTMLIFrameElement | null = null;
let ready: Promise<void> | null = null;
const waiting = new Map<string, { resolve: (svg: string) => void; reject: (e: Error) => void }>();
let seq = 0;

/** SVG の大きさの上限（これより大きい応答は、図として扱わない） */
const MAX_SVG = 2 * 1024 * 1024;
/** 1 つの図を待つ時間 */
const TIMEOUT_MS = 15_000;

function ensureFrame(): Promise<void> {
  if (ready) return ready;
  ready = new Promise((resolve, reject) => {
    const f = document.createElement('iframe');
    // allow-same-origin を付けない：中の文書はオパークなオリジンになり、アプリのオリジンとストレージに触れない
    f.setAttribute('sandbox', 'allow-scripts');
    f.setAttribute('aria-hidden', 'true');
    f.setAttribute('tabindex', '-1');
    f.title = 'Mermaid を描く';
    f.className = 'mermaid-frame';
    f.src = '/mermaid.html';
    const t = setTimeout(() => reject(new Error('Mermaid を読み込めませんでした')), TIMEOUT_MS);
    window.addEventListener('message', (e: MessageEvent) => {
      if (!frame || e.source !== frame.contentWindow) return;
      const d = e.data as { ready?: boolean; id?: string; svg?: unknown; error?: unknown };
      if (d?.ready) { clearTimeout(t); resolve(); return; }
      const w = d?.id ? waiting.get(d.id) : undefined;
      if (!w) return;
      waiting.delete(d.id!);
      if (typeof d.svg === 'string' && d.svg.length <= MAX_SVG && d.svg.trimStart().startsWith('<svg')) w.resolve(d.svg);
      else w.reject(new Error(typeof d.error === 'string' ? d.error : 'Mermaid の図を描けませんでした'));
    });
    f.addEventListener('error', () => { clearTimeout(t); reject(new Error('Mermaid を読み込めませんでした')); });
    frame = f;
    document.body.appendChild(f);
  });
  // 読み込みに失敗したら、次の図で作り直す
  ready.catch(() => { frame?.remove(); frame = null; ready = null; });
  return ready;
}

/** Mermaid の文を、SVG の文字列にする。描けなければ Error */
export async function renderMermaid(code: string): Promise<string> {
  await ensureFrame();
  const id = `d${++seq}`;
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => { waiting.delete(id); reject(new Error('Mermaid の図が時間内に描けませんでした')); }, TIMEOUT_MS);
    waiting.set(id, { resolve: (s) => { clearTimeout(t); resolve(s); }, reject: (e) => { clearTimeout(t); reject(e); } });
    frame!.contentWindow!.postMessage({ id, code }, '*');
  });
}
