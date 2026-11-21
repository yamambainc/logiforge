# 人への依頼を起票する

opener が、計算機の中で検証できない葉について PVR を起票するときに、CLI から呼ぶ。
opener が、ストーリーが尽きた仮説について、人に判断を仰ぐときにも、CLI から使う。

## 呼び出し

- `POST /repositories/{repository}/human-requests`
- operationId：`createHumanRequest`

## すること

- 対象のノード、種類、本文（検証手順か問い）を受け取って、依頼を起票する。
- 呼んだ主体のトークンで、GitForge に issue を作る。依頼の識別子は、その issue の番号である。
- issue のタイトルは、対象ノードの識別子で始める（[人への依頼の issue](../../../adapters/texts.md#人への依頼の-issue)）。
- 状態は open で始まる。

## 関係する要件

- [人への依頼](../../../../docs/human-requests/README.md)
- [仮説が偽になったとき](../../../../docs/tree/falsification.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createHumanRequest` に書く。
