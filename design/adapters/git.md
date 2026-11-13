# git アダプタ

[木の書き込み](../backend/ports/tree-write.md) のポートを、git でどう実装するかを確かめるときに読む。
ファイルの形式は、[木のファイル配置](../files/README.md) に従う。

## 2 つの部分

| 部分 | 動く場所 | すること |
|---|---|---|
| 木の書き込みのアダプタ | バックエンド | [worker の内部 API](../backend/worker-api.md) のクライアント。git には触れない |
| worker の git | [worker](../backend/worker.md) | clone を持ち、fetch、commit、push をする |

- git に触れるのは、worker だけである。このページの残りは、worker の git について書く。

## リモートと clone

- 正本は、リモートの git リポジトリである。リモートは、GitHub か [独自フォージ](../forge/README.md) である。
- どちらのリモートでも、同じ実装にする。違うのは、リモートの URL と認証情報だけである。
  - なぜ：git の smart HTTP の push と fetch だけを使えば、どちらのリモートにも同じ手順で読み書きできるから。
- worker は、repository ごとの clone を、永続するストレージに持ち続ける。

## 操作と git の対応

| 操作 | git での実装 |
|---|---|
| 用意する | `git push --dry-run` で書き込めることを確かめる |
| リビジョンを作る | 親の tree に、渡されたファイルを `git hash-object` と `git update-index` で足し、`git write-tree` と `git commit-tree` で作る。blob の SHA を渡されたファイルは、その blob をそのまま使う |
| ref を書く | 下の「ref を書く」 |
| 追いつく | `git ls-remote` で確かめ、変わっていれば `git fetch` |

## ref を書く

リモートの ref は、期待する今の値を付けて push する。いくつかの ref を書くときは、`--atomic` で 1 回の push にする。

```
git push --atomic --porcelain \
  --force-with-lease=<ref 1>:<期待する値 1> --force-with-lease=<ref 2>:<期待する値 2> \
  <リモート> <コミット 1>:<ref 1> :<ref 2>
```

- `--atomic` では、どれか 1 つでも拒まれると、どの ref も書かれない。
  - なぜ：版の作成と `head` の更新、反映と `head` の削除を、片方だけが通ることのないようにするため（[apply request の作成と更新](../backend/apply-request.md)、[反映の手順](../backend/merge.md)）。
  - GitHub と、独自フォージの git のホスティングが `--atomic` を受け付けるかは、アダプタを作るときに確かめる（[GitHub アダプタ](github.md)、[git のホスティング](../forge/git-hosting.md)）。
- ref を消すときは、`:<ref>` と書く。期待する値は、今の値にする。
- 期待する値を空にすると、ref がまだないときだけ書ける。
- 期待する値は、push の要求に含まれ、リモートが ref を更新するときに確かめる。
  - そのため、確かめてから書くまでの間に、別の push が割り込めない。
- 値を省いた `--force-with-lease` は使わない。
  - なぜ：値を省くと、clone にある古い値を期待する値にするので、fetch の時点によって結果が変わるから。
- 結果は、`--porcelain` の出力を ref ごとに読む。
  - lease で拒まれたときは、どの ref が拒まれたかを、[失敗の種類](../backend/ports/tree-write.md) の失敗として返す。
  - ほかの理由（認証、ブランチ保護、通信）で拒まれたときは、それと区別できる別の失敗として返す。
    - なぜ：lease の失敗だけが「別の処理に負けた」ことを表し、バックエンドが次の動きを変えるから。

## push の結果がわからないとき

リモートが push を受け付けたあと、応答を受け取る前に通信が切れると、結果がわからない。

- worker は、`git ls-remote` で、ref の今の値を読む。
  - ref が書いた値になっていれば、成功とする。
  - それ以外なら、ほかの失敗として返す。
- どの ref の push にも当てはめる。`--atomic` の push では、書いたすべての ref が書いた値になっているときだけ、成功とする。
- なぜ：送り直すと、自分の push に lease で負けたように見え、成功したのに失敗として扱ってしまうから。

## 資格情報

- push と fetch の資格情報は、git の credential helper で渡す。
  - worker が用意する credential helper が、設定の資格情報を git に返す。
  - なぜ：URL やコマンドの引数にトークンを書くと、プロセスの一覧やログに残るから。
- 独自フォージでは、`writer` のトークンを Basic 認証のパスワードとして渡す（[git のホスティング](../forge/git-hosting.md)）。
- GitHub では、GitHub App のインストールのトークンを渡す。
  - このトークンは 1 時間で切れる。worker は、切れる前に App の秘密鍵から作り直す。
- GitHub では、App が `main` に push できるように、管理者がルールセットかブランチ保護を設定しておく（[main への書き込みを守る](github-main.md)）。

## 名前を付けたリビジョン

ポートから受け取った名前を、worker が次の ref に組み立てる。

| 名前 | ref |
|---|---|
| 版（apply request の識別子と版の番号） | `refs/logiforge/apply-requests/<識別子>/<版の番号>` |
| 今の版（apply request の識別子と `head`） | `refs/logiforge/apply-requests/<識別子>/head` |
| 回答（人への依頼の識別子） | `refs/logiforge/human-requests/<識別子>` |

- 版は、版ごとに別の ref に置く。
  - なぜ：古い版のコミットも ref から辿れるようにするため。辿れないコミットは git gc で消え、古い版の差分と、版を書いたセッション（trailer）を読めなくなる。
- 回答の ref は、[artifact の置き方](../files/artifacts.md) の回答のリビジョンを指す。

## 制約

- Windows でクローンするときは、git の `core.longpaths` を有効にする必要がある（[深さの上限](../files/parent-child.md)）。
