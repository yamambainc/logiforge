# コマンドの一覧

CLI のコマンドと、そのコマンドが呼ぶ API と git の操作を引くときに使う。
API の詳細は、[openapi.yaml](../api/openapi.yaml) が正本である。

## repository と認証

| コマンド | API | git |
|---|---|---|
| `auth login --token <トークン>` | なし | なし |
| `auth setup-git` | なし | credential helper を設定する |
| `repo list` | `listRepositories` | なし |
| `repo view <repository>` | `getRepository` | なし |
| `repo clone <repository>` | `getRepository`（`git_url` を得る） | `git clone` |

- repository の作成は、利用者が WebUI で行う（[役割](../../docs/roles/README.md)）。CLI には置かない。

## 木

| コマンド | API |
|---|---|
| `tree view [--revision]` | `getTree` |
| `tree nodes [--root] [--depth] [--revision]` | `listNodes` |
| `tree completion` | `getCompletion` |
| `tree export --format json\|markdown [--revision]` | `exportTree` |
| `node view <ノード> [--revision]` | `getNode` |
| `node history <ノード>` | `getNodeHistory` |
| `verdict view <検証結果>` | `getVerdict` |
| `artifact get <sha256> [--revision] [-o <ファイル>]` | `getArtifact` |

- ひな形と `check` は、[ひな形と check](local.md) に書く。

## apply request

| コマンド | API | git |
|---|---|---|
| `ar create --branch <ブランチ> [--description]` | `createApplyRequest` | なし（先に push しておく） |
| `ar update <apply request> --base-version <n> [--branch]` | `updateApplyRequest` | なし（先に push しておく） |
| `ar view <apply request>` | `getApplyRequest` | なし |
| `ar list [--state]` | `listApplyRequests` | なし |
| `ar material <apply request>` | `getReviewMaterial` | なし |
| `ar review <apply request> --approve\|--request-changes\|--comment --version <n> [--body] [--prompt-revision]` | `reviewApplyRequest` | なし |
| `ar close <apply request> --reason` | `closeApplyRequest` | なし |
| `ar cleanup` | `listApplyRequests` | 反映か close された apply request のブランチを、`git push --delete` で消す |

- `ar create` と `ar update` は、ブランチが push 済みで、手元の先頭と remote の先頭が同じかを確かめてから API を呼ぶ。`--revision` に手元の先頭を入れて送る。
  - なぜ：push し忘れた変更を、知らないまま取り込まないため。
- `ar create` は、`Idempotency-Key` を自動で付ける。送り直すときは、同じキーを使う（[同じ要求の繰り返し](../api/writes.md)）。

## 作業と人への依頼

| コマンド | API |
|---|---|
| `work list [--subtree] [--kind] [--include-claimed]` | `listWork` |
| `human-request create --node <ノード> --kind pvr\|decision --body <ファイル>` | `createHumanRequest` |
| `human-request list [--kind] [--state] [--node]` | `listHumanRequests` |
| `human-request view <人への依頼>` | `getHumanRequest` |

- 人への依頼への回答は、利用者が WebUI で行う。CLI には置かない。
