# 着手できる作業を読む

opener や reviewer が、次にする作業を探すときに、CLI から呼ぶ。
外の仕組みが、定期的に問い合わせるときにも、CLI から使う。

## 呼び出し

- `GET /repositories/{repository}/work`
- operationId：`listWork`

## すること

- 未決の葉、要再検討の仮説、レビュー待ちと修正待ちの apply request、回答済みの人への依頼、引用元が変化した検証結果を返す。
- 種類ごとの件数を返す。
- 部分木を指定して絞り込める。
- open の apply request や、open の人への依頼があるノードの作業は、既定では返さない。

## 関係する要件

- [着手できる作業](../../../../docs/work/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listWork` に書く。
