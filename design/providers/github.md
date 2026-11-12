# GitHub アダプタ

GitHub を使って Logiforge を動かすときに、何を何に対応づけるかを確かめるために読む。
インターフェースは、[プロバイダ](README.md) が正本である。

## 対応

| インターフェース | GitHub |
|---|---|
| 提案 | pull request |
| 版 | pull request の head のコミット |
| 反映の失敗 | バックエンドが書く check run（`logiforge/core`）の結果 |
| 反映 | バックエンドが呼ぶ squash merge |
| レビュー | pull request review |
| 人への依頼 | issue（ラベル `logiforge/pvr` か `logiforge/decision`） |
| 回答 | 利用者の最初のコメント |
| イベント | GitHub の webhook（`pull_request`、`pull_request_review`、`issues`、`issue_comment`、`push`） |

## 記録の仕方

- apply request の識別子は、`apr_` と pull request の番号をつないだものにする。
- レビューのセッションとプロンプトのリビジョンは、レビュー本文の末尾に trailer として書く。
  - `Logiforge-Session-Id:` と `Logiforge-Prompt-Revision:`
  - trailer のないレビュー（GitHub の画面から直接付けたものなど）は、バックエンドが数えない。
- 人への依頼の本文の先頭に、対象ノードの識別子を書く。
- 回答を受け付けたら、バックエンドは issue に `logiforge/answered` ラベルを付けて閉じる。
  - 回答は 1 回だけなので、それ以後のコメントは数えない。

## main への書き込みを守る

- バックエンドは、GitHub App として動き、squash merge だけで `main` に書き込む。
- Team 以上のプランでは、`main` への書き込みを、バックエンドの GitHub App だけに限る。
  - ブランチ保護（またはルールセット）で、直接の push と、ほかの主体による merge を止める。
  - `logiforge/core` の check を、merge の必須条件にする。
- Free プランの private リポジトリでは、ブランチ保護が使えない。
  - そのため、`main` への直接の push や、GitHub の画面からの merge を止められない。
  - バックエンドは、`push` の webhook で、反映の手順によらないコミットを検出する。
  - 検出したら、API の応答と webhook で警告する。
  - なぜ：止められない以上、木が不変条件を外れたことを、すぐ気づけるようにするため。

## 対応しないこと

- GitHub の画面からの approve を、反映の条件に数えること
  - なぜ：セッション ID と、プロンプトのリビジョンが記録されないから（[保証の範囲](../../docs/apply-request/review.md)）。
