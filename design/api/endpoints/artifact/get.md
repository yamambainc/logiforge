# artifact を読む

利用者は WebUI から、reviewer は CLI から、レポートが参照する根拠を確かめるときに呼ぶ。

## 呼び出し

- `GET /repositories/{repository}/artifacts/{artifactId}`
- operationId：`getArtifact`

## すること

- artifact の種類と、中身のハッシュかリンクを返す。
- 中身そのものは、ファイルの中身を読む API で読む。

## 関係する要件

- [artifact](../../../../docs/tree/verdict.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getArtifact` に書く。
