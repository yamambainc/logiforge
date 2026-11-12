# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、何を何に対応づけるかを確かめるために読む。
インターフェースは、[プロバイダ](README.md) が正本である。

## 対応

| インターフェース | GitHub |
|---|---|
| repository | GitHub のリポジトリ（1 つの repository に 1 つ） |
| 提案 | pull request |
| 版 | pull request の head のコミット |
| 反映の失敗 | バックエンドが書く check run（`logiforge/core`）の結果 |
| 反映 | バックエンドが呼ぶ squash merge |
| レビュー | `event=COMMENT` の pull request review |
| 人への依頼 | issue（ラベル `logiforge/pvr` か `logiforge/decision`） |
| 回答 | バックエンドの App が、利用者の回答として書いたコメント |
| イベント | GitHub の webhook（`pull_request`、`pull_request_review`、`issues`、`issue_comment`、`push`） |

## repository を用意する

1. 人が、空の GitHub リポジトリを作る。
2. 人が、そのリポジトリにバックエンドの App を入れる。
3. 人が、書き込み権限を App だけに与え、ほかの主体を read にする（下の「main への書き込みを守る」）。
4. 利用者が、そのリポジトリの名前を指定して repository を作る。
5. バックエンドは、リポジトリが空であることと、App が書き込めることを確かめて、根の仮説を最初のコミットとして書き込む。

## バックエンドの App が作ったものだけを数える

- バックエンドは、GitHub App として動く。
  - App は、organization にリポジトリを作らない。
    - なぜ：App に、organization の Administration write という強い権限を持たせないため。
- 版は、作者がバックエンドの App のコミットだけで作る。
  - ほかの主体のコミットがブランチに入ったら、その版は無効にする。無効な版は反映しない。
- レビューは、作者がバックエンドの App のものだけを数える。
- 回答は、バックエンドの App が書いたコメントだけを数える。
- なぜ：セッション ID とプロンプトのリビジョンを、バックエンドが記録したものだけに限るため（[保証の範囲](../../docs/apply-request/review.md)）。

## レビューの書き方

- レビューは、すべて `event=COMMENT` の review として書く。
  - なぜ：pull request を作った App は、その pull request を approve できないから。
- レビューの種類は、本文の末尾の trailer で表す。
  - `Logiforge-Review-Kind: approve|request_changes|comment`
  - `Logiforge-Session-Id:`
  - `Logiforge-Prompt-Revision:`
- GitHub 上の approve の状態は、反映の条件に使わない。

## 人への依頼の書き方

- issue の本文の先頭に、対象ノードの識別子を書く。
- 「回答は 1 回だけ」の判定は、バックエンドが行う。
  - 回答を書いたら、バックエンドは issue に `logiforge/answered` ラベルを付けて閉じる。

## main への書き込みを守る

- リポジトリへの書き込み権限は、バックエンドの App だけに与える。人とエージェントは read にする。
- owner と admin は、それでも `main` に直接 push できる。
  - バックエンドは、`push` の webhook で、反映の手順によらないコミットを検出して警告する。
  - force push も、`push` の webhook の `forced` で検出して、警告に含める。
  - 警告は、API の応答と webhook で知らせる。
- Team 以上のプランでは、ブランチ保護（またはルールセット）でも、`main` への書き込みをバックエンドの App だけに限る。
- squash merge なので、提案のブランチのコミットは `main` には残らない。版を書いたセッションは、squash したコミットの trailer にまとめて書く。

## GitHub の制約への対処

- merge は、1 分に 1 件を目安にする。
  - なぜ：GitHub は、短い間に多くの merge やコミットの作成を行うと、制限をかけることがあるから。
- GitHub の webhook は、届かなかったときに自動では送り直されない。
  - 手動で送り直せるのは、3 日前までのものだけである。
- そのため、バックエンドは定期的に突き合わせ（reconcile）を行う。
  - `main` の HEAD が、バックエンドが最後に反映したコミットと同じかを確かめる。
  - 前回の突き合わせのあとに更新された pull request と issue を読み直す。
