# 設定の DB

設定の DB の種類と、そこに置く表を確かめるときに読む。
何を置くかは、[設定](README.md#設定の-db-に置くもの) に書く。

## 置くものと置かないもの

- 設定の DB に置くのは、設定だけである。
- 木、apply request、人への依頼、スナップショットは置かない。
  - なぜ：それらの正本は GitForge と置き場にあるから（[状態を持たない](../backend/README.md#状態を持たない)）。
- サインインのトークンも置かない（[サインイン](../webui/auth.md#トークンの持ち方)）。

## DB の種類

SQLite にする。

- なぜ：
  - バックエンドと worker は、もともと 1 つのタスクで、置き場のボリュームを共有して動くから（[タスクの形](../deploy/README.md#タスクの形)）。同じボリュームに DB のファイルを置けば、ほかのサービスが要らない。
  - 1 台の Linux（[1 台の Linux に置く](../../docs/adoption/linux-host/README.md)）でも、compose でも、そのまま動くから。
- WAL のモードで開く。バックエンドは読み書きし、worker は読むだけで開く。
- DB のファイルは、NFS などの共有のボリュームに置かない。
  - なぜ：ネットワークのファイルシステムでは、SQLite のロックが効かないことがあるから。

選ばなかった案：PostgreSQL。1 台で動かすときにも、もう 1 つのサービスを動かすことになるから。

- 設定の DB は、ポートの奥に置く。
  - なぜ：バックエンドを複数のノードに並べるときに（[バックエンドだけを増やすとき](../deploy/README.md#バックエンドだけを増やすとき)）、ほかの DB のアダプタを足せば済むから。

## 表

| 表 | 列 | 中身 |
|---|---|---|
| `settings` | `name`、`value`、`encrypted`、`updated_at`、`updated_by` | 名前と値の組。秘密は暗号文（[秘密の暗号化](secrets.md)） |
| `repositories` | `name`、`gitforge_ref`、`directory` | 登録した repository（GitHub リポジトリの repository と、ディレクトリの repository）。Forgejo の構成のふつうの repository は置かない（[repository の登録](../adapters/README.md#repository-の登録)） |
| `users` | `kind`（`login` か `team`）、`value` | `user` とみなすアカウント名と team |
| `admins` | `login`、`added_by`、`added_at` | 管理者（[管理者](admins.md)） |
| `record_keys` | `key_id`、`public_key`、`private_key`、`created_at`、`primary` | 記録の署名の鍵。`private_key` は暗号文 |
| `setup` | `code_hash`、`created_at`、`done_at` | 初回のセットアップ（[初回のセットアップ](setup.md)） |

- 表の定義は、実装の migration が正本である。
- `updated_by` と `added_by` は、変えた管理者のアカウント名である。
  - なぜ：誰が設定を変えたかを、あとから辿れるようにするため。
- 値の形を変えるときは、DB の中に版を持ち、起動のときに移す。
