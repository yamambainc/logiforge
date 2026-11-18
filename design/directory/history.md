# 履歴（ディレクトリ方式）

ディレクトリの repository で、スナップショットツリーの版をどう数え、worker が何を記録するかを確かめるときに読む。
git リポジトリ全体の repository の決まりは、[main のコミットと記録](../files/revisions.md) と [スナップショット](../backend/snapshot/README.md) に書く。

## 版

- ディレクトリの repository の版は、`main` の first-parent の並びのうち、そのディレクトリの中のファイルを変えたコミットだけである。
  - ディレクトリを変えないコミット（コードの merge）は、版にしない。スナップショットツリーも、コミットの記録も作らない。
  - なぜ：ディレクトリの中身は変わらないので、前の版と同じスナップショットツリーになるから。コードのコミットはディレクトリのコミットよりずっと多く、すべてに作ると、置き場が増えるだけだから。
- 版の番号（コミットの記録の `number`）は、ディレクトリを変えたコミットだけを、古い順に 1 から数える。
- コミットの記録の `parent` は、1 つ前の版のコミットである。git の親とは限らない。
- `main` の並びの索引も、版だけを並べる。
- 1 つの git リポジトリの 1 つのコミットが、2 つのディレクトリを変えたら、それぞれの repository の版になる。

## 用意のコミット

- repository の用意は、利用者のトークンで、根のファイルをディレクトリに置くコミットを 1 つ作り、`main` に置く。
  - GitHub：Git Data API で、今の `main` の木にディレクトリのファイルを足したコミットを作り、`main` の ref を進める（fast-forward でなければ、`main` を読み直して作り直す）。
  - Forgejo：`POST /repos/{owner}/{repo}/contents` で、ディレクトリのファイルを 1 つのコミットにまとめて置く。
  - なぜ pull request にしないか：用意のコミットは、元の apply request がないので、反映かの判定の「apply request の反映のコミット」にならないから。署名で、用意のコミットと決める（下の「反映かの判定」）。
