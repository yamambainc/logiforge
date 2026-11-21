# apply request を close する

opener が取り下げるとき、または reviewer が却下するときに、CLI から呼ぶ。

## 呼び出し

- `POST /repositories/{repository}/apply-requests/{applyRequestId}/close`
- operationId：`closeApplyRequest`

## すること

- 理由を付けて、apply request を closed にする。理由は、close する主体のトークンで書く pull request のコメントに、人が読む文で残す（[close のコメント](../../../adapters/texts.md#close-のコメント)）。
- apply request を作った人からなら、取り下げ、ほかの人からなら、却下である（[状態](../../../../docs/apply-request/README.md)）。

## 関係する要件

- [状態](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `closeApplyRequest` に書く。
