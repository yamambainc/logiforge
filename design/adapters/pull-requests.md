# GitForge のポートを pull request と issue で実装する

GitHub アダプタと Forgejo アダプタが、[GitForge](../backend/ports/gitforge.md) のポートを、同じ形で実装するところを確かめるときに読む。
GitForge ごとの違いは、[GitHub アダプタ](github.md) と [Forgejo アダプタ](forgejo.md) に書く。
review と issue の本文に書くことは、[review と issue の本文](texts.md) に書く。

## 対応

| GitForge のポートの操作 | 実装 |
|---|---|
| apply request | 利用者のブランチから `main` への pull request。apply request を作る主体（opener）のトークンで作る |
| apply request の識別子 | pull request の番号 |
| apply request を作った主体 | pull request の作者 |
| 説明欄 | pull request の本文 |
| 先頭のコミット | pull request の head のコミット（[先頭のコミット](../backend/apply-request/README.md#先頭のコミット)） |
| レビュー | pull request の review（approve、request changes、comment）。reviewer のトークンで出す |
| 反映する | pull request の merge。merge の操作を呼んだ主体のトークンで、squash を選び、head の SHA とメッセージを渡す（[merge](../backend/merge.md#merge)）。GitForge の画面からの merge も受け入れる |
| 反映できない理由 | `approve` か merge が今の `main` での検査に通らなかった理由を、人が読む文で書いた、pull request のふつうのコメントと、ラベルの付け替え（[レビュー待ちと修正待ちのラベル](#レビュー待ちと修正待ちのラベル)）。要求を送った主体のトークンで書く |
| close | pull request の close。理由は、close した主体のトークンで書く pull request のコメント（人が読む文） |
| 人への依頼 | issue。起票した主体のトークンで作る。識別子は issue の番号。タイトルは対象ノードの識別子で始める |
| 回答 | 回答した利用者のトークンで書く issue のコメントと、その issue の close |
| 一覧のカーソル | pull request と issue の番号。時刻は使わない |

- GitForge に書くのは、いつも、その API の要求を送った主体のトークンである。Logiforge は、書くための自分のアカウント（書き込み主体）を持たない。
  - なぜ：バックエンドが書く操作は、どれも主体の要求の中で同期で行うので、そのトークンがいつも手元にあるから。
  - そのため、主体には、書く操作に応じた GitForge の権限が要る（[GitHub アダプタ](github.md)、[Forgejo アダプタ](forgejo.md)）。
- 1 つのブランチに、open の pull request は 1 つまでである。
- 人への依頼の種類は、issue のラベル（`logiforge/pvr`、`logiforge/decision`）で決める。
  - ラベルがなければ、付けるときに作る。
- check run と commit status は使わない。
  - なぜ：反映の失敗で要るのは、opener が直すのを待つかどうかと、人が読む理由だけで、それはラベルとコメントで表せるから。check run は Forgejo に API がなく、commit status を書くには GitHub App の権限を増やすことになる。

## apply request の状態

| 状態 | 決め方 |
|---|---|
| merged | pull request が merged |
| closed | pull request が closed |
| closed | pull request は open だが、head のブランチがない。worker の [ブランチの履歴](../backend/snapshot/README.md) で、消えたことがわかる。これは、ブランチの履歴を読むバックエンドが決める。アダプタは、GitForge の状態だけから決める |
| open | それ以外 |

- 画面から close された pull request も、バックエンドを通して close されたものと同じに扱う。画面から reopen されれば、open に戻る。
- head のブランチがないときの扱いは、ブランチを消したときに GitForge が pull request を閉じるかどうかによらない。

## 数える review

- 数えるのは、次のすべてを満たす review である。
  - 出された review である（pending でない）。dismiss されていない。
  - `approve` と `request changes` は、書いた人が、repository に書く権限を持つ。`comment` は、だれのものでも数える。
    - なぜ：public の repository では、書く権限のない人も review を出せるから。数えると、書く権限のない人が、`approve` で反映させたり、`request changes` で反映を止めたりできてしまう。
    - GitHub は、その pull request の review を、GraphQL の 1 回で読み、review の `authorCanPushToRepository` で見る。読んだ時点の権限で決まる。
      - なぜ GraphQL か：REST の `GET /repos/{owner}/{repo}/collaborators/{user}/permission` は、呼ぶ主体に push の権限が要る（なければ 403「Must have push access to view collaborator permission.」）。そのため、読むだけの利用者では読めない。`authorCanPushToRepository` は、読む権限だけで読める。
      - 使うのは、1 件の pull request の review を読むとき（反映、詳細の読み、ラベルの直し）だけである。一覧では、GraphQL を使わない（[一覧の読み](../backend/list-reads.md)）。
    - Forgejo は、review の `official` で見る。review を出した時点で決まる。approve できる人の一覧（approvals whitelist）があれば、それで決まる。
      - なぜ `official` か：ほかの人の権限を読む API は、repository の admin しか使えないから。Forgejo は、書く権限を持つ人の、いちばん新しい `approve` と `request changes` を `official` にする。
    - 書く権限を持つかを読めない review（値がない、読み失敗）は、数えずに、警告する。詳細では「確かめられない」と示す。なぜ：読めないものを書く権限ありとすると、書く権限のない人の review で反映できてしまうから。
    - `comment` は、反映の条件に効かないので、権限を見ない。
- review は、それを出した head のコミット（review の `commit_id`）への review である。
- GitForge は、pull request の作者が自分の pull request を `approve` することを拒む（GitHub と Forgejo）。そのため、opener と reviewer は、別の GitForge のアカウントにする。

## レビュー待ちと修正待ちのラベル

レビュー待ちか修正待ちかは、GitForge の pull request の状態からは決まらない。そのため、バックエンドが pull request のラベルに書く。一覧は、このラベルで決める（[計算](../backend/compute.md#レビュー待ちと修正待ち)）。

| ラベル | 色 | 説明 | 意味 |
|---|---|---|---|
| `logiforge/review-waiting` | `#1d76db` | Logiforge：レビュー待ち | reviewer の review を待つ |
| `logiforge/fix-waiting` | `#d93f0b` | Logiforge：修正待ち | opener が直すのを待つ |

- 付けるのは、向き先が `main` の open の pull request だけである。付いているのは、どちらか 1 つだけである。
  - `approve` されたのに open のもの（`snapshot_pending` や `merge_forbidden` のあと、`approve` し直しを待つもの）は、レビュー待ちである。別のラベルは作らない。なぜ：待っているのが reviewer の `approve` し直しだから。

バックエンドは、次のときに、要求を送った主体のトークンでラベルを付け替える。付け替えるときは、もう一方を外す。

| とき | 付けるラベル | だれのトークンか |
|---|---|---|
| apply request を作ったとき | レビュー待ち | opener |
| `request changes` の review を出したとき | 修正待ち | reviewer |
| `approve` か merge が、opener が直すべき理由（`stale_target`、`invariant_violation`、`conflict`）で今の `main` での検査に通らなかったとき | 修正待ち | 要求を送った主体 |
| opener が直し終えたと申告したとき（[直し終えたと申告する](../api/endpoints/apply-request/fixed.md)） | レビュー待ち | opener |
| 反映したとき、close したとき | 両方を外す | merge した主体か、close した主体 |

- 検査に通らなかったときは、付けるラベルが既に付いていても、外して付け直す。
  - なぜ：ずれ直しは、検査に通らなかった出来事を、そのときラベルを付けた出来事で読むから（下の「ラベルがずれたとき」）。付け直さないと、`request changes` で修正待ちになったあとの出来事が残らない。
- push では、ラベルを変えない。なぜ：push しても、直し終えたとは限らないから。直し終えたかは、opener が申告する。
- ラベルがなければ、付けるときに、上の色と説明で作る。repository の用意のときには作らない。
  - なぜ付ける前に作るか：GitHub では、ない名前のラベルを pull request に付けると、既定の色で作られてしまうから。
- ラベルを作り、付け、外すのに要る権限は、repository に書く権限である（GitHub と Forgejo）。読むだけの主体は、付け外しできない。

### ラベルがずれたとき

ラベルは表示（一覧のレビュー待ちと修正待ち）にだけ使う。反映できるかは、反映の手順で、その pull request の review をその場で読んで決める（[反映の手順](../backend/merge.md)）。
ラベルがずれていると分かったら、そのときにラベルを直す。

- 分かるのは、バックエンドが、その pull request を 1 件読むか、その pull request に書くときである。
  - 1 件読むとき：apply request の詳細とレビューの材料を読む要求
  - 書くとき：review、反映、close、直し終えたという申告
  - 一覧を読むときは、review を読まないので、分からない。worker も、ずれを直さない（worker のトークンは読むだけである）。
- 正しいラベルは、その pull request の次の出来事のうち、いちばん新しいもので決める。
  - 数える `request changes` の review（GitForge の画面から直接出したものも含む）：修正待ち
  - 修正待ちのラベルを付けたこと（opener が直すべき理由で検査に通らなかったこと。GitForge の画面で付けたものも含む）：修正待ち
    - なぜ付けた人を見ないか：検査に通らなかった `approve` は GitForge に出ないので、付けた人の `approve` を手がかりにできないから。ラベルを付けられるのは、repository に書く権限のある人だけである。
  - レビュー待ちのラベルを付けたこと（作成、直し終えたという申告）：レビュー待ち
  - 数える `approve` の review：レビュー待ち。なぜ：`approve` のあとに open のまま残るのは、`approve` し直しを待つものだから
  - どれもなければ、レビュー待ち
  - 反映できない理由のコメントは、読まない。なぜ：人が読む文で、だれでも書けるから。検査に通らなかった出来事は、そのときに付け替えたラベルの出来事で読む。
  - ラベルを付けた時刻は、pull request の出来事の一覧（GitHub は issue の events、Forgejo は issue の timeline）で読む。
  - GitForge の時刻は秒の単位なので、同じ秒の出来事は、どちらが新しいか決まらない。そのときは、今のラベルを変えない。
- 直すときは、その要求を送った主体のトークンで書く。その主体に書く権限がなければ、直さずに、応答に警告を付ける。警告には、書く権限のある人が読めば直ることを書く。
- 一覧での扱いは、直るまで次のとおりである。
  - どちらのラベルもない：レビュー待ちとみなす。ラベルを付ける前に止まった apply request も、レビュー待ちに見えるようにするため。
  - 2 つとも付いている：修正待ちとみなし、警告する。修正を求められたのに、レビュー待ちに見えて reviewer の手間になることを避けるため。
  - 向き先が `main` でないのに付いている：ラベルを見ない。

## main を守る

- `main` を守る設定は、勧めるが求めない。
  - なぜ：GitForge とプランによっては使えない（例：GitHub Free の private のリポジトリ）から。守る設定がなくても、Logiforge は `main` のコミットをそのまま受け入れ、木が不変条件を満たすかだけを見る（[最新のコミットが valid でないとき](../backend/invalid-main.md)）。
- 守る設定を使うときは、次のようにする。
  - `main` への push と force push を禁じ、pull request の merge だけにする。
  - rebase による merge を切る（squash と merge のコミットだけを許す）。なぜ：rebase の merge で並んだ途中のコミットは、pull request の merge のコミットでないので、どの apply request の反映かがわからないから（[どの apply request の反映か](../backend/invalid-main.md#どの-apply-request-の反映か)）。
  - merge できる主体は、絞らない（reviewer が merge するため）。
  - `main` に追いついていることを merge の条件にする設定は、使わない（[衝突](../backend/merge.md)）。

## 信頼の境界

- repository に書く権限（Write）を持つ人とエージェントを信じる。Write は、信頼できる人にだけ配る（[利用者の決め方](../settings/users.md)）。
- Write を持つ主体ができることと、その扱いは、次のとおりである。
  - 他人のブランチを消す。他人の open の apply request が closed になる。反映済みの apply request は変わらない（[ブランチの掃除](../backend/apply-request/README.md)）。
  - pull request と issue を、GitForge の画面から close し、reopen する。GitForge の状態に従う。
  - 本文、タイトル、ラベル、他人のコメントを書き換え、消す。そのため、人への依頼の対象ノード（issue のタイトルの頭）は、偽れる。プロンプトの版と close の理由（人が読む文）も書き換えられる。
  - GitForge の画面や API から、直接 review を出す。バックエンドを通した review と同じに数える。
  - ラベルを付け外す。変わるのは、レビュー待ちか修正待ちかの表示だけである。反映できるかは、review で決まる。
- Write を持つ主体でも、次のものは書き換えられない。バックエンドと worker が信じるのは、これらである。
  - pull request、issue、review、コメントの作者
  - pull request と issue の状態（open、merged、closed）、merge のコミット
  - review の種類（approve、request changes、comment）と、review を出したコミット
  - 他人の review の本文
- review が dismiss されるか消されたら、その review は数えない。dismiss と削除は、取り下げとして扱う。
  - Forgejo では、dismiss できるのは admin だけで、消せるのは review を書いた人と admin だけである。
  - GitHub では、repository に書く権限（Write）を持つ人は、他人の review を dismiss できる（`main` を守る設定で、dismiss できる人を絞っていなければ）。
  - そのため、GitHub では、Write を持つ opener が、他人の `request changes` を dismiss できる。そのあと、opener と別の 1 人の `approve` があれば、GitForge の画面から merge できる。
    - これは、受け入れる危険である。なぜ：Write を持つ主体を信じる（上の「信頼の境界」）からであり、dismiss は GitForge が記録し、だれが行ったかが残るから。
    - Forgejo では、dismiss できるのは admin だけなので、Write を持つだけの opener は、これをできない。
