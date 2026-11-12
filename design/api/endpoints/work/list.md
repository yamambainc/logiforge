# 着手できる作業を読む

opener や reviewer が、次にする作業を探すときに呼ぶ。
外の仕組みが、通知を受けて問い合わせ直すときにも使う。

## 呼び出し

- `GET /work`
- operationId：`listWork`

## すること

- 未決の葉、要再検討の仮説、レビュー待ちと修正待ちの apply request、回答済みの人への依頼、引用元が変化した検証結果を返す。
- 種類ごとの件数を返す。
- 部分木を指定して絞り込める。
- open の apply request や、open の人への依頼があるノードの作業は、既定では返さない。

## 失敗する主な場合

- `validation_failed`（種類の指定が正しくない）
- `not_found`（部分木の根がない）

エラーの形は、[エラー](../../README.md) に書く。

## 関係する要件

- [着手できる作業と通知](../../../../docs/work/README.md)

## 詳細

パラメータとスキーマは、[openapi.yaml](../../openapi.yaml) の `listWork` に書く。
