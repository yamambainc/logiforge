# repository を作る

利用者が、forge に新しい問いを持ち込むときに、WebUI から呼ぶ。

## 呼び出し

- `POST /repositories`
- operationId：`createRepository`

## すること

- repository の名前と、根の仮説の主文を受け取る。
- やりとりのアダプタから、その名前の provider_ref を求める。
- やりとり、木の読み書き、スナップショットの置き場の順に repository を用意する（[repository の用意](../../../backend/README.md)）。
- 根の仮説を、最初のリビジョンとして書き込む。
- 同じ名前の repository があれば作らない。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [役割（利用者）](../../../../docs/roles/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createRepository` に書く。
