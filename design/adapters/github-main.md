# GitHub での main の守りと制約

GitHub を使う構成で、`main` への書き込みをどう守り、GitHub の制約にどう対処するかを確かめるときに読む。
GitHub アダプタそのものは、[GitHub アダプタ](github.md) に書く。

## main への書き込みを守る

- GitHub リポジトリへの書き込み権限は、バックエンドの App だけに与える。人とエージェントは read にする。
- owner と admin は、それでも `main` に直接 push できる。
  - [worker](../backend/worker.md) が、反映の手順によらないコミットと force push を見つけ、スナップショットを作って警告する。
- Team 以上のプランでは、ブランチ保護（またはルールセット）でも、`main` への書き込みをバックエンドの App だけに限る。
  - App は、git の push で `main` を進める（[git アダプタ](git.md)）。そのため、App をその制限の例外にしておく。

## GitHub の制約への対処

- GitHub が、短い間の多くの書き込みに制限をかけたら、待ってからやり直す。
  - なぜ：バックエンドはロックを取らないので、間隔を 1 か所でそろえられない。やり直しで足りる（[同時に走る反映](../backend/merge.md)）。
- worker は、`main` の変化を、GitHub の API ではなく `git ls-remote` で確かめる（[worker](../backend/worker.md)）。
  - なぜ：git のプロトコルなので、GitHub の API のレート制限に当たらないから。
- 外の仕組みは、GitHub の webhook や Actions のトリガーを、直接使ってよい。
