# Forgejo アダプタ

自前で持つ構成で、[Forgejo](https://forgejo.org/) を使って Logiforge を動かすときに、[GitForge のポート](../backend/ports/gitforge.md) と [認証](../backend/ports/authentication.md) のポートをどう実装するかを確かめるために読む。
GitHub アダプタと共通のところは、[GitForge アダプタの共通の書き方](pull-requests.md) に書く。

## Forgejo を使う理由

- 自前で持つ構成の GitForge に、Forgejo を使う。pull request、レビュー、issue、添付、ブランチの保護、トークン、API を、Forgejo がすでに持つ。
  - なぜ：これらを自前で作らずに済み、GitHub を使う構成と同じ pull request の形で扱えるから。
- Forgejo は、別のプロセス（単体のバイナリかコンテナ）として動かす。Logiforge は、Forgejo の HTTP の API と git だけを使う。
- Forgejo のライセンスは GPL である。Logiforge は、Forgejo のコードをリンクせず、別のプロセスとして使う。

## 主体

| 主体 | 使う者 | 権限 |
|---|---|---|
| 読むだけのユーザー | [worker](../backend/worker.md)、バックエンドの作業用の git の複製 | repository を読む。トークンの範囲は `read:repository`、`read:organization`（repository を列挙するため）、`read:issue`（記録のコメントを読むため）だけにする。organization が private なら、コード、issue、pull request を読むだけの team に入れる |
| 利用者 | WebUI | repository を作り（team の `can_create_org_repo`）、読み、issue と pull request にコメントを書く |
| opener と reviewer（人とエージェント） | CLI | repository を読み、`main` 以外のブランチに push し、pull request とレビューとコメントを書き、reviewer は merge する。team の権限は、コード、issue、pull request の書き込みにする |

- Logiforge は、Forgejo に書くための自分のユーザー（書き込み主体）を持たない。書くのは、いつも、API の要求を送った主体のトークンである（[GitForge アダプタの共通の書き方](pull-requests.md)）。
- worker のユーザーのトークンは、設定に置く（[設定と秘密情報](README.md)）。
- 利用者とエージェントは、issue と pull request を書く権限を持つので、ほかの主体の記録のコメントを編集し、消し、pull request を close できる。
  - 編集は署名で見つけ、worker の控えに残る記録は、消されても判定に使う（[信頼の境界](pull-requests.md#信頼の境界)）。

## 対応

| GitForge のポートの操作 | Forgejo での実装 |
|---|---|
| repository | 設定した organization の repository。gitforge_ref は `<organization>/<名前>` |
| repository を列挙する | `GET /orgs/{org}/repos` |
| repository を用意する | 下の「repository の用意」 |
| 提案を作る | `POST /repos/{owner}/{repo}/pulls` |
| 反映する | `POST /repos/{owner}/{repo}/pulls/{index}/merge` に、`Do: squash`、`head_commit_id`（先頭のコミット）、`MergeTitleField` と `MergeMessageField`（trailer を含むメッセージ）を渡す |
| レビュー | pull request の記録のコメント（`POST /repos/{owner}/{repo}/issues/{index}/comments`。[レビューの書き方](pull-requests.md#レビューの書き方)） |
| 反映かの判定 | `GET /repos/{owner}/{repo}/commits/{sha}/pull` と `GET /repos/{owner}/{repo}/issues/comments?since=`（[反映かの判定](../backend/verify.md)） |
| close | `PATCH /repos/{owner}/{repo}/pulls/{index}` で `state: closed` |
| 人への依頼と回答 | issue とコメント |
| 回答の添付 | コメントの添付（`POST /repos/{owner}/{repo}/issues/comments/{id}/assets`）。上限は、artifact と同じ 10 MiB |

- Forgejo を使う構成では、回答の添付をファイルで受け付ける。GitHub を使う構成では、リンクだけである（[人への依頼の回答](../files/artifacts.md)）。

## repository の用意

作る利用者のトークンで、次の順に行う。repository を作った主体は、その repository の管理者になる。

1. `POST /orgs/{org}/repos` で、repository を作る。`auto_init` は付けない。
2. `POST /repos/{owner}/{repo}/contents` で、根のファイルを 1 つのコミットにまとめて `main` に置く。trailer `Logiforge-Init:` と `Logiforge-Init-Signature:` を付ける（[反映かの判定](../backend/verify.md) の「repository の用意のコミット」）。
3. `POST /repos/{owner}/{repo}/branch_protections` で、`main` を守る。
   - 最初のコミットのあとに作る。なぜ：`main` への push を禁じたあとでは、最初のコミットを置けないから。

- 用意が途中で止まったら、利用者の送り直しで、止まったところから続ける。
  - repository がすでにあれば、作らない。`main` があれば、コミットを置かない。
  - `main` を守る設定は、なければ作り直す。すでにあれば、何もしない。

`main` を守る設定は、次のとおりである。

- `enable_push: false`：だれも `main` に push できない。
- `enable_merge_whitelist: false`：merge できる人は絞らない。reviewer が merge するからである。承認されていない pull request の merge は、worker が見つける（[反映かの判定](../backend/verify.md)）。
- `block_on_outdated_branch: false`：`main` に追いついていることを merge の条件にしない。

- pull request の最後の先頭のコミットは、`refs/pull/<index>/head` で取れる（[ブランチの履歴](../backend/snapshot.md)）。

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

動く Forgejo（17.0.1）で、次のことを確かめた（`internal/adapters/forgejo` の結合テスト）。

| 項目 | 結果 |
|---|---|
| `POST /contents` で、空の repository に最初のコミットを作れるか | 作れる。`auto_init` は要らない |
| ブランチを消すと、pull request が閉じるか | 閉じる |
| repository の管理者が、`enable_push: false` を越えて `main` に push できないか | できない。サイトの管理者でも、pre-receive で拒まれる |
| 主体に、organization に repository を作る権限を与える方法 | team の `can_create_org_repo: true`。主体を、その team に入れる |
| `refs/pull/<index>/head` が、git の fetch で読めるか | 読める |
| merge を呼べるのはいつか | Forgejo が merge できるかを確かめ終わるまで、405 を返す。アダプタは、上限つきで待って呼び直す。head が違えば 409 |
| issue と pull request を書ける主体が、ほかの主体のコメントを編集し、消せるか | できる |
| `issues/comments?since=` が、編集されたコメントを返すか | 返す。`updated_at` は編集の時刻になる |
| worker のトークンで、コメントを読めるか | `read:issue` のスコープと、issue と pull request を読める team が要る。`read:repository` だけでは 403 |
| merge の権限のない主体（コードを読むだけの team）が merge を呼ぶと、どう断られるか | 405 で、`message` は `User not allowed to merge PR`。アダプタは、待たずに `merge_forbidden` にする |
| merge のすぐあとに、`/commits/{sha}/pull` で pull request を引けるか | 引けないことがある（404）。worker は、初めて見てから 60 秒まで待つ |

## 認証

Forgejo を使う構成では、[認証](../backend/ports/authentication.md) のポートも、このアダプタが実装する。

- API は Forgejo のトークンを受け取り、要求ごとに `GET /user` で主体を確かめる。結果を保持しない。
- 主体の種類は、設定した organization の team で決める。
  - `user`：設定した team（例：`logiforge-users`）に入っている人
  - `client`：それ以外の、organization の人とエージェント
  - organization に属さない主体は、`unauthenticated` で受け付けない。
- すべての主体が、organization のすべての repository を読める。
  - そのため、repository を読めるかの確かめ（[認証](../backend/ports/authentication.md)）では、主体のトークンで Forgejo を呼ばない。主体が organization にいることは、主体を確かめるときに確かめている。
  - なぜ：利用者のトークンに `read:repository` を求めずに済むから（[サインインのスコープ](../webui/auth.md#スコープ)）。
