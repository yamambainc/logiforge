---
name: logiforge-review
description: Logiforge の apply request を reviewer として審査し、approve、request_changes、comment のどれかを付けるときに使う。着手できる作業に review_waiting があるとき、apply request（AR、pull request の番号）のレビューや論理レビューを頼まれたとき、`logiforge ar material` や `logiforge ar review` を使う場面では、Logiforge の名前が無くても使う。ストーリーの十分性や、検証結果の根拠を審査するときも使う。apply request を出す・直すときは logiforge を使う。
---

# Logiforge の apply request をレビューする

エージェントが reviewer として、[apply request](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/README.md) を審査し、結果を付けるときに使う。
論理レビューの仕組みの正本は [論理レビュー](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/review.md) である。
apply request を出す・直す側の使い方は、[logiforge](https://github.com/yamambainc/logiforge/blob/main/.agents/skills/logiforge/SKILL.md) に書く。

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
- スナップショットツリーへの変更は、すべて apply request で出す。reviewer が `approve` した apply request を、merge すると反映される。reviewer は merge しない（[merge](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/review.md#merge)）。
- reviewer が審査するのは、論理と根拠である。
  - ストーリーの子仮説で、親仮説が本当に言えるか。
    - 兄弟ストーリーに頼らず、このストーリーだけで判断する（[ストーリー](https://github.com/yamambainc/logiforge/blob/main/docs/tree/README.md#ストーリー)）。
    - 親仮説の一部の場合や側面だけを言うストーリーは、`request_changes` にする。
    - そのときの直し方は、分けたものをすべて、1 つのストーリーの子仮説として並べることである。
    - なぜ：仮説の値は、子ストーリーの OR だから。
    - 一部しか言えないストーリーを通すと、親仮説の残りを確かめないまま、親仮説が true になる。
  - 検証結果のレポートが、値を本当に支えているか。
    - 探索の対象の成果物（Logiforge で問いを探索しているプロジェクトが、最終的に作るもの）を根拠にした検証結果は、値を支えていても `request_changes` にする。
      - ソフトウェア開発なら、そのソフトウェアのソースコードとテストコード。
        - 例：開発中のソフトウェアのテストが通ったことを根拠にした検証結果。
      - 論文なら、tex プロジェクトの中のファイル。
        - 例：書いている論文の本文に書いてあることを根拠にした検証結果。
    - なぜ：Logiforge は、広い空間の探索結果を残して次へ進むためのツールで、成果物はその探索結果を整理したものにすぎないから。
      - 成果物を根拠にすると、探索の結果を、それを整理したもので裏付けることになり、順序が逆になる。
    - 検証のために別に書いたプログラムと、その実行結果の artifact は、根拠にしてよい。
    - そのときの直し方は、成果物に頼らない根拠（検証のためのプログラム、文献、人への依頼の回答など）で検証し直すことである。
  - 形の決まり（文字数、操作の種類など）は、Logiforge が検査する。

### 別のコンテキストでレビューする理由

- opener と同じ文脈で読むと、opener の推論をそのまま受け入れてしまい、審査にならない。
- そのため、差分を書いたのとは別のアカウントで、レビューの材料だけを読んで審査する。
  - 材料には、apply request の説明欄を含めない。opener の推論が入るから。
- 1 つの apply request ごとに新しいコンテキストで始めると、前のレビューに引きずられず、同じ材料なら同じ判断になりやすい。

## 準備

- トークンとバックエンドの URL を置く：`logiforge auth login --with-token --host <URL>`（トークンは標準入力から渡す）。
- repository の名前を、環境変数 `LOGIFORGE_REPO` に置くか、各コマンドに `--repo <repository>` を付ける。
- 出力を機械的に読むときは、どのコマンドにも `--json` を付ける。

## 始める前に

- 差分を書いたアカウントとは別の GitForge のアカウントのトークンを使う。作者自身の `approve` は、GitForge が拒む（[反映の条件](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/review.md#反映の条件)）。
- 自分が書いた差分や、自分が指示した差分はレビューしない。別のアカウントでも、同じ文脈でレビューすると、確かめる意味がなくなるから。
- 1 つの apply request ごとに、新しいコンテキストで始める（[想定する使い方](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/review.md#想定する使い方)）。
  - 前のレビューの文脈に、引きずられないため。

## 手順

1. レビュー待ちを探す：`logiforge work list --kind review_waiting`
   - 各項目の `subject_id` が、apply request の識別子である（text の出力では 2 つ目の列）。
2. 材料を読む：`logiforge ar material <apply request> --json`
   - 材料の中身は、[レビューの材料](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/review.md#レビューの材料) に書いてある。
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

- 操作ごとの観点は、[論理レビューの審査観点](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/review-criteria.md) に従う。
  - 材料の `operations` の `kind` で、どの節を使うかを決める。
  - `story.inactivate` と false の `verdict.submit` が同じ仮説に並んでいれば、「偽と結論する」の観点も使う。
- `main` の不変条件の破れを直す apply request も、ふつうの apply request と同じ観点で審査する（[main への直接の変更](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/README.md#main-への直接の変更)）。
- CLI が、最新のコミットは valid でないと標準エラーに警告しても、審査を続けてよい。
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
  - 却下は、apply request を作った人とは別の人が行う（[状態](https://github.com/yamambainc/logiforge/blob/main/docs/apply-request/README.md#状態)）。作った人からの close は、取り下げになる。

## 付けたあと

`ar review` が成功したら、レビューは GitForge に出ている。`approve` しても、merge はされない。merge は、opener か人が行う。

`ar review` が誤りを返したとき：

- `stale_target`、`invariant_violation`、`conflict`：`approve` が、今の `main` での確かめに通らなかった。`approve` は出ていない。opener が直す。reviewer がすることは無い。
- `commit_conflict`：材料を読んだあとに push された。手順 2 から、やり直す。
- `snapshot_pending`：少し待って、材料を読み直し、送り直す。
- `not_open`：もう open でない。レビューをやめる。
- 自分の GitForge のアカウントが作った apply request は、GitForge が `approve` を拒む。別のアカウントの reviewer に任せる。

## 報告

レビューを頼んだ人や仕組みには、次のことを返す。

- apply request の識別子
- 付けた種類と、レビューした `commit`
- 判断の理由（`--body` の要約）
- `approve` が確かめに通らなかったときは、その理由（誤りの `code` と `detail`）
