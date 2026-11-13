# レイヤー構成

Logiforge のバックエンドを、どのレイヤーに分けたかと、その理由を確かめるときに読む。
要件として必要な構成は、[全体構成](../docs/architecture/README.md) が正本である。このページは、その実現のしかたを書く。

## 図

```mermaid
flowchart TB
  subgraph CL["Client"]
    CLI["CLI"]
    WEB["WebUI"]
  end
  subgraph BE["バックエンド"]
    API["Logiforge API"]
    RULE["ルール判定"]
    PT["ポート"]
  end
  subgraph AD["アダプタ"]
    GHA["GitHub アダプタ"]
    NFA["Forgejo アダプタ"]
    SSA["スナップショットの置き場のアダプタ"]
  end
  subgraph EX["外のサービス"]
    W["worker"]
    GH["GitHub"]
    NF["Forgejo"]
    OBJ["静的ファイルストレージ"]
  end
  CL --> API
  API --> RULE
  RULE --> PT
  PT -.-|実装| AD
  AD --> EX
  W --> GH
  W --> NF
  W --> OBJ
```

- 上から、呼ぶ側から外のサービスの順に並べた。
- 実線の矢印は呼び出しを表す。
- 点線は実装を表す。下のアダプタが、上のポートを実装する。
- エージェントと外の仕組みは CLI を、人（利用者）は WebUI を使う。どれもクライアントではなく、クライアントを使う側である。
- 外のサービスは、構成ごとに使うものが決まる（[組み合わせ](adapters/README.md)）。
- Forgejo を使う構成では、静的ファイルストレージの代わりに、手元のディレクトリを使う。
- [worker](backend/worker.md) は、バックエンドと同じコードの、別のプロセスである。サーバの側で git を読むのは、worker だけである。
  - バックエンドのスナップショットの置き場のアダプタが、worker にブランチの取り込みを頼む。そのため、worker は、バックエンドから見て、外のサービスの段に置いた。
  - worker は、GitHub か Forgejo の git を読み、スナップショットを静的ファイルストレージに書く。git には書かない。
- 外のサービスの段の中の矢印は、worker が読む先と書く先を表す。
- ポートの一覧とアダプタの対応は、[ポート](backend/ports/README.md) に書く。

## レイヤー

- **Client**：CLI と WebUI。バックエンドの API を使う（[クライアントの要件との対応](api/clients.md)）。
  - CLI は、git とセットで使う。木を変えるときは、git でブランチを、GitHub か Forgejo に push する（[CLI](cli/README.md)）。図では、この線を省いた。
- **バックエンド（core）**：外向きのインターフェースが API、内向きのインターフェースがポートである。その間でルールを判定する。
  - API：[API](api/README.md)
  - ルール：[バックエンド](backend/README.md)、反映の流れは [反映の手順](backend/merge.md)
  - ポート：[ポート](backend/ports/README.md)
- **アダプタ**：ポートを実装する（[アダプタ](adapters/README.md)）。
- **外のサービス**：git リポジトリ、pull request とやりとり、主体とトークンを持つ GitHub か Forgejo（[Forgejo アダプタ](adapters/forgejo.md)）。読むための木を置く静的ファイルストレージ。git を読んでスナップショットを作る [worker](backend/worker.md)。

バックエンドはデータを保持しない。保持するのは、外のサービス（正本）、worker（clone）、静的ファイルストレージ（スナップショット）である。

## このレイヤー構造にした理由

ねらいは、GitHub を使う構成と Forgejo を使う構成を、同じ API・同じルールのまま差し替えられることである。

- バックエンドの API を 1 つにする。
  - なぜ：CLI と WebUI が、どちらの構成でも同じ API を使えるようにするため。
- ルール判定は、バックエンドにだけ置く。
  - なぜ：構成を差し替えても、同じルールで判定されるようにするため。
  - アダプタと外のサービスは、ルールを判定しない。
- ポートを置き、依存の向きを、アダプタからポートへの 1 方向にする（ports and adapters）。
  - なぜ：構成を、バックエンドを変えずに差し替えられるようにするため。
