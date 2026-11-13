# SQLite アダプタ

GitHub を使わない構成で、[サーバの状態](../backend/ports/server-state.md) と [認証](../backend/ports/authentication.md) のポートを、SQLite でどう実装するかを確かめるときに読む。

## 選んだ理由

- GitHub を使わない構成では、認証の主体とトークンという正本を、サーバが持つ。
  - そのため、サーバの状態もメモリではなく、同じ SQLite ファイルに置く。
- 反映は repository ごとに直列に行い、書き込みが少ないので、1 本の書き込みで足りる。
- 独自プロバイダのテーブルも、同じ SQLite ファイルに置く（[独自プロバイダ](native.md)）。

## サーバの状態

| テーブル | 列 |
|---|---|
| `repository` | `repository_name`（主キー）、`provider_ref`（キャッシュ） |
| `idempotency_key` | `key`、`principal_id`、`response`、`created_at` |
| `reconcile_state` | `repository_name`、`last_reconciled_main`、`last_reconciled_at`（キャッシュ） |

## 認証

[認証](../backend/ports/authentication.md) のポートは、次のテーブルで実装する。

| テーブル | 列 |
|---|---|
| `principal` | `principal_id`、`kind`（`user` / `client`） |
| `token` | `token_hash`、`principal_id`、`created_at` |

- この 2 つのテーブルは正本である（[組み合わせ](README.md)）。
- 主体の種類（`kind`）は、管理者が主体ごとに決める。
- トークンは、ハッシュだけを持つ。
- GitHub を使わない構成では、すべての主体が、すべての repository を読める。
