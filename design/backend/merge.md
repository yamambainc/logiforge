# 反映の手順

apply request を `approve` するときと、`main` に merge するときに、バックエンドが何を確かめ、どう反映するかを確かめるときに読む。
要件は、[反映](../../docs/apply-request/README.md) と [反映の条件](../../docs/apply-request/review.md) が正本である。

## approve と merge の前に確かめること

`approve` の前と、Logiforge からの merge の前に、バックエンドは、同じ確かめを今の `main` で行う。

1. 先頭のコミットのスナップショットがある。なければ、`snapshot_pending`。
2. `latest` を読み、今の `main` のスナップショットを得る。
   - GitForge の `main` が `latest` より進んでいれば、worker が追いつくのを待つ。待つ時間の上限を過ぎたら、`snapshot_pending`。
   - GitForge の `main` は、GitForge のポートの「`main` の先頭を読む」で読む（[GitForge のポート](ports/gitforge.md)）。
   - 待ち始めに読んだ `main` の先頭が、`main` の並びに入り、その番号が `latest` の番号以下になったら、待つのをやめる（[コミットの情報から引くもの](snapshot/main.md#コミットの情報から引くもの)）。そのあとに `main` がさらに進んでもよい。
3. 今の `main` のスナップショットで、次のことを確かめる。
   - 対象ノードが、base のあとに変わっていない（下の「対象ノードの変化」）。変わっていれば、`stale_target`。
   - 先頭のコミットの差分を、今の `main` に重ねる（重ね方は [向き先に重ねる](snapshot/branch.md#向き先に重ねる)）。重ねられなければ、`conflict`。
   - 先頭のコミットの差分を今の `main` に重ねた木に、[差分の検査](checks.md) を行う。破れていれば、`invariant_violation`。
   - 今の `main` は、`latest` のコミットである。`latest` が valid でなくても、`valid_commit` に戻さない（[木を読む API](invalid-main.md#木を読む-api)）。

確かめに通らなければ、その操作（`approve` か merge）を断り、理由をエラーの code で返す。

- opener が直すべき理由（`stale_target`、`invariant_violation`、`conflict`）なら、理由を、pull request のふつうのコメントに、人が読む文で書き、修正待ちのラベルに付け替える（[レビュー待ちと修正待ちのラベル](../adapters/pull-requests.md#レビュー待ちと修正待ちのラベル)）。
  - コメントとラベルは、要求を送った主体のトークンで書く。
- `snapshot_pending` では、GitForge に書かない。なぜ：少し待って送り直せば済むから。
- apply request は、open のまま残る。

## approve

`approve` を受け付けたら、バックエンドは、次の順に処理する。

1. 向き先が `main` なら、[approve と merge の前に確かめること](#approve-と-merge-の前に確かめること) を行う。通らなければ、GitForge に `approve` を出さない。
   - 向き先が `main` でなければ、確かめない。`approve` は出し、応答の警告に、Logiforge が確かめていないことを書く（[向き先](apply-request/target.md#向き先)）。
2. GitForge の pull request に、呼んだ主体のトークンで、`approve` の review を出す。
   - reviewer は、pull request の作者と別の GitForge のアカウントである。GitForge が、作者自身の `approve` を拒む（[数える review](../adapters/pull-requests.md#数える-review)）。
     - なぜセッションを確かめないか：書いた人と確かめる人を分ける保証は、アカウントで GitForge が行うから。セッション ID は自己申告で、偽れるので、確かめても保証にならない。
3. merge はしない。merge は、下の「merge」の操作か、GitForge の画面で行う。
   - なぜ分けるか：`approve` は論理レビューの結果で、merge はそれを `main` に入れる別の判断だから。だれが、いつ merge するかを、`approve` から切り離す。

## merge

Logiforge からの merge（[apply request を merge する](../api/endpoints/apply-request/merge.md)）では、バックエンドは、次の順に処理する。

1. 反映の条件を、GitForge から読んだ pull request と review で確かめる。
   - review は、その pull request の review の一覧を、その場で読む。数えるのは、[数える review](../adapters/pull-requests.md#数える-review) だけである。
   - pull request の向き先が `main` である。違えば、`validation_failed` で断る。Logiforge の反映の対象でないので、GitForge で merge する（[向き先](apply-request/target.md#向き先)）。
   - すべての先頭のコミットに対するレビューのうち、最後の決めるレビュー（`approve` か `request_changes`）が、先頭のコミット（head のコミット）への `approve` である。違えば、`not_approved` で断る。
     - なぜ先頭のコミットに限らないか：ブランチを前の先頭のコミットに戻しても、そのあとの `request_changes` を見落とさないため。
2. [approve と merge の前に確かめること](#approve-と-merge-の前に確かめること) を、もう一度行う。
   - なぜもう一度か：`approve` のあとに、別の apply request が反映されているかもしれないから。
3. GitForge の pull request の squash merge を、先頭のコミットの SHA を指定して、呼んだ主体のトークンで呼ぶ。
   - 呼べるのは、`client` と、その repository に Write がある `user` である。opener も呼べる。
   - Logiforge は、merge するための自分のアカウントを持たない（[GitForge のポートを pull request と issue で実装する](../adapters/pull-requests.md)）。
   - 呼んだ主体に merge の権限がなければ、GitForge が拒む。そのときは、`merge_forbidden` で断る。GitForge には書かない。
   - メッセージには、機械が読む trailer を書かない（[反映のコミット](../files/revisions.md#反映のコミット)）。
4. `latest` が反映のコミットになるまで、上限つきで待ってから応答する（[書いた直後の読み](../api/writes.md)）。
   - 手順 1 から手順 3 のあいだに向き先が変えられると、GitForge は、`main` でないブランチに merge する。
   - このすき間は、ふさがない。`main` に入らない変更は、Logiforge の検査の対象でないからである（[向き先](apply-request/target.md#向き先)）。

- もう反映された apply request への merge の送り直しは、成功として、今の状態を返す。
- 承認された apply request を、人が GitForge の画面で merge してもよい。Logiforge は止めない。`main` のコミットは、どう入ったかによらず受け入れ、ぶつかっていたら、worker が反映のあとに確かめて警告する（[main はいつも正しい](invalid-main.md#main-はいつも正しい)、[コミットの警告](worker/patrol.md#コミットの警告)）。

## 対象ノードの変化

- 各操作の対象ノードについて、「変わった」とみなすパスを決める。
  - 仮説：自分の `hypothesis.md`、`background.md`、`verdicts/`、子ストーリーの `story.md` と `inactivated.md`、祖先のストーリーの `inactivated.md`
  - ストーリー：自分の `story.md` と `inactivated.md`、祖先のストーリーの `inactivated.md`
  - 引用した部分木のノード（部分木のどれかのノードの `last_changed` の番号が、base の番号より大きければ、変わったとみなす）
- 今の `main` のスナップショットツリーで、対象ノードの `last_changed` の番号が、base の番号より大きければ、変わったとみなす（[ノードを変えたコミット](snapshot/main.md#ノードを変えたコミット)）。
  - 1 つのスナップショットツリーを読むだけで確かめられる。
  - base が `main` の並びになければ、変わったものとみなす（[コミットの情報から引くもの](snapshot/main.md#コミットの情報から引くもの)）。
- 今の `main` にないノードは、変わったとみなさない。対象ノードでも、引用した部分木のノードでも同じである。
  - なぜ：同じ apply request で加えるノードなので、base のあとに変わりようがないから。例：同じ apply request で作った仮説の下にストーリーを作る、その仮説に検証結果を出す。
  - そのノードが、今の `main` に差分を重ねた木にあるかは、[head の木で確かめること](checks.md#head-の木で確かめること) で確かめる。
- `stale_target` の説明は、opener が何を直せばよいかわかるように書く。内部の語の「base」は使わず、「ブランチが `main` から分かれたコミット」と書く。
  - 変わったとき：どのノードが、ブランチが `main` から分かれたコミットのあとに、どの `main` のコミット（どの apply request の反映か）で変わったか。
  - base が `main` の並びにないとき：ブランチが `main` から分かれたコミットが、今の `main` の履歴にないこと。変わったとは書かない。
  - どちらも、ブランチを今の `main` に合わせて（rebase か merge で）push し、レビューを受け直すことを書く。
- 伝播で値だけが変わることは、ファイルに現れないので、「変わった」に含まれない。
  - なぜ：値は保存しないから（[保存しないもの](../files/layout.md)）。

## 衝突

- 今の main での確かめを通っても、git の衝突で merge できないことがある（例：同じ `background.md` の末尾への追加どうし）。GitForge の merge が失敗したら、`conflict` で断り、確かめに通らなかったときと同じく、コメントを書いて修正待ちにする。
  - opener は、ブランチを今の `main` に合わせて直し、push する。新しい先頭のコミットになるので、レビューからやり直す。
- merge の手順 3 で、pull request の head が指定した SHA でなければ、GitForge の merge が失敗する。バックエンドは `commit_conflict` を返す。
  - なぜ：レビューのあとに push されたコミットを、レビューなしに反映しないため。
- `main` に追いついていることを merge の条件にする GitForge の保護と、ブランチを `main` に追いつかせる操作は、使わない。
  - なぜ：衝突しない変更まで、`main` が進むたびに作り直しになるから。今の main での確かめと git の衝突で足りる。

## 検査と merge のすき間

- merge の手順 2 で確かめてから、手順 3 で merge するまでの間に、別の apply request が反映されることがある。このすき間は、ふさがない。
  - そのため、まれに、同じ仮説への重なり（例：偽の結論と、新しいストーリーの追加）が、両方反映されることがある。
  - なぜふさがないか：ふさぐには、`main` への書き込みをバックエンドの側で直列にする仕組みが要る。起きる頻度に対して、仕組みが重いから。
- [worker](worker/README.md) は、反映のあとの `main` で、不変条件と、同じ対象ノードへの重なりと、引用した部分木の変化を確かめ、見つけたら警告する（[コミットの警告](worker/patrol.md#コミットの警告)）。
  - 警告を見た opener と reviewer は、新しい apply request で直す（例：ストーリーを inactive にする）。
  - 直す apply request は、すでに破れている条件では拒まれない（[差分の検査](checks.md)）。

## 確かめに通らなかったとき

- apply request は、open のまま残る。
- opener は、ブランチを直して push する。新しい先頭のコミットになる。
  - それまでの approve は、新しい先頭のコミットには効かない（[反映の条件](../../docs/apply-request/review.md)）。
- 修正待ちになるのは、opener が直すべき理由（`stale_target`、`invariant_violation`、`conflict`）のときだけである。
- `snapshot_pending` のあとは、同じ操作（`approve` か merge）を送り直す。
- `merge_forbidden` のあとは、merge の権限のある人が merge する。`approve` し直す必要はない。
