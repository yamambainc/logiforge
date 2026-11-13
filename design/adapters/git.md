# git アダプタ

[木の読み書き](../backend/ports/tree-read-write.md) のポートを、git でどう実装するかを確かめるときに読む。
ファイルの形式は、[木のファイル配置](../files/README.md) に従う。

## リモートと手元の複製

- 正本は、リモートの git リポジトリである。リモートは、GitHub か [独自フォージ](../forge/README.md) である。
- どちらのリモートでも、同じ実装にする。違うのは、リモートの URL と認証情報だけである。
  - なぜ：git の smart HTTP の push と fetch だけを使えば、どちらのリモートにも同じ手順で読み書きできるから。
- アダプタは、repository ごとに、手元に bare の複製を持つ。
  - 読む操作は、手元の複製で行う。
  - 書く操作は、手元の複製でコミットを作り、リモートに push する。
  - 手元の複製は、正本ではない。消えても、リモートから fetch し直せる。
- 手元の複製を置くディレクトリは、サーバの設定に書く（[設定と秘密情報](README.md)）。

## 操作と git の対応

| 操作 | git での実装 |
|---|---|
| 用意する | 手元の複製がなければ作り、`git push --dry-run` で書き込めることを確かめる |
| 今の main を読む | `git fetch` のあと、手元の `refs/heads/main` を読む |
| 最初のリビジョンで main を作る | `git commit-tree`（親なし）のあと、下の「ref を書く」で `refs/heads/main` を、ないことを期待して書く |
| 木を読む | `git ls-tree` と `git cat-file`（リビジョンを指定する） |
| リビジョンの情報を読む | `git cat-file commit <リビジョン>` |
| 変更を列挙する | `git log <a>..<b> --first-parent -- <パス>` |
| 共通の祖先を求める | `git merge-base <a> <b>` |
| リビジョンの並びを読む | `git rev-list --first-parent --reverse main` |
| リビジョンを作る | `git commit-tree` で作る。名前があれば、下の「ref を書く」で、その名前の ref を、ないことを期待して書く |
| 名前でリビジョンを読む | `git fetch` のあと、手元の `refs/logiforge/<名前の ref>` を読む |
| main を進める | 下の「ref を書く」で、`refs/heads/main` を、期待する親を期待して書く |

- 手元の複製にないリビジョンを読むときは、先に `git fetch` する。
- `git fetch` は、`refs/heads/main` と `refs/logiforge/` の下の ref を取る。

## ref を書く

リモートの ref は、期待する今の値を付けて push する。

```
git push --porcelain --force-with-lease=<ref>:<期待する値> <リモート> <コミット>:<ref>
```

- 期待する値を空にすると、ref がまだないときだけ書ける。
- 期待する値は、push の要求に含まれ、リモートが ref を更新するときに確かめる。
  - そのため、確かめてから書くまでの間に、別の push が割り込めない。
- 値を省いた `--force-with-lease` は使わない。
  - なぜ：値を省くと、手元の複製にある古い値を期待する値にするので、fetch の時点によって結果が変わるから。
- 結果は、`--porcelain` の出力を ref ごとに読む。
  - lease で拒まれたときは、[失敗の種類](../backend/ports/tree-read-write.md) の失敗として返す。
    - `refs/heads/main` を、ないことを期待して書いたとき：「`main` がすでにある」
    - 名前の ref を書いたとき：「同じ名前のリビジョンがすでにある」
    - `refs/heads/main` を、期待する親を期待して書いたとき：「`main` が期待する親でない」
  - ほかの理由（認証、ブランチ保護、通信）で拒まれたときは、それと区別できる別の失敗として返す。
    - なぜ：lease の失敗だけが「別の処理に負けた」ことを表し、バックエンドが次の動きを変えるから。

## push の結果がわからないとき

リモートが push を受け付けたあと、応答を受け取る前に通信が切れると、結果がわからない。

- アダプタは、fetch し直して、ref の今の値を読む。
  - ref が書いた値になっていれば、成功とする。
  - それ以外なら、ほかの失敗として返す。
- `refs/heads/main`（最初の main と、main を進めるとき）と、名前の ref の、すべての push に当てはめる。
- なぜ：送り直すと、自分の push に lease で負けたように見え、成功したのに失敗として扱ってしまうから。

## 資格情報

- push と fetch の資格情報は、git の credential helper で渡す。
  - アダプタが用意する credential helper が、サーバの設定の資格情報を git に返す。
  - なぜ：URL やコマンドの引数にトークンを書くと、プロセスの一覧やログに残るから。
- 独自フォージでは、`writer` のトークンを Basic 認証のパスワードとして渡す（[git のホスティング](../forge/git-hosting.md)）。
- GitHub では、GitHub App のインストールのトークンを渡す。
  - このトークンは 1 時間で切れる。アダプタは、切れる前に App の秘密鍵から作り直す。
- GitHub では、App が `main` に push できるように、管理者がルールセットかブランチ保護を設定しておく（[main への書き込みを守る](github.md)）。

## 名前を付けたリビジョン

ポートから受け取った名前を、アダプタが次の ref に組み立てる。

| 名前 | ref |
|---|---|
| 版（apply request の識別子と版の番号） | `refs/logiforge/apply-requests/<識別子>/<版の番号>` |
| 回答（人への依頼の識別子） | `refs/logiforge/human-requests/<識別子>` |

- 版は、版ごとに別の ref に置く。
  - なぜ：古い版のコミットも ref から辿れるようにするため。辿れないコミットは git gc で消え、古い版の差分と、版を書いたセッション（trailer）を読めなくなる。
- 回答の ref は、[artifact の置き方](../files/artifacts.md) の回答のリビジョンを指す。

## 制約

- 読む操作のたびに fetch する。
  - なぜ：owner や admin が直接 push しても、古い木を返さないようにするため（[main への書き込みを守る](github.md)）。
  - 必要になったら、短い時間だけ fetch を省く。
- Windows でクローンするときは、git の `core.longpaths` を有効にする必要がある（[深さの上限](../files/parent-child.md)）。
