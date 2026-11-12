# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、[やりとり](../backend/ports/interaction.md) のポートをどう実装するかを確かめるために読む。
木のリポジトリは、[git アダプタ](git.md) の GitHub のリモートで扱う。

## 対応

| やりとりの操作 | GitHub での実装 |
|---|---|
| repository | GitHub リポジトリ（1 つの repository に 1 つ）。provider_ref は `owner/name` |
| 提案 | pull request |
| 版 | pull request の head のコミット |
| 反映の失敗 | バックエンドが書く check run（`logiforge/core`）の結果 |
| 反映されたことの記録 | バックエンドが pull request に反映したコミットを書き、close する |
| レビュー | `event=COMMENT` の pull request review |
| 人への依頼 | issue（ラベル `logiforge/pvr` か `logiforge/decision`） |
| 回答 | バックエンドの App が、利用者の回答として書いたコメント |
| イベント | GitHub の webhook（`pull_request`、`pull_request_review`、`issues`、`issue_comment`、`push`） |

- pull request は merge しない。`main` は [git アダプタ](git.md) の「main を進める」で進める。
  - なぜ：検査したときの `main` から進めることを、確実にするため。

## repository を用意する

1. 人が、空の GitHub リポジトリを作る。
2. 人が、その GitHub リポジトリにバックエンドの App を入れる。
3. 人が、書き込み権限を App だけに与え、ほかの主体を read にする（下の「main への書き込みを守る」）。
4. 利用者が、`owner/name` を provider_ref に指定して repository を作る。
5. バックエンドは、GitHub リポジトリが空であることと、App が書き込めることを確かめて、根の仮説を最初のコミットとして書き込む。

- App は、organization に GitHub リポジトリを作らない。
  - なぜ：App に、organization の Administration write という強い権限を持たせないため。

## バックエンドの App が作ったものだけを数える

- 版は、[記録の形式](../files/revisions.md) の署名で確かめる。
  - 補助として、コミットの作者がバックエンドの App かも確かめる。
- レビューは、作者がバックエンドの App のものだけを数える。
- 回答は、バックエンドの App が書いたコメントだけを数える。
- なぜ：セッション ID とプロンプトのリビジョンを、バックエンドが記録したものだけに限るため（[保証の範囲](../../docs/apply-request/review.md)）。

## コメントとレビューの書き方

- レビューは、すべて `event=COMMENT` の review として書く。
  - なぜ：pull request を作った App は、その pull request を approve できないから。
- レビューの本文の末尾に、次の trailer を書く。
  - `Logiforge-Review-Kind: approve|request_changes|comment`
  - `Logiforge-Session-Id:`
  - `Logiforge-Prompt-Revision:`
- GitHub 上の approve の状態は、反映の条件に使わない。
- 提案の close と、人への依頼の起票では、App が書くコメントの末尾に `Logiforge-Session-Id:` を書く。
- 人への依頼の issue の本文の先頭に、対象ノードの識別子を書く。
- 「回答は 1 回だけ」の判定は、バックエンドが行う。
  - 回答を書いたら、バックエンドは issue に `logiforge/answered` ラベルを付けて閉じる。

## main への書き込みを守る

- GitHub リポジトリへの書き込み権限は、バックエンドの App だけに与える。人とエージェントは read にする。
- owner と admin は、それでも `main` に直接 push できる。
  - バックエンドは、`push` の webhook と突き合わせで、反映の手順によらないコミットを検出して警告する。
  - force push も、`push` の webhook の `forced` で検出して、警告に含める。
- Team 以上のプランでは、ブランチ保護（またはルールセット）でも、`main` への書き込みをバックエンドの App だけに限る。

## GitHub の制約への対処

- 反映は、1 分に 1 件を目安にする。
  - なぜ：GitHub は、短い間に多くのコミットの作成を行うと、制限をかけることがあるから。
- GitHub の webhook は、届かなかったときに自動では送り直されない。
  - 手動で送り直せるのは、3 日前までのものだけである。
- そのため、バックエンドは定期的に突き合わせを行い、前回のあとに更新された pull request と issue を読み直す。
- 外の仕組みは、GitHub の webhook や Actions のトリガーを、直接使ってよい。
