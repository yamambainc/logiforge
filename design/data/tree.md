# 木

木、仮説、ストーリーを、どの列で持つかを確かめるときに読む。
木の決まりは、[仮説とストーリーの木](../../docs/tree/README.md) が正本である。

ここにある表は、すべて[投影](history.md)である。正本はイベントログにある。

## tree

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `tre_` で始まる |
| `root_hypothesis_id` | 識別子 | 根の仮説。一意 |
| `created_at` | 時刻 | 根の主文が作られた時刻 |
| `revision` | 整数 | この木に最後に反映されたイベントの番号 |

- 木は、根の主文の作成と同時に作る。
- 1 つの forge で、複数の木を持てる。

## hypothesis

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `hyp_` で始まる |
| `tree_id` | 識別子 | 属する木 |
| `parent_story_id` | 識別子 | 親ストーリー。根のときだけ null |
| `statement` | 文字列 | 主文 |
| `background` | 文字列 | 背景情報。`node.edit` で末尾に加わる |
| `value` | true / false / null | [伝播](../../docs/tree/propagation.md) で決まる値 |
| `valid_verdict_id` | 識別子 | 有効な検証結果。なければ null |
| `active_child_story_count` | 整数 | active な子ストーリーの数 |
| `inactive` | 真偽 | 祖先に inactive なストーリーがあれば真 |
| `leaf_when_inactivated` | 真偽 | inactive になった時点で葉だったか |
| `last_changed_revision` | 整数 | この仮説が最後に変わったイベントの番号 |

- `parent_story_id` は story を、story の `parent_hypothesis_id` は hypothesis を指す。
  - なぜ：仮説とストーリーを別の表に分けると、交互に並ぶことを表の形で守れるから。
- `value`、`active_child_story_count`、`inactive` は、反映のたびに計算し直す。
  - 葉かどうかは、`active_child_story_count = 0` で判定する。
  - 要再検討かどうかは、active な子ストーリーがあり、そのすべての `value` が false かで判定する。
- `leaf_when_inactivated` は、inactive な葉への検証結果を許すかの判定に使う（[検証結果](../../docs/tree/verdict.md)）。

## story

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `sto_` で始まる |
| `tree_id` | 識別子 | 属する木 |
| `parent_hypothesis_id` | 識別子 | 親仮説 |
| `title` | 文字列 | タイトル |
| `summary` | 文字列 | 概要 |
| `state` | `active` / `inactive` | 保存する状態 |
| `reason` | 文字列 | inactive にした理由。active のときは null |
| `value` | true / false / null | 子仮説の AND |
| `inactive` | 真偽 | 自分か祖先が inactive なら真 |
| `last_changed_revision` | 整数 | このストーリーが最後に変わったイベントの番号 |

- 子仮説の集合を変える列や操作は、持たない（[ストーリー](../../docs/tree/README.md)）。

## 「変わった」の意味

`last_changed_revision` は、次のことが起きたイベントの番号に更新する。

- 仮説：検証結果の提出または差し替え
- 仮説：子ストーリーの作成
- 仮説：子ストーリーの inactive 化
- 仮説：背景情報への追加
- 仮説とストーリー：自分か祖先の inactive 化

伝播で値だけが変わったときは、更新しない。

- なぜ：値の変化まで含めると、離れた場所の反映のたびに、無関係な apply request が `open` に戻ってしまうから。

この列は、[反映の判定](apply-request.md) で使う。
