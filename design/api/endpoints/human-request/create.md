# 人への依頼を起票する

opener が、計算機の中で検証できない葉について PVR を起票するときに呼ぶ。
opener が、ストーリーが尽きた仮説について、人に判断を仰ぐときにも使う。

## 呼び出し

- `POST /human-requests`
- operationId：`createHumanRequest`

## すること

- 対象のノード、種類、本文（検証手順か問い）を受け取って、依頼を起票する。
- 起票したセッションを記録する。
- 状態は open で始まる。
- 外の仕組みに通知する。

## 関係する要件

- [人への依頼](../../../../docs/human-requests/README.md)
- [仮説が偽になったとき](../../../../docs/tree/falsification.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createHumanRequest` に書く。
