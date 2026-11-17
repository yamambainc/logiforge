# 壊れた repository

反映によらない変更を worker がどう見つけ、バックエンドが壊れた repository をどう扱い、直すための apply request をどう反映するかを確かめるときに読む。
要件は [壊れた repository を直す](../../docs/apply-request/repair.md) が正本である。

## 反映かの判定

worker は、`main` の新しいコミットごとに、反映によるものかを決める。

- 反映によるもの：次のどちらか。
  - 書き込み主体が merge した pull request の merge のコミット（squash merge のコミット）である。GitForge の API で確かめる。
  - repository の根のコミットで、`Logiforge-Init:` がある（[repository の用意](README.md)）。
    - 位置で決めるのは、根のコミット 1 つだけである。背景情報など、根のあとに置くものは、書き込み主体が pull request で置く（反映によるコミットになる）。
    - なぜ根だけか：2 番目より後を位置と trailer で決めると、背景情報のない repository で、trailer 付きの push で偽れるから。
- それ以外は、反映によらないコミットである。
- なぜ GitForge の API で確かめるか：コミットのメッセージの trailer は、`main` に push できる人ならだれでも書けるから。merge した主体は、GitForge が記録し、書き換えられない。
- なぜ根のコミットは位置で決めるか：repository はバックエンドが作るので、根を先に置ける人がいないから。

| GitForge | 確かめ方 |
|---|---|
| GitHub | `GET /repos/{owner}/{repo}/commits/{sha}/pulls` で、そのコミットの pull request を読み、`merged_by` が書き込み主体で、`merge_commit_sha` がそのコミットであること |
| Forgejo | `GET /repos/{owner}/{repo}/commits/{sha}/pull` で、そのコミットを merge した pull request を読み、`merged_by` が書き込み主体であること |

- API を呼ぶのは、`main` の新しいコミットごとに 1 回である。
- worker は、そのために、GitForge の読むだけの API を呼ぶ。資格情報は、git を読むのと同じものである（[git アダプタ](../adapters/git.md)）。
  - GitHub：worker の App に、Pull requests の read を付ける。
  - Forgejo：worker のユーザーのトークンの `read:repository` で読める。
- API が答えないときは、そのコミットを取り込まずに、次の見回りでやり直す。`latest` は進めない。
  - なぜ：判定できないコミットを、正しいとも壊れているとも記録しないため。

## 壊れている状態

