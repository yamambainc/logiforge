# apply request をレビューする

reviewer が、論理レビューの結果を付けるときに、CLI か WebUI から呼ぶ。
外の仕組みに起動された reviewer は、レビューの材料を読んだあとに、CLI から使う。利用者は、apply request と履歴の画面から使う（[WebUI からのレビュー](../../../settings/users.md#webui-からのレビュー)）。

## 呼び出し

- `POST /repositories/{repository}/apply-requests/{applyRequestId}/reviews`
- operationId：`reviewApplyRequest`

## すること

- approve、request_changes、comment のどれかの review を、呼んだ主体のトークンで、GitForge の pull request に出す。
- review は、レビューした先頭のコミットに出す。client の review では、本文の最後に、使ったプロンプトのリビジョンを、人が読む文で書く（[review の本文](../../../adapters/texts.md#review-の本文)）。
  - `user`（WebUI）のレビューには、プロンプトのリビジョンがない。書かない。
- 呼んだ主体が、その pull request の作者なら、GitForge が approve を拒む。opener と reviewer は、別の GitForge のアカウントである。
- `client` も `user` も呼べる。`user` は、その repository に Write があるときだけ受け付ける。
- approve で反映の条件を満たしたら、この呼び出しの中で反映する。
- 反映に失敗しても、エラーにはせず、失敗の理由を head のコミットの commit status と、pull request のコメントに書く（[反映の手順](../../../backend/merge.md)）。

## 関係する要件

- [論理レビュー](../../../../docs/apply-request/review.md)
- [反映](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `reviewApplyRequest` に書く。
