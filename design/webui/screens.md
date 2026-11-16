# 画面と API

WebUI の各画面が、BE のどの API を使うかを確かめるときと、WebUI のために API を変えるときに読む。
画面の要件は [docs/webui](../../docs/webui/README.md)、API の詳細は [openapi.yaml](../api/openapi.yaml) が正本である。

## URL

画面の状態は、URL に持つ。URL を送れば、相手は同じものを見られる。

| 画面 | パス |
|---|---|
| repository の一覧 | `/` |
| スナップショットツリーのビュワー | `/r/{repository}/tree?view=&orient=&root=&node=&commit=&apply_request=&q=&f=`（`view` は `outline`、`graph`、`3d`。`orient` は図の向きで、`h` か `v`。`root` は「ここから見る」の表示の根） |
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
- 回答した添付：`getAnswerAttachment`。保存させるだけにする（[ファイルの開き方](security.md)）。
- リンクの添付は、BE が回答の本文の末尾に「添付（リンク）」として書く。回答の `attachments` には、ファイルだけが入る。
- 書きかけは、書くたびに `sessionStorage` に置く（本文と、添付の名前とリンク）。回答したら消す。
- 状態の絞り込みは、既定を open にする。
- 「作業として残っているもの」の絞り込み（`work=1`）は、`listWork` の `kind=human_request_answered` の `subject_id` に入る依頼だけを示す。ダッシュボードの「回答済みの依頼」からは、`state=answered&work=1` で開く。件数と一覧の数は同じになる。
- 回答が `not_open` になったら（ほかのタブや人が先に回答した）、「この依頼は、もう回答されています」と示し、読み直して回答を示し、書きかけを消す。
- サインアウトでは、書きかけをすべて消す。
- 添付のリンクは、送る前も示すときも、`new URL()` で読んで、`protocol` が http か https かを確かめる。
- ファイルを 2 つ以上選び、合計が 8 MiB を超えたら、モバイルでは送るのに時間がかかることがある、と示す。上限は変えない。

### [apply request と履歴](../../docs/webui/history.md)

- 一覧：`listApplyRequests`（`state` で絞り込む）。
- 詳細：`getApplyRequest`。差分は `operations`、先頭のコミットは `head_commits`、レビューは `reviews` で示す。
  - 議論は、`comment` のレビューとして `reviews` に入っている。ほかの種類のレビューと同じく、時刻の順に示す。
- 反映したあとの姿：ビュワーを `?apply_request=` で開く（[差分の重ね表示](overlay.md#差分の重ね表示)）。
- 過去の時点の姿：`listCommits` で `main` のコミットを選び、ビュワーを `?commit=` で開く（[足す API](#足す-api)）。
  - 画面の「main の履歴」で、新しい 50 個ずつ示し、「もっと読む」で続きを読む。
- 反映済みの apply request は、「反映した時点を見る」で、`merged_commit` のビュワーを開く。
- 差分の操作の対象は、ビュワーへのリンク（`?node=`）で示す。
- 作業としての絞り込み（`work=review_waiting`、`work=fix_waiting`）は、`listWork` のその種類の `subject_id` に入る apply request だけを示す。ダッシュボードの件数から、この絞り込みで開く。
- URL は `/r/{repository}/apply-requests?view=&state=&id=&work=`（`view` は、apply request か `commits`）。
- ダッシュボードの「引用元が変化した検証結果」は、ビュワーを `f=cited` で開く。`listWork` の `citation_changed` の `node_id`（検証結果を持つ仮説）だけを示す。

### [ダッシュボード](../../docs/webui/dashboard.md)

- 根の値と作業の終わりの判定：`getCompletion`。
- 種類ごとの着手できる作業の件数：`listWork` を `limit=1` で呼び、応答の `counts`（ページに関わらない、種類ごとの全体の件数）を使う。
  - 件数は、open の apply request が対象にしているノードの作業を数えない（`include_claimed` を付けない）。作業の一覧と同じ数え方である。
- 件数を押すと、その作業の一覧へ行く。未決の葉と要再検討は、作業と同じ定義の絞り込みでビュワーを開く（`f=leaf`、`f=nr-work`）。件数と、ビュワーの一致の数は同じになる。レビュー待ちと修正待ちは apply request の一覧、回答済みの依頼は人への依頼の一覧、引用元が変化した検証結果はビュワーの絞り込みに、それぞれの画面ができたらつなぐ。
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
