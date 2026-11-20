# 設定

バックエンドと worker の設定を、どこに置き、誰が変えるかを確かめるときに読む。

## 読む順

1. このページ：起動時に渡すものと、設定の DB に置くもの
2. [秘密の暗号化](secrets.md)：秘密ごとの鍵と、worker から隠すもの
3. [設定の DB](database.md)：DB の種類と、置く表
4. [初回のセットアップ](setup.md)：だれがセットアップできるか、GitHub App の作成、最初の利用者
5. [利用者](users.md)：利用者の決め方と、設定を変えられる主体

## 起動時に渡すもの

起動時に環境変数で渡すのは、次のものだけである。名前と形は、[環境変数と秘密](../deploy/env.md) に書く。

- 設定の DB の場所
- 秘密を暗号化する鍵（[秘密の暗号化](secrets.md)）
- そのホストに結び付くもの
  - 待ち受けるアドレス
  - 置き場、clone、作業用の git の複製のディレクトリ
  - なぜ環境変数か：DB を読む前に要り、ホストごとに違うから。

ほかの設定は、すべて設定の DB に置く。

- なぜ：repository を足すたびに、サーバを起動し直さずに済むから。利用者が WebUI から変えられる。

## 設定の DB に置くもの

| 設定 | 構成 | 使う | 秘密 |
|---|---|---|---|
| 構成（GitHub を使う / Forgejo を使う） | 両方 | 両方 | |
| GitHub App の ID、client_id、slug、秘密鍵、client secret（[GitHub アダプタ](../adapters/github-app.md#github-app)） | GitHub | バックエンド | 秘密鍵と client secret |
| worker のための installation token と期限（[worker のトークン](../adapters/github-app.md#worker-のトークン)） | GitHub | 両方（バックエンドが書く） | ✓ |
| GitHub の REST と画面の URL | GitHub | 両方 | |
| Forgejo の URL（BE から、ブラウザから）、organization | Forgejo | 両方 | |
| Forgejo の OAuth2 のアプリケーションの client_id と client secret | Forgejo | バックエンド | client secret |
| worker のユーザーとトークン（[Forgejo アダプタ](../adapters/forgejo.md)） | Forgejo | 両方 | トークン |
| GitHub リポジトリの repository の一覧（名前と `owner/name`）（[repository の登録](../adapters/README.md#repository-の登録)） | GitHub | 両方 | |
| ディレクトリの repository の一覧（名前、`owner/name`、パス） | 両方 | 両方 | |
| 記録の署名の秘密鍵（[記録のコメント](../adapters/records.md#記録のコメント)） | 両方 | バックエンド | ✓ |
| 記録の署名の公開鍵 | 両方 | 両方 | |
| WebUI の URL（サインインの `redirect_uri` に使う） | 両方 | バックエンド | |
| 見回りの間隔、repository の一覧を読み直す間隔、記録を読む間隔、git の操作の時間の上限（[見回り](../backend/worker/patrol.md)） | 両方 | worker | |
| 書いたものが読む側に出るまで待つ上限（[書いた直後の読み](../api/writes.md)） | 両方 | バックエンド | |

- 秘密は、暗号化して置く（[秘密の暗号化](secrets.md)）。
- git リポジトリには、設定を置かない。なぜ：git リポジトリは、人やエージェントが読めるから。

## 設定を読むとき

- バックエンドは、要求ごとに設定の DB を読む。メモリに保持しない。
  - なぜ：利用者が変えた設定が、次の要求からそのまま効くから。何台並べても、食い違わない（[状態を持たない](../backend/README.md#状態を持たない)）。
- worker は、repository の一覧を読み直すたびに、設定の DB を読む（[見回り](../backend/worker/patrol.md)）。
  - 見回りの間隔と git の操作の時間の上限は、読み直したあとの見回りから効く。
- worker は、設定の DB を読むだけで、書かない。OS の権限で、書けないようにする（[設定の DB](database.md#db-の種類)）。
  - worker が書くもの（ハートビート、スナップショット）は、clone のディレクトリと置き場に書く（[ハートビート](../backend/worker/operation.md#ハートビート)）。
- バックエンドは、要求のほかに、1 分ごとに worker のトークンを作り直す（[worker のトークン](../adapters/github-app.md#worker-のトークン)）。
- 設定の DB に、セットアップの終わった記録がなければ、バックエンドは、セットアップの API、サインインの中継、WebUI だけを返す（[セットアップが終わるまでのほかの API](../api/endpoints/setup.md#セットアップが終わるまでのほかの-api)）。worker は、見回りをせずに待つ。

## 設定のコマンド

設定の DB を直接書くコマンドである。サーバのホストで使う。どれも、`LOGIFORGE_DB` と 2 つの鍵を求める（[環境変数](../deploy/env.md)）。

| コマンド | すること |
|---|---|
| `logiforge settings load <ファイル>` | JSON の設定を、設定の DB に書く。`-` なら標準入力から読む |
| `logiforge settings rekey --old-backend-key-file <ファイル> --old-shared-key-file <ファイル>` | すべての秘密を、新しい鍵で暗号化し直す（[鍵を取り替える](secrets.md#鍵を取り替える)） |

- `settings load` の用途は、テストや compose で、セットアップの画面を使わずに設定を入れることである。
  - JSON の項目は、[設定の DB に置くもの](#設定の-db-に置くもの) と同じである。形は、実装の `internal/settings` の `Config` が正本である。
  - 書くのは、渡した設定の全体である。JSON にない設定は消す。repository は、JSON にないものを外す。記録の鍵は、足すだけで消さない。
  - 登録した repository の名前と `owner/name` とディレクトリは、変えられない（[repository を外したとき](../api/endpoints/settings.md#repository-を外したとき)）。
  - 記録の鍵は、32 バイトの種（`seed`）か、32 バイト以上の乱数の文字列（`secret`。種はその SHA-256）で渡す。短い `secret` は断る。
    - なぜ：短い文字列から作った鍵は、総当たりで見つけられ、記録を偽られるから（[記録のコメント](../adapters/records.md#記録のコメント)）。
- どちらのコマンドも、ほかの人は使えない。なぜ：設定の DB のファイルと、バックエンドの鍵が要り、それを読めるのはサーバのホストに入れる人だけだから。

## バックアップ

- 設定の DB は、バックアップを取る。
  - なぜ：git から作り直せないから。失うと、セットアップからやり直し、記録の署名の鍵も失う。
  - 古いバックアップから戻したときの、識別子の番号の扱いは、[番号の重なり](../backend/ids/numbering.md#番号の重なり) に書く。
- 暗号化の鍵は、DB と別の場所に、バックアップを取る。鍵を失うと、DB の秘密を読めない。

## 対応しないこと

- v0.3.0 からの移行。環境変数で設定を渡していた版の設定と記録の署名の鍵は、設定の DB に移さない。新しく入れるときは、初回のセットアップで鍵を作る。
