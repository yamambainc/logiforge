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
| 反映する | pull request の merge。`approve` をした reviewer のトークンで、squash を選び、head の SHA とメッセージを渡す |
| 反映の失敗 | head のコミットへの commit status（context は `logiforge/merge`、state は `failure`、description は失敗の理由）と、理由を書いた pull request のコメント |
| close | pull request の close。理由とセッション ID は、close した主体のトークンで書く pull request のコメント |
| 人への依頼 | issue。起票した主体のトークンで作る。識別子は issue の番号 |
| 回答 | 回答した利用者のトークンで書く issue のコメントと、その issue の close |
| 一覧のカーソル | pull request と issue の番号。時刻は使わない |

- GitForge に書くのは、いつも、その API の要求を送った主体のトークンである。Logiforge は、書くための自分のアカウント（書き込み主体）を持たない。
  - なぜ：バックエンドが書く操作は、どれも主体の要求の中で同期で行うので、そのトークンがいつも手元にあるから。
  - そのため、主体には、書く操作に応じた GitForge の権限が要る（[GitHub アダプタ](github.md)、[Forgejo アダプタ](forgejo.md)）。
- 1 つのブランチに、open の pull request は 1 つまでである。
- 人への依頼の種類は、issue のラベル（`logiforge/pvr`、`logiforge/decision`）で決める。直すための apply request の種類は、pull request のラベル（`logiforge/repair.revert`、`logiforge/repair.accept`）で決める。
  - ラベルがなければ、付けるときに作る。
- GitHub の check run は使わない。なぜ：Forgejo に、check run の API がないから。commit status は、両方にある。

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
  - 書いた人が、repository に書く権限を持つ。GitHub は `authorCanPushToRepository`、Forgejo は `official` で見る。
    - なぜ：public の repository では、書く権限のない人も review を出せるから。
  - `approve` と `request changes` は、本文に、書いたセッションの行がある（[review の本文](texts.md#review-の本文)）。行のない `approve` は数えず、警告する。
- review は、それを出した head のコミット（review の `commit_id`）への review である。
- GitForge は、pull request の作者が自分の pull request を `approve` することを拒む（GitHub と Forgejo）。そのため、opener と reviewer は、別の GitForge のアカウントにする。

## main を守る

- `main` を守る設定は、勧めるが求めない。
  - なぜ：GitForge とプランによっては使えない（例：GitHub Free の private のリポジトリ）から。反映によらない変更は、あとから見つけて直す（[壊れた repository](../backend/broken.md)）。
- 守る設定を使うときは、次のようにする。
  - `main` への push と force push を禁じ、pull request の merge だけにする。
  - rebase による merge を切る（squash と merge のコミットだけを許す）。なぜ：rebase の merge は、反映によるものにならないから（[反映かの判定](../backend/verify/README.md)）。
  - merge できる主体は、絞らない（reviewer が merge するため）。承認されていない pull request の merge は、worker があとから見つける（[反映かの判定](../backend/verify/README.md)）。
  - `main` に追いついていることを merge の条件にする設定は、使わない（[衝突](../backend/merge.md)）。
- 反映によらない変更が `main` に入ったら、[worker](../backend/worker/README.md) が見つけ、repository を壊れた状態にする（[壊れた repository](../backend/broken.md)）。

## 信頼の境界

- repository に書く権限（Write）を持つ人とエージェントを信じる。Write は、信頼できる人にだけ配る（[利用者の決め方](../settings/users.md)）。
- Write を持つ主体ができることと、その扱いは、次のとおりである。
  - 他人のブランチを消す。他人の open の apply request が closed になる。反映済みの apply request は壊れない（[ブランチの掃除](../backend/apply-request/README.md)）。
  - pull request と issue を、GitForge の画面から close し、reopen する。GitForge の状態に従う。
  - 本文、ラベル、他人のコメントを書き換え、消す。そのため、本文に書いたセッション ID、プロンプトの版、対象ノード、close の理由は、偽れる。
  - GitForge の画面や API から、直接 review を出す。本文にセッションの行があれば、バックエンドを通した review と同じに数える。
  - commit status を立てる。`logiforge/merge` の status を偽っても、変わるのはレビュー待ちか修正待ちかの表示だけである。
- Write を持つ主体でも、次のものは書き換えられない。バックエンドと worker が信じるのは、これらである。
  - pull request、issue、review、コメントの作者
  - pull request と issue の状態（open、merged、closed）、merge のコミット
  - review の種類（approve、request changes、comment）と、review を出したコミット
  - 他人の review の本文
- review が dismiss されるか消されたら、その review は数えない。dismiss と削除は、取り下げとして扱う。
  - Forgejo では、dismiss できるのは admin だけで、消せるのは review を書いた人と admin だけである。
