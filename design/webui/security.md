# スクリプトの注入への備えと、ファイルの開き方

WebUI の応答のヘッダーと、利用者やエージェントが書いた文字やファイルの示し方を実装するときに読む。

## スクリプトの注入への備え

トークンはブラウザのメモリにあるので、スクリプトの注入（XSS）を防ぐことが、トークンを守ることになる（[受け入れた危険](auth.md#受け入れた危険)）。

- `logiforge serve` は、WebUI の応答に次のヘッダーを付ける。
  - `Content-Security-Policy: default-src 'self'; img-src 'self' data: blob:; style-src 'self'; frame-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'`
  - 例外は、Mermaid の図を描く `/mermaid.html` だけである。この文書には、インラインの style を許し、`sandbox allow-scripts` を付けた、別の CSP を付ける。アプリとは `MessageChannel` のポートだけでやりとりする（[Markdown と Mermaid の表示](markdown.md#csp)）。
- `assets/` の応答には、`Access-Control-Allow-Origin: *` を付ける。sandbox の iframe（オパークなオリジン）が、モジュールのスクリプトを読むためである。`assets/` には、公開してよい静的なファイルしかない。
  - `Referrer-Policy: no-referrer`
- `logiforge serve` は、WebUI と API のすべての応答に `X-Content-Type-Options: nosniff` を付ける。
- ビルドした出力（`index.html` と `mermaid.html` と、3D の部分を含むすべての JS と CSS）に、インラインの `<script>` と `<style>` がないことを、CI で確かめる。
  - なぜ：CSP の `script-src` と `style-src` を `'self'` だけにしておくため。
- `default-src 'self'` は、`connect-src` と `img-src` の外のオリジンも防ぐ。そのため、注入されたスクリプトがあっても、`fetch` や画像でトークンを外へ送ることはできない。ただし、ページの移動（`location.href` や `window.open` で外の URL を開く）は止めない（[受け入れた危険](auth.md#受け入れた危険)）。
- HTML を文字列から作らない。React の `dangerouslySetInnerHTML` を使わない。
- 主文とタイトルは、文字列として示す。レポート、背景情報、概要、人への依頼の本文と回答、レビューの本文は、Markdown を React の要素に組み立てて示す。生の HTML は通さない。Mermaid の図は、sandbox の iframe で描き、`<img>` で示す（[Markdown と Mermaid の表示](markdown.md)）。
- artifact の `link` と、Markdown の中のリンクは、`https:` と `http:` のとき（Markdown では、同じオリジンのパスも）だけ、リンクにする。ほかは文字列として示す。

## ファイルの開き方

artifact（`getArtifact`）と回答の添付（`getAnswerAttachment`）は、利用者やエージェントが置いた任意のファイルである。WebUI と同じオリジンから返るので、開き方を絞る。

- WebUI は、ファイルを `Authorization` を付けて読み、`application/octet-stream` の blob にして、`download` 属性のリンクで保存させる。タブや iframe では開かない。
- その場で示すのは、次のものだけである。
  - png、jpeg、gif、webp の画像。`<img>` で示す。
  - テキスト。文字列として示す。
- SVG は、画像としても示さない。保存させるだけである。
  - なぜ：SVG はスクリプトを含められるから。
- BE は、この 2 つの API の応答に、次のヘッダーを付ける。
  - `Content-Disposition: attachment`
  - `Content-Security-Policy: sandbox`
  - なぜ：URL を直接開かれても、WebUI と同じオリジンの文書として動かないようにするため。
