# 属性の案

データの属性を設計するときに、出発点として使う。
決まりは [仮説とストーリーの木](../docs/tree/README.md) と、そこからリンクしたページが正本である。

## 仮説

| 属性 | 内容 |
|---|---|
| `id` | 識別子 |
| `parent_story_id` | 親ストーリー。根では null |
| `statement` | 主文 |
| `background` | 背景情報 |

値、葉かどうか、inactive かどうか、要再検討かどうかは、保存せずに計算で求める。

## ストーリー

| 属性 | 内容 |
|---|---|
| `id` | 識別子 |
| `parent_hypothesis_id` | 親仮説 |
| `title` | タイトル |
| `summary` | 概要 |
| `state` | `active` または `inactive` |
| `inactive_note` | inactive にした理由（自由記述） |

## 検証結果とレポート

| 対象 | 属性 |
|---|---|
| 検証結果 | `id`、`hypothesis_id`、`value`、`report_id`、`ar_id` |
| レポート | `id`、`body`、`artifact_refs`、`citations` |
| artifact | `id`、`kind`、`sha256`、`uri` |

- `citations` には、artifact か、`{node_id, revision}` を入れる。
- artifact の `kind` は、file、link、citation、program、output、human_entry とする。

## AR、スレッド、履歴

| 対象 | 属性 |
|---|---|
| AR | `id`、`operations`、`opener`、`reviewer`、`state`、`base_revision`、`reviews`、`comments` |
| スレッド | `id`、`kind`、`node_id`、`body`、`replies`、`state` |
| 履歴のイベント | `seq`、`at`、`actor`、`kind`、`subject_id`、`before`、`after`、`ar_id` |
