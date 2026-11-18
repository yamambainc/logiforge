# WebUI のビルド

WebUI のコードを変えるときと、WebUI 入りのバイナリを作るときに読む。
WebUI の設計は [design/webui](../../design/webui/README.md) に書く。

## 作り

- WebUI は [webui/](../../webui/) にある。
- React と TypeScript を、Vite でビルドする。
- 出力（`webui/dist/`）を、`logiforge serve` が embed して配る。

```sh
cd webui
npm ci
npm test            # 単体のテスト
npm run build       # 型の検査、ビルド、インラインの script と style がないことの検査
npm run dev         # 開発。/v1 は手元の serve（127.0.0.1:8080）に渡す
```

## バイナリに入れる

- `webui/dist/` をビルドしてから `go build` すると、バイナリに WebUI が入る。
- ビルドしていなければ、`serve` は WebUI のかわりに案内を返す。

## サインイン

- サインインには、`serve` に次の環境変数を渡す（[サインイン](../../design/webui/auth.md)）。
  - `LOGIFORGE_OAUTH_CLIENT_ID`
  - `LOGIFORGE_OAUTH_CLIENT_SECRET`
  - `LOGIFORGE_OAUTH_REDIRECT_URI`
