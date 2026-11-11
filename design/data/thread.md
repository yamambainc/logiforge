# スレッド

スレッドと書き込みを、どの列で持つかを確かめるときに読む。
決まりは、[スレッド](../../docs/threads/README.md) が正本である。

## thread

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `thr_` で始まる |
| `tree_id` | 識別子 | 属する木 |
| `kind` | `request` / `pvr` | 種類 |
| `node_id` | 識別子 | 対象のノード。なければ null |
| `state` | `open` / `closed` | 状態 |
| `last_post_author_kind` | `user` / `client` | 最後の書き込みの主体の種類 |

- 返信待ちは、`state` と `last_post_author_kind` で判定する。
  - `user` なら、opener の返信待ちである。
  - `client` なら、利用者の返信待ちである。
- 主体の種類（`user` と `client`）は、[認証](../api/README.md) に書く。

## post

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `pst_` で始まる |
| `thread_id` | 識別子 | 属するスレッド |
| `author_principal_id` | 識別子 | 書いた主体 |
| `author_kind` | `user` / `client` | 書いた主体の種類 |
| `session_id` | 文字列 | 書いたセッション。`client` のときだけ |
| `body_sha256` | 文字列 | 本文の [blob](files.md) |
| `artifact_id` | 識別子 | 本文を artifact として登録したもの |

- 書き込みは、作ったあと変えない。
- 添付は、`post_attachment` 表で artifact とつなぐ。
- PVR に利用者が書いた本文は、`kind = human_entry` の artifact としても登録する。
  - 本文の blob は、書き込みと artifact で共有する。
  - なぜ：記入を、検証結果のレポートから引用できるようにするため（[PVR](../../docs/threads/README.md)）。
