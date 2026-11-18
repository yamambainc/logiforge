# サインインの中継

BE の、WebUI のサインインのための要求を実装するときに読む。
サインインの方式と、なぜ BE が中継するかは、[サインイン](auth.md) に書く。

## 要求

BE は、WebUI のサインインのために、次の 2 つの要求を持つ。どちらも認証を求めず、状態を持たない。

| 要求 | すること |
|---|---|
| `GET /v1/oauth/config` | authorize の URL、`client_id`、`redirect_uri`、スコープ、GitForge の種類（`github` か `forgejo`）を返す |
| `POST /v1/oauth/token` | `code` と `code_verifier` を受け取り、client secret を足して GitForge で交換し、アクセストークンと寿命だけを返す |

## 設定の読み方

- BE は、要求ごとに、設定の DB から次のものを読む（[設定](../settings/README.md#設定の-db-に置くもの)）。メモリに保持しない。
  - 構成
  - client_id と client secret（GitHub App のもの、または Forgejo の OAuth2 のアプリケーションのもの）
  - WebUI の URL。`redirect_uri` は、その `/oauth/callback` である
  - GitHub の画面の URL、または Forgejo の URL（BE から、ブラウザから）
  - なぜ：利用者が設定の画面で入れ直した client secret が、次の要求から効くようにするため（[設定を読むとき](../settings/README.md#設定を読むとき)）。
- client secret は、バックエンドの鍵で暗号化してある（[秘密の暗号化](../settings/secrets.md)）。
- Forgejo を使う構成では、authorize の URL を、ブラウザから Forgejo への URL から作る。トークンのエンドポイントは、BE から Forgejo への URL から作る。
  - なぜ 2 つか：BE から Forgejo への URL が、内部の名前で、ブラウザから開けない配置があるから。
- GitHub を使う構成では、どちらも GitHub の画面の URL から作る。
- 構成と、その GitForge の client_id がまだ設定の DB になければ、どちらの要求も `setup_required`（503）を返す。
  - 初回のセットアップのあいだも、それがそろえば、2 つの要求は答える。なぜ：最初の利用者は、セットアップの途中でサインインするから（[初回のセットアップ](../settings/setup.md#流れ)）。

## 開いたままの代理にしない

中継が、開いたままの代理（open proxy）にならないように、次のことを守る。

- 送り先は、設定の DB にある GitForge のトークンのエンドポイントだけである。要求から送り先を受け取らない。
- 受け取るのは `code` と `code_verifier` だけである。`grant_type`（`authorization_code`）、`client_id`、client secret、`redirect_uri` は、BE が設定の DB から付ける。
- `Content-Type: application/json` だけを受け付け、本文は 4 KiB までにする。
- cookie を使わないので、ほかのサイトから呼ばれても、利用者の権限は使われない。JSON だけを受け付けるので、フォームからの送信も通らない。

## GitForge の応答の扱い

- 交換の応答から、`access_token`、`token_type`、`expires_in` だけを返す。`refresh_token` と `refresh_token_expires_in` は返さない（[トークンの持ち方](auth.md#トークンの持ち方)）。
- GitHub には、`Accept: application/json` を付けて交換する。
  - なぜ：付けないと、GitHub は、フォームの形で返すから。
- GitHub は、交換を拒んだときも、200 で `error` を返す。BE は、`error` があれば、拒まれたものとして扱う。
- GitForge が交換を拒んだら（code が正しくない、使い済み、PKCE が合わない）、BE は `unauthenticated`（401）を返す。
- GitForge につながらないとき、または GitForge が 5xx を返したときは、`unavailable`（503）を返す。WebUI は、「しばらくしてから、もう一度サインインしてください」と示す。

## ほかの決まり

- BE は、受け取った code とトークンをログに書かない。
- 詳細は、[openapi.yaml](../api/openapi.yaml) の `getOAuthConfig` と `exchangeOAuthCode` に書く。
- サインインに使うアプリを作り、client_id と client secret を設定の DB に置く手順は、[初回のセットアップ](../settings/setup.md) に書く。
