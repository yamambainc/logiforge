# スナップショットの置き場

バックエンドと worker が、[スナップショット](../snapshot.md) を置き、読むときに使うポートを確かめるために読む。

## 置くもの

| キー | 中身 | 書き方 |
|---|---|---|
| `<repository>/commits/<SHA>` | スナップショットのコミットの記録 | 1 度だけ書く。上書きも削除もしない |
| `<repository>/snapshots/<SHA>` | スナップショットの木 | 1 度だけ書く。上書きも削除もしない |
| `<repository>/artifacts/<sha256>` | artifact のファイルの中身 | 1 度だけ書く。上書きも削除もしない |
| `<repository>/latest` | 今の `main` の SHA と、force push の記録 | 上書きする |
| `<repository>/refs` と `repositories` | [ref の一覧](../snapshot.md) | 上書きする |

## 操作

| 操作 | 内容 | 使う者 |
|---|---|---|
| 読む | キーの中身を返す。なければ、ないことを返す | バックエンドと worker |
| 書く | キーに中身を書く | worker だけ |

- 書くのは worker だけなので、書き込みがぶつかることは、ふつうは起きない（[worker](../worker.md)）。
- worker が誤って 2 つ動いても、`latest` は、[latest](../snapshot.md) の規則（親をたどった先が今の `latest` のときだけ進める）で、古い SHA に戻らない。
  - 置き場が条件付きの書き込み（今の値が期待したものと同じときだけ書く）を持つなら、`latest` はそれで書く。2 つの worker の読んでから書くまでの間の重なりも防げる。
- SHA と sha256 のキーは、中身から決まる。同じキーに書き直しても、中身は変わらない。

アダプタは、[スナップショットの置き場のアダプタ](../../adapters/snapshot-store.md) に書く。
