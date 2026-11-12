# artifact を読む

利用者や reviewer が、レポートが参照する根拠を確かめるときに呼ぶ。

## 呼び出し

- `GET /artifacts/{artifactId}`
- operationId：`getArtifact`

## すること

- artifact の種類と、中身のハッシュかリンクを返す。
- 中身そのものは、ファイルの中身を読む API で読む。

## 失敗する主な場合

- `not_found`

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [artifact](../../../../docs/tree/verdict.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `getArtifact` に書く。
