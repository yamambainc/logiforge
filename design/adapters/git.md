# git アダプタ

[worker](../backend/worker.md) が、木の git リポジトリをどう読むかを確かめるときに読む。
ファイルの形式は、[木のファイル配置](../files/README.md) に従う。

## 読むのは worker だけ

- サーバの側で git を読むのは、worker だけである。バックエンドは git を読まず、clone も作らない。
- サーバの側が git に書くのは、GitForge の API による repository の用意と、pull request の squash merge だけである（[GitForge のポート](../backend/ports/gitforge.md)）。
  - そのため、このアダプタには、書く操作がない。

## リモートと clone

- 正本は、リモートの git リポジトリである。リモートは、GitHub か [Forgejo アダプタ](forgejo.md) である。
- どちらのリモートでも、同じ実装にする。違うのは、リモートの URL と認証情報だけである。
  - なぜ：git の smart HTTP の fetch だけを使えば、どちらのリモートにも同じ手順で読めるから。
- worker は、repository ごとの clone を、永続するストレージに持ち続ける。

## 読むもの

| もの | git での読み方 |
|---|---|
| `main` とブランチの先頭 | `git ls-remote <リモート> refs/heads/*` |
| コミットと木 | `git fetch` のあと、`git ls-tree` と `git cat-file` |
| ブランチと `main` の分かれた点 | `git merge-base` |
| 変わったファイル | `git diff-tree -r` |

- 解析の前に、blob の大きさとモードを `git ls-tree -l` で確かめる。大きすぎる blob と、普通のファイル（100644）でないものは、中身を読まない（[差分の検査](../backend/checks.md)）。

## 資格情報

- 資格情報は、git の credential helper で渡す。
  - なぜ：URL やコマンドの引数にトークンを書くと、プロセスの一覧やログに残るから。
- worker の資格情報は、読むだけの権限のものにする（[設定と秘密情報](README.md)）。
  - GitHub：contents の read だけを持つ、worker のための GitHub App
  - Forgejo：`read:repository` と `read:organization` だけを持つ、worker のためのユーザーのトークン（[Forgejo アダプタ](forgejo.md)）

## 制約

- Windows でクローンするときは、git の `core.longpaths` を有効にする必要がある（[深さの上限](../files/parent-child.md)）。
