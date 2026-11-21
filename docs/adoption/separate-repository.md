# 別リポジトリ方式の導入

GitHub にある既存のプロジェクトのために、Logiforge を動かし、別の repository でスナップショットツリーを始めるときに読む。
置き方の選び方は、[既存のプロジェクトへの導入](README.md) に書く。subtree 方式も、この手順を済ませてから行う。

## 構成

- Logiforge の repository を置く GitForge は、Forgejo か GitHub から選ぶ。プロジェクトのリポジトリは、GitHub のままにする。
  - Forgejo：このページの手順で進める。GitHub のアカウントのない人も参加できる。
  - GitHub：apply request と人への依頼を、GitHub の pull request と issue として見られる。「Logiforge を動かす」から「main を守る」までを、[Logiforge の repository を GitHub に置く](github/README.md) に置き換える。
- Forgejo、バックエンド、worker の関係は、[動かし方（コンテナ）](../../design/deploy/README.md) に書く。

## Logiforge を動かす

- 試すとき：[compose で動かす](../development/compose.md) の手順で、Forgejo、バックエンド、worker を手元で動かす。
- 続けて使うとき：[動かし方（コンテナ）](../../design/deploy/README.md) に従って、タスクを置く。起動時に渡す環境変数は、[環境変数](../../design/deploy/env.md) の表に従う。
  - 渡すのは、設定の DB の場所、2 つの鍵、ホストに結び付くディレクトリとアドレスだけである。ほかの設定は、下の「Forgejo を用意する」のあと、セットアップの画面で入れる。
- v0.3.0 からの移行には対応しない。新しく入れ、初回のセットアップから始める。

## Forgejo を用意する

Forgejo に、次のものを作る。権限の正本は、[Forgejo アダプタ](../../design/adapters/forgejo.md) の「主体」である。

1. organization（例：`logiforge`）
2. worker のユーザーと、読むだけの team。worker のユーザーは、この team にだけ入れる。トークンの範囲は `read:repository`、`read:organization`、`read:issue` にする（[worker のトークンを確かめる](../../design/adapters/forgejo.md#worker-のトークンを確かめる)）。
3. 利用者の team。コード、issue、pull request を書け（`write`）、repository を作れる（`can_create_org_repo`）team にする。
   - 利用者の名簿はない。WebUI でサインインし、organization のコードを書ける team（`write` 以上）にいる人が、利用者（`user`）になる（[利用者](../../design/settings/users.md)）。
   - 利用者は、設定も変えられる。そのため、コードを書ける team には、信頼できる人だけを入れる。
4. エージェントの team。コード、issue、pull request を書ける team にする。reviewer は、この team で merge する。
   - エージェントは、WebUI でサインインしない。これは運用の決まりである。CLI のトークンを使うので、`client` になる（[Forgejo アダプタ](../../design/adapters/forgejo.md#認証)）。
5. WebUI のサインインに使う OAuth2 のアプリケーション（confidential client）。redirect URI は、WebUI の `/oauth/callback` にする（[サインイン](../../design/webui/auth.md)）。

- 作り方の例は、[scripts/compose-setup.sh](../../scripts/compose-setup.sh) にある。compose で試すときは、このスクリプトがすべて作り、設定の DB にも書く。
- プロジェクトの人は、Forgejo のユーザーを作って、利用者の team に入れる。

## セットアップ

初めて起動したあとに 1 回だけ行う。画面は [セットアップ](../webui/setup.md)、流れの正本は [初回のセットアップ](../../design/settings/setup.md) である。

1. WebUI（例：`https://logiforge.example.com/setup`）を開き、構成で Forgejo を選ぶ。
2. Forgejo の URL と organization を入れる。
3. OAuth2 のアプリケーションの client_id と client secret、worker のユーザーとトークンを入れる。
4. 「Forgejo でサインインして、利用者になる」を押す。サインインした人が、最初の利用者になる。コードを書ける team にいなければ、断られる。

- あとから変えるときは、[設定](../webui/settings.md) の画面で変える。秘密の入れ直しも、そこで行う。サーバは起動し直さない。

## main を守る

- Forgejo では、repository を作るときに、バックエンドが `main` を守る設定を掛ける。人が掛ける設定はない（[Forgejo アダプタ](../../design/adapters/forgejo.md) の「repository の用意」）。
- 掛かったことは、Forgejo の API で確かめる。`$FORGEJO_TOKEN` は、repository を作った利用者か organization の owner のトークンにする。
  - なぜ：ブランチを守る設定を読むには、repository の admin の権限が要るから。repository を作った利用者は、その repository の admin になる。
- `main` の規則で、`enable_push` が `false` ならよい。merge できる人は絞らない（reviewer が merge する）。

```sh
curl -fsS -H "Authorization: token $FORGEJO_TOKEN" \
  https://forgejo.example.com/api/v1/repos/logiforge/myapp-cache-strategy/branch_protections
```

- 設定が外れて、反映によらない変更が `main` に入っても、Logiforge は受け入れる（[main への直接の変更](../apply-request/README.md#main-への直接の変更)）。

## repository を作る

1. 利用者が、WebUI（バックエンドの URL）を開き、Forgejo のアカウントでサインインする。
2. 設定の画面の「足す」で、名前と、根の仮説の主文を書いて作る（[repository を足す](../webui/settings.md#repository-を足す)）。

- 名前は、プロジェクトと問いがわかるものにする（例：`myapp-cache-strategy`）。
  - なぜ：1 つの organization に、プロジェクトをまたいだ repository が並ぶから。

## CLI を用意する

opener と reviewer（人とエージェント）ごとに行う。

1. CLI を入れる（[インストール](../install.md)）。
2. Forgejo で、アクセストークンを作る。スコープは、[CLI の設計](../../design/cli/README.md) の「認証」に従う。
3. 次のコマンドで、トークンを渡し、git を設定し、clone する。

```sh
logiforge auth login --with-token --host https://logiforge.example.com < token.txt
logiforge auth setup-git --url https://forgejo.example.com
logiforge repo clone myapp-cache-strategy
```

- このあとの作業の流れ（ひな形、`check`、commit と push、`ar create`）は、[CLI の設計](../../design/cli/README.md) の「流れ」に書く。

## プロジェクトとつなぐ

スナップショットツリーとプロジェクトは、互いを SHA 付きのリンクで指す。

- スナップショットツリーから、プロジェクトのコードや実験の結果を指すとき
  - 検証結果に、`link` の artifact を書く（[artifact の置き方](../../design/files/artifacts.md)）。
  - `uri` は、コミットの SHA を含む GitHub の URL にする（例：`https://github.com/<owner>/<repo>/blob/<SHA>/bench/result.csv`）。
  - ファイルを指すときは、`sha256` も書く。
  - なぜ：ブランチの名前の URL では、指す中身があとで変わるから（[再現性](../architecture/README.md)）。
- プロジェクトの PR や issue から、結論を指すとき
  - WebUI のビュワーの URL を書く：`https://logiforge.example.com/r/<repository>/tree?node=<ノードの識別子>&commit=<main のコミットの SHA>`（[画面の一覧](../../design/webui/screens.md)）。
  - なぜ `commit` を付けるか：あとで結論が変わっても、PR を書いた時点のスナップショットツリーを見られるから。
