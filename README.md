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

## パッケージ

| パッケージ | 中身 |
|---|---|
| [internal/ids](internal/ids/) | ノードの識別子（`h-`、`s-`、`v-` と 10 文字）を作り、確かめる |
| [internal/tree](internal/tree/) | 木のファイルの読み書き、値の計算、不変条件と差分の検査。git には触れない |
| [internal/snapshot](internal/snapshot/) | スナップショットの形（Go の型が正本）、作り方、tree.Files への戻し方、ブランチの差分の重ね方（Overlay）。git には触れない |
| [internal/gitx](internal/gitx/) | git の CLI で、木の git リポジトリを読む。書く操作はない |
| [internal/store](internal/store/) | スナップショットの置き場（キーの決まり、スナップショットの読み書き、手元のディレクトリ） |
| [internal/worker](internal/worker/) | worker（巡回、スナップショットと latest とブランチの履歴、反映の重なりの警告、内部 API） |
| [cmd/logiforge](cmd/logiforge/) | 1 つのバイナリ。今は `logiforge worker` だけ |
| [internal/ports](internal/ports/) | バックエンドのポート（GitForge、認証）のインターフェース |
| [internal/adapters/pullreq](internal/adapters/pullreq/) | GitForge のポートを、pull request と issue と記録のコメントで実装する。GitHub と Forgejo に共通 |
| [internal/adapters/forgejo](internal/adapters/forgejo/) | Forgejo アダプタ（REST の呼び方、repository の用意、認証） |

- `internal/tree` は、worker、CLI の `check`、バックエンドが同じものを使う（[差分の検査](design/backend/checks.md)、[ファイルの配置](design/files/layout.md)）。
