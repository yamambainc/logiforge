# 一覧の読み

バックエンドと worker が、一覧を返すときや、多くの項目をまとめて扱うときに、GitForge と置き場をどう読むかを確かめるときに読む。
1 回の要求で読む REST の回数の見積もりは、[費用と規模の見積もり](../estimates/README.md#github-アダプタ) が正本である。

- 正本は GitForge である。一覧の読みでも、正本は変えない。
- 一覧は、GitForge の一覧の API で読む。項目ごとに GitForge の API を呼ばない。
  - なぜ：項目ごとに呼ぶと、REST の回数が項目の数に比例し、主体ごとの上限（1 時間に 5,000 回）に当たるから。
  - 一覧の API のページ送り（`per_page=100`、Forgejo は 50）は、項目ごとの呼び出しに数えない。
- 一覧の API だけで足りない情報は、置き場の記録の控えと `latest` から読む（[キャッシュ](caches.md)）。
- 1 回の要求の中で、同じ一覧を二度読まない。読んだ一覧は、その要求の中で使い回す。

## どの一覧の API で読むか

| 読むもの | GitHub | Forgejo |
|---|---|---|
| open の apply request | `pulls?state=open` | `pulls?state=open` |
| merged の apply request | `pulls?state=closed` | `pulls?state=closed` |
| closed の apply request、状態で絞らない一覧 | `pulls?state=all` | `pulls?state=all` |
| 記録とレビュー | 記録の控えと `issues/comments?since=` | 記録の控えと `issues/comments?since=` |
| 人への依頼 | `issues?state=all`（pull request も返る） | `issues?state=all&type=issues` |
| GitForge の repository の識別子 | pull request の一覧の `base.repo.node_id` | pull request の一覧の `base.repo.id` |
| 利用者が読める repository | `/user/installations/{installation_id}/repositories` | （問い合わせない。[Forgejo アダプタの認証](../adapters/forgejo.md#認証)） |
| 反映した pull request（worker） | `pulls?state=closed&sort=updated&direction=desc` | `pulls?state=closed&sort=recentupdate` |

- パスは、どれも `GET /repos/{owner}/{repo}/` の下である（利用者が読める repository を除く）。
- GitForge で open の pull request は、バックエンドで open の apply request をすべて含む。そのため、open の一覧は `state=open` で足りる。
  - ブランチが消えた open の apply request は `closed` に数える（[apply request](apply-request/README.md)）。そのため、`closed` の一覧では、GitForge で open のものも読む。
- 記録とレビューは、記録の控えと、控えの読む位置より後のコメントを合わせて作る（[キャッシュ](caches.md#記録の控えの読み方)）。
  - pull request ごとのコメントの一覧（`issues/{number}/comments`）は、一覧の読みでは使わない。
  - repository のコメントの一覧を、はじめから全部は読まない。なぜ：コメントの数は pull request の数の数倍で、全部読むとページ送りだけで REST の回数が増えるから。
- GitForge の repository の識別子は、その要求で読んだ pull request の一覧から取る。
  - 一覧を読まない経路（1 件の読み、人への依頼の一覧、書き込み）と、一覧が空のときは、`GET /repos/{owner}/{repo}` を要求ごとに 1 回だけ呼び、その要求の中でだけ覚える。
  - 一覧が空で、ほかに記録が要らないとき（open の apply request の一覧など）は、識別子も記録も読まない。

## 1 件の読み

- apply request を 1 件読むときは、記録の控えと `since` のコメントから、識別子に当たる pull request の番号を引き、`pulls/{number}` を読む。
  - なぜ：pull request の一覧をたどってタイトルで探すと、REST の回数が P に比例するから。
- 人への依頼の回答の添付は、その issue のコメント（`issues/{number}/comments`）から読む。控えは添付を持たないから（[記録の控え](verify/records.md)）。

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
- repository の一覧で、利用者が読めるかは、上の表の「利用者が読める repository」で決める。repository ごとに `GET /repos/{owner}/{repo}` を呼ばない。
- 設定の画面の repository の一覧で、`prepared` は、置き場の `latest` があるかで決める（[latest](snapshot/main.md#latest)）。
  - なぜ：`latest` は、worker が `main` を取り込んだ印で、GitForge に repository ごとに聞かずに分かるから。
  - 登録した直後は、worker が取り込むまで、`prepared` は `false` に見える。WebUI での扱いは [セットアップの画面](../webui/screens-setup.md) に書く。

## 呼び出しの回数のテスト

- 一覧を返す API ごとに、GitHub と Forgejo のアダプタが送った HTTP の要求の数を数えるテストを置く。
  - 項目が 2 つのときと 40 個のときで、呼んだ回数が同じであることを確かめる。ページ送りの分は除く。
- 新しく一覧を返す API を足すときは、このテストにも足す。
- なぜ：項目ごとの呼び出しは、少ない項目のテストでは遅さとして見えず、レビューでも見落としやすいから。
