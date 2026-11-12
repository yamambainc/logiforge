# 木の概要を読む

利用者は WebUI から、opener と reviewer は CLI から、木の根と根の値、今のリビジョンを確かめるときに呼ぶ。
WebUI のダッシュボードや、CLI で状況をつかむときの最初の問い合わせになる。

## 呼び出し

- `GET /projects/{project}/tree`
- operationId：`getTree`

## すること

- 根の仮説の識別子を返す。
- 根の値（true、false、null）を返す。
- 今のリビジョン（main のコミット）を返す。
- リビジョンを指定すると、その時点の概要を返す。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [真偽値の伝播](../../../../docs/tree/propagation.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getTree` に書く。
