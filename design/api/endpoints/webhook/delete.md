# webhook の登録を消す

外の仕組みを設定する人が、受け取りをやめるときに、CLI から呼ぶ。

## 呼び出し

- `DELETE /webhooks/{webhookId}`
- operationId：`deleteWebhook`

## すること

- 登録を消し、以後はその受け取り先に送らない。

## 関係する要件

- [通知](../../../../docs/work/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `deleteWebhook` に書く。
