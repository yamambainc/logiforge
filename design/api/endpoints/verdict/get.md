# 検証結果を読む

利用者は WebUI から、reviewer は CLI から、検証結果とレポートを読むときに呼ぶ。
差し替えられた古い検証結果も読める。

## 呼び出し

- `GET /repositories/{repository}/verdicts/{verdictId}`
- operationId：`getVerdict`

## すること

- 値と、レポートの本文を返す。
- レポートが参照する artifact と、引用を返す。
- 反映された `main` のコミットと、差し替えられた `main` のコミットを返す。
- 最新のコミットが valid でなければ、`valid_commit` の木を返す（[木を読む API](../../../backend/invalid-main.md#木を読む-api)）。

## 関係する要件

- [検証結果と引用](../../../../docs/tree/verdict.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getVerdict` に書く。
