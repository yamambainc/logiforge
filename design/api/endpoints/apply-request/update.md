# apply request を更新する

opener が、request_changes を受けたあとや、反映に失敗したあとに差分を直すときに呼ぶ。
どの client のセッションからでも更新できる。

## 呼び出し

- `PATCH /apply-requests/{applyRequestId}`
- operationId：`updateApplyRequest`

## すること

- 操作をすべて置き換えた、新しい版を作る。
- 新しい版を書いたセッションを記録する。
- それまでの approve は、新しい版には効かない。
- 外の仕組みに通知する。

## 失敗する主な場合

- `not_open`
- `validation_failed`
- `session_required`
- `forbidden`（user から呼んだ）

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [反映の条件](../../../../docs/apply-request/review.md)
- [反映](../../../../docs/apply-request/README.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `updateApplyRequest` に書く。
