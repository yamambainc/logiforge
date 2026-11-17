# Logiforge の repository を GitHub に置く

別リポジトリ方式と subtree 方式で、Logiforge の repository も GitHub に置くときに読む。apply request と人への依頼は、GitHub の pull request と issue になる。
共通の手順は [別リポジトリ方式の導入](separate-repository.md) にある。このページは、その「Logiforge を動かす」から「main を守る」までを置き換える。

## 構成

- Logiforge は、GitHub に書くための自分のアカウント（GitHub App など）を持たない。GitHub に書くのは、いつも、要求を送った人やエージェントのトークンである（[GitHub アダプタ](../../design/adapters/github.md)）。
  - 反映（merge）は、`approve` をした reviewer のトークンで行う。承認された apply request を、人が GitHub の画面で merge してもよい。反映によるものかは、worker が中身で決める（[反映かの判定](../../design/backend/verify.md)）。
- repository は、管理者が GitHub に作り、サーバの設定に登録する。登録していない名前では作れない（`repository_not_registered`）。
- 登録する repository の owner（organization か個人）は、いくつでもよい。
- repository は、public でも private でもよい。
  - `main` を守る設定は、勧めるが求めない（下の「main を守る」）。GitHub Free の private のリポジトリでは、守る設定が使えない。そのときは、守る設定なしで使う。
  - 守る設定がなくても、反映によらない変更は、worker が見つけて、repository を壊れた状態にする（[壊れた repository を直す](../apply-request/repair.md)）。

## 用意するもの

| もの | 作る人 | 使い道 |
|---|---|---|
| OAuth App | organization の owner | WebUI のサインイン。利用者を `user` とみなすのに使う |
| 読むだけのトークン | 管理者 | worker と、バックエンドの作業用の git の複製が、repository を読む |
| 署名の秘密（`LOGIFORGE_RECORD_KEYS`） | 管理者 | 記録の署名。バックエンドだけが持つ |

### OAuth App

organization の「Settings」→「Developer settings」→「OAuth Apps」で作る。

- Authorization callback URL：WebUI の `https://logiforge.example.com/oauth/callback`
- 作ったら、Client ID と client secret を控える。
- OAuth App がなくても、CLI（`client`）だけで使える。そのときは、`user` の主体がいないので、WebUI で repository を作ることと、人への依頼に答えることができない。

### 読むだけのトークン

- fine-grained personal access token を、Logiforge の repository だけに絞って作る。権限は、Contents、Issues、Pull requests の Read と、Metadata の Read である。
- public のリポジトリだけなら、なくても動く。そのときは、トークンなしで読む。
  - ただし、トークンのない要求の上限は、1 時間に 60 回（送り元の IP ごと）である。反映かの判定で、`main` のコミットごとに REST を読むので、ふつうは足りない。試すときだけにする。
- private のリポジトリでは、要る。

## repository を作って登録する

1. 管理者が、空の GitHub のリポジトリを作る（例：`acme/myapp-cache-strategy`）。README などは足さない。
2. 管理者が、利用者とエージェントに、リポジトリの Write の権限を与える。
   - 利用者は、repository の用意のコミットを置き、回答のコメントを書く。エージェントは、ブランチを push し、pull request とコメントを書く。reviewer は、merge する。
3. サーバの設定 `LOGIFORGE_GITHUB_REPOSITORIES` に、名前と `owner/name` の組を足し、バックエンドと worker を起動し直す。
4. 利用者が、WebUI で repository を作る（[repository を作る](separate-repository.md#repository-を作る)）。
5. 使えるなら、`main` を守るルールセットを作る（下の「main を守る」）。
   - なぜ用意のあとか：用意の 2 つ目のコミット（背景情報）は、`main` を直接進めるから（[GitHub アダプタ](../../design/adapters/github.md) の「repository を用意する」）。

## main を守る

守る設定は、勧めるが求めない。使えるときは、リポジトリの「Settings」→「Rules」→「Rulesets」で、ブランチのルールセットを作る。

- Target branches：`main`（Include default branch）
- Bypass list：だれも入れない
- Rules：
  - Require a pull request before merging（承認の数は 0）
  - Restrict deletions
  - Block force pushes
- 「Require branches to be up to date before merging」は使わない（[衝突](../../design/backend/merge.md)）。
- merge できる人は絞らない。reviewer が merge するからである。
- 守る設定の正本は、[GitHub での main の守りと制約](../../design/adapters/github-main.md) である。

## Logiforge を動かす

- タスクの形は、[動かし方（コンテナ）](../../design/deploy.md) に従う。環境変数は、その「GitHub を使う構成」の表に従う。
- 1 台の Linux のマシンに systemd で置き、Tailscale で公開する手順は、[1 台の Linux に置く](linux-host/README.md) に書く。
- 手元で試すときは、バイナリで、worker とバックエンドを動かせる。

```sh
export LOGIFORGE_GITFORGE=github
export LOGIFORGE_GITHUB_REPOSITORIES=myapp-cache-strategy=acme/myapp-cache-strategy
export LOGIFORGE_GITHUB_READ_TOKEN_FILE=read_token          # public のリポジトリで試すだけなら、なくてもよい
export LOGIFORGE_RECORD_KEYS="k1=$(openssl rand -hex 20)"
export LOGIFORGE_RECORD_PUBLIC_KEYS=$(logiforge record-public-keys)
export LOGIFORGE_STORE_DIR=$PWD/store LOGIFORGE_WORKER_DIR=$PWD/worker
export LOGIFORGE_WORKER_INTERVAL=10s LOGIFORGE_WORKER_RECORD_INTERVAL=30s
logiforge worker &

export LOGIFORGE_USER_TEAM=acme/logiforge-users
export LOGIFORGE_OAUTH_CLIENT_ID=<Client ID> LOGIFORGE_OAUTH_CLIENT_SECRET_FILE=client_secret
export LOGIFORGE_OAUTH_REDIRECT_URI=http://localhost:8080/oauth/callback
logiforge serve
```

- `LOGIFORGE_RECORD_KEYS` は、作ったら控えて、変えない。worker には、公開鍵（`LOGIFORGE_RECORD_PUBLIC_KEYS`）だけを渡す（[設定と秘密情報](../../design/adapters/README.md#設定と秘密情報)）。

## 利用者とエージェント

- 利用者は、WebUI で、Logiforge の OAuth App でサインインする。`LOGIFORGE_USER_LOGINS` か `LOGIFORGE_USER_TEAM` に入っていれば `user` になる。
- CLI は `client` として動く。トークンは、[CLI の設計](../../design/cli/README.md) の「認証」に従う。
  - 人は、gh のトークンでよい。
  - エージェントは、登録した repository だけに絞った fine-grained personal access token を使う。

```sh
gh auth token | logiforge auth login --with-token --host https://logiforge.example.com
logiforge auth setup-git --url https://github.com
logiforge repo clone myapp-cache-strategy
```

- このあとは、共通の手順の「プロジェクトとつなぐ」に戻る。
