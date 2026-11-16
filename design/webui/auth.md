# 認証

WebUI のサインインと、トークンの扱いを実装するときに読む。
BE が主体を確かめる方法は、[認証](../backend/ports/authentication.md) と [Forgejo アダプタの認証](../adapters/forgejo.md#認証) に書く。

## トークンの受け取り方

Forgejo を使う構成では、利用者は、Forgejo で作ったアクセストークンを、WebUI のサインインの画面に貼る。

- トークンの範囲は、`read:user` と `read:organization` だけでよい。
  - なぜ：BE は、利用者のトークンを主体を確かめるのにだけ使い（`GET /user`、`GET /user/teams`）、GitForge への書き込みは書き込み主体が行うから（[書き込み主体と主体](../adapters/forgejo.md#書き込み主体と主体)）。
  - サインインの画面には、Forgejo のトークンの作成画面へのリンクと、選ぶ範囲を示す。
- 貼ったトークンで `GET /v1/repositories` を呼び、成功したらサインインできたとみなす。
  - `unauthenticated` なら、トークンが正しくないか、organization の主体でないと示す。
  - 主体が `user` でないとき（`client` のトークン）は、repository の作成と回答で `forbidden` になる。そのときに、利用者の team に入っていないことを示す。
- なぜ OAuth のログインにしないか：Forgejo に OAuth のアプリケーションを登録し、トークンの交換と更新を WebUI に作る必要があり、利用者が少ない今は、その手間に見合わないから。
- GitHub を使う構成のサインイン（GitHub App のユーザー認可）は、[GitHub アダプタ](../adapters/github.md#認証) と一緒に作る。この WebUI の範囲ではない。

## トークンの持ち方

- トークンは、ブラウザの `sessionStorage` に置く。
  - タブを閉じると消える。タブを開き直したら、もう一度貼る。
  - なぜ `localStorage` にしないか：ブラウザを閉じても残り、盗まれたときに使える期間が長くなるから。
  - なぜ cookie にしないか：BE は、要求ごとに `Authorization` ヘッダーでトークンを受け取る（[API の認証](../api/README.md#認証)）。cookie にすると、BE に CSRF の対策が要るから。
- API を呼ぶたびに、`Authorization: Bearer <トークン>` を付ける。
- `unauthenticated`（401）が返ったら、トークンを消し、サインインの画面に戻す。
- サインアウトは、トークンを消すだけである。トークンを無効にするのは、利用者が Forgejo で行う。

## スクリプトの注入への備え

トークンをブラウザに置くので、スクリプトの注入（XSS）を防ぐことが、トークンを守ることになる。

- `logiforge serve` は、WebUI の応答に次のヘッダーを付ける。
  - `Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self'; frame-ancestors 'none'`
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: no-referrer`
- スナップショットツリーの文字（主文、背景情報、レポート）は、文字列として表示する。HTML として差し込まない（React の `dangerouslySetInnerHTML` を使わない）。
- 検証結果のレポートは、Markdown を整形しない。改行だけを保つ。
  - なぜ：整形には、HTML の無害化のライブラリが要るから。読む利用者にとって、整形の価値は小さい。
- artifact の `link` は、`https:` と `http:` のときだけ、リンクにする。ほかは文字列として示す。
