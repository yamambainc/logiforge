# 反映かの判定

worker が、`main` の新しいコミットごとに、そのコミットが反映（論理レビューを通った apply request の merge）によるものかを、どう確かめるかを確かめるときに読む。
反映によらないコミットを見つけたあとの扱いは、[壊れた repository](broken.md) に書く。

## 考え方

- 反映によるものかは、中身だけで決める。だれが merge したか、どこから（Logiforge か、GitForge の画面か）merge したかは、使わない。
  - 使うもの：`main` のコミットの中身、元の pull request の先頭のコミットの SHA、Logiforge が書いた記録（だれのトークンで書かれたかは問わない）。
  - なぜ：merge する主体を 1 つ（書き込み主体）に絞らずに済むから。承認された apply request なら、reviewer のトークンで merge しても、人が GitForge の画面で merge しても、正しい反映になる。
- コミットのメッセージの trailer は、判定に使わない。なぜ：`main` に push できる人なら、だれでも書けるから。
- 記録は、バックエンドが Ed25519 の秘密鍵で署名する。worker は、公開鍵だけで確かめる（[記録のコメント](../adapters/pull-requests.md#記録のコメント)）。
  - なぜ：worker に、記録を作れる鍵を持たせないため。
- worker は、見た記録を、追記だけの形で覚える（下の「記録の控え」）。判定には、控えにある記録を使う。
  - なぜ：記録があとから消されたり書き換えられたりしても、判定が変わらないようにするため。例：`approve` のあとの `request_changes` を、記録でない本文に書き換えても、控えに残る。

## 確かめること

`main` のコミット C（1 つ前の `main` のコミットを P とする）について、次のどちらかなら、反映によるものとする。

### repository の用意のコミット

- C が根のコミットで、trailer `Logiforge-Init:` がある。
  - なぜ位置で決めてよいか：repository はバックエンドが作るので、根を先に置ける人がいないから。
- C が根の次のコミットで、trailer `Logiforge-Init-Signature:` の署名が合う。
  - 署名は、repository の識別子、P の SHA、C で加わったファイルのパスと sha256 に対して作る。
  - C の差分が、署名に書いたファイルを加えることだけであることも確かめる。
  - なぜ：GitHub の空のリポジトリには、ファイルを 1 つずつしか置けないので、背景情報を 2 つ目のコミットで置くから（[GitHub アダプタ](../adapters/github.md)）。

### apply request の反映のコミット

次のすべてを満たす。

1. C を merge した pull request が、GitForge にある。向き先が `main` で、merge のコミットが C である（下の「GitForge の API」）。その先頭のコミットを H とする。
2. C の形が、次のどちらかである。
   - squash merge：親が P だけである。
   - merge のコミット：最初の親が P で、2 つ目の親が H である。
   - rebase による merge（複数のコミットが並ぶもの）は、受け付けない。なぜ：途中のコミットの木が、どの先頭のコミットの木とも合わないから。
3. 控えに、その pull request の `apply_request` の記録がある。
4. 控えの記録で、反映の手順の手順 1 の条件を満たす（[反映の手順](merge.md)）。
   - その apply request の、すべての先頭のコミットに対するレビューのうち、最後の決めるレビュー（`approve` か `request_changes`）が、H への `approve` である。
     - なぜ H に限らないか：H1 への `approve` のあと H2 に `request_changes` が付いても、ブランチを H1 に戻せば、H1 への最後の決めるレビューは `approve` のままだから。
   - H に、書いたセッションの trailer がある。reviewer のセッションが、どの先頭のコミットを書いたセッションとも違う。
5. P の木に、H の差分（ブランチのスナップショット）を重ねられ、重ねた木の全体が、C の木と同じである。
   - H のスナップショットがなければ、`refs/pull/<number>/head` を fetch して作る。
6. 重ねた木が、反映の手順の手順 3 の検査（対象ノードの変化、差分の検査）を、P を今の `main` として満たす。
   - 直すための apply request（`repair.revert`、`repair.accept`）は、手順 3 のかわりに、直す条件を確かめる（[壊れた repository](broken.md) の「直すための apply request」）。

- どれかを満たさなければ、反映によらないコミットとする。コミットの記録の警告に、満たさなかった条件を書く。
- 壊れているあいだは、直すための apply request の反映のコミットだけを、反映によるものとする。

## 記録の控え

- worker は、見回りのたびに、repository の記録のコメントを読み、署名の合うものを、置き場の `keep/records/<gitforge_ref>/<番号>` に追記する（[スナップショットの置き場](ports/snapshot-store.md)）。
  - なぜ gitforge_ref で分けるか：バックエンドの GitForge のアダプタが、gitforge_ref で記録を読むから。
  - 読むのは、repository のコメントの一覧のうち、前に読んだあとに作られたか変わったもの（`since`）だけである。
  - 控えは、記録の識別子ごとに、最初に見た中身を持つ。同じ識別子で中身が違うものを見たら、最初のものを残し、警告する。
  - 控えからは、消さない。GitForge から記録が消えても、控えには残る。
  - 控えは、記録といっしょに、GitForge のコメントの id を持つ。レビューの順番（「最後の決めるレビュー」）は、この id の順で決める。
    - なぜ：控えにだけある記録と、今の GitForge の記録を合わせたときに、順番を 1 つに決めるため。コメントの id は、GitForge の中で作った順に増える。
- 控えは、git から作り直せないので、`keep/` に置き、バックアップを取る。
- 判定は、控えの記録と、そのときに GitForge から読んだ記録の、署名の合うものを合わせて行う。
- 記録を読む間隔は、見回りの間隔と同じである。GitHub では、REST の上限のために、30 秒にする（[GitHub アダプタ](../adapters/github.md) の「見回りの間隔」）。

## GitForge の API

| GitForge | C を merge した pull request の読み方 |
|---|---|
| GitHub | `GET /repos/{owner}/{repo}/commits/{sha}/pulls` のうち、merge されていて、`merge_commit_sha` が C で、向き先が `main` のもの |
| Forgejo | `GET /repos/{owner}/{repo}/commits/{sha}/pull` が返すもので、merge されていて、向き先が `main` のもの |

- 記録は、repository のコメントの一覧（`GET /repos/{owner}/{repo}/issues/comments?since=`）から読む。GitHub と Forgejo の両方にある。
- API が答えないときは、C を取り込まずに、次の見回りでやり直す。`latest` は進めない。
  - なぜ：判定できないコミットを、正しいとも壊れているとも記録しないため。
- C を merge した pull request が見つからないときは、worker が C を初めて見てから 60 秒までは、決めずにやり直す。
  - なぜ：GitForge は、merge のすぐあと、merge のコミットから pull request を引けないことがあるから（Forgejo 17 で確かめた）。
  - コミットの時刻は使わない。push する人が、未来の時刻にできるから。初めて見た時刻は、worker のプロセスの中だけに持つ。

## 重さ

`main` のコミット 1 つの判定に、次のものがかかる。

| もの | 量 | 目安 |
|---|---|---|
| GitForge の API | C を merge した pull request の読み 1 回 | 100〜300 ms |
| 控えを読む | その pull request の記録（置き場のキー 1 つ） | 数 ms |
| 署名を確かめる | 記録の数だけ（多くて数十） | 1 つ 0.1 ms より小さい |
| 差分を重ねる計算 | 先頭のコミットの差分のファイルの数に比例する | 数 ms |
| 重ねた木の検査 | スナップショットツリーの組み立てと検査と同じく、木のノードの数に比例する | ノード 100 で約 20 ms、1,000 で約 110 ms、4,000 で約 320 ms（[費用と規模の見積もり](../estimates.md) の「組み立てと検査」） |

- 合わせて、ノード 4,000 の木でも、コミット 1 つに 0.6 秒ほどである。今の 1 コミットの取り込み（ノード 4,000 で約 460 ms）に足すと、約 2.3 倍になる。
- 重ねた木は、そのコミットのスナップショットツリーと同じものになるので、検査の結果を、スナップショットツリーを作るのに使い回せる。そのときは、足す時間は、API の待ちのほかは数 ms になる。
- 記録を読むのは、見回りごとに、repository ごとに REST が 1 回（変わったものがなければ、空の 1 ページ）である。GitHub で 30 秒ごとなら、repository 1 つで 1 時間に 120 回で、上限（1 時間に 5,000）に対して、repository 30 個ほどまで足りる。
- 判定は、新しいコミットにだけ行う。取り込み済みのコミットは、判定し直さない。

## 確かめたこと

Forgejo 17 で、次のことを確かめた（`internal/adapters/forgejo` の結合テストと、`internal/e2e` の E2E）。

- issue と pull request を書ける主体は、ほかの主体のコメントを、編集でき、消せる。
- `issues/comments?since=` は、編集されたコメントを、`updated_at` とともに返す。
  - 時刻は秒の単位なので、worker は、前に読んだ位置の 1 秒前から読む。同じものを読み直しても、控えには足さない。
- 承認された apply request を、人が Forgejo の画面から merge のコミットで merge すると、反映によるものになる。
- `approve` のあとに `request_changes` が付いた apply request を、人が squash merge すると、反映によらないコミットになり、壊れた状態になる。

## 確かめること

- GitHub で、上のことが同じか（[GitHub アダプタ](../adapters/github.md)）。
- pull request の先頭のコミットを、直接 `main` に push したとき、GitForge が pull request を merge 済みにするか。そのときの `merge_commit_sha`。
