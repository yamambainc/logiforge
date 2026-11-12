# イベントの一覧を読む

外の仕組みが、前に読んだ位置より後の変化を知るために、CLI から定期的に呼ぶ。
監査をする人が、変更の履歴をたどるときにも、CLI から使う。

## 呼び出し

- `GET /repositories/{repository}/events`
- operationId：`listEvents`

## すること

- 監査用のイベントログを、起きた順に返す。
- 指定した番号より後のイベントだけを返せる。
- 一覧はページに分けて返す。

## 関係する要件

- [監査](../../../../docs/architecture/README.md)
- [イベント](../../../../docs/work/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listEvents` に書く。
