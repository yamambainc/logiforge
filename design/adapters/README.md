# アダプタ

バックエンドのポートを、何で実装するかを決めるときと、アダプタを作るときに読む。
ポートの定義は、[ポート](../backend/ports/README.md) が正本である。

## 一覧

| ポート | アダプタ | ページ |
|---|---|---|
| やりとり | GitHub アダプタ、Forgejo アダプタ | [GitHub アダプタ](github.md)、[Forgejo アダプタ](forgejo.md) |
| スナップショットの置き場 | 静的ファイルストレージ、手元のディレクトリ | [スナップショットの置き場のアダプタ](snapshot-store.md) |

- [worker](../backend/worker.md) が git を読むやり方は、[git アダプタ](git.md) に書く。
| 認証 | GitHub アダプタ、Forgejo アダプタ | [GitHub アダプタ](github.md)、[Forgejo アダプタ](forgejo.md) |

## 組み合わせ

```mermaid
flowchart TB
  subgraph G["GitHub を使う構成"]
    G2["GitHub アダプタ"] ==> GH[("GitHub")]
    G3["静的ファイルストレージのアダプタ"] ==> OBJ[("静的ファイルストレージ")]
    GW["worker"] ==> GH
    GW ==> OBJ
  end
  subgraph N["Forgejo を使う構成"]
    N2["Forgejo アダプタ"] ==> NF[("Forgejo")]
    N3["手元のディレクトリのアダプタ"] ==> DIR[("手元のディレクトリ")]
    NW["worker"] ==> NF
    NW ==> DIR
  end
```

- 太い矢印は、アダプタが外のサービスを読み書きすることを表す。
- 構成は、サーバの設定で 1 つに決める。
  - なぜ：repository ごとに構成を選べると、組み合わせが増えて単純さを失うから。
- worker は、どちらの構成でも同じ実装である。リモートの URL と認証情報だけが違う。worker は git を読み、置き場に書く。
- 置き場を読むのは、スナップショットの置き場のアダプタ（バックエンド）である。
- git に書くのは、GitHub アダプタか Forgejo アダプタが、プロバイダの API で行う repository の用意と pull request の merge だけである。
- やりとりと認証は、同じアダプタが実装する。
  - なぜ：どちらの正本も、同じ外のサービス（GitHub か Forgejo）にあるから。
- GitHub で PR（pull request）や issue としてやりとりを見たいなら、GitHub を使う構成にする。
- GitHub を使わずに動かしたいなら、Forgejo を使う構成にする（[Forgejo アダプタ](forgejo.md)）。
- GitHub を使う構成は、複数人で使い、バックエンドを何台か並べることを想定する。そのため、置き場を、どのプロセスからも読める静的ファイルストレージにする。
- Forgejo を使う構成は、1 台のマシンで使うことを想定する。Forgejo、worker、バックエンドの 3 つのプロセスを動かし、置き場を手元のディレクトリにする。
- どちらの構成でも、[worker](../backend/worker.md) が置き場に書き、バックエンドが読む。

構成ごとの正本とバックアップは、次のとおりである。

| 構成 | 正本の置き場所 | バックアップ |
|---|---|---|
| GitHub を使う構成 | GitHub | GitHub に任せる |
| Forgejo を使う構成 | Forgejo | Forgejo のバックアップの仕組み（`forgejo dump` など）で取る |

- どちらの構成でも、バックエンドはデータを保持しない（[状態を持たない](../backend/README.md)）。
- worker の clone と、置き場の多くは、正本ではない。失っても、worker が git から作り直す。そのため、バックアップは要らない。
- ただし、置き場の `branches/` と、消えたブランチの版のスナップショットは、git から作り直せない。ここだけはバックアップを取る（[ブランチの履歴](../backend/snapshot.md)）。

## repository の登録

repository の名前の正本は、構成ごとに次のとおりである。

- GitHub を使う構成：サーバの設定（repository の名前と `owner/name` の組）
  - 管理者が GitHub リポジトリを作り、書き込み主体を入れて、この組を登録する（[GitHub アダプタ](github.md)）。
  - provider_ref は `owner/name` である。
  - 設定に登録されていない名前では、repository を作れない（`repository_not_registered`）。
- Forgejo を使う構成：設定した organization の、Forgejo の repository の一覧
  - 利用者が作るときに、バックエンドが Forgejo に repository を作る（[Forgejo アダプタ](forgejo.md)）。
  - provider_ref は、`<organization>/<名前>` である。
  - 管理者の登録は要らない。そのため、`repository_not_registered` は起きない。

- 利用者は、名前と根の主文だけで repository を作る。

## 設定と秘密情報

次のものは、設定ファイルか環境変数に置く。git リポジトリには置かない。

- なぜ：git リポジトリは、人やエージェントが読めるから。

バックエンド：

- 構成（GitHub を使う / Forgejo を使う）
- worker の内部 API の宛先と、内部 API の鍵（[worker の内部 API](../backend/worker-api.md)）
- 置き場の接続先と、読む鍵（[スナップショットの置き場のアダプタ](snapshot-store.md)）
- GitHub を使う構成だけ
  - GitHub の書き込み主体の認証情報（GitHub App の秘密鍵）
  - `user` とみなす GitHub のアカウント名か、organization の team（[GitHub アダプタ](github.md)）
  - repository の名前と `owner/name` の組
- Forgejo を使う構成だけ
  - Forgejo の URL
  - 書き込み主体（Forgejo のユーザー）のトークン（[Forgejo アダプタ](forgejo.md)）
  - repository を置く organization と、`user` とみなす team

[worker](../backend/worker.md)：

- 構成と、repository の名前と `owner/name` の組（GitHub を使う構成だけ）
- git を読み、repository の一覧を読む、読むだけの資格情報（[git アダプタ](git.md)）
  - GitHub を使う構成：contents の read だけを持つ、worker のための GitHub App の秘密鍵
  - Forgejo を使う構成：`read:repository` だけを持つ、worker のためのユーザーのトークン
- 内部 API の鍵（バックエンドと同じもの）
- 置き場の接続先と、書く鍵
- 巡回の間隔と、内部 API を受ける HTTP のエンドポイント
- clone を置く、永続するストレージのパス

鍵を置く場所（バックエンドと worker を動かす環境）の管理者を信頼する。

- ほかの主体には、鍵を置く場所を読ませない。
- worker は、git とプロバイダに書く鍵を持たない。
