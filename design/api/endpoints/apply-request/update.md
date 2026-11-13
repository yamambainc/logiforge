# apply request を更新する

opener が、request_changes を受けたあとや、反映に失敗したあとに、ブランチを直して push してから、CLI から呼ぶ。
どの client のセッションからでも更新できる。

## 呼び出し

- `PATCH /repositories/{repository}/apply-requests/{applyRequestId}`
- operationId：`updateApplyRequest`

## すること

- もとにした版の番号を受け取る。今の版と違えば、受け付けない。
  - クライアントは、`version_conflict` が返ったら、今の版が自分の書いたものかを読んで確かめる。送り直しで、最初の要求が通っていたことがあるため。
- ブランチの今の先頭を、新しい版として取り込む。それまでの版の差分を、すべて置き換える。
- 新しい版を取り込んだセッションを、版の記録に書く。
- それまでの approve は、新しい版には効かない。

## 関係する要件

- [反映の条件](../../../../docs/apply-request/review.md)
- [反映](../../../../docs/apply-request/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `updateApplyRequest` に書く。
