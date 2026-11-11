# apply request

apply request、操作、レビューを、どの列で持ち、どう反映するかを確かめるときに読む。
決まりは、[apply request](../../docs/apply-request/README.md) と [論理レビュー](../../docs/apply-request/review.md) が正本である。

## apply_request

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `apr_` で始まる |
| `tree_id` | 識別子 | 対象の木 |
| `opener_principal_id` | 識別子 | opener の主体（[認証](../api/README.md)） |
| `opener_session_id` | 文字列 | opener のセッション |
| `description` | 文字列 | 説明欄。レビューの材料には含めない |
| `state` | `open` / `merged` / `closed` | 状態 |
| `version` | 整数 | 差分の版。操作が変わるたびに 1 増える |
| `closed_reason` | 文字列 | `closed` にした理由 |
| `merged_revision` | 整数 | 反映されたイベントの番号 |

## operation

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `opr_` で始まる |
| `apply_request_id` | 識別子 | 属する apply request |
| `position` | 整数 | 適用する順 |
| `kind` | 列挙 | `story.create`、`story.inactivate`、`verdict.submit`、`node.edit` |
| `target_id` | 識別子 | 対象ノード。同じ apply request で作るノードなら null |
| `target_ref` | 文字列 | 同じ apply request で作るノードを指す名前 |
| `base_revision` | 整数 | 操作を加えたときの、対象ノードの `last_changed_revision` |
| `payload` | JSON | 操作ごとの中身（[API](../api/openapi.yaml) の `Operation`） |

- `target_ref` は、`story.create` で作る子仮説に付けた名前を指す。
  - なぜ：流用では、同じ apply request で作った子仮説に検証結果を出すから（[流用](../../docs/tree/verdict.md)）。

## review

| 列 | 型 | 内容 |
|---|---|---|
| `id` | 識別子 | `rev_` で始まる |
| `apply_request_id` | 識別子 | 対象の apply request |
| `version` | 整数 | レビューした差分の版 |
| `kind` | `approve` / `request_changes` / `comment` | 種類 |
| `body` | 文字列 | 本文 |
| `reviewer_principal_id` | 識別子 | reviewer の主体 |
| `reviewer_session_id` | 文字列 | reviewer のセッション |
| `prompt_revision` | 文字列 | 使ったプロンプトのリビジョン |

- `reviewer_session_id` が `opener_session_id` と同じレビューは、受け付けない。
- `approve` は、`version` が apply request の今の `version` と同じときだけ有効である。

## merge_failure

| 列 | 型 | 内容 |
|---|---|---|
| `apply_request_id` | 識別子 | 反映に失敗した apply request |
| `version` | 整数 | 失敗した差分の版 |
| `reason` | `stale_target` / `invariant_violation` | 理由 |
| `detail` | 文字列 | 失敗した操作と条件 |

## 反映の判定

`approve` を受け付けたら、次の順に処理する。全体を 1 つのトランザクションで行う。

1. 今の `version` に対するレビューのうち、最後のものが `approve` かを確かめる。そうでなければ、何もしない。
2. 各操作の `base_revision` が、対象ノードの今の `last_changed_revision` と同じかを確かめる。
   - 違えば、`stale_target` として `merge_failure` に記録し、`open` のままにする。
3. 操作を `position` の順に当てはめ、当てはめたあとの状態で[不変条件](invariants.md)を確かめる。
   - 満たさなければ、`invariant_violation` として記録し、`open` のままにする。
4. イベントを[イベントログ](history.md)に加え、投影を更新し、値を計算し直す。
5. `state` を `merged` にし、`merged_revision` を埋める。

- 最後のレビューより新しい `merge_failure` がある apply request を、修正待ちとする（[着手できる作業](../../docs/work/README.md)）。
- 反映は、forge 全体で 1 件ずつ順に行う。
  - なぜ：不変条件の確認と反映の間に、別の反映が割り込まないようにするため。
