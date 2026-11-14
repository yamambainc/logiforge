# バックエンド

バックエンド（core）が、どのルールを持ち、何を計算するかを確かめるときに読む。
ルールを持つのはこのレイヤーだけ、という決まりは [レイヤー構成](../architecture.md) が正本である。

## 持つルール

| ルール | 要件 | 実現 |
|---|---|---|
| 不変条件 | [仮説とストーリーの木](../../docs/tree/README.md)、[inactive 化](../../docs/tree/inactive.md) | [差分の検査](checks.md) |
| 反映の条件 | [論理レビュー](../../docs/apply-request/review.md) | [反映の手順](merge.md) |
| 対象ノードの変化 | [反映](../../docs/apply-request/README.md) | [反映の手順](merge.md) |
| 値の計算 | [真偽値の伝播](../../docs/tree/propagation.md) | 下の「計算」 |
| 着手できる作業 | [着手できる作業](../../docs/work/README.md) | 下の「計算」 |
| 作業の終わりの判定 | [作業の終わりの判定](../../docs/work/README.md) | 下の「計算」 |
| 認可 | [役割](../../docs/roles/README.md) | 下の「認可」 |

バックエンドが使うポートは、[ポート](ports/README.md) に、木の読み方は [スナップショット](snapshot.md) に書く。

## 認可

- 主体には、次の 2 種類がある。
  - `user`：WebUI を使う利用者
  - `client`：CLI を使う主体（エージェントか人）
- `user` が使える書き込みは、利用者の 2 つの操作だけである。
  - repository の作成（根の主文を含む）
  - 人への依頼への回答
- それ以外の書き込みは、`client` だけが使える。
- 主体と主体の種類は、[認証](ports/authentication.md) で確かめる。
- repository を使えるのは、その repository を読める主体だけである。

## repository の用意

利用者が repository の名前と根の主文を送ったら、バックエンドは次の順に処理する。

1. GitForge のポートの「名前から gitforge_ref を求める」を呼ぶ（[repository の登録](../adapters/README.md)）。
2. GitForge のポートの「repository を用意する」を呼ぶ。根の仮説だけを入れた最初のコミットを、`main` に置く。
   - 最初のコミットの中身は、どの構成でも同じである。
3. worker を起こし、`latest` が出るまで、上限つきで待ってから応答する（[書いた直後の読み](../api/writes.md)）。
   - `latest` があると、repository が API から見えるようになる（下の「状態を持たない」）。

途中で失敗したら、同じ要求をもう一度送れば、続きから終わらせられる。

- 手順 2 が「`main` がすでにある」で失敗したら、バックエンドは、`main` のスナップショットを読み（なければ worker を待つ）、次のように判定する。
  - `main` が最初のコミットだけで、根の主文が同じなら、済んでいるものとして手順 3 に進む。
  - それ以外なら、`repository_exists` で失敗する。
- なぜ：2 つの用意が同時に走っても、負けた側が、この判定で成功して終われるようにするため。

## 操作と差分

- 4 種類の操作（[操作](../../docs/apply-request/README.md)）と、ファイルの変化は、1 対 1 に対応する。
  - `story.create`：仮説とストーリーのディレクトリを、ファイルごと加える。
  - `story.inactivate`：`inactivated.md` を加える。
  - `verdict.submit`：`verdicts/` に検証結果のファイルを、`artifacts/sha256/` に artifact のファイルを加える。
  - `node.edit`：`background.md` の末尾に文章を加える。
- 利用者は、ファイルを変えたブランチを push する。バックエンドは、その差分のスナップショットから、操作を復元する。
  - 4 種類の操作として読めない変化は、[差分の検査](checks.md) で拒む。
  - なぜ：操作を別に保存しなくても、差分だけで API の応答を作れるようにするため。
- バックエンドが自分でファイルを書き出す（serialize）のは、最初のコミットだけである。反映は、GitForge の squash merge である。git のファイルを読んで解析しない。
- 版の作り方と、apply request の作成・更新の順は、[apply request の作成と更新](apply-request.md) に書く。

## 計算

バックエンドは、次のものを [スナップショット](snapshot.md) と、GitForge から読んだやりとりから計算する。

- 値と、葉・要再検討・inactive
  - worker がスナップショットを作るときに計算して入れたものを、そのまま使う。
