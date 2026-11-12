# 木を書き出す

利用者や外の仕組みが、forge の外で木と根拠を読むために呼ぶ。

## 呼び出し

- `GET /tree/export`
- operationId：`exportTree`

## すること

- 木を JSON か Markdown で書き出す。
- リビジョンを指定すると、その時点の木を書き出す。
- 検証結果のレポートと引用も含める。

## 失敗する主な場合

- `validation_failed`（形式の指定が正しくない）
- `not_found`

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [エクスポート](../../../../docs/architecture/README.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `exportTree` に書く。
