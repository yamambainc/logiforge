# 独自プロバイダ

GitHub を使わずに Logiforge を動かすときに、[やりとり](../backend/ports/interaction.md) のポートをどう実装するかを確かめるために読む。
提案・レビュー・人への依頼は、自前の DB（データベース）の表に置く。
木のリポジトリは、[git アダプタ](git.md) のローカルの bare リポジトリで扱う。

## 置き場所

- 提案・レビュー・人への依頼は、SQLite に置く。
  - ファイルは、[状態ストア](sqlite.md) と同じ SQLite ファイルにする。表は分ける。
  - なぜ：論理的には別のポートだが、置き場所を 1 つにすると運用が単純になるから。
- 作った行は変えない。
  - 例外は、`apply_request` の `state`、`closed_reason`、`closed_by_session_id` を、close か反映のときに 1 度だけ埋めることである。

## repository_name

- すべての表は、`repository_name` を持つ。
- repository の一覧の表は持たない。repository は、木のリポジトリの「列挙する」で得る。

## apply_request

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `apr_` と ULID（作った順に並ぶ一意の識別子）|
| `description` | 文字列 | 説明欄 |
| `state` | `open` / `merged` / `closed` | 状態 |
| `closed_reason` | 文字列 | close の理由 |
| `closed_by_session_id` | 文字列 | close したセッション |

## apply_request_version

| 列 | 型 | 内容 |
|---|---|---|
| `apply_request_id` | 識別子 | 属する提案 |
| `version` | 整数 | 1 から順に増える版 |
| `revision` | 文字列 | 版のリビジョン（木のリポジトリの参照） |

- 版を書いたセッションは、コミットの trailer から読む。表には持たない。

## review

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `rev_` と ULID |
| `apply_request_id` | 識別子 | 対象の提案 |
| `version` | 整数 | レビューした版 |
| `kind` | `approve` / `request_changes` / `comment` | 種類 |
| `body` | 文字列 | 本文 |
| `reviewer_session_id` | 文字列 | reviewer のセッション |
| `prompt_revision` | 文字列 | 使ったプロンプトのリビジョン |

## merge_failure

| 列 | 型 | 内容 |
|---|---|---|
| `apply_request_id` | 識別子 | 反映に失敗した提案 |
| `version` | 整数 | 失敗した版 |
| `reason` | `stale_target` / `invariant_violation` | 理由 |
| `detail` | 文字列 | 失敗した操作と条件 |

## human_request と human_request_answer

| 表 | 列 |
|---|---|
| `human_request` | `id`（`hrq_` と ULID）、`node_id`、`kind`（`pvr` / `decision`）、`body`、`created_by_session_id`、`created_at` |
| `human_request_answer` | `human_request_id`（主キー）、`body`、`answered_by_principal_id`、`answered_at` |
| `human_request_attachment` | `human_request_id`、`sha256` |

- `human_request_answer` の主キーで、回答を 1 回だけにする。
- 人への依頼の状態は保存しない。回答の行があれば `answered` である。
