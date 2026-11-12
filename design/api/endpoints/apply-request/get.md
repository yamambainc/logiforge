# apply request の詳細を読む

opener が、レビューの結果や反映の失敗を確かめるときに呼ぶ。
利用者が、差分と議論を見るときにも使う。

## 呼び出し

- `GET /apply-requests/{applyRequestId}`
- operationId：`getApplyRequest`

## すること

- 今の版の差分を返す。
- 版ごとの、差分を書いたセッションを返す。
- レビューと、それが今の版に有効かを返す。
- 反映の失敗と、その理由を返す。

## 失敗する主な場合

- `not_found`

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [apply request](../../../../docs/apply-request/README.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `getApplyRequest` に書く。
