# apply request

木と検証結果を変えるときに、変更をどう提案し、どう反映するかを確かめるときに読む。

## 操作

apply request は、次の 4 種類の操作をいくつか並べた差分である。
apply request 自体は種類を持たない。

| 操作 | 内容 |
|---|---|
| `story.create` | ストーリーを、子仮説と一緒に作る |
| `story.inactivate` | ストーリーを inactive にする |
| `verdict.submit` | 葉の仮説に、検証結果を出す |
| `node.edit` | 仮説の背景情報に、文章を加える |

各操作が満たすべき決まりは、次のページに書く。

- `story.create`：[ストーリー](../tree/README.md) と [葉と検証結果](../tree/README.md)
- `story.inactivate`：[inactive 化](../tree/inactive.md)
- `verdict.submit`：[検証結果](../tree/verdict.md)

`node.edit` では、主文、タイトル、概要を変えない。

- なぜ：主文やストーリーの意味が変わると、それまでの審査と検証が成り立たなくなるから。

複数の操作を 1 つの apply request にまとめる使い方は、[流用](../tree/verdict.md) と [偽と結論する](../tree/falsification.md) に書く。

## opener と reviewer

apply request には、opener と reviewer がいる。

- 役割の説明は、[役割](../roles/README.md) に書く。
- 反映の条件と論理レビューの仕組みは、[論理レビュー](review.md) に書く。

## 状態

```mermaid
stateDiagram-v2
  [*] --> open
  open --> open: 更新
  open --> merged: 反映
  open --> closed: close
  merged --> [*]
  closed --> [*]
```

- apply request の状態は、`open`、`merged`、`closed` の 3 つである。
- `request_changes`、opener の修正、反映の失敗のときは、`open` のままである。
- レビューの種類は、`approve`、`request_changes`、`comment` の 3 つである。
- [反映の条件](review.md) を満たすと、apply request は反映される。
- reviewer の却下と、opener の取り下げは、どちらも `closed` にする。
  - どちらの場合も、理由を書く。
  - 取り下げは、差分を書いたいずれかのセッションが行う。
  - 却下は、差分を書いたどのセッションとも別のセッションが行う。

## 反映

- apply request の操作は、全部まとめて反映する。
  - 一部だけが反映されることはない。
- 不変条件は、apply request が反映されるたびに、常に成り立つ。
  - 反映すると不変条件を満たさなくなる apply request は、反映しない。
  - 不変条件は、[仮説とストーリーの木](../tree/README.md) と [inactive 化](../tree/inactive.md) に書く。
- apply request を作ったあとに、その操作の対象ノードが別の apply request によって変わっていたら、反映しない。
  - その apply request は、理由を残して `open` に戻す。
  - なぜ：古い状態を前提にした変更を、気づかないまま反映しないため。
- 反映すると不変条件を満たさなくなった場合も、理由を残して `open` に戻す。
