# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、[やりとり](../backend/ports/interaction.md) のポートをどう実装するかを確かめるために読む。
木の書き込みは、[git アダプタ](git.md) が、GitHub リポジトリをリモートとして扱う。

## 前提：main を守る設定

- `main` を守る設定（ルールセットかブランチ保護）は、必須である。管理者が用意する（[main への書き込みを守る](github-main.md)）。
  - `main` に入るのは、App による pull request の merge だけにする。
- 守る設定がないと、反映によらない変更が `main` に入りうる。[worker](../backend/worker.md) は、それを見つけたら警告する。

## 書き込み主体

GitHub アダプタに必要なのは、バックエンドだけが使う、専用の書き込み主体である。

- なぜ：GitHub に書き込む認証情報が要るから。
- なぜ：`main` に merge できる主体を、バックエンドだけに絞るから。

書き込み主体には、GitHub App を使う。以下では「App」と書く。

- 権限を細かく絞れる。人に紐づかない。席を消費しない。

## 対応

| やりとりの操作 | GitHub での実装 |
|---|---|
| repository | GitHub リポジトリ（1 つの repository に 1 つ）。provider_ref は `owner/name` |
| repository を用意する | 空のリポジトリに、contents API（`PUT /repos/{owner}/{repo}/contents/{path}`）で最初のコミットを作る |
| 提案 | 利用者のブランチから `main` への pull request。App が作る |
| 提案の識別子と、作ったときの先頭 | pull request の本文の末尾の trailer `Logiforge-Apply-Request:` と `Logiforge-First-Revision:` |
| 版 | pull request の head のコミット（[版](../backend/apply-request.md)） |
| 反映する | `PUT /repos/{owner}/{repo}/pulls/{number}/merge` を、`sha`（版のコミット）、`merge_method: squash`、メッセージを付けて呼ぶ |
| close | pull request を close する。理由とセッションは、コメントに書く |
| 反映の失敗 | バックエンドが書く check run（`logiforge/core`）の結果 |
| レビュー | `event=COMMENT` の pull request review。`commit_id` に版のコミットを入れる |
| 人への依頼 | issue（ラベル `logiforge/pvr` か `logiforge/decision`） |
| 人への依頼の識別子 | issue の本文の末尾の trailer `Logiforge-Human-Request:` |
| 回答 | App が、利用者の回答として書いたコメント |
| 回答の添付 | 静的ファイルストレージの `<repository>/answers/<人への依頼の識別子>/<sha256>`。バックエンドが置く（[人への依頼の回答](../files/artifacts.md)） |
| 一覧のカーソル | pull request と issue の number。時刻は使わない |

- 1 つのブランチに、open の pull request は 1 つまでである。GitHub も、同じ head と base の pull request を 2 つ開けない。
- 利用者がブランチを消すと、GitHub は pull request を閉じる。バックエンドは、それを close として扱う。
- 根の仮説に背景情報があるときは、contents API が 1 回に 1 つのファイルしか書けないので、2 つ目のコミットで足す。
  - 用意のコミットには、trailer `Logiforge-Init:` を付ける。worker は、これを反映によらない変更として扱わない。
- GitHub アダプタを作るときに、次のことを確かめる。
  - App が、`main` を守る設定の例外として、pull request を merge できるか
  - 空のリポジトリに、contents API で最初のコミットを作れるか

## repository を用意する

1. 管理者が、空の GitHub リポジトリを作る。
2. 管理者が、その GitHub リポジトリに App を入れる。
3. 管理者が、利用者とエージェントに、ブランチを push できる権限を与え、`main` を守る設定を用意する（[main への書き込みを守る](github-main.md)）。
4. 管理者が、repository の名前と `owner/name` の組を、サーバの設定に登録する（[repository の登録](README.md)）。この組が、名前の正本である。
5. 利用者が、名前と根の主文で repository を作る。バックエンドは、[repository の用意](../backend/README.md) の手順で、最初のコミットを置く。

- App は、organization に GitHub リポジトリを作らない。
  - なぜ：App に、organization の Administration write という強い権限を持たせないため。

## バックエンドの App が作ったものだけを数える

- 提案は、App が作った pull request だけを数える。
- レビューは、作者が App のものだけを数える。
- 回答は、App が書いたコメントだけを数える。
- なぜ：セッション ID とプロンプトのリビジョンを、バックエンドを通したものだけに限るため（[保証の範囲](../../docs/apply-request/review.md)）。

## 認証

GitHub を使う構成では、[認証](../backend/ports/authentication.md) のポートも、このアダプタが実装する。認証は GitHub に任せる。

- 主体は、GitHub のアカウント（人）と、GitHub App または機械用アカウント（エージェント、外の仕組み）である。
- API は GitHub のトークンを受け取り、要求ごとに GitHub に問い合わせて主体を確かめる。結果を保持しない。
- 利用者は、WebUI で、書き込み主体と同じ GitHub App のユーザー認可（user-to-server トークン）でログインする。OAuth App は使わない。
- 主体の種類は、管理者が主体ごとに決める。
  - `user` とみなすのは、user-to-server トークンを持ち、かつ管理者がサーバの設定に登録した人（GitHub のアカウント名か、organization の team）だけである。
  - それ以外の主体（gh の CLI のトークン、App のトークンなど）は、`client` である。
- GitHub への問い合わせは、主体のトークンで行う。そのため、主体ごとの上限に数え、App の上限には数えない。
- 読める repository は、主体のトークンで、サーバの設定に登録した GitHub リポジトリの権限を確かめて求める。
  - `main` に merge できるのは、書き込み主体（App）だけにする（[main への書き込みを守る](github-main.md)）。
- どの操作を使えるかの判定は、バックエンドの [認可](../backend/README.md) が行う。
- そのため、この構成では、サーバに正本が残らない（[組み合わせ](README.md)）。

## コメントとレビューの書き方

- レビューは、すべて `event=COMMENT` の review として書く。
  - なぜ：pull request を作った App は、その pull request を approve できないから。
- レビューの本文の末尾に、次の trailer を書く。
  - `Logiforge-Review-Id:`（レビューの識別子）
  - `Logiforge-Review-Kind: approve|request_changes|comment`
  - `Logiforge-Session-Id:`
  - `Logiforge-Prompt-Revision:`
- GitHub 上の approve の状態は、反映の条件に使わない。
- 提案の close と、人への依頼の起票では、App が書くコメントの末尾に `Logiforge-Session-Id:` を書く。
- 人への依頼の issue の本文の先頭に、対象ノードの識別子を書く。
- 「回答は 1 回だけ」の判定は、バックエンドが行う。
  - 回答を書いたら、バックエンドは issue に `logiforge/answered` ラベルを付けて閉じる。

`main` への書き込みの守り方と、GitHub の制約への対処は、[GitHub での main の守りと制約](github-main.md) に書く。
