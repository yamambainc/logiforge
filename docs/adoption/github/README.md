# Logiforge の repository を GitHub に置く

別リポジトリ方式と subtree 方式で、Logiforge の repository も GitHub に置くときに読む。
apply request と人への依頼は、GitHub の pull request と issue になる。
共通の手順は [別リポジトリ方式の導入](../separate-repository.md) にある。このページは、その「Logiforge を動かす」から「main を守る」までを置き換える。

## 構成

- Logiforge は、GitHub に書くための自分のアカウントを持たない。GitHub に書くのは、いつも、要求を送った人やエージェントのトークンである（[GitHub App とトークン](../../../design/adapters/github-app.md)）。
  - Logiforge が持つ GitHub のものは、1 つの GitHub App だけである。worker の読み取りと、WebUI のサインインに使う（[GitHub App](apps.md)）。
  - personal access token は使わない。
  - 反映（merge）は、`approve` をした reviewer のトークンで行う。承認された apply request を、人が GitHub の画面で merge してもよい。反映によるものかは、worker が中身で決める（[反映かの判定](../../../design/backend/verify/README.md)）。
- repository は、GitHub でリポジトリを作る人（owner の admin か、repository を作れる人）が GitHub に作り、利用者が設定の画面で登録する。登録していない名前では作れない（`repository_not_registered`）。
- 登録する repository の owner（organization か個人）は、いくつでもよい。
- repository は、public でも private でもよい。
  - `main` を守る設定は、勧めるが求めない（下の「main を守る」）。GitHub Free の private のリポジトリでは、守る設定が使えない。そのときは、守る設定なしで使う。
  - 守る設定がなくても、反映によらない変更は、worker が見つけて、repository を壊れた状態にする（[壊れた repository を直す](../../apply-request/repair.md)）。

## Logiforge を動かす

- タスクの形は、[動かし方（コンテナ）](../../../design/deploy/README.md) に従う。起動時に渡す環境変数は、[環境変数](../../../design/deploy/env.md) の表に従う。
  - 渡すのは、設定の DB の場所、2 つの鍵、ホストに結び付くディレクトリとアドレスだけである。ほかの設定は、設定の DB に置く（[設定](../../../design/settings/README.md)）。
- 1 台の Linux のマシンに systemd で置き、Tailscale で公開する手順は、[1 台の Linux に置く](../linux-host/README.md) に書く。
- v0.3.0 からの移行には対応しない。新しく入れ、初回のセットアップから始める。
- 手元で試すときは、バイナリで、worker とバックエンドを動かせる。

```sh
head -c 32 /dev/urandom | base64 > backend_key
head -c 32 /dev/urandom | base64 > shared_key
export LOGIFORGE_DB=$PWD/settings.db LOGIFORGE_SHARED_KEY_FILE=$PWD/shared_key
export LOGIFORGE_STORE_DIR=$PWD/store LOGIFORGE_WORKER_DIR=$PWD/worker
logiforge worker &
LOGIFORGE_BACKEND_KEY_FILE=$PWD/backend_key logiforge serve
```

