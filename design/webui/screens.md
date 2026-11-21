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
| セットアップ | `/setup`。manifest flow から戻る先は `/setup/github/callback` |
| 設定 | `/settings?tab=&add=`（`tab` は `repository`、`gitforge`。`add` は `new` か `first` で、repository を足す段を開く） |
| サインインから戻る先 | `/oauth/callback`（[サインインの流れ](signin-flow.md)） |

## 画面ごとの API

### [repository の一覧](../../docs/webui/repositories.md)

- 一覧：`listRepositories`。根の主文は、`Repository` の `root_statement` を使う（[WebUI のための API](#webui-のための-api)）。
- 「新しい repository」は、`/settings?tab=repository&add=new` を開く。作成は、設定の画面で行う（[セットアップと設定の画面と API](screens-setup.md#設定)）。

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
- レビューの材料：`getReviewMaterial`。レビューの欄に示す。説明欄は示さない。
- 自分が作ったか：`getApplyRequest` の `opened_by_caller`。`true` なら、approve の前に示す。
- レビュー：`reviewApplyRequest`。`commit` には、画面で示した先頭のコミットを送る。`commit_conflict` なら、新しい先頭のコミットを読み直して示し、もう一度確かめさせる。
  - 応答の `apply_request` で、反映したか、反映の失敗の理由を示す。
  - 議論は、`comment` のレビューとして `reviews` に入っている。ほかの種類のレビューと同じく、`created_at` の順に示し、書いた人（`reviewer`）と時刻を添える。
  - 差分の `verdict.submit` は、レポートを折りたたみで、Markdown として示す（[Markdown と Mermaid の表示](markdown.md)）。
- 一覧は、50 個ずつ読み、「もっと読む」で続きを読む。作業としての絞り込みでは、対象の apply request だけを読む。
- 反映したあとの姿：ビュワーを `?apply_request=` で開く（[差分の重ね表示](overlay.md#差分の重ね表示)）。
- 過去の時点の姿：`listCommits` で `main` のコミットを選び、ビュワーを `?commit=` で開く（[WebUI のための API](#webui-のための-api)）。
  - 画面の「main の履歴」で、新しい 50 個ずつ示し、「もっと読む」で続きを読む。
- 反映済みの apply request は、「反映した時点を見る」で、`merged_commit` のビュワーを開く。
- 差分の操作の対象は、ビュワーへのリンク（`?node=`）で示す。
- 作業としての絞り込み（`work=review_waiting`、`work=fix_waiting`）は、`listWork` のその種類の `subject_id` に入る apply request だけを示す。ダッシュボードの件数から、この絞り込みで開く。
- URL は `/r/{repository}/apply-requests?view=&state=&id=&work=`（`view` は、apply request か `commits`）。

### [ダッシュボード](../../docs/webui/dashboard.md)

- 根の値と作業の終わりの判定：`getCompletion`。
- 種類ごとの着手できる作業の件数：`listWork` を `limit=1` で呼び、応答の `counts`（ページに関わらない、種類ごとの全体の件数）を使う。
  - 件数は、open の apply request が対象にしているノードの作業を数えない（`include_claimed` を付けない）。作業の一覧と同じ数え方である。
- 件数を押すと、その作業の一覧へ行く。未決の葉と要再検討は、作業と同じ定義の絞り込みでビュワーを開く（`f=leaf`、`f=nr-work`）。件数と、ビュワーの一致の数は同じになる。レビュー待ちと修正待ちは apply request の一覧、回答済みの依頼は人への依頼の一覧、引用元が変化した検証結果はビュワーの絞り込みにつなぐ。
- ダッシュボードの「引用元が変化した検証結果」は、ビュワーを `f=cited` で開く。`listWork` の `citation_changed` の `node_id`（検証結果を持つ仮説）だけを示す。
  - 件数の単位は検証結果である。1 つの仮説が、引用元の変わった検証結果を 2 つ持つこともあるので、ビュワーの帯には「検証結果 N 件（仮説 M 個）」と両方を示す。
- ダッシュボードの件数には、単位（件）を添える。
- 画面の切り替え：デスクトップは上の帯のタブ、モバイルは下のタブ（[画面の骨組み](mobile.md#画面の骨組み)）。URL は `/r/{repository}` がダッシュボード、`/r/{repository}/tree` がビュワーである。

セットアップと設定の画面が使う API は、[セットアップと設定の画面と API](screens-setup.md) に書く。

## WebUI のための API

API は、WebUI のために次の 2 つを持つ。どれも、BE が持つものを返すだけである。

| もの | 返すもの | 使う画面 | 元 |
|---|---|---|---|
| `GET /repositories/{repository}/commits`（`listCommits`） | `main` のコミットの一覧。新しい順。項目は `number`、`commit`、`committed_at`、`apply_request_id` | apply request と履歴（過去の時点） | `main` の並びの索引と、コミットの記録（[スナップショット](../backend/snapshot/README.md)） |
| `Repository` の `root_statement` | 根の仮説の主文 | repository の一覧 | 今の `main` のスナップショットツリー |

- `listCommits` の `apply_request_id` は、worker の反映かの判定の結果である（[main のコミットと記録](../files/revisions.md)）。ない（反映によらないコミット）なら null である。
- `listCommits` のページングは、ほかの一覧と同じくカーソルで分ける。カーソルは、次に読むコミットの `number` である（[ページング](../api/paging.md)）。
- なぜ `root_statement` を持つか：持たないと、一覧の行ごとに `getNode` を呼ぶことになるから。
