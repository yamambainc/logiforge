# API

Logiforge のバックエンドの API を実装するときと、クライアント（CLI、WebUI）を作るときに読む。
各エンドポイントの詳細は、[openapi.yaml](openapi.yaml) が正本である。このページには、全体に共通する決まりだけを書く。

## 想定用途

- CLI は、opener と reviewer がこの API を使うためのクライアントである（[CLI](../../docs/cli/README.md)）。
- WebUI は、利用者がこの API を使うためのクライアントである（[WebUI](../../docs/webui/README.md)）。
- 外の仕組み（ループ、論理レビューの起動）は、着手できる作業だけを、定期的に読む。
- エージェントと外の仕組みは、通常は CLI からこの API を使う（[レイヤー構成](../architecture.md)）。

API が CLI と WebUI の要件を満たすかは、[クライアントの要件との対応](clients.md) で確かめる。
API ごとの概要は、[API の一覧](endpoints/README.md) にある。

## 認証

- リクエストは、`Authorization: Bearer <トークン>` で主体を示す。
  - トークンの発行元は、構成による（[GitHub アダプタ](../adapters/github.md)、[独自フォージのアダプタ](../adapters/forge.md)）。
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
- バックエンドは、セッション ID を版とレビューに記録する。
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

- apply request の差分のファイルは、git のブランチで push する。API では送らない（[artifact の置き方](../files/artifacts.md)）。
- 人への依頼への回答の添付は、回答の要求に、base64（バイナリを文字列にする符号化）で含めて送る。
  - なぜ：利用者は WebUI から回答し、git を使わないから。
- 1 回の要求の本文は、32 MiB までにする。
  - なぜ：上限（10 MiB）のファイルを base64（約 1.33 倍）にして 2 つ送れ、1 回の HTTP の要求で途中で切れずに送り切れる大きさだから。
  - それより多くのファイルは、リンクの artifact にする。

## 書き込み

- 書いた直後の読みと、同じ要求の繰り返しの決まりは、[書き込みの決まり](writes.md) に書く。

## ページング

- 一覧を返すエンドポイントは、カーソルでページを分ける。
- `limit` の既定は 50、上限は 200 とする。
  - なぜ：1 回の応答の大きさを、数百 KB に収めるため。
- 応答の `next_cursor` が null なら、最後のページである。
- 並びは、一覧ごとに次のとおりにする。

| 一覧 | 並び |
|---|---|
| repository | 名前の順 |
| apply request、人への依頼 | 作った順 |
| 着手できる作業 | 作業の種類の順、次にノードの識別子の順、次に `subject_id` の順 |
| ノードの履歴 | 新しい順 |

- 作った順は、識別子の順ではない。`Idempotency-Key` から作った識別子は、作った順に並ばないから。
  - カーソルには、プロバイダの連番を使う（[テーブル](../forge/tables.md)、[GitHub アダプタ](../adapters/github.md)）。
- ノードの履歴のカーソルは、次に読むコミットの番号である。今の `main` のスナップショットの、ノードの変更の一覧から続きを返す（[スナップショット](../backend/snapshot.md)）。
- 着手できる作業のカーソルは、応答の `revision` に結びつける。
  - なぜ：作業は、`main` のリビジョンから計算するので、ページの間に `main` が進むと並びがずれるから。
  - カーソルのリビジョンが今の `main` と違えば、そのリビジョンで計算し直して続きを返す。
  - やりとりの側（apply request と人への依頼）は、今のデータを使う。そのため、ページの間に変わった項目は、増えたり減ったりしうる。
