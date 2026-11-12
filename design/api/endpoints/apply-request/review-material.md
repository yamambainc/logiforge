# レビューの材料を読む

外の仕組みに起動された reviewer が、論理レビューの最初に、CLI から呼ぶ。
reviewer は、この材料だけを読んで審査することを想定する。

## 呼び出し

- `GET /apply-requests/{applyRequestId}/review-material`
- operationId：`getReviewMaterial`

## すること

- 今の版の差分を返す。
- 対象ノードの今の値、親仮説から根までの主文、兄弟ストーリーを返す。
- 引用している部分木（固定したリビジョンのもの）と artifact を返す。
- apply request の説明欄は返さない。

## 関係する要件

- [レビューの材料](../../../../docs/apply-request/review.md)
- [論理レビューの審査観点](../../../../docs/apply-request/review-criteria.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getReviewMaterial` に書く。
