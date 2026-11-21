# main のコミットの一覧を読む

利用者が、過去の時点のスナップショットツリーを見るために、時点を選ぶときに、WebUI から呼ぶ。

## 呼び出し

- `GET /repositories/{repository}/commits`
- operationId：`listCommits`

## すること

- worker が取り込んだ `main` のコミットを、新しい順に返す。
- 各コミットの番号、SHA、時刻、反映した apply request を返す。
- 反映した apply request は、worker の反映かの判定の結果（コミットの記録の `apply_request`）から引く（[main のコミットと記録](../../../files/revisions.md)）。
- `main` の並びの索引と、コミットの記録から引く。GitForge には問い合わせない（[コミットの記録から引くもの](../../../backend/snapshot/main.md#コミットの記録から引くもの)）。
- 一覧はページに分けて返す。カーソルは、次に読むコミットの番号である。

## 関係する要件

- [apply request と履歴](../../../../docs/webui/history.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listCommits` に書く。
