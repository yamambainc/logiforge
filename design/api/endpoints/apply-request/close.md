# apply request を close する

opener が取り下げるとき、または reviewer が却下するときに、CLI から呼ぶ。

## 呼び出し

- `POST /repositories/{repository}/apply-requests/{applyRequestId}/close`
- operationId：`closeApplyRequest`

## すること

- 理由を付けて、apply request を closed にする。
- 差分を書いたいずれかのセッションからなら、取り下げとして扱う。
- それ以外のセッションからなら、却下として扱う。
- 外の仕組みに通知する。

## 関係する要件

- [状態](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `closeApplyRequest` に書く。
