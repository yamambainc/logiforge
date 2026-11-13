# Forgejo アダプタ

自前で持つ構成で、[Forgejo](https://forgejo.org/) を使って Logiforge を動かすときに、[GitForge のポート](../backend/ports/gitforge.md) と [認証](../backend/ports/authentication.md) のポートをどう実装するかを確かめるために読む。
GitHub アダプタと共通のところは、[GitForge アダプタの共通の書き方](pull-requests.md) に書く。

## Forgejo を使う理由

- 自前で持つ構成の GitForge に、Forgejo を使う。pull request、レビュー、issue、添付、ブランチの保護、トークン、API を、Forgejo がすでに持つ。
  - なぜ：これらを自前で作らずに済み、GitHub を使う構成と同じ pull request の形で扱えるから。
- Forgejo は、別のプロセス（単体のバイナリかコンテナ）として動かす。Logiforge は、Forgejo の HTTP の API と git だけを使う。
- Forgejo のライセンスは GPL である。Logiforge は、Forgejo のコードをリンクせず、別のプロセスとして使う。

## 書き込み主体と主体

| 主体 | 使う者 | 権限 |
|---|---|---|
| 書き込み主体（Forgejo のユーザー） | バックエンド | organization で repository を作り、pull request を作り・merge し・close し、レビューとコメントを書く |
| 読むだけのユーザー | [worker](../backend/worker.md) | repository を読む。トークンの範囲は `read:repository` だけにする。organization が private なら、読むだけの team に入れる |
| 利用者とエージェント | CLI、WebUI | repository を読み、`main` 以外のブランチに push する。team の権限は、コードを書き込み、issue と pull request を読むだけにする |

- 書き込み主体と、worker のユーザーのトークンは、設定に置く（[設定と秘密情報](README.md)）。
- 利用者とエージェントに、issue と pull request を書く権限を与えない。
  - なぜ：書く権限があると、書き込み主体の記録のコメントを編集し、消し、pull request を close できるから（[信頼の境界](pull-requests.md)）。

## 対応

| やりとりの操作 | Forgejo での実装 |
|---|---|
| repository | 設定した organization の repository。provider_ref は `<organization>/<名前>` |
| repository を列挙する | `GET /orgs/{org}/repos` |
| repository を用意する | 下の「repository の用意」 |
| 提案を作る | `POST /repos/{owner}/{repo}/pulls` |
| 反映する | `POST /repos/{owner}/{repo}/pulls/{index}/merge` に、`Do: squash`、`head_commit_id`（版のコミット）、`MergeTitleField` と `MergeMessageField`（trailer を含むメッセージ）を渡す |
| レビュー | `POST /repos/{owner}/{repo}/pulls/{index}/reviews` に、`event: COMMENT`、`commit_id`、本文（trailer を含む）を渡す |
| close | `PATCH /repos/{owner}/{repo}/pulls/{index}` で `state: closed` |
| 人への依頼と回答 | issue とコメント |
| 回答の添付 | コメントの添付（`POST /repos/{owner}/{repo}/issues/comments/{id}/assets`）。上限は、artifact と同じ 10 MiB |

- Forgejo を使う構成では、回答の添付をファイルで受け付ける。GitHub を使う構成では、リンクだけである（[人への依頼の回答](../files/artifacts.md)）。

## repository の用意

1. `POST /orgs/{org}/repos` で、repository を作る。`auto_init` は付けない。
2. `POST /repos/{owner}/{repo}/contents` で、根のファイルを 1 つのコミットにまとめて `main` に置く。
3. `POST /repos/{owner}/{repo}/branch_protections` で、`main` を守る。
   - 最初のコミットのあとに作る。なぜ：`main` への push を禁じたあとでは、最初のコミットを置けないから。

- 用意が途中で止まったら、利用者の送り直しで、止まったところから続ける。
  - repository がすでにあれば、作らない。`main` があれば、コミットを置かない。
  - `main` を守る設定は、なければ作り直す。すでにあれば、何もしない。

`main` を守る設定は、次のとおりである。

- `enable_push: false`：だれも `main` に push できない。
- `enable_merge_whitelist: true`、`merge_whitelist_usernames`：書き込み主体だけが merge できる。
- `block_on_outdated_branch: false`：`main` に追いついていることを merge の条件にしない。

- 版のコミットは、`refs/pull/<index>/head` で、最後の版が取れる（[ブランチの履歴](../backend/snapshot.md)）。

## 確かめたこと

Forgejo の API の定義（`swagger.v1.json`、Forgejo 17 系）で、次のことを確かめた。

| 項目 | 結果 |
|---|---|
| merge で head の SHA を指定できるか | できる。`MergePullRequestOption.head_commit_id` |
| squash を選べるか | できる。`Do: squash` |
| メッセージに trailer を書けるか | できる。`MergeMessageField` に本文を渡す |
| レビューが commit_id を持てるか | できる。`CreatePullReviewOptions.commit_id` と `PullReview.commit_id` |
| issue とコメントに、API でファイルを添付できるか | できる。`/issues/{index}/assets` と `/issues/comments/{id}/assets`（multipart の `attachment`） |
| main への push を禁じ、merge を特定のユーザーに絞れるか | できる。`CreateBranchProtectionOption` の `enable_push`、`enable_merge_whitelist`、`merge_whitelist_usernames` |
| トークンを確かめる API | `GET /user`。team の所属は `GET /user/teams` |
| 1 つのコミットで複数のファイルを置けるか | できる。`POST /repos/{owner}/{repo}/contents`（`ChangeFilesOptions`）。コミットの時刻も `dates` で指定できる |

作るときに確かめること（API の定義だけではわからないもの）：

- `POST /contents` で、空の repository に最初のコミットを作れるか。作れなければ、`auto_init` で作ったあとに、`tree/` の外のファイルを残さないように用意する手順を決める
- ブランチを消すと、pull request が閉じるか（Gitea で直された振る舞い）。閉じなくても、バックエンドは closed として扱う（[提案の状態](pull-requests.md)）
- repository の管理者が、`enable_push: false` を越えて `main` に push できないか
- 書き込み主体に、organization に repository を作る権限を与える方法（organization の設定か、team の権限か）
- issue と pull request を読むだけの team の人が、他人のコメントを編集・削除できず、pull request を close できないか
- `refs/pull/<index>/head` が、git の fetch で読めるか

## 認証

Forgejo を使う構成では、[認証](../backend/ports/authentication.md) のポートも、このアダプタが実装する。

- API は Forgejo のトークンを受け取り、要求ごとに `GET /user` で主体を確かめる。結果を保持しない。
- 主体の種類は、設定した organization の team で決める。
  - `user`：設定した team（例：`logiforge-users`）に入っている人
  - `client`：それ以外の、organization の人とエージェント
  - organization に属さない主体は、`unauthenticated` で受け付けない。
- すべての主体が、organization のすべての repository を読める。
