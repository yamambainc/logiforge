# 人への依頼

人への依頼と、その回答を、どの列で持つかを確かめるときに読む。
決まりは、[人への依頼](../../docs/human-requests/README.md) が正本である。

## human_request

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `hrq_` で始まる |
| `node_id` | 識別子 | 対象のノード |
| `kind` | `pvr` / `decision` | 種類 |
| `body` | 文字列 | 本文（検証手順、または問い） |
| `created_by_session_id` | 文字列 | 起票したセッション |
| `created_at` | 時刻 | 起票した時刻 |

## human_request_answer

| 列 | 型 | 内容 |
|---|---|---|
| `human_request_id` | 識別子 | 回答した依頼。主キー |
| `body` | 文字列 | 回答の本文 |
| `artifact_id` | 識別子 | 回答を artifact にしたもの（`kind = human_entry`） |
| `answered_by_principal_id` | 識別子 | 回答した利用者 |
| `answered_at` | 時刻 | 回答した時刻 |

- `human_request_id` を主キーにして、回答を 1 回だけにする。
- 依頼の状態は、保存せずに計算する。
  - 回答の行があれば `answered`、なければ `open` である。
  - なぜ：状態の列を持つと、回答の行と食い違わないように更新する必要が出るから。
- 回答の本文は、[blob](files.md) に置き、`human_entry` の artifact の中身にする。
  - なぜ：回答を、検証結果のレポートから引用できるようにするため。
- 添付は、`human_request_attachment` 表で artifact とつなぐ。
  - 列：`human_request_id`、`artifact_id`
