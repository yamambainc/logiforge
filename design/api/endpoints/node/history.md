# ノードの履歴を読む

利用者が、あるノードがどう変わってきたかを確かめるときに、WebUI から呼ぶ。

## 呼び出し

- `GET /repositories/{repository}/nodes/{nodeId}/history`
- operationId：`getNodeHistory`

## すること

- そのノードのパスを変えた `main` のコミットを、新しい順に返す。
- コミットの署名と trailer から、誰が、いつ、どの apply request で変えたかを返す。
- 一覧はページに分けて返す。

## 関係する要件

- [監査](../../../../docs/architecture/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getNodeHistory` に書く。
