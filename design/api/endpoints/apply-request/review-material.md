# レビューの材料を読む

外の仕組みに起動された reviewer が、論理レビューの最初に、CLI から呼ぶ。
reviewer は、この材料だけを読んで審査することを想定する。

## 呼び出し

- `GET /repositories/{repository}/apply-requests/{applyRequestId}/review-material`
- operationId：`getReviewMaterial`

## すること

- 先頭のコミットの差分を返す。
- 対象ノードの今の値、親仮説から根までの主文、兄弟ストーリーを返す。
  - 対象ノード：`story.create` では親仮説、`story.inactivate` ではそのストーリー、`verdict.submit` と `node.edit` ではその仮説である。
  - 親仮説から根まで：対象ノードの親の仮説から、根までの仮説である。
  - 兄弟ストーリー：`story.create` では親仮説のほかの子ストーリー、`story.inactivate` では同じ親仮説のほかのストーリー、`verdict.submit` と `node.edit` では、対象の仮説が属するストーリーと同じ親仮説を持つ、ほかのストーリーである。
- 引用している部分木（固定した `main` のコミットのもの）と artifact を返す。
- apply request の説明欄は返さない。

## 関係する要件

- [レビューの材料](../../../../docs/apply-request/review.md)
- [論理レビューの審査観点](../../../../docs/apply-request/review-criteria.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getReviewMaterial` に書く。
