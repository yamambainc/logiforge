# クライアントの要件との対応

API が、CLI と WebUI の要件を満たしているかを確かめるときに読む。
クライアントの要件は、[CLI](../../docs/cli/README.md) と [WebUI](../../docs/webui/README.md) が正本である。
各 API が何をするかは、リンク先のページに書く。

## CLI

| 要件 | API |
|---|---|
| すべてのコマンドで JSON を出す | すべての API が JSON を返す |
| 木を変えるコマンドは apply request を作るか、更新する | [作る](endpoints/apply-request/create.md)、[更新する](endpoints/apply-request/update.md) |
| 着手できる作業を問い合わせる | [着手できる作業を読む](endpoints/work/list.md) |
| 作業の終わりを判定する | [作業の終わりを判定する](endpoints/tree/completion.md) |
| 論理レビューを行う | [レビューの材料を読む](endpoints/apply-request/review-material.md)、[レビューする](endpoints/apply-request/review.md) |
| 検証結果に添えるファイルを置く | [ファイルの中身を置く](endpoints/blob/upload.md)、[artifact を登録する](endpoints/artifact/create.md) |
| 人への依頼を起票する | [人への依頼を起票する](endpoints/human-request/create.md) |
| 外の仕組みで通知を受ける | [webhook を登録する](endpoints/webhook/create.md)、[イベントの一覧を読む](endpoints/event/list.md) |

## WebUI

| 画面 | 要件 | API |
|---|---|---|
| [木のビューア](../../docs/webui/tree-viewer.md) | 木とバッジを表示する | [木のノードを読む](endpoints/tree/list-nodes.md) |
| 木のビューア | ノードの詳細 | [ノードの詳細を読む](endpoints/node/get.md)、[検証結果を読む](endpoints/verdict/get.md)、[artifact を読む](endpoints/artifact/get.md)、[ファイルの中身を読む](endpoints/blob/get.md) |
| 木のビューア | ノードの履歴 | [ノードの履歴を読む](endpoints/node/history.md) |
| 木のビューア | 根の主文を作る | [根の主文を作る](endpoints/tree/create-root.md) |
| [人への依頼](../../docs/webui/human-requests.md) | 種類と状態で絞り込む | [人への依頼の一覧を読む](endpoints/human-request/list.md) |
| 人への依頼 | 回答し、ファイルを添付する | [人への依頼に回答する](endpoints/human-request/answer.md)、[ファイルの中身を置く](endpoints/blob/upload.md)、[artifact を登録する](endpoints/artifact/create.md) |
| [apply request と履歴](../../docs/webui/history.md) | 一覧と、差分とレビュー | [一覧を読む](endpoints/apply-request/list.md)、[詳細を読む](endpoints/apply-request/get.md) |
| apply request と履歴 | 過去の時点の木 | [木のノードを読む](endpoints/tree/list-nodes.md) |
| [ダッシュボード](../../docs/webui/dashboard.md) | 根の値と、作業の終わりの判定 | [作業の終わりを判定する](endpoints/tree/completion.md) |
| ダッシュボード | 種類ごとの着手できる作業の件数 | [着手できる作業を読む](endpoints/work/list.md) |

## WebUI に置かない操作

- WebUI は、`user` のトークンで API を使う。
- `user` は、apply request を作る API と、レビューする API を使えない（[主体と認証](README.md)）。
  - そのため、WebUI からは、木の変更も `approve` もできない（[反映の条件](../../docs/apply-request/review.md)）。
