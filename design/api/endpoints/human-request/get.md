# 人への依頼を読む

利用者が回答する前に依頼を読むとき、opener が回答を検証結果に変える前に読むときに呼ぶ。

## 呼び出し

- `GET /human-requests/{humanRequestId}`
- operationId：`getHumanRequest`

## すること

- 対象のノード、種類、本文、状態を返す。
- 回答済みなら、回答の本文と、回答を登録した artifact を返す。

## 関係する要件

- [人への依頼](../../../../docs/human-requests/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getHumanRequest` に書く。
