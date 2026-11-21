# 履歴（ディレクトリ方式）

ディレクトリの repository で、スナップショットツリーの版をどう数え、worker が何を置き場に書くかを確かめるときに読む。
git リポジトリ全体の repository の決まりは、[main のコミットとコミットの情報](../files/revisions.md) と [スナップショット](../backend/snapshot/README.md) に書く。

## 版

- ディレクトリの repository の版は、`main` の first-parent の並びのうち、そのディレクトリの中のファイルを変えたコミットだけである。
  - ディレクトリを変えないコミット（コードの merge）は、版にしない。スナップショットツリーも、コミットの情報も作らない。
  - なぜ：ディレクトリの中身は変わらないので、前の版と同じスナップショットツリーになるから。コードのコミットはディレクトリのコミットよりずっと多く、すべてに作ると、置き場が増えるだけだから。
- 版の番号（コミットの情報の `number`）は、ディレクトリを変えたコミットだけを、古い順に 1 から数える。
- コミットの情報の `parent` は、1 つ前の版のコミットである。git の親とは限らない。
- `main` の並びの索引も、版だけを並べる。
- 1 つの git リポジトリの 1 つのコミットが、2 つのディレクトリを変えたら、それぞれの repository の版になる。

## 用意のコミット

- repository の用意は、利用者のトークンで、根のファイルをディレクトリに置くコミットを 1 つ作り、`main` に置く。
  - GitHub：Git Data API で、今の `main` の木にディレクトリのファイルを足したコミットを作り、`main` の ref を進める（fast-forward でなければ、`main` を読み直して作り直す）。
  - Forgejo：`POST /repos/{owner}/{repo}/contents` で、ディレクトリのファイルを 1 つのコミットにまとめて置く。
  - なぜ pull request にしないか：用意は、利用者が repository を作る操作で、論理レビューを通す apply request ではないから（[repository の用意](../backend/README.md#repository-の用意)）。
- 移したときは、用意のコミットのメッセージに、`Logiforge-Migrated-From:` を付ける（[別リポジトリ方式から移す](migrate.md)）。
- 用意のコミットは、そのディレクトリの版の 1 番である。
  - そのディレクトリを変えた、前のコミットがあってはいけない。あれば、用意を断る（`repository_exists`）。
- 守る設定は、用意のあとに、そのディレクトリに掛ける（[main の守り](main.md)）。用意のコミットは、`main` に直接置くからである。
- 用意が止まったら（コミットを置く前に止まった、`main` が進んで置けなかった）、送り直しで、もう一度作る。

## 反映のコミット

- 反映のコミットのメッセージには、git リポジトリ全体の repository と同じく、機械が読む trailer を書かない（[反映のコミット](../files/revisions.md#反映のコミット)）。

## valid

- 版が valid かは、git リポジトリ全体の repository と同じく、その版の木だけで決める（[最新のコミットが valid でないとき](../backend/invalid-main.md)）。木は、ディレクトリの中だけである。
  - ディレクトリの外は、見ない。なぜ：外は、プロジェクトのコードで、プロジェクトの PR とレビューと守る設定で守るものだから。
- 1 つのコミットが、2 つのディレクトリ A と B を変えたら、A と B の repository で、別々に決める。
- `main` の書き換えは、`scanned` が git の `main` の並びになくなったときに見つける。worker は、git リポジトリ全体の repository と同じく、新しい first-parent の並びを、そのまま受け入れる（[latest](../backend/snapshot/main.md#latest)）。
  - その repository の `latest` が、新しい並びに残っていれば、版は変わらない。`scanned` だけを進める。
  - 残っていなければ、新しい並びのうち、スナップショットツリーのあるいちばん新しい版から続ける。

## どの apply request の反映か

- worker は、版ごとに、git リポジトリ全体の repository と同じく、反映した pull request の一覧で、その版を反映した apply request を探す（[どの apply request の反映か](../backend/invalid-main.md#どの-apply-request-の反映か)）。
- 探すのは、この repository の apply request とみなす pull request だけである（[apply request とみなす PR](README.md#apply-request-とみなす-pr)）。

## worker

- worker は、git リポジトリ全体の repository と同じく、repository ごとに 1 つの clone を持ち、repository ごとに見回る。同じ git リポジトリのディレクトリの repository が 2 つあれば、clone も 2 つになる。
  - なぜ git リポジトリごとにまとめないか：repository ごとのロックと見回りを、そのまま使えるから。clone が増えるのは、ディレクトリの repository の数だけで、ディスクのほかの費用は変わらない。
- `main` の新しいコミットごとに、`git diff-tree` の変わったパスで、そのディレクトリを変えたかを決める。変えたコミットだけを、版にする。
  - 版の差分とファイルは、パスからディレクトリを除いて、ディレクトリを根として扱う。そのため、スナップショットツリー、差分の検査、重ねる計算は、git リポジトリ全体の repository と同じものを使う。
- repository ごとに、置き場に `scanned` を置く。worker が読み終えた、git の `main` の先頭の SHA である。
  - `latest` は、git リポジトリ全体の repository と同じく、いちばん新しい版の SHA である。
  - バックエンドは、GitForge の `main` に worker が追いついたかを、`scanned` で決める（[main の守り](main.md) の「反映の順番」）。
  - `scanned` は、その SHA までの版を置き終えてから書く。
- 新しい版の `parent` が今の `latest` であるときだけ、`latest` を書く（[latest](../backend/snapshot/main.md#latest) と同じ。比べる相手が、git の親でなく 1 つ前の版になるだけである）。
- ブランチのスナップショットは、base からの差分がそのディレクトリの中のファイルを変えるブランチにだけ作る。
  - なぜ：コードのブランチを、すべての repository のブランチの履歴に入れないため。
  - スナップショットの base は、その時点の版（base までにディレクトリを変えた、いちばん新しい `main` のコミット）にする。なぜ：本当の base がコードのコミットだと、スナップショットツリーがないから。ディレクトリの中身は、どちらも同じである。
  - ディレクトリの外で変わったパスも、スナップショットに持つ。バックエンドは、それで `outside_directory` を決める。

## 引用

- [引用した時点](../files/revisions.md#引用した時点)は、いつもその repository の版である。
  - なぜ：検証結果のファイルを加えたコミットは、ディレクトリの中を変えるから。
