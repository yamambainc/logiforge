# バックエンド

バックエンド（core）が、どのルールを持ち、何を計算するかを確かめるときに読む。
ルールを持つのはこのレイヤーだけ、という決まりは [レイヤー構成](../architecture.md) が正本である。

## 読む順

1. このページ：受け持つ要件、認可、repository の用意、操作と差分、状態を持たない
2. [差分の検査](checks.md)
3. [反映の手順](merge.md)
4. [計算](compute.md)
5. [apply request](apply-request/README.md)
6. [人への依頼の起票と回答](human-requests.md)
7. [ポート](ports/README.md)
8. [スナップショット](snapshot/README.md)
9. [worker](worker/README.md)
10. [最新のコミットが valid でないとき](invalid-main.md)
11. [一覧の読み](list-reads.md)
12. [キャッシュ](caches.md)

## 受け持つ要件

| 受け持つもの | 要件 | 実現 |
|---|---|---|
| 不変条件 | [仮説とストーリーの木](../../docs/tree/README.md)、[inactive 化](../../docs/tree/inactive.md) | [差分の検査](checks.md) |
| 反映の条件 | [論理レビュー](../../docs/apply-request/review.md) | [反映の手順](merge.md) |
| 対象ノードの変化 | [反映](../../docs/apply-request/README.md) | [反映の手順](merge.md) |
| 値の計算 | [真偽値の伝播](../../docs/tree/propagation.md) | [計算](compute.md) |
| 着手できる作業 | [着手できる作業](../../docs/work/README.md) | [計算](compute.md) |
| 作業の終わりの判定 | [作業の終わりの判定](../../docs/work/README.md) | [計算](compute.md) |
| 認可 | [役割](../../docs/roles/README.md) | 下の「認可」 |

## 認可

- 主体には、次の 2 種類がある。
  - `user`：WebUI を使う利用者
  - `client`：CLI を使う主体（エージェントか人）
- `user` が使える書き込みは、利用者の次の操作だけである。
  - repository の作成（根の主文を含む）
  - 人への依頼への回答
  - apply request のレビューと merge（[WebUI からのレビュー](../settings/users.md#webui-からのレビュー)）
  - 設定の変更（設定の API）
- それ以外の書き込みは、`client` だけが使える。
- 主体と主体の種類は、[認証](ports/authentication.md) で確かめる。
- repository を使えるのは、その repository を読める主体だけである。
- repository ごとの `user` の書き込み（repository の作成、人への依頼への回答、レビュー）は、その repository に Write があるときだけ受け付ける（[repository ごとの権限](../settings/users.md#repository-ごとの権限)）。
- 設定の API は、`user` だけが使える（[利用者の決め方](../settings/users.md#利用者の決め方)）。
  - `user` でなければ、`forbidden` を返す。
- セットアップの API は、セットアップが終わるまで、だれでも使える（[初回のセットアップ](../settings/setup.md#だれがセットアップできるか)）。

## repository の用意

利用者が repository の名前と根の主文を送ったら、バックエンドは次の順に処理する。

1. GitForge のポートの「名前から gitforge_ref を求める」を呼ぶ（[repository の登録](../adapters/README.md)）。
2. GitForge のポートの「repository を用意する」を、作る利用者のトークンで呼ぶ。根の仮説だけを入れた最初のコミットを、`main` に置く。
   - 最初のコミットの中身は、どの構成でも同じである。
3. `latest` が出るまで、上限つきで待ってから応答する（[書いた直後の読み](../api/writes.md)）。
   - `latest` があると、repository が API から見えるようになる（下の「状態を持たない」）。

途中で失敗したら、同じ要求をもう一度送れば、続きから終わらせられる。

- 手順 2 が「`main` がすでにある」で失敗したら、バックエンドは、`latest` のスナップショットを読み（なければ worker を待つ）、次のように判定する。
  - `latest` のコミットの番号が 1 で、根の主文が同じなら、済んでいるものとして手順 3 に進む。
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
- バックエンドが自分でファイルを書き出す（serialize）のは、最初のコミットだけである。反映は、GitForge の squash merge である。反映の検査では、git のファイルを読んで解析せず、スナップショットを使う（[反映の手順](merge.md)）。
- 先頭のコミットの決め方と、apply request の作成・更新の順は、[apply request](apply-request/README.md) に書く。

## 状態を持たない

- バックエンドは、木と apply request と人への依頼のデータを保持しない。プロセスのメモリのキャッシュは、権限を問い合わせた結果（60 秒）のほかに持たない（[権限のキャッシュ](../settings/users.md#権限のキャッシュ)）。
  - 要求のほかにする仕事は、worker のトークンを 1 分ごとに作り直すことだけである（[worker のトークン](../adapters/github-app.md#worker-のトークン)）。何台並べてもよい（[バックエンドだけを増やすとき](../deploy/README.md#バックエンドだけを増やすとき)）。
  - 保持するのは、外のサービス（正本）、worker（clone）、静的ファイルストレージ（スナップショット）である。
- バックエンドが持つ唯一の永続のデータは、設定の DB である（[設定の DB](../settings/database.md)）。
  - なぜ持つか：repository の一覧と GitForge とのつなぎ方は、git にも GitForge にも正本がなく、利用者が WebUI から変えるから（[設定](../settings/README.md)）。
  - 設定の DB は、要求ごとに読み、メモリに保持しない（[設定を読むとき](../settings/README.md#設定を読むとき)）。
    - そのため、同じ設定の DB を読むバックエンドどうしで、設定は食い違わない。
  - 設定の DB の SQLite のファイルは、NFS などで、ほかのノードと共有しない（[DB の種類](../settings/database.md#db-の種類)）。
  - apply request の作成のために、作業用の git の複製を一時的なディスクに持つ。これは使い捨てのキャッシュで、正本ではない（[作業用の git の複製](apply-request/create.md#作業用の-git-の複製)）。
  - 作業用の git の複製は、プロセスの間で共有しないので、何台並べても互いに影響しない。
  - 書き込みの正しさは、ロックではなく、git の ref の CAS で守る（[反映の手順](merge.md)、[apply request](apply-request/README.md)）。
- git を読むのは、apply request の作成の検査のときだけで、作業用の複製から読む。要求ごとに clone せず、fetch だけにする。
- git に書くのは、GitForge の API で、repository の用意と pull request の merge だけである（[GitForge のポート](ports/gitforge.md)）。
  - なぜ：ブランチの先頭のコミットや `main` の更新を、GitForge の pull request の仕組みにそのまま任せ、バックエンドが git の ref を自前で扱わずに済むようにするため。
- 置き場と GitForge から読んだものも、要求ごとに読み直す。
  - 一覧は、GitForge の一覧の API で読み、項目ごとに GitForge を呼ばない（[一覧の読み](list-reads.md)）。
  - 置き場は、転送の費用なしで読める場所に置く。遅さが問題になったときは、置き場の手前に CDN などを置く（[スナップショットの置き場のアダプタ](../adapters/snapshot-store.md)）。
- 認証も、要求ごとに GitForge で確かめる。結果を保持しない（[認証](ports/authentication.md)）。
- repository の一覧は、そのつど置き場の `repositories` から読む（[latest](snapshot/main.md#latest)）。
  - `latest` がある repository だけが入る。`latest` のないものは `not_found` に見える。
  - なぜ：用意が途中で止まった repository を、スナップショットツリーのない repository として見せないため。同じ名前で作り直せば、用意の続きから終わる。
- `main` のコミットが valid か（[最新のコミットが valid でないとき](invalid-main.md)）は、worker が作るスナップショットから出す。定期的な突き合わせは、行わない。
