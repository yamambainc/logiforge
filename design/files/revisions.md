# main のコミットと記録

要件の変更が `main` のコミットにどう並ぶかと、コミットに残す記録の形式を確かめるときに読む。

## main のコミット

- 要件の各時点は、`main` ブランチの first-parent の並びにあるコミットである。
  - 各時点は、コミットの SHA で指す。
- apply request が 1 件反映されるたびに、`main` に 1 つのコミットができる（[反映の手順](../backend/merge.md)）。
  - 反映は、GitForge の pull request の squash merge である。
  - そのため、1 つの apply request の変更は、すべて同じコミットで反映される。
- コミットの順番は、first-parent の並びで決まる。

## 引用した時点

- 部分木の引用は、ノードの識別子だけで書く（[検証結果のファイル](layout.md)）。決まりは [引用](../../docs/tree/verdict.md#引用) に従う。
- 引用した時点（検証結果が反映された `main` のコミット）は、その検証結果のファイルが加わった `main` のコミットとして求める。
  - なぜ：検証結果のファイルは、加えたあと変えないので、加わったコミットは git の履歴で 1 つに決まるから。
  - 加わったコミットは、スナップショットツリーの、検証結果のファイルの `added` に持つ（[ノードを変えたコミット](../backend/snapshot/main.md#ノードを変えたコミット)）。
- 引用した時点の部分木は、レビューした先頭のコミットの木の部分木と同じである。
  - なぜ：反映の前の検査で、引用した部分木が base のあとに変わっていないことを確かめるから（[対象ノードの変化](../backend/merge.md#対象ノードの変化)）。
  - ただし、検査と merge のすき間と、GitForge の画面での merge では、base から反映までに部分木が変わることがある。
  - その変化は、引用元の変化には現れない。worker が、反映のコミットの重なり（`overlap`）として警告する（[コミットの警告](../backend/worker/patrol.md#コミットの警告)）。
- 引用した時点の部分木は、そのコミットの `tree/` から、識別子のディレクトリを探して読む（下の「任意の時点の木」）。
- 引用した時点は、次のことに使う。
  - 引用元の変化の判定（[引用元の変化](../../docs/tree/verdict.md#引用元の変化)）
  - 引用した時点の部分木の再現

## 任意の時点の木

- `main` のコミット c の木は、c を checkout したときの `tree/` である。
- 値は、その `tree/` から計算し直す（[真偽値の伝播](../../docs/tree/propagation.md)）。

## 記録の形式

バックエンドは、反映のコミットのメッセージ（squash merge のメッセージ）の末尾に、次の trailer を書く。

| trailer | 内容 |
|---|---|
| `Logiforge-Apply-Request:` | apply request の識別子 |
| `Logiforge-Version:` | 反映した先頭のコミットの、先頭のコミットの一覧の中の位置と、その SHA（例：`3 <commit SHA>`） |
| `Logiforge-Session-Id:` | 先頭のコミットを書いたセッション。自己申告である（下の「セッション」） |
| `Logiforge-Repository:` | repository の名前。ディレクトリの repository だけに書く（[ディレクトリ方式の履歴](../directory/history.md)） |
| `Logiforge-Migrated-From:` | 元の repository の名前と、移したコミットの SHA。別リポジトリ方式から移した用意のコミットだけに書く（[別リポジトリ方式から移す](../directory/migrate.md)） |

- trailer の名前は変えない。
  - なぜ：git の履歴に残る、永続する形だから。
- `Logiforge-Version:` は、名前に Version とあるが、反映した先頭のコミットを指す。
  - 位置の数は、API には出さない。
- 最初のコミットは、`Logiforge-Apply-Request:` を持たない。
- 反映によるコミットかは、trailer でなく、中身（元の pull request の記録とレビュー、差分を重ねた木）で決める（[反映かの判定](../backend/verify/README.md)）。
  - なぜ：`main` を守る設定は求めないので、trailer はだれでも書けるから。
- trailer に署名は付けない。
  - なぜ：中身で決めるので、trailer を偽っても、反映によるコミットにはならないから。
  - 人が GitForge の画面で merge して trailer がなくても、反映によるコミットになる。
- ディレクトリの repository では、[版](../directory/history.md#版)はディレクトリを変えたコミットだけである。
  - 反映かの判定も、そのコミットにだけ行う。

## セッション

- apply request の先頭のコミットを書いたセッションは、利用者の自己申告である。
  - 先頭のコミットのメッセージの trailer `Logiforge-Session-Id:` に、CLI が書く（[CLI](../cli/README.md)）。
  - trailer のない先頭のコミットは、書いたセッションがわからないので、`approve` できない（[反映の条件](../backend/merge.md)）。
- 自己申告であることと、その限りでの保証は、[保証の範囲](../../docs/apply-request/review.md) に書く。

## 誰が、いつ、どの apply request で

- 誰が：反映のコミットの `Logiforge-Session-Id:` と、pull request を作った主体
- いつ：コミットの時刻
- どの apply request で：`Logiforge-Apply-Request:`

レビューの記録は、[GitForge のポート](../backend/ports/gitforge.md) に置く。
