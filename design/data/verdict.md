# 検証結果と根拠

検証結果（レポートを含む）、artifact、引用を、どの列で持つかを確かめるときに読む。
決まりは、[検証結果と引用](../../docs/tree/verdict.md) が正本である。

## verdict

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `ver_` で始まる |
| `hypothesis_id` | 識別子 | 対象の仮説 |
| `value` | true / false | 値 |
| `report_body` | 文字列 | レポートの本文 |
| `apply_request_id` | 識別子 | この検証結果を出した apply request |
| `submitted_revision` | 整数 | 反映されたリビジョン |
| `superseded_revision` | 整数 | 差し替えられたリビジョン。有効な間は null |

- 1 行が、値とレポートの 1 対 1 の組を表す。
  - なぜ：レポートを別の表にしなくても、1 対 1 は 1 行で表せるから。
- 有効な検証結果は、`superseded_revision` が null のものである。
  - 1 つの仮説に、有効な検証結果は 1 件までにする（部分一意の制約）。
- 差し替えでは、古い行の `superseded_revision` を埋め、新しい行を足す。

## verdict_artifact

| 列 | 型 | 内容 |
|---|---|---|
| `verdict_id` | 識別子 | 検証結果 |
| `artifact_id` | 識別子 | レポートが参照する artifact |

## artifact

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `art_` で始まる |
| `kind` | 列挙 | `file`、`link`、`reference`、`program`、`output`、`human_entry` |
| `sha256` | 文字列 | 中身の [blob](files.md)。`link` のときは null |
| `uri` | 文字列 | `link` のときの参照先 |
| `media_type` | 文字列 | 中身の形式 |
| `created_by_session_id` | 文字列 | 登録したセッション |

- artifact は、作ったあと変えない。
- `reference` は、参考文献の書誌情報と引用した箇所を表す。

## citation

| 列 | 型 | 内容 |
|---|---|---|
| `verdict_id` | 識別子 | 引用しているレポートの検証結果 |
| `target_kind` | `artifact` / `subtree` | 引用の種類 |
| `artifact_id` | 識別子 | `artifact` のときの引用先 |
| `node_id` | 識別子 | `subtree` のときの部分木の根 |
| `revision` | 整数 | `subtree` のときの、固定したリビジョン |

- 流用は、`subtree` の引用を 1 件だけ持つ検証結果で表す。専用の列は持たない。

## 引用元の変化の判定

引用元の変化は、保存せずに問い合わせのときに判定する。

- 有効な検証結果の、`subtree` の引用を対象にする。
- 引用した部分木（根の `node_id` 自身を含む）で、`revision` より後に次のことが起きていれば、引用元が変化している。
  - 検証結果の差し替え（`superseded_revision` が `revision` より後）
  - ストーリーの inactive 化（`inactivated_revision` が `revision` より後）
- 引用している検証結果を出し直すと、古い行が差し替えられるので、変化は解消する（[引用元の変化](../../docs/tree/verdict.md)）。
