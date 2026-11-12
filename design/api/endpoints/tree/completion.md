# 作業の終わりを判定する

外の仕組みが、ループを続けるかどうかを決めるときに呼ぶ。
利用者がダッシュボードで、作業の終わりに近いかを見るときにも使う。

## 呼び出し

- `GET /tree/completion`
- operationId：`getCompletion`

## すること

- 根の値が決まっているかを返す。
- 根拠部分木で着手できる作業の件数を返す。
- 両方を満たすかどうか（終わったか）を返す。

## 失敗する主な場合

- `not_found`（根がまだない）

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [作業の終わりの判定](../../../../docs/work/README.md)
- [根拠部分木](../../../../docs/tree/propagation.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `getCompletion` に書く。
