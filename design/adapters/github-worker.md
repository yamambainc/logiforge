# GitHub での main の守りと worker

GitHub を使う構成で、`main` への書き込みをどう守り、[worker](../backend/worker.md) をどう動かし、GitHub の制約にどう対処するかを確かめるときに読む。
GitHub アダプタそのものは、[GitHub アダプタ](github.md) に書く。

## main への書き込みを守る

- GitHub リポジトリへの書き込み権限は、バックエンドの App だけに与える。人とエージェントは read にする。
- owner と admin は、それでも `main` に直接 push できる。
  - [worker](../backend/worker.md) が、反映の手順によらないコミットと force push を検出して警告する。
- Team 以上のプランでは、ブランチ保護（またはルールセット）でも、`main` への書き込みをバックエンドの App だけに限る。
  - App は、git の push で `main` を進める（[git アダプタ](git.md)）。そのため、App をその制限の例外にしておく。

## worker のワークフロー

- worker は、`main` への push で走る GitHub Actions のワークフローで動かす。
- バックエンドの App の push では、job を skip する。
  - job に `if:` を付け、push した主体（`github.event.sender`）が App の bot のときは、走らせない。
  - なぜ：skip した job は、Actions の時間に数えられないから（[費用と規模の見積もり](../estimates.md)）。
  - なぜ skip してよいか：App の push のあとに止まったときは、次の書き込みでバックエンドが直すから（[バックエンドが直すもの](../backend/worker.md)）。
- そのため、worker が走るのは、反映によらずに `main` が変わったとき（owner や admin の直接の push、force push）だけである。

## ワークフローのファイル

- バックエンドが、最初のリビジョンに `.github/workflows/` として入れる。
  - なぜ：push で走るワークフローは、push された `main` の中にある必要があるから。管理者が先に置くと、GitHub リポジトリが空でなくなり、用意の判定と合わなくなる。
- 反映の差分では、変えられない（[差分の検査](../backend/checks.md)）。
- 渡す鍵は、管理者が GitHub リポジトリの Actions の secrets に置く（[設定と秘密情報](README.md)）。

## GitHub の制約への対処

- GitHub が、短い間の多くの書き込みに制限をかけたら、待ってからやり直す。
  - なぜ：バックエンドはロックを取らないので、間隔を 1 か所でそろえられない。やり直しで足りる（[同時に走る反映](../backend/merge.md)）。
- 外の仕組みは、GitHub の webhook や Actions のトリガーを、直接使ってよい。
