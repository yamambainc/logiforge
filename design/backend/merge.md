# 反映の手順

apply request を main に反映するとき、バックエンドが何を確かめ、どう反映するかを確かめるために読む。
要件は、[反映](../../docs/apply-request/README.md) と [反映の条件](../../docs/apply-request/review.md) が正本である。

## 手順

`approve` を受け付けたら、バックエンドは次の順に処理する。

1. 反映の条件を、GitForge から読んだ pull request とレビュー、worker の記録の控え（[記録の控え](verify.md#記録の控え)）、[スナップショット](snapshot.md) で確かめる。
   - レビューは、GitForge の記録と控えの記録の、署名の合うものを合わせて読む。なぜ：GitForge から消された `request_changes` を、見落とさないため。
   - pull request の向き先が `main` である。違えば、反映の手順に進まない。レビューを記録し、応答の警告に、Logiforge の反映の対象でないことを書く（[向き先](apply-request.md#向き先)）。
   - repository が壊れていれば、直すための apply request である（[壊れた repository](broken.md)）。違えば、反映の失敗の理由を `repository_broken` として記録する。
   - pull request の今の head のスナップショットがある。なければ、反映の失敗の理由を `snapshot_pending` として記録する。
   - レビューの鎖が切れていない（[レビューの書き方](../adapters/pull-requests.md)）。
   - すべての先頭のコミットに対するレビューのうち、最後の決めるレビュー（`approve` か `request_changes`）が、先頭のコミット（head のコミット）への `approve` である。
     - なぜ先頭のコミットに限らないか：worker の判定（[反映かの判定](verify.md)）と同じ条件にするため。ブランチを前の先頭のコミットに戻しても、そのあとの `request_changes` を見落とさない。
   - 先頭のコミットに、書いたセッションの trailer がある（[セッション](../files/revisions.md#セッション)）。
   - その reviewer のセッションが、どの先頭のコミットを書いたセッションとも違う。
2. `latest` を読み、今の `main` のスナップショットを得る。
   - GitForge の `main` が `latest` より進んでいれば、worker が追いつくのを待つ。待つ時間の上限を過ぎたら、反映の失敗の理由を `snapshot_pending` として記録する。
   - GitForge の `main` は、GitForge のポートの「`main` の先頭を読む」で読む（[GitForge のポート](ports/gitforge.md)）。
   - 待ち始めに読んだ `main` の先頭が、`main` の並びに入り、その番号が `latest` の番号以下になったら、待つのをやめる（[コミットの記録から引くもの](snapshot.md#コミットの記録から引くもの)）。そのあとに `main` がさらに進んでもよい。
3. 今の `main` のスナップショットで、次のことを確かめる。
   - 対象ノードが、base のあとに変わっていない（下の「対象ノードの変化」）。変わっていれば、`stale_target`。
   - 先頭のコミットの差分を、今の `main` に重ねる（重ね方は [向き先に重ねる](snapshot.md#向き先に重ねる)）。重ねられなければ、`conflict`。
   - 先頭のコミットの差分を今の `main` に重ねた木に、[差分の検査](checks.md) を行う。破れていれば、`invariant_violation`。
4. GitForge の pull request の squash merge を、先頭のコミットの SHA を指定して、`approve` をした reviewer のトークンで呼ぶ。
   - なぜ reviewer のトークンか：反映は、`approve` の要求の中で同期で行うので、そのトークンがいつも手元にあるから。Logiforge は、merge するための自分のアカウントを持たない（[GitForge アダプタの共通の書き方](../adapters/pull-requests.md)）。
   - reviewer に merge の権限がなければ、GitForge が拒む。そのときは、反映の失敗として記録する。apply request は open のまま残り、merge の権限のある reviewer が `approve` し直せば反映される。
   - 承認された apply request を、人が GitForge の画面で merge してもよい。worker は、中身で反映かを決める（[反映かの判定](verify.md)）。
   - メッセージの末尾に、[記録の形式](../files/revisions.md) の trailer を書く。
5. `latest` が反映のコミットになるまで、上限つきで待ってから応答する（[書いた直後の読み](../api/writes.md)）。
   - 手順 1 から手順 4 のあいだに向き先が変えられると、GitForge は、`main` でないブランチに merge する。このすき間は、ふさがない。`main` に入らない変更は、Logiforge の検査の対象でないからである（[向き先](apply-request.md#向き先)）。
6. 手順 1〜4 で反映できなければ、失敗の理由を GitForge に記録する。apply request は open のまま残る。

- reviewer の主体が、pull request を作った主体と同じなら、反映はするが、レビューの応答と apply request に警告を付ける。
  - なぜ：セッションは自己申告なので、同じ主体による書き込みとレビューは、確かめられないから（[保証の範囲](../../docs/apply-request/review.md)）。

## 対象ノードの変化

- 各操作の対象ノードについて、「変わった」とみなすパスを決める。
  - 仮説：自分の `hypothesis.md`、`background.md`、`verdicts/`、子ストーリーの `story.md` と `inactivated.md`、祖先のストーリーの `inactivated.md`
  - ストーリー：自分の `story.md` と `inactivated.md`、祖先のストーリーの `inactivated.md`
  - 引用した部分木のノード（部分木のどれかのノードの `last_changed` の番号が、base の番号より大きければ、変わったとみなす）
- 今の `main` のスナップショットツリーで、対象ノードの `last_changed` の番号が、base の番号より大きければ、変わったとみなす（[ノードを変えたコミット](snapshot.md#ノードを変えたコミット)）。
  - 1 つのスナップショットツリーを読むだけで確かめられる。
  - base が `main` の並びになければ、変わったものとみなす（[コミットの記録から引くもの](snapshot.md#コミットの記録から引くもの)）。
- 伝播で値だけが変わることは、ファイルに現れないので、「変わった」に含まれない。
  - なぜ：値は保存しないから（[保存しないもの](../files/layout.md)）。

## 衝突

- 手順 3 を通っても、git の衝突で merge できないことがある（例：同じ `background.md` の末尾への追加どうし）。GitForge の merge が失敗したら、反映の失敗の理由を `conflict` として記録する。
  - opener は、ブランチを今の `main` に合わせて直し、push する。新しい先頭のコミットになるので、レビューからやり直す。
- 手順 4 で、pull request の head が指定した SHA でなければ、GitForge の merge が失敗する。バックエンドは `commit_conflict` を返す。
  - なぜ：レビューのあとに push されたコミットを、レビューなしに反映しないため。
- `main` に追いついていることを merge の条件にする GitForge の保護と、ブランチを `main` に追いつかせる操作は、使わない。
  - なぜ：衝突しない変更まで、`main` が進むたびに作り直しになるから。手順 3 と git の衝突で足りる。

## 検査と merge のすき間

- 手順 3 の検査と、手順 4 の merge の間に、別の apply request が反映されることがある。このすき間は、ふさがない。
  - そのため、まれに、同じ仮説への重なり（例：偽の結論と、新しいストーリーの追加）が、両方反映されることがある。
  - なぜふさがないか：ふさぐには、`main` への書き込みをバックエンドの側で直列にする仕組みが要る。起きる頻度に対して、仕組みが重いから。
- [worker](worker.md) は、反映のあとの `main` で、不変条件と、同じ対象ノードへの重なりを確かめ、見つけたら警告する。
  - 警告を見た opener と reviewer は、新しい apply request で直す（例：ストーリーを inactive にする）。
  - 直すための apply request は、すでに破れている条件では拒まれない（[差分の検査](checks.md)）。

## 反映できなかったとき

- apply request は、open のまま残る。
- opener は、ブランチを直して push する。新しい先頭のコミットになる。
  - それまでの approve は、新しい先頭のコミットには効かない（[反映の条件](../../docs/apply-request/review.md)）。
- 修正待ちになるのは、opener が直すべき理由（`stale_target`、`invariant_violation`、`conflict`）のときだけである。`snapshot_pending` のあとは、reviewer が `approve` し直す（[計算](README.md)）。

## レビューの鎖が切れたとき

- レビューの鎖が切れた apply request には、レビューも反映もできない（[レビューの書き方](../adapters/pull-requests.md)）。
- レビューの API は、`not_open` を返す。応答の `detail` に、opener の手を書く。
- opener の手：その apply request を close し、同じブランチから、apply request を作り直す。
  - 新しい apply request は、新しい pull request なので、レビューの鎖は空から始まる。
  - なぜ作り直すか：切れた鎖を、正しい形に戻す手段がないから。数えないレビューを消すことは、GitForge の権限で防いでいる。
