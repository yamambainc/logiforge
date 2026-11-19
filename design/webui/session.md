# サインインのセッション

WebUI のサインインを、再読み込みや新しいタブで保つ仕組みを実装するときに読む。
方式とトークンの持ち方は、[サインイン](auth.md) に書く。交換の中継は、[サインインの中継](oauth-relay.md) に書く。

## 目的

- GitForge の authorize に行くのは、次のときだけにする。
  - 最初のサインイン
  - リフレッシュトークンが切れたか、取り消されたとき
  - サインアウトしたあと
- なぜ：アクセストークンをメモリだけに持つと、再読み込み、新しいタブ、アクセストークンの期限のたびに、GitForge に行き来するから。

## どこに何を持つか

| もの | 置き場 |
|---|---|
| リフレッシュトークン | BE。設定の DB の `webui_sessions`。バックエンドの鍵で暗号化する |
| アクセストークン | ブラウザのメモリ。BE も、タブのあいだで分けるために、暗号化して持つ |
| セッション ID | ブラウザの cookie。BE は、その SHA-256 だけを持つ |

- `webui_sessions` の列は、[設定の DB](../settings/database.md) に書く。
- リフレッシュトークンは、ブラウザに渡さない。JavaScript から読める場所にも置かない。
- セッション ID は、32 バイトの乱数である。

## cookie

- 名前は `__Host-lf_session` である。属性は、`HttpOnly`、`Secure`、`SameSite=Strict`、`Path=/` で、`Domain` を付けない。
  - `HttpOnly`：スクリプトが注入されても、セッション ID を読まれないため。
  - `__Host-` と `Secure`：https でだけ送り、ほかのホストやパスから書き換えられないため。
  - `SameSite=Strict`：ほかのサイトからの要求に付かないため。
- 寿命は 180 日である。リフレッシュトークンが先に切れたら、そこで使えなくなる。
- cookie は、`/v1/oauth/` の下の要求でだけ読む。ほかの API の認証には使わない。API は、今までどおり `Authorization: Bearer` のアクセストークンで認証する。

## 要求

| 要求 | すること |
|---|---|
| `POST /v1/oauth/token` | 交換したあと、リフレッシュトークンがあればセッションを作り、cookie を付ける。前のセッションの cookie があれば、そのセッションを消す |
| `POST /v1/oauth/session` | cookie のセッションのアクセストークンを返す |
| `POST /v1/oauth/signout` | cookie のセッションを消し、cookie を消す |

- `POST /v1/oauth/session`（`getOAuthSession`）は、置いたアクセストークンの残りが 10 分より長ければ、それを返す。短ければ、GitForge で refresh し、新しいトークンを置いてから返す。
  - なぜ：タブがいくつあっても、refresh を期限の近くの 1 回にまとめるため。
- refresh は、セッションごとに 1 つずつ行う。BE は 1 台なので、BE のメモリのロックで足りる。
  - なぜ：GitHub と Forgejo のリフレッシュトークンは 1 回しか使えないので、同時に refresh すると、片方が断られるから。
- GitForge が新しいリフレッシュトークンを返したら、置き換える（回転）。
- 次のときは、セッションと cookie を消し、`unauthenticated`（401）を返す。WebUI は、authorize に行く。
  - cookie がない、または知らないセッションである。
  - セッションを作ったときの client_id が、今の設定と違う（サインインのアプリを作り直した）。
  - GitForge が refresh を断った（取り消された、期限が切れた、すでに使われた）。
- GitForge につながらないときは、セッションを消さずに `unavailable`（503）を返す。
- 交換の応答に、リフレッシュトークンか `expires_in` がなければ、セッションを作らない。そのときは、今までどおり、再読み込みのたびに authorize に行く。

## CSRF

cookie を読む 3 つの要求は、次のものだけを受け付ける。

- `POST` であること。
- `Content-Type: application/json` であること。フォームからは送れない。
- `Origin` が、WebUI のオリジン（設定の DB の WebUI の URL）と一致すること。違えば `forbidden`（403）を返す。

- CORS は開けない。ほかのサイトからは、応答のアクセストークンを読めない。

## WebUI

- 起動したとき（再読み込み、新しいタブ、共有されたリンク）は、まず `getOAuthSession` を呼ぶ。
  - 401 なら、authorize に行く（[サインインの流れ](signin-flow.md)）。
  - 503 なら、authorize に行かずに、「しばらくしてから」と示す。
- アクセストークンの期限の 1 分前に、`getOAuthSession` を呼ぶ。503 なら、30 秒後にもう 1 度呼ぶ。
- 回答や review を送る直前に、残りが 5 分より短ければ、`getOAuthSession` を呼ぶ。401 のときだけ、書きかけを置いて authorize に行く。
- API が `unauthenticated`（401）を返したら（そのトークンで 1 度は成功したあと）、`getOAuthSession` を 1 度呼ぶ。
  - 401 か、同じトークンしか返らなければ、authorize に行く。なぜ：GitForge でアクセストークンだけが取り消されたときに、同じトークンで繰り返さないため。
- サインアウトは、`POST /v1/oauth/signout` を呼ぶ。そのタブでは、`sessionStorage` の印で、次に開いてもサインインの画面を出す。

## 消すとき

- サインアウトしたとき。GitForge 側の取り消しはしない。
  - なぜ：リフレッシュトークンは BE の外に出ていないので、BE の記録を消せば、だれも使えないから。
- refresh を断られたとき。
- 180 日使われなかったとき（セッションを書くときに消す）。
- バックエンドの鍵を入れ替えたとき（[秘密の暗号化](../settings/secrets.md)）。暗号化し直さずに、すべて消す。利用者は、1 度だけサインインし直す。

## 構成ごとの寿命

| 構成 | アクセストークン | リフレッシュトークン |
|---|---|---|
| GitHub | 8 時間 | 6 か月 |
| Forgejo | 1 時間（既定） | 730 時間、約 30 日（既定） |

- Forgejo の寿命は、Forgejo の管理者が `[oauth2]` の `ACCESS_TOKEN_EXPIRATION_TIME` と `REFRESH_TOKEN_EXPIRATION_TIME` で変えられる。
- どちらの GitForge も、refresh に client secret を求め、リフレッシュトークンを 1 回しか使わせない。
- GitHub では、authorize に `offline_access` を付けるので、App の設定で期限を切っていなくても、リフレッシュトークンが来る（[スコープ](auth.md#スコープ)）。

受け入れた危険は、[サインイン](auth.md#受け入れた危険) に書く。
