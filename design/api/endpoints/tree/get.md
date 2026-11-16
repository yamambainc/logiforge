# 木の概要を読む

利用者は WebUI から、opener と reviewer は CLI から、木の根と根の値、今の `main` のコミットを確かめるときに呼ぶ。
WebUI のダッシュボードや、CLI で状況をつかむときの最初の問い合わせになる。

## 呼び出し

- `GET /repositories/{repository}/tree`
- operationId：`getTree`

## すること

- 根の仮説の識別子を返す。
- 根の値（true、false、null）を返す。
- 今の `main` のコミットを返す。
- コミットを指定すると、その時点の概要を返す。
- open の apply request を指定すると、それを反映したあとの姿を返す（[向き先に重ねる](../../../backend/snapshot.md#向き先に重ねる)）。重ねられなければ、衝突したパスを返す。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [真偽値の伝播](../../../../docs/tree/propagation.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getTree` に書く。
