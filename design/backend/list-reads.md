# 一覧の読み

バックエンドと worker が、一覧を返すときや、多くの項目をまとめて扱うときに、GitForge と置き場（スナップショットの置き場）をどう読むかを確かめるときに読む。
1 回の要求で読む回数の見積もりは、[費用と規模の見積もり](../estimates/README.md#github-アダプタ) が正本である。

- 正本は GitForge である。一覧の読みでも、正本は変えない。
- 一覧は、GitForge の一覧の API で読む。項目ごとに GitForge の API を呼ばない。
  - なぜ：項目ごとに呼ぶと、呼ぶ回数が項目の数に比例し、主体ごとの上限（GitHub は 1 時間に REST 5,000 回）に当たるから。
  - 一覧の API のページ送りは、項目ごとの呼び出しに数えない。
- 一覧の API だけで足りない情報は、置き場の `latest` と、権限のキャッシュから読む（[キャッシュ](caches.md)）。
- 1 回の要求の中で、同じ一覧を二度読まない。読んだ一覧は、その要求の中で使い回す。

## どの一覧の API で読むか

| 読むもの | GitHub | Forgejo |
|---|---|---|
| apply request（pull request）と、そのレビュー待ちか修正待ちか | `GET /repos/{owner}/{repo}/pulls` | `GET /repos/{owner}/{repo}/pulls` |
| 人への依頼（issue）と、回答済みか | `GET /repos/{owner}/{repo}/issues?state=all&labels=…`（pull request も返るので除く） | `GET /repos/{owner}/{repo}/issues?state=all&type=issues&labels=…` |
| 主体が読める repository | `GET /user/repos?affiliation=owner,collaborator,organization_member`（60 秒持つ） | （問い合わせない。[Forgejo アダプタの認証](../adapters/forgejo.md#認証)） |
| 反映した pull request（worker） | `GET /repos/{owner}/{repo}/pulls?state=closed&sort=updated&direction=desc` | `GET /repos/{owner}/{repo}/pulls?state=closed&sort=recentupdate` |

- 一覧では、review を読まない。レビュー待ちか修正待ちかは、pull request の一覧に入っているラベルで決める（[レビュー待ちと修正待ちのラベル](../adapters/pull-requests.md#レビュー待ちと修正待ちのラベル)）。
  - なぜ：review は、GitHub の REST でも Forgejo でも、pull request ごとにしか読めないから。review の状態で絞れる一覧もない。
- pull request の一覧の各項目には、番号、作者、状態、merge のコミット、head の SHA とブランチ、向き先、ラベルが入っている（GitHub と Forgejo）。一覧に要るのは、これだけである。
- open の一覧は、`state=open` で読む。ブランチが消えた open の apply request は `closed` に数える（[apply request の状態](../adapters/pull-requests.md#apply-request-の状態)）。そのため、`closed` の一覧では、GitForge で open のものも読む。
- 人への依頼の種類はラベル、回答済みかは issue の状態、対象ノードは issue のタイトルの頭で決める（[人への依頼の issue](../adapters/texts.md#人への依頼の-issue)）。どれも issue の一覧に入っている。

## 1 件の読みと書き込み

- apply request を 1 件読むときと、`approve` で反映するときは、その pull request と、その review の一覧と、head の commit status を、その場で読む。
  - なぜ：反映の条件は、今の GitForge の review で決めるから（[反映の手順](merge.md)）。
  - GitHub では、その pull request の review を、GraphQL の 1 回で読む。書いた人が push できるか（`authorCanPushToRepository`）を、読む権限だけで読めるからである（[数える review](../adapters/pull-requests.md#数える-review)）。GraphQL を使うのは、ここだけである。
- 人への依頼を 1 件読むときは、その issue のコメントから回答を読む（[回答](../adapters/texts.md#回答)）。

## 反映した pull request の一覧（worker）

- worker は、見回りで新しい `main` のコミットがあるときだけ読む（[反映かの判定](verify/README.md#gitforge-の-api)）。
- `merge_commit_sha` が新しいコミットと同じものを探す。見つからないコミットが残るあいだだけ、次のページを読む。
- pull request の `updated_at` が、探しているコミットの committer date より 5 分以上前になったら、そこで読むのをやめる。
  - なぜ：merge した時刻は `updated_at` より前にならないので、それより先のページに答えはないから。直接 push したコミットのために、closed の pull request を全部読まないため。
  - なぜ 5 分か：merge のコミットの時刻と `updated_at` を付ける時計のずれを見込むため。
  - committer date が今より後なら、今の時刻を使う。
    - なぜ：committer date を未来にできるのは、直接 push したコミットだけである。そのコミットは、どのみち反映した pull request が見つからない。未来のままだと、やめる条件に当たらず、待つ 60 秒のあいだ、見回りごとに closed の pull request を全部読むから。

## 置き場の読み

- 一覧の項目ごとに置き場を読むのは、返すページの項目（`limit` まで）だけにする（[ページ送り](../api/paging.md)）。
  - ただし、`closed` の一覧では、GitForge で open の apply request ごとに、ブランチの履歴を読む。ブランチが消えたかを決めるためである。読む数は、open の数に比例する。
- repository の一覧で、主体が読めるかは、上の表の「主体が読める repository」で決める。repository ごとに `GET /repos/{owner}/{repo}` を呼ばない。
  - 一覧にないもの（主体が関わっていない public のリポジトリ）だけ、そのリポジトリを 1 件読んで確かめ、その結果も 60 秒持つ。
  - なぜ一覧に頼りきらないか：GitHub の一覧は、関わっていない public のリポジトリを返さないから。そうしたリポジトリは、ふつうは少ない。
- 設定の画面の repository の一覧で、`prepared` は、置き場の `latest` があるかで決める（[latest](snapshot/main.md#latest)）。
  - なぜ：`latest` は、worker が `main` を取り込んだ印で、GitForge に repository ごとに聞かずに分かるから。
  - 登録した直後は、worker が取り込むまで、`prepared` は `false` に見える。WebUI での扱いは [セットアップの画面](../webui/screens-setup.md) に書く。

## 呼び出しの回数のテスト

- 一覧を返す API ごとに、項目が 2 つのときと 40 個のときで、GitForge を呼ぶ回数が同じであることを確かめるテストを置く。数え方は、次の 2 つである。
  - バックエンドのテスト：GitForge のポートの、すべてのメソッドの呼び出しを数える（作業の一覧、作業の終わりの判定、apply request の一覧、人への依頼の一覧）。apply request と人への依頼の両方を、2 つと 40 個にする。
  - アダプタのテスト：GitHub と Forgejo の REST の呼び方（`pullreq` の API）の呼び出しを、偽の API で数える（apply request と人への依頼の一覧、repository の一覧、設定の repository の一覧）。ページ送りの分は除く。
- 新しく一覧を返す API を足すときは、このテストにも足す。
- なぜ：項目ごとの呼び出しは、少ない項目のテストでは遅さとして見えず、レビューでも見落としやすいから。
