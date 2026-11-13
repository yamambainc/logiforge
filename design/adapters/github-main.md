# GitHub での main の守りと制約

GitHub を使う構成で、`main` への書き込みをどう守り、GitHub の API をどう使い、GitHub の制約にどう対処するかを確かめるときに読む。
GitHub アダプタそのものは、[GitHub アダプタ](github.md) に書く。

## main への書き込みを守る

- 利用者とエージェントは、GitHub リポジトリに、apply request のもとになるブランチを push する（[CLI](../cli/README.md)）。
- `main` を守る設定（ルールセットかブランチ保護）は、必須である。管理者が用意する。
  - `main` への push を禁じ、pull request の merge だけにする。
  - merge できるのは、App だけにする。
- `main` に追いついていることを merge の条件にする設定は、使わない（[衝突](../backend/merge.md)）。
- 守る設定が外れて、反映によらない変更が `main` に入ったら、[worker](../backend/worker.md) が見つけて警告する。

## GitHub の API の使い方

| 使う者 | 使い方 | 数える上限 |
|---|---|---|
| バックエンド（認証） | 要求ごとに、主体のトークンで、主体、team の所属、リポジトリの権限を確かめる | 主体ごとの上限 |
| バックエンド（やりとりの読み） | 要求ごとに、REST で、PR、レビュー、issue、コメントを読む | App の REST の上限 |
| バックエンド（やりとりの書き込み） | REST で、repository の用意、PR の作成・merge・close、レビュー、issue、コメントを書く | App の REST の上限 |
| worker | `git ls-remote` と `git fetch` だけ | API の上限に数えない |

- バックエンドは、読んだ結果を保持しない（[状態を持たない](../backend/README.md)）。
- レート制限は、今は考えない。見積もりと、当たるようになったときの対処は、[費用と規模の見積もり](../estimates.md) に書く。

## GitHub の制約への対処

- GitHub が、短い間の多くの書き込みに制限をかけたら、`Retry-After` を守って待ち、待つ間隔を指数的に延ばしながらやり直す。
  - なぜ：バックエンドはロックを取らないので、間隔を 1 か所でそろえられない。やり直しで足りる。
- 外の仕組みは、GitHub の webhook や Actions のトリガーを、直接使ってよい。
