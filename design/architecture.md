# レイヤー構成

Logiforge のバックエンドを、どのレイヤーとポートに分けるかを確かめるときに読む。
要件として必要な構成は、[全体構成](../docs/architecture/README.md) が正本である。このページは、その実現のしかたを書く。

## 図

```mermaid
flowchart TB
  subgraph US["利用する主体"]
    AGT["エージェント"]
    EXT["外の仕組み"]
    HUM["人"]
  end
  subgraph CL["Client"]
    CLI["CLI"]
    WEB["WebUI"]
  end
  subgraph AP["Logiforge API"]
    API["API"]
  end
  subgraph BE["バックエンド"]
    RULE["ルール判定"]
    MERGE["反映"]
    subgraph PT["ポート"]
      P1["木の読み書き"]
      P2["やりとり"]
      P3["サーバの状態"]
      P4["ファイルの保管"]
    end
  end
  subgraph AD["アダプタ"]
    GITR["git アダプタ（GitHub）"]
    GITL["git アダプタ（bare）"]
    GH["GitHub アダプタ"]
    NA["独自プロバイダ"]
    SQ["SQLite アダプタ"]
    CA["CAS アダプタ"]
  end
  subgraph DS["データストア"]
    GHR[("git リポジトリ（GitHub）")]
    BARE[("git リポジトリ（bare）")]
    GHS[("GitHub の PR と issue")]
    DB[("SQLite ファイル")]
    OBJ[("CAS（ローカル / S3 互換）")]
  end
  AGT --> CLI
  EXT --> CLI
  HUM --> WEB
  CLI --> API
  WEB --> API
  API --> RULE
  RULE --> MERGE
  RULE --> P1
  RULE --> P2
  RULE --> P3
  RULE --> P4
  MERGE --> P1
  MERGE --> P2
  MERGE --> P3
  GITR -.-> P1
  GITL -.-> P1
  GH -.-> P2
  NA -.-> P2
  SQ -.-> P3
  CA -.-> P4
  GITR ==> GHR
  GITL ==> BARE
  GH ==> GHS
  NA ==> DB
  SQ ==> DB
  CA ==> OBJ
  GH -->|イベント| P2
  NA -->|イベント| P2
```

バックエンドの中にあるのはポート（何をしてほしいかの約束）だけで、実体はアダプタとデータストアにある。

矢印は 3 種類である。

- `-->`：呼び出し
- `-.->`：実装（アダプタがポートを実装する）
- `==>`：データの読み書き

## レイヤー

- **利用する主体**：エージェントと外の仕組みは CLI を、人は WebUI を使う。クライアントではない。
- **Client**：CLI と WebUI。API だけを使う（[クライアントの要件との対応](api/clients.md)）。
- **Logiforge API**：どのアダプタでも同じ API（[API](api/README.md)）。
- **バックエンド（core）**：ルールと 4 つのポートを持ち、複数の repository を扱う（[バックエンド](backend/README.md)、[ポート](backend/ports/README.md)）。
- **アダプタ**：ポートを実装する。やりとりのアダプタをプロバイダと呼ぶ（[アダプタ](adapters/README.md)）。
- **データストア**：アダプタが読み書きする先。サーバの状態と独自プロバイダは、同じ SQLite ファイルに置く（[SQLite アダプタ](adapters/sqlite.md)）。
  - CAS（内容アドレス方式のストレージ）は、ファイルの保管の実体である（[CAS アダプタ](adapters/cas.md)）。

## 決まり

- 依存の向きは、アダプタからバックエンドのポートへの 1 方向である（ports and adapters）。
  - なぜ：アダプタを差し替えても、バックエンドを変えずに済むようにするため。
- ルールはバックエンドにだけある。
  - なぜ：アダプタを差し替えても、同じルールで判定されるようにするため。
  - アダプタは、ルールを判定しない。
- 木の読み書きのポートは、git の形式で実装する（[木のファイル配置](files/README.md)）。
  - なぜ：変更の履歴、差分、任意の時点の木を、git の仕組みでそのまま扱えるから。
- `main` を進めるのは、バックエンドの [反映の手順](backend/merge.md) だけである。
  - なぜ：不変条件を確かめずに木が変わることを防ぐため。
