# API の一覧

API ごとの概要を引くときに使う。
各エンドポイントの詳細は、[openapi.yaml](../openapi.yaml) が正本である。

API を呼ぶのは、CLI と WebUI だけである。エージェント、外の仕組み、人は、CLI か WebUI を通して API を使う（[レイヤー構成](../../architecture.md)）。

## project

- [project の一覧を読む](project/list.md) → `GET /projects`
- [project を作る](project/create.md) → `POST /projects`
- [project を読む](project/get.md) → `GET /projects/{project}`

## 木

- [木の概要を読む](tree/get.md) → `GET /projects/{project}/tree`
- [木のノードを読む](tree/list-nodes.md) → `GET /projects/{project}/tree/nodes`
- [作業の終わりを判定する](tree/completion.md) → `GET /projects/{project}/tree/completion`
- [木を書き出す](tree/export.md) → `GET /projects/{project}/tree/export`

## ノード

- [ノードの詳細を読む](node/get.md) → `GET /projects/{project}/nodes/{nodeId}`
- [ノードの履歴を読む](node/history.md) → `GET /projects/{project}/nodes/{nodeId}/history`

## apply request

- [apply request の一覧を読む](apply-request/list.md) → `GET /projects/{project}/apply-requests`
- [apply request を作る](apply-request/create.md) → `POST /projects/{project}/apply-requests`
- [apply request の詳細を読む](apply-request/get.md) → `GET /projects/{project}/apply-requests/{applyRequestId}`
- [apply request を更新する](apply-request/update.md) → `PATCH /projects/{project}/apply-requests/{applyRequestId}`
- [apply request をレビューする](apply-request/review.md) → `POST /projects/{project}/apply-requests/{applyRequestId}/reviews`
- [apply request を close する](apply-request/close.md) → `POST /projects/{project}/apply-requests/{applyRequestId}/close`
- [レビューの材料を読む](apply-request/review-material.md) → `GET /projects/{project}/apply-requests/{applyRequestId}/review-material`

## 着手できる作業

- [着手できる作業を読む](work/list.md) → `GET /projects/{project}/work`

## ファイルの中身

- [ファイルの中身を置く](blob/upload.md) → `POST /projects/{project}/blobs`
- [ファイルの中身を読む](blob/get.md) → `GET /projects/{project}/blobs/{sha256}`

## artifact

- [artifact を登録する](artifact/create.md) → `POST /projects/{project}/artifacts`
- [artifact を読む](artifact/get.md) → `GET /projects/{project}/artifacts/{artifactId}`

## 検証結果

- [検証結果を読む](verdict/get.md) → `GET /projects/{project}/verdicts/{verdictId}`

## 人への依頼

- [人への依頼の一覧を読む](human-request/list.md) → `GET /projects/{project}/human-requests`
- [人への依頼を起票する](human-request/create.md) → `POST /projects/{project}/human-requests`
- [人への依頼を読む](human-request/get.md) → `GET /projects/{project}/human-requests/{humanRequestId}`
- [人への依頼に回答する](human-request/answer.md) → `POST /projects/{project}/human-requests/{humanRequestId}/answer`

## イベント

- [イベントの一覧を読む](event/list.md) → `GET /projects/{project}/events`

## webhook

- [webhook の登録を読む](webhook/list.md) → `GET /projects/{project}/webhooks`
- [webhook を登録する](webhook/create.md) → `POST /projects/{project}/webhooks`
- [webhook の登録を消す](webhook/delete.md) → `DELETE /projects/{project}/webhooks/{webhookId}`
