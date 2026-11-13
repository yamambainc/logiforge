# 反映の手順

apply request を main に反映するとき、バックエンドが何を確かめ、どう反映するかを確かめるために読む。
要件は、[反映](../../docs/apply-request/README.md) と [反映の条件](../../docs/apply-request/review.md) が正本である。

## 手順

`approve` を受け付けたら、バックエンドは次の順に処理する。

1. 反映の条件を、GitForge から読んだ pull request とレビュー、[スナップショット](snapshot.md) で確かめる。
   - pull request の今の head のスナップショットがある。なければ、反映の失敗の理由を `snapshot_pending` として記録する。
   - レビューの鎖が切れていない（[レビューの書き方](../adapters/pull-requests.md)）。
   - 今の版（head のコミット）に対する最後のレビューが `approve` である。
   - 今の版に、書いたセッションの trailer がある（[セッション](../files/revisions.md)）。
   - その reviewer のセッションが、どの版を書いたセッションとも違う。
2. `latest` を読み、今の `main` のスナップショットを得る。
   - GitForge の `main` が `latest` より進んでいれば、worker を起こして、追いつくのを待つ。待つ時間の上限を過ぎたら、反映の失敗の理由を `snapshot_pending` として記録する。
3. 今の `main` のスナップショットで、次のことを確かめる。
   - 対象ノードが、base のあとに変わっていない（下の「対象ノードの変化」）。変わっていれば、`stale_target`。
   - 版の差分を、今の `main` に重ねる。差分が変えたファイルが、base のあとに `main` でも変わっていれば重ねられないので、`conflict`（[スナップショット](snapshot.md) のブランチの木）。
   - 版の差分を今の `main` に重ねた木に、[差分の検査](checks.md) を行う。破れていれば、`invariant_violation`。
4. GitForge の pull request の squash merge を、版のコミットの SHA を指定して呼ぶ。
   - メッセージの末尾に、[記録の形式](../files/revisions.md) の trailer を書く。
5. worker を起こし、`latest` が反映のコミットになるまで、上限つきで待ってから応答する（[書いた直後の読み](../api/writes.md)）。
6. 手順 1〜4 で反映できなければ、失敗の理由を GitForge に記録する。apply request は open のまま残る。

- reviewer の主体が、pull request を作った主体と同じなら、反映はするが、レビューの応答と apply request に警告を付ける。
  - なぜ：セッションは自己申告なので、同じ主体による書き込みとレビューは、確かめられないから（[保証の範囲](../../docs/apply-request/review.md)）。

## 対象ノードの変化

- 各操作の対象ノードについて、「変わった」とみなすパスを決める。
  - 仮説：自分の `hypothesis.md`、`background.md`、`verdicts/`、子ストーリーの `story.md` と `inactivated.md`、祖先のストーリーの `inactivated.md`
  - ストーリー：自分の `story.md` と `inactivated.md`、祖先のストーリーの `inactivated.md`
  - 引用した部分木のノード
- 今の `main` のスナップショットで、対象ノードの [ノードの変更の一覧](snapshot.md) に、base の番号より大きい番号があれば、変わったとみなす。
  - 1 つのスナップショットを読むだけで確かめられる。
  - base が今の `main` の `history` になければ、変わったものとみなす。
- 伝播で値だけが変わることは、ファイルに現れないので、「変わった」に含まれない。
  - なぜ：値は保存しないから（[保存しないもの](../files/layout.md)）。

## 衝突

- 手順 3 を通っても、git の衝突で merge できないことがある（例：同じ `background.md` の末尾への追加どうし）。GitForge の merge が失敗したら、反映の失敗の理由を `conflict` として記録する。
  - opener は、ブランチを今の `main` に合わせて直し、push する。新しい版になるので、レビューからやり直す。
- 手順 4 で、pull request の head が指定した SHA でなければ、GitForge の merge が失敗する。バックエンドは `version_conflict` を返す。
  - なぜ：レビューのあとに push された版を、レビューなしに反映しないため。
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
- opener は、ブランチを直して push する。新しい版になる。
  - それまでの approve は、新しい版には効かない（[反映の条件](../../docs/apply-request/review.md)）。
