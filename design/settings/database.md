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
- ジャーナルは、rollback journal（`journal_mode=DELETE`）にする。WAL にしない。
  - なぜ：WAL では、読むだけの接続も `-shm` のファイルに書くので、worker に書く権限を渡すことになるから。設定は書くことが少ないので、WAL の速さは要らない。
- worker は、`mode=ro` で開き、読むたびにトランザクションの中で読む。
  - rollback journal では、読むトランザクションは共有のロックを取り、書く側はそれが外れるのを待つ。そのため、バックエンドが書いている最中でも、worker は書く前か書いたあとのどちらかを、まとめて読む。
  - 書く側がロックを持っているあいだは `SQLITE_BUSY` になるので、`busy_timeout`（5 秒）を付けて待つ。
  - バックエンドが書く途中で落ちて、ホットジャーナル（書きかけの `-journal`）が残ると、読むだけの接続は戻せず、誤りになる。worker は、ログに出して次の読み直しまで待つ。バックエンドが次に開いたときに戻す。
- バックエンドは読み書きする。worker は、OS の権限で、読むことしかできない。
  - DB のファイルは、バックエンドだけのボリュームかディレクトリに置き、worker には読むだけでマウントする（別の OS のユーザーで動かすときは、ファイルの権限で読むだけにする）。
  - なぜ：worker が設定の DB に書けると、乗っ取った者が、記録の署名の公開鍵、repository の一覧、WebUI の URL を書き換えられるから。鍵を 2 つに分けても、守れない（[秘密の暗号化](secrets.md#2-つの鍵)）。
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
| `record_keys` | `key_id`、`public_key`、`private_key`、`created_at`、`primary` | 記録の署名の鍵。`private_key` は暗号文 |
| `worker_tokens` | `owner`、`token`、`expires_at`、`repositories` | worker のための installation token（[worker のトークン](../adapters/github-app.md#worker-のトークン)）。`token` は暗号文 |
| `setup` | `code_hash`、`created_at`、`done_at` | 初回のセットアップ（[初回のセットアップ](setup.md)） |

- 表の定義は、実装の migration が正本である。
- `updated_by` は、変えた利用者のアカウント名である。
  - なぜ：誰が設定を変えたかを、あとから辿れるようにするため。
- 値の形を変えるときは、DB の中に版を持ち、起動のときに移す。
