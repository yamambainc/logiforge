# apply request の作成と更新

バックエンドが、apply request の作成・更新・close と、人への依頼の起票・回答で、何をどうするかを確かめるときに読む。
API の決まり（`Idempotency-Key` と識別子の作り方）は、[同じ要求の繰り返し](../api/writes.md) が正本である。反映は [反映の手順](merge.md) に書く。

## apply request と pull request

| apply request | プロバイダ（GitHub か独自フォージ） |
|---|---|
| apply request | 利用者のブランチから `main` への pull request |
| 版 | pull request の head のコミット |
| 版の更新 | 利用者が、同じブランチに push する |
| レビュー | pull request のレビュー |
| 反映 | pull request の squash merge（head の SHA を指定する） |
| close | pull request の close |

- サーバの側が git に書くのは、pull request の merge と、repository の用意だけである。
  - なぜ：版や `main` の更新を、プロバイダの pull request の仕組みにそのまま任せ、バックエンドが git の ref を自前で扱わずに済むようにするため。
- 1 つのブランチにつき、open の apply request は 1 つまでである。
- 利用者がブランチを消すと、pull request は閉じる。バックエンドは、それを close として扱う。
  - GitHub では、これは GitHub の決まりである。独自フォージも、同じように振る舞う（[独自フォージ](../forge/README.md)）。
- 状態（open、merged、closed）は、pull request の状態をそのまま使う。

## 版

- 版は、[worker](worker.md) が見たブランチの先頭のコミットである。
  - worker は、ブランチごとに、先頭のコミットが変わった順の一覧を、置き場に書く（[ブランチの履歴](snapshot.md)）。
  - apply request の版は、その一覧のうち、apply request を作ったときの先頭のコミットから後のものである。版の番号は、その中の順番（1 から）である。
  - 利用者が短い間に何度も push すると、worker が見なかったコミットは、版にならない。
- 版の差分は、版のコミットと `main` の分かれた点（base）からの差分である。ブランチの途中のコミットは見ない。
- 版を書いたセッションは、版のコミットのメッセージの trailer `Logiforge-Session-Id:` の自己申告である（[セッション](../files/revisions.md)）。

## 作成

1. 識別子を求める（[同じ要求の繰り返し](../api/writes.md)）。
2. プロバイダで、その識別子の apply request を読む。あれば、それを返して終わる。中身は比べない。
3. worker に、ブランチを読んでもらい、先頭のコミットと base と、差分のスナップショットを得る（[worker の内部 API](worker-api.md)）。
   - 要求に `revision` があり、ブランチの先頭と違えば、`validation_failed` で受け付けない。
4. スナップショットの差分に、[差分の検査](checks.md) を行う。4 種類の操作として読めない変化があれば、`validation_failed` で受け付けない。
5. プロバイダに、pull request を作る。本文の末尾に、識別子と、作ったときの先頭のコミットを書く。
   - そのブランチに open の pull request がすでにあれば、`validation_failed` で受け付けない。
6. 応答する。

- 人への依頼の起票は、手順 2 のあと、プロバイダに記録して応答する。git には書かない。

## 更新

- 版の更新は、利用者が同じブランチに push するだけである。API は呼ばない。
  - worker が新しい先頭を見つけると、新しい版になる。
  - 新しい版の差分は、レビューの前に、worker が作ったスナップショットで検査する。検査に通らない版は、`approve` しても反映しない（[反映の手順](merge.md)）。
- `updateApplyRequest` は、説明欄を書き換えるだけである。

## close

1. プロバイダで、pull request を close する。
2. プロバイダに、close の理由とセッションを記録する。
3. 応答する。

- すでに閉じていれば、今の状態を返す。

## 回答

1. 回答の本文と添付を、プロバイダに記録する（[人への依頼の回答](../files/artifacts.md)）。
   - すでに回答があり、本文と回答した利用者が同じなら、送り直しとみなして成功とする。違えば `not_open` で受け付けない。
2. 応答する。

- 回答は、git に書かない。

## ブランチの掃除

- バックエンドと worker は、利用者のブランチを消さない。
  - なぜ：ブランチは利用者のものだから。ブランチを消すと、open の apply request は close になる。
- 利用者は、反映か close されたあとに、手元のブランチを消す。CLI の `ar cleanup` が手伝う（[コマンドの一覧](../cli/commands.md)）。
- 他人のブランチを消しても、反映済みの apply request は壊れない。反映の中身は `main` にあるからである。open のものは close になる。
