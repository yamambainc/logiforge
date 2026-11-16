# apply request の作成と更新

バックエンドが、apply request の作成・更新・close と、人への依頼の起票・回答で、何をどうするかを確かめるときに読む。
API の決まり（`Idempotency-Key` と識別子の作り方）は、[同じ要求の繰り返し](../api/writes.md) が正本である。反映は [反映の手順](merge.md) に書く。

## apply request と pull request

| apply request | GitForge |
|---|---|
| apply request | 利用者のブランチから `main` への pull request |
| 先頭のコミット | pull request の head のコミット |
| 新しい先頭のコミット | 利用者が、同じブランチに push する |
| レビュー | pull request のレビュー |
| 反映 | pull request の squash merge（head の SHA を指定する） |
| close | pull request の close |

- サーバの側が git に書くのは、pull request の merge と、repository の用意だけである。
  - なぜ：ブランチの先頭のコミットや `main` の更新を、GitForge の pull request の仕組みにそのまま任せ、バックエンドが git の ref を自前で扱わずに済むようにするため。
- 1 つのブランチにつき、open の apply request は 1 つまでである。
- 利用者がブランチを消すと、apply request は close になる。
  - head のブランチがない open の pull request は、バックエンドが closed として扱う。worker の [ブランチの履歴](snapshot.md) で、消えたことがわかる。
  - なぜ：ブランチを消したときに、GitForge が pull request を閉じるかどうかによらずに、同じ結果にするため。
- 状態（open、merged、closed）は、pull request の状態と、書き込み主体の記録から決める（[提案の状態](../adapters/pull-requests.md)）。
- apply request の識別子、作ったときの先頭、close の記録は、pull request の本文ではなく、書き込み主体のコメントに置く（[記録のコメント](../adapters/pull-requests.md)）。
  - なぜ：pull request の本文は、repository に書く権限を持つ人が書き換えられるから。

## 先頭のコミット

- apply request の先頭のコミットは、[worker](worker.md) が見たブランチの先頭のコミットである。SHA で指す。
  - worker は、ブランチごとに、先頭のコミットが変わった順の一覧を、置き場に書く（[ブランチの履歴](snapshot.md)）。
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
- コミットを書いたセッションは、先頭のコミットのメッセージの trailer `Logiforge-Session-Id:` の自己申告である（[セッション](../files/revisions.md#セッション)）。

## 作成

1. 識別子を求める（[同じ要求の繰り返し](../api/writes.md)）。
2. GitForge で、その識別子の apply request を読む。あれば、それを返して終わる。中身は比べない。
3. 作業用の git の複製に、`main` とブランチを fetch する（下の「作業用の git の複製」）。
4. ブランチの先頭と `main` の分かれた点（base）を求め、base から変わったファイルだけを読む。
   - 要求に `commit` があり、ブランチの先頭と違えば、`validation_failed` で受け付けない。
   - base の木は、置き場にスナップショットツリーがあれば、それを使う。なければ（worker が追いついていない）、作業用の複製から読む。
5. 差分に、[差分の検査](checks.md) を行う。4 種類の操作として読めない変化があれば、`validation_failed` で受け付けない。
   - 解析のコードは、worker と CLI の `check` と同じものである。
6. GitForge に、pull request を作り、識別子と、作ったときの先頭のコミットを、記録のコメントに書く。
   - そのブランチに open の pull request がすでにあれば、`validation_failed` で受け付けない。
7. 応答する。
   - 先頭のコミットの一覧とレビューの材料は、worker のスナップショットから読む。worker がブランチを取り込むまで、レビューは `snapshot_pending` になる（[レビュー](../api/writes.md)）。
   - 応答の操作の一覧は、worker がまだブランチを取り込んでいなければ、手順 5 の検査の結果から返す。
   - 注記：作成の時点の base と、worker があとでブランチを取り込むときの base（そのときの `main` との分かれた点）は、あいだに `main` が進むと違いうる。先頭のコミットの一覧とレビューの材料は、worker が求めた base で作る。反映の前に、今の `main` に対して確かめ直す（[反映の手順](merge.md)）ので、正しさには関わらない。

- 作成は、worker を待たない。なぜ：検査をバックエンドで行えば、バックエンドを増やすだけで、作成の数に合わせて広げられるから。
- 作成の時点の検査は、今の `latest` のスナップショットツリーを、検査の文脈（引用元など）に使う。worker が `main` に追いついていなければ、文脈は少し古い。反映の前の検査（[反映の手順](merge.md)）で、今の `main` に対して確かめ直す。

## 作業用の git の複製

- バックエンドは、repository ごとに、作業用の git の複製（bare）を持つ。
  - 置き場所は、一時的なディスク（コンテナの `/tmp` や `emptyDir`）である。
  - 初めて要るときに、1 度だけ clone する。そのあとの要求では、`main` とそのブランチを fetch するだけにする。要求ごとに clone しない。
  - blob を省いた clone（`--filter=blob:none`）にはしない。なぜ：置き場に base のスナップショットツリーがないとき、base のすべてのファイルを読む。blob を省くと、そのとき blob ごとに GitForge へ取りに行き、遅くなるから。
- 複製は、正本ではない。使い捨てのキャッシュである。消えても、次の要求で作り直す。
  - プロセスごとに持ち、プロセスの間で共有しない。プロセスが止まるときに消す。
- 同じプロセスの中で、同じ repository への要求が重なったら、repository ごとのロックで、1 つずつ通す。
  - なぜ：fetch と読むことが、互いに混ざらないようにするため。違う repository への要求は、並行してよい。
- git は、worker と同じ、読むだけのユーザーの資格情報で読む（[git アダプタ](../adapters/git.md)）。書く鍵は使わない。

- 人への依頼の起票は、手順 2 のあと、GitForge に記録して応答する。git には書かない。
  - 対象は、今の `main` のスナップショットツリーの、active な仮説で、葉か要再検討であるものに限る（[人への依頼](../../docs/human-requests/README.md)）。違えば `validation_failed` にする。

## 更新

- apply request を更新するには、利用者が同じブランチに push するだけである。API は呼ばない。
  - worker が新しい先頭を見つけると、それが新しい先頭のコミットになる。
  - 新しい先頭のコミットの差分は、レビューの前に、worker が作ったスナップショットで検査する。検査に通らないコミットは、`approve` しても反映しない（[反映の手順](merge.md)）。
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
