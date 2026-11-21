# apply request をレビューする

reviewer が、論理レビューの結果を付けるときに、CLI か WebUI から呼ぶ。
外の仕組みに起動された reviewer は、レビューの材料を読んだあとに、CLI から使う。利用者は、apply request と履歴の画面から使う（[WebUI からのレビュー](../../../settings/users.md#webui-からのレビュー)）。

## 呼び出し

- `POST /repositories/{repository}/apply-requests/{applyRequestId}/reviews`
- operationId：`reviewApplyRequest`

## すること

- approve、request_changes、comment のどれかの review を、呼んだ主体のトークンで、GitForge の pull request に出す。
- comment と request_changes には、本文が要る。GitHub も Forgejo も、本文のない comment と request changes の review を拒むので、本文が空なら、GitForge に送らずに validation_failed で断る。approve は、本文がなくてよい。
- review は、レビューした先頭のコミットに出す。client の review では、本文の最後に、使ったプロンプトのリビジョンを、人が読む文で書く（[review の本文](../../../adapters/texts.md#review-の本文)）。
  - `user`（WebUI）のレビューには、プロンプトのリビジョンがない。書かない。
- 呼んだ主体が、その pull request の作者なら、GitForge が approve を拒む。opener と reviewer は、別の GitForge のアカウントである。
- `client` も `user` も呼べる。`user` は、その repository に Write があるときだけ受け付ける。
- approve は、向き先が `main` なら、先に今の `main` で検査する。通ったときだけ、GitForge に approve を出す。merge はしない（[approve](../../../backend/merge.md#approve)）。
  - 検査に通らなければ、approve を出さず、理由をエラーの code で返す（`stale_target`、`conflict`、`invariant_violation`、`snapshot_pending`）。
- merge は、別の操作である（[apply request を merge する](merge.md)）。

## 関係する要件

- [論理レビュー](../../../../docs/apply-request/review.md)
- [反映](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `reviewApplyRequest` に書く。
