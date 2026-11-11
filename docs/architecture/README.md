# 全体構成

ハーネスを作る前に、構成要素と、全体が満たすべき性質を確かめるときに読む。

## 構成要素

```mermaid
flowchart TB
  B["Builder"] -- CLI --> API
  R["Reviewer"] -- CLI --> API
  O["owner"] -- WebUI --> API
  subgraph API["ハーネスの API"]
    V["validator"]
    AR["AR エンジン"]
    P["伝播エンジン"]
    S["スケジューラ"]
    N["通知"]
  end
  API --> Store
  subgraph Store["ストア"]
    T["構造化された木、AR、スレッド"]
    F["ファイルと artifact"]
    L["履歴"]
  end
```

- [CLI](../cli/README.md) と [WebUI](../webui/README.md) は、同じ API を使う。
  - なぜ：エージェントと人間が、同じ規則で同じデータを扱うため。
- ストアは、GitHub に近い情報管理基盤である。
  - 構造化された木を、規則付きで管理する。
  - ファイルと artifact を、変更できない形で保存する。
  - 履歴を、加える一方で残す。

各機能の役割は、[ハーネスの機能](../agents/README.md) に書く。

## 監査

- すべての変更を履歴に残す。
  - 誰が、いつ、どの AR で変えたかを記録する。
- 履歴は、加える一方で、書き換えも削除もしない。
- ノードごとの履歴を表示できる。
- 任意の時点の木を表示できる。

## 再現性

- artifact は、中身のハッシュで識別し、変更しない（[artifact](../tree/verdict.md)）。
- 引用は、リビジョンを固定して記録する。

## 並行性

- 複数の Builder と Reviewer が同時に動いても、不変条件が常に成り立つ。
- 担当が途中で止まったタスクは、別の担当に回す（[スケジューラ](../agents/scheduler.md)）。

## 権限

- 役割（Builder、Reviewer、owner）ごとに、使える操作を分ける。
  - 各役割の操作は、[役割](../agents/README.md) に書く。

## エクスポート

- データを JSON と Markdown でエクスポートできる。
  - なぜ：ハーネスの外でも、木と根拠を読めるようにするため。
