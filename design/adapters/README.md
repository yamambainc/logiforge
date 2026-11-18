# アダプタ

バックエンドのポートを、何で実装するかを決めるときと、アダプタを作るときに読む。
ポートの定義は、[ポート](../backend/ports/README.md) が正本である。

## 一覧

| ポート | アダプタ | ページ |
|---|---|---|
| GitForge | GitHub アダプタ、Forgejo アダプタ | [GitHub アダプタ](github.md)、[Forgejo アダプタ](forgejo.md) |
| スナップショットの置き場 | 静的ファイルストレージ、手元のディレクトリ | [スナップショットの置き場のアダプタ](snapshot-store.md) |
| 認証 | GitHub アダプタ、Forgejo アダプタ | [GitHub アダプタ](github.md)、[Forgejo アダプタ](forgejo.md) |
| 設定の DB | SQLite | [設定の DB](../settings/database.md) |

- [worker](../backend/worker/README.md) が git を読むやり方は、[git アダプタ](git.md) に書く。
- GitHub アダプタと Forgejo アダプタに共通するところは、[GitForge アダプタの共通の書き方](pull-requests.md) と [記録のコメントとレビュー](records.md) に書く。
- 構成ごとの設定と秘密は、[設定](../settings/README.md) と [秘密の暗号化](../settings/secrets.md) に書く。

## GitForge アダプタに求めること

バックエンドとクライアントは、GitForge に次の性質を求める。どの GitForge でも、アダプタがこれを満たす。

- pull request を、head の SHA を指定して squash merge できる。
- merge したコミットから、その pull request を API で引ける（[反映かの判定](../backend/verify/README.md)）。
- 主体のトークンで、pull request、レビュー、コメント、issue を書ける。
- git の HTTP の Basic 認証のパスワードに、GitForge のトークンを入れて読み書きできる（[CLI](../cli/README.md)）。
- pull request の最後の先頭のコミットを、ref に残す。

GitForge ごとに違う性質は、次のとおりである。

| 性質 | GitHub | Forgejo |
|---|---|---|
| API でコメントにファイルを添付できるか（[人への依頼の回答](../files/artifacts.md)） | できない。添付はリンクだけ | できる |
| repository をバックエンドが作れるか（[repository の登録](#repository-の登録)） | 作らない。管理者が GitHub で作り、利用者が登録する | 作る |
| pull request の最後の先頭のコミットの ref | `refs/pull/<number>/head` | `refs/pull/<index>/head` |

## 組み合わせ

```mermaid
flowchart TB
  subgraph F["GitForge"]
    GA["GitHub アダプタ"] ==> GH[("GitHub")]
    FA["Forgejo アダプタ"] ==> FJ[("Forgejo")]
  end
  subgraph S["置き場"]
    SA["静的ファイルストレージのアダプタ"] ==> OBJ[("静的ファイルストレージ")]
    DA["手元のディレクトリのアダプタ"] ==> DIR[("手元のディレクトリ")]
  end
  W["worker"] ==> GH
  W ==> FJ
  W ==> OBJ
  W ==> DIR
```

- 太い矢印は、アダプタと worker が外のサービスを読み書きすることを表す。
- worker は、外のサービスではなく、バックエンドと並ぶ Logiforge のプロセスである（[レイヤー構成](../architecture.md)）。図では、外のサービスとのつながりを見せるために、アダプタと並べた。
- GitForge のアダプタと、置き場のアダプタは、それぞれ 1 つを選ぶ。
- 構成（GitHub を使う構成か、Forgejo を使う構成か）は、設定の DB で 1 つに決める（[初回のセットアップ](../settings/setup.md)）。
  - なぜ：repository ごとに構成を選べると、組み合わせが増えて単純さを失うから。
- 置き場のアダプタは、構成と別に選ぶ。どちらの構成でも、どちらの置き場でもよい。
  - 手元のディレクトリ：バックエンドと worker を並べた 1 つのタスクで動かすときに使う。置き場を、タスクの中で共有するボリュームにする（[タスクの形](../deploy/README.md#タスクの形)）。
  - 静的ファイルストレージ：バックエンドを何台か並べるときに使う。どのプロセスからも読めるから（[バックエンドだけを増やすとき](../deploy/README.md#バックエンドだけを増やすとき)）。
- worker は、どちらの構成でも同じ実装である。リモートの URL と認証情報だけが違う。worker は git を読み、置き場に書く。
- 置き場を読むのは、スナップショットの置き場のアダプタ（バックエンド）である。
- git に書くのは、GitHub アダプタか Forgejo アダプタが、GitForge の API で行う repository の用意と pull request の merge だけである。
- GitForge と認証のポートは、同じアダプタが実装する。
  - なぜ：どちらの正本も、同じ GitForge にあるから。
- GitHub の PR（pull request）や issue として、apply request や人への依頼を見たいなら、GitHub を使う構成にする。
- GitHub を使わずに動かしたいなら、Forgejo を使う構成にする。Forgejo を使う理由は、[Forgejo アダプタ](forgejo.md#forgejo-を使う理由) に書く。

正本とバックアップは、次のとおりである。

| もの | バックアップ |
|---|---|
| GitHub | GitHub に任せる |
| Forgejo | Forgejo のバックアップの仕組み（`forgejo dump` など）で取る |
| 置き場の `keep/`（静的ファイルストレージ） | ストレージの仕組みで取る |
| 置き場の `keep/`（手元のディレクトリ） | ディレクトリを写して取る |
| 設定の DB | [設定のバックアップ](../settings/README.md#バックアップ) に従う |

- どちらの構成でも、バックエンドはデータを保持しない（[状態を持たない](../backend/README.md)）。
- worker の clone と、置き場の多くは、正本ではない。失っても、worker が git から作り直す。そのため、バックアップは要らない。
- ただし、置き場の `keep/` は、git から作り直せないものを持つので、バックアップを取る。`keep/` に何を置くかは、[スナップショットの置き場](../backend/ports/snapshot-store.md) に書く。

## repository の登録

repository の名前の正本は、構成ごとに次のとおりである。

- GitHub を使う構成：設定の DB（repository の名前と `owner/name` の組。利用者が設定の画面で登録する）
  - 管理者が GitHub リポジトリを作り、利用者がこの組を登録する（[GitHub アダプタ](github.md)、[設定の画面](../../docs/webui/settings.md)）。
  - gitforge_ref は `owner/name` である。
  - 設定に登録されていない名前では、repository を作れない（`repository_not_registered`）。
- Forgejo を使う構成：設定した organization の、Forgejo の repository の一覧
  - 利用者が作るときに、バックエンドが、利用者のトークンで Forgejo に repository を作る（[Forgejo アダプタ](forgejo.md)）。
  - gitforge_ref は、`<organization>/<名前>` である。
  - 設定の画面での登録は要らない。そのため、`repository_not_registered` は起きない。

- ディレクトリの repository は、どちらの構成でも、設定の DB（repository の名前と、git リポジトリとディレクトリのパスの組。利用者が設定の画面で登録する）が正本である（[ディレクトリ方式](../directory/README.md)）。

- 利用者は、名前と根の主文だけで repository を作る。
