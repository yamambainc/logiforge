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
- そのため、時点ごとのスナップショットや、別の履歴のテーブルは要らない。

## 記録の形式

worker が作るコミットと、版の記録（タグ）には、メッセージの末尾に次の trailer を書く。

| trailer | 書くもの | 内容 |
|---|---|---|
| `Logiforge-Session-Id:` | 版の記録、反映 | 差分を取り込んだ API の呼び出しのセッション |
| `Logiforge-Apply-Request:` | 版の記録、反映 | apply request の識別子 |
| `Logiforge-Version:` | 版の記録、反映 | 版の番号。反映では、版の番号と、版のコミット（例：`3 <commit SHA>`） |
| `Logiforge-Base:` | 版の記録 | 版の差分の base（[版と ref](../backend/apply-request.md)） |
| `Logiforge-Human-Request:` | 回答 | 人への依頼の識別子 |
| `Logiforge-Signature:` | 最初のリビジョン、版の記録、反映、回答 | worker の署名 |

- 版の記録は、利用者のコミットを指す、注釈付きのタグである（[版の記録](../backend/apply-request.md)）。利用者のコミットには、何も書き足さない。
- 利用者のコミットのメッセージにある trailer は、記録として扱わない。
- `Logiforge-Signature:` は、コミットの tree と親（タグでは、指すコミット）、ほかの trailer に対する HMAC-SHA256（鍵付きのハッシュによる署名）である。
  - 鍵は、worker だけが持つ（[設定と秘密情報](../adapters/README.md)）。署名を付けるのも、確かめるのも worker である。git に書くのは worker だけだからである。
  - なぜ：どのアダプタを使っても、版と回答がバックエンドの作ったものかを、同じ方法で確かめられるようにするため。
- 反映のコミットには、反映したすべての版のセッションを、`Logiforge-Session-Id:` に並べて書く。
- 最初のリビジョンは、`Logiforge-Apply-Request:` を持たない。突き合わせでは、用意によるものとして扱う。
- `main` の first-parent の並びのうち、`Logiforge-Signature:` のない、または正しくないコミットは、反映によらない変更とみなす。

## 誰が、いつ、どの apply request で

- 誰が：反映のコミットと、版の記録の `Logiforge-Session-Id:`
- いつ：コミットの時刻
- どの apply request で：`Logiforge-Apply-Request:`

レビューの記録は、[やりとり](../backend/ports/interaction.md) に置く。
