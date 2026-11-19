---
name: logiforge
description: Logiforge の木を CLI（logiforge）で読み書きするときに使う。木の今の状態を読む、着手できる作業を探す、apply request を出す、request_changes や反映の失敗のあとに直す、壊れた repository を直す、人への依頼（PVR、判断の依頼）を起票する、のどれかをするとき。apply request をレビューするときは logiforge-review を使う。
---

# Logiforge を使う

エージェントが opener として、Logiforge の木を読み、apply request を出し、直すときに使う。
apply request のレビューは、[logiforge-review](../logiforge-review/SKILL.md) に書く。

言葉は、[用語](../../../docs/concepts/glossary.md) に従う。
コマンドとフラグの正本は、`logiforge help` と [コマンドの一覧](../../../design/cli/commands.md) である。

## 準備

- トークンとバックエンドの URL を置く：`logiforge auth login --with-token --host <URL>`（トークンは標準入力から渡す）。
  - トークンの種類とスコープは、[CLI の認証](../../../design/cli/auth.md) に従う。
- git にトークンを渡す：`logiforge auth setup-git`
- 木を clone する：`logiforge repo clone <repository>`。以降のコマンドは、clone の中で動かす。
- セッション ID を、環境変数 `LOGIFORGE_SESSION_ID` に置く。
  - CLI は作らない。自分で一意な値を決める。
  - 同じ apply request をレビューする reviewer とは、別の値にする（[保証の範囲](../../../docs/apply-request/review.md#保証の範囲)）。
- 出力を機械的に読むときは、どのコマンドにも `--json` を付ける。

## 今の状態を読む

- 木の概要：`logiforge tree view`
- ノードの一覧：`logiforge tree nodes [--root <ノード>] [--depth <n>]`
- ノードの詳細と履歴：`logiforge node view <ノード>`、`logiforge node history <ノード>`
- 検証結果とレポート：`logiforge verdict view <検証結果>`
- artifact の中身：`logiforge artifact get <sha256> [-o <ファイル>]`
- 木の全体を書き出す：`logiforge tree export --format json|markdown`
- 作業が終わったか：`logiforge tree completion`（[作業の終わりの判定](../../../docs/work/README.md#作業の終わりの判定)）
- `tree view`、`tree nodes`、`tree export`、`node view`、`artifact get` は、`--commit <SHA>` で、過去の `main` の時点を読める。
- 値と要再検討の意味は、[真偽値の伝播](../../../docs/tree/propagation.md) に従う。

## 作業を選ぶ

`logiforge work list [--subtree <ノード>] [--kind <種類>,...]` で、着手できる作業を探す（[着手できる作業](../../../docs/work/README.md)）。
ノードを対象にした open の apply request がある作業は、既定では出ない。それも見るときは `--include-claimed` を付ける。
種類ごとに、次のことをする。

- `undecided_leaf`：葉を検証して `verdict.submit` を出す。1 つの検証で決められないなら、`story.create` で分解する。
- `needs_reconsideration`：[要再検討の仮説に対してすること](../../../docs/tree/falsification.md#要再検討の仮説に対してすること) のどれかをする。
- `fix_waiting`：自分が出した apply request を直す（下の「apply request を直す」）。
- `human_request_answered`：回答を引用して `verdict.submit` を出す（下の「人への依頼」）。
- `citation_changed`：引用元の変化を確かめ、要るなら検証結果を差し替える（[引用元の変化](../../../docs/tree/verdict.md#引用元の変化)）。
  - 差し替えは、`logiforge verdict new ... --supersedes <検証結果>` で出す。`--supersedes` が無いと、検証結果が 2 つ並ぶ。
- `repair_repository`：壊れた repository を直す（下の「壊れた repository を直す」）。
- `review_waiting`：[logiforge-review](../logiforge-review/SKILL.md) を、別のセッションで使う。

## apply request を出す

1. `git fetch origin` し、`origin/main` から新しいブランチを作る。`main` は使えない。
2. ひな形を作る（[ひな形](../../../design/cli/local.md#ひな形)）。
   - `story.create`：`logiforge story new --parent <仮説> --title <タイトル> --summary <概要> --hypothesis <主文>...`
   - `verdict.submit`：`logiforge verdict new --hypothesis <仮説> --value true|false --report <ファイル> [--artifact <ファイル>]... [--cite ...]...`
   - `story.inactivate`：`logiforge story inactivate <ストーリー> --reason <理由>`
   - `node.edit`：`logiforge node edit <仮説> --append-background <ファイル>`
3. 要るなら、できたファイルを手で直す。
4. `logiforge check` で検査する。誤りが無くなるまで直す。
5. `logiforge commit -m <メッセージ>` でコミットする。
   - `git commit` ではなく、これを使う。セッションの trailer を付けるため。trailer の無い先頭のコミットは `approve` できない。
6. `git push -u origin <ブランチ>`
7. `logiforge ar create --branch <ブランチ> [--description <説明>]`

- 各操作の決まりは、[apply request](../../../docs/apply-request/README.md#操作) からたどる。
- 審査で何を見られるかは、[論理レビューの審査観点](../../../docs/apply-request/review-criteria.md) に書いてある。出す前に、自分で確かめる。
- 説明欄は、レビューの材料に入らない。審査に要ることは、レポートや背景情報に書く。
- 偽と結論するときは、2 つの操作を 1 つの apply request にまとめる（[偽と結論する](../../../docs/tree/falsification.md#偽と結論する)）。
- `ar create` が失敗して送り直すときは、出力された `Idempotency-Key` を `--idempotency-key` に渡す。

## apply request を直す

`logiforge ar view <apply request> --json` で、レビューの本文（`reviews`）と反映の失敗（`merge_failures`）を読む。

- `request_changes`、`stale_target`、`invariant_violation`：同じブランチでファイルを直し、`logiforge commit` して push する。
  - push すると新しい先頭のコミットになり、レビューからやり直しになる。
- `conflict`：ブランチを今の `origin/main` に合わせて直し、`logiforge check` してから push する。
- `snapshot_pending`、`merge_forbidden`：opener が直すものではない。reviewer の `approve` し直しを待つ。
- `repository_broken`：repository が壊れているあいだは、直すための apply request しか反映されない。先に下の「壊れた repository を直す」で直し、そのあと reviewer の `approve` し直しを待つ。
- レビューの鎖が切れた（レビューの API が `not_open` を返す）：`logiforge ar close <apply request> --reason <理由>` し、同じブランチから `ar create` し直す。
- 取り下げるとき：`logiforge ar close <apply request> --reason <理由>`
- 反映か close のあと、手元のブランチは `logiforge ar cleanup` で消せる。

## 壊れた repository を直す

壊れた repository では、木を読むコマンドが、最後に正しかった時点の木を出し、終了コード 1 で終わる（[壊れているあいだ](../../../docs/apply-request/repair.md#壊れているあいだ)）。
戻すか認めるかは、[直し方](../../../docs/apply-request/repair.md#直し方) で決める。

1. `logiforge repair revert` か `logiforge repair accept`。ブランチとコミットを作る。
2. `git push -u origin <ブランチ>`
3. `logiforge ar create --branch <ブランチ> --kind repair.revert|repair.accept`

## 人への依頼

計算機の中で検証できない葉や、ストーリーが尽きた要再検討の仮説は、人に頼む（[人への依頼](../../../docs/human-requests/README.md)）。

- 起票：`logiforge human-request create --node <ノード> --kind pvr|decision --body <ファイル>`
- 状態を見る：`logiforge human-request list [--kind pvr|decision] [--state open|answered] [--node <ノード>]`、`logiforge human-request view <人への依頼>`
- 回答が来たら：`logiforge verdict new` に `--cite answer:<人への依頼>` を付け、回答から値を判断したレポートで出す。
  - 回答をそのまま値にしない。判断の過程も審査の対象である。

## してはいけないこと

- `main` に直接 push しない。反映によらない変更は、repository を壊す（[壊れた repository](../../../docs/apply-request/repair.md#壊れた-repository)）。
- 自分の書いた apply request を、同じセッションでレビューしない。
- `node.edit` で、主文、タイトル、概要を変えない。
