# API

Logiforge のバックエンドの API を実装するときと、クライアント（CLI、WebUI）を作るときに読む。
各エンドポイントの詳細は、[openapi.yaml](openapi.yaml) が正本である。このページには、全体に共通する決まりだけを書く。

## 想定用途

- CLI は、opener と reviewer がこの API を使うためのクライアントである（[CLI](../../docs/cli/README.md)）。
- WebUI は、利用者がこの API を使うためのクライアントである（[WebUI](../../docs/webui/README.md)）。
- 外の仕組み（ループ、論理レビューの起動）は、着手できる作業とイベントを、定期的に読む。
- エージェントと外の仕組みは、通常は CLI からこの API を使う（[レイヤー構成](../architecture.md)）。

API が CLI と WebUI の要件を満たすかは、[クライアントの要件との対応](clients.md) で確かめる。
API ごとの概要は、[API の一覧](endpoints/README.md) にある。

## 認証

- リクエストは、`Authorization: Bearer <トークン>` で主体を示す。
  - GitHub を使う構成では、GitHub のトークンを使う。
  - GitHub を使わない構成では、サーバが発行したトークンを使う。
- 主体の確かめ方は、[認証](../backend/ports/authentication.md) に書く。
- どの主体がどの操作を使えるかは、[認可](../backend/README.md) に書く。
- トークンの発行方法は、この API の範囲外である（[目的と範囲](../../docs/concepts/README.md)）。

## repository

- repository の一覧と作成のほかは、すべての API を `/repositories/{repository}/` の下に置く。
  - なぜ：1 つのバックエンドが、複数の repository を扱うから（[木の形](../../docs/tree/README.md)）。
- `{repository}` は、repository の名前である。
  - 名前は、小文字の英数字とハイフンで、63 文字以下にする。
  - なぜ：URL のパスに、そのまま入れられるようにするため。

## セッション ID

- `client` の書き込みには、`Logiforge-Session-Id` ヘッダーを必ず付ける。
  - 値は、クライアントが決める文字列（128 文字以下）である。
  - なぜ：差分を書いたセッションと reviewer のセッションが別であることを確かめるため（[保証の範囲](../../docs/apply-request/review.md)）。
- バックエンドは、セッション ID を版、レビュー、イベントに記録する。
- 同じ主体が別のセッション ID を名乗ることは、防がない。

## リビジョン

- 木を読むエンドポイントは、`revision` を指定できる。
  - 指定しなければ、最新のリビジョンを返す。
- 応答には、読んだリビジョンを含める。
- リビジョンは、不透明な文字列である（[リビジョンとコミット](../files/revisions.md)）。

## エラー

- エラーは、RFC 9457（HTTP API のエラーの形式の規格）の `application/problem+json` で返す。
- `code` に、機械で判定できるエラーの種類を入れる。

- エラーの種類と意味は、[openapi.yaml](openapi.yaml) の `Problem` が正本である。

- 反映の失敗は、エラーにしない。
  - レビューの応答で、apply request が `open` のままであることと、失敗の理由を返す。
  - なぜ：`approve` 自体は受け付けており、失敗は apply request の状態だから（[反映の手順](../backend/merge.md)）。

## アップロードの大きさ

- ファイルの中身は、`verdict.submit` の操作と回答の要求に、base64（バイナリを文字列にする符号化）で含めて送る。
  - なぜ：アップロードの API と置き場所を、別に持たずに済むから（[artifact の置き方](../files/artifacts.md)）。
- 1 回の要求の本文は、32 MiB までにする。
  - なぜ：上限（10 MiB）のファイルを base64（約 1.33 倍）にして 2 つ送れ、1 回の HTTP の要求で途中で切れずに送り切れる大きさだから。
  - それより多くのファイルは、apply request を分けて出す。

## 同じ要求の繰り返し

- POST には、`Idempotency-Key` ヘッダーを付けられる。
- 同じキーの POST が 24 時間以内に来たら、最初の応答を返す。
  - キーと最初の応答は、[サーバの状態](../backend/ports/server-state.md) に置く。
  - サーバの状態を作り直したあとは、それより前の要求を見分けられない。
  - なぜ：通信が切れて送り直しても、apply request や人への依頼が二重にできないようにするため。
  - 24 時間は、クライアントが送り直す最も長い間隔として見込んだ値である。

## ページング

- 一覧を返すエンドポイントは、カーソルでページを分ける。
- `limit` の既定は 50、上限は 200 とする。
  - なぜ：1 回の応答の大きさを、数百 KB に収めるため。
- 応答の `next_cursor` が null なら、最後のページである。
- 並びは、作った順（識別子の順）にする。
