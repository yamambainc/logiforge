# apply request を merge する

approve された apply request を、`main` に反映するときに、CLI か WebUI から呼ぶ。
エージェントは CLI から、利用者は apply request と履歴の画面から使う。GitForge の画面から直接 merge してもよい（[merge](../../../../docs/apply-request/review.md#merge)）。

## 呼び出し

- `POST /repositories/{repository}/apply-requests/{applyRequestId}/merge`
- operationId：`mergeApplyRequest`

## すること

- 承認されているかと、木が壊れないかを、今の `main` で確かめ直してから、呼んだ主体のトークンで、GitForge の pull request を squash merge する（[merge](../../../backend/merge.md#merge)）。
- 確かめに通らなければ、merge しない。理由をエラーの code で返す。
  - `not_approved`：承認の状態が承認済みでない。または、GitForge の承認を必須にする設定が、承認が足りないとして merge を断った。
  - `stale_target`、`conflict`、`invariant_violation`：今の `main` での確かめに通らない。pull request のふつうのコメントにも、理由を書く。
  - `snapshot_pending`：先頭のコミットか今の `main` を、worker がまだ取り込んでいない。少し待って送り直す。
  - `merge_forbidden`：呼んだ主体に merge の権限がなく、GitForge が断った。
- `client` も `user` も呼べる。`user` は、その repository に Write があるときだけ受け付ける。opener も呼べる。
- 反映のコミットが読む側に出るまで待ってから応答する（[書いた直後の読み](../../writes.md)）。
- もう反映された apply request に送り直したら、成功として今の状態を返す。

## 関係する要件

- [論理レビュー](../../../../docs/apply-request/review.md)
- [反映](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `mergeApplyRequest` に書く。
