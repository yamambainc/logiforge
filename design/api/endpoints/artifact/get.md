# artifact のファイルを読む

利用者は WebUI から、reviewer は CLI から、artifact のファイルの中身を読むときに呼ぶ。
反映の前のファイルは版のリビジョンを、回答の添付は回答のリビジョンを指定して読む。

## 呼び出し

- `GET /repositories/{repository}/artifacts/{sha256}`
- operationId：`getArtifact`

## すること

- 指定したリビジョンの `artifacts/sha256/` から、ファイルの中身を返す。
- リビジョンを指定しなければ、今の `main` から読む。
- link の artifact の先は、取りに行かない。

## 関係する要件

- [artifact](../../../../docs/tree/verdict.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `getArtifact` に書く。
