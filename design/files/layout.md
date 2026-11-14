# ファイルの配置

木の各ノードを、どのディレクトリとファイルで表すかを確かめるときに読む。
親子関係をパスで表す理由は、[親子関係の持ち方](parent-child.md) に書く。

## ディレクトリ

```
tree/
  h-7k3m9q2x4p/                      根の仮説
    hypothesis.md                    主文
    background.md                    背景情報
    verdicts/
      v-2c8d0f6t1n.md                検証結果とレポート
    s-9w4b1r7e3k/                    子ストーリー
      story.md                       タイトルと概要
      inactivated.md                 inactive にした理由（inactive のときだけ）
      h-5j2n8c0y6m/                  子仮説
        hypothesis.md
        ...
artifacts/
  sha256/<先頭 2 文字>/<残りの 62 文字>   artifact のファイル（[artifact の置き方](artifacts.md)）
```

- 仮説は `h-`、ストーリーは `s-`、検証結果は `v-` で始まるディレクトリかファイルにする。
- 仮説のディレクトリの子は、ストーリーのディレクトリだけにする。
- ストーリーのディレクトリの子は、仮説のディレクトリだけにする。

## 識別子

- 識別子は、種類の接頭辞と、10 文字の小文字の英数字（Crockford の base32）をつなぐ。
  - 例：`h-7k3m9q2x4p`
- 識別子は、ノードを作るときに、クライアント（CLI のひな形）が乱数で決める（[CLI](../cli/README.md)）。
  - ほかのノードとぶつかったら、[差分の検査](../backend/checks.md) で拒む。
- 識別子は、ディレクトリ名とファイル名にそのまま使う。
  - なぜ：パスを短く保つため。10 文字なら 50 ビットあり、1 つの木の中で重なることはまずない。

## ファイルの中身

各ファイルは、YAML の front matter と、Markdown の本文からなる。

| ファイル | front matter | 本文 |
|---|---|---|
| `hypothesis.md` | `id`、`statement` | なし |
| `background.md` | なし | 背景情報 |
| `story.md` | `id`、`title`、`summary` | なし |
| `inactivated.md` | なし | inactive にした理由 |
| `verdicts/v-*.md` | `id`、`value`、`supersedes`、`artifacts`、`citations` | レポート |

- `supersedes` は、差し替える前の検証結果の識別子の一覧である。最初の検証結果では書かない。
  - 1 つだけ差し替えるときも、一覧で書く（例：`supersedes: [v-7k3m9q2x4p]`）。
  - 有効な検証結果が 2 つ以上ある仮説では、それらをまとめて差し替えられる（[差分の検査](../backend/checks.md)）。
- `artifacts` は、レポートが参照する artifact の一覧である（[artifact の置き方](artifacts.md)）。
- `citations` は、引用の一覧である。
  - artifact の引用は、`sha256` で書く。
  - 部分木の引用は、ノードの識別子と、`main` のコミットの SHA（キー `commit`）の組で書く（[引用で固定するコミット](revisions.md#引用で固定するコミット)）。
  - `commit` の代わりに `revision` と書いた引用も読む。なぜ：git の履歴にある検証結果のファイルを書き換えずに読めるようにするため。

## 有効な検証結果

- ある仮説の有効な検証結果は、ほかの検証結果の `supersedes` に書かれていないものである。
- 有効な検証結果は、1 つの仮説に 1 つまでにする（[差分の検査](../backend/checks.md)）。

## 保存しないもの

- 値（真偽）
- 葉、要再検討、inactive かどうか
  - inactive かどうかは、自分か祖先のストーリーに `inactivated.md` があるかで判定する。
- 引用元が変化したかどうか

これらは、バックエンドがファイルから計算する。
