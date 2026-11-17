import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { Markdown, safeHref } from './Markdown';

const html = (text: string) => renderToStaticMarkup(createElement(Markdown, { text }));

// 利用者とエージェントの書いた文章を、Markdown として示しても、スクリプトを動かさない（design/webui/markdown.md）
describe('Markdown の XSS', () => {
  it('生の HTML は通さない（script、onerror、iframe、style）', () => {
    const out = html('前 <script>alert(1)</script> <img src=x onerror="alert(1)"> <iframe src="https://evil.example"></iframe> <b style="color:red">太</b> 後');
    expect(out).not.toMatch(/<script|onerror|<iframe|style=|<b\b/i);
    expect(out).toContain('前');
  });
  it('javascript: や data: のリンクは、リンクにしない', () => {
    for (const u of ['javascript:alert(1)', 'JaVaScRiPt:alert(1)', ' javascript:alert(1)', 'data:text/html,<script>alert(1)</script>', 'vbscript:x', '//evil.example/x']) {
      const out = html(`[押す](${u})`);
      expect(out, u).not.toMatch(/href=/);
      expect(out).toContain('押す');
    }
  });
  it('自動リンクと参照のリンクも同じ', () => {
    expect(html('<javascript:alert(1)>')).not.toMatch(/href="javascript/i);
    expect(html('[a][r]\n\n[r]: javascript:alert(1)')).not.toMatch(/href=/);
  });
  it('http(s) は新しいタブで、rel=noopener noreferrer。同じオリジンのパスは、そのまま', () => {
    expect(html('[外](https://example.com/a?b=1)')).toContain('href="https://example.com/a?b=1" target="_blank" rel="noopener noreferrer"');
    expect(html('[中](/r/q/tree?node=h-1)')).toContain('href="/r/q/tree?node=h-1"');
    expect(html('[中](/r/q/tree?node=h-1)')).not.toContain('target=');
  });
  it('画像は埋め込まず、リンクにする', () => {
    const out = html('![図](https://example.com/a.png) ![x](javascript:alert(1))');
    expect(out).not.toMatch(/<img/);
    expect(out).toContain('画像：図');
    expect(out).toContain('href="https://example.com/a.png"');
    expect(out).not.toMatch(/href="javascript/);
  });
  it('Mermaid の文は、このページでは描かない（sandbox の iframe で描く）。HTML も click もここでは動かない', () => {
    const out = html('```mermaid\ngraph TD\n  A["<img src=x onerror=alert(1)>"] --> B\n  click A call alert()\n```');
    expect(out).not.toMatch(/<img|onerror|<svg/i);
    expect(out).toContain('図を描いています');
  });
});

describe('GFM', () => {
  it('表、コード、引用、チェックリスト', () => {
    const out = html('| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nfmt.Println(1)\n```\n\n> 引用\n\n- [x] 済み\n- [ ] まだ');
    expect(out).toContain('<table>');
    expect(out).toContain('class="md-scroll"');
    expect(out).toContain('<pre class="md-pre"><code>fmt.Println(1)</code></pre>');
    expect(out).toContain('<blockquote>');
    expect(out).toMatch(/type="checkbox"[^>]*checked/);
  });
  it('見出しは h3 から', () => {
    expect(html('# 大見出し\n## 中')).toBe('<div class="md "><h3>大見出し</h3>\n<h4>中</h4></div>');
  });
});

describe('safeHref', () => {
  it('http(s) と同じオリジンのパスだけ', () => {
    expect(safeHref('https://a.example/x')).toBe('https://a.example/x');
    expect(safeHref('HTTP://A.example')).toBe('http://a.example/');
    expect(safeHref('/r/q')).toBe('/r/q');
    expect(safeHref('//evil.example')).toBeNull();
    expect(safeHref('mailto:a@example.com')).toBeNull();
    expect(safeHref('relative/path')).toBeNull();
    expect(safeHref(undefined)).toBeNull();
  });
});
