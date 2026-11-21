# compose で動かす

Forgejo、worker、バックエンドを、手元のコンテナで動かすときに読む。
既存のプロジェクトで使い始めるときは、[既存のプロジェクトへの導入](../adoption/README.md) から読む。

## 形

- [compose.yaml](../../compose.yaml) は、Forgejo、worker、バックエンドの 3 つを、1 つのタスクに見立てて動かす。
- イメージは [Dockerfile](../../Dockerfile) の 1 つである。
  - コンテナごとに、コマンド（`serve`、`worker`）を変える。
- 形の理由は、[動かし方（コンテナ）](../../design/deploy/README.md) に書く。

## 起動

```sh
docker compose build
scripts/compose-setup.sh        # 初めの 1 回だけ。鍵を作り、Forgejo に organization、team、ユーザー、OAuth2 のアプリケーションを作り、設定の DB に書く
docker compose up -d --wait
```

- バックエンドの API は、`http://localhost:8080` で待ち受ける。
- Forgejo は、`http://localhost:3000` で待ち受ける。

## 設定と秘密

- 起動時に渡すのは、設定の DB の場所と 2 つの鍵と、ホストに結び付くものだけである（[compose.yaml](../../compose.yaml)、[環境変数](../../design/deploy/env.md)）。
- 2 つの鍵は `.compose/secrets/` に置き、compose の `secrets` でファイルとして渡す。worker には、バックエンドの鍵を渡さない。
- ほかの設定（Forgejo の URL と organization、OAuth2 のアプリケーション、worker のトークン）は、`scripts/compose-setup.sh` が `logiforge settings load` で設定の DB に書く（[設定のコマンド](../../design/settings/README.md#設定のコマンド)）。
  - そのため、compose では、セットアップの画面を通らない。
  - 設定の DB は、ボリューム `config` に置く。worker は、読むだけでマウントする。

## トークン

- 利用者 alice のトークンは、`.compose/alice_token` にある。
  - WebUI のサインインと同じ流れ（Forgejo の OAuth2 のアプリケーション）で得たトークンである。1 時間で切れる。
  - なぜ：利用者（`user`）になるのは、WebUI のサインインで得たトークンだけだから。personal access token では `client` になる（[利用者](../../design/settings/users.md)）。
- エージェントのトークンは、次のファイルにある。
  - opener：`.compose/agent_token`
  - reviewer：`.compose/reviewer_token`

## 使ってみる

- WebUI は `http://localhost:8080/` で開く。
  - alice とパスワード（`.compose/alice_password`）で Forgejo にログインして、サインインする。
- CLI で使うときは、エージェントのトークンで、次の 2 つを行う。CLI の主体は `client` である。

```sh
logiforge auth login --with-token --host http://localhost:8080 < .compose/agent_token
logiforge auth setup-git --url http://localhost:3000 --insecure
```

## 作り直しと片付け

- Forgejo だけが再起動したときは、worker とバックエンドを作り直す。
  - `docker compose up -d --force-recreate worker backend`
  - なぜ：worker とバックエンドは Forgejo のネットワークを共有しているので、Forgejo のコンテナが変わると、つながらなくなるから。
- すべて消すときは、`docker compose down -v` のあと、`.compose` を消す。

## CI

- CI は、イメージをビルドし、compose で動かして、repository の作成と読みを検査する（[.github/workflows/go.yml](../../.github/workflows/go.yml)）。