- 自前で持つ構成のプロバイダには、Forgejo を使う。GitHub と同じ役割の、別のプロセスのサービスである。
  - なぜ：pull request、レビュー、issue、添付、ブランチの保護、トークンを、Forgejo がすでに持つから。自前で作らずに、GitHub と同じ pull request の形で扱える。
  - なぜ：どちらの構成でも、git リポジトリ・やりとり・主体の正本をバックエンドの外に置けるから。バックエンドの形が、構成によって変わらない。
  - Forgejo のライセンスは GPL である。Logiforge は、Forgejo を別のプロセスとして使い、コードをリンクしない。
- apply request は、プロバイダの pull request に寄せる（[apply request と pull request](backend/apply-request.md)）。
  - 木を変えるのは、利用者が push したブランチと、バックエンドが API で行う pull request の merge である。サーバの側が git に書くのは、merge と repository の用意だけである。
  - なぜ：版、反映、衝突の扱いを、プロバイダの pull request の仕組みにそのまま任せ、バックエンドが git の ref を自前で扱わずに済むようにするため。
  - なぜ git か：木を git リポジトリで管理するという要件（[木の置き場所](../docs/architecture/README.md)）を満たし、変更の履歴と任意の時点の木を、git の仕組みでそのまま扱うため。
- 認証も、ポートにする（[認証](backend/ports/authentication.md)）。
  - なぜ：主体の正本が、GitHub か Forgejo にあり、どちらに問い合わせるかをアダプタの差し替えだけで吸収するため。
- 木を読むときは、git ではなく、worker が書いた [スナップショット](backend/snapshot.md) を、置き場から読む。
  - なぜ：バックエンドの台数と repository の数を掛けた数の clone を持たずに済むから。
  - なぜ：木の読みでは、GitHub の API のレート制限に当たらないから。
  - やりとりは、プロバイダから要求ごとに読む。そのレート制限は、今は考えない（[費用と規模の見積もり](estimates.md)）。
  - スナップショットの置き場をポートにする。複数人の構成では静的ファイルストレージ、1 台の構成では手元のディレクトリに差し替えるため。
- サーバの側で git を読んで解析するのは、worker だけにする。worker は git に書かない。
  - なぜ：clone を持つものを 1 つにし、解析のコードを 1 か所に置くため。書く鍵を持つものも、バックエンドだけにできる。
  - そのかわり、書いたものが読む側に出るのは、worker が追いついてからになる（[書いた直後の読み](api/writes.md)）。
- バックエンドは、データを保持しない（[状態を持たない](backend/README.md)）。
  - なぜ：プロセスを何台並べても、そのまま動くようにするため。
  - 書き込みの取り合いは、ロックではなく、プロバイダの pull request の merge と git の衝突で決まる（[衝突](backend/merge.md)）。
- バックエンドが持つのは、設定と鍵だけである（[設定と秘密情報](adapters/README.md)）。

## 起動時の組み立て

- サーバの起動処理（composition root）が、設定を読む。
- 起動処理は、構成（GitHub を使う / Forgejo を使う）に合ったアダプタを作り、バックエンドのポートに差し込む（依存性の注入）。
  - 構成とアダプタの組み合わせは、[組み合わせ](adapters/README.md) に書く。
  - 読む設定は、[設定と秘密情報](adapters/README.md) に書く。
- 起動処理は、バックエンドの外側に置く。
  - なぜ：バックエンドの中に置くと、バックエンドがアダプタを知ることになり、依存の向きが崩れるから。

依存性の逆転と依存性の注入は、別のことである。

- 依存性の逆転：設計上の依存の向き。アダプタがバックエンドのポートに依存し、バックエンドはアダプタに依存しない。
- 依存性の注入：実行時の組み立て。起動処理が、作ったアダプタをバックエンドに渡す。

## 決まり

- `main` を進めるのは、バックエンドの [反映の手順](backend/merge.md) だけである。
  - なぜ：不変条件を確かめずに木が変わることを防ぐため。
