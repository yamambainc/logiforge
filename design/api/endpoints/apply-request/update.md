# apply request を更新する

opener が、apply request の説明欄を書き換えるときに、CLI から呼ぶ。
apply request の先頭のコミットを変えるには、ブランチに push するだけで、この API は使わない（[先頭のコミット](../../../backend/apply-request.md#先頭のコミット)）。

## 呼び出し

- `PATCH /repositories/{repository}/apply-requests/{applyRequestId}`
- operationId：`updateApplyRequest`

## すること

- 説明欄を書き換える。

## 関係する要件

- [反映の条件](../../../../docs/apply-request/review.md)
- [反映](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `updateApplyRequest` に書く。
