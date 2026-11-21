# 壊れた repository

反映によらない変更を worker がどう見つけ、バックエンドが壊れた repository をどう扱い、直すための apply request をどう反映するかを確かめるときに読む。
要件は [壊れた repository を直す](../../docs/apply-request/repair.md) が正本である。

## 反映かの判定

- worker は、`main` の新しいコミットごとに、反映によるものかを決める。決め方は [反映かの判定](verify/README.md) に書く。
  - だれが merge したかでは決めない。中身が反映の条件を満たしているかで決める。

## 壊れている状態

- 次のどちらかを取り込んだら、そのコミットの記録の警告に付け、repository を壊れた状態にする。
  - 反映によらないコミット（`no_apply_request`）
  - `main` の書き換え（`main_rewritten`。[latest](snapshot/main.md#latest)）
    - なぜ書き換えも壊れた状態にするか：古い反映まで巻き戻すと、残ったコミットはどれも反映によるものなので、反映かの判定では気づかないまま、結論が消えるから。
- 壊れていることの記録は、repository ごとに 1 つ、置き場の `keep/<repository>/broken` に置く。壊れていなければ、空である。
  - なぜコミットの記録に持たないか：書き換えが取り込み済みのコミットまで短くされると、記録するコミットがないから。1 か所に持てば、読む側はそれだけを読めばよい。
  - なぜ `keep/` か：書き換えの前の `latest` は、git からは作り直せないから（[スナップショットの置き場](ports/snapshot-store.md)）。
- 記録は、次のものを持つ。
  - 最後に正しかった時点のコミットの SHA（`last_good`）
  - 壊れた最初のコミット
  - 理由
- `last_good` は、次のコミットである。
  - 反映によらないコミットのときは、その直前のコミット
  - 書き換えのときは、すでに壊れていれば今の `last_good`、壊れていなければ書き換えの前の `latest`
    - なぜ引き継ぐか：すでに壊れているときに書き換えが起きても、最後に正しかった時点は変わらないから。そのコミットは、今の `main` の並びにないことがあるが、そのコミットの記録とスナップショットツリーは、置き場に残る（[スナップショット](snapshot/README.md)）。
  - なぜ番号でなく SHA か：書き換えの前のコミットは、今の `main` の並びの番号を持たないから。
- 壊れたあとのコミットは、反映によるものでも、`last_good` を引き継ぐ。直すための apply request の反映だけが、`last_good` を空に戻す。
- スナップショットツリーは、すべての `main` のコミットに作る。壊れたあとのコミットにも作る。
  - なぜ：直すための apply request の材料（最後に正しかった時点からの差分）と、認めたあとの木を読むため。

## バックエンド

- 木を読む API は、壊れていれば、`last_good` のコミットの木を返す。
- repository と木を読む応答（`getRepository`、木、ノード、検証結果）に、ヘッダー `Logiforge-Repository-Broken`（`last_good`）を付ける。apply request と人への依頼を読む応答には付けない。
- `Repository` の `broken` は、壊れた最初のコミットと、最後に正しかった時点のコミットを示す（[openapi.yaml](../api/openapi.yaml)）。
- CLI は、応答を出力し、ヘッダー `Logiforge-Repository-Broken` があれば、`code` が `repository_broken` の誤りを標準エラーに書き、終了コード 1 で終わる。
  - この誤りは、API の `Problem` の `code` ではなく、`cli_error` と同じ CLI の中の誤りである（[CLI](../cli/README.md) の「出力」）。
- WebUI は、最後に正しかった時点の木を示し、壊れていることを画面の上に示す。
- `commit` を指定した読みは、指定したコミットの木を返す。壊れていれば、ヘッダーを付ける。
- 壊れているあいだ、反映の手順は、直すための apply request でなければ、反映の失敗の理由 `repository_broken` にして止まる（[反映の手順](merge.md)）。
- apply request の作成は、壊れているあいだも受け付ける。
- git を読む CLI のコマンド（`check`、ひな形、`repo clone`、`ar create`）も、`getRepository` の `broken` を見て、壊れていれば `repository_broken` の誤りを返す。
  - なぜ：壊れているあいだ、git で clone した木（壊れた木）と、API が返す木（最後に正しかった時点の木）が違うから。気づかずに、壊れた木の上で作業しないようにするため。
  - `repair revert` と `repair accept` は、例外である。

## 直すための apply request

直すための apply request は、apply request の作成で種類（`kind`）を指定して作る。バックエンドは、pull request にラベル `logiforge/repair.revert` か `logiforge/repair.accept` を付ける。ふつうの apply request と同じく、論理レビューの `approve` で反映する。

- 直すための apply request の reviewer の材料（`ar material` の `repair`）は、`last_good` のコミットから今の `main` の先頭までの、ファイルの変化である。
- worker は、反映によるコミットで、その pull request のラベルが直すための apply request の種類であり、下の条件が成り立てば、`last_good` を空に戻す。
  - なぜラベルで決めるか：人が GitForge の画面で merge すると、反映のコミットのメッセージに trailer が入らないから。
  - ラベルは、Write を持つ主体なら付け替えられる。付け替えても、下の中身の条件（木の全体の比べ方）を満たさなければ、直したことにならない（[信頼の境界](../adapters/pull-requests.md#信頼の境界)）。

### repair.revert

- 先頭のコミットは、`main` から分けたブランチで、木の全体を `last_good` のコミットのものに戻したコミットである。CLI の `repair revert` が作る。
- 反映の前に、先頭の木の全体が、`last_good` のコミットの木と同じであることを確かめる。[差分の検査](checks.md) は行わない。
- worker が `last_good` を空に戻すのは、そのコミットの木の全体が、`last_good` のコミットの木と同じときである。
- なぜ木の全体を比べるか：`tree/` と `artifacts/` だけを比べると、その外の変更（`.github/workflows` など）を、差分の検査なしに `main` に入れられるから。
- `repair revert` は、戻す木を、git でなく API（`getRepairFiles`）から読む。`last_good` のコミットのファイルを、スナップショットツリーと artifact の置き場から返す。
  - なぜ：書き換えのあと、`last_good` のコミットは GitForge にないことがあるから。
  - `last_good` のコミットの木の全体は、`tree/` と `artifacts/` だけである。反映によるコミットは、その外を変えないから（[差分の検査](checks.md)）。そのため、スナップショットツリーと artifact だけから戻せる。

### repair.accept

- 先頭のコミットは、`main` から分けたブランチで、認めた記録の artifact を 1 つ加えるコミットである。CLI の `repair accept` が作る。
  - 認めた記録は、壊れた最初のコミット、`last_good`、`main` の先頭を持つ。
- 反映の前に、次のことを確かめる。
  - 差分が、その artifact を 1 つ加えるだけである。
  - その artifact は、差分の検査の artifact の規則（名前が中身の sha256、大きさの上限）を満たす。
  - ブランチが、今の `main` の先頭から分かれている。認めるのは、そこまでの変更である。
  - 今の `main` の木に、`tree/` と `artifacts/` の外のファイルがない。あれば断る。
- worker が `last_good` を空に戻すのは、squash のコミットの親が、apply request のブランチを作った時点の `main` の先頭（先頭のコミットの親）であるときである。
- なぜ外のファイルを断るか：認めると、あとで壊れたときの最後に正しかった時点の木が、`getRepairFiles`（`tree/` と `artifacts/` だけ）と一致せず、revert で直せなくなるから。外のファイルを含む変更は、revert でしか直せない。
- なぜ空のコミットにしないか：Forgejo では、空のコミットだけの pull request は squash merge できないから。
