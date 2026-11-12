# 人への依頼に回答する

利用者が、PVR の結果や判断を記入するときに、WebUI から呼ぶ。
WebUI の人への依頼の画面が使う。

## 呼び出し

- `POST /repositories/{repository}/human-requests/{humanRequestId}/answer`
- operationId：`answerHumanRequest`

## すること

- 回答の本文と添付を受け取る。
- 回答を artifact（human_entry）として登録する。
- 状態を answered にする。回答は 1 回だけである。
- 外の仕組みに通知する。

## 関係する要件

- [人への依頼](../../../../docs/human-requests/README.md)
- [役割（利用者）](../../../../docs/roles/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `answerHumanRequest` に書く。
