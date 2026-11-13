# logiforge

仮説とストーリーの木を、規則付きで管理する forge のリポジトリ。
開発に加わるときと、サーバを動かすときに読む。

- 要件：[docs/](docs/README.md)
- 設計：[design/](design/README.md)
- 実装：`src/`（バックエンドとアダプタ）、`test/`

## 開発

要るもの：

- Node.js 22 以上
- git

```sh
npm ci
npm run typecheck
npm test
```

- テストは、一時ディレクトリに bare リポジトリを作って動かす。
- CI（`.github/workflows/ci.yml`）は、pull request と `main` への push で同じコマンドを動かす。
