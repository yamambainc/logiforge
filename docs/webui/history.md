# apply request と履歴の画面

利用者が、木がどう変わってきたかを確かめるときに使う画面。
apply request の決まりは、[apply request](../apply-request/README.md) に書く。

## ワイヤフレーム

```
+-----------------------------------------------------------------+
| apply request と履歴    状態: [open ▼]     時点: [最新 ▼]       |
+------------------------------+----------------------------------+
| S2 の作成            open    | 差分                             |
| H2 の検証            merged  |  + story.create   S2 ストーリー2 |
| S0 の inactive 化    merged  |  + verdict.submit H3 true        |
|                              | 先頭のコミット                   |
|                              |  a1b2c3d  11-13 10:42            |
|                              |  e4f5a6b  11-13 11:05            |
|                              | レビュー                         |
|                              |  reviewer: request_changes ...   |
|                              |  opener:   修正しました ...      |
|                              | [approve] [request_changes]      |
|                              | [comment]  本文 [            ]   |
+------------------------------+----------------------------------+
```

## apply request の閲覧

- apply request を、状態（open、merged、closed）で絞り込める。
- 選んだ apply request について、次のものを示す。
  - 差分
  - push された先頭のコミットの一覧。各項目は、SHA と、Logiforge がそのコミットを見た時刻を持つ
  - レビュー。だれが、いつ書いたかも示す。
    - 議論は、`comment` のレビューで示す（[レビューの種類](../apply-request/README.md)）。
  - pull request のコメント。だれが、いつ書いたかも示す。反映できなかった理由と、閉じた理由も、ここに入る。
- open の apply request は、その repository に書ける利用者が、この画面でレビューできる（[反映の条件](../apply-request/review.md)）。
  - レビューは、approve、request_changes、comment のどれかと、本文である。
  - レビューの欄には、CLI の reviewer と同じレビューの材料を示す（[レビューの材料](../apply-request/review.md#レビューの材料)）。
    - apply request の説明欄は、レビューの欄に示さない。なぜ：材料に含めないものを、判断の横に置かないため。
  - approve の前に、レビューする先頭のコミットと、反映したあとの木を示す。押したら、確かめてから送る。
  - 自分が作った apply request なら、approve の前に「あなたが作った apply request です」と示す。
  - approve は、今の `main` での確かめに通ったときだけ出す。通らなかったら、その理由を示す（[approve](../apply-request/review.md#approve)）。
- approve された apply request は、その repository に書ける利用者が、この画面で merge できる（[merge](../apply-request/review.md#merge)）。
  - 承認済みのときだけ、merge の欄を示す（[反映の条件](../apply-request/review.md#反映の条件)）。
- 一覧と詳細には、承認の状態（承認済み、変更を求められている、承認待ち）を示す。一覧では、分からなければ示さない。
  - 押したら、確かめてから送る。merge できなかったら、その理由を示す。

## 反映したあとの木

- open の apply request を選ぶと、それを向き先の今の先頭に反映したあとの木を、[木のビューア](tree-viewer.md) と同じ形で示す。
  - 値と、葉・要再検討・inactive の状態も、反映したあとのものを示す。
  - 差分で加わるノードと変わるノードを、ほかのノードと見分けられるように示す。
  - なぜ：利用者が、差分だけでなく、反映したら木がどうなるかを、反映の前に確かめられるようにするため。
- 向き先が `main` で、今の `main` に重ねられない（衝突する）ときは、木のかわりに、衝突していることと、衝突したファイルを示す。
  - なぜ：衝突したままでは反映できず、反映したあとの木が決まらないから。
- 向き先が `main` でない apply request（ほかの apply request のブランチに向けたものなど）でも、向き先のブランチに反映したあとの木を示す。
  - 木の上に、「`main` 向きではないので、検査していません」と示す。
  - Logiforge は、衝突を確かめない。衝突は、GitForge の画面で確かめる。
  - 示す値が決まりに沿っていることは、保証しない。
  - なぜ：`main` 向きでない apply request は `main` に入らないので、Logiforge は検査しないから。

## 過去の時点の木

- 時点を選ぶと、その時点の木を、[木のビューア](tree-viewer.md) と同じ形で示す。
