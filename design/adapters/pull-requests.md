# GitForge アダプタの共通の書き方

GitHub アダプタと Forgejo アダプタが、[GitForge](../backend/ports/gitforge.md) のポートを、同じ形で実装するところを確かめるときに読む。
GitForge ごとの違いは、[GitHub アダプタ](github.md) と [Forgejo アダプタ](forgejo.md) に書く。

## 対応

| GitForge のポートの操作 | 実装 |
|---|---|
| 提案 | 利用者のブランチから `main` への pull request。書き込み主体が作る |
| 提案の記録（識別子と、作ったときの先頭） | 書き込み主体が、pull request を作った直後に書く最初のコメント |
| 説明欄 | pull request の本文 |
| 版 | pull request の head のコミット（[版](../backend/apply-request.md)） |
| 反映する | pull request の merge。squash を選び、head の SHA とメッセージを渡す |
| close | pull request を close し、書き込み主体が close の記録のコメントを書く |
| レビュー | コメントの種類の review。版のコミットを `commit_id` に入れる |
| 人への依頼 | issue。書き込み主体が、起票の直後に記録のコメントを書く |
| 回答 | 書き込み主体が、利用者の回答として書く記録のコメント |
| 一覧のカーソル | pull request と issue の番号。時刻は使わない |

- 1 つのブランチに、open の pull request は 1 つまでである。
- ラベル（`logiforge/pvr`、`logiforge/decision`、`logiforge/answered`）は、GitForge の画面で見分けるためだけに付ける。バックエンドは読まない。

## 記録のコメント

書き込み主体が書くコメントの末尾に、trailer を書く。`Logiforge-Record:` で、記録の種類を見分ける。

| `Logiforge-Record:` | 書くとき | ほかの trailer |
|---|---|---|
| `apply_request` | 提案を作った直後 | `Logiforge-Apply-Request:`、`Logiforge-First-Revision:` |
| `close` | 提案を close したとき | `Logiforge-Session-Id:`。理由は本文 |
| `human_request` | 人への依頼を起票した直後 | `Logiforge-Human-Request:`、`Logiforge-Human-Request-Kind: pvr\|decision`、`Logiforge-Target-Node:`、`Logiforge-Session-Id:` |
| `answer` | 回答を受け付けたとき | `Logiforge-Answered-By:`（回答した利用者）、添付ごとに `Logiforge-Attachment: <名前> sha256:<16 進>`。回答は本文と添付 |

- 人への依頼の種類と対象ノードは、`human_request` の記録で決める。issue の本文とラベルは使わない。
- 回答済みかどうかは、`answer` の記録があるかで決める。「回答は 1 回だけ」の判定は、バックエンドが行う。
  - 回答を書いたら、バックエンドは issue を閉じる。issue の状態は、回答済みかの判定に使わない。
- すべての記録のコメントの trailer の最後に、`Logiforge-Record-Mac:` を書く。
  - 値は、`<鍵の ID>:<HMAC-SHA256>` である。HMAC は、本文とほかの trailer に対して求める。鍵は、バックエンドの設定に置く（[設定と秘密情報](README.md)）。
  - `answer` の記録では、添付の sha256 の一覧（`Logiforge-Attachment:`）も、MAC に含む。そのため、添付を足したり差し替えたりすると、MAC が合わなくなる。
  - バックエンドは、読むたびに MAC を確かめる。合わない記録は数えず、警告する。
  - なぜ：編集されたかを、GitForge の `updated_at` に頼らずに見分けるため。添付を足すと `updated_at` が変わる GitForge がある。
- 回答の添付は、`answer` の記録に書いた sha256 で守る。添付を読むときに sha256 を確かめ、合わなければ返さずに警告する。
- 一覧の API は、repository のコメントの一覧（`GET /repos/{owner}/{repo}/issues/comments`）から、まとめて読む。GitHub と Forgejo の両方にある。
- 1 件を読む API は、その pull request か issue のコメントだけを読む（`GET /repos/{owner}/{repo}/issues/{index}/comments`）。
- 提案を作ったあと、記録のコメントを書く前に止まったら、送り直しで記録のコメントを書く。
  - 書き込み主体が作った open の pull request で、同じブランチのもので、記録のコメントがないものがあれば、新しく作らずに、それに記録を書く。

## レビューの書き方

- レビューは、すべてコメントの種類の review として書く。
  - なぜ：pull request を作った書き込み主体は、その pull request を approve できない GitForge があるから。GitForge の approve の状態は、反映の条件に使わない。
- レビューの本文の末尾に、次の trailer を書く。
  - `Logiforge-Review-Id:`（レビューの識別子）
  - `Logiforge-Review-Kind: approve|request_changes|comment`
  - `Logiforge-Session-Id:`
  - `Logiforge-Prompt-Revision:`
  - `Logiforge-Record-Mac:`（記録のコメントと同じ。最後に書く）

## 書き込み主体が書いたものだけを信じる

- 提案、レビュー、記録のコメントは、作者が書き込み主体のものだけを数える。
  - なぜ：セッション ID とプロンプトのリビジョンを、バックエンドを通したものだけに限るため（[保証の範囲](../../docs/apply-request/review.md)）。
- pull request と issue の本文、タイトル、ラベルは、記録として読まない。
  - なぜ：repository に書く権限を持つ人は、これらを書き換えられるから。
- 書き込み主体のコメントでも、MAC が合わないもの（あとから編集されたもの）は数えず、警告する（上の「記録のコメント」）。
  - なぜ：repository に書く権限を持つ人は、他人のコメントも編集できるから。書き込み主体は、記録のコメントを編集しない。

## 提案の状態

| 状態 | 決め方 |
|---|---|
| merged | GitForge の pull request が merged |
| closed | 書き込み主体の `close` の記録がある。pull request が reopen されていても、closed とする |
| closed | GitForge の pull request が closed（画面からの close と、ブランチを消したとき） |
| closed | pull request は open だが、head のブランチがない。worker の [ブランチの履歴](../backend/snapshot.md) で、消えたことがわかる |
| open | それ以外 |

- 画面から close された pull request は、GitForge の状態に従う。画面から reopen されれば、open に戻る。
  - なぜ：close の記録がないので、バックエンドを通した close と見分けられないから。
- head のブランチがないときの扱いは、ブランチを消したときに GitForge が pull request を閉じるかどうかによらない。

## main を守る

- `main` を守る設定は、必須である。
  - `main` への push を禁じ、pull request の merge だけにする。
  - merge できるのは、書き込み主体だけにする。
  - `main` に追いついていることを merge の条件にする設定は、使わない（[衝突](../backend/merge.md)）。
- 守る設定が外れて、反映によらない変更が `main` に入ったら、[worker](../backend/worker.md) が見つけて警告する。

## 信頼の境界

- ブランチに push できる主体は、他人のブランチも消せる。消せば、他人の open の apply request を close にできる。
  - 消されても、反映済みの apply request は壊れない（[ブランチの掃除](../backend/apply-request.md)）。
- repository に書く権限を持つ人は、GitForge の画面から、pull request を close し、reopen できる。
  - 書き込み主体が close したものは、reopen されても closed のままである。
  - 画面から close されたものは、GitForge の状態に従う。
- 本文、ラベル、コメントの書き換えでは、記録は変わらない（上の「書き込み主体が書いたものだけを信じる」）。
- repository に書く権限を持つ人は、書き込み主体のコメントを消すこともできる。消された記録は戻らない。
  - GitHub では、これを防げない。Forgejo では、利用者の権限を絞って防ぐ（[Forgejo アダプタ](forgejo.md)）。
