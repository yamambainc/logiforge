# アダプタ

バックエンドのポートを、何で実装するかを決めるときと、アダプタを作るときに読む。
ポートの定義は、[ポート](../backend/ports/README.md) が正本である。

## 一覧

| ポート | アダプタ | ページ |
|---|---|---|
| 木のリポジトリ | git（GitHub のリモート）、git（ローカルの bare リポジトリ） | [git アダプタ](git.md) |
| やりとり | GitHub アダプタ、独自プロバイダ | [GitHub アダプタ](github.md)、[独自プロバイダ](native.md) |
| 状態ストア | SQLite | [SQLite アダプタ](sqlite.md) |
| CAS | ローカルのディレクトリ、S3 互換のストレージ | [CAS アダプタ](cas.md) |

## 組み合わせ

- やりとりのアダプタ（プロバイダ）は、サーバの設定で 1 つに決める。
  - なぜ：repository ごとにプロバイダを選べると、組み合わせが増えて単純さを失うから。
- GitHub アダプタを使うときは、木のリポジトリに git（GitHub のリモート）を使う。
- 独自プロバイダを使うときは、木のリポジトリに git（ローカルの bare リポジトリ）を使う。
- GitHub で PR や issue としてやりとりを見たいなら、GitHub アダプタを使う。
- GitHub を使わずに動かしたいなら、独自プロバイダを使う。

## repository の登録

- 管理者が、repository ごとに、名前と provider_ref をサーバの設定に登録する。
  - GitHub アダプタ：管理者が GitHub リポジトリを作って App を入れ、`owner/name` を provider_ref として登録する（[GitHub アダプタ](github.md)）。
  - 独自プロバイダ：bare リポジトリを置くディレクトリを設定する。名前を登録しなくても、利用者が作るときに bare リポジトリを作る（[git アダプタ](git.md)）。
- 利用者は、名前と根の主文だけで repository を作る。

## 設定と秘密情報

次のものは、サーバの設定ファイルか環境変数に置く。git リポジトリには置かない。

- GitHub App の秘密鍵
- GitHub の webhook の secret
- SQLite ファイルのパス（状態ストアと独自プロバイダで同じファイル）
- CAS の場所と認証情報
- 版の署名の鍵（[記録の形式](../files/revisions.md)）

- なぜ：git リポジトリは、人やエージェントが読めるから。
