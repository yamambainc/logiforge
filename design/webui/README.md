# WebUI の設計

WebUI を実装するときと、WebUI のために BE の API を変えるときに読む。
WebUI の要件（画面と、利用者ができること）は、[docs/webui](../../docs/webui/README.md) が正本である。この設計は要件に従い、要件の内容は写さずにリンクで指す。

## 読む順

1. [技術と配り方](stack.md)：フレームワーク、ビルド、BE と同じコンテナからの配り方
2. [認証](auth.md)：GitForge のトークンの受け取り方と持ち方
3. [画面と API](screens.md)：5 つの画面が、どの API を使うか。WebUI のために足す API
4. [スナップショットツリーのビュワー](snapshot-tree-viewer.md)：ノード 4,000 個でも使える作りと、表示の決まり
5. [スナップショットツリーの図](graph.md)：ノードと線で描く表示。ズームとパン
6. [モバイルの表示](mobile.md)：幅 767 px 以下のレイアウトと、タッチ操作の決まり
7. [差分の重ね表示と過去の時点](overlay.md)：apply request を反映したあとの姿と、過去の `main` の姿
8. [最初に作る範囲](first-release.md)：最初の実装に入れるものと、後に回すもの。試作

## WebUI の決まり

- WebUI は、BE の API だけを使うクライアントである（[クライアントの要件との対応](../api/clients.md)）。
  - GitForge の API と、静的ファイルストレージを、直接は呼ばない。
  - なぜ：構成（Forgejo か GitHub か）の違いを、BE のアダプタだけに閉じ込めるため（[レイヤー構成](../architecture.md)）。
- WebUI は、スナップショットツリーを変えない。例外は repository の作成である（[WebUI](../../docs/webui/README.md)）。
- WebUI は、状態をサーバに持たない。表示の状態（開いたノード、検索の語）は、ブラウザの中と URL にだけ持つ。
  - なぜ：BE が状態を持たない決まりを崩さないため（[状態を持たない](../backend/README.md)）。

## 対応しないこと

- 多言語。表示は日本語だけである。
- ダークテーマ。
