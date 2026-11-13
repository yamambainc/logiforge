# スナップショットの置き場

バックエンドが、[スナップショット](../snapshot.md) を読むときに使うポートを確かめるために読む。

## 置いてあるもの

| キー | 中身 | 変わるか |
|---|---|---|
| `<repository>/commits/<SHA>` | スナップショットのコミットの記録 | 変わらない |
| `<repository>/snapshots/<SHA>` | スナップショットの木 | 変わらない |
| `<repository>/artifacts/<sha256>` | artifact のファイルの中身 | 変わらない |
| `<repository>/latest` | 今の `main` の SHA、ref の一覧、force push の記録（[latest](../snapshot.md)） | 変わる |
| `repositories` | `latest` がある repository の名前の一覧 | 変わる |

## 操作

| 操作 | 内容 |
|---|---|
| 読む | キーの中身を返す。なければ、ないことを返す |

- バックエンドは、読むだけである。
- 書くのは [worker](../worker.md) だけで、worker の中の別のインターフェースで書く。このポートには、書く操作を置かない。
  - なぜ：バックエンドが置き場に書かないことを、ポートの形で守るため。

アダプタは、[スナップショットの置き場のアダプタ](../../adapters/snapshot-store.md) に書く。
