# 書き込みの決まり

書き込みの API を実装するときと、クライアントが書き込みの応答を扱うときに読む。
全体に共通する決まりは、[API](README.md) に書く。

## 書いた直後の読み

- 木は、[worker](../backend/worker/README.md) がスナップショットにしてから、読む側に出る。
  - worker は、見回りの間隔ごとに git を見て取り込む（[見回りの間隔](../backend/worker/patrol.md#見回りの間隔)）。

### git に書く API

- git に関わる書き込み（repository の作成、`approve` による反映）の API は、書いたあと、読む側に出るまで待ってから応答する。
  - 読む側に出るとは、`latest` がそのコミットに進むことである。
  - worker は起こさない。
- 待つ時間の上限は、設定の DB に置く（[設定](../settings/README.md#設定の-db-に置くもの)）。
- 上限を過ぎたら、成功として応答し、`Logiforge-Pending-Commit` ヘッダーに、待っていた `main` のコミットを入れる。
  - クライアントは、少し待ってから、そのコミットを指定して読み直す。
  - スナップショットがまだなければ、`snapshot_pending` が返る。

### apply request の作成

- apply request の作成は、worker を待たない。
  - バックエンドが、作業用の git の複製から読んで検査し、pull request を作ったら応答する（[作成](../backend/apply-request/create.md#作成)）。
- worker がブランチを取り込む前は、応答の操作の一覧を、バックエンドの検査の結果から返す。
- 先頭のコミットのスナップショットは、worker がブランチを取り込んでから、読む側に出る。
  - それまで、そのコミットへのレビューは `snapshot_pending` になる。クライアントは、少し待って送り直す。
- 利用者がブランチに push した新しい先頭のコミットは、worker が次に見回ったときに、読む側に出る（[先頭のコミット](../backend/apply-request/README.md#先頭のコミット)）。
- worker が止まっているときの振る舞いは、[worker が止まっているとき](../backend/worker/operation.md#worker-が止まっているとき) に書く。
- GitForge の git を読めないと、apply request の作成は `unavailable` を返す。クライアントは、少し待ってから送り直す。

### snapshot_pending を返す時点

- バックエンドは、`snapshot_pending` を、GitForge にも git にも何かを書く前にだけ返す。
  - なぜ：クライアントが、`snapshot_pending` のあとに同じ要求を送り直しても、二重に書かないようにするため（[コマンドの一覧](../cli/commands.md#apply-request)）。
- 書いたあとに、読む側に出るのを待ちきれなかったときは、成功として `Logiforge-Pending-Commit` を付けて応答する（[git に書く API](#git-に書く-api)）。

### GitForge だけに書く API

- GitForge だけに書く API（レビュー、close、人への依頼の起票と回答）は、待たない。
  - なぜ：GitForge から直接読むので、すぐに見えるから。

## 同じ要求の繰り返し

- apply request の作成と、人への依頼の起票には、`Idempotency-Key` ヘッダーを付けられる。
  - なぜ：通信が切れて送り直しても、apply request や人への依頼が二重にできないようにするため。
- キーがあれば、バックエンドは、キーと番号の対応を設定の DB に置き、同じキーには同じ識別子を返す（[同じ要求の繰り返し](../backend/ids/numbering.md#同じ要求の繰り返し)）。
- 同じ識別子のものがすでにあれば、新しく作らずに、それを返す。
  - 要求の中身は比べない。
  - なぜ：操作の中で作る仮説とストーリーの識別子は、要求ごとに乱数で決まり、最初の要求と比べられないから。
- クライアントは、中身の違う要求ごとに、新しいキーを作る。
- バックエンドが書く順は、[apply request](../backend/apply-request/README.md) に書く。
- 応答は、どこにも置かない。
  - なぜ：同じ識別子を返せば、GitForge から読んで同じ応答を作れるから。
- repository の作成と、人への依頼への回答は、キーを使わない。
  - repository の作成：名前で一意である。送り直しは、[repository の用意](../backend/README.md#repository-の用意) の判定で済む。
  - 回答：1 回だけである。送り直しは、回答がすでにあり、本文と回答した利用者が同じなら、成功とする（[人への依頼の回答](../files/artifacts.md#人への依頼の回答)）。
