# リリースの作り方

logiforge の版を出すときと、リリースの本文を書くときに読む。
取り方は、[インストール](../install.md) に書く。

## タグを push する

- タグ（`v0.1.0` など）を push すると、CI（[.github/workflows/release.yml](../../.github/workflows/release.yml)）がリリースを作る。
- CI は、[GoReleaser](https://goreleaser.com/)（[.goreleaser.yaml](../../.goreleaser.yaml)）でバイナリを作り、Releases に置く。
- チェックサム（`checksums.txt`、SHA-256）も置く。

## ビルドの中身

- WebUI を先にビルドし、バイナリに入れる。
  - `webui/` で `npm ci` と `npm run build` を行う（[WebUI のビルド](webui.md)）。
- 版とコミットと日付は、`-ldflags` で入れる（[internal/version](../../internal/version/)）。

## リリースの本文

- リリースの本文の先頭に書くことは、`release-notes/<タグ>.md` に書く（例：[release-notes/v0.2.0.md](../../release-notes/v0.2.0.md)）。
  - 書くことは、互換のない変更と、移り方である。
- タグを push すると、CI がそのファイルを本文の先頭に置く。
  - ファイルがなければ、本文はコミットの一覧だけになる。
- 互換のない変更を main に入れるときは、次の版のファイルに書き足す。
- リンクは、タグを固定した絶対の URL（`https://github.com/yamambainc/logiforge/blob/<タグ>/...`）で書く。
  - なぜ：本文は Releases のページに置かれるので、相対のリンクは切れるから。

## 手元で作る

- 手元で作るときは、どこにも置かずに `dist/` に作る。

```sh
goreleaser release --snapshot --clean
```

## pull request ごとの CI

- pull request ごとの CI でも、linux/amd64 の版だけを作る（[go.yml](../../.github/workflows/go.yml) の `release-snapshot`）。
- そのバイナリに、WebUI と版とコミットが入ることを検査する。

## 権限

- リリースのジョブは、repository に書く権限を持つ。
- そのため、次のように版を固定する。
  - 外の action は、コミットの SHA で指す。
  - GoReleaser は、版を決めて使う。
