# スクリプトの注入への備えと、ファイルの開き方

WebUI の応答のヘッダーと、利用者やエージェントが書いた文字やファイルの示し方を実装するときに読む。

## スクリプトの注入への備え

トークンはブラウザのメモリにあるので、スクリプトの注入（XSS）を防ぐことが、トークンを守ることになる（[受け入れた危険](auth.md#受け入れた危険)）。

- `logiforge serve` は、WebUI の応答に次のヘッダーを付ける。
  - `Content-Security-Policy: default-src 'self'; img-src 'self' data: blob:; style-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'`
  - `Referrer-Policy: no-referrer`
- `logiforge serve` は、WebUI と API のすべての応答に `X-Content-Type-Options: nosniff` を付ける。
- ビルドした `index.html` に、インラインの `<script>` と `<style>` がないことを、CI で確かめる。
  - なぜ：CSP の `script-src` と `style-src` を `'self'` だけにしておくため。
- `default-src 'self'` は、`connect-src` も `'self'` にする。そのため、注入されたスクリプトがあっても、`fetch` などでトークンを外のオリジンへ送れない。
- スナップショットツリーの文字（主文、背景情報、レポート）は、文字列として表示する。HTML として差し込まない（React の `dangerouslySetInnerHTML` を使わない）。
- 検証結果のレポートは、Markdown を整形しない。改行だけを保つ。
  - なぜ：整形には、HTML の無害化のライブラリが要るから。読む利用者にとって、整形の価値は小さい。
- artifact の `link` は、`https:` と `http:` のときだけ、リンクにする。ほかは文字列として示す。

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
