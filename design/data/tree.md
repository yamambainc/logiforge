# 木

仮説、背景情報、ストーリーを、どの列で持つかを確かめるときに読む。
木の決まりは、[仮説とストーリーの木](../../docs/tree/README.md) が正本である。

## hypothesis

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `hyp_` で始まる |
| `parent_story_id` | 識別子 | 親ストーリー。根のときだけ null |
| `statement` | 文字列 | 主文 |
| `created_revision` | 整数 | 作られたリビジョン |

- `parent_story_id` が null の行は、1 行だけにする（部分一意の制約）。
- `parent_story_id` は story を、story の `parent_hypothesis_id` は hypothesis を指す。
  - なぜ：仮説とストーリーを別の表に分けると、交互に並ぶことを表の形で守れるから。

## background

| 列 | 型 | 内容 |
|---|---|---|
| `hypothesis_id` | 識別子 | 対象の仮説 |
| `text` | 文字列 | 加えた文章 |
| `added_revision` | 整数 | 加えたリビジョン |

- 仮説を作るときの背景情報も、1 行目として入れる。
- `node.edit` のたびに、1 行足す。
- リビジョン r の背景情報は、`added_revision ≤ r` の行を順につないだものである。

## story

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `sto_` で始まる |
| `parent_hypothesis_id` | 識別子 | 親仮説 |
| `title` | 文字列 | タイトル |
| `summary` | 文字列 | 概要 |
| `created_revision` | 整数 | 作られたリビジョン |
| `inactivated_revision` | 整数 | inactive にしたリビジョン。active な間は null |
| `reason` | 文字列 | inactive にした理由 |

- 子仮説の集合を変える列や操作は、持たない（[ストーリー](../../docs/tree/README.md)）。

## 計算で求めるもの

次のものは保存せず、リビジョンを決めて計算する。

- 値（[真偽値の伝播](../../docs/tree/propagation.md)）
- 葉かどうか：active な子ストーリーが 0 件か
- 要再検討かどうか
- inactive かどうか：自分か祖先のストーリーの `inactivated_revision` が、そのリビジョン以下か
- inactive になる前に葉だったか：inactive になったリビジョンの 1 つ前で、葉だったか

## ノードの最後の変更

[反映の判定](apply-request.md) では、対象ノードが最後に変わったリビジョンを使う。
仮説が最後に変わったリビジョンは、次のリビジョンのうち最も大きいものである。

- 仮説が作られた
- 背景情報が加わった
- 検証結果が出された、または差し替えられた
- 子ストーリーが作られた、または inactive になった
- 祖先のストーリーが inactive になった

ストーリーについては、作られたリビジョンと、自分か祖先が inactive になったリビジョンを使う。

伝播で値だけが変わったときは、含めない。

- なぜ：値の変化まで含めると、離れた場所の反映のたびに、無関係な apply request が `open` に戻ってしまうから。
