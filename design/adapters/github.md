# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、[GitForge](../backend/ports/gitforge.md) のポートをどう実装するかを確かめるために読む。
Forgejo アダプタと共通のところは、[GitForge アダプタの共通の書き方](pull-requests.md) と [記録のコメントとレビュー](records.md) に書く。
[worker](../backend/worker/README.md) は、[git アダプタ](git.md) で、GitHub リポジトリを読む。

## アカウントとトークン

Logiforge は、GitHub に書くための自分のアカウント（書き込み主体）を持たない。GitHub に書くのは、いつも、API の要求を送った主体のトークンである（[GitForge アダプタの共通の書き方](pull-requests.md)）。
読むためだけに、読むだけの GitHub App を持つ（[読むだけの GitHub App](#読むだけの-github-app)）。personal access token は使わない。

| 使う者 | トークン | 要る権限 |
|---|---|---|
| 利用者（人） | WebUI のサインインで得る、Logiforge の OAuth App のトークン | リポジトリへの Write（repository の用意、回答のコメント） |
| opener と reviewer（人とエージェント） | `gh auth login`（GitHub CLI の OAuth App）で得たトークン | スコープ `repo`。リポジトリへの Write（ブランチの push、pull request とコメントとレビュー、reviewer は merge） |
| worker と、バックエンドの作業用の git の複製 | 読むだけの GitHub App の installation token。public のリポジトリだけなら、トークンなしでもよい | Contents、Issues、Pull requests、Metadata の Read だけ |

- reviewer に merge の権限が要る。なぜ：反映は、`approve` をした reviewer のトークンで merge するから（[反映の手順](../backend/merge.md)）。
- opener と reviewer のトークン：
  - `gh auth login` で、ブラウザか device flow でサインインして得る。エージェントの機械用アカウントも、そのアカウントでサインインしたブラウザで、device flow のコードを入れて得る。`gh auth token` で取り出し、CLI に渡す（[CLI の認証](../cli/auth.md)）。
  - スコープは `repo` が要る。なぜ：push と、pull request とコメントとレビューと merge を、このトークンで書くから。`GET /user` は、スコープなしで呼べる。`gh auth login` の既定のスコープ（`repo`、`read:org`、`gist`）に入っている。
  - `repo` は、そのアカウントが書けるすべてのリポジトリに及ぶ。範囲は広いが、受け入れる。なぜ：OAuth のスコープは、リポジトリごとに絞れないから。絞りたいときは、Logiforge のためだけの機械用アカウントを作り、登録した repository にだけ Write を与える。
  - organization が OAuth App のアクセスを制限しているときは、organization の owner が GitHub CLI を許す。
  - `gh auth login --with-token` で personal access token を gh に入れたものは使わない。
  - Logiforge の OAuth App のトークンを CLI に使っても動く。そのとき、`LOGIFORGE_USER_LOGINS` か `LOGIFORGE_USER_TEAM` に入る人は `user` になる（[認証](#認証)）。CLI に、Logiforge の OAuth App でサインインする手順は持たない。
- worker の読むトークンは、public のリポジトリだけなら、なくてもよい。そのときは、git も REST も、トークンなしで読む。
  - ただし、トークンのない要求の上限は、1 時間に 60 回（送り元の IP ごと）である。反映かの判定で、`main` のコミットごとに REST を何回も読むので、ふつうは足りない（[反映かの判定の重さ](../backend/verify/README.md#重さ)）。試すときだけにし、続けて使うときは App を置く。
  - private のリポジトリでは、要る。
- Issues の Read は、記録のコメント（`issues/comments`）を読むのに使う（[記録の控え](../backend/verify/records.md)）。
- App の ID と秘密鍵は、サーバの設定に置く（[設定と秘密情報](settings.md)）。

### 読むだけの GitHub App

worker と、バックエンドの作業用の git の複製は、読むだけの GitHub App の installation token で読む。

- App は書かない。権限は、Repository permissions の Contents、Issues、Pull requests、Metadata の Read だけにする。Webhook は使わない。
  - 反映かの判定は、中身だけで決める（[反映かの判定](../backend/verify/README.md)）。App は判定に関わらない。
- なぜ App か：personal access token は、人のアカウントに結び付き、その人が組織を離れると止まり、期限を人が更新するから。App の installation token は、人に結び付かず、1 時間で切れ、そのたびに作り直すので、漏れても長くは使えない。
- 管理者が、App を作り、登録する repository の owner（organization か個人）に install し、登録する GitHub リポジトリを選ぶ。owner が複数なら、それぞれに install する。
- installation token の作り方：
  1. App の ID と秘密鍵で、JWT（RS256、10 分より短い）を作る。
  2. owner の installation を、`GET /repos/{owner}/{repo}/installation`（その owner の登録したリポジトリのどれか）で引く。
  3. `POST /app/installations/{installation_id}/access_tokens` を、`repositories`（その owner の登録したリポジトリの名前）と `permissions`（上の 4 つの Read）を付けて呼ぶ。なぜ絞るか：install で選んだリポジトリが広くても、トークンを登録したリポジトリと読む権限だけにするため。
- installation token は、owner ごとに 1 つ持ち、使い回す。切れるまでの残りが 20 分より短くなったら、次に使うときに作り直す。
  - なぜ 20 分：git の操作 1 回の時間の上限（既定 10 分）のあいだに、切れないようにするため。git は、HTTP の要求を始めるときにだけトークンを使うので、見回りの `ls-remote` のあとの `fetch` を始めるときに、まだ残っていればよい。
  - そのため、GitHub を使う構成では、`LOGIFORGE_GIT_TIMEOUT` を 20 分より短くする。長くすると、worker は起動しない。
  - バックエンドの作業用の複製は、fetch するときにだけトークンを求める。手元の複製だけで読むときは、GitHub の API が落ちていても読める。
  - git には、git の操作のたびに、そのときのトークンを credential helper で渡す（[git アダプタの資格情報](git.md#資格情報)）。
- App が owner に install されていないか、権限が足りなければ、GitHub が断る。worker は、その repository の見回りを誤りにする（ログに出す）。
- REST の上限は、installation ごとに 1 時間に 5,000 回からである。

## 見回りの間隔

- GitHub を使う構成では、worker の見回りの間隔を、既定の 1 秒より長くする。目安は 10 秒である（[見回り](../backend/worker/patrol.md)）。
  - なぜ：repository の数 × 1 秒に 1 回の `git ls-remote` は、GitHub の git の操作の、乱用を防ぐ制限に当たるおそれがあるから（[費用と規模の見積もり](../estimates/README.md#worker)）。
  - そのかわり、push と書き込みが読む側に出るまでの遅れが、間隔の分だけ長くなる。
- 制限に当たったら（`ls-remote` が拒まれる、遅くなる）、間隔をさらに延ばす。
- 記録を読んで控えに足す間隔は、30 秒にする（[記録の控え](../backend/verify/records.md)）。なぜ：repository ごとに REST を 1 回使うので、10 秒ごとでは、repository が多いと上限に当たるから。

## 対応

GitHub に固有のところだけを書く。

| GitForge のポートの操作 | GitHub での実装 |
|---|---|
| repository | 設定に登録した GitHub リポジトリ（1 つの repository に 1 つ）。gitforge_ref は `owner/name` |
| repository を用意する | 空のリポジトリに、作る利用者のトークンで、contents API（`PUT /repos/{owner}/{repo}/contents/{path}`）で最初のコミットを作る |
| 反映する | reviewer のトークンで、`PUT /repos/{owner}/{repo}/pulls/{number}/merge` を、`sha`（先頭のコミット）、`merge_method: squash`、メッセージを付けて呼ぶ |
| レビュー | reviewer のトークンで、pull request の記録のコメント（[レビューの書き方](records.md#レビューの書き方)） |
| 回答の添付 | リンクだけ。ファイルは受け付けない（[人への依頼の回答](../files/artifacts.md)） |
| 反映かの判定 | `GET /repos/{owner}/{repo}/commits/{sha}/pulls`（[反映かの判定](../backend/verify/README.md)） |

- GitHub は、merge のすぐあとに、`GET /repos/{owner}/{repo}/commits/{sha}/pulls` で空の一覧を返すことがある。
  - そのため、worker は、pull request が見つからないコミットを、すぐには反映によらないものと決めない（[反映かの判定](../backend/verify/README.md)）。
- 根の仮説に背景情報があるときは、根のコミットのあとに、2 つ目のコミットで足す（[repository を用意する](#repository-を用意する)）。
  - 2 つ目のコミットには、`Logiforge-Init-Signature:` を付ける（[repository の用意のコミット](../backend/verify/init.md#repository-の用意のコミット)）。
  - なぜ 1 つのコミットにしないか：空のリポジトリでは、Git Data API（blob、tree、commit）が使えず（409）、contents API は 1 回に 1 つのファイルしか書けないから。
- 登録する repository の owner（organization かユーザー）は、いくつでもよい。
- public のリポジトリでも、private のリポジトリでも使える。GitHub Free の private のリポジトリでは、`main` を守る設定が使えないので、守る設定なしで使う（[main を守る](pull-requests.md#main-を守る)）。

### merge を断られたとき

merge の要求に GitHub が次のものを返したら、アダプタは反映の失敗 `merge_forbidden` にする（[反映の手順](../backend/merge.md)）。

- 403。主体に merge の権限がない（Write がない、トークンの権限が足りない）。
  - レート制限による 403 は、`merge_forbidden` にしない。待ってやり直す（[GitHub での main の守りと制約](github-main.md)）。
- 405 で、応答が `Repository rule violations found` のもの。ルールセットが、この主体の merge を断った。
  - 例：ディレクトリの守りの (B) の bypass にいない人が、ディレクトリを変える PR を merge した（[main の守り（ディレクトリ方式）](../directory/main.md#github-の守る設定)）。リポジトリの管理者でも同じである。
  - 応答には、`Waiting on required approvals from <承認の team>` も付く。
  - なぜ待たないか：待っても、断られることは変わらないから。
- ほかの 405 は、`merge_forbidden` にしない。pull request を読み直し、衝突や先頭のコミットの変化として扱う。

## repository を用意する

1. 管理者が、空の GitHub リポジトリを作る。
2. 管理者が、利用者とエージェントに、Write の権限を与える。
3. 管理者が、repository の名前と `owner/name` の組を、サーバの設定に登録する（[repository の登録](README.md#repository-の登録)）。この組が、名前の正本である。
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
      - GitHub は、ほかの OAuth App のトークンには 404 を返す。client secret が違えば 401 を返す。
    - team の所属は、主体のトークンで `GET /user/teams` を呼んで確かめる。アカウント名と team の名前は、大文字と小文字を区別せずに比べる。
  - それ以外の GitHub のアカウントのトークンは、`client` である。
- 読める repository は、主体のトークンで、登録した GitHub リポジトリを読めるかで決める。
- どの操作を使えるかの判定は、バックエンドの [認可](../backend/README.md) が行う。

GitHub での `main` の守り方と、GitHub の制約への対処は、[GitHub での main の守りと制約](github-main.md) に書く。
