# webhook を登録する

外の仕組みを設定する人が、forge の変化を受け取り始めるときに呼ぶ。

## 呼び出し

- `POST /webhooks`
- operationId：`createWebhook`

## すること

- 受け取り先の URL と、受け取るイベントの種類を登録する。
- 署名の秘密鍵を、この応答でだけ返す。

## 失敗する主な場合

- `validation_failed`（URL が正しくない）
- `session_required`
- `forbidden`（user から呼んだ）

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [通知](../../../../docs/work/README.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `createWebhook` に書く。
