# git のホスティング

独自フォージが、git リポジトリをどう置き、どう公開するかを確かめるときに読む。

## 置き方

- 1 つの repository に、1 つの bare リポジトリを置く。
- パスは、`<設定したディレクトリ>/<repository の名前>.git` とする。
- repository の一覧は、このディレクトリの中の bare リポジトリの名前から得る。
  - なぜ：一覧のテーブルを持つと、ディレクトリと食い違わないようにする仕組みが要るから。
- bare リポジトリは、repository を作る API で作る（[openapi.yaml](openapi.yaml) の `putRepository`）。
  - 作りかけのものを一覧に出さないように、別の名前で作ってから、名前を変える。

## 公開

- git の smart HTTP で公開する。
  - URL は、`<独自フォージの URL>/git/<repository の名前>.git` である。
  - なぜ：git の標準の仕組みなので、バックエンドも、利用者の git の道具も、そのまま使えるから。
- 認証は、HTTP の Basic 認証の、パスワードにトークンを入れる。ユーザー名は見ない。
  - なぜ：git の資格情報の仕組みで、そのまま渡せるから。
- 主体ごとに、次のことを許す（[主体](README.md)）。
  - fetch と clone：すべての主体
  - push：`writer` だけ

## push で確かめること

- push の中身（ファイルの形や不変条件）は、確かめない。
  - なぜ：ルールはバックエンドにだけ置くから。`writer` はバックエンドだけである。
- ref の期待する値（`--force-with-lease`）は、git の標準のとおりに確かめる（[ref を書く](../adapters/git.md)）。
