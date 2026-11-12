# リビジョンとコミット

要件のリビジョンを git のコミットにどう対応づけるかと、コミットに残す記録の形式を確かめるときに読む。

## 対応

- リビジョンは、`main` ブランチの first-parent の並びにあるコミットである。
- apply request が 1 件反映されるたびに、`main` に 1 つのコミットができる（[反映の手順](../backend/merge.md)）。
  - 1 つの apply request の操作は、すべて同じコミットで反映される。
- リビジョンの順番は、first-parent の並びで決まる。
  - 数で表したいときは、根のコミットからの first-parent の数を使う。

## 引用で固定するリビジョン

- 部分木の引用は、ノードの識別子と、`main` の commit SHA の組で書く。
  - なぜ：commit SHA は、どのクローンでも同じで、指す中身が変わらないから。
- 引用先の部分木は、その commit の `tree/` から、識別子のディレクトリを探して読む。

## 任意の時点の木

- リビジョン r の木は、commit r を checkout したときの `tree/` である。
- 値は、その `tree/` からバックエンドが計算し直す（[真偽値の伝播](../../docs/tree/propagation.md)）。
- そのため、時点ごとのスナップショットや、別の履歴の表は要らない。

## 記録の形式

バックエンドが作るコミットには、メッセージの末尾に次の trailer を書く。

| trailer | 書くコミット | 内容 |
|---|---|---|
| `Logiforge-Session-Id:` | 版、反映 | 差分を書いたセッション |
| `Logiforge-Apply-Request:` | 版、反映 | apply request の識別子 |
| `Logiforge-Signature:` | 版 | バックエンドの署名 |

- `Logiforge-Signature:` は、コミットの tree、親、ほかの trailer に対する HMAC-SHA256 である。
  - 鍵は、サーバだけが持つ（[設定と秘密情報](../adapters/README.md)）。
  - なぜ：どのアダプタを使っても、版がバックエンドの作ったものかを、同じ方法で確かめられるようにするため。
- 反映のコミットには、反映したすべての版のセッションを、`Logiforge-Session-Id:` に並べて書く。

## 誰が、いつ、どの apply request で

- 誰が：`Logiforge-Session-Id:` と、apply request を出したセッション
- いつ：コミットの時刻
- どの apply request で：`Logiforge-Apply-Request:`

レビューの記録は、[やりとり](../backend/ports/interaction.md) に置く。
