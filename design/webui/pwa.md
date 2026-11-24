# PWA

WebUI を、ホーム画面に追加して単独のアプリとして開けるようにするところと、Service Worker を変えるときに読む。
要件は [WebUI](../../docs/webui/README.md#ホーム画面に追加して使う) に書く。

## ホーム画面に追加する

- `index.html` から、`/manifest.webmanifest` を指す。中身は次のとおりである。
  - `name` と `short_name` は `Logiforge`、`start_url` と `scope` と `id` は `/`、`display` は `standalone` である。
  - `theme_color` は上の帯と同じ白（`#ffffff`）、`background_color` は画面の地の色（`#f6f7f9`）である。
  - アイコンは、192 px と 512 px の PNG、マスクできる 512 px の PNG、SVG である。ファイルは `webui/public/` に置く。
- iOS の Safari のために、`index.html` に次のものを置く。
  - `apple-touch-icon`（180 px の PNG）。なぜ：iOS は、ホーム画面のアイコンに manifest のアイコンを使わないから。
  - `apple-mobile-web-app-capable` と `apple-mobile-web-app-title`。なぜ：manifest の `display` を読まない古い iOS でも、単独のアプリとして開くため。
  - `apple-mobile-web-app-status-bar-style` は `default` にする。
  - なぜ：`black-translucent` にすると、画面が状態の帯の下に入り、上の帯に `safe-area-inset-top` の余白が要るから。下の余白は、すでに足している（[モバイルの表示](mobile.md#レイアウトの切り替え)）。
- 「ホーム画面に追加」を促すボタンは作らない。ブラウザのメニューから追加する。

## Service Worker

Service Worker が持つのは、アプリの外枠（`index.html`、それが読む JS と CSS、アイコン、manifest）だけである。

| 要求 | 扱い |
|---|---|
| `/v1/` と `/healthz`（サインインの中継 `/v1/oauth/` を含む） | 触れない。ブラウザがそのまま BE に送る |
| GET でない要求、ほかのオリジン（GitForge の authorize など） | 触れない |
| 画面の遷移（`/oauth/callback` を含む） | ネットを先に使う。つながらないときだけ、控えの `index.html` を返す |
| ページの中の文書（Mermaid の iframe の `mermaid.html`） | 触れない |
| `/assets/` のハッシュ付きの JS と CSS | 控えを先に使う。なければネットから読み、控えに足す |
| アイコンと manifest | ネットを先に使う。つながらないときだけ控え |
| `sw.js` とほかのもの | 触れない |

- 振り分けは `webui/src/sw-route.ts`、Service Worker は `webui/src/sw.ts` である。ビルドで `dist/sw.js` になる。依存は足さない（[フレームワーク](stack.md#フレームワーク)）。
- API の応答を控えない。
  - なぜ：アクセストークンはメモリだけに持つ決まりだから（[サインイン](auth.md)）。API の応答は、サインインした人にだけ見せるデータである。端末に残すと、サインアウトしても、共用の端末で次の人が読める。
  - なぜ `/v1/oauth/` に触れないか：セッションの cookie（HttpOnly）とアクセストークンの受け渡しを、今までと同じにするため（[サインインのセッション](session.md)）。
- Service Worker は、`/sw.js` に置き、スコープを `/` にする。ビルドした WebUI でだけ入れる（開発の `vite` では入れない）。

## 更新

新しいイメージに替えたとき、古い外枠が残らないようにする。

- 画面の遷移は、ネットを先に使う。そのため、つながっていれば、開くたび・リロードのたびに、新しい `index.html` を使う。控えの `index.html` を使うのは、つながらないときだけである。
- ビルドの最後に、`sw.js` に、版と、控えに入れる外枠の一覧を書き込む（`webui/vite.config.ts` の `pwaShell`）。
  - 版は、外枠の中身から作る。外枠のどれかが変われば `sw.js` が変わる。
  - 一覧は、`index.html` と、それが読む JS と CSS（`dist/index.html` から読む）と、アイコンと manifest である。使うときだけ読む部分（3D、Mermaid）は入れない。読んだときに控えに足す。
- ブラウザは、画面を開くたびに `sw.js` を読み直し、変わっていれば、新しい Service Worker を入れる。
  - `sw.js` は `Cache-Control: no-cache` で返し、登録では `updateViaCache: 'none'` にする。なぜ：HTTP のキャッシュから古い `sw.js` を読まないため。
- 新しい Service Worker は、入れたらすぐ動かす（`skipWaiting` と `clients.claim`）。動き始めたら、ほかの版の控えを消す。
  - なぜすぐ動かしてよいか：画面の遷移はネットを先に使うので、開いている画面の外枠を、Service Worker が差し替えることはないから。
- 開いたままの画面は、リロードするまで、古い版のままである。古い版の、使うときだけ読む部分は、BE にないので読めない（404）。これは、Service Worker がないときと同じである（[配り方](stack.md#配り方)）。
- BE の配り方（`index.html` と、ハッシュの付かないファイルは `no-cache`、`assets/` は `immutable`）は変えない。`ETag` は付けない。

## サインイン

- サインインの流れ（[サインインの流れ](signin-flow.md)）、`redirect_uri`（`https://<WebUI のホスト>/oauth/callback`）、CSP は変えない。
  - GitForge の authorize は、ほかのオリジンなので、Service Worker は触れない。`/oauth/callback` は、ふつうの画面の遷移として、ネットから読む。
- authorize には、`location.assign` で、同じ窓のまま行く。`window.open` や `target="_blank"` は使わない。
  - なぜ：単独のアプリとして開いているときに、別のブラウザに移らないようにするため。
- 単独のアプリとして開いているときに、スコープ（`/`）の外の GitForge に行くと、ブラウザは、同じ窓の中で、URL の帯をつけて示す。`/oauth/callback` に戻ると、アプリの表示に戻る。
  - Android の Chrome も、iOS の Safari も、同じ窓のままなので、`sessionStorage` の `state` と `code_verifier` が残り、交換できる。
  - もし別のブラウザで戻った場合は、`state` が合わないので、サインインの画面を出す。authorize に行き直さない（[authorize とサインインの画面](signin-flow.md#authorize-とサインインの画面)）。authorize との行き来が繰り返し続くことはない。
- iOS では、ホーム画面から開いたアプリの cookie とストレージは、Safari と別である。
  - そのため、初めてホーム画面から開いたときは、Safari でサインインしていても、アプリの中で GitForge のログインの画面が出る。そのあとは、アプリの中のセッションの cookie で、GitForge に行かずに開ける（[サインインが続く長さ](../../docs/webui/README.md#サインインが続く長さ)）。
- アクセストークンは、今までどおりメモリだけに持つ。アプリを閉じて開き直すと、セッションからもらい直す。

## つながっていないとき

- 端末がつながっていない（`navigator.onLine` が `false`）あいだは、画面の上に「つながっていません」と重ねて示す。
- 開いたときにつながっていなければ、控えの外枠で開き、上の帯の「つながっていません」と、サインインの画面に「つながると、自動でサインインし直します」と示す。サインインのボタンは出さない。
  - つながったら（`online` のイベント）、もう一度セッションからアクセストークンをもらう。GitForge には行かない（セッションが切れていれば行く）。
- `navigator.onLine` が `true` でも、BE が答えないときは、今までどおり「つながりませんでした」と示す。
- データ（スナップショットツリー、人への依頼など）を、つながっていないときに見ることはできない。控えないからである（[Service Worker](#service-worker)）。

## 確かめ方

- ブラウザの E2E（`webui/e2e/run.mjs`）で、次のことを確かめる。
  - manifest とアイコンを読めること
  - Service Worker が外枠を控え、API とサインインの応答を控えないこと。API の要求が Service Worker を通らないこと
  - つながっていないときに開くと「つながっていません」と示し、つながると GitForge に行かずにサインインし直すこと
- iOS の Safari の単独のアプリでのサインインの戻りは、E2E では確かめられない。実機で、次のとおり確かめる。
  1. Safari で WebUI を開き、共有のメニューから「ホーム画面に追加」する。
  2. ホーム画面のアイコンから開く。GitForge のログインの画面が、アプリの中に URL の帯つきで出る。
  3. ログインすると、アプリの表示に戻り、repository の一覧が出る。Safari に移らない。
  4. アプリを閉じて開き直すと、GitForge に行かずに一覧が出る。
  5. 機内モードにして開き直すと、「つながっていません」と出る。機内モードを切ると、一覧が出る。

## 対応しないこと

- つながっていないときに、データを見ること。
- プッシュ通知。
- 開いたままの画面に、新しい版があると知らせること。
  - なぜ：つながっていれば、開き直すかリロードすれば、新しい版になるから。
