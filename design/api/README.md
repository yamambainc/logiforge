# API

Logiforge のバックエンドの API を実装するときと、クライアント（CLI、WebUI）を作るときに読む。
各エンドポイントの詳細は、[openapi.yaml](openapi.yaml) が正本である。このページには、全体に共通する決まりだけを書く。

## 想定用途

- CLI は、opener と reviewer がこの API を使うためのクライアントである（[CLI](../../docs/cli/README.md)）。
- WebUI は、利用者がこの API を使うためのクライアントである（[WebUI](../../docs/webui/README.md)）。
- 外の仕組み（ループ、論理レビューの起動）は、[webhook](webhook.md) を受けて、CLI で問い合わせ直す。
- エージェントと外の仕組みは、この API を直接は呼ばない。
  - CLI を通して使う（[レイヤー構成](../architecture.md)）。

API が CLI と WebUI の要件を満たすかは、[クライアントの要件との対応](clients.md) で確かめる。
API ごとの概要は、[API の一覧](endpoints/README.md) にある。

## 主体と認証

- リクエストは、`Authorization: Bearer <トークン>` で主体を示す。
- 主体には、次の 2 種類がある。
  - `user`：WebUI を使う利用者
  - `client`：CLI を使う主体（エージェントか人）
- `user` が使える書き込みは、利用者の 2 つの操作だけである（[利用者](../../docs/roles/README.md)）。
  - 根の主文の作成
  - 人への依頼への回答
    - 回答に添えるファイルの blob と artifact の登録
- それ以外の書き込みは、`client` だけが使える。
- トークンの発行方法は、この API の範囲外である（[目的と範囲](../../docs/concepts/README.md)）。

## セッション ID

- `client` の書き込みには、`Logiforge-Session-Id` ヘッダーを必ず付ける。
  - 値は、クライアントが決める文字列（128 文字以下）である。
  - なぜ：差分を書いたセッションと reviewer のセッションが別であることを確かめるため（[保証の範囲](../../docs/apply-request/review.md)）。
- forge は、セッション ID を apply request、レビュー、イベントに記録する。
- 同じ主体が別のセッション ID を名乗ることは、防がない。

## リビジョン

- 木を読むエンドポイントは、`revision` を指定できる。
  - 指定しなければ、最新のリビジョンを返す。
- 応答には、読んだリビジョンを含める。
- リビジョンは、`main` のコミットの SHA である（[リビジョンとコミット](../files/revisions.md)）。

## エラー

- エラーは、RFC 9457 の `application/problem+json` で返す。
- `code` に、機械で判定できるエラーの種類を入れる。

| `code` | 状態 | 起きるとき |
|---|---|---|
| `validation_failed` | 422 | 文字数や形が正しくない |
| `unauthenticated` | 401 | トークンがない、または正しくない |
| `forbidden` | 403 | 主体の種類が、その操作を使えない |
| `session_required` | 400 | `Logiforge-Session-Id` がない |
| `not_found` | 404 | 対象がない |
| `same_session` | 409 | opener と同じセッションでレビューした |
| `not_open` | 409 | `open` でない apply request を変えようとした |
| `root_exists` | 409 | 根がすでにあるのに、根の主文を作ろうとした |

- 反映の失敗は、エラーにしない。
  - レビューの応答で、apply request が `open` のままであることと、失敗の理由を返す。
  - なぜ：`approve` 自体は受け付けており、失敗は apply request の状態だから（[反映の手順](../backend/merge.md)）。

## 同じ要求の繰り返し

- POST には、`Idempotency-Key` ヘッダーを付けられる。
- 同じキーの POST が 24 時間以内に来たら、最初の応答を返す。
  - なぜ：通信が切れて送り直しても、apply request や人への依頼が二重にできないようにするため。
  - 24 時間は、クライアントが送り直す最も長い間隔として見込んだ値である。

## ページング

- 一覧を返すエンドポイントは、カーソルでページを分ける。
- `limit` の既定は 50、上限は 200 とする。
  - なぜ：1 回の応答の大きさを、数百 KB に収めるため。
- 応答の `next_cursor` が null なら、最後のページである。
- 並びは、作った順（識別子の順）にする。
