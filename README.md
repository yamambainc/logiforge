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

## パッケージ

| パッケージ | 中身 |
|---|---|
| [internal/ids](internal/ids/) | ノードの識別子（`h-`、`s-`、`v-` と 10 文字）を作り、確かめる |
| [internal/tree](internal/tree/) | 木のファイルの読み書き、値の計算、不変条件と差分の検査。git には触れない |

- `internal/tree` は、worker、CLI の `check`、バックエンドが同じものを使う（[差分の検査](design/backend/checks.md)、[ファイルの配置](design/files/layout.md)）。
