# webhook の登録を読む

外の仕組みを設定する人が、登録済みの受け取り先を確かめるときに、CLI から呼ぶ。

## 呼び出し

- `GET /projects/{project}/webhooks`
- operationId：`listWebhooks`

## すること

- 登録した受け取り先の URL と、受け取るイベントの種類を返す。
- 署名の秘密鍵は返さない。

## 関係する要件

- [通知](../../../../docs/work/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listWebhooks` に書く。
