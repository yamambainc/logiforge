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
- open の apply request を指定すると、それを反映したあとの姿を返す（[向き先に重ねる](../../../backend/snapshot.md#向き先に重ねる)）。
  - 重ねられなければ、`overlay_conflict`（409）を返し、`detail` に衝突したパスを書く。
  - 向き先が `main` でなければ、`overlay_unsupported`（422）を返す。
  - その識別子の apply request がこの repository になければ（ほかの repository のものを含む）、`not_found`（404）を返す。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [真偽値の伝播](../../../../docs/tree/propagation.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getTree` に書く。
