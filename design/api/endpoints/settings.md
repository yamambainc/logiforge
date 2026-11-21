# 設定の API

利用者が、設定の画面から呼ぶ（[設定](../../../docs/webui/settings.md)）。
どの設定があり、誰が読むかは、[設定](../../settings/README.md) に書く。

## 呼び出し

| 呼び出し | operationId | すること |
|---|---|---|
| `GET /settings` | `getSettings` | 設定を返す。秘密は、置いたかどうかだけを返す |
| `PATCH /settings/gitforge` | `updateGitForgeSettings` | GitForge とのつなぎ方と、見回りの間隔を変える。秘密は、入れたものだけを置き換える |
| `GET /settings/repositories` | `listRegisteredRepositories` | 登録した repository の一覧を返す。それぞれに `prepared` を付ける |
| `PUT /settings/repositories/{repository}` | `registerRepository` | repository を登録する。登録済みの名前の `gitforge_ref` とディレクトリは変えられない（`validation_failed`）。応答に `prepared` を付ける |
| `DELETE /settings/repositories/{repository}` | `unregisterRepository` | repository を外す |

## 認証と認可

- どれも、主体が、WebUI のサインインのトークンの `user` であることを求める（[利用者の決め方](../../settings/users.md#利用者の決め方)、[トークンの出どころ](../../settings/users.md#トークンの出どころ)）。そうでなければ `forbidden`（403）を返す。
  - 書く呼び出しは、権限のキャッシュを使わずに確かめる（[権限のキャッシュ](../../settings/users.md#権限のキャッシュ)）。
- 書く呼び出しは、変えた利用者と時刻を残す（[記録](../../settings/users.md#記録)）。

## 確かめること

- repository の名前は、[repository](../README.md#repository) の決まりに従う。
- GitHub を使う構成で repository を登録するとき、GitHub App の installation token で、その GitHub リポジトリを読めることを確かめる。読めなければ `validation_failed` を返し、`detail` に install の URL を書く。
- Forgejo を使う構成で worker のトークンを置き換えるとき、そのトークンで読めて、書けないことを確かめる（[worker のトークンを確かめる](../../adapters/forgejo.md#worker-のトークンを確かめる)）。
- 登録できるのは、要求した本人が Write を持つ git リポジトリだけである（本人のトークンで、キャッシュを使わずに確かめる）。そうでなければ `forbidden` を返す。
  - なぜ：登録すると、その git リポジトリに Write を持つ人が `user` になるので、利用者の範囲を、本人の権限の外まで広げないため。
- サインインと権限の確かめに使う設定（client_id、client secret、GitForge の URL、organization）を変えるときは、置く前に、変えたあとの設定でも、要求した本人が `user` のままであることを確かめる。そうでなければ `validation_failed` を返す。
  - なぜ：間違った資格情報を置くと、だれも `user` でなくなり、設定の画面から戻せないから。
- GitHub App の ID か秘密鍵を変えるときは、置く前に、新しいもので、登録した repository を読めることを確かめる。

## repository を外したとき

- GitHub を使う構成では、外したあとも、外す本人が、残りの repository のどれかに Write を持つことを求める。そうでなければ `validation_failed` を返す。最後の 1 つは外せない。
  - なぜ：GitHub の `user` は、登録した repository の Write で決まるので、外すと、本人（最後の 1 つなら全員）が `user` でなくなり、設定の画面から戻せなくなるから（[締め出されたとき](../../settings/users.md#締め出されたとき)）。

- worker は、次に一覧を読み直したときから、その repository を見回らない。
- 置き場のスナップショットと `keep/` は消さない。
- 外した名前を登録し直せるのは、前と同じ `gitforge_ref` とディレクトリのときだけである。そのときは、続きから読む。
  - 違うものなら、`validation_failed` を返す。
  - なぜ：置き場は、repository の名前をキーにするから。別の git リポジトリを同じ名前にすると、履歴が混ざり、別の git リポジトリのスナップショットを返すことがあるから。
- そのため、外した repository の名前と `gitforge_ref` とディレクトリは、設定の DB に残す。
  - なぜ：誤って外したときに、作り直さずに戻せるようにするため。

## prepared

- `prepared` は、登録した git リポジトリに、木の最初のコミットがもうあるかである。
  - git リポジトリ全体の repository：`main` があるか。
  - ディレクトリの repository：`main` に、そのディレクトリがあるか。
- 要求した本人のトークンで、GitForge に聞く。
- 聞けなかったときは、`prepared` を返さない。登録と一覧は、失敗にしない。
  - なぜ：GitForge の一時的な失敗で、登録と一覧を止めないため。
- WebUI は、`prepared` で、最初の問いを入れる段が要るかを決める（[repository を足す](../../../docs/webui/settings.md#repository-を足す)）。
- なぜ worker のスナップショットで決めないか：登録した直後は、worker が、まだその repository を読んでいないから。

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../openapi.yaml) の各 operationId に書く。
