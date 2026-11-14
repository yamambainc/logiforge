# logiforge

仮説検証ハーネス Logiforge のリポジトリである。要件は [docs/](docs/)、設計は [design/](design/) にある。

## 開発

Go で書く。版は [go.mod](go.mod) に書く。

```sh
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go test -race ./...
```

CI は、pull request ごとに同じことを行う（[.github/workflows/go.yml](.github/workflows/go.yml)）。

Forgejo アダプタの結合テストは、動く Forgejo を使う。手元では、Forgejo のバイナリを取ってきて、次のように動かす。

```sh
scripts/forgejo-test.sh <forgejo のバイナリ> 3999 > /tmp/forgejo.env
set -a; . /tmp/forgejo.env; set +a
go test -count=1 -run TestForgejo ./internal/adapters/forgejo/
kill "$FORGEJO_PID"
```

FORGEJO_URL と FORGEJO_ADMIN_TOKEN がなければ、結合テストは飛ばす。

E2E（[internal/e2e](internal/e2e/)）も、同じ Forgejo で動かす。テストが logiforge をビルドし、`serve` と `worker` を起動し、CLI と API で利用の流れを通す。

```sh
go test -count=1 -timeout 20m -v ./internal/e2e/
```

大きめの木の取り込みの時間は、`E2E_LARGE_COMMITS`（main のコミットの数。例：300）を付けたときだけ測る。

## compose で動かす

[compose.yaml](compose.yaml) は、Forgejo、worker、バックエンドの 3 つを、1 つのタスクに見立てて動かす。イメージは [Dockerfile](Dockerfile) の 1 つで、コンテナごとにコマンド（`serve`、`worker`）を変える。形の理由は、[動かし方（コンテナ）](design/deploy.md) に書く。

```sh
docker compose build
scripts/compose-setup.sh        # 初めの 1 回だけ。Forgejo を起動し、organization、team、ユーザー、トークンを作る
docker compose up -d --wait
```

- バックエンドの API は `http://localhost:8080`、Forgejo は `http://localhost:3000` で待ち受ける。
- 秘密は `.compose/secrets/` に置き、compose の `secrets` でファイルとして渡す。利用者 alice のトークンは `.compose/alice_token` にある。
- CLI で試すときは、`logiforge auth login --with-token --host http://localhost:8080 < .compose/alice_token` のあと、`logiforge auth setup-git --url http://localhost:3000 --insecure` を行う。
- すべて消すときは、`docker compose down -v` のあと、`.compose` を消す。

CI は、イメージをビルドし、compose で動かして、repository の作成と読みを確かめる。

## パッケージ

| パッケージ | 中身 |
|---|---|
| [internal/ids](internal/ids/) | ノードの識別子（`h-`、`s-`、`v-` と 10 文字）を作り、確かめる |
| [internal/tree](internal/tree/) | 木のファイルの読み書き、値の計算、不変条件と差分の検査。git には触れない |
| [internal/snapshot](internal/snapshot/) | スナップショットの形（Go の型が正本）、作り方、tree.Files への戻し方、ブランチの差分の重ね方（Overlay）。git には触れない |
| [internal/gitx](internal/gitx/) | git の CLI で、木の git リポジトリを読む。書く操作はない |
| [internal/store](internal/store/) | スナップショットの置き場（キーの決まり、スナップショットの読み書き、手元のディレクトリ） |
| [internal/worker](internal/worker/) | worker（巡回、スナップショットと latest とブランチの履歴、反映の重なりの警告、内部 API） |
| [internal/adapters/snapstore](internal/adapters/snapstore/) | スナップショットの置き場のポートのアダプタ（置き場を読む、worker の内部 API のクライアント） |
| [internal/backend](internal/backend/) | バックエンド（認可、読む API と書く API、反映の手順、着手できる作業、作業の終わりの判定、書き出し） |
| [internal/api](internal/api/) | HTTP の API（/v1）。エラーは application/problem+json |
| [internal/cli](internal/cli/) | opener と reviewer の CLI（認証、clone、ひな形、check、commit、apply request、作業、木、人への依頼。`--json` と終了コード） |
| [internal/e2e](internal/e2e/) | Forgejo と logiforge（serve、worker、CLI）をつないだ E2E |
| [cmd/logiforge](cmd/logiforge/) | 1 つのバイナリ。`logiforge serve`（API）、`logiforge worker`、それ以外は CLI（`logiforge help`） |
| [internal/ports](internal/ports/) | バックエンドのポート（GitForge、認証）のインターフェース |
| [internal/adapters/pullreq](internal/adapters/pullreq/) | GitForge のポートを、pull request と issue と記録のコメントで実装する。GitHub と Forgejo に共通 |
| [internal/adapters/forgejo](internal/adapters/forgejo/) | Forgejo アダプタ（REST の呼び方、repository の用意、認証） |

- `internal/tree` は、worker、CLI の `check`、バックエンドが同じものを使う（[差分の検査](design/backend/checks.md)、[ファイルの配置](design/files/layout.md)）。

## worker のロック

`logiforge worker` は、clone のディレクトリと置き場の根に、`.worker.lock` のロックを取る。unix では、worker が止まればロックは外れる。unix でない環境では、ロックはファイルで、worker が落ちると残る。そのときは、ほかの worker が動いていないことを確かめてから、2 つのディレクトリの `.worker.lock` を手で消す。
