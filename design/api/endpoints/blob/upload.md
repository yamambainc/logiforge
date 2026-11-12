# ファイルの中身を置く

opener が、プログラムや実行結果などのファイルを artifact にする前に、CLI から呼ぶ。
利用者が、人への依頼の回答にファイルを添えるときにも、WebUI から使う。

## 呼び出し

- `POST /repositories/{repository}/blobs`
- operationId：`uploadBlob`

## すること

- 受け取った中身の SHA-256 を計算して、変更しない形で置く。
- 同じ中身がすでにあれば、置かずに同じ結果を返す。
- 中身のハッシュと大きさを返す。

## 関係する要件

- [artifact](../../../../docs/tree/verdict.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `uploadBlob` に書く。
