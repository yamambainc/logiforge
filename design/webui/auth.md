# サインイン

WebUI のサインインと、トークンの扱いを実装するときに読む。
BE が主体を確かめる方法は、[認証](../backend/ports/authentication.md)、[GitHub アダプタの認証](../adapters/github.md#認証)、[Forgejo アダプタの認証](../adapters/forgejo.md#認証) に書く。
スクリプトの注入への備えは、[スクリプトの注入への備えと、ファイルの開き方](security.md) に書く。
authorize から戻るまでの流れは、[サインインの流れ](signin-flow.md) に書く。
再読み込みや新しいタブでサインインを保つ仕組みは、[サインインのセッション](session.md) に書く。

## 方式

- GitForge の OAuth2（認可コードと PKCE）でサインインする。トークンを貼る形は作らない。
  - PKCE の `code_challenge_method` は、どちらの構成も `S256` にする。GitHub は `plain` を受け付けない。
- サインインに使うアプリは、構成で決まる。

| 構成 | サインインに使うアプリ | 作る人 |
|---|---|---|
| GitHub を使う | Logiforge の GitHub App（[GitHub App とトークン](../adapters/github-app.md)） | 初回のセットアップで、manifest flow で作る（[初回のセットアップ](../settings/setup.md#github-を使う構成app-を-manifest-flow-で作る)） |
| Forgejo を使う | Forgejo の OAuth2 のアプリケーション（confidential client） | 初回のセットアップで、Forgejo で作る（[初回のセットアップ](../settings/setup.md#forgejo-を使う構成)） |

- authorize とトークンのエンドポイントは、次のとおり。
  - GitHub：GitHub の画面の URL（既定は `https://github.com`）の `/login/oauth/authorize` と `/login/oauth/access_token`
  - Forgejo：Forgejo の URL の `/login/oauth/authorize` と `/login/oauth/access_token`
- client_id、client secret、`redirect_uri` の元の WebUI の URL、GitForge の URL は、BE が要求ごとに設定の DB から読む（[設定](../settings/README.md#設定の-db-に置くもの)）。
- トークンの交換は、BE が中継する（[サインインの中継](oauth-relay.md)、[サインインの流れ](signin-flow.md)）。
- ブラウザが持つのは、アクセストークンと、セッションの cookie だけである。リフレッシュトークンは、BE が持ち、ブラウザに渡さない（[トークンの持ち方](#トークンの持ち方)）。

## client secret の役目

- client secret は、守りにはならない。
  - なぜ：中継があるので、code を持っていれば、だれでも BE を通して交換できるから。
  - 守りは、`redirect_uri` の完全一致、PKCE、`state` である。PKCE と confidential client の組み合わせは、OAuth 2.1 と OAuth の Security BCP が勧める形である。
- client secret が要る理由は、構成で違う。
  - GitHub：GitHub は、交換に client secret を必ず求める。
  - Forgejo：Forgejo に、同意の画面を省かせるため。Forgejo は、許可済みの利用者が authorize に来たとき、confidential client なら同意の画面を出さずにすぐ戻す。public client なら、毎回同意の画面を出す。
- どちらの構成でも、許可済みの利用者には、同意の画面が出ない。
  - なぜ要るか：セッションが切れたときや、サインアウトしたあとに authorize に行くとき、押させないためである。
- refresh にも、client secret を使う。GitHub も Forgejo も、refresh に client secret を求める。
- なぜ BE が中継するか：
  - client secret を、ブラウザに渡さずに済むから。
  - WebUI と BE は同じオリジンなので、GitForge の CORS と、CSP の `connect-src` を広げずに済むから。Forgejo の CORS の設定は、Forgejo の API 全体に効く。GitHub のトークンのエンドポイントは、ブラウザからの要求に CORS で応じない。

## スコープ

### GitHub を使う構成

- GitHub App のトークンの権限は、スコープでは決まらない。App の権限と、その人のリポジトリの権限の、両方にあるものになる（[GitHub App とトークン](../adapters/github-app.md#アカウントとトークン)）。
- authorize の `scope` には、`offline_access` だけを付ける。ほかのスコープは、GitHub App では効かない。
- authorize には、`prompt` などの、ログインや同意をやり直させる値を付けない。値は、`state` と `code_challenge` のほかは、毎回同じである。
  - [GitHub の docs](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/refreshing-user-access-tokens) では、`offline_access` を付けると、App の設定で期限を切っていなくても、期限つきの user access token（8 時間）とリフレッシュトークンを返す。リフレッシュトークンは、BE が持つ（[サインインのセッション](session.md)）。
  - リフレッシュトークンが来なければ、BE はセッションを作らない。そのときは、再読み込みのたびに authorize に行く。
  - なぜ：期限のないトークンは、盗まれたときに、利用者が許可を取り消すまで使えるから。リフレッシュトークンがあれば、再読み込みのたびに GitHub に行かずに済むから。
- 交換の応答に `expires_in` かリフレッシュトークンがなければ、BE はセッションを作らない。アクセストークンは返す。
- 盗まれたときに書ける範囲は、App が install されていて、その人が書けるリポジトリの、App の権限の中である。

### Forgejo を使う構成

- スコープは、`read:user`、`read:organization`、`write:organization`、`write:repository`、`write:issue` にする。
  - `read:user` と `read:organization`：BE が、主体を確かめるため（`GET /user`、`GET /user/teams`。[Forgejo アダプタの認証](../adapters/forgejo.md#認証)）。
  - `write:organization` と `write:repository`：BE が、利用者のトークンで、organization に repository を作り、最初のコミットを置くため（[Forgejo アダプタ](../adapters/forgejo.md) の「repository の用意」）。
    - `write:repository` は、WebUI のレビューで、pull request に review を出し、merge し、commit status を立てるのにも使う（[WebUI からのレビュー](../settings/users.md#webui-からのレビュー)）。
  - `write:issue`：BE が、利用者のトークンで、回答のコメントを書き、issue を閉じるため。pull request のラベルを付け替え、コメントを書くのにも使う。
- 盗まれたときに書ける範囲は、利用者が Forgejo で書けるすべての repository に広がる。

どちらの構成でも、書く権限を求める。なぜ：Logiforge は、書くための自分のアカウントを持たず、要求を送った主体のトークンで書くから（[GitForge のポートを pull request と issue で実装する](../adapters/pull-requests.md)）。そのかわり、トークンは、メモリにだけ持ち、短い寿命で使う（[トークンの持ち方](#トークンの持ち方)）。

## トークンの持ち方

- アクセストークンは、ブラウザでは、メモリ（JavaScript の変数）だけに持つ。`localStorage`、`sessionStorage`、cookie には保存しない。
- アクセストークンの寿命は、構成で決まる。
  - GitHub：8 時間（[user access token](../adapters/github-app.md#user-access-token)）
  - Forgejo：1 時間（Forgejo の既定）
- リフレッシュトークンは、BE が設定の DB に暗号化して持ち、ブラウザに渡さない。ブラウザには、セッション ID を入れた `HttpOnly` の cookie だけを渡す（[サインインのセッション](session.md)）。
  - なぜ BE に持つか：JavaScript から読めない場所に置き、サインアウトで BE の記録を消せば使えなくするため。
- 同じブラウザのタブは、同じセッションの、同じアクセストークンを使う。期限が近づくと、BE が 1 度だけ refresh する。
- 再読み込み、新しいタブ、期限の前には、セッションからアクセストークンをもらう。authorize には行かない。
- API を呼ぶたびに、`Authorization: Bearer <アクセストークン>` を付ける。
- セッションが切れたときは、authorize へのページの移動が起きるので、書きかけを失わないようにする。
  - 人への依頼の回答の書きかけは、書くたびに `sessionStorage` に置き、戻ったら戻す（置くものは [人への依頼](screens.md#人への依頼) に書く）。
    - なぜ置いてよいか：トークンではないから。
    - 添付のファイルの中身は置かない。戻ったら、選び直すように示す。
  - 回答を送る直前に、アクセストークンの残りが 5 分より少なければ、先にセッションから新しいものをもらう。セッションが切れていれば、authorize に行く。

## サインアウト

- サインアウトは、BE のセッションと cookie、メモリのアクセストークン、回答の書きかけ（`sessionStorage`）を消し、サインインの画面を出す。
  - GitForge での取り消しはしない。リフレッシュトークンは BE の外に出ていないので、BE の記録を消せば使えないからである。
  - サインインの画面には、構成に合わせて「GitHub でサインイン」か「Forgejo でサインイン」のボタンだけを置く。構成は、`getOAuthConfig` の `gitforge` で知る。
- GitForge のログインは残る。そのため、新しいタブを開いたり、ボタンを押したりすると、押す操作なしにサインインし直す。
  - 本当に外れるには、GitForge からログアウトする。サインインの画面に、そのためのリンクを置く。

## 受け入れた危険

- スクリプトが注入されると、そのページが開いているあいだ、メモリのアクセストークンは読まれ、Logiforge の API がその利用者として使われうる（スナップショットツリーを読む、人への依頼に回答する、repository を作る）。
  - apply request を approve して、merge することも使われうる。盗まれた WebUI のトークンでできることの中で、いちばん大きい（[WebUI からのレビュー](../settings/users.md#webui-からのレビュー)）。
  - 設定の API も使われうる（[利用者の決め方](../settings/users.md#利用者の決め方)）。
- CSP の `default-src 'self'` は、`fetch` や画像で外のオリジンへ送ることを防ぐ。ただし、ページの移動（`location.href` や `window.open` で外の URL を開く）でトークンを外へ送ることは、止めない（[スクリプトの注入への備え](security.md)）。
- 外へ渡ったアクセストークンを使えるのは、その寿命（GitHub は 8 時間、Forgejo は 1 時間）までである。
- スクリプトが注入されると、そのページが開いているあいだ、`POST /v1/oauth/session` で、新しいアクセストークンをもらわれうる。リフレッシュトークンとセッション ID は、読まれない（`HttpOnly`）。
- 設定の DB とバックエンドの鍵の両方が漏れると、リフレッシュトークンで、その寿命（GitHub は 6 か月、Forgejo は約 30 日）まで、利用者として使われうる。
  - 同じ DB と鍵で守る GitHub App の秘密鍵や client secret と、同じ守りである（[秘密の暗号化](../settings/secrets.md)）。
- 端末の cookie を盗まれると、サインアウトするか、リフレッシュトークンが切れるまで、利用者として使われうる。端末そのものが侵害されたときは、守りの外とする。
