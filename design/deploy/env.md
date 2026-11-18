# 環境変数と秘密

バックエンドと worker に渡す環境変数を確かめるときに読む。
渡す設定と秘密情報の中身と理由は、[設定と秘密情報](../adapters/settings.md) に書く。

Forgejo を使う構成のものを書く。GitHub を使う構成で違うものは、[GitHub を使う構成](#github-を使う構成) に書く。✓ の行は秘密である。秘密は、`_FILE` を付けて、ファイルで渡す。

## バックエンド

バックエンド（`logiforge serve`）に渡す。

| 名前 | 秘密 | 中身 |
|---|---|---|
| `LOGIFORGE_ADDR` | | 待ち受けるアドレス。既定は `127.0.0.1:8080`。コンテナでは `0.0.0.0:8080` にする |
| `LOGIFORGE_STORE_DIR` | | 置き場（手元のディレクトリ）のパス。読むだけ |
| `LOGIFORGE_FORGEJO_URL` | | Forgejo の URL。クライアントに返す git の URL にも使う |
| `LOGIFORGE_FORGEJO_ORG` | | repository を置く organization |
| `LOGIFORGE_USER_TEAM` | | `user` とみなす team |
| `LOGIFORGE_DIRECTORIES` | | ディレクトリの repository（`名前=owner/name:パス,…`）。バックエンドと worker に、同じ値を渡す（[ディレクトリ方式](../directory/README.md)） |
| `LOGIFORGE_WORKER_LOGIN` | | worker のユーザー。API の主体としては受け付けない。apply request の作成で git を読むのにも使う |
| `LOGIFORGE_WORKER_TOKEN` | ✓ | worker のユーザーのトークン（読むだけ）。apply request の作成で、作業用の git の複製に fetch するのに使う |
| `LOGIFORGE_GIT_CACHE_DIR` | | 作業用の git の複製を置く、一時的なディレクトリ。既定は OS の一時ディレクトリ（`/tmp`） |
| `LOGIFORGE_GIT_ALLOW_PROTOCOL` | | git で使える転送の方式。worker と同じ |
| `LOGIFORGE_RECORD_KEYS` | ✓ | 記録の署名の秘密（`鍵の ID=秘密,…`。最初のものを書くときに使う）。Ed25519 の秘密鍵は、秘密の SHA-256 から作る。worker に渡す公開鍵は、`logiforge record-public-keys` が、この環境変数から作って書く |
| `LOGIFORGE_WAIT_TIMEOUT` | | 書いたものが読む側に出るまで待つ上限。既定は `10s` |
| `LOGIFORGE_OAUTH_CLIENT_ID` | | WebUI のサインインに使う、Forgejo の OAuth2 のアプリケーションの client_id（[サインインの中継](../webui/oauth-relay.md)） |
| `LOGIFORGE_OAUTH_CLIENT_SECRET` | ✓ | その client secret |
| `LOGIFORGE_OAUTH_REDIRECT_URI` | | Forgejo に登録した `redirect_uri`。登録したものと完全に一致させる |
| `LOGIFORGE_FORGEJO_PUBLIC_URL` | | ブラウザから Forgejo への URL。WebUI のサインインで、authorize に使う。既定は `LOGIFORGE_FORGEJO_URL`。BE から Forgejo への URL（内部の名前など）と、ブラウザからの URL が違う配置で付ける |

## worker

worker（`logiforge worker`）に渡す。

| 名前 | 秘密 | 中身 |
|---|---|---|
| `LOGIFORGE_WORKER_DIR` | | clone を置く、永続のボリュームのパス |
| `LOGIFORGE_STORE_DIR` | | 置き場（手元のディレクトリ）のパス。読み書きする |
| `LOGIFORGE_FORGEJO_URL` | | Forgejo の URL |
| `LOGIFORGE_FORGEJO_ORG` | | repository を置く organization |
| `LOGIFORGE_WORKER_LOGIN` | | worker のユーザー |
| `LOGIFORGE_WORKER_TOKEN` | ✓ | worker のユーザーのトークン（`read:repository`、`read:organization`、`read:issue` だけ） |
| `LOGIFORGE_DIRECTORIES` | | ディレクトリの repository（`名前=owner/name:パス,…`）。バックエンドと同じ値 |
| `LOGIFORGE_RECORD_PUBLIC_KEYS` | | 記録の署名の公開鍵（`鍵の ID=base64,…`。`logiforge record-public-keys` の出力）。反映かの判定で、記録を確かめるのに使う（[反映かの判定](../backend/verify/README.md)）。秘密ではないが、なければ起動しない |
| `LOGIFORGE_WORKER_RECORD_INTERVAL` | | 記録を読んで控えに足す間隔。既定は見回りの間隔。GitHub では `30s`（[記録の控え](../backend/verify/records.md)） |
| `LOGIFORGE_WORKER_INTERVAL` | | 見回りの間隔。既定は `1s`（[見回り](../backend/worker/patrol.md)） |
| `LOGIFORGE_WORKER_LIST_INTERVAL` | | repository の一覧を読み直す間隔。既定は `10s`（[見回り](../backend/worker/patrol.md)） |
| `LOGIFORGE_GIT_TIMEOUT` | | git の操作 1 回の時間の上限。既定は `10m`（[すること](../backend/worker/patrol.md#すること)）。GitHub を使う構成では `20m` より短くする（[読むだけの GitHub App](../adapters/github.md#読むだけの-github-app)） |
| `LOGIFORGE_GIT_ALLOW_PROTOCOL` | | git で使える転送の方式。既定は `https:file`。`http` は、テストのときだけ足す |

## GitHub を使う構成

構成は、`LOGIFORGE_GITFORGE`（`forgejo` か `github`。既定は `forgejo`）で選ぶ。バックエンドと worker に、同じ値を渡す。
`github` のときは、`LOGIFORGE_FORGEJO_*`、`LOGIFORGE_WORKER_LOGIN`、`LOGIFORGE_WORKER_TOKEN` のかわりに、次のものを渡す。ほかは、上の表と同じである。

| 名前 | 使う | 秘密 | 中身 |
|---|---|---|---|
| `LOGIFORGE_GITHUB_REPOSITORIES` | 両方 | | repository の名前と `owner/name` の組（`名前=owner/name,…`）。owner は複数でもよい（[GitHub アダプタ](../adapters/github.md)）。ディレクトリの repository は `名前=owner/name:パス`（[ディレクトリ方式](../directory/README.md)） |
| `LOGIFORGE_GITHUB_APP_ID` | 両方 | | 読むだけの GitHub App の App ID（[GitHub アダプタ](../adapters/github.md#読むだけの-github-app)）。public のリポジトリだけなら、なくてもよい。そのときは、トークンなしで読む（REST の上限は 1 時間に 60 回） |
| `LOGIFORGE_GITHUB_APP_PRIVATE_KEY` | 両方 | ✓ | その App の秘密鍵（GitHub が渡す PEM。`_FILE` でファイルから渡す）。`LOGIFORGE_GITHUB_APP_ID` と一緒に置く |
| `LOGIFORGE_USER_LOGINS` | バックエンド | | `user` とみなす GitHub のアカウント名（`,` で並べる） |
| `LOGIFORGE_USER_TEAM` | バックエンド | | `user` とみなす team（`org/team の slug`。`,` で並べる） |
| `LOGIFORGE_GITHUB_API_URL`、`LOGIFORGE_GITHUB_URL` | 両方 | | REST の URL と、画面と git の URL。既定は `https://api.github.com` と `https://github.com` |

- WebUI のサインインには、Logiforge の OAuth App の Client ID と client secret を、`LOGIFORGE_OAUTH_CLIENT_ID` と `LOGIFORGE_OAUTH_CLIENT_SECRET` に渡す。`LOGIFORGE_OAUTH_REDIRECT_URI` は、OAuth App の Authorization callback URL と同じにする。`LOGIFORGE_FORGEJO_PUBLIC_URL` は使わない。
  - バックエンドは、同じ 2 つで、主体のトークンが Logiforge の OAuth App のものかを確かめる。渡さなければ、`user` の主体はいない（[GitHub アダプタの認証](../adapters/github.md#認証)）。
- `LOGIFORGE_GITHUB_READ_TOKEN`（personal access token）を置くと、起動しない。
  - なぜ：personal access token は使わないから（[GitHub アダプタ](../adapters/github.md#アカウントとトークン)）。
- 見回りの間隔は `10s`、記録を読む間隔（`LOGIFORGE_WORKER_RECORD_INTERVAL`）は `30s` を目安にする（[GitHub アダプタの見回りの間隔](../adapters/github.md#見回りの間隔)）。
