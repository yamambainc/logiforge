# pull request のプロバイダの共通の書き方

GitHub アダプタと Forgejo アダプタが、[やりとり](../backend/ports/interaction.md) のポートを、同じ形で実装するところを確かめるときに読む。
プロバイダごとの違いは、[GitHub アダプタ](github.md) と [Forgejo アダプタ](forgejo.md) に書く。

## 対応

| やりとりの操作 | 実装 |
|---|---|
| 提案 | 利用者のブランチから `main` への pull request。書き込み主体が作る |
| 提案の識別子と、作ったときの先頭 | pull request の本文の末尾の trailer `Logiforge-Apply-Request:` と `Logiforge-First-Revision:` |
| 版 | pull request の head のコミット（[版](../backend/apply-request.md)） |
| 反映する | pull request の merge。squash を選び、head の SHA とメッセージを渡す |
| close | pull request を close する。理由とセッションは、コメントに書く |
| レビュー | コメントの種類の review。版のコミットを `commit_id` に入れる |
| 人への依頼 | issue（ラベル `logiforge/pvr` か `logiforge/decision`） |
| 人への依頼の識別子 | issue の本文の末尾の trailer `Logiforge-Human-Request:` |
| 回答 | 書き込み主体が、利用者の回答として書いたコメント |
| 一覧のカーソル | pull request と issue の番号。時刻は使わない |

- 1 つのブランチに、open の pull request は 1 つまでである。
- 利用者がブランチを消すと、プロバイダは pull request を閉じる。バックエンドは、それを close として扱う。

## コメントとレビューの書き方

- レビューは、すべてコメントの種類の review として書く。
  - なぜ：pull request を作った書き込み主体は、その pull request を approve できないプロバイダがあるから。プロバイダの approve の状態は、反映の条件に使わない。
- レビューの本文の末尾に、次の trailer を書く。
  - `Logiforge-Review-Id:`（レビューの識別子）
  - `Logiforge-Review-Kind: approve|request_changes|comment`
  - `Logiforge-Session-Id:`
  - `Logiforge-Prompt-Revision:`
- 提案の close と、人への依頼の起票では、書き込み主体が書くコメントの末尾に `Logiforge-Session-Id:` を書く。
- 人への依頼の issue の本文の先頭に、対象ノードの識別子を書く。
- 「回答は 1 回だけ」の判定は、バックエンドが行う。
  - 回答を書いたら、バックエンドは issue に `logiforge/answered` ラベルを付けて閉じる。

## 書き込み主体が作ったものだけを数える

- 提案は、書き込み主体が作った pull request だけを数える。
- レビューは、作者が書き込み主体のものだけを数える。
- 回答は、書き込み主体が書いたコメントだけを数える。
- なぜ：セッション ID とプロンプトのリビジョンを、バックエンドを通したものだけに限るため（[保証の範囲](../../docs/apply-request/review.md)）。

## main を守る

- `main` を守る設定は、必須である。
  - `main` への push を禁じ、pull request の merge だけにする。
  - merge できるのは、書き込み主体だけにする。
  - `main` に追いついていることを merge の条件にする設定は、使わない（[衝突](../backend/merge.md)）。
- 守る設定が外れて、反映によらない変更が `main` に入ったら、[worker](../backend/worker.md) が見つけて警告する。
- 信頼の境界：ブランチに push できる主体は、他人のブランチも消せる。プロバイダは、ブランチが消えると pull request を閉じるので、他人の open の apply request を close にできる。
  - 消されても、反映済みの apply request は壊れない（[ブランチの掃除](../backend/apply-request.md)）。
