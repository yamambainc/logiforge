# 木の概要を読む

利用者、opener、reviewer が、木の根と根の値、今のリビジョンを確かめるときに呼ぶ。
WebUI のダッシュボードや、CLI で状況をつかむときの最初の問い合わせになる。

## 呼び出し

- `GET /tree`
- operationId：`getTree`

## すること

- 根の仮説の識別子を返す。
- 根の値（true、false、null）を返す。
- 今のリビジョン（main のコミット）を返す。
- リビジョンを指定すると、その時点の概要を返す。

## 失敗する主な場合

- `not_found`（根がまだない）

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [真偽値の伝播](../../../../docs/tree/propagation.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `getTree` に書く。
