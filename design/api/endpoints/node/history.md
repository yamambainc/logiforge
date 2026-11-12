# ノードの履歴を読む

利用者が、あるノードがどう変わってきたかを確かめるときに、WebUI から呼ぶ。

## 呼び出し

- `GET /nodes/{nodeId}/history`
- operationId：`getNodeHistory`

## すること

- そのノードに関わるイベントを、起きた順に返す。
- 誰が、いつ、どの apply request で変えたかがわかる。
- 一覧はページに分けて返す。

## 関係する要件

- [監査](../../../../docs/architecture/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getNodeHistory` に書く。
