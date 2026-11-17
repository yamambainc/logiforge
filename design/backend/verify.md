# 反映かの判定

worker が、`main` の新しいコミットごとに、そのコミットが反映（論理レビューを通った apply request の、バックエンドによる merge）によるものかを、どう確かめるかを確かめるときに読む。
反映によらないコミットを見つけたあとの扱いは、[壊れた repository](broken.md) に書く。

## 考え方

- 反映によるものかは、だれのアカウントが merge したかでは決めない。バックエンドが、反映の手順（[反映の手順](merge.md)）を通して merge したことを示す、署名のある記録で決める。
  - なぜアカウントで決めないか：merge するための自分のアカウント（書き込み主体）を持たずに済むから。merge は、`approve` をした reviewer のトークンで行う。
  - なぜ記録が要るか：reviewer は、Logiforge を通さずに、GitForge の画面から merge できるから。画面からの merge は、反映の手順の検査（とくに、`approve` のあとの `request_changes`）を通らない。
- 署名は、バックエンドだけが持つ鍵で作る。worker は、確かめる公開鍵だけを持つ（下の「署名」）。
- コミットのメッセージの trailer は、判定に使わない。なぜ：`main` に push できる人なら、だれでも書けるから。

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

1. C を merge した pull request が、GitForge にある。向き先が `main` で、merge のコミットが C で、親が P だけである（squash merge）。
2. その pull request に、署名の合う `merge` の記録があり、C と、merge した先頭のコミット H を指す（[記録のコメント](../adapters/pull-requests.md#記録のコメント)）。
   - バックエンドは、反映の手順の手順 1 から 3 を通し、merge したあとに、この記録を書く（[反映の手順](merge.md)）。
3. P の木に、H の差分（ブランチのスナップショット）を重ねた木の全体が、C の木と同じである。
   - H のスナップショットがなければ、`refs/pull/<number>/head` を fetch して作る。
   - なぜ：記録の指す H と、実際に `main` に入った変更が同じことを、git の中身で確かめるため。

- 反映の手順の手順 1 と 3 の条件（レビュー、セッション、対象ノードの変化、差分の検査）は、worker は確かめ直さない。
  - なぜ：バックエンドは、それを確かめてからでなければ、`merge` の記録に署名しないから。worker が確かめ直しても、署名の鍵が漏れたときのほかに、見つけるものがない。
- どれかを満たさなければ、反映によらないコミットとする。コミットの記録の警告に、満たさなかった条件を書く。
- 人が GitForge の画面から merge したコミットは、`merge` の記録がないので、反映によらないコミットになる。
- 壊れているあいだは、直すための apply request の反映のコミットだけを、反映によるものとする。

## GitForge の API

| GitForge | C を merge した pull request の読み方 |
|---|---|
| GitHub | `GET /repos/{owner}/{repo}/commits/{sha}/pulls` のうち、merge されていて、`merge_commit_sha` が C で、向き先が `main` のもの |
| Forgejo | `GET /repos/{owner}/{repo}/commits/{sha}/pull` が返すもので、merge されていて、向き先が `main` のもの |

- 記録は、その pull request のコメントの一覧から読む（[GitForge アダプタの共通の書き方](../adapters/pull-requests.md)）。
- API が答えないときは、C を取り込まずに、次の見回りでやり直す。`latest` は進めない。
  - なぜ：判定できないコミットを、正しいとも壊れているとも記録しないため。
- C を merge した pull request か、`merge` の記録が見つからないときは、worker が C を初めて見てから 60 秒までは、決めずにやり直す。
  - なぜ：GitForge は、merge のすぐあと、merge のコミットから pull request を引けないことがあるから（Forgejo 17 で確かめた）。また、`merge` の記録は、merge のあとに書くから。
  - コミットの時刻は使わない。push する人が、未来の時刻にできるから。初めて見た時刻は、worker のプロセスの中だけに持つ。

## 署名

- `merge` の記録と、用意の 2 つ目のコミットには、HMAC でなく、Ed25519 の署名を付ける。
  - バックエンドは、署名の秘密鍵を持つ。worker は、公開鍵だけを持つ（[設定と秘密情報](../adapters/README.md#設定と秘密情報)）。
  - なぜ：worker が確かめるものを、worker が作れないようにするため。HMAC の鍵を worker に持たせると、worker が記録を偽れる。
- ほかの記録は、今までどおり HMAC で守る。確かめるのは、バックエンドだけだからである。
- 鍵の取り替えは、HMAC の鍵と同じく、鍵の ID で選ぶ。

## 重さ

`main` のコミット 1 つの判定に、次のものがかかる。

| もの | 量 | 目安 |
|---|---|---|
| GitForge の API | pull request の読み 1 回、コメントの一覧の読み 1 回（ページが続けば、その分） | 2 回、1 回あたり 100〜300 ms |
| 署名を確かめる | 記録 1 つ | 1 ms より小さい |
| 差分を重ねて木を比べる | 先頭のコミットの差分のファイルの数に比例する。ほかのファイルは、git の blob の SHA で比べる | 数 ms |

- 合わせて、コミット 1 つに 0.6 秒ほどである。多くは GitForge の API の待ちで、木の大きさにはほとんどよらない。
- `main` に入るコミットの数は、apply request の反映の数と同じなので、多くても 1 時間に数十である。GitHub の REST の上限（1 時間に 5,000）に対して、十分に小さい。
- 判定は、新しいコミットにだけ行う。取り込み済みのコミットは、判定し直さない。

## 確かめること

実装するときに、動く GitForge で次のことを確かめる。

- Write の権限の人が、ほかの人のレビューとコメントを、編集でき、消せるか（GitHub と Forgejo）。記録は MAC と署名で守るが、消された記録は戻らない（[信頼の境界](../adapters/pull-requests.md#信頼の境界)）。
- pull request の先頭のコミットを、直接 `main` に push したとき、GitForge が pull request を merge 済みにするか。そのとき、`merge_commit_sha` に何が入るか。
  - merge 済みになっても、`merge` の記録がないので、反映によらないコミットになる。
- squash だけを許す設定（GitHub の「Allow squash merging」だけを残す、Forgejo の merge の方法の設定）が、API の merge にも効くか。
