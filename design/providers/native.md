# 独自プロバイダ

GitHub を使わずに Logiforge を動かすときに、提案・レビュー・人への依頼・イベントを、どの表に置くかを確かめるために読む。
インターフェースは、[プロバイダ](README.md) が正本である。

## 置き場所

- 木と小さな artifact は、repository ごとの bare の git リポジトリに置く（[木のファイル配置](../files/README.md)）。
  - 提案のブランチは、`refs/logiforge/apply-requests/<識別子>` に置く。
- 提案・レビュー・人への依頼・イベントは、DB に置く。既定は SQLite とする。
  - なぜ：反映は repository ごとに 1 件ずつ直列に行うので、書き込みが 1 本の SQLite で足りるから。
- 作った行は変えない。
  - 例外は、`apply_request` の `state`、`closed_reason`、`closed_by_session_id` を、close か反映のときに 1 度だけ埋めることである。

## repository

| 列 | 型 | 内容 |
|---|---|---|
| `repository_name` | 文字列 | repository の名前。主キー |
| `git_path` | 文字列 | bare の git リポジトリのパス |
| `created_at` | 時刻 | 作った時刻 |

ほかの表は、すべて `repository_name` を持つ。

## apply_request

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `apr_` と ULID |
| `description` | 文字列 | 説明欄 |
| `state` | `open` / `merged` / `closed` | 状態 |
| `closed_reason` | 文字列 | close の理由 |
| `closed_by_session_id` | 文字列 | close したセッション |

## apply_request_version

| 列 | 型 | 内容 |
|---|---|---|
| `apply_request_id` | 識別子 | 属する提案 |
| `version` | 整数 | 1 から順に増える版 |
| `head_commit` | 文字列 | 版の head のコミット |

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

## event

| 列 | 型 | 内容 |
|---|---|---|
| `seq` | 整数 | repository ごとに 1 から順に増える番号 |
| `kind` | 文字列 | 変化の種類（例：`apply_request.reviewed`） |
| `subject_id` | 識別子 | 変化した対象 |
| `actor_session_id` | 文字列 | 起こしたセッション |
| `occurred_at` | 時刻 | 起きた時刻 |

- 行を加えるだけにする。DB のトリガーで、UPDATE と DELETE を拒む。
- バックエンドは、この表を読んで [webhook](../api/webhook.md) を送る。
