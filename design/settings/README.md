# 設定

バックエンドと worker の設定を、どこに置き、誰が変えるかを確かめるときに読む。

## 読む順

1. このページ：起動時に渡すものと、設定の DB に置くもの
2. [秘密の暗号化](secrets.md)：秘密ごとの鍵と、worker から隠すもの
3. [設定の DB](database.md)：DB の種類と、置く表
4. [初回のセットアップ](setup.md)：使い捨てのコード、GitHub App の作成、最初の管理者
5. [管理者](admins.md)：設定を変えられる主体
6. [v0.3.0 からの移行](migrate.md)

## 起動時に渡すもの

起動時に環境変数で渡すのは、次のものだけである。名前と形は、[環境変数と秘密](../deploy/env.md) に書く。

- 設定の DB の場所
- 秘密を暗号化する鍵（[秘密の暗号化](secrets.md)）
- そのホストに結び付くもの
  - 待ち受けるアドレス
  - 置き場、clone、作業用の git の複製のディレクトリ
  - なぜ環境変数か：DB を読む前に要り、ホストごとに違うから。

ほかの設定は、すべて設定の DB に置く。

- なぜ：repository や利用者を足すたびに、サーバを起動し直さずに済むから。管理者が WebUI から変えられる。

## 設定の DB に置くもの

| 設定 | 構成 | 使う | 秘密 |
|---|---|---|---|
| 構成（GitHub を使う / Forgejo を使う） | 両方 | 両方 | |
| GitHub App の ID、client_id、slug、秘密鍵、client secret（[GitHub アダプタ](../adapters/github-app.md#github-app)） | GitHub | 両方 | 秘密鍵と client secret |
| GitHub の REST と画面の URL | GitHub | 両方 | |
| Forgejo の URL（BE から、ブラウザから）、organization | Forgejo | 両方 | |
| Forgejo の OAuth2 のアプリケーションの client_id と client secret | Forgejo | バックエンド | client secret |
| worker のユーザーとトークン（[Forgejo アダプタ](../adapters/forgejo.md)） | Forgejo | 両方 | トークン |
| GitHub リポジトリの repository の一覧（名前と `owner/name`）（[repository の登録](../adapters/README.md#repository-の登録)） | GitHub | 両方 | |
| ディレクトリの repository の一覧（名前、`owner/name`、パス） | 両方 | 両方 | |
| `user` とみなすアカウント名と team | 両方 | バックエンド | |
| 管理者（[管理者](admins.md)） | 両方 | バックエンド | |
| 記録の署名の秘密鍵（[記録のコメント](../adapters/records.md#記録のコメント)） | 両方 | バックエンド | ✓ |
| 記録の署名の公開鍵 | 両方 | 両方 | |
| WebUI の URL（サインインの `redirect_uri` に使う） | 両方 | バックエンド | |
| 見回りの間隔、repository の一覧を読み直す間隔、記録を読む間隔、git の操作の時間の上限（[見回り](../backend/worker/patrol.md)） | 両方 | worker | |
| 書いたものが読む側に出るまで待つ上限（[書いた直後の読み](../api/writes.md)） | 両方 | バックエンド | |

- 秘密は、暗号化して置く（[秘密の暗号化](secrets.md)）。
- git リポジトリには、設定を置かない。なぜ：git リポジトリは、人やエージェントが読めるから。

## 設定を読むとき

- バックエンドは、要求ごとに設定の DB を読む。メモリに保持しない。
  - なぜ：管理者が変えた設定が、次の要求からそのまま効くから。何台並べても、食い違わない（[状態を持たない](../backend/README.md#状態を持たない)）。
- worker は、repository の一覧を読み直すたびに、設定の DB を読む（[見回り](../backend/worker/patrol.md)）。
  - 見回りの間隔と git の操作の時間の上限は、読み直したあとの見回りから効く。
- worker は、設定の DB を読むだけで、書かない。
- 設定の DB に、セットアップの終わった記録がなければ、バックエンドは、セットアップの API、サインインの中継、WebUI だけを返す（[セットアップが終わるまでのほかの API](../api/endpoints/setup.md#セットアップが終わるまでのほかの-api)）。worker は、見回りをせずに待つ。

## バックアップ

- 設定の DB は、バックアップを取る。
  - なぜ：git から作り直せないから。失うと、セットアップからやり直し、記録の署名の鍵も失う。
- 暗号化の鍵は、DB と別の場所に、バックアップを取る。鍵を失うと、DB の秘密を読めない。
