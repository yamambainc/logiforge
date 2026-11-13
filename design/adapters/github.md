# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、[GitForge](../backend/ports/gitforge.md) のポートをどう実装するかを確かめるために読む。
Forgejo アダプタと共通のところ（pull request の対応、レビューとコメントの書き方、書き込み主体が作ったものだけを数えること、main を守ること）は、[GitForge アダプタの共通の書き方](pull-requests.md) に書く。
[worker](../backend/worker.md) は、[git アダプタ](git.md) で、GitHub リポジトリを読む。

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

GitHub に固有のところだけを書く。

| GitForge のポートの操作 | GitHub での実装 |
|---|---|
| repository | GitHub リポジトリ（1 つの repository に 1 つ）。provider_ref は `owner/name` |
| repository を用意する | 空のリポジトリに、contents API（`PUT /repos/{owner}/{repo}/contents/{path}`）で最初のコミットを作る |
| 反映する | `PUT /repos/{owner}/{repo}/pulls/{number}/merge` を、`sha`（版のコミット）、`merge_method: squash`、メッセージを付けて呼ぶ |
| レビュー | `event=COMMENT` の pull request review |
| 反映の失敗 | バックエンドが書く check run（`logiforge/core`）の結果 |
| 回答の添付 | リンクだけ。ファイルは受け付けない。Forgejo を使う構成と違い、GitHub の構成では、利用者がファイルを外に置いてリンクを書く（[人への依頼の回答](../files/artifacts.md)） |

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

GitHub での `main` の守り方と、GitHub の制約への対処は、[GitHub での main の守りと制約](github-main.md) に書く。
