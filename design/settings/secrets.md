# 秘密の暗号化

設定の DB に置く秘密を、どの鍵で暗号化し、どのプロセスが読めるかを確かめるときに読む。
どの設定が秘密かは、[設定](README.md#設定の-db-に置くもの) に書く。

## 2 つの鍵

秘密は、次の 2 つの鍵のどちらかで暗号化する。

| 鍵 | 渡す先 |
|---|---|
| バックエンドの鍵 | バックエンドだけ |
| 共有の鍵 | バックエンドと worker |

- worker には、バックエンドの鍵を渡さない。
- なぜ 2 つに分けるか：worker が、記録の署名の秘密鍵と、GitHub App の秘密鍵を読めないようにするため。
  - worker は、記録の署名を確かめるだけで、記録を作らない（[反映かの判定](../backend/verify/README.md)）。
  - worker が秘密鍵を読めると、worker を乗っ取った者が、反映の記録を偽れる（[記録のコメント](../adapters/records.md#記録のコメント)）。
- 平文の設定（記録の署名の公開鍵、repository の一覧、URL）は、worker が設定の DB に書けないことで守る（[設定の DB](database.md#db-の種類)）。
- なぜ DB の権限で分けないか：
  - SQLite には、表ごとの権限がないから（[設定の DB](database.md)）。
  - 暗号化で分けると、DB の種類によらず、同じ決まりで守れるから。

## 秘密ごとの鍵

| 秘密 | 構成 | 読むプロセス | 鍵 |
|---|---|---|---|
| GitHub App の秘密鍵 | GitHub | バックエンド | バックエンドの鍵 |
| worker のための installation token（[worker のトークン](../adapters/github-app.md#worker-のトークン)） | GitHub | バックエンド（書く）、worker（読む） | 共有の鍵 |
| GitHub App の client secret | GitHub | バックエンド | バックエンドの鍵 |
| Forgejo の worker のユーザーのトークン | Forgejo | バックエンド、worker | 共有の鍵 |
| Forgejo の OAuth2 のアプリケーションの client secret | Forgejo | バックエンド | バックエンドの鍵 |
| 記録の署名の秘密鍵 | 両方 | バックエンド | バックエンドの鍵 |

- 記録の署名の公開鍵は、秘密ではないので、暗号化しない。worker は、これで記録を確かめる。
- GitHub App の秘密鍵は、バックエンドだけが読む。バックエンドは、これで installation token を作る（[installation token](../adapters/github-app.md#installation-token)）。
  - なぜ worker に渡さないか：秘密鍵を持つ者は、`permissions` を絞らずに、書ける installation token を作れるから。App の Contents と Issues は Read and write なので、install したすべてのリポジトリ（ディレクトリ方式では、プロジェクトのコードのリポジトリ）に書ける。
  - worker は、バックエンドが Read に絞って作ったトークンだけを読む（[worker のトークン](../adapters/github-app.md#worker-のトークン)）。
- Forgejo の worker のトークンは、バックエンドと worker の両方が読む。worker は git と API を読むのに、バックエンドは作業用の git の複製に fetch するのに使う。
  - worker に渡してよい理由：スコープが `read:repository`、`read:organization`、`read:issue` だけで、書けないから（[Forgejo アダプタ](../adapters/forgejo.md)）。
  - 書けないことは、2 つで守る。
    - バックエンドは、置く前に、そのトークンで書けないことを確かめる（[Forgejo の worker のトークンを確かめる](../adapters/forgejo.md#worker-のトークンを確かめる)）。書ければ、`validation_failed` を返し、置かない。
    - worker のユーザーは、organization で、読むだけの権限の team にだけ入れる。なぜ：スコープの確かめが漏れても、ユーザーの権限で書けないようにするため。
- そのため、worker が読める秘密では、GitForge に書けない。
- client secret は、サインインの中継と、トークンが Logiforge のものかの確かめに使う。worker は使わない（[サインインの中継](../webui/oauth-relay.md)）。

## 暗号の形

- 鍵は、32 バイトの乱数を base64 にしたものである。
- 暗号は、AES-256-GCM にする。nonce は、書くたびに乱数で作る。
- 追加の認証データ（AAD）に、設定の名前を入れる。
  - なぜ：ある設定の暗号文を、別の設定の行に写しても、読めないようにするため。
- 暗号文の頭に、鍵の ID を付ける。
  - なぜ：鍵を取り替えるときに、古い鍵で書いた行を見分けるため。

## 秘密を差し替える

利用者は、設定の画面から、次の秘密を差し替えられる（[設定の画面](../../docs/webui/settings.md)、[設定の API](../api/endpoints/settings.md) の `updateGitForgeSettings`）。

- GitHub App の client secret
- GitHub App の秘密鍵（`.pem`）
- Forgejo の OAuth2 のアプリケーションの client secret
- Forgejo の worker のユーザーのトークン

手順（GitHub App の例）：

1. 利用者が、GitHub の App の設定の画面で、新しい client secret か秘密鍵を作る。
   - App の設定の画面を開くには、App を作った owner の admin の権限が要る（organization なら organization の owner）。
2. 利用者が、それを設定の画面に入れる。
   - バックエンドは、置く前に、新しい値で GitHub を呼べることを確かめる。秘密鍵なら JWT で `GET /app` を、client secret なら `POST /applications/{client_id}/token` を呼ぶ。呼べなければ `validation_failed` を返し、置かない。
3. 利用者が、GitHub で、古いものを消す。
   - なぜ消すのを最後にするか：新しい値が効く前に古いものを消すと、そのあいだ、サインインと worker の読みが止まるから。

読む側：

- バックエンドは、次の要求から、新しい値を使う。要求ごとに設定の DB を読むから（[設定を読むとき](README.md#設定を読むとき)）。
- worker は、次に repository の一覧を読み直したときから、新しい値を使う。
- 秘密鍵を差し替えたら、バックエンドは、次に worker のトークンを作り直すときから、新しい鍵を使う。作ってあるトークンは、切れるまで使える。
- どちらも、起動し直さない。

画面と API は、秘密を読み返さない。秘密ごとに、置いてあるかと、最後に変えた利用者と時刻だけを返す。

- なぜ：画面やログに出ると、漏れる場所が増えるから。

## 鍵を取り替える

- `logiforge settings rekey` で、新しい鍵で、すべての秘密を暗号化し直す。
  - WebUI のサインインのセッションは、暗号化し直さずに消す。利用者は、1 度だけサインインし直す（[サインインのセッション](../webui/session.md#消すとき)）。
  - 古い鍵を `--old-backend-key-file` と `--old-shared-key-file` で、新しい鍵を `LOGIFORGE_BACKEND_KEY_FILE` と `LOGIFORGE_SHARED_KEY_FILE` で渡す（[環境変数と秘密](../deploy/env.md)）。
  - 動かすあいだ、バックエンドと worker を止める。
- 鍵が漏れたときは、鍵を取り替えたうえで、その鍵で守っていた秘密も作り直す。
  - 例：GitHub App の秘密鍵と client secret を、GitHub で作り直し、設定の画面で入れ直す（[設定の画面](../../docs/webui/settings.md)）。

## 鍵を置く場所

- 鍵は、DB と別の場所に置く。ファイルで渡す（`_FILE`。[環境変数と秘密](../deploy/env.md)）。
  - なぜ：DB のバックアップが漏れても、秘密を読めないようにするため。
- 鍵を置く場所（バックエンドと worker を動かす環境）を読める人を信頼する。
  - ほかの主体には、鍵を置く場所を読ませない。
- worker は、git と GitForge に書く鍵を持たない。記録を作る秘密鍵も持たない。
