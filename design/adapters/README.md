# アダプタ

バックエンドのポートを、何で実装するかを決めるときと、アダプタを作るときに読む。
ポートの定義は、[ポート](../backend/ports/README.md) が正本である。

## 一覧

| ポート | アダプタ | ページ |
|---|---|---|
| 木の読み書き | git（GitHub のリモート）、git（ローカルの bare リポジトリ） | [git アダプタ](git.md) |
| やりとり | GitHub アダプタ、独自プロバイダ | [GitHub アダプタ](github.md)、[独自プロバイダ](native.md) |
| サーバの状態 | メモリのキャッシュ（GitHub を使う構成）、SQLite（使わない構成） | [メモリのキャッシュ](memory.md)、[SQLite アダプタ](sqlite.md) |
| 認証 | GitHub アダプタ（GitHub を使う構成）、SQLite アダプタ（使わない構成） | [GitHub アダプタ](github.md)、[SQLite アダプタ](sqlite.md) |

## 組み合わせ

```mermaid
flowchart TB
  subgraph G["GitHub を使う場合"]
    G1["git アダプタ（GitHub）"] ==> G1D[("git リポジトリ（GitHub）")]
    G2["GitHub アダプタ"] ==> G2D[("GitHub の PR と issue")]
    G3["メモリのキャッシュ"] ==> G3D[("サーバのメモリ")]
  end
  subgraph N["GitHub を使わない場合"]
    N1["git アダプタ（bare）"] ==> N1D[("git リポジトリ（bare）")]
    N2["独自プロバイダ"] ==> N3D[("SQLite ファイル")]
    N3["SQLite アダプタ"] ==> N3D
  end
```

- 太い矢印は、アダプタがデータストアを読み書きすることを表す。
- GitHub を使う場合は、SQLite を使わない。サーバの状態は、メモリのキャッシュに置く。
- GitHub を使わない場合は、独自プロバイダ、サーバの状態、認証が、同じ SQLite ファイルにテーブルを分けて置く。

- やりとりのアダプタ（プロバイダ）は、サーバの設定で 1 つに決める。
  - なぜ：repository ごとにプロバイダを選べると、組み合わせが増えて単純さを失うから。
- GitHub アダプタを使うときは、木の読み書きに git（GitHub のリモート）を使う。
- 独自プロバイダを使うときは、木の読み書きに git（ローカルの bare リポジトリ）を使う。
- 認証は、GitHub アダプタを使うときは GitHub アダプタが、独自プロバイダを使うときは SQLite アダプタが実装する。
- GitHub で PR（pull request）や issue としてやりとりを見たいなら、GitHub アダプタを使う。
- GitHub を使わずに動かしたいなら、独自プロバイダを使う。

構成ごとの、サーバの上の正本とバックアップは、次のとおりである。

| 構成 | サーバの上の正本 | バックアップ |
|---|---|---|
| GitHub を使う構成 | なし（すべてキャッシュ） | 要らない |
| GitHub を使わない構成 | 認証の主体とトークン、独自プロバイダのテーブル | SQLite ファイルを取る |

サーバの状態を失ったとき（GitHub を使う構成では、再起動したとき）の影響は、次のとおりである。

| 失ったもの | 影響 |
|---|---|
| repository の登録 | 起動時に作り直す |
| イベントのログ | 起動時に作り直す。ログの世代が変わるので、クライアントには `cursor_expired` を返す |
| Idempotency-Key | 前に送った要求を送り直すと、二重に処理されることがある |
| 突き合わせの状態 | 次の突き合わせで作り直す |
| 主体とトークン | GitHub を使う構成では影響なし（GitHub が正本）。使わない構成では、誰も API を使えなくなるので、バックアップから戻す |

## repository の登録

repository の名前の正本は、アダプタごとに次のとおりである。

- GitHub アダプタ：サーバの設定（repository の名前と `owner/name` の組）
  - 管理者が GitHub リポジトリを作り、書き込み主体を入れて、この組を登録する（[GitHub アダプタ](github.md)）。
- 独自プロバイダ：bare リポジトリのディレクトリ名
  - 管理者は、bare リポジトリを置くディレクトリだけを設定する。利用者が作るときに、bare リポジトリを作る（[git アダプタ](git.md)）。

サーバの状態の登録は、これらから作り直すキャッシュである（[サーバの状態](../backend/ports/server-state.md)）。
- 利用者は、名前と根の主文だけで repository を作る。

## 設定と秘密情報

次のものは、サーバの設定ファイルか環境変数に置く。git リポジトリには置かない。

- GitHub の書き込み主体の認証情報（GitHub App の秘密鍵など）
- GitHub の webhook の secret
- SQLite ファイルのパス（GitHub を使わない構成だけ）
- `user` とみなす GitHub のアカウント名か、organization の team（GitHub を使う構成だけ。[GitHub アダプタ](github.md)）
- 版の署名の鍵（[記録の形式](../files/revisions.md)）

- なぜ：git リポジトリは、人やエージェントが読めるから。
