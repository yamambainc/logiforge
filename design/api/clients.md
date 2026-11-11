# クライアントの要件との対応

API が、CLI と WebUI の要件を満たしているかを確かめるときに読む。
クライアントの要件は、[CLI](../../docs/cli/README.md) と [WebUI](../../docs/webui/README.md) が正本である。
エンドポイントの詳細は、[openapi.yaml](openapi.yaml) にある。

## CLI

| 要件 | 使うエンドポイント |
|---|---|
| すべてのコマンドで JSON を出す | すべてのエンドポイントが JSON を返す |
| 木を変えるコマンドは apply request を作るか、操作を加える | `POST /apply-requests`、`PATCH /apply-requests/{id}` |
| 着手できる作業を問い合わせる | `GET /work` |
| 作業の終わりを判定する | `GET /tree/completion` |
| 論理レビューの材料を読む | `GET /apply-requests/{id}/review-material` |
| レビューを付ける | `POST /apply-requests/{id}/reviews` |
| 検証結果に添えるファイルを置く | `POST /blobs`、`POST /artifacts` |
| 人への依頼を起票する | `POST /human-requests` |
| 外の仕組みで通知を受ける | `POST /webhooks`、`GET /events` |

## WebUI

| 画面 | 要件 | 使うエンドポイント |
|---|---|---|
| [木のビューア](../../docs/webui/tree-viewer.md) | 木を表示する | `GET /tree/nodes` |
| 木のビューア | 要再検討、審査中の apply request、open の人への依頼のバッジ | `Hypothesis` と `Story` の `needs_reconsideration`、`open_apply_request_ids`、`open_human_request_ids` |
| 木のビューア | ノードの詳細（検証結果、レポート、artifact、引用） | `GET /nodes/{id}`、`GET /verdicts/{id}`、`GET /artifacts/{id}`、`GET /blobs/{sha256}` |
| 木のビューア | ノードの履歴 | `GET /nodes/{id}/history` |
| 木のビューア | 根の主文を作る | `POST /tree` |
| [人への依頼](../../docs/webui/human-requests.md) | 種類と状態で絞り込む | `GET /human-requests` |
| 人への依頼 | 回答し、ファイルを添付する | `POST /blobs`、`POST /artifacts`、`POST /human-requests/{id}/answer` |
| [apply request と履歴](../../docs/webui/history.md) | apply request を状態で絞り込み、差分とレビューを見る | `GET /apply-requests`、`GET /apply-requests/{id}` |
| apply request と履歴 | 過去の時点の木 | `GET /tree/nodes?revision=` |
| [ダッシュボード](../../docs/webui/dashboard.md) | 根の値と、作業の終わりの判定 | `GET /tree/completion` |
| ダッシュボード | 種類ごとの着手できる作業の件数 | `GET /work` の `counts` |

## WebUI に置かない操作

- WebUI は、`user` のトークンで API を使う。
- `user` は、`POST /apply-requests` と `POST /apply-requests/{id}/reviews` を使えない（[主体と認証](README.md)）。
  - そのため、WebUI からは、木の変更も `approve` もできない（[反映の条件](../../docs/apply-request/review.md)）。
