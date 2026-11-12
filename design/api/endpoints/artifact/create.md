# artifact を登録する

opener が、検証結果のレポートから参照する根拠を登録するときに呼ぶ。
利用者が、人への依頼の回答にファイルを添えるときにも使う。

## 呼び出し

- `POST /artifacts`
- operationId：`createArtifact`

## すること

- 種類（file、link、reference、program、output、human_entry）を付けて登録する。
- 中身は、置いた blob のハッシュか、リンクの URI で指す。
- 登録したセッションを記録する。
- artifact は、登録したあと変えない。

## 関係する要件

- [artifact](../../../../docs/tree/verdict.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `createArtifact` に書く。