- 次のどちらかを取り込んだら、そのコミットの記録の警告に付け、repository を壊れた状態にする。
  - 反映によらないコミット（`no_apply_request`）
  - `main` の書き換え（`main_rewritten`。[latest](snapshot.md#latest)）
    - なぜ書き換えも壊れた状態にするか：古い反映まで巻き戻すと、残ったコミットはどれも書き込み主体が merge したものなので、反映かの判定では気づかないまま、結論が消えるから。
- コミットの記録は、`last_good` を持つ。最後に正しかった時点のコミットの SHA である。壊れていなければ、空である。
  - 反映によらないコミットのときは、その直前のコミットである。
  - 書き換えのときは、書き換えの前の `latest` のコミットの記録に `last_good` があればそれ、なければ書き換えの前の `latest` である。
    - なぜ引き継ぐか：すでに壊れているときに書き換えが起きても、最後に正しかった時点は変わらないから。そのコミットは、今の `main` の並びにないことがあるが、そのコミットの記録とスナップショットツリーは、置き場に残る（[スナップショット](snapshot.md)）。
  - なぜ番号でなく SHA か：書き換えの前のコミットは、今の `main` の並びの番号を持たないから。
  - 壊れたあとのコミットは、反映によるものでも、`last_good` を引き継ぐ。直すための apply request の反映だけが、`last_good` を空に戻す。
- スナップショットツリーは、今までどおり、すべての `main` のコミットに作る。壊れたあとのコミットにも作る。
  - なぜ：直すための apply request の材料（最後に正しかった時点からの差分）と、認めたあとの木を読むため。

## バックエンド

- 木を読む API は、`latest` のコミットの記録に `last_good` があれば、そのコミットの木を返す。応答にヘッダー `Logiforge-Repository-Broken` を付ける。`Repository` の `broken` は、壊れた最初のコミットと、最後に正しかった時点のコミットを示す（[openapi.yaml](../api/openapi.yaml)）。
  - CLI は、応答を出力し、ヘッダー `Logiforge-Repository-Broken` があれば、`code` が `repository_broken` の誤りを標準エラーに書き、終了コード 1 で終わる。API の `Problem` の `code` ではなく、`cli_error` と同じ CLI の中の誤りである（[CLI](../cli/README.md) の「出力」）。
  - WebUI は、最後に正しかった時点の木を示し、壊れていることを画面の上に示す。
  - `commit` を指定した読みは、指定したコミットの木を返す。そのコミットが壊れたあとなら、ヘッダーを付ける。
- 壊れているあいだ、反映の手順は、直すための apply request でなければ、反映の失敗の理由 `repository_broken` を記録して止まる（[反映の手順](merge.md)）。
- apply request の作成は、壊れているあいだも受け付ける。
- git を読む CLI のコマンド（`check`、ひな形、`repo clone`、`ar create`）も、`getRepository` の `broken` を見て、壊れていれば `repository_broken` の誤りを返す。
  - なぜ：壊れているあいだ、git で clone した木（壊れた木）と、API が返す木（最後に正しかった時点の木）が違うから。気づかずに、壊れた木の上で作業しないようにするため。
  - `repair revert` と `repair accept` は、例外である。

## 直すための apply request

直すための apply request は、apply request の作成で種類（`kind`）を指定して作る。ふつうの apply request と同じく、論理レビューの `approve` で反映する。

| 種類 | 先頭のコミット | 反映の前に確かめること |
|---|---|---|
| `repair.revert` | `main` から分けたブランチで、木の全体を `last_good` のコミットのものに戻したコミット。CLI の `repair revert` が作る | 先頭の木の全体が、`last_good` のコミットの木と同じである。差分の検査（[差分の検査](checks.md)）は行わない |
| `repair.accept` | `main` から分けたブランチの、空のコミット。CLI の `repair accept` が作る | 先頭のコミットの親が、今の `main` の先頭である。認めるのは、そこまでの変更である。その木に、`tree/` と `artifacts/` の外のファイルがあれば断る |

- `repair revert` は、戻す木を、git でなく API（`getRepairFiles`）から読む。`last_good` のコミットのファイルを、スナップショットツリーと artifact の置き場から返す。
  - なぜ：書き換えのあと、`last_good` のコミットは GitForge にないことがあるから。
  - `last_good` のコミットの木の全体は、`tree/` と `artifacts/` だけである。反映によるコミットは、その外を変えないから（[差分の検査](checks.md)）。そのため、スナップショットツリーと artifact だけから戻せる。
- `repair.accept` の reviewer の材料は、`last_good` のコミットから今の `main` の先頭までの差分である（`ar material`）。
- 反映のコミットに、trailer `Logiforge-Repair: revert` か `Logiforge-Repair: accept` を書く。
- worker は、反映によるコミットで、`Logiforge-Repair:` があり、次のことが成り立てば、`last_good` を空に戻す。
  - `revert`：そのコミットの木の全体が、`last_good` のコミットの木と同じである。
  - `accept`：squash のコミットの親が、apply request のブランチを作った時点の `main` の先頭（先頭のコミットの親）である。
- なぜ accept で外のファイルを断るか：認めると、あとで壊れたときの最後に正しかった時点の木が、`getRepairFiles`（`tree/` と `artifacts/` だけ）と一致せず、revert で直せなくなるから。外のファイルを含む変更は、revert でしか直せない。
- なぜ木の全体を比べるか：`tree/` と `artifacts/` だけを比べると、その外の変更（`.github/workflows` など）を、差分の検査なしに `main` に入れられるから。
- なぜ trailer を信じてよいか：反映によるコミットかは、上の「反映かの判定」で決まっている。書き込み主体が merge したコミットのメッセージは、バックエンドが書いたものだから。

## 確かめること

実装するときに、動く GitForge で次のことを確かめる。

- 空のコミットだけのブランチから、pull request を作り、squash merge できるか（GitHub と Forgejo）
- `GET /commits/{sha}/pulls` と `GET /commits/{sha}/pull` が、squash merge のコミットから、その pull request と `merged_by` を返すか
