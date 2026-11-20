# Forgejo アダプタ

自前で持つ構成で、[Forgejo](https://forgejo.org/) を使って Logiforge を動かすときに、[GitForge のポート](../backend/ports/gitforge.md) と [認証](../backend/ports/authentication.md) のポートをどう実装するかを確かめるために読む。
GitHub アダプタと共通のところは、[GitForge のポートを pull request と issue で実装する](pull-requests.md) に書く。

## Forgejo を使う理由

- 自前で持つ構成の GitForge に、Forgejo を使う。pull request、レビュー、issue、添付、ブランチの保護、トークン、API を、Forgejo がすでに持つ。
  - なぜ：これらを自前で作らずに済み、GitHub を使う構成と同じ pull request の形で扱えるから。
- Forgejo は、別のプロセス（単体のバイナリかコンテナ）として動かす。Logiforge は、Forgejo の HTTP の API と git だけを使う。
- Forgejo のライセンスは GPL である。Logiforge は、Forgejo のコードをリンクせず、別のプロセスとして使う。

## 主体

| 主体 | 使う者 | 権限 |
|---|---|---|
| 読むだけのユーザー | [worker](../backend/worker/README.md)、バックエンドの作業用の git の複製 | repository を読む。トークンの範囲は `read:repository`、`read:organization`（repository を列挙するため）、`read:issue`（issue と pull request のコメントを読むため）だけにする。organization が private なら、コード、issue、pull request を読むだけの team に入れる |
| 利用者 | WebUI | repository を作り（team の `can_create_org_repo`）、読み、issue と pull request にコメントを書き、WebUI のレビューで merge する。team の権限は、コード、issue、pull request の書き込みにする |
| opener と reviewer（人とエージェント） | CLI | repository を読み、`main` 以外のブランチに push し、pull request とレビューとコメントを書き、reviewer は merge する。team の権限は、コード、issue、pull request の書き込みにする |

- Logiforge は、Forgejo に書くための自分のユーザー（書き込み主体）を持たない。書くのは、いつも、API の要求を送った主体のトークンである（[GitForge のポートを pull request と issue で実装する](pull-requests.md)）。
- worker のユーザーとトークンは、設定の DB に置く（[設定の DB に置くもの](../settings/README.md#設定の-db-に置くもの)、[秘密の暗号化](../settings/secrets.md)）。
- 利用者とエージェントは、issue と pull request を書く権限を持つので、ほかの主体のコメントと本文を編集し、消し、pull request を close できる。そういう主体を信じる（[信頼の境界](pull-requests.md#信頼の境界)）。

## 対応

| GitForge のポートの操作 | Forgejo での実装 |
|---|---|
| repository | 設定した organization の repository。gitforge_ref は `<organization>/<名前>` |
| repository を列挙する | `GET /orgs/{org}/repos` |
| repository を用意する | [repository の用意](#repository-の用意) |
| apply request を作る | `POST /repos/{owner}/{repo}/pulls` |
| 反映する | `POST /repos/{owner}/{repo}/pulls/{index}/merge` に、`Do: squash`、`head_commit_id`（先頭のコミット）、`MergeTitleField` と `MergeMessageField`（trailer を含むメッセージ）を渡す |
| レビュー | `POST /repos/{owner}/{repo}/pulls/{index}/reviews`（`event` は `APPROVED`、`REQUEST_CHANGES`、`COMMENT`。本文は [review の本文](texts.md#review-の本文)） |
| 反映の失敗 | `POST /repos/{owner}/{repo}/statuses/{sha}`（context は `logiforge/merge`）と、pull request のコメント |
| ラベルの付け外し | `POST /repos/{owner}/{repo}/issues/{index}/labels`（名前で付けられる）、`DELETE /repos/{owner}/{repo}/issues/{index}/labels/{id}`。付ける前に、なければ `POST /repos/{owner}/{repo}/labels` で作る。書く権限で足りる |
| 反映かの判定 | `GET /repos/{owner}/{repo}/pulls?state=closed&sort=recentupdate` と `GET /repos/{owner}/{repo}/pulls/{index}/reviews`（[反映かの判定](../backend/verify/README.md)） |
| close | `PATCH /repos/{owner}/{repo}/pulls/{index}` で `state: closed` |
| 人への依頼と回答 | issue とコメント |
| 回答の添付 | コメントの添付（`POST /repos/{owner}/{repo}/issues/comments/{id}/assets`）。上限は、artifact と同じ 10 MiB |

- Forgejo を使う構成では、回答の添付をファイルで受け付ける。GitHub を使う構成では、リンクだけである（[人への依頼の回答](../files/artifacts.md)）。

## repository の用意

作る利用者のトークンで、次の順に行う。repository を作った主体は、その repository の admin になる。

1. `POST /orgs/{org}/repos` で、repository を作る。`auto_init` は付けない。
2. `POST /repos/{owner}/{repo}/contents` で、根のファイルを 1 つのコミットにまとめて `main` に置く。trailer `Logiforge-Init:` を付ける（[repository の用意のコミット](../backend/verify/init.md#repository-の用意のコミット)）。
3. `POST /repos/{owner}/{repo}/branch_protections` で、`main` を守る。
   - 最初のコミットのあとに作る。なぜ：`main` への push を禁じたあとでは、最初のコミットを置けないから。

- 用意が途中で止まったら、利用者の送り直しで、止まったところから続ける。
  - repository がすでにあれば、作らない。`main` があれば、コミットを置かない。
  - `main` を守る設定は、なければ作り直す。すでにあれば、何もしない。

`main` を守る設定は、次のとおりである。

- `enable_push: false`：だれも `main` に push できない。
- `enable_merge_whitelist: false`：merge できる人は絞らない。reviewer が merge するからである。承認されていない pull request の merge は、worker が見つける（[反映かの判定](../backend/verify/README.md)）。
- `block_on_outdated_branch: false`：`main` に追いついていることを merge の条件にしない。

- pull request の最後の先頭のコミットは、`refs/pull/<index>/head` で取れる（[ブランチの履歴](../backend/snapshot/README.md)）。

## Forgejo の挙動

アダプタと worker が前提にする、Forgejo の挙動である。

- pull request の作者は、自分の pull request を `approve` できない（422）。
- 同じ人があとで `approve` すると、その人の前の `request changes` は `dismissed` になる。`official` は、書く権限を持つ人の、いちばん新しい `approve` と `request changes` にだけ付く。`comment` の review には付かない。
- ほかの人の権限を読む API（`GET /repos/{owner}/{repo}/collaborators/{user}/permission`）は、repository の admin しか使えない。
- draft の項目はなく、タイトルの接頭辞（`WIP: ` など）で決まる。Logiforge は、draft を使わない。
- timeline のラベルの出来事は、付けたときに本文が `1` になる。

- 空の repository にも、`POST /repos/{owner}/{repo}/contents` で最初のコミットを置ける。`auto_init` は要らない。
- `enable_push: false` の `main` には、repository の admin も Forgejo の site admin も push できない。pre-receive で拒まれる。
- ブランチを消すと、そのブランチの pull request は閉じる。
- `refs/pull/<index>/head` は、git の fetch で読める。
- merge の API は、Forgejo が merge できるかを確かめ終わるまで、405 を返す。
  - アダプタは、上限つきで待って呼び直す。
- merge の API は、head が `head_commit_id` と違えば、409 を返す。
- merge の権限のない主体が merge を呼ぶと、405 で、`message` が `User not allowed to merge PR` になる。
  - アダプタは、待たずに反映の失敗 `merge_forbidden` にする（[反映の手順](../backend/merge.md)）。
- `GET /repos/{owner}/{repo}/issues/comments?since=` は、編集されたコメントも返す。`updated_at` は、編集の時刻になる。
- コメントを読むには、`read:issue` のスコープと、issue と pull request を読める team が要る。`read:repository` だけでは 403 になる。

## worker のトークンを確かめる

バックエンドは、worker のトークンを置く前に（[初回のセットアップ](../settings/setup.md#forgejo-を使う構成)、[設定の API](../api/endpoints/settings.md)、[設定のコマンド](../settings/README.md#設定のコマンド) の `settings load`）、次のことを確かめる。

- そのトークンで、organization を読める。
  - `GET /user` は使わない。なぜ：トークンが `read:user` を持たないから。
- そのトークンで、書くスコープの要る API が 403 になる。
  - 害のない要求を使う。どれも本文を空にして呼ぶ。
    - `PATCH /user/settings`（`write:user`）
    - `POST /repos/{owner}/{repo}/issues`（`write:issue`）。repository は、`GET /orgs/{org}/repos` の最初のものにする。organization に repository がなければ、この要求は飛ばす。
  - Forgejo は、スコープの足りないトークンを、本文を確かめる前に 403 で断る。
  - 403 なら、書けないとみなす。404 は、どちらともみなさない。それ以外（200、400、422 など、スコープを通った応答）なら、書けるトークンとみなして断る。
- worker のユーザーが、organization の team のうち、書ける team（権限が `read` より強いか、`units_map` に `write` があるもの）に入っていない。
  - `GET /orgs/{org}/teams` と `GET /teams/{id}/members` で調べる。どちらも `read:organization` で読める。
  - Forgejo は、team のメンバーの一覧を、その team のメンバーにだけ見せる。403 なら、worker のユーザーはその team にいない。
- worker のユーザーが、organization の今ある repository のどれにも書けない（個人の collaborator として Write を持たない）。
  - `GET /orgs/{org}/repos` を全ページ読み、`permissions` の `push` か `admin` が `true` のものがあれば断る。team の調べは、これから作る repository のために残す。

## 認証

Forgejo を使う構成では、[認証](../backend/ports/authentication.md) のポートも、このアダプタが実装する。

- API は Forgejo のトークンを受け取り、要求ごとに `GET /user` で主体を確かめる。結果を保持しない。
- team の所属は、主体のトークンで `GET /user/teams` を呼んで確かめる。
- WebUI のサインインで得たトークンかは、`POST /login/oauth/introspect` を、Logiforge の OAuth2 のアプリケーションの client_id と client secret で呼んで確かめる。
  - 応答の `active` が `true` で、かつ `aud` に Logiforge の client_id があるときだけ、WebUI のサインインのトークンとみなす。
  - なぜ `active` だけで決めないか：Forgejo は、ほかのアプリケーションが出したトークンにも `active: true` を返すから。どのアプリケーションのトークンかは、`aud` に入る。
  - personal access token には、`active: false` が返る。
  - なぜ：CLI のトークンを持つ主体を、`user` にしないため（[トークンの出どころ](../settings/users.md#トークンの出どころ)）。
- 主体の種類は、リポジトリの権限で決める。名簿は持たない（[利用者の決め方](../settings/users.md#利用者の決め方)）。
  - `user`：Logiforge の OAuth2 のアプリケーションのトークンを持ち、設定した organization の team のどれかで、コードに書ける人（`units_map` の `repo.code` が `write` か、team の権限が `admin` か `owner`）
    - なぜ team の `permission` だけで決めないか：Forgejo は、team の `permission` を `units_map` から決めるので、コードが `read` で issue と pull request が `write` の team は、`read` になるから。
  - `client`：それ以外の、organization の人とエージェント
  - organization に属さない主体は、`unauthenticated` で受け付けない。
- すべての主体が、organization のすべての repository を読める。
  - そのため、repository を読めるかの確かめ（[認証](../backend/ports/authentication.md)）では、主体のトークンで Forgejo を呼ばない。主体が organization にいることは、主体を確かめるときに確かめている。
  - なぜ：organization にいる主体は、どれも読めると決めているので、Forgejo に聞いても答えが変わらないから。
