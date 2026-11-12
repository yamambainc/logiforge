# ファイルの中身を読む

利用者や reviewer が、artifact の中身を読むときに呼ぶ。

## 呼び出し

- `GET /blobs/{sha256}`
- operationId：`getBlob`

## すること

- ハッシュで指定した中身を返す。
- 中身は置いたときから変わらない。

## 失敗する主な場合

- `not_found`

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [artifact](../../../../docs/tree/verdict.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `getBlob` に書く。
