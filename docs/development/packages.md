# パッケージ

コードを変えるときに、どのパッケージに何があるかを引くために使う。
レイヤーの分け方の理由は、[レイヤー構成](../../design/architecture.md) に書く。

## 一覧

| パッケージ | 中身 |
|---|---|
| [cmd/logiforge](../../cmd/logiforge/) | 1 つのバイナリ。`logiforge serve`（API と WebUI）、`logiforge worker`、それ以外は CLI（`logiforge help`） |
| [internal/ids](../../internal/ids/) | ノードの識別子（`h-`、`s-`、`v-` と 10 文字）を作り、検査する |
| [internal/tree](../../internal/tree/) | 木のファイルの読み書き、値の計算、不変条件と差分の検査。git には触れない |
| [internal/snapshot](../../internal/snapshot/) | スナップショットの形（Go の型が正本）、作り方、tree.Files への戻し方、ブランチの差分の重ね方（Overlay）。git には触れない |
| [internal/gitx](../../internal/gitx/) | git の CLI で、木の git リポジトリを読む。書く操作はない |
| [internal/store](../../internal/store/) | スナップショットの置き場（キーの決まり、スナップショットの読み書き、手元のディレクトリ） |
| [internal/worker](../../internal/worker/) | worker（見回り、スナップショットと latest とブランチの履歴、反映の重なりの警告、ハートビート） |
| [internal/workcopy](../../internal/workcopy/) | ブランチの差分のスナップショットの作り方（worker とバックエンドが同じものを使う）と、バックエンドの作業用の git の複製 |
| [internal/backend](../../internal/backend/) | バックエンド（認可、読む API と書く API、反映の手順、着手できる作業、作業の終わりの判定、書き出し） |
| [internal/api](../../internal/api/) | HTTP の API（/v1）。エラーは application/problem+json。WebUI の配り方、サインインの中継、応答のヘッダーと gzip |
| [internal/cli](../../internal/cli/) | opener と reviewer の CLI（認証、clone、ひな形、check、commit、apply request、作業、木、人への依頼。`--json` と終了コード） |
| [internal/ports](../../internal/ports/) | バックエンドのポート（GitForge、認証）のインターフェース |
| [internal/adapters/snapstore](../../internal/adapters/snapstore/) | スナップショットの置き場のポートのアダプタ（置き場を読む） |
| [internal/adapters/pullreq](../../internal/adapters/pullreq/) | GitForge のポートを、pull request、review、issue、ラベル、コメントで実装する。GitHub と Forgejo に共通 |
| [internal/adapters/forgejo](../../internal/adapters/forgejo/) | Forgejo アダプタ（REST の呼び方、repository の用意、認証） |
| [internal/adapters/github](../../internal/adapters/github/) | GitHub アダプタ（REST の呼び方、repository の用意、認証、読むだけの GitHub App のトークン） |
| [internal/e2e](../../internal/e2e/) | Forgejo と logiforge（serve、worker、CLI）をつないだ E2E |
| [internal/bench](../../internal/bench/) | スナップショットツリーを作る時間、読む時間、大きさの測定。テストだけで、製品のコードではない |
| [internal/version](../../internal/version/) | バイナリの版、コミット、ビルドした日付（`logiforge version`。リリースのビルドが `-ldflags` で入れる） |
| [webui](../../webui/) | WebUI（React と TypeScript）。ビルドした `dist/` を `embed.go` がバイナリに入れる |

## 共通の木のパッケージ

- `internal/tree` は、次のものが同じものを使う。
  - worker
  - CLI の `check`
  - バックエンド
- 検査の決まりは [差分の検査](../../design/backend/checks.md)、ファイルの配置は [ファイルの配置](../../design/files/layout.md) に書く。
