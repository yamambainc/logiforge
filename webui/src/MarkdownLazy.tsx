// Markdown の表示を、要るときだけ読み込む（react-markdown と remark-gfm は gzip で約 50 KB。最初の読み込みを重くしない）。
// 読み込むまでは、元の文字列を、改行を保って示す（design/webui/markdown.md）。
import { lazy, Suspense } from 'react';

const Impl = lazy(() => import('./Markdown'));

export function Markdown({ text, className }: { text: string; className?: string }) {
  return (
    <Suspense fallback={<p className={`prose report md-plain ${className ?? ''}`}>{text}</p>}>
      <Impl text={text} className={className} />
    </Suspense>
  );
}
