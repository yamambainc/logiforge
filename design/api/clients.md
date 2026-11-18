# クライアントの要件との対応

API が、CLI と WebUI の要件を満たしているかを確かめるときに読む。
クライアントの要件は、[CLI](../../docs/cli/README.md) と [WebUI](../../docs/webui/README.md) が正本である。
各 API が何をするかは、リンク先のページに書く。

## CLI

| 要件 | API |
|---|---|
| すべてのコマンドで JSON を出す | すべての API が JSON を返す |
| 木を変えるときは apply request を作るか、更新する | git でブランチを push し、[作る](endpoints/apply-request/create.md)、[更新する](endpoints/apply-request/update.md)（[CLI](../cli/README.md)） |
| 着手できる作業を問い合わせる | [着手できる作業を読む](endpoints/work/list.md) |
| 作業の終わりを判定する | [作業の終わりを判定する](endpoints/tree/completion.md) |
| 論理レビューを行う | [レビューの材料を読む](endpoints/apply-request/review-material.md)、[レビューする](endpoints/apply-request/review.md) |
| 検証結果に添えるファイルを置く | ブランチのコミットに入れる（[artifact の置き方](../files/artifacts.md)） |
| 人への依頼を起票する | [人への依頼を起票する](endpoints/human-request/create.md) |
| 外の仕組みが次の作業を知る | [着手できる作業を読む](endpoints/work/list.md) |

## WebUI

画面ごとの API の使い方は、[WebUI の設計](../webui/screens.md) に書く。

| 画面 | 要件 | API |
|---|---|---|
| [repository の一覧](../../docs/webui/repositories.md) | repository を、根の主文と根の値と一緒に一覧し、選ぶ | [repository の一覧を読む](endpoints/repository/list.md)（`root_statement`、`root_value`） |
| repository の一覧 | repository を作る（根の主文を含む） | [repository を作る](endpoints/repository/create.md) |
| [木のビューア](../../docs/webui/tree-viewer.md) | 木とバッジを表示する | [木のノードを読む](endpoints/tree/list-nodes.md) |
| 木のビューア | ノードの詳細 | [ノードの詳細を読む](endpoints/node/get.md)、[検証結果を読む](endpoints/verdict/get.md)、[artifact のファイルを読む](endpoints/artifact/get.md) |
| 木のビューア | ノードの履歴 | [ノードの履歴を読む](endpoints/node/history.md) |
| [人への依頼](../../docs/webui/human-requests.md) | 種類と状態で絞り込む | [人への依頼の一覧を読む](endpoints/human-request/list.md) |
| 人への依頼 | 回答し、ファイルを添付する | [人への依頼に回答する](endpoints/human-request/answer.md)（ファイルは回答に含める） |
| [apply request と履歴](../../docs/webui/history.md) | 一覧と、差分とレビュー | [一覧を読む](endpoints/apply-request/list.md)、[詳細を読む](endpoints/apply-request/get.md) |
| apply request と履歴 | 過去の時点の木 | [main のコミットの一覧を読む](endpoints/commit/list.md)、[木のノードを読む](endpoints/tree/list-nodes.md)（`commit` を指定する） |
| apply request と履歴 | 反映したあとの木 | [木のノードを読む](endpoints/tree/list-nodes.md)、[ノードの詳細を読む](endpoints/node/get.md)（どちらも `apply_request` を指定する） |
| [ダッシュボード](../../docs/webui/dashboard.md) | 根の値と、作業の終わりの判定 | [作業の終わりを判定する](endpoints/tree/completion.md) |
| ダッシュボード | 種類ごとの着手できる作業の件数 | [着手できる作業を読む](endpoints/work/list.md) |

## WebUI に置かない操作

- WebUI は、`user` のトークンで API を使う。
- `user` は、apply request を作る API と、レビューする API を使えない（[認可](../backend/README.md#認可)）。
- そのため、WebUI からは、木の変更も `approve` もできない（[反映の条件](../../docs/apply-request/review.md)）。
