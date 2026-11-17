# 壊れた repository

反映によらない変更を worker がどう見つけ、バックエンドが壊れた repository をどう扱い、直すための apply request をどう反映するかを確かめるときに読む。
要件は [壊れた repository を直す](../../docs/apply-request/repair.md) が正本である。

## 反映かの判定

worker は、`main` の新しいコミットごとに、反映によるものかを決める。

- 反映によるもの：次のどちらか。
  - 書き込み主体が merge した pull request の merge のコミット（squash merge のコミット）である。GitForge の API で確かめる。
  - repository の用意のコミット（`Logiforge-Init:` のある、根から続くコミット。[repository の用意](README.md)）である。
- それ以外は、反映によらないコミットである。
- なぜ GitForge の API で確かめるか：コミットのメッセージの trailer は、`main` に push できる人ならだれでも書けるから。merge した主体は、GitForge が記録し、書き換えられない。
- なぜ用意のコミットは位置で決めるか：repository はバックエンドが作るので、根より前にコミットを置ける人がいないから。

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

- 反映によらないコミットを取り込んだら、そのコミットの記録の警告に `no_apply_request` を付け、repository を壊れた状態にする。
- コミットの記録は、`last_good` を持つ。最後に正しかった時点（壊れる前の、いちばん新しい反映によるコミット）の番号である。壊れていなければ、空である。
  - 壊れたあとのコミットは、反映によるものでも、`last_good` を引き継ぐ。直すための apply request の反映だけが、`last_good` を空に戻す。
- スナップショットツリーは、今までどおり、すべての `main` のコミットに作る。壊れたあとのコミットにも作る。
  - なぜ：直すための apply request の材料（最後に正しかった時点からの差分）と、認めたあとの木を読むため。

## バックエンド

- 木を読む API は、`latest` のコミットの記録に `last_good` があれば、その番号のコミットの木を返す。応答にヘッダー `Logiforge-Repository-Broken` を付ける。`Repository` の `broken` は、壊れた最初のコミットと、最後に正しかった時点のコミットを示す（[openapi.yaml](../api/openapi.yaml)）。
  - CLI は、応答を出力し、ヘッダー `Logiforge-Repository-Broken` があれば、`code` が `repository_broken` の誤りを標準エラーに書き、終了コード 1 で終わる。API の `Problem` の `code` ではなく、`cli_error` と同じ CLI の中の誤りである（[CLI](../cli/README.md) の「出力」）。
  - WebUI は、最後に正しかった時点の木を示し、壊れていることを画面の上に示す。
  - `commit` を指定した読みは、指定したコミットの木を返す。そのコミットが壊れたあとなら、ヘッダーを付ける。
- 壊れているあいだ、反映の手順は、直すための apply request でなければ、反映の失敗の理由 `repository_broken` を記録して止まる（[反映の手順](merge.md)）。
- apply request の作成は、壊れているあいだも受け付ける。

## 直すための apply request

直すための apply request は、apply request の作成で種類（`kind`）を指定して作る。ふつうの apply request と同じく、論理レビューの `approve` で反映する。

| 種類 | 先頭のコミット | 反映の前に確かめること |
|---|---|---|
| `repair.revert` | `main` から分けたブランチで、`tree/` と `artifacts/` を `last_good` のコミットのものに戻したコミット。CLI の `repair revert` が作る | 先頭の木の `tree/` と `artifacts/` が、`last_good` のコミットのものと同じである。差分の検査（[差分の検査](checks.md)）は行わない |
| `repair.accept` | `main` から分けたブランチの、空のコミット。CLI の `repair accept` が作る | 先頭のコミットの親が、今の `main` の先頭である。認めるのは、そこまでの変更である |

- `repair.accept` の reviewer の材料は、`last_good` のコミットから今の `main` の先頭までの差分である（`ar material`）。
- 反映のコミットに、trailer `Logiforge-Repair: revert` か `Logiforge-Repair: accept` を書く。
- worker は、反映によるコミットで、`Logiforge-Repair:` があり、次のことが成り立てば、`last_good` を空に戻す。
  - `revert`：そのコミットの木の `tree/` と `artifacts/` が、`last_good` のコミットのものと同じである。
  - `accept`：そのコミットの直前までに、ほかの反映によらないコミットがない（認めたあとに、新しく壊れていない）。
- なぜ trailer を信じてよいか：反映によるコミットかは、上の「反映かの判定」で決まっている。書き込み主体が merge したコミットのメッセージは、バックエンドが書いたものだから。

## 確かめること

実装するときに、動く GitForge で次のことを確かめる。

- 空のコミットだけのブランチから、pull request を作り、squash merge できるか（GitHub と Forgejo）
- `GET /commits/{sha}/pulls` と `GET /commits/{sha}/pull` が、squash merge のコミットから、その pull request と `merged_by` を返すか
