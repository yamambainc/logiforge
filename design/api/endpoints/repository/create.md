# repository を作る

利用者が、Logiforge に新しい問いを持ち込むときに、WebUI から呼ぶ。

## 呼び出し

- `POST /repositories`
- operationId：`createRepository`

## すること

- repository の名前と、根の仮説の主文を受け取る。
- GitForge アダプタから、その名前の gitforge_ref を求める。
- GitForge に repository を用意し、根の仮説を入れた最初のコミットを置く（[repository の用意](../../../backend/README.md#repository-の用意)）。
- 同じ名前の repository があれば作らない。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [役割（利用者）](../../../../docs/roles/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createRepository` に書く。
