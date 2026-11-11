# スレッド

スレッドと書き込みを、どの列で持つかを確かめるときに読む。
決まりは、[スレッド](../../docs/threads/README.md) が正本である。

## thread

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `thr_` で始まる |
| `kind` | `request` / `pvr` | 種類 |
| `node_id` | 識別子 | 対象のノード。なければ null |
| `state` | `open` / `closed` | 状態 |

- 返信待ちは、最後の書き込みの `author_kind` で判定する。
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
| `body` | 文字列 | 本文 |
| `artifact_id` | 識別子 | PVR への記入を artifact にしたもの |

- 書き込みは、作ったあと変えない。
- 添付は、`post_attachment` 表で artifact とつなぐ。
- PVR に利用者が書いた本文は、`kind = human_entry` の artifact としても登録する。
  - 本文を [blob](files.md) に置き、その artifact の中身にする。
  - なぜ：記入を、検証結果のレポートから引用できるようにするため（[PVR](../../docs/threads/README.md)）。
