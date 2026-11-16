# ノードの詳細を読む

利用者が木のビューアでノードを選んだときに WebUI から、opener や reviewer がノードの根拠を確かめるときに CLI から呼ぶ。

## 呼び出し

- `GET /repositories/{repository}/nodes/{nodeId}`
- operationId：`getNode`

## すること

- 仮説かストーリーの内容と、値・状態を返す。
- 子ノードを返す。
- 有効な検証結果と、そのレポート・artifact・引用を返す。
- このノードの部分木を引用している検証結果を返す。
- コミットを指定すると、その時点の内容を返す。
- open の apply request を指定すると、それを反映したあとの姿を返す（[向き先に重ねる](../../../backend/snapshot.md#向き先に重ねる)）。重ねられなければ、`overlay_conflict`（409）を返し、`detail` に衝突したパスを書く。向き先が `main` でなければ、`overlay_unsupported`（422）を返す。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [検証結果と引用](../../../../docs/tree/verdict.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getNode` に書く。
