# Apply Request

木と検証結果を変えるときに、変更をどう提案し、どう適用するかを確かめるときに読む。

## 操作

Apply Request（AR）は、次の 4 種類の操作をいくつか並べた差分である。
AR 自体は種類を持たない。

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

複数の操作を 1 つの AR にまとめる使い方は、[流用](../tree/verdict.md) と [偽と結論する](../tree/falsification.md) に書く。

## opener と reviewer

- opener は、AR を出す主体である。
- reviewer は、AR を審査する主体である。
- opener と reviewer は、別の主体でなければならない。
  - なぜ：仮説とストーリーを考える主体と、それを審査する主体を分けるため。

誰が opener と reviewer を担うかは、[役割](../agents/README.md) に書く。
審査の観点は、[Reviewer の審査観点](review.md) に書く。

## 状態

```mermaid
stateDiagram-v2
  [*] --> open
  open --> open: request_changes / opener の修正 / 適用の失敗
  open --> merged: approve のあと、ハーネスが適用する
  open --> closed: 却下 / 取り下げ（理由が必須）
  merged --> [*]
  closed --> [*]
```

- AR の状態は、`open`、`merged`、`closed` の 3 つである。
- レビューの種類は、`approve`、`request_changes`、`comment` の 3 つである。
- reviewer が `approve` すると、ハーネスが自動で適用する。
- reviewer の却下と、opener の取り下げは、どちらも `closed` にする。
  - どちらの場合も、理由を書く。

## 適用

- AR の操作は、全部まとめて適用する。
  - 一部だけが反映されることはない。
- 提出時と適用時に、適用後の状態が不変条件を満たすかを確かめる。
  - 不変条件は、[仮説とストーリーの木](../tree/README.md) と [inactive 化](../tree/inactive.md) に書く。
- 適用に失敗したら、理由を残して `open` に戻す。
  - 例：同時に別の AR が適用され、不変条件を満たさなくなった。
- 値の計算は、適用と同じトランザクションで行う（[真偽値の伝播](../tree/propagation.md)）。
