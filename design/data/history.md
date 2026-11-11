# 履歴

イベントログ、リビジョン、投影、任意の時点の再現を、どう作るかを確かめるときに読む。
満たすべき性質は、[監査](../../docs/architecture/README.md) が正本である。

## event

| 列 | 型 | 内容 |
|---|---|---|
| `seq` | 整数 | forge 全体で 1 から順に増える番号 |
| `tree_id` | 識別子 | 対象の木。木に関わらないイベントでは null |
| `kind` | 列挙 | イベントの種類 |
| `subject_id` | 識別子 | 主な対象 |
| `actor_principal_id` | 識別子 | 起こした主体 |
| `actor_session_id` | 文字列 | 起こしたセッション |
| `apply_request_id` | 識別子 | 反映による変化なら、その apply request |
| `payload` | JSON | 変化の中身 |
| `occurred_at` | 時刻 | 起きた時刻 |

- 「誰が・いつ・どの apply request で」は、`actor_*`、`occurred_at`、`apply_request_id` で記録する。
- `approve` のイベントの `payload` には、`reviewer_session_id` と `prompt_revision` を含める（[保証の範囲](../../docs/apply-request/review.md)）。

## イベントの種類

| 種類 | 起きるとき |
|---|---|
| `tree.created` | 根の主文が作られた |
| `story.created` | ストーリーと子仮説が作られた |
| `story.inactivated` | ストーリーが inactive になった |
| `verdict.submitted` | 検証結果が出された、または差し替えられた |
| `node.edited` | 背景情報に文章が加わった |
| `apply_request.opened` | apply request が open になった |
| `apply_request.updated` | apply request の操作が変わった |
| `apply_request.reviewed` | レビューが付いた |
| `apply_request.merge_failed` | 反映に失敗した |
| `apply_request.merged` | apply request が反映された |
| `apply_request.closed` | apply request が close された |
| `thread.created` | スレッドが作られた |
| `thread.posted` | スレッドに書き込みがあった |
| `thread.closed` | スレッドが閉じられた |
| `citation.changed` | 引用元が変化した |

1 つの apply request の反映では、操作ごとの木のイベントと `apply_request.merged` を、同じトランザクションで加える。

## リビジョン

- リビジョンは、イベントの `seq` である。
- 木のリビジョンは、その木に最後に反映されたイベントの `seq` である。
- 引用や参照で「リビジョン r の木」と言うときは、`seq ≤ r` の木のイベントだけを再生した状態を指す。

## append-only

- `event` 表には、行を加えるだけにする。
  - DB のトリガーで、`event` 表への UPDATE と DELETE を拒む。
- 投影の表は、イベントから作り直せる。
  - なぜ：投影が壊れても、イベントログから元に戻せるようにするため。

## 任意の時点の再現

- 木ごとに、ある間隔でスナップショットを取る。
  - スナップショットは、その時点の木を JSON にした [blob](files.md) である。
  - `snapshot` 表の列：`tree_id`、`revision`、`blob_sha256`
- リビジョン r の木は、r 以下で最も新しいスナップショットから、r までのイベントを再生して作る。
