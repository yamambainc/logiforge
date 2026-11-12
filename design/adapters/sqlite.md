# SQLite アダプタ

[状態ストア](../backend/ports/state-store.md) のポートを、SQLite でどう実装するかを確かめるときに読む。

## 選んだ理由

- 状態ストアの既定は、SQLite とする。
  - なぜ：反映は repository ごとに直列に行い、書き込みが少ないので、1 本の書き込みで足りるから。

## event

| 列 | 型 | 内容 |
|---|---|---|
| `repository_name` | 文字列 | repository |
| `seq` | 整数 | repository ごとに 1 から順に増える番号 |
| `kind` | 文字列 | 変化の種類（例：`apply_request.reviewed`） |
| `subject_id` | 識別子 | 変化した対象 |
| `actor_session_id` | 文字列 | 起こしたセッション |
| `occurred_at` | 時刻 | 起きた時刻 |

- 行を加えるだけにする。DB のトリガーで、UPDATE と DELETE を拒む。

## そのほかの表

| 表 | 列 |
|---|---|
| `repository` | `repository_name`（主キー）、`provider_ref`、`created_at` |
| `idempotency_key` | `key`、`principal_id`、`response`、`created_at` |
| `principal` | `principal_id`、`kind`（`user` / `client`） |
| `token` | `token_hash`、`principal_id`、`created_at` |
| `reconcile_state` | `repository_name`、`last_merged_revision`、`last_reconciled_at` |

- トークンは、ハッシュだけを持つ。
