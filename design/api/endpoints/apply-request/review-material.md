# レビューの材料を読む

外の仕組みに起動された reviewer が、論理レビューの最初に、CLI から呼ぶ。
reviewer は、この材料だけを読んで審査することを想定する。

## 呼び出し

- `GET /repositories/{repository}/apply-requests/{applyRequestId}/review-material`
- operationId：`getReviewMaterial`

## すること

- [レビューの材料](../../../../docs/apply-request/review.md#レビューの材料) にあるものを返す。
  - 対象ノードと兄弟ストーリーの決め方も、そのページに従う。
- 差分は、先頭のコミットのものを返す。

## 関係する要件

- [レビューの材料](../../../../docs/apply-request/review.md)
- [論理レビューの審査観点](../../../../docs/apply-request/review-criteria.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getReviewMaterial` に書く。
