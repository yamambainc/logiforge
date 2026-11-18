# GitHub App とトークン

GitHub を使う構成で、どの主体がどのトークンで GitHub を読み書きするかと、Logiforge の GitHub App の権限を確かめるときに読む。
GitHub アダプタのほかの決まりは、[GitHub アダプタ](github.md) に書く。

## アカウントとトークン

Logiforge は、GitHub に書くための自分のアカウント（書き込み主体）を持たない。GitHub に書くのは、いつも、API の要求を送った主体のトークンである（[GitForge アダプタの共通の書き方](pull-requests.md)）。
Logiforge が持つ GitHub のものは、1 つの GitHub App だけである。personal access token と OAuth App は使わない。

| 使う者 | トークン | 要る権限 |
|---|---|---|
| 利用者と管理者（人） | WebUI のサインインで得る、GitHub App の user access token | App の権限と、その人のリポジトリの権限の、両方にあるもの |
| opener と reviewer（人とエージェント） | `gh auth login`（GitHub CLI の OAuth App）で得たトークン | スコープ `repo`。リポジトリへの Write（ブランチの push、pull request とコメントとレビュー、reviewer は merge） |
| worker と、バックエンドの作業用の git の複製 | GitHub App の installation token | Contents、Issues、Pull requests、Metadata の Read だけ |

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
| Repository の Contents | Read and write | 利用者が repository を用意する（contents API と Git Data API）。worker が git を読む |
| Repository の Issues | Read and write | 利用者が、人への依頼に回答し、issue を閉じる。worker が記録のコメントを読む |
| Repository の Pull requests | Read | WebUI で apply request を読む。worker が反映かを判定する |
| Repository の Metadata | Read | GitHub が必ず求める |
| Organization の Members | Read | バックエンドが、利用者の team の所属を確かめる（[認証](github.md#認証)） |

- Webhook は使わない。反映かの判定は、中身だけで決める（[反映かの判定](../backend/verify/README.md)）。
- 書く権限は、user access token のためだけにある。installation token には、Read だけを求める（下の「installation token」）。
  - なぜ 1 つの App にまとめるか：管理者が作り、install し、秘密を守るものを、1 つにするため。
- 管理者が、App を、登録する GitHub リポジトリの owner（organization か個人）に install し、リポジトリを選ぶ。owner が複数なら、それぞれに install する。
- App が owner に install されていないか、権限が足りなければ、GitHub が断る。worker は、その repository の見回りを誤りにする（ログに出す）。

## installation token

1. App の ID と秘密鍵で、JWT（RS256、10 分より短い）を作る。
2. owner の installation を、`GET /repos/{owner}/{repo}/installation`（その owner の登録したリポジトリのどれか）で引く。
3. `POST /app/installations/{installation_id}/access_tokens` を、`repositories`（その owner の登録したリポジトリの名前）と `permissions`（Contents、Issues、Pull requests、Metadata の Read）を付けて呼ぶ。
   - なぜ絞るか：App の権限と install で選んだリポジトリが広くても、worker のトークンを、登録したリポジトリを読むことだけにするため。

- installation token は、owner ごとに 1 つ持ち、使い回す。切れるまでの残りが 20 分より短くなったら、次に使うときに作り直す。
  - なぜ 20 分：git の操作 1 回の時間の上限（既定 10 分）のあいだに、切れないようにするため。git は、HTTP の要求を始めるときにだけトークンを使う。
  - そのため、GitHub を使う構成では、git の操作の時間の上限を 20 分より短くする。長い値は、設定の画面で受け付けない。
- バックエンドの作業用の複製は、fetch するときにだけトークンを求める。
- git には、git の操作のたびに、そのときのトークンを credential helper で渡す（[git アダプタの資格情報](git.md#資格情報)）。
- REST の上限は、installation ごとに 1 時間に 5,000 回からである。

## user access token

- user access token は、8 時間で切れる。
- バックエンドは、交換の応答から refresh token を除いて、ブラウザに返す。refresh token は保持しない（[トークンの持ち方](../webui/auth.md#トークンの持ち方)）。
  - 切れたら、WebUI は authorize に行き直す。許可済みなら、GitHub は同意の画面を出さずに戻す。
  - なぜ refresh token を使わないか：盗まれたときに使える期間を、8 時間までに収めるため。
