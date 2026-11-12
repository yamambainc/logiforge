# 人への依頼の一覧を読む

利用者が、回答する人への依頼を探すときに呼ぶ。
opener が、回答済みの依頼を探すときにも使う。

## 呼び出し

- `GET /human-requests`
- operationId：`listHumanRequests`

## すること

- 種類（pvr、decision）と状態（open、answered）で絞り込んで返す。
- 対象のノードで絞り込める。
- 一覧はページに分けて返す。

## 関係する要件

- [人への依頼](../../../../docs/human-requests/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listHumanRequests` に書く。
