# テスト

Forgejo アダプタの結合テストと、E2E を手元で動かすときに読む。
単体のテストは、[開発](README.md) の「Go」に書く。

## Forgejo アダプタの結合テスト

- Forgejo アダプタの結合テストは、動く Forgejo を使う。
- 手元では、Forgejo のバイナリを取ってきて、次のように動かす。

```sh
scripts/forgejo-test.sh <forgejo のバイナリ> 3999 > /tmp/forgejo.env
set -a; . /tmp/forgejo.env; set +a
go test -count=1 -run TestForgejo ./internal/adapters/forgejo/
kill "$FORGEJO_PID"
```

- `FORGEJO_URL` と `FORGEJO_ADMIN_TOKEN` がなければ、結合テストは飛ばす。

## E2E

- E2E（[internal/e2e](../../internal/e2e/)）も、結合テストと同じ Forgejo で動かす。
- テストは、次のことを行う。
  - logiforge をビルドする。
  - `serve` と `worker` を起動する。
  - CLI と API で、利用の流れを通す。
- 設定は、テストが、起動の前に設定の DB に書く。起動時に渡すのは、DB の場所と 2 つの鍵と、ホストに結び付くものだけである（[環境変数](../../design/deploy/env.md)）。
  - 初回のセットアップの流れ（ログのコードから最初の利用者まで）は、`TestE2ESetupForgejo` が、セットアップの API で通す（Forgejo の構成だけ）。
- 利用者 alice のトークンは、テストが Forgejo に登録した OAuth2 のアプリケーションで、WebUI のサインインと同じ流れで得る。エージェントは personal access token を使い、`client` になる（[利用者](../../design/settings/users.md)）。

```sh
go test -count=1 -timeout 20m -v ./internal/e2e/
```

## ブラウザの E2E

- ブラウザの E2E は、WebUI のサインインから流れを通す。
- Chrome のパスを `LOGIFORGE_E2E_CHROME` に渡したときだけ動かす。
- 先に WebUI をビルドしておく（[WebUI のビルド](webui.md)）。
- スクリーンショットは、`LOGIFORGE_E2E_OUT` のディレクトリに書く。

## 大きなスナップショットツリーの取り込み

- 大きめのスナップショットツリーの取り込みの時間は、`E2E_LARGE_COMMITS` を付けたときだけ測る。
- `E2E_LARGE_COMMITS` は、main のコミットの数である（例：300）。
