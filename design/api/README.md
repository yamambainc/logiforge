# API

Logiforge のバックエンドの API を実装するときと、クライアント（CLI、WebUI）を作るときに読む。
各エンドポイントの詳細は、[openapi.yaml](openapi.yaml) が正本である。
このページには、全体に共通する決まりだけを書く。

## 想定用途

- CLI は、opener と reviewer がこの API を使うためのクライアントである（[CLI](../../docs/cli/README.md)）。
- WebUI は、利用者がこの API を使うためのクライアントである（[WebUI](../../docs/webui/README.md)）。
- 外の仕組み（ループ、論理レビューの起動）は、着手できる作業だけを、定期的に読む。
- エージェントと外の仕組みは、通常は CLI からこの API を使う（[レイヤー構成](../architecture.md)）。

API が CLI と WebUI の要件を満たすかは、[クライアントの要件との対応](clients.md) で確かめる。
API ごとの概要は、[API の一覧](endpoints/README.md) にある。

## 認証

- リクエストは、`Authorization: Bearer <トークン>` で主体を示す。
  - トークンは、[GitForge](../README.md#用語) が発行したものである（[アダプタ](../adapters/README.md)）。
- 主体の確かめ方は、[認証](../backend/ports/authentication.md) に書く。
- どの主体がどの操作を使えるかは、[認可](../backend/README.md#認可) に書く。
- この API は、トークンを発行しない。
  - WebUI のために、Forgejo の OAuth2 の交換を中継するだけである（[サインインの中継](../webui/oauth-relay.md)）。
- ヘルスチェックの `GET /healthz` だけは、`/v1` の外に置き、認証を求めない（[ヘルスチェック](../deploy/README.md#ヘルスチェック)）。
- WebUI のサインインの中継（`/v1/oauth/` の下の 2 つ）も、認証を求めない。
  - なぜ：サインインの前に呼ぶから（[サインインの中継](../webui/oauth-relay.md)）。

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
- バックエンドは、ヘッダーのセッション ID を、レビューと close と人への依頼に記録する。
- 先頭のコミットを書いたセッションは、ヘッダーではなく、そのコミットの trailer の自己申告である（[セッション](../files/revisions.md#セッション)）。
- 同じ主体が別のセッション ID を名乗ることは、防がない。

## コミット

- 木を読むエンドポイントは、`commit` でコミットの SHA を指定できる。
  - 指定できるのは、`main` のコミットか、worker がスナップショットを作ったブランチのコミットである。
  - ブランチのコミットのスナップショットツリーは、読むたびに作る（[ブランチのコミットのスナップショットツリー](../backend/snapshot/branch.md#ブランチのコミットのスナップショットツリー)）。
  - 指定しなければ、今の `main` のコミットを返す。
- 応答には、読んだコミット（`commit`）を含める。
- コミットは、不透明な文字列である（[main のコミットと記録](../files/revisions.md)）。

## 反映したあとの木

- 木の概要、木のノード、ノードの詳細は、`apply_request` で open の apply request を指定できる。
  - その apply request を反映したあとの、向き先に重ねたスナップショットツリーを返す（[向き先に重ねる](../backend/snapshot/branch.md#向き先に重ねる)）。
  - `commit` と同時には指定できない。
- 向き先が `main` で、重ねられなければ、`overlay_conflict`（409）を返す。
  - `detail` に、衝突したパスを書く。
- 向き先が `main` でなければ、向き先のブランチの今の先頭に重ねる。
  - 衝突は判定しない。検査もしないので、値が決まりに沿っているとは限らない。
  - 向き先のブランチが消えていれば、`not_found`（404）を返す。
- その識別子の apply request がこの repository になければ、`not_found`（404）を返す。
  - ほかの repository の apply request も、これにあたる。
- 反映すると消えるノードを指定したら、`not_found`（404）を返す。
  - なぜ：反映したあとの木に、そのノードがないから。

## エラー

- エラーは、RFC 9457（HTTP API のエラーの形式の規格）の `application/problem+json` で返す。
- `code` に、機械で判定できるエラーの種類を入れる。
- エラーの種類と意味は、[openapi.yaml](openapi.yaml) の `Problem` が正本である。

- 反映の失敗は、エラーにしない。
  - レビューの応答で、apply request が `open` のままであることと、失敗の理由を返す。
  - なぜ：`approve` 自体は受け付けており、失敗は apply request の状態だから（[反映の手順](../backend/merge.md)）。

## アップロードの大きさ

- apply request の差分のファイルは、git のブランチで push する。
  - API では送らない（[artifact の置き方](../files/artifacts.md)）。
- 人への依頼への回答の添付は、回答の要求に、base64（バイナリを文字列にする符号化）で含めて送る。
  - なぜ：利用者は WebUI から回答し、git を使わないから。
- 1 回の要求の本文は、32 MiB までにする。
  - なぜ：上限（10 MiB、[artifact](../../docs/tree/verdict.md#artifact)）のファイルを base64（約 1.33 倍）にして 2 つ送れ、1 回の HTTP の要求で途中で切れずに送り切れる大きさだから。
  - それより多くのファイルは、リンクの artifact にする。

## ほかの決まり

- 書き込み：書いた直後の読みと、同じ要求の繰り返し（[書き込みの決まり](writes.md)）
- 一覧：カーソルと並び（[ページング](paging.md)）
- 書き出し：木を書き出す API の JSON と Markdown の形（[書き出しの形](export.md)）
