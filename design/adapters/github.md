# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、[GitForge](../backend/ports/gitforge.md) のポートをどう実装するかを確かめるために読む。
Forgejo アダプタと共通のところ（pull request の対応、レビューとコメントの書き方、署名の合う記録だけを数えること、main を守ること）は、[GitForge アダプタの共通の書き方](pull-requests.md) に書く。
[worker](../backend/worker.md) は、[git アダプタ](git.md) で、GitHub リポジトリを読む。

## アカウントとトークン

Logiforge は、GitHub に書くための自分のアカウント（書き込み主体）を持たない。GitHub に書くのは、いつも、API の要求を送った主体のトークンである（[GitForge アダプタの共通の書き方](pull-requests.md)）。
読むためだけに、読むだけの GitHub App を持つ（下の「読むだけの GitHub App」）。personal access token は使わない。

| 使う者 | トークン | 要る権限 |
|---|---|---|
| 利用者（人） | WebUI のサインインで得る、Logiforge の OAuth App のトークン | リポジトリへの Write（repository の用意、回答のコメント） |
| opener と reviewer（人とエージェント） | `gh auth login`（GitHub CLI の OAuth App）で得たトークン | スコープ `repo`。リポジトリへの Write（ブランチの push、pull request とコメントとレビュー、reviewer は merge） |
| worker と、バックエンドの作業用の git の複製 | 読むだけの GitHub App の installation token。public のリポジトリだけなら、トークンなしでもよい | Contents、Issues、Pull requests、Metadata の Read だけ |

