# 全体構成

forge を作る前に、外から見た構成と、全体が満たすべき性質を確かめるときに読む。

## 構成

```mermaid
flowchart TB
  O["opener"] -- CLI --> S
  R["reviewer"] -- CLI --> S
  U["利用者"] -- WebUI --> S
  subgraph S["forge"]
    T1["木"]
    T2["apply request"]
    T3["人への依頼"]
    F["artifact"]
    L["履歴"]
  end
  S -- webhook --> X["外の仕組み"]
```

- [CLI](../cli/README.md) と [WebUI](../webui/README.md) は、同じデータを扱う。
  - なぜ：エージェントと人間が、同じ規則で同じデータを扱うため。
- forge は、GitHub に近い情報管理基盤である。
  - 構造化された木を、規則付きで管理する。
  - ファイルと artifact を保存する（[artifact](../tree/verdict.md)）。
  - 履歴を残す。
- 作業のループは、forge の外の仕組みで回す（[着手できる作業と通知](../work/README.md)）。

## 監査

- すべての変更を履歴に残す。
- 履歴には、誰が、いつ、どの apply request で変えたかを記録する。
- 履歴は append-only にする。
  - 書き換えも削除もしない。
- ノードごとの履歴を表示できる。
- 任意の時点の木を表示できる。

## 再現性

- 検証の再現に要るものは、[artifact](../tree/verdict.md) と [引用](../tree/verdict.md) の決まりで残す。

## 並行性

- 複数の主体が同時に apply request を出しても、不変条件が常に成り立つ（[反映](../apply-request/README.md)）。

## 権限

- 役割（opener、reviewer、利用者）ごとに、使える操作を分ける（[役割](../roles/README.md)）。

## エクスポート

- データを JSON と Markdown でエクスポートできる。
  - なぜ：forge の外でも、木と根拠を読めるようにするため。
