# 不変条件の守り方

要件の不変条件と性質を、データ構造でどう守るかを確かめるときに読む。
不変条件は、[仮説とストーリーの木](../../docs/tree/README.md)、[inactive 化](../../docs/tree/inactive.md)、[apply request](../../docs/apply-request/README.md) が正本である。
「反映時」は、[反映の判定](apply-request.md) の手順 3 で確かめることを指す。

## 木の形

- 根は 1 つ
  - `tree.root_hypothesis_id` を一意にする。
  - 根の仮説は、木を作るときにだけ作る。
- 根だけが親を持たない
  - `hypothesis.parent_story_id` が null の仮説は、`tree.root_hypothesis_id` と一致するものだけにする（反映時）。
- 仮説とストーリーが交互に並ぶ
  - [仮説とストーリーを別の表](tree.md)にし、親の列が相手の表を指す外部キーにする。
- ストーリーの子仮説は 1 つ以上
  - `story.create` の操作に、子仮説が 1 つ以上あることを確かめる（受け付け時）。
- ストーリーの子仮説を変えない
  - 子仮説を足したり外したりする操作を、API に置かない。

## 文字数

- 主文、タイトル、概要の上限を、操作を受け付けたときと反映時に確かめる（[仮説](../../docs/tree/README.md)）。
- 文字数は、Unicode NFC に正規化したあとのコードポイント数で数える。

## 葉と検証結果

- 検証結果とレポートは 1 対 1
  - `verdict.report_id` を一意にし、null を許さない。
- 有効な検証結果は 1 件まで
  - `verdict` に、`superseded_revision` が null の行についての部分一意の制約を付ける。
- 有効な検証結果と active な子ストーリーを同時に持たない
  - 反映時に、変わった仮説について確かめる。
- 検証結果を持つ仮説の下にストーリーを作らない
  - 反映時に確かめる。

## inactive

- inactive にできるのはストーリーだけ
  - `state` と `reason` の列は、`story` にだけ置く。
- 仮説の inactive は導かれる状態
  - `hypothesis.inactive` は投影で、ストーリーの inactive 化のたびに子孫へ広げる。
- inactive な部分木の下にストーリーを作らない
  - 反映時に、親仮説の `inactive` を確かめる。
- inactive な葉への検証結果は、inactive になる前に葉だった仮説だけ
  - 反映時に `leaf_when_inactivated` を確かめる。

## 反映

- 操作は全部まとめて反映する
  - 反映を 1 つのトランザクションで行う。
- 対象ノードが変わっていたら反映しない
  - `operation.base_revision` と、対象ノードの `last_changed_revision` を比べる。
- opener と reviewer は別のセッション
  - `review.reviewer_session_id` と `apply_request.opener_session_id` が同じレビューを拒む。
- 反映の条件
  - 今の `version` に対する最後のレビューが `approve` かを確かめる。

## 履歴

- 履歴は append-only
  - `event` 表への UPDATE と DELETE をトリガーで拒む。
  - blob を書き換えない（[ファイル](files.md)）。
- 任意の時点の木を再現できる
  - [スナップショットとイベントの再生](history.md)で作る。
