# project を読む

利用者は WebUI から、opener と reviewer は CLI から、project の情報を確かめるときに呼ぶ。

## 呼び出し

- `GET /projects/{project}`
- operationId：`getProject`

## すること

- project の名前と、使っているプロバイダを返す。
- 根の仮説の識別子と、根の値を返す。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getProject` に書く。
