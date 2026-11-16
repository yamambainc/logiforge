# 技術と配り方

WebUI を作り始めるときと、イメージのビルドを変えるときに読む。

## フレームワーク

- React と TypeScript で、1 ページのアプリケーション（SPA）として作る。
  - なぜ React か：ビュワーの行の仮想化、キーボードの操作、詳細のパネルの読み込みを、部品に分けて書け、書ける人と資料が最も多いから。
  - なぜ SPA か：画面の中身はすべて BE の API の応答から作るので、サーバ側で HTML を作る必要がないから。
- 使うライブラリは、`react` と `react-dom` だけにする。
  - 行の仮想化、URL の状態、API の呼び出しは、自前で書く。どれも数十行で済む（[試作](first-release.md#試作)）。
  - なぜ：依存を増やすと、上げる作業と脆弱性の確認が増えるから。
- 見た目は、CSS の変数を使った 1 つのスタイルシートで書く。CSS のフレームワークは使わない。
  - 色は、値（真・偽・未決）と状態（要再検討、審査中の apply request、open の人への依頼、inactive）にだけ使う（[色と印](snapshot-tree-viewer.md#色と印)）。

## ビルド

- ビルドには Vite を使う。出力は、`index.html` と、ハッシュ付きの JS と CSS だけである。
- ソースは、リポジトリの `webui/` に置く。Node.js の LTS 版でビルドする。
- 型の検査（`tsc --noEmit`）とビルドを、CI で動かす。

## 配り方

- ビルドした静的ファイルを、Go の `embed` で `logiforge` のバイナリに入れる。`logiforge serve` が、API と同じポートで配る。
  - `/v1/` と `/healthz` のほかのパスは、`index.html` か静的ファイルを返す。
  - なぜ同じバイナリか：イメージを増やさずに済むから（[イメージ](../deploy.md#イメージ)）。
  - なぜ同じオリジンか：CORS の設定が要らず、WebUI は API を相対パスで呼べるから。
- Dockerfile に、Node.js で WebUI をビルドする段を足す。Go の段は、その出力を `embed` する。
  - Node.js の base のイメージも、digest で固定する（[イメージ](../deploy.md#イメージ)）。
- キャッシュの決まりは、次のとおり。
  - ハッシュ付きの JS と CSS は、`Cache-Control: public, max-age=31536000, immutable` を返す。
  - `index.html` は、`Cache-Control: no-cache` を返す。
  - なぜ：新しいイメージに替えたとき、利用者が古い画面を使い続けないようにするため。
- `commit` を指定した `listNodes` と `getNode` の応答には、応答の本文のハッシュを `ETag` として付け、`Cache-Control: private, no-cache` を返す。`If-None-Match` が合えば `304` を返す。
  - なぜ：過去の時点と、重ね表示の読み直し（[差分の重ね表示](overlay.md#差分の重ね表示)）で、同じコミットを何度も読むから。2 回目からは本文を送らずに済む。
  - なぜ `immutable` にしないか：コミットを指定しても、ノードの審査中の apply request と open の人への依頼は、今のデータだから（[コミット](../api/README.md#コミット)）。
- API の JSON の応答は、要求が `Accept-Encoding: gzip` を持てば、gzip で返す。
  - なぜ：ビュワーは、`listNodes` でスナップショットツリーのすべてのノードを読むから。ノード 4,000 個の試作のサンプルで、応答は約 1.5 MB、gzip で約 0.1 MB だった（[ノードの読み方](snapshot-tree-viewer.md#ノードの読み方)）。
