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
scripts/compose-setup.sh        # 初めの 1 回だけ。Forgejo を起動し、organization、team、ユーザー、トークンを作る
docker compose up -d --wait
```

- バックエンドの API は、`http://localhost:8080` で待ち受ける。
- Forgejo は、`http://localhost:3000` で待ち受ける。

## 秘密とトークン

- 秘密は `.compose/secrets/` に置き、compose の `secrets` でファイルとして渡す。
- 利用者 alice のトークンは、`.compose/alice_token` にある。
- エージェントのトークンは、次のファイルにある。
  - opener：`.compose/agent_token`
  - reviewer：`.compose/reviewer_token`

## 使ってみる

- WebUI は `http://localhost:8080/` で開く。
  - alice とパスワード（`.compose/alice_password`）で Forgejo にログインして、サインインする。
- CLI で使うときは、次の 2 つを行う。

```sh
logiforge auth login --with-token --host http://localhost:8080 < .compose/alice_token
logiforge auth setup-git --url http://localhost:3000 --insecure
```

## 作り直しと片付け

- Forgejo だけが再起動したときは、worker とバックエンドを作り直す。
  - `docker compose up -d --force-recreate worker backend`
  - なぜ：worker とバックエンドは Forgejo のネットワークを共有しているので、Forgejo のコンテナが変わると、つながらなくなるから。
- すべて消すときは、`docker compose down -v` のあと、`.compose` を消す。

## CI

- CI は、イメージをビルドし、compose で動かして、repository の作成と読みを検査する（[.github/workflows/go.yml](../../.github/workflows/go.yml)）。
