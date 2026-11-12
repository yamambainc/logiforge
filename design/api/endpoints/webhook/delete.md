# webhook の登録を消す

外の仕組みを設定する人が、受け取りをやめるときに呼ぶ。

## 呼び出し

- `DELETE /webhooks/{webhookId}`
- operationId：`deleteWebhook`

## すること

- 登録を消し、以後はその受け取り先に送らない。

## 失敗する主な場合

- `not_found`
- `session_required`

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [通知](../../../../docs/work/README.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `deleteWebhook` に書く。
