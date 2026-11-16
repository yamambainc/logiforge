# 画面と API

WebUI の各画面が、BE のどの API を使うかを確かめるときと、WebUI のために API を変えるときに読む。
画面の要件は [docs/webui](../../docs/webui/README.md)、API の詳細は [openapi.yaml](../api/openapi.yaml) が正本である。

## URL

画面の状態は、URL に持つ。URL を送れば、相手は同じものを見られる。

| 画面 | パス |
|---|---|
| repository の一覧 | `/` |
| スナップショットツリーのビュワー | `/r/{repository}/tree?view=&orient=&root=&node=&commit=&apply_request=&q=`（`view` は `outline`、`graph`、`3d`。`orient` は図の向きで、`h` か `v`。`root` は「ここから見る」の表示の根） |
| 人への依頼 | `/r/{repository}/human-requests?kind=&state=&id=` |
| apply request と履歴 | `/r/{repository}/apply-requests?state=&id=` |
| ダッシュボード | `/r/{repository}` |

## 画面ごとの API

### [repository の一覧](../../docs/webui/repositories.md)

- 一覧：`listRepositories`。根の主文は、`Repository` の `root_statement` を使う（[足す API](#足す-api)）。
- 作成：`createRepository`。応答の `Logiforge-Pending-Commit` があれば、スナップショットツリーが出るまで、ビュワーで「取り込み中」と示す（[書いた直後の読み](../api/writes.md)、[取り込み中の読み直し](snapshot-tree-viewer.md#取り込み中の読み直し)）。

### [スナップショットツリーのビュワー](../../docs/webui/tree-viewer.md)

- スナップショットツリー：`getTree` と `listNodes`（`root` と `depth` は指定しない）。
- 詳細のパネル：`getNode`。検証結果のレポートは `getVerdict`、ファイルは `getArtifact`、履歴は `getNodeHistory`。
- 読み方と表示の決まりは、[スナップショットツリーのビュワー](snapshot-tree-viewer.md) に書く。

### [人への依頼](../../docs/webui/human-requests.md)

- 一覧：`listHumanRequests`（`kind`、`state` で絞り込む）。
- 本文：`getHumanRequest`。対象ノードは、ビュワーへのリンクで示す（`?node=`）。
- 回答：`answerHumanRequest`。添付は base64 で回答に含める（[アップロードの大きさ](../api/README.md#アップロードの大きさ)）。
  - 送る前に、ファイルが 10 MiB 以下で、要求の本文が 32 MiB 以下になるかを確かめ、超えればリンクで添えるように示す。
- 回答の書きかけは、アクセストークンの期限でページが移動しても失わないようにする（[トークンの持ち方](auth.md#トークンの持ち方)）。
- 回答した添付：`getAnswerAttachment`。

### [apply request と履歴](../../docs/webui/history.md)

- 一覧：`listApplyRequests`（`state` で絞り込む）。
- 詳細：`getApplyRequest`。差分は `operations`、先頭のコミットは `head_commits`、レビューは `reviews` で示す。
  - 議論は、`comment` のレビューとして `reviews` に入っている。ほかの種類のレビューと同じく、時刻の順に示す。
- 反映したあとの姿：ビュワーを `?apply_request=` で開く（[差分の重ね表示](overlay.md#差分の重ね表示)）。
- 過去の時点の姿：`listCommits` で `main` のコミットを選び、ビュワーを `?commit=` で開く（[足す API](#足す-api)）。

### [ダッシュボード](../../docs/webui/dashboard.md)

- 根の値と作業の終わりの判定：`getCompletion`。
- 種類ごとの着手できる作業の件数：`listWork` を `limit=1` で呼び、応答の `counts`（ページに関わらない、種類ごとの全体の件数）を使う。
  - 件数は、open の apply request が対象にしているノードの作業を数えない（`include_claimed` を付けない）。作業の一覧と同じ数え方である。
- 画面の切り替え：デスクトップは上の帯のタブ、モバイルは下のタブ（[すべての画面の決まり](mobile.md#すべての画面の決まり)）。URL は `/r/{repository}` がダッシュボード、`/r/{repository}/tree` がビュワーである。

## 足す API

WebUI のために、次の 2 つを API に足す。どれも、BE がすでに持つものを返すだけである。

| 足すもの | 返すもの | 使う画面 | 元 |
|---|---|---|---|
| `GET /repositories/{repository}/commits`（`listCommits`） | `main` のコミットの一覧。新しい順。項目は `number`、`commit`、`committed_at`、`apply_request_id` | apply request と履歴（過去の時点） | `main` の並びの索引と、コミットの記録（[スナップショット](../backend/snapshot.md)） |
| `Repository` の `root_statement` | 根の仮説の主文 | repository の一覧 | 今の `main` のスナップショットツリー |

- `listCommits` の `apply_request_id` は、コミットの記録の trailer `Logiforge-Apply-Request:` の値である（[main のコミットと記録](../files/revisions.md)）。ない（反映によらないコミット）なら null である。
- `listCommits` のページングは、ほかの一覧と同じくカーソルで分ける。カーソルは、次に読むコミットの `number` である（[ページング](../api/README.md#ページング)）。
- なぜ `root_statement` を足すか：足さないと、一覧の行ごとに `getNode` を呼ぶことになるから。
