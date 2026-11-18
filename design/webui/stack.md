# 技術と配り方

WebUI を作り始めるときと、イメージのビルドを変えるときに読む。

## フレームワーク

- React と TypeScript で、1 ページのアプリケーション（SPA）として作る。
  - なぜ React か：ビュワーの行の仮想化、キーボードの操作、詳細のパネルの読み込みを、部品に分けて書け、書ける人と資料が最も多いから。
  - なぜ SPA か：画面の中身はすべて BE の API の応答から作るので、サーバ側で HTML を作る必要がないから。
- 使うライブラリは、`react` と `react-dom` だけにする。例外は、次の 2 つである。
  - 3D の表示の `three`（three.js）（[3D の描き方](viewer-3d.md#描き方)）
  - 文章の表示の `react-markdown` と `remark-gfm`、図の `mermaid`（[Markdown と Mermaid の表示](markdown.md)）
  - どれも、使うときだけ読み込む。アウトラインと図の読み込みは増えない。
  - 版は固定する。上げるのは、その部分に手を入れるときだけにする。なぜ：`three` と `mermaid` は新しい版がよく出て、版の間で API が変わることがあるから。
  - 行の仮想化、図のレイアウトとズーム、URL の状態、API の呼び出しは、自前で書く。行の仮想化は約 30 行で書ける。図は [図の描き方](graph.md#描き方) に書く。
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
  - なぜ同じバイナリか：イメージを増やさずに済むから（[イメージ](../deploy/README.md#イメージ)）。
  - なぜ同じオリジンか：CORS の設定が要らず、WebUI は API を相対パスで呼べるから。
- Dockerfile には、Node.js で WebUI をビルドする段を置く。Go の段は、その出力を `embed` する。
  - Node.js の base のイメージも、digest で固定する（[イメージ](../deploy/README.md#イメージ)）。
- キャッシュの決まりは、次のとおり。
  - ハッシュ付きの JS と CSS は、`Cache-Control: public, max-age=31536000, immutable` を返す。
  - `index.html` は、`Cache-Control: no-cache` を返す。
  - なぜ：新しいイメージに替えたとき、利用者が古い画面を使い続けないようにするため。
- API の JSON の応答は、要求が `Accept-Encoding: gzip` を持てば、gzip で返す。
  - なぜ：ビュワーは、`listNodes` でスナップショットツリーのすべてのノードを読むから（[ノードの読み方](snapshot-tree-viewer.md#ノードの読み方)）。
  - ノード 4,000 個の応答は約 1.5 MB で、gzip で約 0.1 MB になる。
