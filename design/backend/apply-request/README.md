# apply request

バックエンドが、apply request を GitForge の pull request としてどう扱い、先頭のコミット・更新・close をどうするかを確かめるときに読む。
送り直しの扱いは、[同じ要求の繰り返し](../../api/writes.md) が正本である。反映は [反映の手順](../merge.md) に書く。

## 読む順

1. このページ：pull request との対応、先頭のコミット、更新、close、ブランチの掃除
2. [作成](create.md)：apply request の作成と、作業用の git の複製
3. [向き先](target.md)：向き先が `main` でない apply request
4. [人への依頼の起票と回答](../human-requests.md)

## apply request と pull request

| apply request | GitForge |
|---|---|
| apply request | 利用者のブランチから `main` への pull request |
| 識別子 | pull request の番号 |
| apply request を作った主体（opener） | pull request の作者 |
| 先頭のコミット | pull request の head のコミット |
| 新しい先頭のコミット | 利用者が、同じブランチに push する |
| レビュー | pull request の review（[数える review](../../adapters/pull-requests.md#数える-review)） |
| 反映の失敗 | 理由を書いた pull request のふつうのコメントと、レビュー待ちか修正待ちかのラベル |
| 反映 | pull request の squash merge（head の SHA を指定する） |
| close | pull request の close |

- サーバの側が git に書くのは、pull request の merge と、repository の用意だけである（[状態を持たない](../README.md#状態を持たない)）。
- 1 つのブランチにつき、open の apply request は 1 つまでである。
- 利用者がブランチを消すと、apply request は close になる。
  - head のブランチがない open の pull request は、バックエンドが closed として扱う。worker の [ブランチの履歴](../snapshot/branch.md#ブランチの履歴) で、消えたことがわかる。
  - なぜ：ブランチを消したときに、GitForge が pull request を閉じるかどうかによらずに、同じ結果にするため。
- 状態（open、merged、closed）は、pull request の状態から決める（[apply request の状態](../../adapters/pull-requests.md#apply-request-の状態)）。
- pull request は、apply request を作る主体（opener）のトークンで作る。
  - なぜ：作った主体を、GitForge が記録する pull request の作者で決めるため。作者は、だれにも書き換えられない。

## 先頭のコミット

- apply request の先頭のコミットは、[worker](../worker/README.md) が見たブランチの先頭のコミットである。SHA で指す。
  - worker は、ブランチごとに、先頭のコミットが変わった順の一覧を、置き場に書く（[ブランチの履歴](../snapshot/branch.md#ブランチの履歴)）。
  - apply request の先頭のコミットの一覧は、その一覧のうち、pull request を作ったときの先頭のコミットから後のものである。
  - 利用者が短い間に何度も push すると、worker が見なかったコミットは、一覧に入らない。
- 一覧の始まりは、ブランチの履歴のうち、worker が pull request を作った時刻（GitForge の `created_at`）の 5 分前より後に見た、最初の項目である。その前にしか見ていなければ、最後の項目である。
  - なぜ 5 分前か：作ったときの先頭を、worker が作る前に見たか後に見たかは、時計のずれでわからないから。前の項目が 1 つ多く入っても、レビューの条件（すべての先頭のコミットへの最後の決めるレビュー）が厳しくなるだけである。
  - 先頭が `main` と同じ（base と同じで、ブランチに自分のコミットがない）項目は、一覧に入れない。なぜ：`main` から作ったばかりのブランチを worker が見ると、`main` のコミットが先頭に見えるから。
  - 一覧は、ブランチが消えた項目で終わる。
- 一覧の各項目には、worker がそのコミットを見た時刻（`seen_at`）を添える。
- GitForge の今の head が、まだブランチの履歴にない（worker が追いついていない）ときは、それを、base のない最後の項目として見せる。その差分は、スナップショットができるまで返さない。
- レビューと反映は、SHA でコミットを指す。
- ブランチの先頭が、前に見た SHA に戻ったら（A → B → A）、今の先頭のコミットは A である。A への前のレビューは、A に対するものとして有効である。
  - なぜ：レビューした差分と、今の差分が同じだからである。
- 先頭のコミットの差分は、そのコミットと `main` の分かれた点（base）からの差分である。ブランチの途中のコミットは見ない。

## 更新

- apply request を更新するには、利用者が同じブランチに push するだけである。API は呼ばない。
  - worker が新しい先頭を見つけると、それが新しい先頭のコミットになる。
  - 新しい先頭のコミットの差分は、レビューの前に、worker が作ったスナップショットで確かめる。確かめに通らないコミットには、`approve` を出さず、merge もしない（[反映の手順](../merge.md)）。
- `updateApplyRequest` は、説明欄を書き換えるだけである。

## close

1. close する主体のトークンで、close の理由を、人が読む文で書いた pull request のコメントを書く（[close のコメント](../../adapters/texts.md#close-のコメント)）。
2. 同じトークンで、pull request を close する。
3. 応答する。

- すでに閉じていれば、今の状態を返す。

## ブランチの掃除

- バックエンドと worker は、利用者のブランチを消さない。
  - なぜ：ブランチは利用者のものだから。ブランチを消すと、open の apply request は close になる。
- 利用者は、反映か close されたあとに、手元のブランチを消す。CLI の `ar cleanup` が手伝う（[コマンドの一覧](../../cli/commands.md)）。
- 他人のブランチを消しても、反映済みの apply request は壊れない。反映の中身は `main` にあるからである。open のものは close になる。
