# ファイルの中身を読む

利用者は WebUI から、reviewer は CLI から、artifact の中身を読むときに呼ぶ。

## 呼び出し

- `GET /projects/{project}/blobs/{sha256}`
- operationId：`getBlob`

## すること

- ハッシュで指定した中身を返す。
- 中身は置いたときから変わらない。

## 関係する要件

- [artifact](../../../../docs/tree/verdict.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getBlob` に書く。