- 鍵は、設定の DB と別の場所に置き、バックアップを取る（[鍵を置く場所](../../../design/settings/secrets.md#鍵を置く場所)）。
- worker には、バックエンドの鍵を渡さない。

## セットアップ

初めて起動したあとに 1 回だけ行う。画面は [セットアップ](../../webui/setup.md)、流れの正本は [初回のセットアップ](../../../design/settings/setup.md) である。

1. 空の GitHub のリポジトリを 1 つ作る（例：`acme/myapp-cache-strategy`）。README などは足さない。
   - 最初の利用者になる人に、そのリポジトリの Write を与える。なぜ：最後の段で、そのリポジトリに書ける人だけが最初の利用者になれるから。
2. WebUI（例：`https://logiforge.example.com/setup`）を開き、構成で GitHub を選ぶ。
3. GitHub App を作る owner と WebUI の URL を入れ、「GitHub で App を作る」を押す。GitHub の画面で App を作る（[GitHub App](apps.md)）。
   - その owner の admin の権限が要る（organization なら organization の owner）。
4. 「GitHub で install する」を押し、App を、登録するリポジトリの owner に install する。
5. 最初の repository の名前と `owner/name` を入れる。
6. 「GitHub でサインインして、利用者になる」を押す。サインインした人が、最初の利用者になる。

- 途中でページを閉じても、入れた設定は残る。開き直せば、続きから進む。

## repository を作って登録する

2 つ目からの repository は、次のように足す。

1. 空の GitHub のリポジトリを作る。README などは足さない。
2. 利用者とエージェントに、リポジトリの Write の権限を与える。
   - 利用者は、repository の用意のコミットを置き、回答のコメントを書き、WebUI でレビューする。
   - エージェントは、ブランチを push し、pull request とコメントを書く。
   - reviewer は、merge する。
3. App の install の設定で、そのリポジトリを選び足す（App を「Only select repositories」で install したとき）。
4. 利用者が、[設定の画面](../../webui/settings.md) の「repository」で、名前と `owner/name` を足す。サーバは起動し直さない。
5. 利用者が、WebUI で repository を作る（[repository を作る](../separate-repository.md#repository-を作る)）。
6. 使えるなら、`main` を守るルールセットを作る（下の「main を守る」）。
   - なぜ用意のあとか：用意の 2 つ目のコミット（背景情報）は、`main` を直接進めるから（[GitHub アダプタ](../../../design/adapters/github.md) の「repository を用意する」）。

- セットアップで登録した最初の repository も、5 と 6 を行う。

## 見回りの間隔

- 利用者が、設定の画面の「GitForge」で、見回りの間隔を 10 秒に、記録を読む間隔を 30 秒にする。既定の見回りの間隔は 1 秒である。
  - なぜ：GitHub の git の操作と REST の上限に当たらないようにするため（[GitHub アダプタ](../../../design/adapters/github.md) の「見回りの間隔」）。

## main を守る

守る設定は、勧めるが求めない。使えるときは、リポジトリの「Settings」→「Rules」→「Rulesets」で、ブランチのルールセットを作る。

- 規則と bypass は、[GitHub での main の守りと制約](../../../design/adapters/github-main.md#main-への書き込みを守る) に従う。
- Target branches は、`main`（Include default branch）にする。
- Require a pull request before merging の承認の数は、0 にする。

## 利用者とエージェント

- 利用者の名簿はない。WebUI でサインインし、登録した repository のどれかに Write 以上がある人が、利用者（`user`）になる（[利用者](../../../design/settings/users.md)）。
  - サインインは、Logiforge の GitHub App で行う。
  - 利用者は、設定も変えられる。そのため、登録したリポジトリの Write は、信頼できる人にだけ配る。
  - 利用者は、WebUI の apply request の画面で、approve、変更の求め、コメントができる。書くのは、本人のトークンである。
- CLI は `client` として動く。トークンは、[CLI の認証](../../../design/cli/auth.md) に従う。
  - 人もエージェントも、`gh auth login` でサインインして得たトークン（OAuth）を使う。スコープ `repo` が要る（`gh auth login` の既定に入っている）。personal access token は使わない。
  - エージェントの機械用アカウントは、`gh auth login` が出すコードを、そのアカウントでサインインしたブラウザで入れる。
  - エージェントの機械用アカウントは、WebUI でサインインしない。なぜ：Write を持つので、サインインすると利用者になり、設定を変えられるから。
  - organization が OAuth App のアクセスを制限しているときは、organization の owner が GitHub CLI（OAuth App である）を許す（[CLI がつながらないとき](#cli-がつながらないとき)）。

- CLI は、トークンを渡さなくても、`gh auth token` を読む（[gh のトークン](../../../design/cli/auth.md#gh-のトークン)）。そのため、`gh auth login` のあとは、バックエンドの URL だけを置く。
  - gh のトークンが変わっても、置き直さなくてよい。

```sh
gh auth login --hostname github.com --git-protocol https --web
logiforge auth login --host https://logiforge.example.com
logiforge auth setup-git --url https://github.com
logiforge repo clone myapp-cache-strategy
```

- このあとは、共通の手順の「プロジェクトとつなぐ」に戻る。

### CLI がつながらないとき

- gh がない、またはログインしていない。
  - CLI は「gh がない」か「gh auth token --hostname <ホスト> でトークンを得られなかった」と示して止まる。
  - gh を入れて、`gh auth login --hostname github.com --git-protocol https --web` でログインする。
- gh のトークンに、スコープ `repo` がない。
  - `gh auth status` で、トークンのスコープを確かめる。
  - 足りなければ、`gh auth refresh --hostname github.com --scopes repo` で足す。
- organization が OAuth App のアクセスを制限している。
  - `repo` があっても、その organization のリポジトリの push や pull request が GitHub に断られる。
  - organization の owner が、organization の設定の Third-party Access（OAuth application policy）で、GitHub CLI を許す。
