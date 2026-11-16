# サインインの中継

BE の、WebUI のサインインのための要求を実装するときに読む。
サインインの流れと、なぜ BE が中継するかは、[サインイン](auth.md) に書く。

## 要求

BE は、WebUI のサインインのために、次の 2 つを足す。どちらも認証を求めず、状態を持たない。

| 要求 | すること |
|---|---|
| `GET /v1/oauth/config` | authorize の URL、`client_id`、`redirect_uri`、スコープを返す |
| `POST /v1/oauth/token` | `code` と `code_verifier` を受け取り、client secret を足して Forgejo で交換し、アクセストークンと寿命だけを返す |

## 開いたままの代理にしない

中継が、開いたままの代理（open proxy）にならないように、次のことを守る。

- 送り先は、設定した Forgejo のトークンのエンドポイントだけである。要求から送り先を受け取らない。
- 受け取るのは `code` と `code_verifier` だけである。`grant_type`（`authorization_code`）、`client_id`、client secret、`redirect_uri` は、BE の設定から付ける。
- `Content-Type: application/json` だけを受け付け、本文は 4 KiB までにする。
- cookie を使わないので、ほかのサイトから呼ばれても、利用者の権限は使われない。JSON だけを受け付けるので、フォームからの送信も通らない。

## ほかの決まり

- Forgejo が交換を拒んだら（code が正しくない、使い済み、PKCE が合わない）、BE は `unauthenticated`（401）を返す。
- Forgejo につながらないとき、または Forgejo が 5xx を返したときは、`unavailable`（503）を返す。WebUI は、「しばらくしてから、もう一度サインインしてください」と示す。
- authorize の URL は、ブラウザから Forgejo への URL（`LOGIFORGE_FORGEJO_PUBLIC_URL`。既定は `LOGIFORGE_FORGEJO_URL`）から作る。トークンのエンドポイントは、BE から Forgejo への URL から作る（[環境変数と秘密](../deploy.md#環境変数と秘密)）。
- BE は、受け取った code とトークンをログに書かない。
- 詳細は、[openapi.yaml](../api/openapi.yaml) の `getOAuthConfig` と `exchangeOAuthCode` に書く。
- `client_id`、client secret、`redirect_uri` は、環境変数で渡す（[環境変数と秘密](../deploy.md#環境変数と秘密)）。
- OAuth2 のアプリケーションは、管理者が Forgejo で登録する（confidential client）。
