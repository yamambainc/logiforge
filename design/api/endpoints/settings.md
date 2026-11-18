# 設定の API

利用者が、設定の画面から呼ぶ（[設定](../../../docs/webui/settings.md)）。
どの設定があり、誰が読むかは、[設定](../../settings/README.md) に書く。

## 呼び出し

| 呼び出し | operationId | すること |
|---|---|---|
| `GET /settings` | `getSettings` | 設定を返す。秘密は、置いたかどうかだけを返す |
| `PATCH /settings/gitforge` | `updateGitForgeSettings` | GitForge とのつなぎ方と、見回りの間隔を変える。秘密は、入れたものだけを置き換える |
| `GET /settings/repositories` | `listRegisteredRepositories` | 登録した repository の一覧を返す |
| `PUT /settings/repositories/{repository}` | `registerRepository` | repository を登録する。登録済みの名前の `gitforge_ref` とディレクトリは変えられない（`validation_failed`） |
| `DELETE /settings/repositories/{repository}` | `unregisterRepository` | repository を外す |
| `GET /settings/record-keys` | `listRecordKeys` | 記録の署名の鍵の ID、公開鍵、作った時刻を返す |
| `POST /settings/record-keys` | `createRecordKey` | 新しい鍵を作り、書くときの鍵にする |

## 認証と認可

- どれも、主体が `user` であることを求める（[利用者の決め方](../../settings/users.md#利用者の決め方)）。`user` でなければ `forbidden`（403）を返す。
- 書く呼び出しは、変えた利用者と時刻を残す（[記録](../../settings/users.md#記録)）。

## 確かめること

- repository の名前は、[repository](../README.md#repository) の決まりに従う。
- GitHub を使う構成で repository を登録するとき、GitHub App の installation token で、その GitHub リポジトリを読めることを確かめる。読めなければ `validation_failed` を返し、`detail` に install の URL を書く。
- Forgejo を使う構成で worker のトークンを置き換えるとき、そのトークンで worker のユーザーと organization を読めることを確かめる。

## repository を外したとき

- worker は、次に一覧を読み直したときから、その repository を見回らない。
- 置き場のスナップショットと `keep/` は消さない。
- 外した名前を登録し直せるのは、前と同じ `gitforge_ref` とディレクトリのときだけである。そのときは、続きから読む。
  - 違うものなら、`validation_failed` を返す。
  - なぜ：置き場は、repository の名前をキーにするから。別の git リポジトリを同じ名前にすると、履歴が混ざり、書き換えとみなされて壊れた状態になるか、古いスナップショットを返す。
- そのため、外した repository の名前と `gitforge_ref` とディレクトリは、設定の DB に残す。
  - なぜ：誤って外したときに、作り直さずに戻せるようにするため。

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../openapi.yaml) の各 operationId に書く。
