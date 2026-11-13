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
| `refs/heads/main` | すべての主体 | だれもできない。merge の API だけが進める |
| `refs/heads/logiforge/` の下 | すべての主体 | だれもできない（予約） |
| そのほかのブランチ（`refs/heads/` の下） | すべての主体 | `user` と `client` |
| `refs/pull/<識別子>/<SHA>` | すべての主体 | だれもできない。独自フォージが書く。番号は持たせない（版の正体は SHA。[版](../backend/apply-request.md)） |
| そのほかの ref（タグなど） | すべての主体 | だれもできない |

- `user` と `client` がブランチに push できるのは、apply request のもとになるブランチを置くためである（[CLI](../cli/README.md)）。
- `main` を進めるのは、`mergeApplyRequest` だけである（[openapi.yaml](openapi.yaml)）。
  - なぜ：GitHub の構成で、`main` を守る設定を必須にするのと同じ振る舞いにするため（[main への書き込みを守る](../adapters/github-main.md)）。
- 独自フォージは、これを pre-receive で強制する。
  - 認証した主体と、その種類を、pre-receive に渡す。
  - 受け取った push の ref を、更新の前に 1 つずつ確かめ、決まりに合わない ref があれば、push 全体を拒む。
- ブランチを消す push を受け取ったら、そのブランチの open の提案を close にする。
  - なぜ：GitHub で、ブランチを消すと pull request が閉じるのと同じ振る舞いにするため。
- 信頼の境界：push できる主体は、他人のブランチも消せる。そのため、他人の open の apply request を close にできる。
  - 消されても、反映済みの apply request は壊れない（[ブランチの掃除](../backend/apply-request.md)）。版のコミットは、下の `refs/pull/` に残る。
- 独自フォージは、提案のブランチの先頭が変わるたびに、その版のコミットを `refs/pull/<識別子>/<SHA>` に置く。
  - なぜ：squash merge とブランチの削除のあとも、版のコミットを git から辿れるようにするため（[ブランチの履歴](../backend/snapshot.md)）。
- push の資格は、receive-pack の広告（`info/refs?service=git-receive-pack`）の時点で確かめる。どの ref に書けるかは、push を受け取ってから確かめる。

## merge

- `mergeApplyRequest` は、GitHub の pull request の squash merge と同じ意味にする。中は、bare リポジトリに対する git で行う。
  - ブランチの先頭が、指定した SHA かを確かめる。
  - `git merge-tree --write-tree` で、今の `main` とブランチを、git の 3-way の merge と同じ規則で合わせる。衝突があれば、失敗する。
  - 合わせた tree を、今の `main` だけを親とする 1 つのコミットにし、受け取ったメッセージを付けて、`main` を進める（squash）。
- 同じ repository の merge は、独自フォージの中で 1 つずつ順に行う。

## push で確かめること

- push の中身（ファイルの形や不変条件）は、確かめない。
  - なぜ：ルールはバックエンドにだけ置くから。利用者のブランチの中身は、apply request を作るときと反映の前に、バックエンドが検査する。
