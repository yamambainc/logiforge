# apply request と履歴の画面

利用者が、木がどう変わってきたかを確かめるときに使う画面。
apply request の決まりは、[apply request](../apply-request/README.md) に書く。

## ワイヤフレーム

```
+-----------------------------------------------------------------+
| apply request と履歴    状態: [open ▼]     時点: [最新 ▼]       |
+------------------------------+----------------------------------+
| S2 の作成            open    | 差分                             |
| H2 の検証            merged  |  + story.create   S2 ストーリー2 |
| S0 の inactive 化    merged  |  + verdict.submit H3 true        |
|                              | レビュー                         |
|                              |  reviewer: request_changes ...   |
|                              |  opener:   修正しました ...      |
+------------------------------+----------------------------------+
```

## apply request の閲覧

- apply request を、状態（open、merged、closed）で絞り込める。
- 選んだ apply request について、次のものを示す。
  - 差分
  - レビュー
  - コメント
- WebUI からは、apply request を審査しない（[反映の条件](../apply-request/review.md)）。

## 過去の時点の木

- 時点を選ぶと、その時点の木を、[木のビューア](tree-viewer.md) と同じ形で示す。
