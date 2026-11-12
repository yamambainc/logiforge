# apply request を作る

opener が、木や検証結果を変えたいときに、CLI から呼ぶ。
ストーリーの作成、inactive 化、検証結果の提出、背景情報への追加は、すべてこれで出す。

## 呼び出し

- `POST /repositories/{repository}/apply-requests`
- operationId：`createApplyRequest`

## すること

- 操作を並べた差分を受け取り、版 1 とする。
- 差分を書いたセッションを記録する。
- apply request を open にし、イベントを記録する。

## 関係する要件

- [apply request](../../../../docs/apply-request/README.md)
- [論理レビュー](../../../../docs/apply-request/review.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createApplyRequest` に書く。
