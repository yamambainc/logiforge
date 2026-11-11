# 検証結果と根拠

検証結果、レポート、artifact、引用を、どの列で持つかを確かめるときに読む。
決まりは、[検証結果と引用](../../docs/tree/verdict.md) が正本である。

## verdict

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `ver_` で始まる |
| `hypothesis_id` | 識別子 | 対象の仮説 |
| `value` | true / false | 値 |
| `report_id` | 識別子 | レポート。一意で、null を許さない |
| `apply_request_id` | 識別子 | この検証結果を出した apply request |
| `submitted_revision` | 整数 | 反映されたイベントの番号 |
| `superseded_revision` | 整数 | 差し替えられたイベントの番号。有効な間は null |

- 有効な検証結果は、`superseded_revision` が null のものである。
  - 1 つの仮説に、有効な検証結果は 1 件までにする（部分一意の制約）。
- `report_id` を一意にして、検証結果とレポートを 1 対 1 にする。
- 差し替えでは、古い行を消さずに `superseded_revision` を埋める。

## report

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `rep_` で始まる |
| `body_sha256` | 文字列 | 本文の [blob](files.md) |

- レポートは、作ったあと変えない。
  - 検証結果を差し替えるときは、新しいレポートを作る。

## artifact

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `art_` で始まる |
| `kind` | 列挙 | `file`、`link`、`citation`、`program`、`output`、`human_entry` |
| `sha256` | 文字列 | 中身の [blob](files.md)。`link` のときは null |
| `uri` | 文字列 | `link` のときの参照先 |
| `media_type` | 文字列 | 中身の形式 |
| `created_by_session_id` | 文字列 | 登録したセッション |

- artifact は、作ったあと変えない。
- `report_artifact` 表で、レポートと artifact を多対多につなぐ。

## citation

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `cit_` で始まる |
| `report_id` | 識別子 | 引用しているレポート |
| `target_kind` | `artifact` / `subtree` | 引用の種類 |
| `artifact_id` | 識別子 | `artifact` のときの引用先 |
| `node_id` | 識別子 | `subtree` のときの部分木の根 |
| `revision` | 整数 | `subtree` のときの、固定したリビジョン |

- リビジョンを固定した部分木は、[任意の時点の再現](history.md) で読み出す。
- 流用は、`subtree` の引用を 1 件だけ持つレポートで表す。専用の列は持たない。

## 引用元の変化の検出

- 反映のたびに、変わったノードの祖先をたどる。
- 祖先のどれかを `node_id` に持つ `subtree` の引用があれば、その引用は「引用元が変化した」とする。
  - 対象の変化は、検証結果の差し替えと、ストーリーの inactive 化である（[引用元の変化](../../docs/tree/verdict.md)）。
- 検出した引用は、`citation_change` 表に記録する。
  - 列：`citation_id`、`detected_revision`、`resolved_revision`
  - 引用しているレポートの検証結果が差し替えられたら、`resolved_revision` を埋める。
