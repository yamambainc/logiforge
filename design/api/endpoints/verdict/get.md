# 検証結果を読む

利用者や reviewer が、検証結果とレポートを読むときに呼ぶ。
差し替えられた古い検証結果も読める。

## 呼び出し

- `GET /verdicts/{verdictId}`
- operationId：`getVerdict`

## すること

- 値と、レポートの本文を返す。
- レポートが参照する artifact と、引用を返す。
- 反映されたリビジョンと、差し替えられたリビジョンを返す。

## 関係する要件

- [検証結果と引用](../../../../docs/tree/verdict.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getVerdict` に書く。
