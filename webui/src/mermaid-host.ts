// Mermaid の図を、sandbox の iframe（/mermaid.html）で描いてもらう（design/webui/markdown.md）。
// iframe は、最初の図を描くときに 1 つだけ作る（Mermaid の部分は、そのときだけ読み込む）。
// やりとり：iframe が ready を送ってきたら、MessageChannel を作り、片方のポートを渡す。以後の図の文と SVG は、ポートだけで送る。
// - ready は、置いた iframe から、最初の 1 回だけ受け取る。iframe が別の URL に移っても、新しい文書にはポートが渡らない（図の文が漏れない）
// - ほかのフレームやウィンドウからの message は、受け取らない（ポートの外の message を聞かない）
// 返ってきた SVG は、文字列から DOM を作らず、Blob にして <img> で示す（Markdown.tsx）。

/** SVG の大きさの上限（これより大きい応答は、図として扱わない） */
export const MAX_SVG = 2 * 1024 * 1024;
/** 1 つの図を待つ時間 */
export const TIMEOUT_MS = 15_000;

/** 受け取った SVG を、図として扱ってよいか。<img> で示すので動かないが、念のため、動くものを含むものは断る */
export function acceptSvg(svg: unknown): svg is string {
  if (typeof svg !== 'string' || svg.length > MAX_SVG || !svg.trimStart().startsWith('<svg')) return false;
  return !/<script|<foreignObject|\son[a-z]+\s*=|javascript:/i.test(svg);
}

/** iframe の代わりにもなる、最小の口（テストでは偽物を渡す） */
export interface Frame {
  /** iframe の中の window（message の送り主を見分けるため） */
  window: unknown;
  /** ポートを渡す */
  sendPort: (port: MessagePort) => void;
  remove: () => void;
}

export interface Host { render: (code: string) => Promise<string> }

/** 描く口を作る。makeFrame は iframe を作り、listen は window の message を聞く */
export function createHost(makeFrame: () => Frame, listen: (f: (e: { source: unknown; data: unknown }) => void) => () => void, timeoutMs = TIMEOUT_MS): Host {
  let frame: Frame | null = null;
  let port: Promise<MessagePort> | null = null;
  const waiting = new Map<string, { resolve: (svg: string) => void; reject: (e: Error) => void }>();
  let seq = 0;

  const connect = (): Promise<MessagePort> => {
    if (port) return port;
    port = new Promise<MessagePort>((resolve, reject) => {
      const f = makeFrame();
      frame = f;
      let done = false;
      const t = setTimeout(() => { stop(); reject(new Error('Mermaid を読み込めませんでした')); }, timeoutMs);
      const stop = listen((e) => {
        // 置いた iframe からの、最初の ready だけ
        if (done || e.source !== f.window || (e.data as { ready?: unknown })?.ready !== true) return;
        done = true;
        clearTimeout(t);
        stop();
        const ch = new MessageChannel();
        ch.port1.onmessage = (m: MessageEvent) => {
          const d = m.data as { id?: unknown; svg?: unknown; error?: unknown };
          const w = typeof d?.id === 'string' ? waiting.get(d.id) : undefined;
          if (!w) return;
          waiting.delete(d.id as string);
          if (acceptSvg(d.svg)) w.resolve(d.svg);
          else w.reject(new Error(typeof d.error === 'string' ? d.error.slice(0, 300) : 'Mermaid の図を描けませんでした'));
        };
        f.sendPort(ch.port2);
        resolve(ch.port1);
      });
    });
    // 読み込めなかったら、次の図で作り直す
    port.catch(() => { frame?.remove(); frame = null; port = null; });
    return port;
  };

  return {
    async render(code: string): Promise<string> {
      const p = await connect();
      const id = `d${++seq}`;
      return new Promise((resolve, reject) => {
        const t = setTimeout(() => { waiting.delete(id); reject(new Error('Mermaid の図が時間内に描けませんでした')); }, timeoutMs);
        waiting.set(id, { resolve: (s) => { clearTimeout(t); resolve(s); }, reject: (e) => { clearTimeout(t); reject(e); } });
        p.postMessage({ id, code });
      });
    },
  };
}

// --- ブラウザでの口 ---

let host: Host | null = null;

function makeIframe(): Frame {
  const f = document.createElement('iframe');
  // allow-same-origin を付けない：中の文書はオパークなオリジンになり、アプリのオリジンとストレージに触れない
  f.setAttribute('sandbox', 'allow-scripts');
  f.setAttribute('aria-hidden', 'true');
  f.setAttribute('tabindex', '-1');
  f.title = 'Mermaid を描く';
  f.className = 'mermaid-frame';
  f.src = '/mermaid.html';
  document.body.appendChild(f);
  return {
    window: f.contentWindow,
    // ポートを渡す相手は、オパークなオリジンなので、宛先は '*' でしか書けない。渡すのはポートだけで、図の文は含めない
    sendPort: (p) => f.contentWindow?.postMessage({ port: true }, '*', [p]),
    remove: () => f.remove(),
  };
}

/** Mermaid の文を、SVG の文字列にする。描けなければ Error */
export function renderMermaid(code: string): Promise<string> {
  host ??= createHost(makeIframe, (f) => {
    const h = (e: MessageEvent) => f({ source: e.source, data: e.data });
    window.addEventListener('message', h);
    return () => window.removeEventListener('message', h);
  });
  return host.render(code);
}
