# 最新のコミットが valid でないとき

`main` のコミットが valid かをどう決め、最新のコミットが valid でないときに、worker、バックエンド、CLI、WebUI がどう振る舞うかを確かめるときに読む。

## main はいつも正しい

- `main` のコミットは、どれも、そのまま受け入れる。
  - だれが、どう push したかは見ない。反映の手順によるかも、GitForge の pull request と review も見ない。
  - force push で `main` が書き換えられても、新しい並びをそのまま受け入れる（[latest](snapshot/main.md#latest)）。
  - なぜ：Logiforge が守るのは、木の中身が不変条件を満たすことだから。満たさない木は、次のコミットで直せばよい。
- 反映の手順は、反映するときに、差分を検査する（[反映の手順](merge.md)）。`main` に入ったあとのコミットは、検査し直さない。

## valid

- `main` のコミットは、そのコミットの木だけで、木の不変条件をすべて満たすとき、valid である（[差分の検査](checks.md) の不変条件）。
  - 1 つ前のコミットとの差分は見ない。追加だけかどうかも見ない。
- 木のファイルを読めないとき（大きすぎる、中身を読めない、など）も、valid でない。理由に、読めないファイルのパスを書く。
  - worker は、読めないファイルがあっても、取り込みを止めない。そのコミットを valid でないスナップショットとして置く。
  - なぜ：読めないファイル 1 つで、それより新しいコミットがすべて取り込めなくなるのを避けるため。
- worker は、`main` のコミットごとに、スナップショットツリーに、次のものを置く（[main のコミット](snapshot/main.md)）。
  - valid か
  - valid でない理由（破れた不変条件と、読めないファイル）
  - `valid_commit`：そのコミットか、それより前の、いちばん新しい valid なコミット。どれも valid でなければ、ない。
  - `invalid_since`：そのコミットが valid でないとき、`valid_commit` のあとの、最初の valid でないコミット。`valid_commit` がなければ、最初の版。
  - なぜスナップショットツリーに持つか：バックエンドが、最新のスナップショットを 1 つ読むだけで、どの木を返すかを決められるから。別の置き場を持たない。

## 木を読む API

- 木を読む API（`getRepository`、木、ノード、検証結果、作業、作業の終わり）は、最新のコミット（`latest`）が valid でなければ、`valid_commit` の木を返す。
  - `valid_commit` がなければ（最初の版から valid でない）、最新のコミットの木を返す。
- `Repository`、木（`Tree`）、ノードの一覧、作業の一覧、作業の終わり（`Completion`）の応答は、`valid_commit` と `invalid_since` を持つ（[openapi.yaml](../api/openapi.yaml)）。
  - 最新のコミットが valid なら、`valid_commit` は最新のコミットで、`invalid_since` は `null` である。
- 最新のコミットが valid でなければ、木を読む応答に、ヘッダー `Logiforge-Latest-Invalid` を付ける。値は、返した木のコミット（`valid_commit`、なければ最新のコミット）である。
  - apply request と人への依頼を読む応答には付けない。
- 書く操作の検査（apply request の作成、反映）は、最新のコミットの木で行う。valid でなくても、`valid_commit` に戻さない。
  - なぜ：git の `main` に重ねるのは、最新のコミットだから。
  - 前から破れている不変条件は、差分の検査で、新しく破ったものとしない（[差分の検査](checks.md)）。そのため、valid でない `main` にも、直す apply request を反映できる。

## CLI

- git を読むコマンド（`check`、ひな形、`repo clone`、`ar create`）は、最新のコミットが valid でなければ、標準エラーに警告を書く。止めずに続ける。
  - 警告：「最新のコミット <invalid_since 以降> は valid でない。API は、コミット <valid_commit> の木を示している」。
  - なぜ止めないか：手元の木を直すコミットを作るのにも、これらのコマンドを使うから。
- API の木を読むコマンドも、ヘッダー `Logiforge-Latest-Invalid` があれば、同じ警告を標準エラーに書く。終了コードは変えない。

## WebUI

- 木を示す画面は、最新のコミットが valid でなければ、画面の上に「最新のコミットは valid でない。コミット X の木を示している」と示す。X は `valid_commit` である。
- 示す木は、API が返す `valid_commit` の木である。

## 直す

- valid でない最新のコミットは、木を直すコミットで直す。
  - apply request の操作（[差分の検査](checks.md)）で直せるもの（足りないファイルを加える、など）は、ふつうの apply request で直せる。
  - 操作で直せないもの（木の外のファイルを消す、ファイルを書き換える、など）は、`main` への直接の push で直す。
- 直したコミットが valid なら、`valid_commit` はそのコミットになり、木を読む API は最新のコミットの木を返す。

## どの apply request の反映か

- `main` のコミットが、どの apply request の反映かは、表示だけに使う（ノードの履歴、コミットの一覧の `apply_request_id`、`reflected`）。valid かには使わない。
- worker が、取り込むときに、反映した pull request の一覧で、`merge_commit_sha` がそのコミットの pull request を探し、コミットの情報に置く（[一覧の読み](list-reads.md#反映した-pull-request-の一覧worker)）。
  - 置くのは、pull request の番号と、その先頭のコミットである。review は読まない。
  - なぜ worker で置くか：読む API が、一覧の項目ごとに GitForge を呼ばずに済むから。worker は、見回りで新しいコミットがあるときに、一覧を 1 ページ読むだけである。
- 見つからなければ、worker が初めて見てから 60 秒までは、そのコミットを取り込まずに、次の見回りでやり直す。過ぎたら、反映の apply request のないコミットとして置く。
  - なぜ：GitForge（Forgejo など）は、merge のすぐあと、merge のコミットから pull request を引けないことがあるから。
- 反映の apply request と、その先頭のコミットがわかれば、worker は、対象ノードの重なりを確かめ、警告にする（[worker の見回り](worker/patrol.md)）。
