# project を作る

利用者が、forge に新しい問いを持ち込むときに、WebUI から呼ぶ。

## 呼び出し

- `POST /projects`
- operationId：`createProject`

## すること

- project の名前と、根の仮説の主文を受け取る。
- プロバイダに git リポジトリを作り、根の仮説を最初のコミットとして書き込む。
- 同じ名前の project があれば作らない。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [役割（利用者）](../../../../docs/roles/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createProject` に書く。
