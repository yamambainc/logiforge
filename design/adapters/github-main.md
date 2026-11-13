# GitHub での main の守りと制約

GitHub を使う構成で、`main` への書き込みをどう守り、GitHub の API をどう使い、GitHub の制約にどう対処するかを確かめるときに読む。
GitHub アダプタそのものは、[GitHub アダプタ](github.md) に書く。

## main への書き込みを守る

- 利用者とエージェントは、GitHub リポジトリに、apply request のもとになるブランチを push する（[CLI](../cli/README.md)）。
- `main` と `refs/logiforge/` を守る設定（ルールセットかブランチ保護）は、運用する人が用意する。
- 用意しないときは、push できる人を信頼する。[worker](../backend/worker.md) が、反映の手順によらない `main` の変化を見つけて警告する。
- App は、git の push で `main` と `refs/logiforge/` を書く（[git アダプタ](git.md)）。守る設定では、App をその例外にしておく。

## GitHub の API の使い方

| 使う者 | 使い方 | 数える上限 |
|---|---|---|
| バックエンド（認証） | 要求ごとに、主体のトークンで、主体、team の所属、リポジトリの権限を確かめる | 主体ごとの上限 |
| バックエンド（やりとりの読み） | 要求ごとに、REST で、PR、レビュー、issue、コメントを読む | App の REST の上限 |
| バックエンド（やりとりの書き込み） | REST で、PR、レビュー、issue、コメントを書く | App の REST の上限 |
| worker | `git ls-remote`、`git fetch`、`git push` だけ | API の上限に数えない |

- バックエンドは、読んだ結果を保持しない（[状態を持たない](../backend/README.md)）。
- レート制限は、今は考えない。見積もりと、当たるようになったときの対処は、[費用と規模の見積もり](../estimates.md) に書く。

## GitHub の制約への対処

- GitHub が、短い間の多くの書き込みに制限をかけたら、`Retry-After` を守って待ち、待つ間隔を指数的に延ばしながらやり直す。
  - なぜ：バックエンドはロックを取らないので、間隔を 1 か所でそろえられない。やり直しで足りる（[同時に走る反映](../backend/merge.md)）。
- 外の仕組みは、GitHub の webhook や Actions のトリガーを、直接使ってよい。
