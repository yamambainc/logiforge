# 設定の API

管理者が、設定の画面から呼ぶ（[設定](../../../docs/webui/settings.md)）。
どの設定があり、誰が読むかは、[設定](../../settings/README.md) に書く。

## 呼び出し

| 呼び出し | operationId | すること |
|---|---|---|
| `GET /settings` | `getSettings` | 設定を返す。秘密は、置いたかどうかだけを返す |
| `PATCH /settings/gitforge` | `updateGitForgeSettings` | GitForge とのつなぎ方と、見回りの間隔を変える。秘密は、入れたものだけを置き換える |
| `GET /settings/repositories` | `listRegisteredRepositories` | 登録した repository の一覧を返す |
| `PUT /settings/repositories/{repository}` | `registerRepository` | repository を登録する。同じ名前があれば、`gitforge_ref` とディレクトリを置き換える |
| `DELETE /settings/repositories/{repository}` | `unregisterRepository` | repository を外す |
| `PUT /settings/users` | `setUsers` | `user` とみなすアカウント名と team の一覧を置き換える |
| `GET /settings/admins` | `listAdmins` | 管理者の一覧を返す |
| `PUT /settings/admins/{login}` | `addAdmin` | 管理者を足す |
| `DELETE /settings/admins/{login}` | `removeAdmin` | 管理者を外す。最後の 1 人なら `validation_failed` |
| `GET /settings/record-keys` | `listRecordKeys` | 記録の署名の鍵の ID、公開鍵、作った時刻を返す |
| `POST /settings/record-keys` | `createRecordKey` | 新しい鍵を作り、書くときの鍵にする |

## 認証と認可

- どれも、主体が管理者であることを求める（[管理者を確かめる](../../settings/admins.md#管理者を確かめる)）。管理者でなければ `forbidden`（403）を返す。
- 書く呼び出しは、変えた管理者と時刻を残す（[記録](../../settings/admins.md#記録)）。

## 確かめること

- repository の名前は、[repository](../README.md#repository) の決まりに従う。
- GitHub を使う構成で repository を登録するとき、GitHub App の installation token で、その GitHub リポジトリを読めることを確かめる。読めなければ `validation_failed` を返し、`detail` に install の URL を書く。
- Forgejo を使う構成で worker のトークンを置き換えるとき、そのトークンで worker のユーザーと organization を読めることを確かめる。

## repository を外したとき

- worker は、次に一覧を読み直したときから、その repository を見回らない。
- 置き場のスナップショットと `keep/` は消さない。同じ名前で登録し直せば、続きから読める。
  - なぜ：誤って外したときに、作り直さずに戻せるようにするため。

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../openapi.yaml) の各 operationId に書く。
