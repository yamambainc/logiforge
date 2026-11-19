---
name: logiforge-review
description: Logiforge の apply request を reviewer として審査し、approve、request_changes、comment のどれかを付けるときに使う。着手できる作業に review_waiting があるとき、apply request（AR、apr_…）のレビューや論理レビューを頼まれたとき、`logiforge ar material` や `logiforge ar review` を使う場面では、Logiforge の名前が無くても使う。ストーリーの十分性や、検証結果の根拠を審査するときも使う。apply request を出す・直すときは logiforge を使う。
---

# Logiforge の apply request をレビューする

エージェントが reviewer として、[apply request](../../../docs/apply-request/README.md) を審査し、結果を付けるときに使う。
論理レビューの仕組みの正本は [論理レビュー](../../../docs/apply-request/review.md) である。
apply request を出す・直す側の使い方は、[logiforge](../logiforge/SKILL.md) に書く。

## 何をレビューするか

この節は、ほかのスキルやリンクを開かなくても読めるように書く。

- Logiforge は、答えの分からない問いを仮説に分けて検証していく過程の、仮説と根拠を記録する正本である。
- 記録は、スナップショットツリーという木である。
  - 根は、問いを表す仮説である。
  - 仮説の子はストーリー、ストーリーの子は仮説で、交互に並ぶ。
  - ストーリーは、「子仮説がすべて真なら、親仮説は真になる」という論理のつながりである。
  - active な子ストーリーを持たない仮説（葉）に、検証結果（値とレポート）を付ける。
- 値を入れるのは検証結果だけで、上の値は自動で決まる（ストーリーは子仮説の AND、仮説は active な子ストーリーの OR）。
  - そのため、通した誤った `true` は、根の結論まで自動で伝わる。
- スナップショットツリーへの変更は、すべて apply request で出す。apply request は、reviewer が `approve` すると反映される。
- reviewer が審査するのは、論理と根拠である。
  - ストーリーの子仮説で、親仮説が本当に言えるか。
  - 検証結果のレポートが、値を本当に支えているか。
  - 形の決まり（文字数、操作の種類など）は、Logiforge が検査する。

### 別のコンテキストでレビューする理由

- opener と同じ文脈で読むと、opener の推論をそのまま受け入れてしまい、審査にならない。
- そのため、差分を書いたのとは別のセッションで、レビューの材料だけを読んで審査する。
  - 材料には、apply request の説明欄を含めない。opener の推論が入るから。
- 1 つの apply request ごとに新しいコンテキストで始めると、前のレビューに引きずられず、同じ材料なら同じ判断になりやすい。

## 準備

- トークンとバックエンドの URL を置く：`logiforge auth login --with-token --host <URL>`（トークンは標準入力から渡す）。
- repository の名前を、環境変数 `LOGIFORGE_REPO` に置くか、各コマンドに `--repo <repository>` を付ける。
- 出力を機械的に読むときは、どのコマンドにも `--json` を付ける。

## 始める前に

