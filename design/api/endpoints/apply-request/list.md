# apply request の一覧を読む

利用者が、審査中や反映済みの apply request を見るときに呼ぶ。
opener や reviewer が、自分の関わる apply request を探すときにも使う。

## 呼び出し

- `GET /apply-requests`
- operationId：`listApplyRequests`

## すること

- apply request を、状態（open、merged、closed）で絞り込んで返す。
- 各 apply request の、レビュー待ちか修正待ちかを返す。
- 一覧はページに分けて返す。

## 関係する要件

- [apply request](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listApplyRequests` に書く。