- 引用元の変化
  - 有効な検証結果の部分木の引用ごとに、部分木のノードの変更の一覧に、引用したリビジョンの番号より大きい番号があるかを見る。
- 着手できる作業（[着手できる作業](../../docs/work/README.md)）
  - 木の作業は、スナップショットの木から計算する。
  - apply request と人への依頼の作業は、[ブランチの履歴](snapshot.md) と、GitForge から読んだ提案、レビュー、人への依頼から計算する。
  - open の apply request の対象ノードは、今の版の差分のファイルのパスを含む、今の木のいちばん近いノードである（新しいストーリーは親の仮説、`inactivated.md` はそのストーリー、検証結果と背景情報はその仮説になる）。今の版のスナップショットがなければ、対象ノードはないものとする。
  - レビュー待ちと修正待ちは、今の版への最後の決めるレビュー（`approve` か `request_changes`）と、今の版の反映の失敗で決める。
    - 修正待ち：`request_changes` か、opener が直すべき理由（`stale_target`、`invariant_violation`、`conflict`）での反映の失敗がある。
    - レビュー待ち：決めるレビューがないか、`approve` のあと、そのほかの理由（`snapshot_pending` など）で反映に失敗した（`approve` し直しを待つ）。
    - [着手できる作業](../../docs/work/README.md) の「反映に失敗して `open` に戻った」は、opener が直すべき失敗のことである。
  - 回答済みの人への依頼は、次のどちらかで作業から外す。
    - その回答を引用した検証結果が、今の `main` の木か、open の apply request の今の版の差分の検証結果のファイルにある。引用とは、検証結果の artifact が、回答の添付を読む API の URL の `link` であるか、その sha256 を持つことである（[人への依頼の回答](../files/artifacts.md)）。背景情報など、検証結果でないファイルは見ない。
    - 対象ノードを最後に変えた反映のコミットが、回答より後である。
  - 引用元の変化は、引用したリビジョンが `main` の並びにないときも、変化したものとみなす。
- 作業の終わりの判定
  - 根拠部分木をスナップショットの木から求め、上の着手できる作業のうち、根拠部分木にあるものを数える。
  - 対象ノードのわからない（worker が追いついていない）レビュー待ち・修正待ちの apply request があれば、終わりとしない。

## 状態を持たない

- バックエンドは、データを保持しない。永続のデータも、プロセスのメモリのキャッシュも持たない。何台並べてもよい（[バックエンドだけを増やすとき](../deploy.md#バックエンドだけを増やすとき)）。
  - 保持するのは、外のサービス（正本）、worker（clone）、静的ファイルストレージ（スナップショット）である。
  - 注記：apply request の作成のために、作業用の git の複製を一時的なディスクに持つ。これは使い捨てのキャッシュで、正本ではない。消えても、次の要求で作り直す。プロセスの間で共有しないので、何台並べても互いに影響しない（[作業用の git の複製](apply-request.md#作業用の-git-の複製)）。
  - 書き込みの正しさは、ロックではなく、git の ref の CAS で守る（[反映の手順](merge.md)、[apply request の作成と更新](apply-request.md)）。
- git を読むのは、apply request の作成の検査のときだけで、作業用の複製から読む。要求ごとに clone せず、fetch だけにする。
- git に書くのは、GitForge の API で、repository の用意と pull request の merge だけである（[GitForge のポート](ports/gitforge.md)）。
- 置き場と GitForge から読んだものも、要求ごとに読み直す。置き場は、転送の費用なしで読める場所に置く。遅さが問題になったときは、置き場の手前に CDN などを置く（[スナップショットの置き場のアダプタ](../adapters/snapshot-store.md)）。
- 認証も、要求ごとに GitForge で確かめる。結果を保持しない（[認証](ports/authentication.md)）。
- repository の一覧は、そのつど置き場の `repositories` から読む（[latest](snapshot.md)）。
  - `latest` がある repository だけが入る。`latest` のないものは `not_found` に見える。
  - なぜ：用意が途中で止まった repository を、木のない repository として見せないため。同じ名前で作り直せば、用意の続きから終わる。
- 反映によらない変更と force push の警告は、worker が作るスナップショットから出す。定期的な突き合わせは、行わない。
