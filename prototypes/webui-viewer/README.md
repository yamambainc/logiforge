# スナップショットツリーのビュワーの試作

WebUI の設計（[design/webui](../../design/webui/README.md)）の、ビュワーの見た目と、ノード 4,000 個での動きを確かめるための試作である。アウトラインと図、デスクトップとモバイル（幅 767px 以下）の表示を持つ。`main` には入れない。

## 動かし方

```sh
npm ci
npm run data      # public/data/{100,4000}/ にサンプルを作る
npm run dev       # http://localhost:5173/?n=4000
```

URL の引数で、開く状態を決められる。

| 引数 | 意味 |
|---|---|
| `n` | ノード数（`100` か `4000`） |
| `node` | 選ぶノードの識別子 |
| `q` | 検索の語 |
| `f` | 絞り込み（`true,false,null,nr,ar,hr,changed` をカンマで） |
| `ar` | 重ねる apply request（`apr_7k2m9q4x` は main 向き、`apr_3t8v1n6c` は main 向きでない（`apr_7k2m9q4x` のブランチに積み上げ）、`apr_9d4h2w7e` は衝突） |
| `open` | 開いておく段数 |
| `view` | `graph` で図、`3d` で 3D、なければアウトライン |
| `orient` | `v` で図を縦（上から下）にする。なければ横 |
| `sheet` | `1` で、モバイルの詳細のボトムシートを開いておく |
| `fsheet` | `1` で、モバイルの絞り込みのシートを開いておく |

## スクリーンショット

`screenshots/` にある。撮り直すときは、`npm run build && npx vite preview` のあとで `scripts/shots.mjs` を動かす（引数は、puppeteer のパス、URL、出力先。Chrome のパスは環境変数 `CHROME`）。

## 3D の計測と GIF

- `scripts/fps3d.mjs`：ノード 4,000 個で、3 段まで開いたときと、すべて開いたときのフレームレートを、デスクトップと、CPU を 4 倍遅くしたモバイルで測る。
- `scripts/gif3d.mjs`：3D を 1 周回す様子を GIF にする。
- headless Chrome の WebGL は SwiftShader（CPU で描くソフトウェアの描画）である。GPU のある端末の数字ではない。
