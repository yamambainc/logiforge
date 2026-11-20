# review と issue の本文

GitHub アダプタと Forgejo アダプタが、review、pull request のコメント、issue に、何を書き、どこを読むかを確かめるときに読む。
どの GitForge の機能に対応させるかは、[GitForge のポートを pull request と issue で実装する](pull-requests.md) に書く。

- 本文に書くものは、GitForge の画面で人が読める、ふつうの文である。署名は付けない。
  - なぜ：状態は GitForge の機能（pull request、review、issue の状態と作者）で表し、本文には、それで表せない補いだけを書くから（[信頼の境界](pull-requests.md#信頼の境界)）。
- バックエンドが読む行は、`<名前>: <値>` の形で、本文の最後に書く。名前は、下の各節のとおりで、変えない。
  - なぜ変えないか：GitForge に残った本文を、あとの版でも読めるようにするため。
- 本文は、Write を持つ主体が書き換えられる。読んだ値は、表示と、自己申告の確かめにだけ使う。

## review の本文

`approve` と `request changes` の review の本文の最後に、次の行を書く。`comment` の review には書かなくてよい。

| 行 | 値 |
|---|---|
| `Session-Id:` | review を書いたセッション。自己申告である |
| `Prompt-Revision:` | reviewer が従ったプロンプトの版。WebUI の review では `-` |

- バックエンドを通した review では、バックエンドが書く。GitForge の画面から直接 review するときは、reviewer が書く。
- `Session-Id:` の行のない `approve` は、数えない（[数える review](pull-requests.md#数える-review)）。
  - なぜ：反映の条件で、reviewer のセッションが、先頭のコミットを書いたセッションと違うことを確かめるから（[反映の手順](../backend/merge.md)）。
- 反映の失敗を書く pull request のコメントには、失敗の理由と詳しい説明を、人が読める文で書く。バックエンドは、このコメントを読まない。失敗の理由は、commit status で読む。

## close のコメント

バックエンドを通して close するときは、close の直前に、close する主体のトークンで、pull request にコメントを書く。

| 行 | 値 |
|---|---|
| `Close-Reason:` | close の理由。要求で送られた文である（[apply request を close する](../api/endpoints/apply-request/close.md)） |
| `Session-Id:` | close したセッション |

- close した主体は、GitForge が記録する、pull request を閉じた人である。
- 画面から close されたものには、このコメントがない。理由とセッションは、空として返す。

## 人への依頼の issue

- タイトルは、依頼の要約である。
- 本文の最後に、次の行を書く。

| 行 | 値 |
|---|---|
| `Target-Node:` | 対象ノードの識別子 |
| `Session-Id:` | 起票したセッション |
| `Idempotency-Key:` | 起票の要求の `Idempotency-Key`。あるときだけ（[同じ要求の繰り返し](../api/writes.md#同じ要求の繰り返し)） |

- 依頼の種類は、ラベル（`logiforge/pvr`、`logiforge/decision`）で決める。起票した主体は、issue の作者である。
- 対象ノードがないか、今の木にない依頼は、作業に出さず、一覧で警告する。

## 回答

- 回答は、回答した利用者のトークンで、issue にコメントを書き、続けて同じトークンで issue を close する。
  - 回答済みかは、issue が closed かで決める。
  - 回答は、issue を閉じた人が、閉じる前に書いた最後のコメントである。回答した利用者は、そのコメントの作者である。
  - なぜ close を使うか：回答済みかを、issue の一覧に入っている状態だけで決められるから。
- 回答の添付は、そのコメントの添付である（Forgejo）。GitHub では、リンクだけを本文に書く（[人への依頼の回答](../files/artifacts.md)）。
  - 添付の名前と sha256 は、本文に書かない。sha256 は、添付を読むときに、その中身から求める。
    - そのため、回答の添付の sha256 を求めるのは、その人への依頼を 1 件読むときと、添付を読むときだけである。一覧では求めない。
- issue が reopen されたら、回答されていないものとして扱う。
