# プロバイダ

どのプロバイダを使うかを決めるときと、プロバイダを実装するときに読む。
プロバイダは、バックエンドが持つ [プロバイダのインターフェース](../backend/provider-interface.md) を実装する。

## 一覧

| プロバイダ | 置き場所 | ページ |
|---|---|---|
| GitHub アダプタ | GitHub の PR、review、check、issue | [GitHub アダプタ](github.md) |
| 独自プロバイダ | 自前の DB と git リポジトリ | [独自プロバイダ](native.md) |

- 木は、どちらのプロバイダでも git リポジトリに置く（[木のファイル配置](../files/README.md)）。

## 選び方

- 使うプロバイダは、サーバの設定で 1 つに決める。
  - なぜ：repository ごとにプロバイダを選べると、組み合わせが増えて単純さを失うから。
- GitHub で PR や issue としてやりとりを見たいなら、GitHub アダプタを使う。
- GitHub を使わずに動かしたいなら、独自プロバイダを使う。

repository について対応しないことは、[木の形](../../docs/tree/README.md) に書く。
