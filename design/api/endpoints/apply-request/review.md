# apply request をレビューする

reviewer が、論理レビューの結果を付けるときに呼ぶ。
外の仕組みに起動された reviewer が、レビューの材料を読んだあとに使う。

## 呼び出し

- `POST /apply-requests/{applyRequestId}/reviews`
- operationId：`reviewApplyRequest`

## すること

- approve、request_changes、comment のどれかを付ける。
- レビューした版と、使ったプロンプトのリビジョンを記録する。
- approve で反映の条件を満たしたら、この呼び出しの中で反映する。
- 反映に失敗しても、エラーにはせず、失敗の理由を記録する。

## 関係する要件

- [論理レビュー](../../../../docs/apply-request/review.md)
- [反映](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `reviewApplyRequest` に書く。
