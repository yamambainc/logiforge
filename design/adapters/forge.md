# 独自フォージのアダプタ

独自フォージを使う構成で、[やりとり](../backend/ports/interaction.md) と [認証](../backend/ports/authentication.md) のポートをどう実装するかを確かめるときに読む。
独自フォージそのものは、[独自フォージ](../forge/README.md) に書く。
[worker](../backend/worker.md) は、[git アダプタ](git.md) で、独自フォージの git リポジトリを読む。

## 形

- アダプタは、独自フォージの API の HTTP クライアントである。
  - API の正本は、独自フォージの [openapi.yaml](../forge/openapi.yaml) である。
- アダプタは、独自フォージの `writer` のトークンを使う（[主体](../forge/README.md)）。
  - トークンは、サーバの設定に置く（[設定と秘密情報](README.md)）。
  - worker は、`reader` のトークンで git を読む。

## やりとり

| やりとりの操作 | 独自フォージの API |
|---|---|
| repository を列挙する | `listRepositories`。provider_ref は名前そのもの |
| 名前から provider_ref を求める | API を呼ばない。名前を返す |
| repository を用意する | `putRepository`。git リポジトリを作り、最初のコミットを置く |
| 提案を作る | `createApplyRequest` |
| 説明欄を書き換える | `updateApplyRequestDescription` |
| 反映する | `mergeApplyRequest` |
| 反映の失敗を記録する | `addMergeFailure` |
| close する | `closeApplyRequest` |
| レビューを記録する | `addReview` |
| 人への依頼を記録する | `createHumanRequest` |
| 回答を記録する | `answerHumanRequest`（添付を含む） |
| 提案、レビュー、人への依頼を読む | `listApplyRequests`、`getApplyRequest`、`listHumanRequests`、`getHumanRequest`、`getAnswerAttachment` |

送り直しの扱い：

- 次のエラーは、送り直しで、最初の要求がすでに済んでいるときにも起きる。
- アダプタは、読み直して、次の項目が送ったものと同じなら成功とする。違えば失敗として返す。

| エラー | 比べる項目 |
|---|---|
| `already_exists` | 送った項目のすべて |
| `not_open`（close のとき） | close したセッションと理由 |
| `already_answered` | 回答の本文と、回答した利用者 |

`mergeApplyRequest` の失敗は、次のように扱う。

| エラー | バックエンドの扱い |
|---|---|
| `head_moved` | `version_conflict`（[衝突](../backend/merge.md)） |
| `conflict` | 反映の失敗の理由 `conflict` を記録する |
| `not_open` | 今の状態を返す |

- なぜ：通信が切れて送り直したときに、済んでいる要求を失敗として扱わないため。

## 認証

| 認証の操作 | 独自フォージの API |
|---|---|
| 主体を確かめる | `verifyToken`。`user` と `client` だけを主体として返し、`writer` は正しくないトークンとして扱う |
| repository を読めるか確かめる | API を呼ばない。すべての主体が、すべての repository を読める |

- `writer` を主体として返さない。
  - なぜ：`writer` はバックエンドのための主体で、Logiforge の API の役割（[認可](../backend/README.md)）を持たないから。
- 確かめた結果は、キャッシュしない。
  - なぜ：独自フォージはバックエンドの近くに置き、レート制限もないから。トークンを消したことが、すぐに効く。
