# GitHub App とトークン

GitHub を使う構成で、どの主体がどのトークンで GitHub を読み書きするかと、Logiforge の GitHub App の権限を確かめるときに読む。
GitHub アダプタのほかの決まりは、[GitHub アダプタ](github.md) に書く。

## アカウントとトークン

Logiforge は、GitHub に書くための自分のアカウント（書き込み主体）を持たない。GitHub に書くのは、いつも、API の要求を送った主体のトークンである（[GitForge アダプタの共通の書き方](pull-requests.md)）。
Logiforge が持つ GitHub のものは、1 つの GitHub App だけである。personal access token と OAuth App は使わない。

| 使う者 | トークン | 要る権限 |
|---|---|---|
| 利用者（人） | WebUI のサインインで得る、GitHub App の user access token | App の権限と、その人のリポジトリの権限の、両方にあるもの |
| opener と reviewer（人とエージェント） | `gh auth login`（GitHub CLI の OAuth App）で得たトークン | スコープ `repo`。リポジトリへの Write（ブランチの push、pull request とコメントとレビュー、reviewer は merge） |
| worker | バックエンドが作った installation token（[worker のトークン](#worker-のトークン)） | Contents、Issues、Pull requests、Metadata の Read だけ |
| バックエンドの作業用の git の複製 | GitHub App の installation token | 同じ |

- reviewer に merge の権限が要る。なぜ：反映は、`approve` をした reviewer のトークンで merge するから（[反映の手順](../backend/merge.md)）。
- opener と reviewer のトークンの決まりは、[CLI の認証](../cli/auth.md) に書く。
  - `repo` は、そのアカウントが書けるすべてのリポジトリに及ぶ。範囲は広いが、受け入れる。なぜ：OAuth のスコープは、リポジトリごとに絞れないから。
  - 絞りたいときは、Logiforge のためだけの機械用アカウントを作り、登録した repository にだけ Write を与える。
  - organization が OAuth App のアクセスを制限しているときは、organization の owner が GitHub CLI を許す。
  - `gh auth login --with-token` で personal access token を gh に入れたものは使わない。
- personal access token を使わない理由：人のアカウントに結び付き、その人が組織を離れると止まり、期限を人が更新するから。

## GitHub App

App は、初回のセットアップで、manifest flow で作る（[初回のセットアップ](../settings/setup.md#github-を使う構成app-を-manifest-flow-で作る)）。App の ID、client_id、秘密鍵、client secret は、設定の DB に置く（[秘密の暗号化](../settings/secrets.md)）。

| 権限 | 値 | 使う者と理由 |
|---|---|---|
| Repository の Contents | Read and write | 利用者が repository を用意する（contents API と Git Data API）。利用者が、WebUI のレビューの approve で、pull request を merge する（merge は Contents の write が要る）。worker が git を読む |
| Repository の Issues | Read and write | 利用者が、人への依頼に回答し、issue を閉じる。worker が記録のコメントを読む |
| Repository の Pull requests | Read and write | 利用者が、WebUI のレビューで、pull request に記録のコメントを書く（pull request へのコメントは、Pull requests の write が要る）。WebUI で apply request を読む。worker が反映かを判定する |
| Repository の Metadata | Read | GitHub が必ず求める |

- Webhook は使わない。反映かの判定は、中身だけで決める（[反映かの判定](../backend/verify/README.md)）。
- 書く権限は、user access token のためだけにある。installation token には、Read だけを求める（下の「installation token」）。
- App の秘密鍵を読むのは、バックエンドだけである。worker には渡さない（[秘密ごとの鍵](../settings/secrets.md#秘密ごとの鍵)）。
  - なぜ 1 つの App にまとめるか：作り、install し、秘密を守るものを、1 つにするため。
- 利用者が、App を、登録する GitHub リポジトリの owner（organization か個人）に install し、リポジトリを選ぶ。owner が複数なら、それぞれに install する。
  - install には、その owner の admin の権限が要る（organization なら organization の owner）。
- App が owner に install されていないか、権限が足りなければ、GitHub が断る。worker は、その repository の見回りを誤りにする（ログに出す）。

## installation token

1. App の ID と秘密鍵で、JWT（RS256、10 分より短い）を作る。
2. owner の installation を、`GET /repos/{owner}/{repo}/installation`（その owner の登録したリポジトリのどれか）で引く。
3. `POST /app/installations/{installation_id}/access_tokens` を、`repositories`（その owner の登録したリポジトリの名前）と `permissions`（Contents、Issues、Pull requests、Metadata の Read）を付けて呼ぶ。
   - なぜ絞るか：App の権限と install で選んだリポジトリが広くても、worker のトークンを、登録したリポジトリを読むことだけにするため。

- バックエンドの作業用の複製は、fetch するときにだけトークンを作る。切れるまでの残りが 20 分より短くなるまで、使い回す。
- git には、git の操作のたびに、そのときのトークンを credential helper で渡す（[git アダプタの資格情報](git.md#資格情報)）。
- REST の上限は、installation ごとに 1 時間に 5,000 回からである。

## worker のトークン

worker は、App の秘密鍵を持たない。バックエンドが作った installation token を、設定の DB から読んで使う。

- バックエンドは、1 分ごとに、登録した repository の owner ごとに、設定の DB の worker のトークンを確かめる。
  - なければ、または切れるまでの残りが 40 分より短ければ、作り直して、期限と一緒に置く（共有の鍵で暗号化する。[秘密ごとの鍵](../settings/secrets.md#秘密ごとの鍵)）。
  - repository を登録したときと外したときは、その owner のトークンを、すぐに作り直す。なぜ：トークンを、登録したリポジトリだけに絞るから。
  - バックエンドが何台かあれば、どれも同じことをする。同じ owner のトークンを 2 台が作っても、どちらも使える。
- worker は、git の操作と REST を始めるたびに、その owner のトークンを設定の DB から読む。
  - 切れるまでの残りが 20 分より短ければ、その owner の repository の見回りを飛ばし、ログに出す。次の見回りで、読み直す。
  - なぜ 20 分：git の操作 1 回の時間の上限（既定 10 分）のあいだに、切れないようにするため。git は、HTTP の要求を始めるときにだけトークンを使う。
  - そのため、GitHub を使う構成では、git の操作の時間の上限を 20 分より短くする。長い値は、設定の画面で受け付けない。
- バックエンドが止まると、トークンは作り直されない。
  - トークンは作ってから 1 時間で切れるので、worker は、バックエンドが止まってから 20〜40 分で見回りを止め、ログに出して待つ。バックエンドが戻れば、次の見回りから続ける。
  - なぜ受け入れるか：バックエンドが止まっているあいだは、反映も止まるから。止まっているあいだに入った変更は、戻ったあとに順に取り込む。
- なぜこの形にするか：App を 1 つにしたまま、worker を乗っ取られても、GitHub に書けないようにするため。

## user access token

- user access token は、8 時間で切れる。
- バックエンドは、交換の応答から refresh token を除いて、ブラウザに返す。refresh token は保持しない（[トークンの持ち方](../webui/auth.md#トークンの持ち方)）。
  - 切れたら、WebUI は authorize に行き直す。許可済みなら、GitHub は同意の画面を出さずに戻す。
  - なぜ refresh token を使わないか：盗まれたときに使える期間を、8 時間までに収めるため。
