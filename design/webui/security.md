# スクリプトの注入への備えと、ファイルの開き方

WebUI の応答のヘッダーと、利用者やエージェントが書いた文字やファイルの示し方を実装するときに読む。

## スクリプトの注入への備え

トークンはブラウザのメモリにあるので、スクリプトの注入（XSS）を防ぐことが、トークンを守ることになる（[受け入れた危険](auth.md#受け入れた危険)）。

- `logiforge serve` は、WebUI の応答に次のヘッダーを付ける。
  - `Content-Security-Policy: default-src 'self'; img-src 'self' data: blob:; style-src 'self'; frame-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'`
  - `Referrer-Policy: no-referrer`
- PWA の manifest（`manifest-src`）と Service Worker（`worker-src`）は、`default-src 'self'` で許される。CSP を変えずに済む（[PWA](pwa.md)）。
- CSP の例外は、Mermaid の図を描く `/mermaid.html` だけである。この文書には、インラインの style を許し、`sandbox allow-scripts` を付けた、別の CSP を付ける（[Markdown と Mermaid の表示](markdown.md#csp)）。
- `assets/` の応答には、`Access-Control-Allow-Origin: *` を付ける。
  - なぜ：sandbox の iframe の中の文書はオパークなオリジンなので、同じサーバのモジュールのスクリプトでも、別のオリジンへの要求（CORS）として読むから。
  - なぜ `*` でよいか：`assets/` には、ビルドした JS と CSS しかなく、だれが読んでもよい。トークンや利用者のデータは含まない。`Authorization` も Cookie も使わない要求である。
- `logiforge serve` は、WebUI と API のすべての応答に `X-Content-Type-Options: nosniff` を付ける。
- ビルドした出力（`index.html` と `mermaid.html` と、3D の部分を含むすべての JS と CSS）に、インラインの `<script>` と `<style>` がないことを、CI で確かめる。
  - なぜ：CSP の `script-src` と `style-src` を `'self'` だけにしておくため。
- `default-src 'self'` は、`connect-src` と `img-src` の外のオリジンも防ぐ。そのため、注入されたスクリプトがあっても、`fetch` や画像でトークンを外へ送ることはできない。ただし、ページの移動（`location.href` や `window.open` で外の URL を開く）は止めない（[受け入れた危険](auth.md#受け入れた危険)）。
- HTML を文字列から作らない。React の `dangerouslySetInnerHTML` を使わない。
- 利用者とエージェントが書く文章は、生の HTML を通さずに示す。示し方は、[Markdown と Mermaid の表示](markdown.md) に書く。
- artifact の `link` は、`https:` と `http:` のときだけ、リンクにする。ほかは文字列として示す。Markdown の中のリンクは、[Markdown の組み立て](markdown.md#markdown-の組み立て) に従う。

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
