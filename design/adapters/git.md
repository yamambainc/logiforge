# git アダプタ

[worker](../backend/worker/README.md) とバックエンドが、木の git リポジトリをどう読むかを確かめるときに読む。
ファイルの形式は、[木のファイル配置](../files/README.md) に従う。

## 読む主体

- worker は、見回りで git を読み、スナップショットを作って置き場に書く。
- バックエンドは、作業用の git の複製から git を読む（[作業用の git の複製](../backend/apply-request/create.md#作業用の-git-の複製)）。置き場には書かない。
  - apply request の作成で、ブランチを検査するとき
  - ディレクトリの repository の反映で、worker が `main` に追いついたかを確かめるとき（[反映の順番](../directory/main.md#反映の順番)）
- サーバの側が git に書くのは、次のものだけである。
  - GitForge の API による、repository の用意と、pull request の squash merge（[GitForge のポート](../backend/ports/gitforge.md)）
  - バックエンドの、識別子の番号の ref（下の「番号の ref に書く」）

## リモートと clone

- 正本は、リモートの git リポジトリである。リモートは、GitHub か [Forgejo アダプタ](forgejo.md) である。
- どちらのリモートでも、同じ実装にする。違うのは、リモートの URL と認証情報だけである。
  - なぜ：git の smart HTTP の fetch だけを使えば、どちらのリモートにも同じ手順で読めるから。
- worker は、repository ごとの clone を、永続するストレージに持ち続ける。
- バックエンドは、repository ごとの作業用の複製を、一時的なディスクに持つ。初めて要るときに 1 度だけ clone し、あとは fetch するだけにする。

## 読むもの

| もの | git での読み方 |
|---|---|
| `main` とブランチの先頭 | `git ls-remote <リモート> refs/heads/*` |
| コミットと木 | `git fetch` のあと、`git ls-tree` と `git cat-file` |
| ブランチと `main` の分かれた点 | `git merge-base` |
| 変わったファイル | `git diff-tree -r` |

- git の操作には、1 回ごとに時間の上限を付ける。超えたら、誤りとして返す（[すること](../backend/worker/patrol.md#すること)）。
- 解析の前に、blob の大きさとモードを `git ls-tree -l` で確かめる。大きすぎる blob と、普通のファイル（100644）でないものは、中身を読まない（[差分の検査](../backend/checks.md)）。

## 番号の ref に書く

バックエンドは、識別子の番号の ref を、git の smart HTTP の push で書く（[番号の ref](../backend/ids/numbering.md#番号の-ref)）。

| すること | git での書き方 |
|---|---|
| 番号の ref とキーの ref を読む | `git ls-remote <リモート> <ref>...`（名前を指定し、一覧は読まない） |
| ref の指すコミットを読む | GitForge の API の `GET /repos/{owner}/{repo}/git/commits/{sha}` |
| 番号のコミットを作る | 一時的な空の bare リポジトリで、`git commit-tree`（空の木、親なし） |
| 番号の ref を進める | `git push --force-with-lease=<ref>:<今の SHA> <リモート> <新しい SHA>:<ref>` |
| キーの ref を作る | `git push --force-with-lease=<ref>: <リモート> <SHA>:<ref>`（すでにあれば拒まれる） |

- push は、要求を送った主体のトークンで行う（[GitForge アダプタの共通の書き方](pull-requests.md)）。
  - なぜ：git を読む資格情報は、読むだけの権限だから（下の「資格情報」）。
- GitHub と Forgejo で、同じ実装にする。
  - なぜ：Forgejo の API には、ref を作る操作がないから。git の push なら、どちらにも同じ手順で書ける。

## 資格情報

- 資格情報は、git の credential helper で渡す。
  - なぜ：URL やコマンドの引数にトークンを書くと、プロセスの一覧やログに残るから。
- worker とバックエンドが git を読む資格情報は、同じ、読むだけの権限のものにする。どちらも、設定の DB から読む（[秘密の暗号化](../settings/secrets.md#秘密ごとの鍵)）。
  - credential helper は、GitForge の URL にだけ設定する。ほかの URL に、トークンを渡さないため。
  - GitHub：バックエンドが作り、設定の DB に置いた、Read だけの installation token（[worker のトークン](github-app.md#worker-のトークン)）。worker は、App の秘密鍵を持たない
    - installation token は 1 時間で切れるので、git の操作のたびに、そのときのトークンを helper に渡す。
  - Forgejo：`read:repository`、`read:organization`、`read:issue` だけを持つ、worker のためのユーザーのトークン（[Forgejo アダプタ](forgejo.md)）

## 制約

- Windows でクローンするときは、git の `core.longpaths` を有効にする必要がある（[深さの上限](../files/parent-child.md)）。
