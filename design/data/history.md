# リビジョンとイベントログ

リビジョンの決め方、任意の時点の木の作り方、監査用のイベントログを確かめるときに読む。
満たすべき性質は、[監査](../../docs/architecture/README.md) が正本である。

## リビジョン

- リビジョンは、木が変わるたびに 1 つ増える整数である。
  - 根の主文を作ったときを 1 とする。
  - apply request が 1 件反映されるたびに、1 つ増える。
- 1 つの apply request の操作は、すべて同じリビジョンで反映される。

## 任意の時点の木

リビジョン r の木は、次の行を選んで作る。

- `created_revision ≤ r` の仮説とストーリー
- `added_revision ≤ r` の背景情報
- `submitted_revision ≤ r` で、`superseded_revision` が null か r より大きい検証結果
- inactive かどうかは、`inactivated_revision ≤ r` かで判定する

選んだ行から、値を計算し直す（[真偽値の伝播](../../docs/tree/propagation.md)）。

## イベントログ

監査のために、すべての変化をイベントとして記録する。正本ではない。

| 列 | 型 | 内容 |
|---|---|---|
| `seq` | 整数 | 1 から順に増える番号 |
| `kind` | 文字列 | イベントの種類（例：`story.created`、`apply_request.reviewed`） |
| `subject_id` | 識別子 | 主な対象 |
| `actor_principal_id` | 識別子 | 起こした主体 |
| `actor_session_id` | 文字列 | 起こしたセッション |
| `apply_request_id` | 識別子 | 反映による変化なら、その apply request |
| `revision` | 整数 | 木が変わったなら、そのリビジョン |
| `payload` | JSON | 変化の中身 |
| `occurred_at` | 時刻 | 起きた時刻 |

- 「誰が・いつ・どの apply request で」は、`actor_*`、`occurred_at`、`apply_request_id` で記録する。
- `approve` のイベントの `payload` には、`reviewer_session_id` と `prompt_revision` を含める（[保証の範囲](../../docs/apply-request/review.md)）。
- イベントは、変化と同じトランザクションで加える。

## append-only

- イベントログには、行を加えるだけにする。
  - DB のトリガーで、UPDATE と DELETE を拒む。
- 正本の表も、作った行を変えない（[保存の方式](README.md)）。
  - DB のトリガーで、次の更新だけを許す。
    - `story` の `inactivated_revision` と `reason` を、同時に 1 度だけ埋める。
    - `verdict` の `superseded_revision` を、1 度だけ埋める。
