# テーブル

独自フォージが、やりとりと主体を、どのテーブルに置くかを確かめるときに読む。
列の定義は、独自フォージの実装の schema が正本である。このページには、列の意味だけを書く。

## 決まり

- やりとりのテーブルは、すべて `repository_name` を持つ。
- 作った行は変えない。例外は、次のものだけである。
  - `apply_request` の `state`、`closed_reason`、`closed_by_session_id` を、close か反映のときに 1 度だけ埋める。
  - `apply_request` の `description` を、更新の API で書き換える。
    - なぜ：説明欄は、レビューの材料にも、版の差分にも含まないから（[レビューの材料](../../docs/apply-request/review.md)）。
- 識別子は、バックエンドが決めたものを、そのまま使う（[やりとり](../backend/ports/interaction.md)）。
- 一覧は、行を作った順に返す。識別子の順ではない（[ページング](../api/README.md)）。

## apply_request

| 列 | 内容 |
|---|---|
| `id` | `apr_` と 26 文字（[識別子](../backend/ports/interaction.md)） |
| `description` | 説明欄 |
| `state` | `open` / `merged` / `closed` |
| `closed_reason` | close の理由 |
| `closed_by_session_id` | close したセッション |

## apply_request_version

| 列 | 内容 |
|---|---|
| `apply_request_id` | 属する提案 |
| `version` | 版の番号。バックエンドが決める。提案の中で一意 |
| `revision` | 版のリビジョン |

- 版を書いたセッションは、コミットの trailer にある。テーブルには持たない（[記録の形式](../files/revisions.md)）。

## review

| 列 | 内容 |
|---|---|
| `id` | `rev_` と 26 文字 |
| `apply_request_id` | 対象の提案 |
| `version` | レビューした版 |
| `kind` | `approve` / `request_changes` / `comment` |
| `body` | 本文 |
| `reviewer_session_id` | reviewer のセッション |
| `prompt_revision` | 使ったプロンプトのリビジョン |

## merge_failure

| 列 | 内容 |
|---|---|
| `apply_request_id` | 反映に失敗した提案 |
| `version` | 失敗した版 |
| `reason` | `stale_target` / `invariant_violation` |
| `detail` | 失敗した操作と条件 |

## human_request と human_request_answer

| テーブル | 列 |
|---|---|
| `human_request` | `id`（`hrq_` と 26 文字）、`node_id`、`kind`（`pvr` / `decision`）、`body`、`created_by_session_id`、`created_at` |
| `human_request_answer` | `human_request_id`（主キー）、`body`、`answered_by_principal_id`、`answered_at` |

- `human_request_answer` の主キーで、回答を 1 回だけにする。
- 人への依頼の状態は保存しない。回答の行があれば `answered` である。

## principal と token

| テーブル | 列 |
|---|---|
| `principal` | `principal_id`、`kind`（`writer` / `user` / `client`） |
| `token` | `token_hash`、`principal_id`、`created_at` |

- 主体の種類は、管理者が主体ごとに決める（[主体](README.md)）。
- トークンは、ハッシュだけを持つ。
