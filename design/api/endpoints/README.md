# API の一覧

API ごとの概要を引くときに使う。
各エンドポイントの詳細は、[openapi.yaml](../openapi.yaml) が正本である。

API を呼ぶのは、CLI と WebUI だけである。
各ページの「〜から呼ぶ」は、主体がどちらのクライアントを通すかを表す（[レイヤー構成](../../architecture.md)）。

## 木

- [木の概要を読む](tree/get.md) → `GET /tree`
- [根の主文を作る](tree/create-root.md) → `POST /tree`
- [木のノードを読む](tree/list-nodes.md) → `GET /tree/nodes`
- [作業の終わりを判定する](tree/completion.md) → `GET /tree/completion`
- [木を書き出す](tree/export.md) → `GET /tree/export`

## ノード

- [ノードの詳細を読む](node/get.md) → `GET /nodes/{nodeId}`
- [ノードの履歴を読む](node/history.md) → `GET /nodes/{nodeId}/history`

## apply request

- [apply request の一覧を読む](apply-request/list.md) → `GET /apply-requests`
- [apply request を作る](apply-request/create.md) → `POST /apply-requests`
- [apply request の詳細を読む](apply-request/get.md) → `GET /apply-requests/{applyRequestId}`
- [apply request を更新する](apply-request/update.md) → `PATCH /apply-requests/{applyRequestId}`
- [apply request をレビューする](apply-request/review.md) → `POST /apply-requests/{applyRequestId}/reviews`
- [apply request を close する](apply-request/close.md) → `POST /apply-requests/{applyRequestId}/close`
- [レビューの材料を読む](apply-request/review-material.md) → `GET /apply-requests/{applyRequestId}/review-material`

## 着手できる作業

- [着手できる作業を読む](work/list.md) → `GET /work`

## ファイルの中身

- [ファイルの中身を置く](blob/upload.md) → `POST /blobs`
- [ファイルの中身を読む](blob/get.md) → `GET /blobs/{sha256}`

## artifact

- [artifact を登録する](artifact/create.md) → `POST /artifacts`
- [artifact を読む](artifact/get.md) → `GET /artifacts/{artifactId}`

## 検証結果

- [検証結果を読む](verdict/get.md) → `GET /verdicts/{verdictId}`

## 人への依頼

- [人への依頼の一覧を読む](human-request/list.md) → `GET /human-requests`
- [人への依頼を起票する](human-request/create.md) → `POST /human-requests`
- [人への依頼を読む](human-request/get.md) → `GET /human-requests/{humanRequestId}`
- [人への依頼に回答する](human-request/answer.md) → `POST /human-requests/{humanRequestId}/answer`

## イベント

- [イベントの一覧を読む](event/list.md) → `GET /events`

## webhook

- [webhook の登録を読む](webhook/list.md) → `GET /webhooks`
- [webhook を登録する](webhook/create.md) → `POST /webhooks`
- [webhook の登録を消す](webhook/delete.md) → `DELETE /webhooks/{webhookId}`
