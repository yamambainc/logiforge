# セットアップの API

管理者が、初回のセットアップで、WebUI から呼ぶ（[セットアップ](../../../docs/webui/setup.md)）。
流れと決まりは、[初回のセットアップ](../../settings/setup.md) に書く。

## 呼び出し

| 呼び出し | operationId | すること |
|---|---|---|
| `GET /setup` | `getSetup` | セットアップが終わったか、どの段まで進んだか、選んだ構成を返す。コードは求めない |
| `POST /setup/code` | `checkSetupCode` | セットアップのコードが合うかだけを確かめる。何も置かない |
| `PUT /setup/gitforge` | `setSetupGitForge` | 構成（`github` か `forgejo`）を決める |
| `POST /setup/github/manifest` | `createSetupGitHubManifest` | owner、WebUI の URL、ほかの owner にも install するかを受け取り、manifest と送り先の URL を返す |
| `POST /setup/github/conversion` | `convertSetupGitHubManifest` | manifest flow の `code` を受け取り、GitHub App を設定の DB に置く。App の slug と install の URL を返す |
| `PUT /setup/forgejo` | `setSetupForgejo` | Forgejo の URL、organization、OAuth2 のアプリケーション、worker のユーザーとトークンを確かめてから置く |
| `POST /setup/finish` | `finishSetup` | サインインした主体を、最初の管理者と `user` にし、セットアップを終える |

## 認証

- `GET /setup` のほかは、`Logiforge-Setup-Code` ヘッダーで、セットアップのコードを求める（[セットアップのコード](../../settings/setup.md#セットアップのコード)）。
- `POST /setup/finish` は、コードに加えて、WebUI のサインインで得たトークンを `Authorization` で求める。
  - トークンが Logiforge の GitHub App か OAuth2 のアプリケーションのものでなければ、`unauthenticated`（401）を返す。
- セットアップが終わったあとは、どれも `setup_done`（409）を返す。`GET /setup` だけは、終わったことを返す。
  - なぜ `GET /setup` はコードを求めないか：WebUI が、セットアップの画面を出すかどうかを、コードを入れる前に決めるから。

## セットアップが終わるまでのほかの API

- セットアップが終わるまで、ほかの API は `setup_required`（503）を返す。次のものは返す。
  - `/healthz`
  - WebUI のサインインの中継（`/oauth/` の下の 2 つ）。構成と、サインインのアプリケーションを置いたあとに限る（[サインインの中継](../../webui/oauth-relay.md)）。
    - なぜ：`finishSetup` に、サインインで得たトークンが要るから。
  - なぜ 503 か：設定がそろうまで、サービスとして使えないから。ヘルスチェックは通し、起動し直されないようにする。

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../openapi.yaml) の各 operationId に書く。
