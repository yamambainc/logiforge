# 独自フォージのアダプタ

独自フォージを使う構成で、[やりとり](../backend/ports/interaction.md) と [認証](../backend/ports/authentication.md) のポートをどう実装するかを確かめるときに読む。
独自フォージそのものは、[独自フォージ](../forge/README.md) に書く。
木の読み書きは、[git アダプタ](git.md) が、独自フォージの git リポジトリをリモートとして扱う。

## 形

- アダプタは、独自フォージの API の HTTP クライアントである。
  - API の正本は、独自フォージの [openapi.yaml](../forge/openapi.yaml) である。
- アダプタは、独自フォージの `writer` のトークンを使う（[主体](../forge/README.md)）。
  - git アダプタも、同じトークンで push する。
  - トークンは、サーバの設定に置く（[設定と秘密情報](README.md)）。

## やりとり

| やりとりの操作 | 独自フォージの API |
|---|---|
| repository を列挙する | `listRepositories`。provider_ref は名前そのもの |
| 名前から provider_ref を求める | API を呼ばない。名前を返す |
| repository の、やりとりの側を用意する | `putRepository`。git リポジトリもできる |
| 提案を作る | `createApplyRequest` |
| 版を足す | `addVersion` |
| 説明欄を書き換える | `updateApplyRequestDescription` |
| 提案と版を読む | `getApplyRequest`、`listApplyRequests` |
| 反映の失敗を記録する | `addMergeFailure` |
| 提案を close する | `closeApplyRequest` |
| 反映されたことを記録する | `markMerged` |
| レビューを記録する・読む | `addReview`、`getApplyRequest` |
| 人への依頼を記録する・読む | `createHumanRequest`、`getHumanRequest`、`listHumanRequests` |
| 回答を記録する | `answerHumanRequest` |

送り直しの扱い：

- 次のエラーは、送り直しで、最初の要求がすでに済んでいるときにも起きる。
- アダプタは、読み直して、次の項目が送ったものと同じなら成功とする。違えば失敗として返す。

| エラー | 比べる項目 |
|---|---|
| `already_exists` | 送った項目のすべて |
| `not_open`（close のとき） | close したセッションと理由 |
| `already_answered` | 回答の本文と、回答した利用者 |

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