- 用意のコミットのメッセージに、`Logiforge-Init:` と `Logiforge-Init-Signature:` を付ける。`Logiforge-Apply-Request:` は付けない。
  - 署名の形は、[用意のコミットの署名](../backend/verify/init.md#用意のコミットの署名) の、種類 `init-directory` である。ディレクトリのパスと、移したときは `Logiforge-Migrated-From:` の値も含める（[別リポジトリ方式から移す](migrate.md)）。
  - なぜ 1 つ前のコミットを含めないか：ディレクトリの repository では、1 つ前のコミットはコードのコミットで、Forgejo の contents API では、置く前に決められないから。署名を使い回せないことは、版の 1 番であること（そのディレクトリを変えた前のコミットがないこと）で守る。
- 用意のコミットは、そのディレクトリの版の 1 番である。
  - そのディレクトリを変えた、前のコミットがあってはいけない。あれば、用意を断る（`repository_exists`）。
- 守る設定は、用意のあとに、そのディレクトリに掛ける（[main の守り](main.md)）。用意のコミットは、`main` に直接置くからである。
- 用意が止まったら（コミットを置く前に止まった、`main` が進んで置けなかった）、送り直しで、もう一度作る。

## 反映のコミットの trailer

- 反映のコミットの trailer は、[記録の形式](../files/revisions.md#記録の形式) と同じである。表示のためで、判定には使わない。
  - `Logiforge-Version:` の意味は変わらない。先頭のコミットの一覧の中の位置と、その SHA である。
  - バックエンドは、反映のコミットのメッセージに `Logiforge-Repository:` も書く。

## 反映かの判定

版ごとに（そのディレクトリを変えた `main` のコミット C ごとに）、反映によるものかを、中身だけで決める。決め方は、git リポジトリ全体の repository と同じで（[反映かの判定](../backend/verify/README.md)）、比べる範囲がディレクトリの中になる。P は、C の 1 つ前の `main` のコミット（git の first-parent）である。1 つ前の版ではない。

次のどちらかなら、反映によるものである。

- 用意のコミット：版の 1 番で、`Logiforge-Init:` と `Logiforge-Init-Signature:` があり、種類 `init-directory` の署名が合い、C の変更がそのディレクトリにファイルを加えることだけである。
- apply request の反映のコミット：[反映かの判定](../backend/verify/README.md) の「apply request の反映のコミット」の 1〜6 を、次のように読み替えて満たす。
  - 3：控えの `apply_request` の記録の `Logiforge-Repository:` が、この repository のディレクトリのパスである。
  - 5：P の木のディレクトリの中に、H の差分を重ね、重ねた木のディレクトリの中が、C の木のディレクトリの中と同じである。
  - 6：差分の検査に、差分がディレクトリの中だけであること（`outside_directory`）を含める。

- どちらでもなければ、`no_apply_request` を付け、この repository を壊れた状態にする。
- ディレクトリの外の変更は、判定に使わない。
  - apply request の差分（base から H まで）は、ディレクトリの中だけである（`outside_directory`）。そのため、GitForge が H を squash merge しても merge のコミットにしても、C が P から変えるのは、ディレクトリの中だけになる。
  - C がディレクトリの外も変えていたら、だれかが GitForge の merge でないやり方で作ったコミットである。ディレクトリの中が条件を満たせば、この repository では反映によるものとし、コミットの記録に警告（`outside_directory`）を付ける。
  - なぜ外で断らないか：ディレクトリの外は、プロジェクトのコードで、プロジェクトの PR とレビューと守る設定で守るものだから。Logiforge は、外を読まない。
- 1 つのコミット C が、2 つのディレクトリ A と B を変えたら、A と B の repository で、別々に決める。
  - 1 つの pull request の `apply_request` の記録は、1 つの repository のものなので、C は、多くても一方の反映である。
  - 例：A の apply request の反映のコミットが、B も変えていたら、A では反映によるもの、B では反映によらないものになり、B が壊れた状態になる。
  - なぜ repository まで比べるか：1 つの git リポジトリに repository が複数あるので、ほかの repository の反映が、このディレクトリも変えたことに気づくため。

## 壊れた repository

壊れた状態と直し方は、[壊れた repository](../backend/broken.md) と同じである。違うのは、比べる範囲がディレクトリの中だけになることである。

- `repair.revert`：先頭のコミットの、ディレクトリの中の木が、`last_good` のコミットのディレクトリの中の木と同じである。差分は、ディレクトリの中だけにする（`outside_directory`）。
  - worker が `last_good` を空に戻す条件も、同じくディレクトリの中の木で比べる。
  - ディレクトリの外（プロジェクトのコード）は、比べない。なぜ：外は、プロジェクトの PR で変わるもので、Logiforge の版ではないから。
- `repair.accept`：先頭のコミットは、認めた記録の artifact を、ディレクトリの中の `artifacts/sha256/` に 1 つ加えるだけにする（[壊れた repository](../backend/broken.md) の「直すための apply request」）。認めるディレクトリの中に、`tree/` と `artifacts/` の外のファイルがあれば断る。
- `main` の書き換えは、`scanned` が git の `main` の並びになくなったときに見つける。そのとき、ディレクトリの repository ごとに決める。
  - その repository の `latest` が、新しい `main` の first-parent の並びに残っていれば、版は消えていないので、壊さない。
  - 残っていなければ、`main_rewritten` として壊れた状態にする。`last_good` は、その `latest`（すでに壊れていれば、今の `last_good`）である。
  - なぜ repository ごとか：守る設定がないと、コードだけの force push はふつうに起きるから。ディレクトリの版を消さない書き換えで、壊れた状態にしない。
- 壊れた状態は、git リポジトリ全体の repository と同じく、repository ごとに 1 か所、置き場の `keep/<repository>/broken` に持つ（[壊れた repository](../backend/broken.md) の「壊れている状態」）。
  - 書き換えのあとに版が増えなくても、この記録だけで示せる。

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

- 部分木の引用の `commit` は、その repository の版のコミットにする。
  - 版でないコミット（コードのコミット）を引用したら、[差分の検査](../backend/checks.md) で拒む。
  - なぜ：版でないコミットには、スナップショットツリーがないから。
