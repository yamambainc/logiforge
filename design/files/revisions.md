# main のコミットと記録

要件の変更が `main` のコミットにどう並ぶかと、コミットに残す記録の形式を確かめるときに読む。

## main のコミット

- 要件の各時点は、`main` ブランチの first-parent の並びにあるコミットである。SHA で指す。
- apply request が 1 件反映されるたびに、`main` に 1 つのコミットができる（[反映の手順](../backend/merge.md)）。
  - 反映は、GitForge の pull request の squash merge である。1 つの apply request の変更は、すべて同じコミットで反映される。
- コミットの順番は、first-parent の並びで決まる。

## 引用で固定するコミット

- 部分木の引用は、ノードの識別子と、`main` のコミットの SHA の組で書く。
  - なぜ：commit SHA は、どのクローンでも同じで、指す中身が変わらないから。
- 引用先の部分木は、その commit の `tree/` から、識別子のディレクトリを探して読む。

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

- trailer の名前（`Logiforge-Version:` など）は、git の履歴に残る永続する形なので、前の呼び名（版、リビジョン）のまま残している。値は、どれもコミットの SHA である。
- `Logiforge-Version:` の数は、反映した先頭のコミットの、先頭のコミットの一覧の中の位置である。API には出さない。
- 最初のコミットは、`Logiforge-Apply-Request:` を持たない。
- 反映によるコミットかは、trailer でなく、GitForge の記録（書き込み主体が merge した pull request か）で決める（[壊れた repository](../backend/broken.md)）。
  - なぜ：`main` を守る設定は求めないので、trailer はだれでも書けるから。
  - 署名は付けない。なぜ：GitForge の記録で決めるので、trailer を偽っても、反映によるコミットにはならないから。

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
