# 開発

logiforge のコードを変えるときに読む。
仕様は [docs/](../README.md)、設計は [design/](../../design/README.md) が正本である。

## 読む順

1. [パッケージ](packages.md)
2. [テスト](testing.md)
3. [WebUI のビルド](webui.md)
4. [compose で動かす](compose.md)
5. [リリースの作り方](release.md)

## Go

- Go で書く。
- Go のバージョンは [go.mod](../../go.mod) に書く。

```sh
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go test -race ./...
```

CI は、pull request ごとに同じことを行う（[.github/workflows/go.yml](../../.github/workflows/go.yml)）。
