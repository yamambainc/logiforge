# コマンドの一覧

CLI のコマンドと、そのコマンドが呼ぶ API と git の操作を引くときに使う。
API の詳細は、[openapi.yaml](../api/openapi.yaml) が正本である。

## repository と認証

| コマンド | API | git |
|---|---|---|
| `auth login --with-token \| --token <トークン> [--host <URL>]` | なし | なし |
| `auth setup-git [--url <GitForge の URL>] [--insecure]` | `listRepositories`（URL を求めるときだけ） | GitForge の URL にだけ、credential helper を設定する |
| `auth git-credential get` | なし | credential helper の本体。git が呼ぶ |
| `commit [git commit の引数]` | なし | `git commit` に、セッションの trailer を付けて呼ぶ |
| `repo list` | `listRepositories` | なし |
| `repo view <repository>` | `getRepository` | なし |
| `repo clone <repository> [<ディレクトリ>]` | `getRepository`（`git_url` を得る） | `git clone`。clone の git の設定に `logiforge.repository` を書く |

- repository の作成は、利用者が WebUI で行う（[役割](../../docs/roles/README.md)）。CLI には置かない。

## 木

| コマンド | API |
|---|---|
| `tree view [--commit]` | `getTree` |
| `tree nodes [--root] [--depth] [--commit]` | `listNodes` |
| `tree completion` | `getCompletion` |
| `tree export --format json\|markdown [--commit]` | `exportTree` |
| `node view <ノード> [--commit]` | `getNode` |
| `node history <ノード>` | `getNodeHistory` |
| `verdict view <検証結果>` | `getVerdict` |
| `artifact get <sha256> [--commit] [-o <ファイル>]` | `getArtifact` |

- ひな形と `check` は、[ひな形と check](local.md) に書く。

## apply request

| コマンド | API | git |
|---|---|---|
| `ar create --branch <ブランチ> [--kind change\|repair.revert\|repair.accept] [--description] [--idempotency-key <キー>]` | `createApplyRequest` | なし（先に push しておく） |
| `repair revert` | `getRepository`（`broken`） | `main` から分けたブランチに、`tree/` と `artifacts/` を最後に正しかった時点のものに戻したコミットを作る（[壊れた repository](../backend/broken.md)） |
| `repair accept` | `getRepository`（`broken`） | `main` から分けたブランチに、空のコミットを作る |
| `ar edit <apply request> --description` | `updateApplyRequest` | なし |
| `ar view <apply request>` | `getApplyRequest` | なし |
| `ar list [--state]` | `listApplyRequests` | なし |
| `ar material <apply request>` | `getReviewMaterial` | なし |
| `ar review <apply request> --approve\|--request-changes\|--comment --commit <SHA> [--body] [--prompt-revision]` | `reviewApplyRequest` | なし |
| `ar close <apply request> --reason` | `closeApplyRequest` | なし |
| `ar cleanup` | `listApplyRequests` | 手元のブランチのうち、反映か close された apply request のものだけを、`git branch -d` と `git push --delete` で消す |

- `ar create` は、ブランチが push 済みで、手元の先頭と remote の先頭が同じかを確かめてから API を呼ぶ。`commit` に手元の先頭のコミットを入れて送る。
  - なぜ：push し忘れた変更を、知らないまま取り込まないため。
- ブランチの名前に、`main` は使えない。GitForge が push を拒む。
- apply request を更新するには、同じブランチに `git push` するだけである。コマンドはない。`ar edit` は説明欄だけを書き換える。
- `ar review` の `--commit` には、レビューした先頭のコミットの SHA を渡す。`ar view` で先頭のコミットの一覧を確かめられる（[先頭のコミット](../backend/apply-request.md#先頭のコミット)）。
  - reviewer は、`ar material` が返すコミット（`commit`）を、そのまま `--commit` に渡す。なぜ：材料を読んだあとに新しいコミットが push されると、読んでいないコミットをレビューしたことになるから。そのときは `commit_conflict` で受け付けない。
- `ar cleanup` は、ほかの人のブランチを消さない。手元にあるブランチだけを対象にする。
- `ar create` は、`Idempotency-Key` を、呼び出しごとに乱数で作る。その呼び出しの中で送り直すとき（通信の誤り、502、503、504）だけ、同じキーを使う（[同じ要求の繰り返し](../api/writes.md)）。
  - `--json` の出力に、使ったキー（`idempotency_key`）を書く。あとで送り直すときは、`--idempotency-key` にそのキーを渡す。
  - なぜ呼び出しごとか：レビューの鎖が切れたときの「close して、同じブランチから作り直す」で、閉じた apply request が返らないようにするため（[反映の手順](../backend/merge.md)）。同じブランチの二重の作成は、バックエンドが拒む。
- `human-request create` は、`--idempotency-key` で `Idempotency-Key` を渡せる。
- どのコマンドも、`snapshot_pending` が返ったら、1 秒待って、3 回まで送り直す。それでも `snapshot_pending` なら、そのまま返す。
  - なぜ：worker は見回りの間隔（既定 1 秒）ごとに取り込むので、数秒待てば、多くは読む側に出ているから（[書いた直後の読み](../api/writes.md)）。
  - バックエンドは、`snapshot_pending` を、何かを書く前にだけ返す（[書いた直後の読み](../api/writes.md)）。そのため、送り直しても二重にならない。

## 作業と人への依頼

| コマンド | API |
|---|---|
| `work list [--subtree] [--kind] [--include-claimed]` | `listWork` |
| `human-request create --node <ノード> --kind pvr\|decision --body <ファイル>` | `createHumanRequest` |
| `human-request list [--kind] [--state] [--node]` | `listHumanRequests` |
| `human-request view <人への依頼>` | `getHumanRequest` |

- 人への依頼への回答は、利用者が WebUI で行う。CLI には置かない。