- 差分を書いたどのセッションとも別のセッション ID を、`LOGIFORGE_SESSION_ID` に置く。
  - 差分を書いたセッションからのレビューは、種類によらず、`same_session` で拒まれる（[反映の条件](../../../docs/apply-request/review.md#反映の条件)）。
- 自分が書いた差分や、自分が指示した差分はレビューしない。セッションを変えても、文脈は分かれないから。
- 1 つの apply request ごとに、新しいコンテキストで始める（[想定する使い方](../../../docs/apply-request/review.md#想定する使い方)）。
  - 前のレビューの文脈に、引きずられないため。

## 手順

1. レビュー待ちを探す：`logiforge work list --kind review_waiting`
   - 各項目の `subject_id` が、apply request の識別子である（text の出力では 2 つ目の列）。
2. 材料を読む：`logiforge ar material <apply request> --json`
   - 材料の中身は、[レビューの材料](../../../docs/apply-request/review.md#レビューの材料) に書いてある。
   - 応答の `commit` を控える。手順 5 でそのまま使う。
3. 要るなら、引用された artifact を読む：`logiforge artifact get <sha256> -o <ファイル>`
   - プログラムによる検証は、artifact から再現できるかを確かめる。
4. 審査する（下の「審査すること」）。
5. 結果を付ける（下の「結果の付け方」）。

材料だけで審査する。次のものは読まない。

- apply request の説明欄（`ar view --json` の `description`）
- GitForge の pull request の画面と議論

なぜ：opener の推論がそのまま入り、コンテキストを分けた意味がなくなるから。

## 審査すること

- 操作ごとの観点は、[論理レビューの審査観点](../../../docs/apply-request/review-criteria.md) に従う。
  - 材料の `operations` の `kind` で、どの節を使うかを決める。
  - `story.inactivate` と false の `verdict.submit` が同じ仮説に並んでいれば、「偽と結論する」の観点も使う。
- 直すための apply request（材料に `repair` がある）では、[直し方](../../../docs/apply-request/repair.md#直し方) に従う。
  - `repair.revert`：`repair.changes` の変化をすべて取り消してよいかを見る。
  - `repair.accept`：`repair.changes` の変化を、ふつうの apply request の差分と同じ観点で審査する。
- 形の検査（スナップショットツリーの形、4 種類の操作として読めるか）は、Logiforge が行う。reviewer は、論理を見る。

## 結果の付け方

```
logiforge ar review <apply request> --approve|--request-changes|--comment \
  --commit <材料の commit> --body <本文> --prompt-revision <リビジョン>
```

- 種類は、次のように選ぶ。
  - `--approve`：どの観点にも問題が無い。
  - `--request-changes`：opener が直せる問題がある。
  - `--comment`：反映を止めない指摘だけがある。
- `--body` には、次のことを書く。
  - `approve`：確かめた観点と、問題が無いと判断した理由。
  - `request_changes`：問題ごとに、対象のノード、外れた観点、直し方。
- `--prompt-revision` には、使ったプロンプト（このスキル）のリビジョンを書く。`approve` では必須である。
  - リビジョンは、logiforge のリポジトリでの、このファイルのコミットの SHA である。スナップショットツリーの git リポジトリのコミットではない。
  - 求め方：`git -C <logiforge のリポジトリ> log -1 --format=%H -- .agents/skills/logiforge-review/SKILL.md`
- 直しても通らない apply request（前提の主張そのものが誤っているなど）は、却下する：`logiforge ar close <apply request> --reason <理由>`
  - 却下できるのは、差分を書いたどのセッションとも別のセッションだけである（[状態](../../../docs/apply-request/README.md#状態)）。差分を書いたセッションからの close は、取り下げになる。

## 付けたあと

`ar review` の応答の `apply_request` で、結果を確かめる。

- `state` が `merged`：反映された。
- `merge_failures` がある：反映できなかった。apply request は open のまま残る。
  - `snapshot_pending`：少し待って、材料を読み直し、`approve` し直す。
  - `merge_forbidden`：この reviewer の主体に merge の権限が無い。権限のある reviewer に任せる。
  - `repository_broken`：repository が壊れていて、直すための apply request でないので反映されない。直すのは別の作業なので、reviewer がすることは無い。
  - `stale_target`、`invariant_violation`、`conflict`：opener が直す。reviewer がすることは無い。
- `warnings` に、reviewer と作成者が同じ主体だという警告があれば、頼んだ人に伝える。

`ar review` が誤りを返したとき：

- `same_session`：差分を書いたセッションと同じセッション ID を使っている。レビューをやめ、別のセッションでやり直す。
- `commit_conflict`：材料を読んだあとに push された。手順 2 から、やり直す。
- `snapshot_pending`：少し待って、送り直す。
- `not_open`：レビューの鎖が切れたか、もう open でない。opener の作業なので、レビューをやめる。

## 報告

レビューを頼んだ人や仕組みには、次のことを返す。

- apply request の識別子
- 付けた種類と、レビューした `commit`
- 判断の理由（`--body` の要約）
- 付けたあとの `state` と、`merge_failures` があればその理由
