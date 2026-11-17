# GitHub での main の守りと制約

GitHub を使う構成で、`main` への書き込みをどう守り、GitHub の API をどう使い、GitHub の制約にどう対処するかを確かめるときに読む。
GitHub アダプタそのものは、[GitHub アダプタ](github.md) に書く。

## main への書き込みを守る

- 利用者とエージェントは、GitHub リポジトリに、apply request のもとになるブランチを push する（[CLI](../cli/README.md)）。
- `main` を守る設定（ルールセットかブランチ保護）は、勧めるが求めない（[main を守る](pull-requests.md#main-を守る)）。使うときは、管理者が用意する。
  - ルールセットで、`main` に「Require a pull request before merging」「Block force pushes」「Restrict deletions」を付ける。bypass には、だれも入れない。
  - merge できる人は絞らない。reviewer が merge するからである。承認されていない pull request の merge は、worker が見つける（[反映かの判定](../backend/verify.md)）。
- `main` に追いついていることを merge の条件にする設定は、使わない（[衝突](../backend/merge.md)）。
- 守る設定がないか外れて、反映によらない変更が `main` に入ったら、[worker](../backend/worker.md) が見つけ、repository を壊れた状態にする（[壊れた repository](../backend/broken.md)）。
- 信頼の境界は、[GitForge アダプタの共通の書き方](pull-requests.md) に書く。
- 他人のブランチの削除を防ぎたいときは、ブランチを守る設定で禁じる。

## GitHub の API の使い方

| 使う者 | 使い方 | 数える上限 |
|---|---|---|
| バックエンド（認証） | 要求ごとに、主体のトークンで、主体、team の所属、リポジトリの権限を確かめる | 主体ごとの上限 |
| バックエンド（GitForge のポートの読み） | 要求ごとに、主体のトークンで、REST で、PR、レビュー、issue、コメントを読む | 主体ごとの上限 |
| バックエンド（GitForge のポートの書き込み） | 主体のトークンで、REST で、repository の用意、PR の作成・merge・close、レビュー、issue、コメントを書く | 主体ごとの上限 |
| worker | `git ls-remote` と `git fetch`。反映かの判定で、`main` のコミットごとに、REST で PR とコメントとレビューを読む | 読むだけのトークンの上限 |

- バックエンドは、読んだ結果を保持しない（[状態を持たない](../backend/README.md)）。
- レート制限は、今は考えない。見積もりと、当たるようになったときの対処は、[費用と規模の見積もり](../estimates.md) に書く。

## GitHub の制約への対処

- GitHub が、短い間の多くの書き込みに制限をかけたら、`Retry-After` を守って待ち、待つ間隔を指数的に延ばしながらやり直す。
  - なぜ：バックエンドはロックを取らないので、間隔を 1 か所でそろえられない。やり直しで足りる。
- 外の仕組みは、GitHub の webhook や Actions のトリガーを、直接使ってよい。
