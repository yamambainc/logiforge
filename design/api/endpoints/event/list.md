# イベントの一覧を読む

外の仕組みが、受け取り損ねた webhook を読み直すときに、CLI から呼ぶ。
監査をする人が、変更の履歴をたどるときにも、CLI から使う。

## 呼び出し

- `GET /events`
- operationId：`listEvents`

## すること

- 監査用のイベントログを、起きた順に返す。
- 指定した番号より後のイベントだけを返せる。
- 一覧はページに分けて返す。

## 関係する要件

- [監査](../../../../docs/architecture/README.md)
- [通知](../../../../docs/work/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listEvents` に書く。
