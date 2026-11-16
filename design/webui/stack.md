# 技術と配り方

WebUI を作り始めるときと、イメージのビルドを変えるときに読む。

## フレームワーク

- React と TypeScript で、1 ページのアプリケーション（SPA）として作る。
  - なぜ React か：ビュワーの行の仮想化、キーボードの操作、詳細のパネルの読み込みを、部品に分けて書け、書ける人と資料が最も多いから。
  - なぜ SPA か：画面の中身はすべて BE の API の応答から作るので、サーバ側で HTML を作る必要がないから。
- 使うライブラリは、`react` と `react-dom` だけにする。例外は、3D の表示の `three`（three.js）である（[3D の描き方](viewer-3d.md#描き方)）。
  - `three` は、3D を開いたときだけ読み込む。アウトラインと図の読み込みは増えない。
  - 行の仮想化、図のレイアウトとズーム、URL の状態、API の呼び出しは、自前で書く。行の仮想化は約 30 行、図は描画を含めて約 350 行である（[試作](first-release.md#試作)、[図の描き方](graph.md#描き方)）。
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
- API の JSON の応答は、要求が `Accept-Encoding: gzip` を持てば、gzip で返す。
  - なぜ：ビュワーは、`listNodes` でスナップショットツリーのすべてのノードを読むから。ノード 4,000 個の試作のサンプルで、応答は約 1.5 MB、gzip で約 0.1 MB だった（[ノードの読み方](snapshot-tree-viewer.md#ノードの読み方)）。
