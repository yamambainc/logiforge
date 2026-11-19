# セットアップの API

WebUI が、初回のセットアップの画面から呼ぶ（[セットアップ](../../../docs/webui/setup.md)）。
流れと決まりは、[初回のセットアップ](../../settings/setup.md) に書く。

## 呼び出し

| 呼び出し | operationId | すること |
|---|---|---|
| `GET /setup` | `getSetup` | セットアップが終わったか、どの段まで進んだか、選んだ構成を返す |
| `PUT /setup/gitforge` | `setSetupGitForge` | 構成（`github` か `forgejo`）と、WebUI の URL（サインインの `redirect_uri` の元）を決める |
| `POST /setup/github/manifest` | `createSetupGitHubManifest` | owner、WebUI の URL、ほかの owner にも install するかを受け取り、manifest と送り先の URL を返す |
| `POST /setup/github/conversion` | `convertSetupGitHubManifest` | manifest flow の `code` を受け取り、GitHub App を設定の DB に置く。App の slug と install の URL を返す |
| `PUT /setup/repository` | `setSetupRepository` | GitHub の構成で、最初の repository（名前と `owner/name`、ディレクトリの repository ならパスも）を登録する。App の installation token で読めなければ `validation_failed` |
| `PUT /setup/forgejo` | `setSetupForgejo` | Forgejo の URL、organization、OAuth2 のアプリケーション、worker のユーザーとトークンを確かめてから置く |
| `POST /setup/finish` | `finishSetup` | サインインした主体を、最初の利用者にし、セットアップを終える |

## 認証

- `POST /setup/finish` のほかは、認証を求めない（[だれがセットアップできるか](../../settings/setup.md#だれがセットアップできるか)）。
- `POST /setup/finish` は、WebUI のサインインで得たトークンを `Authorization` で求める。
  - トークンが Logiforge の GitHub App か OAuth2 のアプリケーションのものでなければ、`unauthenticated`（401）を返す。
- セットアップが終わったあとは、どれも `setup_done`（409）を返す。`GET /setup` だけは、終わったことを返す。

## セットアップが終わるまでのほかの API

- セットアップが終わるまで、ほかの API は `setup_required`（503）を返す。次のものは返す。
  - `/healthz`
  - WebUI のサインインの中継（`/oauth/` の下の 2 つ）。構成と、サインインのアプリケーションを置いたあとに限る（[サインインの中継](../../webui/oauth-relay.md)）。
    - なぜ：`finishSetup` に、サインインで得たトークンが要るから。
  - なぜ 503 か：設定がそろうまで、サービスとして使えないから。ヘルスチェックは通し、起動し直されないようにする。

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../openapi.yaml) の各 operationId に書く。
