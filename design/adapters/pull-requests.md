# GitForge アダプタの共通の書き方

GitHub アダプタと Forgejo アダプタが、[GitForge](../backend/ports/gitforge.md) のポートを、同じ形で実装するところを確かめるときに読む。
GitForge ごとの違いは、[GitHub アダプタ](github.md) と [Forgejo アダプタ](forgejo.md) に書く。

## 対応

| GitForge のポートの操作 | 実装 |
|---|---|
| 提案 | 利用者のブランチから `main` への pull request。提案を作る主体（opener）のトークンで作る |
| 提案の記録（識別子と、作ったときの先頭） | pull request を作った直後に、同じトークンで書く最初のコメント |
| 説明欄 | pull request の本文 |
| 先頭のコミット | pull request の head のコミット（[先頭のコミット](../backend/apply-request.md#先頭のコミット)） |
| 反映する | pull request の merge。`approve` をした reviewer のトークンで、squash を選び、head の SHA とメッセージを渡す |
| close | pull request を close し、close した主体のトークンで close の記録のコメントを書く |
| レビュー | pull request の記録のコメント（下の「レビューの書き方」） |
| 人への依頼 | issue。起票した主体のトークンで、起票の直後に記録のコメントを書く |
| 回答 | 回答した利用者のトークンで書く記録のコメント |
| 一覧のカーソル | pull request と issue の番号。時刻は使わない |

- GitForge に書くのは、いつも、その API の要求を送った主体のトークンである。Logiforge は、書くための自分のアカウント（書き込み主体）を持たない。
  - なぜ：バックエンドが書く操作は、どれも主体の要求の中で同期で行うので、そのトークンがいつも手元にあるから。記録は署名で守るので、だれのトークンで書いても偽れない（下の「署名の合う記録だけを信じる」）。
  - そのため、主体には、書く操作に応じた GitForge の権限が要る（[GitHub アダプタ](github.md)、[Forgejo アダプタ](forgejo.md)）。
- 1 つのブランチに、open の pull request は 1 つまでである。
- ラベル（`logiforge/pvr`、`logiforge/decision`、`logiforge/answered`）は、GitForge の画面で見分けるためだけに付ける。バックエンドは読まない。
  - ラベルがなければ、付けるときに作る。

## 記録のコメント

バックエンドが書くコメントの末尾に、trailer を書く。`Logiforge-Record:` で、記録の種類を見分ける。

| `Logiforge-Record:` | 書くとき | ほかの trailer |
|---|---|---|
| `apply_request` | 提案を作った直後 | `Logiforge-Apply-Request:`、`Logiforge-First-Revision:`（作ったときの先頭のコミット）、`Logiforge-Principal:`（提案を作った主体） |
| `close` | 提案を close したとき | `Logiforge-Apply-Request:`、`Logiforge-Session-Id:`。理由は本文 |
| `merge_failure` | 反映できなかったとき | `Logiforge-Apply-Request:`、`Logiforge-Merge-Failure:`（失敗の理由）、`Logiforge-Revision:`（反映しようとした先頭のコミット）。詳しい説明は本文 |
| `human_request` | 人への依頼を起票した直後 | `Logiforge-Human-Request:`、`Logiforge-Human-Request-Kind: pvr\|decision`、`Logiforge-Target-Node:`、`Logiforge-Principal:`（起票した主体）、`Logiforge-Session-Id:`。依頼の本文は本文 |
| `answer` | 回答を受け付けたとき | `Logiforge-Human-Request:`、`Logiforge-Answered-By:`（回答した利用者）、添付ごとに `Logiforge-Attachment: <名前> sha256:<16 進>`。回答は本文と添付 |

- trailer の名前（`Logiforge-Revision:` など）は、GitForge に残った記録を読めるようにする、永続する形なので、前の呼び名（リビジョン）のまま残している。値は、どれもコミットの SHA である。
- 反映のコミットの `Logiforge-Version:` も同じく前の呼び名のままで、値はコミットの SHA である。その数は、先頭のコミットの一覧の中の位置で、API には出さない（[main のコミットと記録](../files/revisions.md)）。
- `Logiforge-Principal:` は、API の要求を送った主体である。
  - なぜ：GitForge の作者は、画面からの編集で信じられないので、主体は署名で守った値から読むため。反映の手順で、reviewer と提案を作った主体が同じかを確かめるのに使う（[反映の手順](../backend/merge.md)）。

- 記録の識別子を確かめる。
  - 1 つの pull request の、最初の `apply_request` の記録が、提案の識別子を決める。`close`、`merge_failure`、レビューの記録は、`Logiforge-Apply-Request:` がその識別子と合うものだけを数える。
  - 1 つの issue の、最初の `human_request` の記録が、依頼の識別子を決める。`answer` の記録は、`Logiforge-Human-Request:` がその識別子と合うものだけを数える。
  - 同じ場所で、記録の識別子（`apply_request`、`human_request`、`answer`、`Logiforge-Review-Id:`）が重なったら、最初のものだけを数え、残りは数えずに警告する。
- 人への依頼の種類と対象ノードは、`human_request` の記録で決める。issue の本文とラベルは使わない。
- 回答済みかどうかは、`answer` の記録があるかで決める。「回答は 1 回だけ」の判定は、バックエンドが行う。
  - 回答を書いたら、バックエンドは issue を閉じる。issue の状態は、回答済みかの判定に使わない。
- すべての記録のコメントの trailer の最後に、`Logiforge-Record-Signature:` を書く。
  - 値は、`<鍵の ID>:<Ed25519 の署名>` である。署名は、記録を置いた場所（repository の、GitForge の中で変わらない識別子と、pull request か issue の番号）と、本文とほかの trailer に対して作る。秘密鍵はバックエンドの設定に置き、公開鍵はバックエンドと worker の設定に置く（[設定と秘密情報](README.md)）。
  - なぜ署名か：worker が、公開鍵だけで記録を確かめられるから。worker に、記録を作れる鍵を持たせない（[反映かの判定](../backend/verify.md)）。
  - repository の識別子は、Forgejo では repository の id、GitHub では node_id である。名前（gitforge_ref）は使わない。
    - なぜ：repository の名前を変えたり、別の organization に移したりしても、記録が壊れないようにするため。
  - 読むときは、コメントが置かれた pull request か issue の番号で求める。
  - なぜ：正しい記録を、ほかの pull request や issue に写して使い回せないようにするため（例：close の記録を写して closed にする、ほかの issue の回答を写す）。
  - `answer` の記録では、添付の sha256 の一覧（`Logiforge-Attachment:`）も、署名に含む。そのため、添付を足したり差し替えたりすると、署名が合わなくなる。
  - バックエンドは、読むたびに署名を確かめる。合わない記録は数えず、警告する。
  - なぜ：編集されたかを、GitForge の `updated_at` に頼らずに見分けるため。添付を足すと `updated_at` が変わる GitForge がある。
- 回答の添付は、`answer` の記録に書いた sha256 で守る。添付を読むときに sha256 を確かめ、合わなければ返さずに警告する。
- 一覧の API は、repository のコメントの一覧（`GET /repos/{owner}/{repo}/issues/comments`）から、まとめて読む。GitHub と Forgejo の両方にある。
- 1 件を読む API は、その pull request か issue のコメントだけを読む（`GET /repos/{owner}/{repo}/issues/{index}/comments`）。
  - pull request と issue のタイトルは、`[Logiforge] <識別子>` にする。1 件を読むときに、どの番号かを探す手がかりにだけ使う。記録としては読まない。
  - タイトルで見つからなければ（書き換えられた、など）、repository のコメントの一覧から探す。
- 提案を作ったあと、記録のコメントを書く前に止まったら、送り直しで記録のコメントを書く。
  - 同じブランチの open の pull request で、記録のコメントがないものがあれば、新しく作らずに、それに記録を書く。

## レビューの書き方

- レビューは、reviewer のトークンで、pull request の記録のコメントとして書く。GitForge の review は使わない。
  - なぜ：GitForge の approve の状態は、反映の条件に使わないから。反映の条件は、署名の合うレビューの記録で決める。
  - なぜコメントか：worker が、repository のコメントの一覧から、ほかの記録と一緒に、変わったものだけを読めるから（[反映かの判定](../backend/verify.md) の「記録の控え」）。review には、repository の全体の一覧がない。
- レビューのコメントの末尾に、次の trailer を書く。
  - `Logiforge-Record: review`
  - `Logiforge-Apply-Request:`（提案の識別子）
  - `Logiforge-Review-Id:`（レビューの識別子）
  - `Logiforge-Previous-Review:`（書いた時点で、その提案の最後のレビューの識別子。最初のレビューでは `-`）
  - `Logiforge-Review-Revision:`（レビューした先頭のコミット）
  - `Logiforge-Principal:`（reviewer の主体）
  - `Logiforge-Review-Kind: approve|request_changes|comment`
  - `Logiforge-Session-Id:`
  - `Logiforge-Prompt-Revision:`
  - `Logiforge-Record-Signature:`（記録のコメントと同じ。最後に書く）
- レビューは、`Logiforge-Previous-Review:` で鎖にする。
  - 鎖の役目は、途中のレビューが丸ごと消されたことを見つけることだけである。
  - 各レビューの `Logiforge-Previous-Review:` は、それより前に数えたレビューのどれか（最初は `-`）であればよい。
    - なぜ：同時に来た 2 つのレビューが、同じレビューを指すのは正しいから。直前のものに限ると、攻撃がなくても鎖が切れたままになる。
  - `Logiforge-Record: review` のあるコメントのうち、数えないものがあれば、鎖が切れたとする。署名が合わないもの、識別子が重なるものは、どれも書き換えられたものである。
  - 人が GitForge の画面で書いた review とコメントは、読まない。
  - 鎖が切れていれば、バックエンドは反映しない。レビューも足さない（[反映の手順](../backend/merge.md)）。

## 署名の合う記録だけを信じる

- 記録とレビューは、署名の合うものだけを数える。だれが書いたか（GitForge の作者）は見ない。
  - なぜ：署名を作れるのは、秘密鍵を持つバックエンドだけなので、セッション ID とプロンプトのリビジョンを、バックエンドを通したものだけに限れるから（[保証の範囲](../../docs/apply-request/review.md)）。
- pull request と issue の本文、タイトル、ラベルは、記録として読まない。
  - なぜ：repository に書く権限を持つ人は、これらを書き換えられるから。
- 署名が合わないもの（あとから編集されたもの）は数えず、警告する（上の「記録のコメント」）。

## 提案の状態

| 状態 | 決め方 |
|---|---|
| merged | GitForge の pull request が merged |
| closed | 署名の合う `close` の記録がある。pull request が reopen されていても、closed とする |
| closed | GitForge の pull request が closed（画面からの close と、ブランチを消したとき） |
| closed | pull request は open だが、head のブランチがない。worker の [ブランチの履歴](../backend/snapshot.md) で、消えたことがわかる。これは、ブランチの履歴を読むバックエンドが決める。アダプタは、GitForge の状態と記録だけから決める |
| open | それ以外 |

- 画面から close された pull request は、GitForge の状態に従う。画面から reopen されれば、open に戻る。
  - なぜ：close の記録がないので、バックエンドを通した close と見分けられないから。
- head のブランチがないときの扱いは、ブランチを消したときに GitForge が pull request を閉じるかどうかによらない。

## main を守る

- `main` を守る設定は、勧めるが求めない。
  - なぜ：GitForge とプランによっては使えない（例：GitHub Free の private のリポジトリ）から。反映によらない変更は、あとから見つけて直す（[壊れた repository](../backend/broken.md)）。
- 守る設定を使うときは、次のようにする。
  - `main` への push と force push を禁じ、pull request の merge だけにする。
  - rebase による merge を切る（squash と merge のコミットだけを許す）。なぜ：rebase の merge は、反映によるものにならないから（[反映かの判定](../backend/verify.md)）。
  - merge できる主体は、絞らない（reviewer が merge するため）。承認されていない pull request の merge は、worker があとから見つける（[反映かの判定](../backend/verify.md)）。
  - `main` に追いついていることを merge の条件にする設定は、使わない（[衝突](../backend/merge.md)）。
- 反映によらない変更が `main` に入ったら、[worker](../backend/worker.md) が見つけ、repository を壊れた状態にする（[壊れた repository](../backend/broken.md)）。

## 信頼の境界

- ブランチに push できる主体は、他人のブランチも消せる。消せば、他人の open の apply request を close にできる。
  - 消されても、反映済みの apply request は壊れない（[ブランチの掃除](../backend/apply-request.md)）。
- repository に書く権限を持つ人は、GitForge の画面から、pull request を close し、reopen できる。
  - バックエンドを通して close したもの（`close` の記録があるもの）は、reopen されても closed のままである。
  - 画面から close されたものは、GitForge の状態に従う。
- 本文、ラベル、コメントの書き換えでは、記録は変わらない（上の「署名の合う記録だけを信じる」）。
- 記録とレビューは、Write の権限のある人なら、だれでも編集でき、消せる（GitHub と Forgejo）。
  - 消すか、記録でない本文に書き換えると、後の `request_changes` が見えなくなり、前の `approve` が最後のレビューに戻りうる。
  - worker は、見た記録を控えに追記し、反映かの判定は控えで行う（[反映かの判定](../backend/verify.md) の「記録の控え」）。バックエンドも、反映の手順で、GitForge の記録に控えの記録を合わせて読む。そのため、worker が見たあとの記録の削除と書き換えは、反映の手順も反映かの判定も変えない。
  - レビューの鎖は、控えの前に消されたレビューのうち、あとのレビューが指すものを見つける（上の「レビューの書き方」）。
  - 残る弱さ：worker が記録を読む前の短いすき間（記録を読む間隔。GitHub で 30 秒）に、記録を消すか書き換えると、控えにも残らない。Write の権限のある人は、すでにブランチを消し、pull request を close できる、信頼の境界の近くにいる主体として扱う。
