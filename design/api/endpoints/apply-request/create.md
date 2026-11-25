# apply request を作る

opener が、木や検証結果を変えたブランチを push したあとに、CLI から呼ぶ（[CLI](../../../cli/README.md)）。
ストーリーの作成、inactive 化、検証結果の提出、背景情報への追加は、すべてこれで出す。

## 呼び出し

- `POST /repositories/{repository}/apply-requests`
- operationId：`createApplyRequest`

## すること

- push されたブランチから `main` への pull request を作る。ブランチの先頭のコミットが、最初の先頭のコミットになる（[先頭のコミット](../../../backend/apply-request/README.md#先頭のコミット)）。
- 差分が 4 種類の操作として読めなければ、受け付けない。
- 差分から復元した操作と、WebUI で開く URL（`webui_url`）を、応答に入れる。
  - `webui_url` は、apply request を返すほかの応答（1 件の読み、一覧、レビュー、merge など）にも入る。設定に WebUI の URL がなければ null。
  - なぜ：作った人が、レビューする人に URL をそのまま渡せるから。CLI 以外のクライアントも使えるように、サーバで組み立てる。
- apply request を open にする。

## 関係する要件

- [apply request](../../../../docs/apply-request/README.md)
- [論理レビュー](../../../../docs/apply-request/review.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createApplyRequest` に書く。
