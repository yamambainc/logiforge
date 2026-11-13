# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、[やりとり](../backend/ports/interaction.md) のポートをどう実装するかを確かめるために読む。
木の読み書きは、[git アダプタ](git.md) が、GitHub リポジトリをリモートとして扱う。

## 書き込み主体

GitHub アダプタに必要なのは、バックエンドだけが使う、専用の書き込み主体である。

- なぜ：GitHub に書き込む認証情報が要るから。
- なぜ：書き込める主体を、バックエンドだけに絞るから。

書き込み主体には、GitHub App を推奨する。

- 権限を細かく絞れる。
- 人に紐づかない。
- 席を消費しない。

機械用のユーザーアカウントでもよい。その場合は、席を 1 つ使う。
以下では、書き込み主体を「App」と書く。

## 対応

| やりとりの操作 | GitHub での実装 |
|---|---|
| repository | GitHub リポジトリ（1 つの repository に 1 つ）。provider_ref は `owner/name` |
| 提案 | pull request |
| 提案の識別子 | pull request の本文の末尾の trailer `Logiforge-Apply-Request:` |
| 版の一覧 | `refs/logiforge/apply-requests/<識別子>/*` の ref。REST の matching-refs（`GET /repos/{owner}/{repo}/git/matching-refs/logiforge/apply-requests/<識別子>/`）で読み、版の番号は ref の名前から取る |
| 今の版 | pull request の head ブランチ。GitHub アダプタが、git refs の API で、今の版のコミットに動かす |
| 反映の失敗 | バックエンドが書く check run（`logiforge/core`）の結果 |
| 反映されたことの記録 | バックエンドが pull request に反映したコミットを書き、close する |
| レビュー | `event=COMMENT` の pull request review |
| 人への依頼 | issue（ラベル `logiforge/pvr` か `logiforge/decision`） |
| 人への依頼の識別子 | issue の本文の末尾の trailer `Logiforge-Human-Request:` |
| 回答 | バックエンドの App が、利用者の回答として書いたコメント |

- pull request は merge しない。`main` は [git アダプタ](git.md) の「main を進める」で進める。
- 版のコミットは、pull request の head ブランチのほかに、版ごとの ref（[名前を付けたリビジョン](git.md)）にも置く。
  - なぜ：head ブランチは最新の版だけを指すので、古い版のコミットが git gc で消えないようにするため。
  - GitHub が `refs/logiforge/` の下への push を受け付けるかは、GitHub アダプタを作るときに確かめる。

## repository を用意する

1. 管理者が、空の GitHub リポジトリを作る。
2. 管理者が、その GitHub リポジトリに App を入れる（機械用のユーザーアカウントなら、書き込み権限を与える）。
3. 管理者が、書き込み権限を App だけに与え、ほかの主体を read にする（下の「main への書き込みを守る」）。
4. 管理者が、repository の名前と `owner/name` の組を、サーバの設定に登録する（[repository の登録](README.md)）。この組が、名前の正本である。
5. 利用者が、名前と根の主文で repository を作る。
6. バックエンドは、[repository の用意](../backend/README.md) の手順で、根の仮説を最初のリビジョンとして書き込む。
   - git アダプタの「用意する」は、App が書き込めることだけを確かめる。
   - GitHub リポジトリに `main` がすでにあるかは、「最初のリビジョンで main を作る」の結果で判定する。

- App は、organization に GitHub リポジトリを作らない。
  - なぜ：App に、organization の Administration write という強い権限を持たせないため。

## バックエンドの App が作ったものだけを数える

- 版は、[記録の形式](../files/revisions.md) の署名で確かめる。これが主な確認である。
  - 補助として、コミットの作者が App かも確かめる。
- レビューは、作者がバックエンドの App のものだけを数える。
- 回答は、バックエンドの App が書いたコメントだけを数える。
- なぜ：セッション ID とプロンプトのリビジョンを、バックエンドが記録したものだけに限るため（[保証の範囲](../../docs/apply-request/review.md)）。

## 認証

GitHub を使う構成では、[認証](../backend/ports/authentication.md) のポートも、このアダプタが実装する。認証は GitHub に任せる。

- 主体は、GitHub のアカウント（人）と、GitHub App または機械用アカウント（エージェント、外の仕組み）である。
- API は GitHub のトークンを受け取り、GitHub に問い合わせて主体を確かめる。
- 利用者は、WebUI で、書き込み主体と同じ GitHub App のユーザー認可（user-to-server トークン）でログインする。OAuth App は使わない。
- 主体の種類は、管理者が主体ごとに決める。
  - `user` とみなすのは、user-to-server トークンを持ち、かつ管理者がサーバの設定に登録した人（GitHub のアカウント名か、organization の team）だけである。
  - それ以外の主体（gh の CLI のトークン、App のトークンなど）は、`client` である。
- トークンを確かめた結果は、5 分だけキャッシュする。
  - なぜ：要求ごとに GitHub に問い合わせると、GitHub の API のレート制限に当たるから。
  - なぜ 5 分：権限や登録を外したことが、遅くとも 5 分で効くようにするため。
- repository を読めるかは、GitHub リポジトリの権限で確かめる。
  - 書き込み主体（App）以外の主体には、read だけを与える（下の「main への書き込みを守る」）。
- どの操作を使えるかの判定は、バックエンドの [認可](../backend/README.md) が行う。
- そのため、この構成では、サーバに正本が残らない（[組み合わせ](README.md)）。

## コメントとレビューの書き方

- レビューは、すべて `event=COMMENT` の review として書く。
  - なぜ：pull request を作った App は、その pull request を approve できないから。
- レビューの本文の末尾に、次の trailer を書く。
  - `Logiforge-Review-Id:`（レビューの識別子）
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
  - バックエンドは、突き合わせで、反映の手順によらないコミットと force push を検出して警告する（[突き合わせ](../backend/README.md)）。
- Team 以上のプランでは、ブランチ保護（またはルールセット）でも、`main` への書き込みをバックエンドの App だけに限る。
  - App は、git の push で `main` を進める（[git アダプタ](git.md)）。そのため、App をその制限の例外にしておく。

## GitHub の制約への対処

- 反映は、1 分に 1 件を目安にする。
  - なぜ：GitHub は、短い間に多くのコミットの作成を行うと、制限をかけることがあるから。
- 外の仕組みは、GitHub の webhook や Actions のトリガーを、直接使ってよい。
