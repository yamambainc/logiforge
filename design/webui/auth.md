# サインイン

WebUI のサインインと、トークンの扱いを実装するときに読む。
BE が主体を確かめる方法は、[認証](../backend/ports/authentication.md) と [Forgejo アダプタの認証](../adapters/forgejo.md#認証) に書く。
スクリプトの注入への備えは、[スクリプトの注入への備えと、ファイルの開き方](security.md) に書く。
authorize から戻るまでの流れは、[サインインの流れ](signin-flow.md) に書く。

## 方式

- Forgejo の OAuth2（認可コードと PKCE）でサインインする。トークンを貼る形は作らない。
- Forgejo には、WebUI を confidential client（client secret を持つクライアント）として登録する。
- トークンの交換は、BE が中継する（[サインインの中継](oauth-relay.md)、[サインインの流れ](signin-flow.md)）。client secret は、BE の設定に置く。
- ブラウザが持つのは、アクセストークンだけである。リフレッシュトークンは、ブラウザに渡さない（[トークンの持ち方](#トークンの持ち方)）。
- client secret の役目は、Forgejo に同意の画面を省かせることだけである。守りにはならない。
  - なぜ守りにならないか：中継があるので、code を持っていれば、だれでも BE を通して交換できるから。
  - 守りは、`redirect_uri` の完全一致、PKCE、`state` である。PKCE と confidential client の組み合わせは、OAuth 2.1 と OAuth の Security BCP が勧める形である。
  - なぜ同意の画面を省かせるか：トークンをメモリだけに持つので、新しいタブ、リロード、共有されたリンク、アクセストークンの期限のたびに authorize に行くから。そのたびに押させないためである。
    - Forgejo は、許可済みの利用者が authorize に来たとき、confidential client なら同意の画面を出さずにすぐ戻す。
    - public client（client secret なし）なら、毎回同意の画面を出す。
- なぜ BE が中継するか：
  - client secret を、ブラウザに渡さずに済むから。
  - WebUI と BE は同じオリジンなので、Forgejo の CORS と、CSP の `connect-src` を広げずに済むから。Forgejo の CORS の設定は、Forgejo の API 全体に効く。
- GitHub を使う構成でも、同じ流れで、Logiforge の OAuth App でサインインする。authorize とトークンのエンドポイントは、`https://github.com/login/oauth/` の下にある（[GitHub アダプタ](../adapters/github.md#認証)）。

## スコープ

- スコープは、`read:user`、`read:organization`、`write:organization`、`write:repository`、`write:issue` にする。
  - `read:user` と `read:organization`：BE が、主体を確かめるため（`GET /user`、`GET /user/teams`。[Forgejo アダプタの認証](../adapters/forgejo.md#認証)）。
  - `write:organization` と `write:repository`：BE が、利用者のトークンで、organization に repository を作り、最初のコミットを置くため（[Forgejo アダプタ](../adapters/forgejo.md) の「repository の用意」）。
  - `write:issue`：BE が、利用者のトークンで、回答の記録のコメントを書き、issue を閉じるため。
  - なぜ書くスコープを求めるか：Logiforge は、書くための自分のアカウントを持たず、要求を送った主体のトークンで書くから（[GitForge アダプタの共通の書き方](../adapters/pull-requests.md)）。
  - そのかわり、盗まれたときに書ける範囲は、利用者が Forgejo で書けるすべての repository に広がる。トークンは、メモリにだけ持ち、短い寿命で使う（[トークンの持ち方](#トークンの持ち方)）。
- GitHub を使う構成では、Logiforge の OAuth App のスコープを、`repo` と `read:org` にする。
  - `repo`：private のリポジトリを読み書きするため。public のリポジトリだけなら `public_repo` でよい。
  - `read:org`：team の所属を確かめるため（[GitHub アダプタ](../adapters/github.md#認証)）。

## トークンの持ち方

- アクセストークンは、メモリ（JavaScript の変数）だけに持つ。`localStorage`、`sessionStorage`、cookie には保存しない。
- リフレッシュトークンは、ブラウザに渡さない。BE は、Forgejo の応答から除いて返し、保持もしない。
  - なぜ：盗まれたときに使える期間を、アクセストークンの寿命（Forgejo の既定で 1 時間）までに収めるため。リフレッシュトークンを盗まれると、更新を続けて、利用者が Forgejo で許可を取り消すまで使われる。
  - なぜタブどうしで困らないか：タブごとに、それぞれ交換したアクセストークンを持つから。Forgejo では、同じ利用者が何度交換しても、先に交換したアクセストークンは使えたままである。
- API を呼ぶたびに、`Authorization: Bearer <アクセストークン>` を付ける。
- 期限ごとに authorize へのページの移動が起きるので、書きかけを失わないようにする。
  - 人への依頼の回答の書きかけは、書くたびに `sessionStorage` に置き、戻ったら戻す（置くものは [人への依頼](screens.md#人への依頼) に書く）。
    - なぜ置いてよいか：トークンではないから。
    - 添付のファイルの中身は置かない。戻ったら、選び直すように示す。
  - 回答を送る直前に、アクセストークンの残りが 5 分より少なければ、先に authorize に行く。

## サインアウト

- サインアウトは、メモリのアクセストークンと、回答の書きかけ（`sessionStorage`）を消し、サインインの画面（「Forgejo でサインイン」のボタンだけ）を出す。
- Forgejo のログインは残る。そのため、新しいタブを開いたり、ボタンを押したりすると、押す操作なしにサインインし直す。
  - 本当に外れるには、Forgejo からログアウトする。サインインの画面に、そのためのリンクを置く。

## 受け入れた危険

- スクリプトが注入されると、そのページが開いているあいだ、メモリのアクセストークンは読まれ、Logiforge の API がその利用者として使われうる（スナップショットツリーを読む、人への依頼に回答する、repository を作る）。
- CSP の `default-src 'self'` は、`fetch` や画像で外のオリジンへ送ることを防ぐ。ただし、ページの移動（`location.href` や `window.open` で外の URL を開く）でトークンを外へ送ることは、止めない（[スクリプトの注入への備え](security.md)）。
- 外へ渡ったときに使えるのは、アクセストークンの寿命（1 時間）までである。リフレッシュトークンはブラウザにないので、それより延びない。
- トークンを保存しないので、ページを閉じたあとに端末から盗まれることはない。
