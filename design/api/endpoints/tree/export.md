# 木を書き出す

利用者は WebUI から、外の仕組みは CLI から、forge の外で木と根拠を読むために呼ぶ。

## 呼び出し

- `GET /repositories/{repository}/tree/export`
- operationId：`exportTree`

## すること

- 木を JSON か Markdown で書き出す。
- リビジョンを指定すると、その時点の木を書き出す。
- 検証結果のレポートと引用も含める。

## 関係する要件

- [エクスポート](../../../../docs/architecture/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `exportTree` に書く。
