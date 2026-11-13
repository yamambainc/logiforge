# apply request を作る

opener が、木や検証結果を変えたブランチを push したあとに、CLI から呼ぶ（[CLI](../../../cli/README.md)）。
ストーリーの作成、inactive 化、検証結果の提出、背景情報への追加は、すべてこれで出す。

## 呼び出し

- `POST /repositories/{repository}/apply-requests`
- operationId：`createApplyRequest`

## すること

- push されたブランチを受け取り、その差分を版 1 として取り込む（[apply request の作成と更新](../../../backend/apply-request.md)）。
- 差分が 4 種類の操作として読めなければ、受け付けない。
- 差分から復元した操作を、応答に入れる。
- 版を取り込んだセッションを、版の記録に書く。
- apply request を open にする。

## 関係する要件

- [apply request](../../../../docs/apply-request/README.md)
- [論理レビュー](../../../../docs/apply-request/review.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createApplyRequest` に書く。
