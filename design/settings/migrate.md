# v0.3.0 からの移行

環境変数で設定を渡していた Logiforge（v0.3.0）を、設定の DB を使う版に上げるときに読む。

## 移すもの

- 記録の署名の鍵は、必ず移す。
  - なぜ：それまでの記録のコメントは、その鍵で署名してあるから。移さないと、worker がそれまでの反映を確かめられない（[反映かの判定](../backend/verify/README.md)）。
- ほかの設定は、移さなくてもよい。初回のセットアップで入れ直せる。

## 移し方

1. 新しい版のバイナリで、`logiforge settings import-env` を動かす。
   - v0.3.0 の環境変数を読み、設定の DB に書く。
   - 移すのは、構成、repository の一覧、`user` のアカウント名と team、Forgejo の URL と organization、worker のユーザーとトークン、OAuth2 のアプリケーション、記録の署名の鍵、見回りの間隔である。
   - 秘密は、そのとき渡した 2 つの鍵で暗号化する。
2. バックエンドと worker を、DB の場所と 2 つの鍵だけを渡して、起動する。
3. セットアップのコードで、最初の管理者を決める（[初回のセットアップ](setup.md)）。
   - 1 で GitForge の設定を移していれば、構成を選ぶ段と、設定を入れる段は飛ばす。

## GitHub を使う構成

- OAuth App と、読むだけの GitHub App は、移さない。
- 初回のセットアップで、manifest flow で新しい GitHub App を作り、install する（[初回のセットアップ](setup.md#github-を使う構成app-を-manifest-flow-で作る)）。
  - なぜ：読むだけの App に、サインインと書き込みの権限を足すより、manifest で作り直すほうが、権限の誤りがないから。
- 新しい App を install して、worker が読めることを確かめたら、古い OAuth App と読むだけの App を消す。
- 利用者は、新しい App で、サインインし直す。
- `gh auth login` のトークン（CLI）は、そのまま使える。

## 古い環境変数

- 設定の DB を使う版は、v0.3.0 の設定の環境変数（`LOGIFORGE_GITHUB_*`、`LOGIFORGE_OAUTH_*`、`LOGIFORGE_FORGEJO_*`、`LOGIFORGE_RECORD_KEYS` など）を読まない。
- そのどれかが渡されていたら、起動しない。移し方を示すメッセージを出す。
  - なぜ：環境変数の値が効いていると思ったまま、DB の値で動くことを防ぐため。
- この版は、v0.3.0 と互換がない。リリースノートに、この手順を書く。
