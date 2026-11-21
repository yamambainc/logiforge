# 木のノードを読む

利用者が木のビューアで木を表示するときに WebUI から、opener が分解や検証の前に木を読むときに CLI から呼ぶ。
過去の時点の木と、apply request を反映したあとのスナップショットツリーを見るときにも使う。

## 呼び出し

- `GET /repositories/{repository}/tree/nodes`
- operationId：`listNodes`

## すること

- 木の全体か、指定した部分木のノードを返す。
- 各ノードの値と、葉・要再検討・inactive の状態を返す。
- 各ノードを対象にした open の apply request と人への依頼を返す。
- コミットを指定すると、その時点の木を返す。
- open の apply request を指定すると、それを反映したあとの姿を返す（[反映したあとのスナップショットツリー](../../README.md#反映したあとのスナップショットツリー)）。
- 深さを指定すると、その段までを返す。
- 最新のコミットが valid でなければ、`valid_commit` の木を返す（[木を読む API](../../../backend/invalid-main.md#木を読む-api)）。

## 関係する要件

- [仮説とストーリーの木](../../../../docs/tree/README.md)
- [真偽値の伝播](../../../../docs/tree/propagation.md)
- [木のビューア](../../../../docs/webui/tree-viewer.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `listNodes` に書く。
