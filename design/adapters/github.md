# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、[GitForge](../backend/ports/gitforge.md) のポートをどう実装するかを確かめるために読む。
Forgejo アダプタと共通のところ（pull request の対応、レビューとコメントの書き方、MAC の合う記録だけを数えること、main を守ること）は、[GitForge アダプタの共通の書き方](pull-requests.md) に書く。
[worker](../backend/worker.md) は、[git アダプタ](git.md) で、GitHub リポジトリを読む。

## アカウントとトークン

Logiforge は、GitHub に書くための自分のアカウント（GitHub App など）を持たない。GitHub に書くのは、いつも、API の要求を送った主体のトークンである（[GitForge アダプタの共通の書き方](pull-requests.md)）。

| 使う者 | トークン | 要る権限 |
|---|---|---|
| 利用者（人） | WebUI のサインインで得る、Logiforge の OAuth App のトークン | リポジトリへの Write（repository の用意、回答のコメント） |
| opener と reviewer（人とエージェント） | gh のトークンか、fine-grained personal access token | リポジトリへの Write（ブランチの push、pull request とコメントとレビュー、reviewer は merge） |
| worker と、バックエンドの作業用の git の複製 | 読むだけのトークン（fine-grained personal access token） | Contents と Pull requests の Read だけ |

- reviewer に merge の権限が要る。なぜ：反映は、`approve` をした reviewer のトークンで merge するから（[反映の手順](../backend/merge.md)）。
- worker の読むだけのトークンは、public のリポジトリでも要る。なぜ：反映かの判定で REST の API を読み、トークンのない要求の上限（1 時間に 60）では足りないから（[反映かの判定](../backend/verify.md)）。
- 読むだけのトークンは、サーバの設定に置く（[設定と秘密情報](README.md)）。

## 見回りの間隔

- GitHub を使う構成では、worker の見回りの間隔を、既定の 1 秒より長くする。目安は 10 秒である（[見回り](../backend/worker.md#見回り)）。
  - なぜ：repository の数 × 1 秒に 1 回の `git ls-remote` は、GitHub の git の操作の、乱用を防ぐ制限に当たるおそれがあるから（[費用と規模の見積もり](../estimates.md)）。
  - そのかわり、push と書き込みが読む側に出るまでの遅れが、間隔の分だけ長くなる。
- 制限に当たったら（`ls-remote` が拒まれる、遅くなる）、間隔をさらに延ばす。

## 対応

GitHub に固有のところだけを書く。

| GitForge のポートの操作 | GitHub での実装 |
|---|---|
| repository | 設定に登録した GitHub リポジトリ（1 つの repository に 1 つ）。gitforge_ref は `owner/name` |
| repository を用意する | 空のリポジトリに、作る利用者のトークンで、contents API（`PUT /repos/{owner}/{repo}/contents/{path}`）で最初のコミットを作る |
| 反映する | reviewer のトークンで、`PUT /repos/{owner}/{repo}/pulls/{number}/merge` を、`sha`（先頭のコミット）、`merge_method: squash`、メッセージを付けて呼ぶ |
| レビュー | reviewer のトークンで、`event=COMMENT` の pull request review |
| 回答の添付 | リンクだけ。ファイルは受け付けない（[人への依頼の回答](../files/artifacts.md)） |
| 反映かの判定 | `GET /repos/{owner}/{repo}/commits/{sha}/pulls`（[反映かの判定](../backend/verify.md)） |

- 根の仮説に背景情報があるときは、contents API が 1 回に 1 つのファイルしか書けないので、根のコミットのあとに、2 つ目のコミットで足す。
  - 2 つ目のコミットには、`Logiforge-Init-Signature:` を付ける（[反映かの判定](../backend/verify.md) の「repository の用意のコミット」）。
  - なぜ 1 つのコミットにしないか：空のリポジトリでは、Git Data API（blob、tree、commit）が使えないから（409）。
- 登録する repository の owner（organization かユーザー）は、いくつでもよい。
- public のリポジトリでも、private のリポジトリでも使える。GitHub Free の private のリポジトリでは、`main` を守る設定が使えないので、守る設定なしで使う（[main を守る](pull-requests.md#main-を守る)）。

## repository を用意する

1. 管理者が、空の GitHub リポジトリを作る。
2. 管理者が、利用者とエージェントに、Write の権限を与える。`main` を守る設定は、使えるなら用意する（[GitHub での main の守りと制約](github-main.md)）。
3. 管理者が、repository の名前と `owner/name` の組を、サーバの設定に登録する（[repository の登録](README.md)）。この組が、名前の正本である。
4. 利用者が、名前と根の主文で repository を作る。バックエンドは、[repository の用意](../backend/README.md) の手順で、利用者のトークンで最初のコミットを置く。

- バックエンドは、GitHub リポジトリを作らない。なぜ：organization にリポジトリを作る権限を、利用者のトークンに求めないため。

## 認証

GitHub を使う構成では、[認証](../backend/ports/authentication.md) のポートも、このアダプタが実装する。認証は GitHub に任せる。

- 主体は、GitHub のアカウント（人と、エージェントの機械用アカウント）である。
- API は GitHub のトークンを受け取り、要求ごとに `GET /user` で主体を確かめる。結果を保持しない。
- 利用者は、WebUI で、Logiforge の OAuth App でサインインする（[サインイン](../webui/auth.md)）。
- 主体の種類は、管理者が主体ごとに決める。
  - `user` とみなすのは、Logiforge の OAuth App のトークンを持ち、かつサーバの設定に登録した人（GitHub のアカウント名か、organization の team）だけである。
    - Logiforge の OAuth App のトークンかは、`POST /applications/{client_id}/token` を、OAuth App の client_id と client secret で呼んで確かめる。なぜ：ほかのトークン（gh のトークンなど）を持つ主体を、`user` にしないため。
    - team の所属は、主体のトークンで `GET /user/teams` を呼んで確かめる。アカウント名と team の名前は、大文字と小文字を区別せずに比べる。
  - それ以外の GitHub のアカウントのトークンは、`client` である。
- 読める repository は、主体のトークンで、登録した GitHub リポジトリを読めるかで決める。
- どの操作を使えるかの判定は、バックエンドの [認可](../backend/README.md) が行う。

GitHub での `main` の守り方と、GitHub の制約への対処は、[GitHub での main の守りと制約](github-main.md) に書く。
