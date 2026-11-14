# apply request の詳細を読む

opener が、レビューの結果や反映の失敗を確かめるときに、CLI から呼ぶ。
利用者が、差分と議論を見るときにも、WebUI から使う。

## 呼び出し

- `GET /repositories/{repository}/apply-requests/{applyRequestId}`
- operationId：`getApplyRequest`

## すること

- 先頭のコミットの差分を返す。
- push された先頭のコミットの一覧を返す。各項目は、worker がそのコミットを見た時刻（`seen_at`）と、コミットを書いたセッションを持つ。
- レビューと、それが今の先頭のコミットに有効かを返す。
- 反映の失敗と、その理由を返す。

## 関係する要件

- [apply request](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getApplyRequest` に書く。
