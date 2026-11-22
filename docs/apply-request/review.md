# 論理レビュー

apply request が、どういう条件で反映され、論理レビューと merge がどう行われるかを確かめるときに読む。
論理レビューの仕組みの正本は、このページである。

## 反映の条件

[apply request](README.md) が承認されているかは、GitForge の review と、その扱いの設定で決まる。Logiforge は、独自の数え方をしない。

- GitForge は、作者自身の `approve` を拒む。書いた人と確かめる人を分けるのは、この GitForge の仕組みである。
- Logiforge の merge の操作は、承認されていない apply request を merge しない。GitForge の設定によらない（[merge](#merge)）。
- GitForge の画面から直接 merge するときに承認を求めるのは、GitForge の設定である。`main` に、次の設定を掛けることを勧める（[GitForge の設定](#gitforge-の設定)）。
  - 承認を必須にする。
  - 新しい push で、古い `approve` を外す。
    - push のあとに、それまでの `approve` を残すか外すかは、GitForge の設定で決まる。Logiforge は、どのコミットへの `approve` かを見ない。
    - 外さなければ、`approve` のあとに push した差分も、承認済みのまま merge できる。push のたびにレビューし直させたいなら、この設定を掛ける。
- 設定しなければ、GitForge の画面からは、`approve` がなくても merge できる。これは GitForge の仕様どおりである。
- Logiforge が示す承認の状態（承認済み、変更を求められている、まだ）は、GitForge の review から決める。
  - 承認済みとするのは、書く権限を持つ人の `approve` があり、変更を求める review が残っていないときである。外された `approve` は数えない。
  - GitForge によっては、一覧では承認の状態が分からず、示さないことがある。1 件を読めば示す。

### GitForge の設定

GitForge ごとの手順は、次の頁に書く。

- GitHub：[main を守る](../adoption/github/README.md#main-を守る)
- Forgejo：[repository の用意](../../design/adapters/forgejo.md#repository-の用意)

- 論理レビューは、CLI を使う主体か、WebUI の利用者が行う。人も reviewer になれる。
  - WebUI のレビューは、CLI のレビューと同じ意味である。
  - WebUI でレビューできるのは、その repository に書ける利用者だけである（[役割](../roles/README.md#利用者)）。

## approve

- `approve` の前に、Logiforge は、木が壊れないかを、今の `main` で確かめる。
  - 確かめること：対象ノードが先に変わっていないか、差分を今の `main` に重ねられるか、重ねた木が不変条件を満たすか。
  - 通らなければ、`approve` を出さない。理由を返し、opener が直すべき理由なら、pull request にコメントを書いて修正待ちにする。
  - 向き先が `main` でない apply request は、確かめない（[main への直接の変更](README.md#main-への直接の変更)）。
- 通ったら、GitForge の pull request に `approve` を出す。merge はしない。
  - なぜ：`approve` は論理レビューの結果で、`main` に入れるのは別の判断だから。

## merge

- `approve` された apply request は、だれが merge してもよい。エージェントでも人でもよい。
- Logiforge から merge する：CLI、WebUI、API の merge の操作を使う。
  - merge は、呼んだ主体のトークンで行う。その主体に、GitForge で merge する権限が要る。
  - merge の直前に、Logiforge は、`approve` の前と同じ確かめを、今の `main` でやり直す。通らなければ、merge しない。
    - なぜやり直すか：`approve` のあとに、別の apply request が反映されているかもしれないから。
  - 承認されていなければ、merge しない。GitForge の承認を必須にする設定が merge を断ったときも、その理由を返す。
  - merge できるのは、`approve` と同じく、その repository に書ける主体である。opener も merge できる。
- GitForge の画面から直接 merge する：Logiforge は止めない。
  - `main` のコミットは、どう入ったかによらず受け入れる。ぶつかっていたら、あとの確かめで警告する（[main への直接の変更](README.md#main-への直接の変更)）。

## 起動

- 論理レビューを起動するのは、Logiforge の外の仕組み（CI など）である。
  - Logiforge は、論理レビューを起動しない。
- 外の仕組みは、レビュー待ちの apply request を、[着手できる作業](../work/README.md) の問い合わせで見つける。

## レビューの材料

Logiforge は、apply request ごとに「レビューの材料」を返す問い合わせを提供する。
材料は、次のものである。

- 差分そのもの
- 操作の対象ノードの、現在の値
- 対象ノードの親の仮説から根までの、各仮説の主文
- 兄弟ストーリーの、タイトルと概要と値
- 引用している部分木（先頭のコミットの木のもの）と artifact

対象ノードと兄弟ストーリーは、操作ごとに次のものである。

| 操作 | 対象ノード | 兄弟ストーリー |
|---|---|---|
| `story.create` | 親仮説 | 親仮説のほかの子ストーリー |
| `story.inactivate` | そのストーリー | 同じ親仮説のほかの子ストーリー |
| `verdict.submit`、`node.edit` | その仮説 | その仮説が属するストーリーと同じ親仮説を持つ、ほかのストーリー |

根の仮説への `verdict.submit` と `node.edit` には、兄弟ストーリーはない。根の仮説は、ストーリーに属さないから。

apply request の説明欄は、材料に含めない。

- なぜ：opener の推論がそのまま入り、コンテキストを分けた意味がなくなるから。

審査で確かめることは、[論理レビューの審査観点](review-criteria.md) に書く。

## 想定する使い方

次の使い方を想定する。ただし、要件ではない。

- 外の仕組みが、レビュー待ちの apply request を見つけるたびに、新しいコンテキストで reviewer を動かす。
- reviewer は、レビューの材料だけを読んで審査する。
- レビューを終えたら、その reviewer は使い捨てる。

これを要件にしない理由は、Logiforge からは確かめられないからである。
この使い方をすると、次の利点がある。

- 前のレビューの文脈に、引きずられない。
- 同じ材料なら、判断を再現しやすい。

## プロンプト

- レビューのプロンプトは、Logiforge 自体の git リポジトリに置く。
- プロンプトは差し替えられる。
- 標準のプロンプトを 1 つ同梱する。
  - 標準のプロンプトは、[logiforge-review](../../.agents/skills/logiforge-review/SKILL.md) のスキルである。
- プロンプトの変更は、Logiforge の範囲外とする。
  - プロンプトの変更は、その git リポジトリの通常のレビューに従う。

## 保証の範囲

GitForge が確かめること：

- reviewer は、apply request を作った人と別の GitForge のアカウントである。GitForge が、自分の pull request の `approve` を拒むから。
  - 書いた人と確かめる人を分ける保証は、この 1 つである。同じアカウントの別のセッションで書き、確かめることはできない。
- GitForge の画面からの merge で、承認が足りるか。承認を必須にする GitForge の設定があるときだけ確かめる（[GitForge の設定](#gitforge-の設定)）。

Logiforge が確かめること：

- Logiforge からの merge で、承認されているか。
  - GitForge の画面から直接出した review も、Logiforge を通した review と同じに扱う。
- WebUI のレビューでも、別のアカウントであることを、GitForge が確かめる。
  - 自分が指示した差分を、自分のアカウントで `approve` しないのは、人の責任である。
  - WebUI は、自分が作った apply request なら、`approve` の前にそのことを示す（[apply request と履歴](../webui/history.md)）。

Logiforge が review の本文に書くこと：

- client の `approve` ごとの、使ったプロンプトのリビジョン（人が読む文として）

対応しないこと：

- 同じアカウントを、複数の人やエージェントで使うこと
- Write を持つ主体による、review の本文、コメント、ラベルの書き換え
  - 認証基盤は、この仕様の範囲外である（[目的と範囲](../concepts/README.md)）。
