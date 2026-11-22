# GitHub での main の守りと制約

GitHub を使う構成で、`main` への書き込みをどう守り、GitHub の API をどう使い、GitHub の制約にどう対処するかを確かめるときに読む。
GitHub アダプタそのものは、[GitHub アダプタ](github.md) に書く。

## main への書き込みを守る

- 利用者とエージェントは、GitHub リポジトリに、apply request のもとになるブランチを push する（[CLI](../cli/README.md)）。
- `main` を守る設定（ルールセットかブランチ保護）は、勧めるが求めない（[main を守る](pull-requests.md#main-を守る)）。使うときは、リポジトリの admin が用意する。
  - ルールセットで、`main` に「Require a pull request before merging」「Block force pushes」「Restrict deletions」を付ける。bypass には、だれも入れない。
  - 「Require a pull request before merging」では、承認の数を 1 以上にし、新しい push で古い approve を外す（Dismiss stale pull request approvals）。外さなければ、push のあとも reviewDecision は APPROVED のままで、Logiforge もそれに従う。GitHub の画面からの merge にも承認を求めるため（[承認の状態](pull-requests.md#承認の状態)）。
  - merge できる人は絞らない。エージェントも人も merge するからである。
- `main` に追いついていることを merge の条件にする設定は、使わない（[衝突](../backend/merge.md)）。
- 守る設定がないか外れて、`main` に直接 push されても、Logiforge はそのコミットを受け入れる。木が不変条件を満たさなければ、そのコミットは valid でない（[最新のコミットが valid でないとき](../backend/invalid-main.md)）。
- 信頼の境界は、[GitForge のポートを pull request と issue で実装する](pull-requests.md) に書く。
- 他人のブランチの削除を防ぎたいときは、ブランチを守る設定で禁じる。

## GitHub の API の使い方

| 使う者 | 使い方 | 数える上限 |
|---|---|---|
| バックエンド（認証） | 要求ごとに、主体のトークンで、主体、team の所属、リポジトリの権限を確かめる | 主体ごとの上限 |
| バックエンド（GitForge のポートの読み） | 要求ごとに、主体のトークンで、PR と issue の一覧と、1 件の PR、review、コメントを REST で読む | 主体ごとの上限 |
| バックエンド（GitForge のポートの書き込み） | 主体のトークンで、REST で、repository の用意、PR の作成・merge・close、review、ラベル、issue、コメントを書く | 主体ごとの上限 |
| worker | `git ls-remote` と `git fetch`。新しい `main` のコミットがある見回りに、どの apply request の反映かを決めるのに、REST で反映した PR の一覧を読む | Logiforge の App の installation の上限 |

- バックエンドは、読んだ結果を保持しない（[状態を持たない](../backend/README.md)）。
- バックエンドは、GitHub の API のレート制限を避ける仕組み（キャッシュなど）を持たない。回数の見積もりは、[費用と規模の見積もり](../estimates/README.md#github-アダプタ) に書く。

## GitHub の制約への対処

- GitHub が、短い間の多くの書き込みに制限をかけたら、`Retry-After` を守って待ち、待つ間隔を指数的に延ばしながらやり直す。
  - なぜ：バックエンドはロックを取らないので、間隔を 1 か所でそろえられない。やり直しで足りる。
- 外の仕組みは、GitHub の webhook や Actions のトリガーを、直接使ってよい。
