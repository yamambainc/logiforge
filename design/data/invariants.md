# 不変条件の守り方

要件の不変条件と性質を、データ構造でどう守るかを確かめるときに読む。
不変条件は、[仮説とストーリーの木](../../docs/tree/README.md)、[inactive 化](../../docs/tree/inactive.md)、[apply request](../../docs/apply-request/README.md) が正本である。
「反映時」は、[反映の判定](apply-request.md) の手順 3 で確かめることを指す。

## 木の形

- 根は 1 つ
  - `hypothesis.parent_story_id` が null の行を、1 行だけにする（部分一意の制約）。
  - 根は、根の主文の作成でだけ作る。
- 仮説とストーリーが交互に並ぶ
  - [仮説とストーリーを別の表](tree.md)にし、親の列が相手の表を指す外部キーにする。
- ストーリーの子仮説は 1 つ以上
  - `story.create` の操作に、子仮説が 1 つ以上あることを確かめる（受け付け時）。
- ストーリーの子仮説を変えない
  - 子仮説を足したり外したりする操作を、API に置かない。

## 文字数

- 主文、タイトル、概要の上限を、操作を受け付けたときと反映時に確かめる。
  - 上限と数え方は、[仮説とストーリーの木](../../docs/tree/README.md) に従う。

## 葉と検証結果

- 検証結果とレポートは 1 対 1
  - 1 つの `verdict` の行に、値とレポートの本文を持たせる。
- 有効な検証結果は 1 件まで
  - `verdict` に、`superseded_revision` が null の行についての部分一意の制約を付ける。
- 有効な検証結果と active な子ストーリーを同時に持たない
  - 反映時に、変わった仮説について確かめる。
- 検証結果を持つ仮説の下にストーリーを作らない
  - 反映時に確かめる。

## inactive

- inactive にできるのはストーリーだけ
  - `inactivated_revision` と `reason` の列は、`story` にだけ置く。
- 仮説の inactive は導かれる状態
  - 祖先のストーリーの `inactivated_revision` から計算する（[計算で求めるもの](tree.md)）。
- inactive な部分木の下にストーリーを作らない
  - 反映時に、親仮説が inactive でないかを確かめる。
- inactive な葉への検証結果は、inactive になる前に葉だった仮説だけ
  - 反映時に、inactive になったリビジョンの 1 つ前の状態で確かめる。

## 反映

- 操作は全部まとめて反映する
  - 反映を 1 つのトランザクションで行う。
- 対象ノードが変わっていたら反映しない
  - `operation.base_revision` と、対象ノードの[最後の変更](tree.md)のリビジョンを比べる。
- 差分を書いたどのセッションとも別のセッションによる承認
  - `review.reviewer_session_id` が、どれかの版の `author_session_id` と同じなら拒む。
- 反映の条件
  - 今の版に対する最後のレビューが `approve` かを確かめる。

## 履歴

- 履歴は append-only
  - 正本の表とイベントログの、UPDATE と DELETE をトリガーで拒む（[append-only](history.md)）。
  - 例外として許す更新は、[append-only](history.md) に書く。
  - blob を書き換えない（[ファイル](files.md)）。
- 任意の時点の木を再現できる
  - [リビジョン r の時点で有効な行](history.md)を選び、値を計算し直す。
