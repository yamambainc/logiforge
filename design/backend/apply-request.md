# apply request の作成と更新

バックエンドが、apply request の作成・更新・close と、人への依頼の起票・回答で、何をどうするかを確かめるときに読む。
API の決まり（`Idempotency-Key` と識別子の作り方）は、[同じ要求の繰り返し](../api/writes.md) が正本である。反映は [反映の手順](merge.md) に書く。

## apply request と pull request

| apply request | GitForge |
|---|---|
| apply request | 利用者のブランチから `main` への pull request |
| 版 | pull request の head のコミット |
| 版の更新 | 利用者が、同じブランチに push する |
| レビュー | pull request のレビュー |
| 反映 | pull request の squash merge（head の SHA を指定する） |
| close | pull request の close |

- サーバの側が git に書くのは、pull request の merge と、repository の用意だけである。
  - なぜ：版や `main` の更新を、GitForge の pull request の仕組みにそのまま任せ、バックエンドが git の ref を自前で扱わずに済むようにするため。
- 1 つのブランチにつき、open の apply request は 1 つまでである。
- 利用者がブランチを消すと、apply request は close になる。
  - head のブランチがない open の pull request は、バックエンドが closed として扱う。worker の [ブランチの履歴](snapshot.md) で、消えたことがわかる。
  - なぜ：ブランチを消したときに、GitForge が pull request を閉じるかどうかによらずに、同じ結果にするため。
- 状態（open、merged、closed）は、pull request の状態と、書き込み主体の記録から決める（[提案の状態](../adapters/pull-requests.md)）。
- apply request の識別子、作ったときの先頭、close の記録は、pull request の本文ではなく、書き込み主体のコメントに置く（[記録のコメント](../adapters/pull-requests.md)）。
  - なぜ：pull request の本文は、repository に書く権限を持つ人が書き換えられるから。

## 版

- 版の正体は、コミットの SHA である。
  - 版は、[worker](worker.md) が見たブランチの先頭のコミットである。worker は、ブランチごとに、先頭のコミットが変わった順の一覧を、置き場に書く（[ブランチの履歴](snapshot.md)）。
  - apply request の版は、その一覧のうち、apply request を作ったときの先頭のコミットから後のものである。
  - 利用者が短い間に何度も push すると、worker が見なかったコミットは、版にならない。
- 版の番号は、表示のためだけのものである。その中で、SHA を初めて見た順に、1 から付ける。
  - 一覧の始まりは、ブランチの履歴のうち、apply request を作ったときの先頭のコミットの項目である。同じ SHA が何度も出るときは、apply request を作った時刻までに見たもののうち、最後のものにする。ブランチが消えた項目で終わる。
  - GitForge の今の head が、まだブランチの履歴にない（worker が追いついていない）ときは、それを、base のない最後の版として見せる。その版の差分は、スナップショットができるまで返さない。
  - レビューと反映は、SHA で版を決める。API は、番号でも SHA でも受け付け、番号は SHA に引き直す。
  - ブランチの先頭が、前に見た SHA に戻ったら（A → B → A）、今の版は A である。A への前のレビューは、A に対するものとして有効である。
    - なぜ：レビューした差分と、今の差分が同じだからである。
- 版の差分は、版のコミットと `main` の分かれた点（base）からの差分である。ブランチの途中のコミットは見ない。
- 版を書いたセッションは、版のコミットのメッセージの trailer `Logiforge-Session-Id:` の自己申告である（[セッション](../files/revisions.md)）。

## 作成

1. 識別子を求める（[同じ要求の繰り返し](../api/writes.md)）。
2. GitForge で、その識別子の apply request を読む。あれば、それを返して終わる。中身は比べない。
3. worker に、ブランチを読んでもらい、先頭のコミットと base と、差分のスナップショットを得る（[worker の内部 API](worker-api.md)）。
   - 要求に `revision` があり、ブランチの先頭と違えば、`validation_failed` で受け付けない。
4. スナップショットの差分に、[差分の検査](checks.md) を行う。4 種類の操作として読めない変化があれば、`validation_failed` で受け付けない。
5. GitForge に、pull request を作り、識別子と、作ったときの先頭のコミットを、記録のコメントに書く。
   - そのブランチに open の pull request がすでにあれば、`validation_failed` で受け付けない。
6. 応答する。

- 人への依頼の起票は、手順 2 のあと、GitForge に記録して応答する。git には書かない。
  - 対象は、今の `main` の木の、active な仮説で、葉か要再検討であるものに限る（[人への依頼](../../docs/human-requests/README.md)）。違えば `validation_failed` にする。

## 更新

- 版の更新は、利用者が同じブランチに push するだけである。API は呼ばない。
  - worker が新しい先頭を見つけると、新しい版になる。
  - 新しい版の差分は、レビューの前に、worker が作ったスナップショットで検査する。検査に通らない版は、`approve` しても反映しない（[反映の手順](merge.md)）。
- `updateApplyRequest` は、説明欄を書き換えるだけである。

## close

1. GitForge で、pull request を close する。
2. GitForge に、close の理由とセッションを記録する。
3. 応答する。

- すでに閉じていれば、今の状態を返す。

## 回答

1. 回答の本文と添付を、GitForge に記録する（[人への依頼の回答](../files/artifacts.md)）。
   - すでに回答があり、本文と回答した利用者が同じなら、送り直しとみなして成功とする。違えば `not_open` で受け付けない。
2. 応答する。

- 回答は、git に書かない。

## ブランチの掃除

- バックエンドと worker は、利用者のブランチを消さない。
  - なぜ：ブランチは利用者のものだから。ブランチを消すと、open の apply request は close になる。
- 利用者は、反映か close されたあとに、手元のブランチを消す。CLI の `ar cleanup` が手伝う（[コマンドの一覧](../cli/commands.md)）。
- 他人のブランチを消しても、反映済みの apply request は壊れない。反映の中身は `main` にあるからである。open のものは close になる。
