# レイヤー構成

Logiforge のバックエンドを、どのレイヤーに分けるかを確かめるときに読む。
要件として必要な構成は、[全体構成](../docs/architecture/README.md) が正本である。このページは、その実現のしかたを書く。

## 図

```mermaid
flowchart TB
  subgraph US["利用する主体（クライアントの外）"]
    AGT["エージェント<br/>opener / reviewer"]
    EXT["外の仕組み<br/>論理レビューの起動など"]
    HUM["人<br/>利用者"]
  end
  subgraph CL["Client"]
    CLI["CLI"]
    WEB["WebUI"]
  end
  AGT --> CLI
  EXT --> CLI
  HUM --> WEB
  subgraph AP["Logiforge API（OpenAPI）"]
    API["どのプロバイダでも同じ API"]
  end
  subgraph BE["バックエンド（core）"]
    RULE["ルールはここにだけある<br/>不変条件・反映の条件・値の計算<br/>着手できる作業・作業の終わり・対象ノードの変化"]
    MERGE["main への反映<br/>1 件ずつ直列に行う"]
  end
  subgraph PR["プロバイダ（差し替えられる）"]
    IF["インターフェース<br/>提案と版・レビュー・人への依頼・イベント"]
    GH["GitHub アダプタ<br/>PR とコミット・PR review と check<br/>issue・webhook"]
    NA["独自プロバイダ<br/>自前の DB"]
  end
  subgraph DS["データストア"]
    GIT[("git リポジトリ<br/>木と小さな artifact")]
    CAS[("外部の CAS<br/>大きな artifact")]
    GHS[("GitHub<br/>PR・review・issue")]
    DB[("DB<br/>提案・レビュー・人への依頼")]
  end
  CLI --> API
  WEB --> API
  API --> RULE
  RULE --> MERGE
  RULE --> IF
  MERGE ==>|"main への書き込みは、この 1 本だけ"| IF
  IF ==>|"反映"| GH
  IF ==>|"反映"| NA
  GH ==> GIT
  NA ==> GIT
  GH --> GHS
  NA --> DB
  RULE --> CAS
  GH -. "イベント" .-> RULE
  NA -. "イベント" .-> RULE
```

## レイヤー

- **利用する主体**：クライアントを使う側であり、クライアントではない。
  - エージェント（opener と reviewer）と、外の仕組み（論理レビューの起動など）は、CLI を使う。
  - 人（利用者）は、WebUI を使う。
- **Client**：CLI と WebUI の 2 つ。API だけを使う。
  - クライアントの仕様は書かない。API との対応だけを [クライアントの要件との対応](api/clients.md) に書く。
- **Logiforge API**：どのプロバイダを使っても同じ API。
  - 正本は [openapi.yaml](api/openapi.yaml)。概要は [API](api/README.md) と [API の一覧](api/endpoints/README.md)。
- **バックエンド（core）**：ルールを持つ、ただ 1 つのレイヤー。main への反映も、ここが 1 件ずつ行う。
  - [バックエンド](backend/README.md)
- **プロバイダ**：提案と版、レビュー、人への依頼、イベントを置く場所を、差し替えられるようにするアダプタ。
  - [プロバイダ](providers/README.md)（[GitHub アダプタ](providers/github.md)、[独自プロバイダ](providers/native.md)）
- **データストア**：木は、どのプロバイダでも git リポジトリに置く。
  - 木の置き方は、[木のファイル配置](files/README.md) に書く。

## 2 つの決まり

- ルールはバックエンドにだけある。
  - なぜ：プロバイダを差し替えても、同じルールで判定されるようにするため。
  - プロバイダは、ルールを判定しない。受け取ったものを置き、イベントを伝えるだけである。
- main への書き込みの経路は、バックエンドの反映から、プロバイダの反映を通る 1 本だけである。
  - なぜ：不変条件を確かめずに木が変わることを防ぐため（[反映の手順](backend/merge.md)）。
