// 利用者とエージェントが書く文章を、Markdown として示す（design/webui/markdown.md）。
// - Markdown は React の要素に組み立てる（react-markdown）。生の HTML は通さない（skipHtml。rehype-raw は使わない）。
// - リンクは http(s) と、同じオリジンだけ。ほかは文字として示す。画像は読みに行かず、リンクにする。
// - 表、コード、引用、チェックリスト（GFM）を示す。```mermaid は、図にする（MermaidBlock）。
import { useEffect, useState } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';

/** リンクとして出してよい URL か。http(s) と、同じオリジンのパス（/ で始まり、// でない）だけ */
export function safeHref(url: string | undefined | null): string | null {
  if (!url) return null;
  const u = url.trim();
  if (u.startsWith('/') && !u.startsWith('//')) return u;
  try {
    const p = new URL(u);
    return p.protocol === 'http:' || p.protocol === 'https:' ? p.href : null;
  } catch {
    return null;
  }
}

const components: Components = {
  a({ href, children }) {
    const h = safeHref(href);
    if (!h) return <span className="md-badlink">{children}</span>;
    const external = !h.startsWith('/');
    return <a href={h} {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>{children}</a>;
  },
  // 画像は、外へ読みに行くことになるので埋め込まない。代わりにリンクにする
  img({ src, alt }) {
    const h = safeHref(typeof src === 'string' ? src : undefined);
    const label = `画像：${alt || (typeof src === 'string' ? src : '')}`;
    return h ? <a href={h} target="_blank" rel="noopener noreferrer" className="md-img">{label}</a> : <span className="md-img">{label}</span>;
  },
  // 表は、はみ出す分を横にスクロールで見る
  table({ children }) {
    return <div className="md-scroll"><table>{children}</table></div>;
  },
  pre({ children }) {
    return <>{children}</>;
  },
  code({ className, children }) {
    const lang = /language-([\w-]+)/.exec(className ?? '')?.[1];
    const text = String(children ?? '');
    const block = !!className || text.includes('\n');
    if (lang === 'mermaid') return <MermaidBlock code={text.replace(/\n$/, '')} />;
    if (!block) return <code>{children}</code>;
    return <pre className="md-pre"><code>{text.replace(/\n$/, '')}</code></pre>;
  },
  // 見出しは、画面の見出しより小さくする（h3 から）
  h1: ({ children }) => <h3>{children}</h3>,
  h2: ({ children }) => <h4>{children}</h4>,
  h3: ({ children }) => <h5>{children}</h5>,
  h4: ({ children }) => <h6>{children}</h6>,
};

export function Markdown({ text, className }: { text: string; className?: string }) {
  return (
    <div className={`md ${className ?? ''}`}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={components} urlTransform={(u) => u}>{text}</ReactMarkdown>
    </div>
  );
}

// ```mermaid の図。描くのは sandbox の iframe（mermaid-host.ts）。描けなければ、元の文字列をコードとして示す
function MermaidBlock({ code }: { code: string }) {
  const [url, setUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  useEffect(() => {
    let live = true, made = '';
    setUrl(null); setFailed(null);
    import('./mermaid-host')
      .then((m) => m.renderMermaid(code))
      .then((svg) => {
        if (!live) return;
        // SVG は <img> で示す：中のスクリプトは動かず、外のものも読みに行かない
        made = URL.createObjectURL(new Blob([svg], { type: 'image/svg+xml' }));
        setUrl(made);
      })
      .catch((e: Error) => live && setFailed(e.message));
    return () => { live = false; if (made) URL.revokeObjectURL(made); };
  }, [code]);
  if (failed) {
    return (
      <div className="md-mermaid failed">
        <p className="dim small">図を描けませんでした（{failed}）。元の文を示します。</p>
        <pre className="md-pre"><code>{code}</code></pre>
      </div>
    );
  }
  if (!url) return <div className="md-mermaid loading-fig dim small">図を描いています…</div>;
  return <div className="md-mermaid md-scroll"><img src={url} alt="Mermaid の図" /></div>;
}

export default Markdown;
