# 反映かの判定

worker が、`main` の新しいコミットごとに、そのコミットが反映（論理レビューを通った apply request の merge）によるものかを、どう確かめるかを確かめるときに読む。
反映によらないコミットを見つけたあとの扱いは、[壊れた repository](../broken.md) に書く。

## 読む順

1. このページ：考え方、反映のコミットの条件、GitForge の API、重さ
2. [repository の用意のコミット](init.md)：用意のコミット

## 考え方

- 反映によるものかは、中身と、GitForge の pull request と review だけで決める。だれが merge したか、どこから（Logiforge か、GitForge の画面か）merge したかは、使わない。
  - 使うもの：`main` のコミットの中身、元の pull request の状態と先頭のコミットの SHA、その pull request の review（[数える review](../../adapters/pull-requests.md#数える-review)）。
  - なぜ：merge する主体を 1 つ（書き込み主体）に絞らずに済むから。承認された apply request なら、reviewer のトークンで merge しても、人が GitForge の画面で merge しても、正しい反映になる。
- コミットのメッセージの trailer は、判定に使わない。なぜ：`main` に push できる人なら、だれでも書けるから。
- worker は、GitForge を読むだけである。worker のトークンには、書く権限を持たせない（[worker のトークン](../../adapters/github-app.md#worker-のトークン)）。

## 反映によるコミット

`main` のコミット C（1 つ前の `main` のコミットを P とする）が、次のどちらかなら、反映によるものとする。

- repository の用意のコミット（[repository の用意のコミット](init.md)）
- apply request の反映のコミット（下の「apply request の反映のコミット」）

どちらでもなければ、反映によらないコミットとする。

- コミットの記録の警告に、満たさなかった条件を書く。
- ディレクトリの repository では、そのディレクトリを変えたコミット（[版](../../directory/history.md#版)）にだけ判定し、比べる範囲をディレクトリの中にする（[ディレクトリ方式の履歴](../../directory/history.md) の「反映かの判定」）。
- 壊れているあいだは、直すための apply request の反映のコミットだけを、反映によるものとする。

## apply request の反映のコミット

次のすべてを満たす。

1. C を merge した pull request が、GitForge にある。向き先が `main` で、merge のコミットが C である（下の「GitForge の API」）。その先頭のコミットを H とする。
2. C の形が、次のどちらかである。
   - squash merge：親が P だけである。
   - merge のコミット：最初の親が P で、2 つ目の親が H である。
   - rebase による merge（複数のコミットが並ぶもの）は、受け付けない。なぜ：途中のコミットの木が、どの先頭のコミットの木とも合わないから。
3. その pull request の review で、反映の手順の手順 1 の条件を満たす（[反映の手順](../merge.md)）。
   - その apply request の、すべての先頭のコミットに対するレビューのうち、最後の決めるレビュー（`approve` か `request_changes`）が、H への `approve` である。
     - なぜ H に限らないか：H1 への `approve` のあと H2 に `request_changes` が付いても、ブランチを H1 に戻せば、H1 への最後の決めるレビューは `approve` のままだから。
4. P の木に、H の差分（ブランチのスナップショット）を重ねられ、重ねた木の全体が、C の木と同じである。
   - H のスナップショットがなければ、`refs/pull/<number>/head` を fetch して作る。
5. 重ねた木が、反映の手順の手順 3 の検査（対象ノードの変化、差分の検査）を、P を今の `main` として満たす。
   - 直すための apply request（`repair.revert`、`repair.accept`）は、手順 3 のかわりに、直す条件を確かめる（[壊れた repository](../broken.md) の「直すための apply request」）。

## GitForge の API

- worker は、見回りで新しい `main` のコミットがあるとき、反映した pull request の一覧を 1 回読み、`merge_commit_sha` で新しいコミットごとの pull request を引く（[一覧の読み](../list-reads.md#どの一覧の-api-で読むか)）。
  - 引くのは、merge されていて、`merge_commit_sha` が C で、向き先が `main` のものである。
  - なぜ：コミットごとに pull request を問い合わせると、取り込みが遅れて追いつくとき、REST の回数がコミットの数に比例するから。
- review は、見つけた pull request ごとに、その pull request の review の一覧を、その場で読む（GitHub は GraphQL の 1 回、Forgejo は `GET /repos/{owner}/{repo}/pulls/{number}/reviews`。[数える review](../../adapters/pull-requests.md#数える-review)）。
  - 読むのは、新しい `main` のコミットを merge した pull request の分だけである。数は、反映の数と同じである。
- API が答えないときは、C を取り込まずに、次の見回りでやり直す。`latest` は進めない。
  - なぜ：判定できないコミットを、正しいとも壊れているとも記録しないため。
- その pull request の決める review（`approve` か `request_changes`）に、書いた人が書く権限を持つかを読めないものが 1 つでもあれば、API が答えないときと同じに、C を取り込まずに、次の見回りでやり直す。
  - なぜ：読めない review を数えないまま判定すると、読めなかっただけで、反映によらないコミットとして repository を壊れた状態にしてしまうから。
  - 下の 60 秒の待ちは使わない。読めるようになるまでやり直す。読めないあいだは `latest` が進まず、worker はログに理由を出す。
  - 詳細の読みと反映の手順では、読めない review は、数えずに警告するだけである（[数える review](../../adapters/pull-requests.md#数える-review)）。
- 反映した pull request の一覧の読み方（どこで読むのをやめるか）は、[一覧の読み](../list-reads.md#反映した-pull-request-の一覧worker) に書く。
- C を merge した pull request が見つからないときは、worker が C を初めて見てから 60 秒までは、決めずにやり直す。
  - なぜ：GitForge（Forgejo など）は、merge のすぐあと、merge のコミットから pull request を引けないことがあるから。
  - コミットの時刻は使わない。push する人が、未来の時刻にできるから。初めて見た時刻は、worker のプロセスの中だけに持つ。

## 重さ

`main` のコミット 1 つの判定に、次のものがかかる。

| もの | 量 | 目安 |
|---|---|---|
| GitForge の API | 見回りごとに、反映した pull request の一覧 1 ページ（新しいコミットの数によらない） | 100〜300 ms |
| review を読む | merge した pull request 1 つに 1 回 | 100〜300 ms |
| 差分を重ねる計算 | 先頭のコミットの差分のファイルの数に比例する | 数 ms |
| 重ねた木の検査 | 木のノードの数に比例する | [スナップショットの時間と大きさ](../../estimates/snapshot.md#スナップショットツリーを作る時間) の「組み立てと検査」 |

- 合わせて、ノード 4,000 の木でも、コミット 1 つに 0.6 秒ほどである。1 コミットの取り込み（ノード 4,000 で約 460 ms）に足すと、約 2.3 倍になる。
- 重ねた木は、そのコミットのスナップショットツリーと同じものになるので、検査の結果を、スナップショットツリーを作るのに使い回せる。そのときは、足す時間は、API の待ちのほかは数 ms になる。
- 判定は、新しいコミットにだけ行う。取り込み済みのコミットは、判定し直さない。