- reviewer に merge の権限が要る。なぜ：反映は、`approve` をした reviewer のトークンで merge するから（[反映の手順](../backend/merge.md)）。
- opener と reviewer のトークン：
  - `gh auth login` で、ブラウザか device flow でサインインして得る。エージェントの機械用アカウントも、そのアカウントでサインインしたブラウザで、device flow のコードを入れて得る。`gh auth token` で取り出し、CLI に渡す（[CLI の認証](../cli/README.md#認証)）。
  - スコープは `repo` が要る。なぜ：push と、pull request とコメントとレビューと merge を、このトークンで書くから。`GET /user` は、スコープなしで呼べる。`gh auth login` の既定のスコープ（`repo`、`read:org`、`gist`）に入っている。
  - `repo` は、そのアカウントが書けるすべてのリポジトリに及ぶ。範囲は広いが、受け入れる。なぜ：OAuth のスコープは、リポジトリごとに絞れないから。絞りたいときは、Logiforge のためだけの機械用アカウントを作り、登録した repository にだけ Write を与える。
  - organization が OAuth App のアクセスを制限しているときは、organization の owner が GitHub CLI を許す。
  - `gh auth login --with-token` で personal access token を gh に入れたものは使わない。
  - Logiforge の OAuth App のトークンを CLI に使っても動く。そのとき、`LOGIFORGE_USER_LOGINS` か `LOGIFORGE_USER_TEAM` に入る人は `user` になる（下の「認証」）。CLI に、Logiforge の OAuth App でサインインする手順は持たない。
- worker の読むトークンは、public のリポジトリだけなら、なくてもよい。そのときは、git も REST も、トークンなしで読む。
  - ただし、トークンのない要求の上限は、1 時間に 60 回（送り元の IP ごと）である。反映かの判定で、`main` のコミットごとに REST を何回も読むので、ふつうは足りない（[反映かの判定](../backend/verify.md) の「重さ」）。試すときだけにし、続けて使うときは App を置く。
  - private のリポジトリでは、要る。
- Issues の Read は、記録のコメント（`issues/comments`）を読むのに使う（[記録の控え](../backend/verify.md#記録の控え)）。
- App の ID と秘密鍵は、サーバの設定に置く（[設定と秘密情報](README.md)）。

### 読むだけの GitHub App

worker と、バックエンドの作業用の git の複製は、読むだけの GitHub App の installation token で読む。

- App は書かない。権限は、Repository permissions の Contents、Issues、Pull requests、Metadata の Read だけにする。Webhook は使わない。
  - 反映かの判定は、中身だけで決める（[反映かの判定](../backend/verify.md)）。App は判定に関わらない。前になくした「書き込み主体としての App」とは違う。
- なぜ App か：personal access token は、人のアカウントに結び付き、その人が組織を離れると止まり、期限を人が更新するから。App の installation token は、人に結び付かず、1 時間で切れ、そのたびに作り直すので、漏れても長くは使えない。
- 管理者が、App を作り、登録する repository の owner（organization か個人）に install し、登録する GitHub リポジトリを選ぶ。owner が複数なら、それぞれに install する。
- installation token の作り方：
  1. App の ID と秘密鍵で、JWT（RS256、10 分より短い）を作る。
  2. owner の installation を、`GET /repos/{owner}/{repo}/installation`（その owner の登録したリポジトリのどれか）で引く。
  3. `POST /app/installations/{installation_id}/access_tokens` を、`repositories`（その owner の登録したリポジトリの名前）と `permissions`（上の 4 つの Read）を付けて呼ぶ。なぜ絞るか：install で選んだリポジトリが広くても、トークンを登録したリポジトリと読む権限だけにするため。
- installation token は、owner ごとに 1 つ持ち、使い回す。切れるまでの残りが 20 分より短くなったら、次に使うときに作り直す。
  - なぜ 20 分：git の操作 1 回の時間の上限（既定 10 分）のあいだに、切れないようにするため。
  - git には、git の操作のたびに、そのときのトークンを credential helper で渡す（[git アダプタ](git.md) の「資格情報」）。
- App が owner に install されていないか、権限が足りなければ、GitHub が断る。worker は、その repository の見回りを誤りにする（ログに出す）。
- REST の上限は、installation ごとに 1 時間に 5,000 回からである。

## 見回りの間隔

- GitHub を使う構成では、worker の見回りの間隔を、既定の 1 秒より長くする。目安は 10 秒である（[見回り](../backend/worker.md#見回り)）。
  - なぜ：repository の数 × 1 秒に 1 回の `git ls-remote` は、GitHub の git の操作の、乱用を防ぐ制限に当たるおそれがあるから（[費用と規模の見積もり](../estimates.md)）。
  - そのかわり、push と書き込みが読む側に出るまでの遅れが、間隔の分だけ長くなる。
- 制限に当たったら（`ls-remote` が拒まれる、遅くなる）、間隔をさらに延ばす。
- 記録を読んで控えに足す間隔は、30 秒にする（[記録の控え](../backend/verify.md#記録の控え)）。なぜ：repository ごとに REST を 1 回使うので、10 秒ごとでは、repository が多いと上限に当たるから。

## 対応

GitHub に固有のところだけを書く。

| GitForge のポートの操作 | GitHub での実装 |
|---|---|
| repository | 設定に登録した GitHub リポジトリ（1 つの repository に 1 つ）。gitforge_ref は `owner/name` |
| repository を用意する | 空のリポジトリに、作る利用者のトークンで、contents API（`PUT /repos/{owner}/{repo}/contents/{path}`）で最初のコミットを作る |
| 反映する | reviewer のトークンで、`PUT /repos/{owner}/{repo}/pulls/{number}/merge` を、`sha`（先頭のコミット）、`merge_method: squash`、メッセージを付けて呼ぶ |
| レビュー | reviewer のトークンで、pull request の記録のコメント（[レビューの書き方](pull-requests.md#レビューの書き方)） |
| 回答の添付 | リンクだけ。ファイルは受け付けない（[人への依頼の回答](../files/artifacts.md)） |
| 反映かの判定 | `GET /repos/{owner}/{repo}/commits/{sha}/pulls`（[反映かの判定](../backend/verify.md)） |

- 根の仮説に背景情報があるときは、根のコミットのあとに、2 つ目のコミットで足す（下の「repository を用意する」）。
  - 2 つ目のコミットには、`Logiforge-Init-Signature:` を付ける（[反映かの判定](../backend/verify.md) の「repository の用意のコミット」）。
  - なぜ 1 つのコミットにしないか：空のリポジトリでは、Git Data API（blob、tree、commit）が使えず（409）、contents API は 1 回に 1 つのファイルしか書けないから。
- 登録する repository の owner（organization かユーザー）は、いくつでもよい。
- public のリポジトリでも、private のリポジトリでも使える。GitHub Free の private のリポジトリでは、`main` を守る設定が使えないので、守る設定なしで使う（[main を守る](pull-requests.md#main-を守る)）。

## repository を用意する

1. 管理者が、空の GitHub リポジトリを作る。
2. 管理者が、利用者とエージェントに、Write の権限を与える。
3. 管理者が、repository の名前と `owner/name` の組を、サーバの設定に登録する（[repository の登録](README.md)）。この組が、名前の正本である。
4. 利用者が、名前と根の主文で repository を作る。バックエンドは、[repository の用意](../backend/README.md) の手順で、利用者のトークンで用意のコミットを置く。
   - 根のコミット：contents API で、根の仮説のファイルを置く。trailer `Logiforge-Init:` と `Logiforge-Init-Signature:` を付ける。
   - 背景情報があれば、2 つ目のコミット：Git Data API（tree と commit を作り、`main` の ref を進める）で、残りのファイルを 1 つのコミットにまとめる。trailer `Logiforge-Init-Signature:` を付ける。
5. `main` を守る設定は、使えるなら、管理者が用意のあとに掛ける（[GitHub での main の守りと制約](github-main.md)）。
   - なぜ用意のあとか：2 つ目のコミットは、`main` の ref を直接進めるので、「Require a pull request before merging」があると断られるから。

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

## 確かめたこと

実際の GitHub（public のリポジトリ `yamambainc/logiforge-github-adapter-test-init3`）で、Logiforge の OAuth App も読むだけの App もなしに、次のことを確かめた（`internal/adapters/github` の結合テスト `TestGitHubLive`）。

| 項目 | 結果 |
|---|---|
| 空のリポジトリに、利用者のトークンで用意のコミットを置けるか | 置ける。根のコミットは contents API、2 つ目は Git Data API |
| worker が、用意の 2 つのコミットを反映によるものとするか | する。どちらも、署名と、差分がファイルを加えるだけであることで決める |
| `GET /commits/{sha}/pulls` で、merge した pull request を引けるか | 引ける。merge のすぐあとは、空のことがある。worker は、初めて見てから 60 秒まで待つ |
| reviewer のトークンで squash merge したコミット | 反映によるもの |
| 承認された apply request を、人が merge のコミットで merge したコミット | 反映によるもの |
| `approve` のあとに `request_changes` が付いた apply request を、人が squash merge したコミット | 反映によるものでない。repository は壊れた状態になる |
| worker が、トークンなしで public のリポジトリを読めるか | 読める。ただし、REST の上限（1 時間に 60 回）に、1 回の試験の途中で当たった |

試験用の OAuth App（yamambainc）で、次のことも確かめた（2026-11-17）。OAuth App のトークンは、device flow（スコープ `read:user` と `read:org`）で得た。

| 項目 | 結果 |
|---|---|
| OAuth App のトークンを持ち、`LOGIFORGE_USER_LOGINS` に入れたアカウント | `user`。repository の作成が、主体の種類の確かめを通る |
| 同じアカウントの、OAuth App のものでないトークン（gh のトークン） | `client`。repository の作成は 403（`forbidden`） |
| OAuth App のトークンを持つが、`LOGIFORGE_USER_LOGINS` に入れていないアカウント | `client`。403 |
| `POST /applications/{client_id}/token` に、ほかのトークンを渡す / 違う secret で呼ぶ | 404 / 401 |

まだ確かめていないことは、次のとおりである。

- WebUI の画面からのサインイン（authorize から callback まで）。BE の中継は、偽の code で GitHub が交換を断るところまで確かめた。
- private のリポジトリと、`main` を守るルールセットを掛けたリポジトリでの、反映と、守る設定に断られる操作。GitHub Free の organization では、private のリポジトリに守る設定を掛けられないので、確かめられない。
- 読むだけの GitHub App の installation token で、worker とバックエンドの作業用の複製が、git と REST を読めること。偽の GitHub でだけ確かめた（JWT、installation の引き方、トークンの絞り方、作り直し）。
- merge の権限のない reviewer の merge を、GitHub がどう断るか。アダプタは、403 を `merge_forbidden` にする（制限の 403 は、待ってやり直し、権限がないことにしない）。偽の GitHub でだけ確かめた。
