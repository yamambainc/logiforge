# API の一覧

API ごとの概要を引くときに使う。
各エンドポイントの詳細は、[openapi.yaml](../openapi.yaml) が正本である。

API を呼ぶのは、CLI と WebUI だけである。エージェント、外の仕組み、人は、CLI か WebUI を通して API を使う（[レイヤー構成](../../architecture.md)）。

## サインイン（WebUI）

- WebUI のサインインの中継（[サインインの中継](../../webui/oauth-relay.md)）→ `GET /oauth/config`、`POST /oauth/token`

## セットアップと設定（WebUI）

- [セットアップの API](setup.md) → `/setup` の下
- [設定の API](settings.md) → `/settings` の下

## repository

- [repository の一覧を読む](repository/list.md) → `GET /repositories`
- [repository を作る](repository/create.md) → `POST /repositories`
- [repository を読む](repository/get.md) → `GET /repositories/{repository}`

## 木

- [木の概要を読む](tree/get.md) → `GET /repositories/{repository}/tree`
- [木のノードを読む](tree/list-nodes.md) → `GET /repositories/{repository}/tree/nodes`
- [作業の終わりを判定する](tree/completion.md) → `GET /repositories/{repository}/tree/completion`
- [木を書き出す](tree/export.md) → `GET /repositories/{repository}/tree/export`

## main のコミット

- [main のコミットの一覧を読む](commit/list.md) → `GET /repositories/{repository}/commits`

## ノード

- [ノードの詳細を読む](node/get.md) → `GET /repositories/{repository}/nodes/{nodeId}`
- [ノードの履歴を読む](node/history.md) → `GET /repositories/{repository}/nodes/{nodeId}/history`

## apply request

- [apply request の一覧を読む](apply-request/list.md) → `GET /repositories/{repository}/apply-requests`
- [apply request を作る](apply-request/create.md) → `POST /repositories/{repository}/apply-requests`
- [apply request の詳細を読む](apply-request/get.md) → `GET /repositories/{repository}/apply-requests/{applyRequestId}`
- [apply request を更新する](apply-request/update.md) → `PATCH /repositories/{repository}/apply-requests/{applyRequestId}`
- [apply request をレビューする](apply-request/review.md) → `POST /repositories/{repository}/apply-requests/{applyRequestId}/reviews`
- [apply request を close する](apply-request/close.md) → `POST /repositories/{repository}/apply-requests/{applyRequestId}/close`
- [レビューの材料を読む](apply-request/review-material.md) → `GET /repositories/{repository}/apply-requests/{applyRequestId}/review-material`

## 着手できる作業

- [着手できる作業を読む](work/list.md) → `GET /repositories/{repository}/work`

## artifact

- [artifact のファイルを読む](artifact/get.md) → `GET /repositories/{repository}/artifacts/{sha256}`

## 検証結果

- [検証結果を読む](verdict/get.md) → `GET /repositories/{repository}/verdicts/{verdictId}`

## 人への依頼

- [人への依頼の一覧を読む](human-request/list.md) → `GET /repositories/{repository}/human-requests`
- [人への依頼を起票する](human-request/create.md) → `POST /repositories/{repository}/human-requests`
- [人への依頼を読む](human-request/get.md) → `GET /repositories/{repository}/human-requests/{humanRequestId}`
- [人への依頼に回答する](human-request/answer.md) → `POST /repositories/{repository}/human-requests/{humanRequestId}/answer`
