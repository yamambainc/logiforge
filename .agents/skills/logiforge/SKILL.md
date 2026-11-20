---
name: logiforge
description: Logiforge（仮説と根拠を記録する正本）を、opener として CLI（logiforge）で使うときに使う。スナップショットツリーの今の状態や値を読む、着手できる作業（work list）を探す、ストーリーや検証結果の apply request を出す、request_changes や反映の失敗（conflict、stale_target など）のあとに直す、壊れた repository を直す、人への依頼（PVR、判断の依頼）を起票し回答を検証結果にする、のどれかをするとき。`logiforge` コマンド、`LOGIFORGE_` の環境変数、仮説・ストーリー・検証結果・apply request の語が出てきたら、Logiforge の名前が無くても使う。apply request をレビューするときは logiforge-review を使う。
---

# Logiforge を使う

エージェントが opener として、Logiforge のスナップショットツリーを読み、apply request を出し、直すときに使う。
apply request のレビューは、[logiforge-review](https://github.com/yamambainc/logiforge/blob/main/.agents/skills/logiforge-review/SKILL.md) に書く。

言葉は、[用語](https://github.com/yamambainc/logiforge/blob/main/docs/concepts/glossary.md) に従う。
コマンドとフラグの正本は、`logiforge help` と [コマンドの一覧](https://github.com/yamambainc/logiforge/blob/main/design/cli/commands.md) である。

## Logiforge とは

この節は、リンクを開かなくても読めるように書く。詳しい決まりは、各節のリンク先にある。

### 解く課題と位置づけ

- 答えの分からない問いでは、問いを仮説に分け、検証し、外れたら別の道を探す、を繰り返す。
- この過程は、会話やメモに散らばりやすい。散らばると、どの結論がどの根拠に基づくかを、後から辿れない。
- Logiforge は、仮説と根拠を記録する正本（system of record）である。
  - GitForge（GitHub や Forgejo）がコードの正本であるのと同じ位置づけである。
  - エージェントを動かす仕組みではない。エージェントは CLI で、人は WebUI で、同じ記録を読み書きする。

### スナップショットツリー

- 1 つの問いを、根の仮説とする木で表す。これをスナップショットツリーと呼ぶ。
  - 1 つの repository に、スナップショットツリーが 1 つある。
- 仮説とストーリーが、交互に並ぶ。
  - 仮説：主文（50 字以下）と背景情報からなる。
  - ストーリー：「子仮説がすべて真なら、親仮説は真になる」という論理のつながり。タイトルと概要からなる。
    - この保証は、兄弟ストーリーに頼らず、そのストーリーだけで成り立つ（[ストーリー](https://github.com/yamambainc/logiforge/blob/main/docs/tree/README.md#ストーリー)）。
    - 親仮説を場合や側面に分けるときは、分けたものを兄弟ストーリーにせず、1 つのストーリーの子仮説として並べる。
    - なぜ：仮説の値は、子ストーリーの OR だから。
    - 親仮説の一部しか言えないストーリーが true になると、親仮説の残りを確かめないまま、親仮説が true になる。
  - 1 つの仮説に、別々のストーリーを複数立ててよい。
- active な子ストーリーを持たない仮説を、葉と呼ぶ。検証するのは葉だけである。
- 検証結果（verdict）は、葉に付ける値（`true` か `false`）とレポートの組である。
  - レポートは、本文、artifact（プログラムと実行結果、データ、文献など）、引用（ほかのノードや artifact）からなる。
- 欠陥のあるストーリーは消さず、inactive にする。inactive なストーリーは、値の計算から外れる。

### 真偽値の伝播

- 値は、`true`、`false`、`null`（未決）のどれかである。
- 値を入れられるのは、検証結果だけである。ほかのノードの値は、自動で決まる。
  - ストーリーの値は、子仮説の AND である。1 つでも `false` なら `false`、すべて `true` なら `true`。
  - 仮説の値は、active な子ストーリーの OR である。1 つでも `true` なら `true`。
  - `false` は、仮説より上には自動で伝わらない。別のストーリーで検証できる可能性が残るから。
- active な子ストーリーがすべて `false` の仮説を、要再検討と呼ぶ。別のストーリーを立てるか、偽と結論する。

### 変更は apply request で行う

- スナップショットツリーを変えられるのは、承認された apply request（AR）の反映だけである。
- apply request は、次の 4 種類の操作の組である。
  - `story.create`：ストーリーを、子仮説と一緒に作る。
  - `story.inactivate`：ストーリーを inactive にする。
  - `verdict.submit`：葉に検証結果を出す。
  - `node.edit`：仮説の背景情報に、文章を加える。
- 差分を書いたのとは別のセッションの reviewer が `approve` すると、反映される。
  - 別のセッションにするのは、書いた本人の推論に引きずられずに、論理を審査するためである。
  - 差分が更新されたら、`approve` はやり直しになる。

### 役割

- opener：apply request を出す主体。問いの分解、葉の検証、人への依頼をする。エージェントでも人でもよい。
- reviewer：apply request を審査する主体。エージェントでも人でもよい。
- 利用者：WebUI を使う人。repository を作り、人への依頼に回答し、apply request をレビューする。
- 計算機の中で検証できない葉は、人への依頼（PVR）で人に検証してもらう。

### git リポジトリとバックエンド

- スナップショットツリーの正本は、GitForge の git リポジトリ（またはその中の 1 つのディレクトリ）にある。
  - ノードはディレクトリで、中身は md のファイルである。
- apply request は、GitForge の pull request である。opener はブランチを push し、CLI でそれを apply request にする。
- バックエンド（Logiforge のサーバ）は、git リポジトリを取り込んで検査し、値を計算して、API で返す。
  - `approve` されたら、バックエンドが pull request を merge する。
  - 値はファイルに保存されない。今の値は、clone ではなく CLI（API）で読む。
- apply request を通さずに `main` を変えると、repository は「壊れた」状態になる。

## 準備

- トークンとバックエンドの URL を置く：`logiforge auth login --with-token --host <URL>`（トークンは標準入力から渡す）。
  - トークンの種類とスコープは、[CLI の認証](https://github.com/yamambainc/logiforge/blob/main/design/cli/auth.md) に従う。
- git にトークンを渡す：`logiforge auth setup-git`
- スナップショットツリーの git リポジトリを clone する：`logiforge repo clone <repository>`。以降のコマンドは、clone の中で動かす。
- セッション ID を、環境変数 `LOGIFORGE_SESSION_ID` に置く。
  - CLI は作らない。自分で一意な値を決める。
  - 同じ apply request をレビューする reviewer とは、別の値にする（[保証の範囲](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/review.md#保証の範囲)）。
- 出力を機械的に読むときは、どのコマンドにも `--json` を付ける。

## 今の状態を読む

- スナップショットツリーの概要：`logiforge tree view`
- ノードの一覧：`logiforge tree nodes [--root <ノード>] [--depth <n>]`
- ノードの詳細と履歴：`logiforge node view <ノード>`、`logiforge node history <ノード>`
- 検証結果とレポート：`logiforge verdict view <検証結果>`
- artifact の中身：`logiforge artifact get <sha256> [-o <ファイル>]`
- スナップショットツリーの全体を書き出す：`logiforge tree export --format json|markdown`
- 作業が終わったか：`logiforge tree completion`（[作業の終わりの判定](https://github.com/yamambainc/logiforge/blob/main/docs/work/README.md#作業の終わりの判定)）
- `tree view`、`tree nodes`、`tree export`、`node view`、`artifact get` は、`--commit <SHA>` で、過去の `main` の時点を読める。
- 値と要再検討の意味は、[真偽値の伝播](https://github.com/yamambainc/logiforge/blob/main/docs/tree/propagation.md) に従う。

## 作業を選ぶ

`logiforge work list [--subtree <ノード>] [--kind <種類>,...]` で、着手できる作業を探す（[着手できる作業](https://github.com/yamambainc/logiforge/blob/main/docs/work/README.md)）。
ノードを対象にした open の apply request がある作業は、既定では出ない。それも見るときは `--include-claimed` を付ける。
種類ごとに、次のことをする。

- `undecided_leaf`：葉を検証して `verdict.submit` を出す。1 つの検証で決められないなら、`story.create` で分解する。
- `needs_reconsideration`：次のどれかをする（[要再検討の仮説に対してすること](https://github.com/yamambainc/logiforge/blob/main/docs/tree/falsification.md#要再検討の仮説に対してすること)）。
  - まず `logiforge tree nodes --root <仮説> --depth 2` で、子ストーリーと、`false` になった子仮説を確かめる。
  - 別の論理で、ストーリーを新しく作る（`story new --parent <仮説>`）。
  - 仮説を直接否定する証拠があれば、偽と結論する（下の「apply request を出す」）。
  - ストーリーが尽きたが直接の反証が無いなら、種類が `decision` の人への依頼を起票する。本文には、問い、試したストーリーとその結果、判断してほしいことを書く。
- `fix_waiting`：自分が出した apply request を直す（下の「apply request を直す」）。
- `human_request_answered`：回答を引用して `verdict.submit` を出す（下の「人への依頼」）。
- `citation_changed`：引用元の変化を確かめ、要るなら検証結果を差し替える（[引用元の変化](https://github.com/yamambainc/logiforge/blob/main/docs/tree/verdict.md#引用元の変化)）。
  - 差し替えは、`logiforge verdict new ... --supersedes <検証結果>` で出す。`--supersedes` が無いと、検証結果が 2 つ並ぶ。
- `repair_repository`：壊れた repository を直す（下の「壊れた repository を直す」）。
- `review_waiting`：[logiforge-review](https://github.com/yamambainc/logiforge/blob/main/.agents/skills/logiforge-review/SKILL.md) を、別のセッションで使う。

## apply request を出す

1. `git fetch origin` し、`origin/main` から新しいブランチを作る。`main` は使えない。
2. ひな形を作る（[ひな形](https://github.com/yamambainc/logiforge/blob/main/design/cli/local.md#ひな形)）。
   - `story.create`：`logiforge story new --parent <仮説> --title <タイトル> --summary <概要> --hypothesis <主文>...`
   - `verdict.submit`：`logiforge verdict new --hypothesis <仮説> --value true|false --report <ファイル> [--artifact <ファイル>]... [--cite ...]...`
     - 部分木は `--cite subtree:<ノード>` で引用する。コミットは書かない。
     - 引用した時点は、反映された `main` のコミットで決まる（[引用](https://github.com/yamambainc/logiforge/blob/main/docs/tree/verdict.md#引用)）。
   - `story.inactivate`：`logiforge story inactivate <ストーリー> --reason <理由>`
   - `node.edit`：`logiforge node edit <仮説> --append-background <ファイル>`
3. 要るなら、できたファイルを手で直す。
4. `logiforge check` で検査する。誤りが無くなるまで直す。
5. `logiforge commit -m <メッセージ>` でコミットする。
   - `git commit` ではなく、これを使う。セッションの trailer を付けるため。trailer の無い先頭のコミットは `approve` できない。
6. `git push -u origin <ブランチ>`
7. `logiforge ar create --branch <ブランチ> [--description <説明>]`

- 各操作の決まりは、[apply request](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/README.md#操作) からたどる。
- 審査で何を見られるかは、[論理レビューの審査観点](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/review-criteria.md) に書いてある。出す前に、自分で確かめる。
- 説明欄は、レビューの材料に入らない。審査に要ることは、レポートや背景情報に書く。
- 検証結果のレポートには、値を支える根拠と、その根拠から値を決めた理由を書く。プログラムで検証したなら、再現できるように、プログラムと実行結果を artifact に付ける。
- 偽と結論するときは、次の 2 つを 1 つの apply request にまとめる（[偽と結論する](https://github.com/yamambainc/logiforge/blob/main/docs/tree/falsification.md#偽と結論する)）。
  - その仮説の active な子ストーリーを、すべて `story.inactivate` する。
  - その仮説に、`false` の `verdict.submit` を出す。
  - 同じブランチで、先に `story inactivate` をすべて行い、そのあと `verdict new --value false` を作る。子ストーリーがすべて inactive になると、その仮説は葉になるから。
  - 仮説を直接否定する証拠が要る。「別のストーリーが見つからない」だけでは、偽と結論できない。
- `ar create` が失敗して送り直すときは、同じコマンドをもう一度実行する。同じブランチに自分の open の apply request があれば、それが返る。

## apply request を直す

`logiforge ar view <apply request> --json` で、ブランチ（`branch`）、レビューの本文（`reviews`）、反映の失敗（`merge_failures`）を読む。
直したら、同じブランチに push する。push すると新しい先頭のコミットになり、どの理由で直した場合も、レビューからやり直しになる。
修正待ち（`fix_waiting`）の apply request は、push しただけではレビュー待ちに戻らない。直し終えたら、`logiforge ar fixed <apply request>` で申告する。

- `request_changes`、`invariant_violation`：同じブランチでファイルを直し、`logiforge commit` して push する。
- `stale_target`、`conflict`：ブランチを今の `origin/main` に合わせて直す。
  - `stale_target` は、ブランチを分けたあとに、操作の対象ノードが `main` で変わったことを示す。今のノードを読み直し、差分がまだ正しいかを確かめる。
  1. `git fetch origin` し、`git rebase origin/main` で衝突を解く。
     - merge ではなく rebase にする。merge のコミットには、セッションの trailer が付かないから。rebase なら、先頭のコミットの trailer が残る。
     - rebase は、ブランチを書いたセッションで行う。trailer は元のセッションのままなので、別のセッションが衝突を解くと、そのセッションの書いた分が記録されない。別のセッションで行うときは、最後に `logiforge commit --allow-empty -m <メッセージ>` でコミットを 1 つ重ね、先頭のコミットにそのセッションを載せる。
  2. `logiforge check`
  3. `git push --force-with-lease`（rebase で履歴が変わるため）
- `snapshot_pending`、`merge_forbidden`：opener が直すものではない。reviewer の `approve` し直しを待つ。
- `repository_broken`：repository が壊れているあいだは、直すための apply request しか反映されない。先に下の「壊れた repository を直す」で直し、そのあと reviewer の `approve` し直しを待つ。
- 取り下げるとき：`logiforge ar close <apply request> --reason <理由>`
- 反映か close のあと、手元のブランチは `logiforge ar cleanup` で消せる。

## 壊れた repository を直す

壊れた repository では、スナップショットツリーを読むコマンドが、最後に正しかった時点のスナップショットツリーを出し、終了コード 1 で終わる（[壊れているあいだ](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/repair.md#壊れているあいだ)）。
戻すか認めるかは、[直し方](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/repair.md#直し方) で決める。

1. `logiforge repair revert` か `logiforge repair accept`。ブランチとコミットを作る。
2. `git push -u origin <ブランチ>`
3. `logiforge ar create --branch <ブランチ> --kind repair.revert|repair.accept`

## 人への依頼

計算機の中で検証できない葉や、ストーリーが尽きた要再検討の仮説は、人に頼む（[人への依頼](https://github.com/yamambainc/logiforge/blob/main/docs/human-requests/README.md)）。

- 起票：`logiforge human-request create --node <ノード> --kind pvr|decision --body <ファイル>`
- 状態を見る：`logiforge human-request list [--kind pvr|decision] [--state open|answered] [--node <ノード>]`、`logiforge human-request view <人への依頼>`
- 回答が来たら：`logiforge verdict new` に `--cite answer:<人への依頼>` を付け、回答から値を判断したレポートで出す。
  - 回答をそのまま値にしない。判断の過程も審査の対象である。
  - 回答だけで値を決められないときは、新しい人への依頼を起票する。1 つの依頼への回答は 1 回だけで、やりとりは重ねられない。

## してはいけないこと

- `main` に直接 push しない。反映によらない変更は、repository を壊し、直すまでほかの apply request が反映されなくなる（[壊れた repository](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/repair.md#壊れた-repository)）。
- 自分の書いた apply request を、同じセッションでレビューしない。`same_session` で拒まれるうえ、別の文脈で論理を確かめる意味がなくなる。
- `node.edit` で、主文、タイトル、概要を変えない。意味が変わると、それまでの審査と検証が成り立たなくなる。
