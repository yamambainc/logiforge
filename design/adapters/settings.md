# 設定と秘密情報

バックエンドと worker に、どの設定と秘密情報を渡すかを確かめるときに読む。
環境変数の名前と形は、[環境変数と秘密](../deploy/env.md) に書く。

次のものは、設定ファイルか環境変数に置く。git リポジトリには置かない。

- なぜ：git リポジトリは、人やエージェントが読めるから。

バックエンド：

- 構成（GitHub を使う / Forgejo を使う）
- 記録のコメントの署名の秘密鍵（Ed25519。[記録のコメント](records.md#記録のコメント)）
  - 鍵は、バックアップを取る。鍵を失うと、新しい記録に署名できない。それまでの記録は、公開鍵で確かめられる。
  - 鍵を取り替えるときは、新しい鍵で書き、確かめる公開鍵を複数持てるようにする。署名に書いた鍵の ID で、確かめる公開鍵を選ぶ。
- 置き場の接続先と、読む鍵（[スナップショットの置き場のアダプタ](snapshot-store.md)）
- git を読む、読むだけの資格情報（worker と同じもの）と、作業用の git の複製を置く一時的なディレクトリ（[作業用の git の複製](../backend/apply-request/create.md#作業用の-git-の複製)）
- GitHub を使う構成だけ
  - WebUI のサインインに使う OAuth App の client_id と client secret（[GitHub アダプタの認証](github.md#認証)）
  - `user` とみなす GitHub のアカウント名か、organization の team（[GitHub アダプタ](github.md)）
  - repository の名前と `owner/name` の組
- Forgejo を使う構成だけ
  - Forgejo の URL
  - repository を置く organization と、`user` とみなす team
- バックエンドは、GitForge に書く資格情報を持たない。書くときは、要求を送った主体のトークンを使う（[GitForge アダプタの共通の書き方](pull-requests.md)）。

[worker](../backend/worker/README.md)：

- 構成と、repository の名前と `owner/name` の組（GitHub を使う構成だけ）
- git を読み、repository の一覧を読む、読むだけの資格情報（[git アダプタ](git.md)）
  - GitHub を使う構成：Contents、Issues、Pull requests、Metadata の Read だけを持つ、読むだけの GitHub App の ID と秘密鍵。worker が、installation token を作って読む（[GitHub アダプタ](github.md#読むだけの-github-app)）。public のリポジトリだけなら、なくてもよい
  - Forgejo を使う構成：`read:repository`、`read:organization`、`read:issue` だけを持つ、worker のためのユーザーのトークン
- 記録の署名の公開鍵。反映かの判定で、記録を確かめるのに使う（[反映かの判定](../backend/verify/README.md)）
- 置き場の接続先と、書く鍵
- 見回りの間隔（[見回り](../backend/worker/patrol.md)）と、git の操作の時間の上限（[すること](../backend/worker/patrol.md#すること)）
- clone を置く、永続するストレージのパス

鍵を置く場所（バックエンドと worker を動かす環境）の管理者を信頼する。

- ほかの主体には、鍵を置く場所を読ませない。
- worker は、git と GitForge に書く鍵を持たない。記録を作る秘密鍵も持たない。
