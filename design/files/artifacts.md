# artifact の置き方

artifact の中身を、リポジトリに置くか外に置くかを確かめるときに読む。
artifact の決まりは、[artifact](../../docs/tree/verdict.md) が正本である。

## 置き場所

- 中身が 1 MiB 以下の artifact は、リポジトリの `artifacts/sha256/` に置く。
  - なぜ：レポートが参照するコードや小さな実行結果は、木と一緒にクローンできるほうが確かめやすいから。
- 1 MiB を超える artifact は、外部の CAS に置き、リポジトリにはハッシュだけを書く。
  - なぜ：大きなデータや画像を git に入れると、クローンが遅くなるから。
  - 外部の CAS の場所は、`logiforge.yaml` に書く。
- 1 MiB の境目は、`logiforge.yaml` で変えられる。
- リンクの artifact は、中身を置かず、URI だけを書く。

## 検証結果からの参照

検証結果の `artifacts` の各項目は、次のものを持つ。

| 項目 | 内容 |
|---|---|
| `kind` | `file`、`link`、`reference`、`program`、`output`、`human_entry` |
| `sha256` | 中身のハッシュ。`link` のときはない |
| `uri` | `link` のときの参照先 |
| `media_type` | 中身の形式 |

- 中身は、リポジトリにあればそれを、なければ外部の CAS を、ハッシュで探す。
- 人への依頼の回答は、`human_entry` の artifact として、それを引用する apply request に含める（[人への依頼](../../docs/human-requests/README.md)）。

## 変えないこと

- `artifacts/sha256/` のファイルは、ファイル名が中身のハッシュなので、中身を変えると名前と合わなくなる。
- バックエンドは、ファイル名と中身のハッシュが一致するかを確かめる（[差分の検査](../backend/checks.md)）。
- 外部の CAS も、同じハッシュに別の中身を置かない。
