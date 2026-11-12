# SQLite アダプタ

[サーバの状態](../backend/ports/server-state.md) のポートを、SQLite でどう実装するかを確かめるときに読む。

## 選んだ理由

- サーバの状態の既定は、SQLite とする。
  - なぜ：反映は repository ごとに直列に行い、書き込みが少ないので、1 本の書き込みで足りるから。
- 独自プロバイダを使うときは、そのテーブルも同じ SQLite ファイルに置く（[独自プロバイダ](native.md)）。

## event

| 列 | 型 | 内容 |
|---|---|---|
| `repository_name` | 文字列 | repository |
| `seq` | 整数 | repository ごとに 1 から順に増える番号 |
| `kind` | 文字列 | 変化の種類（例：`apply_request.reviewed`） |
| `subject_id` | 識別子 | 変化した対象 |
| `actor_session_id` | 文字列 | 起こしたセッション |
| `occurred_at` | 時刻 | 起きた時刻 |

- 行を加えるだけにする。DB（データベース）のトリガーで、UPDATE と DELETE を拒む。
- 作り直すときだけ、テーブルごと消して入れ直す（[サーバの状態](../backend/ports/server-state.md)）。

## そのほかのテーブル

| テーブル | 列 |
|---|---|
| `repository` | `repository_name`（主キー）、`provider_ref`（キャッシュ） |
| `idempotency_key` | `key`、`principal_id`、`response`、`created_at` |
| `reconcile_state` | `repository_name`、`last_merged_revision`、`last_reconciled_at`（キャッシュ） |

- トークンは、ハッシュだけを持つ。

## 認証

GitHub を使わない構成では、[認証](../backend/ports/authentication.md) のポートも、このアダプタが実装する。

| テーブル | 列 |
|---|---|
| `principal` | `principal_id`、`kind`（`user` / `client`） |
| `token` | `token_hash`、`principal_id`、`created_at` |

- この 2 つのテーブルは正本である（[構成ごとの正本](../backend/ports/server-state.md)）。
- トークンは、ハッシュだけを持つ。
- GitHub を使わない構成では、すべての主体が、すべての repository を読める。
