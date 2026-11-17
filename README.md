# logiforge

仮説検証ハーネス Logiforge のリポジトリである。要件は [docs/](docs/)、設計は [design/](design/) にある。

## インストール

logiforge は、1 つのバイナリである。CLI と、バックエンドの `serve` と、`worker` が入っている。WebUI も、バイナリに入っている（`serve` が配る）。

- CLI（opener と reviewer が使う）は、Linux・macOS・Windows で動く。
- `serve` と `worker` は、Linux のコンテナで動かす前提である（[compose で動かす](#compose-で動かす)、[design/deploy.md](design/deploy.md)）。ほかの OS のバイナリでも起動はするが、確かめていない。

[GitHub Releases](https://github.com/yamambainc/logiforge/releases) に、Linux・macOS・Windows × amd64・arm64 の版を置く。

- この repository は private である。取るには、yamambainc/logiforge を読む権限のある GitHub のアカウントが要る。
- [gh](https://cli.github.com/) で取る（`gh auth login` で、読む権限のあるアカウントにサインインしておく）。

```sh
# 最新の版。Linux の amd64 の例（ほかは darwin・windows、arm64）
gh release download -R yamambainc/logiforge --pattern 'logiforge_*_linux_amd64.tar.gz' --pattern checksums.txt
sha256sum --ignore-missing -c checksums.txt
tar xzf logiforge_*_linux_amd64.tar.gz logiforge
./logiforge version

# 版を決めて取る
gh release download v0.1.0 -R yamambainc/logiforge --pattern 'logiforge_*_darwin_arm64.tar.gz' --pattern checksums.txt
```

- Windows は zip である。macOS では `shasum -a 256 --ignore-missing -c checksums.txt` を使う。
- macOS：バイナリには、署名と公証をしていない。ブラウザで取ったファイルは、初めて動かすときに止められる。チェックサムを確かめたうえで、隔離の印を外す。

  ```sh
  xattr -d com.apple.quarantine ./logiforge
  ```

- `logiforge version` は、版、コミット、ビルドした日付を示す（`--json` で JSON）。

### リリースの作り方

タグ（`v0.1.0` など）を push すると、CI（[.github/workflows/release.yml](.github/workflows/release.yml)）が [GoReleaser](https://goreleaser.com/)（[.goreleaser.yaml](.goreleaser.yaml)）でバイナリを作り、Releases に置く。チェックサム（`checksums.txt`、SHA-256）も置く。

- WebUI を先にビルドし（`webui/` で `npm ci` と `npm run build`）、バイナリに入れる。
- 版とコミットと日付は、`-ldflags` で入れる（[internal/version](internal/version/)）。
- リリースの本文の先頭に書くこと（互換のない変更と、移り方）は、`release-notes/<タグ>.md`（例：[release-notes/v0.2.0.md](release-notes/v0.2.0.md)）に書く。タグを push すると、CI がそのファイルを本文の先頭に置く。なければ、コミットの一覧だけになる。
  - 互換のない変更を main に入れるときは、次の版のファイルに書き足す。
  - リンクは、タグを固定した絶対の URL（`https://github.com/yamambainc/logiforge/blob/<タグ>/...`）で書く。なぜ：本文は Releases のページに置かれるので、相対のリンクは切れるから。
- 手元で試すときは、どこにも置かずに `dist/` に作る：`goreleaser release --snapshot --clean`
- pull request ごとの CI でも、linux/amd64 だけを作り、WebUI と版とコミットが入ることを確かめる（[go.yml](.github/workflows/go.yml) の `release-snapshot`）。
- リリースのジョブは repository に書く権限を持つので、外の action はコミットの SHA で、GoReleaser は版を決めて使う。

## 開発

Go で書く。Go のバージョンは [go.mod](go.mod) に書く。

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

ブラウザの E2E（WebUI のサインインから）は、Chrome のパスを `LOGIFORGE_E2E_CHROME` に渡したときだけ動かす。先に WebUI をビルドしておく（下の「WebUI」）。スクリーンショットは `LOGIFORGE_E2E_OUT` に書く。

大きめの木の取り込みの時間は、`E2E_LARGE_COMMITS`（main のコミットの数。例：300）を付けたときだけ測る。

## WebUI

WebUI は [webui/](webui/) にある。React と TypeScript を Vite でビルドし、出力（`webui/dist/`）を `logiforge serve` が embed して配る。設計は [design/webui](design/webui/README.md) に書く。

```sh
cd webui
npm ci
npm test            # 単体のテスト
npm run build       # 型の検査、ビルド、インラインの script と style がないことの確かめ
npm run dev         # 開発。/v1 は手元の serve（127.0.0.1:8080）に渡す
```

- `webui/dist/` をビルドしてから `go build` すると、バイナリに WebUI が入る。ビルドしていなければ、`serve` は WebUI のかわりに案内を返す。
- サインインには、`serve` に `LOGIFORGE_OAUTH_CLIENT_ID`、`LOGIFORGE_OAUTH_CLIENT_SECRET`、`LOGIFORGE_OAUTH_REDIRECT_URI` を渡す（[サインイン](design/webui/auth.md)）。

## compose で動かす

既存のプロジェクトで使い始めるときは、[既存のプロジェクトへの導入](docs/adoption/README.md) から読む。

[compose.yaml](compose.yaml) は、Forgejo、worker、バックエンドの 3 つを、1 つのタスクに見立てて動かす。イメージは [Dockerfile](Dockerfile) の 1 つで、コンテナごとにコマンド（`serve`、`worker`）を変える。形の理由は、[動かし方（コンテナ）](design/deploy.md) に書く。

```sh
docker compose build
scripts/compose-setup.sh        # 初めの 1 回だけ。Forgejo を起動し、organization、team、ユーザー、トークンを作る
docker compose up -d --wait
```

- バックエンドの API は `http://localhost:8080`、Forgejo は `http://localhost:3000` で待ち受ける。
- 秘密は `.compose/secrets/` に置き、compose の `secrets` でファイルとして渡す。利用者 alice のトークンは `.compose/alice_token` に、エージェント（opener と reviewer）のトークンは `.compose/agent_token` と `.compose/reviewer_token` にある。
- WebUI は `http://localhost:8080/` で開く。alice と、パスワード（`.compose/alice_password`）で、Forgejo にログインしてサインインする。
- CLI で試すときは、`logiforge auth login --with-token --host http://localhost:8080 < .compose/alice_token` のあと、`logiforge auth setup-git --url http://localhost:3000 --insecure` を行う。
- Forgejo だけが再起動したときは、`docker compose up -d --force-recreate worker backend` で、worker とバックエンドを作り直す。2 つは Forgejo のネットワークを共有しているので、Forgejo のコンテナが変わると、つながらなくなるからである。
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
| [internal/worker](internal/worker/) | worker（見回り、スナップショットと latest とブランチの履歴、反映の重なりの警告、ハートビート） |
| [internal/adapters/snapstore](internal/adapters/snapstore/) | スナップショットの置き場のポートのアダプタ（置き場を読む） |
| [internal/backend](internal/backend/) | バックエンド（認可、読む API と書く API、反映の手順、着手できる作業、作業の終わりの判定、書き出し） |
| [internal/api](internal/api/) | HTTP の API（/v1）。エラーは application/problem+json。WebUI の配り方、サインインの中継、応答のヘッダーと gzip |
| [internal/cli](internal/cli/) | opener と reviewer の CLI（認証、clone、ひな形、check、commit、apply request、作業、木、人への依頼。`--json` と終了コード） |
| [internal/e2e](internal/e2e/) | Forgejo と logiforge（serve、worker、CLI）をつないだ E2E |
| [webui](webui/) | WebUI（React と TypeScript）。ビルドした `dist/` を `embed.go` がバイナリに入れる |
| [cmd/logiforge](cmd/logiforge/) | 1 つのバイナリ。`logiforge serve`（API と WebUI）、`logiforge worker`、それ以外は CLI（`logiforge help`） |
| [internal/ports](internal/ports/) | バックエンドのポート（GitForge、認証）のインターフェース |
| [internal/adapters/pullreq](internal/adapters/pullreq/) | GitForge のポートを、pull request と issue と記録のコメントで実装する。GitHub と Forgejo に共通 |
| [internal/adapters/forgejo](internal/adapters/forgejo/) | Forgejo アダプタ（REST の呼び方、repository の用意、認証） |
| [internal/version](internal/version/) | バイナリの版、コミット、ビルドした日付（`logiforge version`。リリースのビルドが `-ldflags` で入れる） |

- `internal/tree` は、worker、CLI の `check`、バックエンドが同じものを使う（[差分の検査](design/backend/checks.md)、[ファイルの配置](design/files/layout.md)）。

## worker のロック

`logiforge worker` は、clone のディレクトリと置き場の根に、`.worker.lock` のロックを取る。unix では、worker が止まればロックは外れる。unix でない環境では、ロックはファイルで、worker が落ちると残る。そのときは、ほかの worker が動いていないことを確かめてから、2 つのディレクトリの `.worker.lock` を手で消す。
