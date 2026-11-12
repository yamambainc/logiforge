# リビジョンとコミット

要件のリビジョンを、git のコミットにどう対応づけるかを確かめるときに読む。

## 対応

- リビジョンは、`main` ブランチの first-parent の並びにあるコミットである。
- apply request が 1 件反映されるたびに、`main` に 1 つのコミットができる。
  - squash merge か merge commit のどちらでもよい。
  - 1 つの apply request の操作は、すべて同じコミットで反映される。
- リビジョンの順番は、first-parent の並びで決まる。
  - 数で表したいときは、根のコミットからの first-parent の数を使う。

## 引用で固定するリビジョン

- 部分木の引用は、ノードの識別子と、`main` の commit SHA の組で書く。
  - なぜ：commit SHA は、どのクローンでも同じで、指す中身が変わらないから。
- 引用先の部分木は、その commit の `tree/` から、識別子のディレクトリを探して読む。

## 任意の時点の木

- リビジョン r の木は、commit r を checkout したときの `tree/` である。
- 値は、その `tree/` から バックエンドが計算し直す（[真偽値の伝播](../../docs/tree/propagation.md)）。
- そのため、時点ごとのスナップショットや、別の履歴の表は要らない。

## 誰が、いつ、どの apply request で

- 誰が：コミットの author と、apply request を出したセッション
- いつ：コミットの時刻
- どの apply request で：コミットメッセージに、apply request の識別子を書く

版を書いたセッションはコミットの trailer に残る。レビューの記録は、プロバイダが持つ（[プロバイダ](../providers/README.md)）。
