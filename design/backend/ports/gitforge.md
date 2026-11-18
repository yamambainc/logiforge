# GitForge

バックエンドが、repository、提案（pull request）、レビュー、人への依頼を扱うときに、GitForge に何を求めるかを確かめるときに読む。
このポートを実装するのが、GitForge アダプタである（[アダプタ](../../adapters/README.md)）。

GitForge は、ルールを判定しない。受け取ったものを置き、求められたら返すだけである。
バックエンドは、読む操作を要求ごとに呼ぶ。結果を保持しない（[状態を持たない](../README.md#状態を持たない)）。

## repository

- repository を列挙する。名前と gitforge_ref の組を返す。
- 名前から gitforge_ref を求める。
- repository を用意する。git リポジトリを作り（作れる GitForge だけ）、根の仮説を入れた最初のコミットを `main` に置く。
  - `main` がすでにあれば、そのことがわかる形で失敗する（[repository の用意](../README.md#repository-の用意)）。
  - バックエンドは、gitforge_ref と、最初のコミットに入れるファイルを渡す。
- `main` の先頭を読む。GitForge の今の `main` の SHA を返す。反映の前に、worker が追いついているかを確かめるのに使う（[反映の手順](../merge.md)）。
- ほかの操作は、どれも repository を指定して行う。

## 提案

提案（apply request）は、GitForge の pull request である（[apply request と pull request](../apply-request/README.md)）。

- 提案を作る。識別子、説明欄、ブランチ、作ったときの先頭のコミットを渡す。
  - そのブランチに open の提案がすでにあれば、そのことがわかる形で失敗する。
- 説明欄を書き換える。
- 提案を読む。状態（open、merged、closed）、ブランチ、向き先（pull request の base のブランチ）、作ったときの先頭のコミット、今の head のコミット、反映したときは merge のコミットを返す。
- 提案を作った順に読む。
- 反映する。head のコミットの SHA と、反映のコミットのメッセージを渡し、squash merge する。
  - head が指定した SHA でなければ、そのことがわかる形で失敗する。
  - git の衝突で merge できなければ、そのことがわかる形で失敗する。
- close する。理由と、close したセッションを記録する。
- 反映の失敗の理由を記録する。

## レビュー

- レビューを記録する。
  - 種類（`approve`、`request_changes`、`comment`）、本文、レビューした先頭のコミット
  - reviewer のセッションと、使ったプロンプトのリビジョン
- 提案ごとのレビューを、記録した順に読む。

## 人への依頼

- 人への依頼を記録する（対象ノード、種類、本文、起票したセッション）。
- 回答を記録する（本文、回答した利用者、添付のファイル）。すでに回答があれば、そのことがわかる形で失敗する。
  - 添付のファイルは、GitForge の側に置く（[人への依頼の回答](../../files/artifacts.md)）。
- 人への依頼と回答を、作った順に読む。添付のファイルの中身を読む。

## 識別子

- apply request、レビュー、人への依頼の識別子は、バックエンドが決める。
  - 形は、接頭辞（`apr_`、`rev_`、`hrq_`）と、Crockford の base32 の 26 文字である。
  - 26 文字は、ULID か、`Idempotency-Key` から決定的に作ったものである（[同じ要求の繰り返し](../../api/writes.md)）。
- GitForge は、受け取った識別子で置き、その識別子で返す。
- GitForge は、一覧を作った順に返す。識別子の順ではない。
