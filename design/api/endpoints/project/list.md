# project の一覧を読む

利用者が、扱う project を選ぶときに、WebUI から呼ぶ。
opener や reviewer が、作業する project を探すときにも、CLI から使う。

## 呼び出し

- `GET /projects`
- operationId：`listProjects`

## すること

- forge が扱う project を、名前と根の値と一緒に返す。
- 一覧はページに分けて返す。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listProjects` に書く。
