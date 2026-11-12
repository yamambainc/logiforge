# webhook の登録を読む

外の仕組みを設定する人が、登録済みの受け取り先を確かめるときに呼ぶ。

## 呼び出し

- `GET /webhooks`
- operationId：`listWebhooks`

## すること

- 登録した受け取り先の URL と、受け取るイベントの種類を返す。
- 署名の秘密鍵は返さない。

## 失敗する主な場合

- `unauthenticated`

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [通知](../../../../docs/work/README.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `listWebhooks` に書く。
