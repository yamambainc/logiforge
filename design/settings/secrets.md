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
- なぜ 2 つに分けるか：worker が、記録の署名の秘密鍵を読めないようにするため。
  - worker は、記録の署名を確かめるだけで、記録を作らない（[反映かの判定](../backend/verify/README.md)）。
  - worker が秘密鍵を読めると、worker を乗っ取った者が、反映の記録を偽れる（[記録のコメント](../adapters/records.md#記録のコメント)）。
- なぜ DB の権限で分けないか：
  - SQLite には、表ごとの権限がないから（[設定の DB](database.md)）。
  - 暗号化で分けると、DB の種類によらず、同じ決まりで守れるから。

## 秘密ごとの鍵

| 秘密 | 構成 | 読むプロセス | 鍵 |
|---|---|---|---|
| GitHub App の秘密鍵 | GitHub | バックエンド、worker | 共有の鍵 |
| GitHub App の client secret | GitHub | バックエンド | バックエンドの鍵 |
| Forgejo の worker のユーザーのトークン | Forgejo | バックエンド、worker | 共有の鍵 |
| Forgejo の OAuth2 のアプリケーションの client secret | Forgejo | バックエンド | バックエンドの鍵 |
| 記録の署名の秘密鍵 | 両方 | バックエンド | バックエンドの鍵 |

- 記録の署名の公開鍵は、秘密ではないので、暗号化しない。worker は、これで記録を確かめる。
- GitHub App の秘密鍵は、worker が installation token を作るのに使う。バックエンドは、作業用の git の複製に fetch するのに使う（[GitHub アダプタ](../adapters/github-app.md#github-app)）。
- Forgejo の worker のトークンも、同じ 2 つのことに使う。
- client secret は、サインインの中継と、トークンが Logiforge のものかの確かめに使う。worker は使わない（[サインインの中継](../webui/oauth-relay.md)）。

## 暗号の形

- 鍵は、32 バイトの乱数を base64 にしたものである。
- 暗号は、AES-256-GCM にする。nonce は、書くたびに乱数で作る。
- 追加の認証データ（AAD）に、設定の名前を入れる。
  - なぜ：ある設定の暗号文を、別の設定の行に写しても、読めないようにするため。
- 暗号文の頭に、鍵の ID を付ける。
  - なぜ：鍵を取り替えるときに、古い鍵で書いた行を見分けるため。

## 鍵を取り替える

- `logiforge settings rekey` で、新しい鍵で、すべての秘密を暗号化し直す。
  - 古い鍵を `--old-backend-key-file` と `--old-shared-key-file` で、新しい鍵を `LOGIFORGE_BACKEND_KEY_FILE` と `LOGIFORGE_SHARED_KEY_FILE` で渡す（[環境変数と秘密](../deploy/env.md)）。
  - 動かすあいだ、バックエンドと worker を止める。
- 鍵が漏れたときは、鍵を取り替えたうえで、その鍵で守っていた秘密も作り直す。
  - 例：GitHub App の秘密鍵と client secret を、GitHub で作り直し、設定の画面で入れ直す（[設定の画面](../../docs/webui/settings.md)）。

## 鍵を置く場所

- 鍵は、DB と別の場所に置く。ファイルで渡す（`_FILE`。[環境変数と秘密](../deploy/env.md)）。
  - なぜ：DB のバックアップが漏れても、秘密を読めないようにするため。
- 鍵を置く場所（バックエンドと worker を動かす環境）の管理者を信頼する。
  - ほかの主体には、鍵を置く場所を読ませない。
- worker は、git と GitForge に書く鍵を持たない。記録を作る秘密鍵も持たない。
