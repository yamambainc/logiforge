# GitForge

バックエンドが、repository、apply request（pull request）、レビュー、人への依頼を扱うときに、GitForge に何を求めるかを確かめるときに読む。
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

## apply request

apply request は、GitForge の pull request である（[apply request と pull request](../apply-request/README.md)）。

- apply request を作る。説明欄とブランチを渡し、GitForge が決めた識別子を受け取る。
  - そのブランチに open の apply request がすでにあれば、そのことがわかる形で失敗する。
- 説明欄を書き換える。
- apply request を読む。状態（open、merged、closed）、作った主体と時刻、ブランチ、向き先（pull request の base のブランチ）、今の head のコミット、ラベル、反映したときは merge のコミットを返す。
- apply request を作った順に読む。
- 反映する。head のコミットの SHA と、反映のコミットのメッセージを渡し、squash merge する。
  - head が指定した SHA でなければ、そのことがわかる形で失敗する。
  - git の衝突で merge できなければ、そのことがわかる形で失敗する。
- close する。理由を、人が読む文で、close する主体の名で残す。
- 反映の失敗の理由を、head のコミットに残す。

## レビュー

- レビューを出す。
  - 種類（`approve`、`request_changes`、`comment`）、本文、レビューした先頭のコミット
  - 使ったプロンプトのリビジョン（本文の最後に、人が読む文で書く。読むときは返さない）
- apply request ごとのレビューを、出された順に読む。書いた人と、書いた人が repository に書く権限を持つかも返す（[数える review](../../adapters/pull-requests.md#数える-review)）。

## 人への依頼

- 人への依頼を起票する（対象ノード、種類、本文）。GitForge が決めた識別子を受け取る。
- 回答を書く（本文、回答した利用者、添付のファイル）。すでに回答があれば、そのことがわかる形で失敗する。
  - 添付のファイルは、GitForge の側に置く（[人への依頼の回答](../../files/artifacts.md)）。
- 人への依頼と回答を、作った順に読む。添付のファイルの中身を読む。

## 識別子

- apply request、レビュー、人への依頼の識別子は、GitForge が決める。それぞれ、pull request の番号、review の id、issue の番号である。
  - なぜ：GitForge がすでに一意の番号を持つので、バックエンドが別の識別子を作って対応を持たずに済むから。
- GitForge は、一覧を作った順に返す。
