# Markdown と Mermaid の表示

利用者とエージェントが書く文章を Markdown として示すところと、その中の Mermaid の図を描くところを実装するときに読む。
スクリプトの注入への備えの全体は、[スクリプトの注入への備えと、ファイルの開き方](security.md) に書く。

## 示すもの

| 文章 | 示し方 |
|---|---|
| 検証結果のレポート、人への依頼の本文と回答、apply request のレビューと議論の本文、背景情報、ストーリーの概要 | Markdown |
| 主文、タイトル | 文字列のまま |

- なぜ主文とタイトルは文字列のままか：一覧の行、図のノード、パンくずなど、1 行に収める場所で使うから。50 字以下の 1 文で、整形の価値は小さい。
- 見出しは、画面の見出しより小さくするため、`#` を `h3` として示す（`##` は `h4`）。

## Markdown の組み立て

- Markdown は、React の要素に組み立てる（`react-markdown`）。HTML の文字列は作らない。`dangerouslySetInnerHTML` は使わない。
- 文章の中の生の HTML は通さない（`skipHtml`）。`rehype-raw` は使わない。
- GFM（表、取り消し線、自動リンク、チェックリスト）を示す（`remark-gfm`）。
- リンクは、`http:` と `https:` と、同じオリジンのパス（`/` で始まり、`//` でないもの）だけにする。ほかは、リンクにせず文字として示す。
  - 外へのリンクは、新しいタブで開き、`rel="noopener noreferrer"` を付ける。
  - URL は `new URL()` で読んで `protocol` を確かめる。添付のリンクと同じ決まりである（[ファイルの開き方](security.md#ファイルの開き方)）。
- 画像は、埋め込まない。「画像：代わりの文」のリンクにする。
  - なぜ：外のオリジンの画像を読みに行くと、読んだことが外に知られるから。CSP の `img-src` も、外のオリジンを許していない。
  - 案：同じ repository の artifact を指す画像（png、jpeg、gif、webp）だけは、`getArtifact` で読んで、blob の `<img>` で示せる。要るかどうかは、使ってみて決める。
- コードは、色付けをしない。
  - 案：色付けのライブラリは、言語の定義を含めると大きい。レポートのコードは短いことが多いので、まず等幅だけで示す。
- 表とコードと図は、はみ出す分を横のスクロールで見る。モバイルでも、画面は横に広がらない。
- Markdown の部分（`react-markdown` と `remark-gfm`）は、示すときだけ読み込む（gzip で約 50 KB）。読み込むまでは、元の文字列を、改行を保って示す。

## Mermaid の図

```` ```mermaid ```` のコードは、図にする。

- 図は、`sandbox="allow-scripts"` の iframe（`/mermaid.html`）の中で、Mermaid に描かせる。`allow-same-origin` は付けない。
  - iframe の中の文書は、アプリと別のオリジン（オパーク）になる。アプリのストレージにも、アプリの文書にも触れない。
  - iframe は、最初の図を描くときに 1 つだけ作り、画面の外に置く。Mermaid の部分は、そのときだけ読み込む。
- アプリは、iframe に文を送り（`postMessage`）、描いた SVG の文字列を受け取る。
  - 受け取った SVG は、文字列から DOM を作らず、`Blob` にして `<img>` で示す。`<img>` の SVG では、中のスクリプトは動かず、外のものも読みに行かない。
  - 受け取るのは、その iframe からの応答だけである（`event.source` を確かめる）。2 MiB を超えるものと、`<svg` で始まらないものは、図として扱わない。
- Mermaid は、`securityLevel: 'strict'` にする（`click` で関数を呼ばない、ラベルの HTML を無害化する）。ラベルは HTML にしない（`htmlLabels: false`）。
  - なぜ HTML にしないか：HTML のラベルは XML にならないことがあり、`<img>` で SVG を読めなくなるから。
- 描けないとき（文法の誤り、読み込みの失敗、15 秒たっても描けない）は、元の文をコードとして示し、描けなかったことを添える。

## CSP

Mermaid は、描く途中で、インラインの `style` を使う。今の CSP（`style-src 'self'`）のままでは、図の大きさを測れず、正しく描けないことを確かめた（headless Chrome）。次の 2 つを比べ、1 を選んだ。

| 案 | 中身 | 選ばなかった理由 |
|---|---|---|
| 1. sandbox の iframe | `/mermaid.html` だけに、`style-src 'self' 'unsafe-inline'` を許す CSP を付ける | （選んだ） |
| 2. CSP を緩める | アプリの全体の `style-src` に `'unsafe-inline'` を足す | 注入された CSS で、画面を偽ったり、属性の値を外へ漏らしたりできるようになるから。範囲を `style-src-attr` に絞っても、アプリの全体に及ぶ |

- `/mermaid.html` の CSP：`default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'`
  - `connect-src` は `'none'` なので、iframe の中から通信はできない。
- アプリの CSP には、`frame-src 'self'` を足す。
- iframe の中の文書は、オパークなオリジンからモジュールのスクリプトを読むので、`assets/` の応答に `Access-Control-Allow-Origin: *` を付ける。`assets/` には、公開してよい静的なファイルしかない。

## 大きさ

ビルドで測った（gzip）。どれも、最初の読み込み（約 95 KB）には入らない。

| 部分 | 大きさ | 読み込むとき |
|---|---|---|
| Markdown（`react-markdown`、`remark-gfm`） | 約 50 KB | Markdown の文章を示すとき |
| Mermaid の iframe の文書と、Mermaid の本体 | 約 160 KB | 最初の図を描くとき |
| 流れ図（`graph`）を描くまで | 合わせて約 640 KB | 最初の流れ図を描くとき。ほかの種類の図は、それぞれの部分を足して読む |

- `assets/` は、ハッシュ付きの名前で、1 年キャッシュする（[配り方](stack.md#配り方)）。2 回目からは読み込まない。
