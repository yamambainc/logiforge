# 一覧の読み

バックエンドと worker が、一覧を返すときや、多くの項目をまとめて扱うときに、GitForge と置き場をどう読むかを確かめるときに読む。
1 回の要求で読む REST の回数の見積もりは、[費用と規模の見積もり](../estimates/README.md#github-アダプタ) が正本である。

## 決まり

- 正本は GitForge である。一覧の読みでも、正本は変えない。
- 一覧は、GitForge の一覧の API で読む。項目ごとに GitForge の API を呼ばない。
  - なぜ：項目ごとに呼ぶと、REST の回数が項目の数に比例し、主体ごとの上限（1 時間に 5,000 回）に当たるから。
  - 一覧の API のページ送り（`per_page=100`、Forgejo は 50）は、項目ごとの呼び出しに数えない。
- 一覧の API だけで足りない情報は、[キャッシュ](caches.md) から読む。
- 1 回の要求の中で、同じ一覧を二度読まない。読んだ一覧は、その要求の中で使い回す。

## どの一覧の API で読むか

| 読むもの | GitHub | Forgejo |
|---|---|---|
| apply request と、その状態 | `GET /repos/{owner}/{repo}/pulls?state=all` | `GET /repos/{owner}/{repo}/pulls?state=all` |
| apply request と人への依頼の記録、レビュー | 記録の控えと、`GET /repos/{owner}/{repo}/issues/comments?since=`（控えの読む位置から） | 記録の控えと、`GET /repos/{owner}/{repo}/issues/comments?since=` |
| 人への依頼 | `GET /repos/{owner}/{repo}/issues?state=all` | `GET /repos/{owner}/{repo}/issues?state=all&type=issues` |
| 利用者が読める repository | `GET /user/installations/{installation_id}/repositories` | （問い合わせない。[Forgejo アダプタの認証](../adapters/forgejo.md#認証)） |
| 反映した pull request | `GET /repos/{owner}/{repo}/pulls?state=closed&sort=updated&direction=desc` | `GET /repos/{owner}/{repo}/pulls?state=closed&sort=recentupdate` |

- 記録とレビューは、記録の控えと、控えの読む位置より後のコメントの一覧を合わせて作る（[キャッシュ](caches.md#記録の控え)）。
  - pull request ごとのコメントの一覧（`issues/{number}/comments`）は、一覧の読みでは使わない。
  - repository のコメントの一覧を、はじめから全部は読まない。なぜ：コメントの数は pull request の数の数倍で、全部読むとページ送りだけで REST の回数が増えるから。
- 状態（`open`、`merged`、`closed`）で絞る一覧は、GitForge から読んだ状態で先に絞り、残ったものだけを組み立てる。
  - なぜ：`state=open` の一覧で、merged と closed の apply request まで組み立てないため。
  - ブランチが消えた open の apply request は `closed` に数える（[apply request](apply-request/README.md)）。そのため、`closed` の一覧では、GitForge で open のものも残す。
- 反映した pull request の一覧は、worker が使う（[反映かの判定](verify/README.md#gitforge-の-api)）。
  - 見回りで新しい `main` のコミットがあるときだけ、1 回読む。`merge_commit_sha` が新しいコミットと同じものを探す。
  - 見つからないコミットが残るあいだだけ、次のページを読む。

## 置き場の読み

- 一覧の項目ごとに置き場を読むのは、返すページの項目（`limit` まで）だけにする（[ページ送り](../api/paging.md)）。
  - なぜ：置き場は GitForge の写しで、REST の上限には数えないが、読む回数はページの大きさまでで足りるから。
- repository の一覧で、利用者が読めるかは、上の表の「利用者が読める repository」の一覧で決める。repository ごとに `GET /repos/{owner}/{repo}` を呼ばない。
- 設定の画面の repository の一覧で、スナップショットツリーがもうあるか（`prepared`）は、置き場の `latest` があるかで決める（[latest](snapshot/main.md#latest)）。
  - なぜ：`latest` は、worker が `main` を取り込んだ印で、GitForge に repository ごとに聞かずに分かるから。
  - 登録した直後は、worker が取り込むまで（repository の一覧を読み直す間隔と、見回りの間隔のあいだ）、`prepared` は `false` に見える。

## 呼び出しの回数のテスト

- 一覧を返す API ごとに、GitForge のポートの下の API を呼んだ回数を数えるテストを置く。
  - 項目が 2 つのときと 40 個のときで、呼んだ回数が同じであることを確かめる。ページ送りの分は除く。
  - 数えるのは、`pullreq` の API と、GitHub と Forgejo のアダプタが送った HTTP の要求である。
- 新しく一覧を返す API を足すときは、このテストにも足す。
- なぜ：項目ごとの呼び出しは、少ない項目のテストでは遅さとして見えず、レビューでも見落としやすいから。
