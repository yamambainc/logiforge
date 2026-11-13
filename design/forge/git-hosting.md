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

| ref | fetch と clone | push |
|---|---|---|
| `refs/heads/main` | すべての主体 | `writer` だけ |
| `refs/logiforge/` の下 | すべての主体 | `writer` だけ |
| そのほかのブランチ（`refs/heads/` の下） | すべての主体 | すべての主体 |
| そのほかの ref（タグなど） | すべての主体 | だれもできない |

- `user` と `client` がブランチに push できるのは、apply request のもとになるブランチを置くためである（[CLI](../cli/README.md)）。
- `main` と `refs/logiforge/` に書けるのは worker だけである。独自フォージが、これを強制する。
  - 受け取った push の ref を、更新の前に 1 つずつ確かめ、決まりに合わない ref があれば、push 全体を拒む。
  - なぜ：`main` を進めるのは反映の手順だけで、版の記録を書くのは worker だけだから。
- push の資格は、receive-pack の広告（`info/refs?service=git-receive-pack`）の時点で確かめる。どの ref に書けるかは、push を受け取ってから確かめる。

## push で確かめること

- push の中身（ファイルの形や不変条件）は、確かめない。
  - なぜ：ルールはバックエンドにだけ置くから。利用者のブランチの中身は、apply request を作るときに、バックエンドが検査する。
- ref の期待する値（`--force-with-lease`）と、`--atomic` の push は、git の標準のとおりに扱う（[ref を書く](../adapters/git.md)）。
  - git の receive-pack が `atomic` を広告することを、作るときに確かめる。
