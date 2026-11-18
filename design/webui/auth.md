# サインイン

WebUI のサインインと、トークンの扱いを実装するときに読む。
BE が主体を確かめる方法は、[認証](../backend/ports/authentication.md)、[GitHub アダプタの認証](../adapters/github.md#認証)、[Forgejo アダプタの認証](../adapters/forgejo.md#認証) に書く。
スクリプトの注入への備えは、[スクリプトの注入への備えと、ファイルの開き方](security.md) に書く。
authorize から戻るまでの流れは、[サインインの流れ](signin-flow.md) に書く。

## 方式

- GitForge の OAuth2（認可コードと PKCE）でサインインする。トークンを貼る形は作らない。
  - PKCE の `code_challenge_method` は、どちらの構成も `S256` にする。GitHub は `plain` を受け付けない。
- サインインに使うアプリは、構成で決まる。

| 構成 | サインインに使うアプリ | 作る人 |
|---|---|---|
| GitHub を使う | Logiforge の GitHub App（[GitHub App とトークン](../adapters/github-app.md)） | 管理者が、初回のセットアップで manifest flow で作る（[初回のセットアップ](../settings/setup.md#github-を使う構成app-を-manifest-flow-で作る)） |
| Forgejo を使う | Forgejo の OAuth2 のアプリケーション（confidential client） | 管理者が、Forgejo で作る（[初回のセットアップ](../settings/setup.md#forgejo-を使う構成)） |

- authorize とトークンのエンドポイントは、次のとおり。
  - GitHub：GitHub の画面の URL（既定は `https://github.com`）の `/login/oauth/authorize` と `/login/oauth/access_token`
  - Forgejo：Forgejo の URL の `/login/oauth/authorize` と `/login/oauth/access_token`
- client_id、client secret、`redirect_uri` の元の WebUI の URL、GitForge の URL は、BE が要求ごとに設定の DB から読む（[設定](../settings/README.md#設定の-db-に置くもの)）。
- トークンの交換は、BE が中継する（[サインインの中継](oauth-relay.md)、[サインインの流れ](signin-flow.md)）。
- ブラウザが持つのは、アクセストークンだけである。リフレッシュトークンは、ブラウザに渡さない（[トークンの持ち方](#トークンの持ち方)）。

## client secret の役目

- client secret は、守りにはならない。
  - なぜ：中継があるので、code を持っていれば、だれでも BE を通して交換できるから。
  - 守りは、`redirect_uri` の完全一致、PKCE、`state` である。PKCE と confidential client の組み合わせは、OAuth 2.1 と OAuth の Security BCP が勧める形である。
- client secret が要る理由は、構成で違う。
  - GitHub：GitHub は、交換に client secret を必ず求める。
  - Forgejo：Forgejo に、同意の画面を省かせるため。Forgejo は、許可済みの利用者が authorize に来たとき、confidential client なら同意の画面を出さずにすぐ戻す。public client なら、毎回同意の画面を出す。
- どちらの構成でも、許可済みの利用者には、同意の画面が出ない。
  - なぜ要るか：トークンをメモリだけに持つので、新しいタブ、リロード、共有されたリンク、アクセストークンの期限のたびに authorize に行くから。そのたびに押させないためである。
- なぜ BE が中継するか：
  - client secret を、ブラウザに渡さずに済むから。
  - WebUI と BE は同じオリジンなので、GitForge の CORS と、CSP の `connect-src` を広げずに済むから。Forgejo の CORS の設定は、Forgejo の API 全体に効く。GitHub のトークンのエンドポイントは、ブラウザからの要求に CORS で応じない。

## スコープ

### GitHub を使う構成

- GitHub App のトークンの権限は、スコープでは決まらない。App の権限と、その人のリポジトリの権限の、両方にあるものになる（[GitHub App とトークン](../adapters/github-app.md#アカウントとトークン)）。
- authorize の `scope` には、`offline_access` だけを付ける。
  - なぜ：App の設定によらず、user access token が 8 時間で切れるようにするため。GitHub は、`offline_access` を付けると、期限つきのトークンとリフレッシュトークンを返す。リフレッシュトークンは、BE が除く。
- 盗まれたときに書ける範囲は、App が install されていて、その人が書けるリポジトリの、App の権限の中である。

### Forgejo を使う構成

- スコープは、`read:user`、`read:organization`、`write:organization`、`write:repository`、`write:issue` にする。
  - `read:user` と `read:organization`：BE が、主体を確かめるため（`GET /user`、`GET /user/teams`。[Forgejo アダプタの認証](../adapters/forgejo.md#認証)）。
  - `write:organization` と `write:repository`：BE が、利用者のトークンで、organization に repository を作り、最初のコミットを置くため（[Forgejo アダプタ](../adapters/forgejo.md) の「repository の用意」）。
  - `write:issue`：BE が、利用者のトークンで、回答の記録のコメントを書き、issue を閉じるため。
- 盗まれたときに書ける範囲は、利用者が Forgejo で書けるすべての repository に広がる。

どちらの構成でも、書く権限を求める。なぜ：Logiforge は、書くための自分のアカウントを持たず、要求を送った主体のトークンで書くから（[GitForge アダプタの共通の書き方](../adapters/pull-requests.md)）。そのかわり、トークンは、メモリにだけ持ち、短い寿命で使う（[トークンの持ち方](#トークンの持ち方)）。

## トークンの持ち方

- アクセストークンは、メモリ（JavaScript の変数）だけに持つ。`localStorage`、`sessionStorage`、cookie には保存しない。
- アクセストークンの寿命は、構成で決まる。
  - GitHub：8 時間（[user access token](../adapters/github-app.md#user-access-token)）
  - Forgejo：1 時間（Forgejo の既定）
- リフレッシュトークンは、ブラウザに渡さない。BE は、GitForge の応答から除いて返し、保持もしない。
  - なぜ：盗まれたときに使える期間を、アクセストークンの寿命までに収めるため。リフレッシュトークンを盗まれると、更新を続けて、利用者が GitForge で許可を取り消すまで使われる。
- タブごとに、それぞれ交換したアクセストークンを持つ。
  - Forgejo では、同じ利用者が何度交換しても、先に交換したアクセストークンは使えたままである。
  - GitHub は、同じ利用者とアプリの組のトークンが多すぎると、古いものを取り消すことがある。取り消されたタブは、API の `unauthenticated` で authorize に行き直す（[サインインの流れ](signin-flow.md#authorize-とサインインの画面)）。
- API を呼ぶたびに、`Authorization: Bearer <アクセストークン>` を付ける。
- 期限ごとに authorize へのページの移動が起きるので、書きかけを失わないようにする。
  - 人への依頼の回答の書きかけは、書くたびに `sessionStorage` に置き、戻ったら戻す（置くものは [人への依頼](screens.md#人への依頼) に書く）。
    - なぜ置いてよいか：トークンではないから。
    - 添付のファイルの中身は置かない。戻ったら、選び直すように示す。
  - 回答を送る直前に、アクセストークンの残りが 5 分より少なければ、先に authorize に行く。

## サインアウト

- サインアウトは、メモリのアクセストークンと、回答の書きかけ（`sessionStorage`）を消し、サインインの画面を出す。
  - サインインの画面には、構成に合わせて「GitHub でサインイン」か「Forgejo でサインイン」のボタンだけを置く。構成は、`getOAuthConfig` の `gitforge` で知る。
- GitForge のログインは残る。そのため、新しいタブを開いたり、ボタンを押したりすると、押す操作なしにサインインし直す。
  - 本当に外れるには、GitForge からログアウトする。サインインの画面に、そのためのリンクを置く。

## 受け入れた危険

- スクリプトが注入されると、そのページが開いているあいだ、メモリのアクセストークンは読まれ、Logiforge の API がその利用者として使われうる（スナップショットツリーを読む、人への依頼に回答する、repository を作る）。
  - 管理者のトークンなら、設定の API も使われうる（[管理者を確かめる](../settings/admins.md#管理者を確かめる)）。
- CSP の `default-src 'self'` は、`fetch` や画像で外のオリジンへ送ることを防ぐ。ただし、ページの移動（`location.href` や `window.open` で外の URL を開く）でトークンを外へ送ることは、止めない（[スクリプトの注入への備え](security.md)）。
- 外へ渡ったときに使えるのは、アクセストークンの寿命（GitHub は 8 時間、Forgejo は 1 時間）までである。リフレッシュトークンはブラウザにないので、それより延びない。
- トークンを保存しないので、ページを閉じたあとに端末から盗まれることはない。
