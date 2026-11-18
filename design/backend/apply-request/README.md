# apply request

バックエンドが、apply request を GitForge の pull request としてどう扱い、先頭のコミット・更新・close をどうするかを確かめるときに読む。
API の決まり（`Idempotency-Key` と識別子の作り方）は、[同じ要求の繰り返し](../../api/writes.md) が正本である。反映は [反映の手順](../merge.md) に書く。

## 読む順

1. このページ：pull request との対応、先頭のコミット、更新、close、ブランチの掃除
2. [作成](create.md)：apply request の作成と、作業用の git の複製
3. [向き先](target.md)：向き先が `main` でない apply request
4. [人への依頼の起票と回答](../human-requests.md)

## apply request と pull request

| apply request | GitForge |
|---|---|
| apply request | 利用者のブランチから `main` への pull request |
| 先頭のコミット | pull request の head のコミット |
| 新しい先頭のコミット | 利用者が、同じブランチに push する |
| レビュー | pull request のレビュー |
| 反映 | pull request の squash merge（head の SHA を指定する） |
| close | pull request の close |

- サーバの側が git に書くのは、pull request の merge と、repository の用意だけである（[状態を持たない](../README.md#状態を持たない)）。
- 1 つのブランチにつき、open の apply request は 1 つまでである。
- 利用者がブランチを消すと、apply request は close になる。
  - head のブランチがない open の pull request は、バックエンドが closed として扱う。worker の [ブランチの履歴](../snapshot/branch.md#ブランチの履歴) で、消えたことがわかる。
  - なぜ：ブランチを消したときに、GitForge が pull request を閉じるかどうかによらずに、同じ結果にするため。
- 状態（open、merged、closed）は、pull request の状態と、署名の合う記録から決める（[提案の状態](../../adapters/pull-requests.md)）。
- apply request の識別子、作ったときの先頭、close の記録は、pull request の本文ではなく、記録のコメントに置く（[記録のコメント](../../adapters/records.md#記録のコメント)）。
- pull request は、apply request を作る主体（opener）のトークンで作る。
  - なぜ：pull request の本文は、repository に書く権限を持つ人が書き換えられるから。

## 先頭のコミット

- apply request の先頭のコミットは、[worker](../worker/README.md) が見たブランチの先頭のコミットである。SHA で指す。
  - worker は、ブランチごとに、先頭のコミットが変わった順の一覧を、置き場に書く（[ブランチの履歴](../snapshot/branch.md#ブランチの履歴)）。
  - apply request の先頭のコミットの一覧は、その一覧のうち、apply request を作ったときの先頭のコミットから後のものである。
  - 利用者が短い間に何度も push すると、worker が見なかったコミットは、一覧に入らない。
- 一覧の始まりは、ブランチの履歴のうち、apply request を作ったときの先頭のコミットの項目である。
  - 同じ SHA が何度も出るときは、apply request を作った時刻までに見たもののうち、最後のものにする。
  - 一覧は、ブランチが消えた項目で終わる。
- 一覧の各項目には、worker がそのコミットを見た時刻（`seen_at`）を添える。
- GitForge の今の head が、まだブランチの履歴にない（worker が追いついていない）ときは、それを、base のない最後の項目として見せる。その差分は、スナップショットができるまで返さない。
- レビューと反映は、SHA でコミットを指す。
- ブランチの先頭が、前に見た SHA に戻ったら（A → B → A）、今の先頭のコミットは A である。A への前のレビューは、A に対するものとして有効である。
  - なぜ：レビューした差分と、今の差分が同じだからである。
- 先頭のコミットの差分は、そのコミットと `main` の分かれた点（base）からの差分である。ブランチの途中のコミットは見ない。
- コミットを書いたセッションは、先頭のコミットのメッセージの trailer `Logiforge-Session-Id:` の自己申告である（[セッション](../../files/revisions.md#セッション)）。

## 更新

- apply request を更新するには、利用者が同じブランチに push するだけである。API は呼ばない。
  - worker が新しい先頭を見つけると、それが新しい先頭のコミットになる。
  - 新しい先頭のコミットの差分は、レビューの前に、worker が作ったスナップショットで検査する。検査に通らないコミットは、`approve` しても反映しない（[反映の手順](../merge.md)）。
- `updateApplyRequest` は、説明欄を書き換えるだけである。

## close

1. GitForge で、pull request を close する。
2. GitForge に、close の理由とセッションを記録する。
3. 応答する。

- すでに閉じていれば、今の状態を返す。

## ブランチの掃除

- バックエンドと worker は、利用者のブランチを消さない。
  - なぜ：ブランチは利用者のものだから。ブランチを消すと、open の apply request は close になる。
- 利用者は、反映か close されたあとに、手元のブランチを消す。CLI の `ar cleanup` が手伝う（[コマンドの一覧](../../cli/commands.md)）。
- 他人のブランチを消しても、反映済みの apply request は壊れない。反映の中身は `main` にあるからである。open のものは close になる。
