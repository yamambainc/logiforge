# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、[GitForge](../backend/ports/gitforge.md) のポートをどう実装するかを確かめるために読む。
Forgejo アダプタと共通のところは、[GitForge のポートを pull request と issue で実装する](pull-requests.md) と [review と issue の本文](texts.md) に書く。
[worker](../backend/worker/README.md) は、[git アダプタ](git.md) で、GitHub リポジトリを読む。

## アカウントとトークン

主体ごとのトークンと、Logiforge の GitHub App の権限は、[GitHub App とトークン](github-app.md) に書く。

## 見回りの間隔

- GitHub を使う構成では、worker の見回りの間隔を、既定の 1 秒より長くする。目安は 10 秒である（[見回り](../backend/worker/patrol.md)）。
  - なぜ：repository の数 × 1 秒に 1 回の `git ls-remote` は、GitHub の git の操作の、乱用を防ぐ制限に当たるおそれがあるから（[費用と規模の見積もり](../estimates/README.md#worker)）。
  - そのかわり、push と書き込みが読む側に出るまでの遅れが、間隔の分だけ長くなる。
- 制限に当たったら（`ls-remote` が拒まれる、遅くなる）、間隔をさらに延ばす。

## 対応

GitHub に固有のところだけを書く。

| GitForge のポートの操作 | GitHub での実装 |
|---|---|
| repository | 設定の DB に登録した GitHub リポジトリ（1 つの repository に 1 つ）。gitforge_ref は `owner/name` |
| repository を用意する | 空のリポジトリに、作る利用者のトークンで、contents API（`PUT /repos/{owner}/{repo}/contents/{path}`）で最初のコミットを作る |
| 反映する | reviewer のトークンで、`PUT /repos/{owner}/{repo}/pulls/{number}/merge` を、`sha`（先頭のコミット）、`merge_method: squash`、メッセージを付けて呼ぶ |
| レビュー | reviewer のトークンで、`POST /repos/{owner}/{repo}/pulls/{number}/reviews`（`event` は `APPROVE`、`REQUEST_CHANGES`、`COMMENT`。本文は [review の本文](texts.md#review-の本文)） |
| 反映の失敗 | reviewer のトークンで、`POST /repos/{owner}/{repo}/issues/{number}/comments`（理由を書いたふつうのコメント）と、ラベルの付け替え |
| apply request の一覧 | `GET /repos/{owner}/{repo}/pulls`。レビュー待ちか修正待ちかはラベル（[一覧の読み](../backend/list-reads.md#どの一覧の-api-で読むか)） |
| ラベルの付け外し | `POST /repos/{owner}/{repo}/issues/{number}/labels`、`DELETE /repos/{owner}/{repo}/issues/{number}/labels/{name}`。ない名前を付けると既定の色で作られるので、先に `POST /repos/{owner}/{repo}/labels` で作る |
| 回答の添付 | リンクだけ。ファイルは受け付けない（[人への依頼の回答](../files/artifacts.md)） |
| どの apply request の反映か | `GET /repos/{owner}/{repo}/pulls?state=closed&sort=updated&direction=desc`（[どの apply request の反映か](../backend/invalid-main.md#どの-apply-request-の反映か)） |

- 根の仮説に背景情報があるときは、根のコミットのあとに、2 つ目のコミットで足す（[repository を用意する](#repository-を用意する)）。
  - なぜ 1 つのコミットにしないか：空のリポジトリでは、Git Data API（blob、tree、commit）が使えず（409）、contents API は 1 回に 1 つのファイルしか書けないから。
- 登録する repository の owner（organization かユーザー）は、いくつでもよい。
- public のリポジトリでも、private のリポジトリでも使える。GitHub Free の private のリポジトリでは、`main` を守る設定が使えないので、守る設定なしで使う（[main を守る](pull-requests.md#main-を守る)）。

### merge を断られたとき

merge の要求に GitHub が次のものを返したら、アダプタは反映の失敗 `merge_forbidden` にする（[反映の手順](../backend/merge.md)）。

- 403。主体に merge の権限がない（Write がない、トークンの権限が足りない）。
  - レート制限による 403 は、`merge_forbidden` にしない。待ってやり直す（[GitHub での main の守りと制約](github-main.md)）。
- 405 で、応答が `Repository rule violations found` のもの。ルールセットが、この主体の merge を断った。
  - 例：ディレクトリの守りの (B) の bypass にいない人が、ディレクトリを変える PR を merge した（[main の守り（ディレクトリ方式）](../directory/main.md#github-の守る設定)）。リポジトリの admin でも同じである。
  - 応答には、`Waiting on required approvals from <承認の team>` も付く。
  - なぜ待たないか：待っても、断られることは変わらないから。
- ほかの 405 は、`merge_forbidden` にしない。pull request を読み直し、衝突や先頭のコミットの変化として扱う。

## repository を用意する

1. GitHub でリポジトリを作る人（owner の admin か、リポジトリを作れる人）が、空の GitHub リポジトリを作る。
2. リポジトリの admin が、利用者とエージェントに、Write の権限を与える。
3. 利用者が、repository の名前と `owner/name` の組を、設定の画面で登録する（[repository の登録](README.md#repository-の登録)）。この組が、名前の正本である。
4. 利用者が、続けて設定の画面で、根の主文を入れて repository を作る（[repository を足す](../../docs/webui/settings.md#repository-を足す)）。バックエンドは、[repository の用意](../backend/README.md) の手順で、利用者のトークンで最初のコミットを置く。
   - 根のコミット：contents API で、根の仮説のファイルを置く。
   - 背景情報があれば、2 つ目のコミット：Git Data API（tree と commit を作り、`main` の ref を進める）で、残りのファイルを 1 つのコミットにまとめる。
5. `main` を守る設定は、使えるなら、リポジトリの admin が用意のあとに掛ける（[GitHub での main の守りと制約](github-main.md)）。
   - なぜ用意のあとか：2 つ目のコミットは、`main` の ref を直接進めるので、「Require a pull request before merging」があると断られるから。

- バックエンドは、GitHub リポジトリを作らない。なぜ：organization にリポジトリを作る権限を、利用者のトークンに求めないため。

## 認証

GitHub を使う構成では、[認証](../backend/ports/authentication.md) のポートも、このアダプタが実装する。認証は GitHub に任せる。

- 主体は、GitHub のアカウント（人と、エージェントの機械用アカウント）である。
- API は GitHub のトークンを受け取り、要求ごとに `GET /user` で主体を確かめる。結果を保持しない。
- 利用者は、WebUI で、Logiforge の GitHub App でサインインする（[サインイン](../webui/auth.md)）。
- 主体の種類は、リポジトリの権限で決める。名簿は持たない（[利用者の決め方](../settings/users.md#利用者の決め方)）。
  - `user`：Logiforge の GitHub App のトークンを持ち、登録した GitHub リポジトリのどれかに Write 以上の権限がある人
    - App のトークンかは、`POST /applications/{client_id}/token` を、App の client_id と client secret で呼んで確かめる。GitHub は、ほかのアプリのトークンには 404 を、client secret が違えば 401 を返す。
    - 権限は、主体のトークンで `GET /user/installations` と `GET /user/installations/{installation_id}/repositories` を呼び、`permissions.push` を見る。
  - それ以外の GitHub のアカウントのトークンは、`client` である。
- 読める repository は、主体のトークンで、登録した GitHub リポジトリを読めるかで決める。
  - user access token で読めるのは、App が install されていて、かつその人が読めるリポジトリだけである。
- どの操作を使えるかの判定は、バックエンドの [認可](../backend/README.md) が行う。

GitHub での `main` の守り方と、GitHub の制約への対処は、[GitHub での main の守りと制約](github-main.md) に書く。
