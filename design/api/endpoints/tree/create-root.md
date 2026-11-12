# 根の主文を作る

利用者が、forge に最初の問いを持ち込むときに、WebUI から呼ぶ。
WebUI の木のビューアが使う。

## 呼び出し

- `POST /tree`
- operationId：`createRoot`

## すること

- 根の仮説を作る。
- 根の主文と背景情報を受け取る。
- 最初のコミットとして、main に書き込む。
- 根は 1 つだけなので、根がすでにあれば作らない。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [役割（利用者）](../../../../docs/roles/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createRoot` に書く。
