# GitForge のポートを pull request と issue で実装する

GitHub アダプタと Forgejo アダプタが、[GitForge](../backend/ports/gitforge.md) のポートを、同じ形で実装するところを確かめるときに読む。
GitForge ごとの違いは、[GitHub アダプタ](github.md) と [Forgejo アダプタ](forgejo.md) に書く。
記録のコメントとレビューの書き方は、[記録のコメントとレビュー](records.md) に書く。

## 対応

| GitForge のポートの操作 | 実装 |
|---|---|
| apply request | 利用者のブランチから `main` への pull request。apply request を作る主体（opener）のトークンで作る |
| apply request の記録（識別子と、作ったときの先頭） | pull request を作った直後に、同じトークンで書く最初のコメント |
| 説明欄 | pull request の本文 |
| 先頭のコミット | pull request の head のコミット（[先頭のコミット](../backend/apply-request/README.md#先頭のコミット)） |
| 反映する | pull request の merge。`approve` をした reviewer のトークンで、squash を選び、head の SHA とメッセージを渡す |
| close | pull request を close し、close した主体のトークンで close の記録のコメントを書く |
| レビュー | pull request の記録のコメント（[レビューの書き方](records.md#レビューの書き方)） |
| 人への依頼 | issue。起票した主体のトークンで、起票の直後に記録のコメントを書く |
| 回答 | 回答した利用者のトークンで書く記録のコメント |
| 一覧のカーソル | pull request と issue の番号。時刻は使わない |

- GitForge に書くのは、いつも、その API の要求を送った主体のトークンである。Logiforge は、書くための自分のアカウント（書き込み主体）を持たない。
  - なぜ：バックエンドが書く操作は、どれも主体の要求の中で同期で行うので、そのトークンがいつも手元にあるから。記録は署名で守るので、だれのトークンで書いても偽れない（[署名の合う記録だけを信じる](records.md#署名の合う記録だけを信じる)）。
  - そのため、主体には、書く操作に応じた GitForge の権限が要る（[GitHub アダプタ](github.md)、[Forgejo アダプタ](forgejo.md)）。
- 1 つのブランチに、open の pull request は 1 つまでである。
- ラベル（`logiforge/pvr`、`logiforge/decision`、`logiforge/answered`）は、GitForge の画面で見分けるためだけに付ける。バックエンドは読まない。
  - ラベルがなければ、付けるときに作る。

## apply request の状態

| 状態 | 決め方 |
|---|---|
| merged | GitForge の pull request が merged |
| closed | 署名の合う `close` の記録がある。pull request が reopen されていても、closed とする |
| closed | GitForge の pull request が closed（画面からの close と、ブランチを消したとき） |
| closed | pull request は open だが、head のブランチがない。worker の [ブランチの履歴](../backend/snapshot/README.md) で、消えたことがわかる。これは、ブランチの履歴を読むバックエンドが決める。アダプタは、GitForge の状態と記録だけから決める |
| open | それ以外 |

- 画面から close された pull request は、GitForge の状態に従う。画面から reopen されれば、open に戻る。
  - なぜ：close の記録がないので、バックエンドを通した close と見分けられないから。
- head のブランチがないときの扱いは、ブランチを消したときに GitForge が pull request を閉じるかどうかによらない。

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

- ブランチに push できる主体は、他人のブランチも消せる。消せば、他人の open の apply request を close にできる。
  - 消されても、反映済みの apply request は壊れない（[ブランチの掃除](../backend/apply-request/README.md)）。
- repository に書く権限を持つ人は、GitForge の画面から、pull request を close し、reopen できる。
  - バックエンドを通して close したもの（`close` の記録があるもの）は、reopen されても closed のままである。
  - 画面から close されたものは、GitForge の状態に従う。
- 本文、ラベル、コメントの書き換えでは、記録は変わらない（[署名の合う記録だけを信じる](records.md#署名の合う記録だけを信じる)）。
- 記録とレビューは、Write の権限のある人なら、だれでも編集でき、消せる（GitHub と Forgejo）。
  - 消すか、記録でない本文に書き換えると、後の `request_changes` が見えなくなり、前の `approve` が最後のレビューに戻りうる。
  - worker は、見た記録を控えに追記し、反映かの判定は控えで行う（[記録の控え](../backend/verify/records.md)）。バックエンドも、反映の手順で、GitForge の記録に控えの記録を合わせて読む。そのため、worker が見たあとの記録の削除と書き換えは、反映の手順も反映かの判定も変えない。
  - レビューの鎖は、控えの前に消されたレビューのうち、あとのレビューが指すものを見つける（[レビューの書き方](records.md#レビューの書き方)）。
  - 残る弱さ：worker が記録を読む前の短いすき間（記録を読む間隔。GitHub で 30 秒）に、記録を消すか書き換えると、控えにも残らない。Write の権限のある人は、すでにブランチを消し、pull request を close できる、信頼の境界の近くにいる主体として扱う。
