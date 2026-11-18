# セットアップと設定の画面と API

WebUI のセットアップと設定の画面が、BE のどの API を使うかを確かめるときに読む。
ほかの画面は、[画面と API](screens.md) に書く。

## 画面ごとの API

### [セットアップ](../../docs/webui/setup.md)

- 画面を出すか：`getSetup`。WebUI を開いたときに、サインインの前に呼ぶ（[セットアップが終わっていないとき](signin-flow.md#セットアップが終わっていないとき)）。
  - 終わっていなければ、どの URL を開いても `/setup` を出す。終わっていれば、`/setup` は repository の一覧を出す。
  - 応答の、進んだ段と選んだ構成から、どの段から続けるかを決める。
- コードの段：`checkSetupCode`。`unauthenticated` なら、コードが違うことを示す。
  - あとの段の呼び出しが `unauthenticated` を返したとき（サーバが起動し直してコードが変わったときなど）も、コードの段に戻す。
- 構成の段：`setSetupGitForge`。
- GitHub App の段：`createSetupGitHubManifest` で manifest と送り先を受け取り、フォームで GitHub に送る。
  - `/setup/github/callback` に戻ったら、`state` を照らし合わせ、`convertSetupGitHubManifest` に `code` を送る（[GitHub を使う構成](../settings/setup.md#github-を使う構成app-を-manifest-flow-で作る)）。交換の前に、`history.replaceState` で URL から `code` を消す。
- install の段：`convertSetupGitHubManifest` の応答の install の URL を開く。
- Forgejo の段：`setSetupForgejo`。`validation_failed` なら、`detail` を、その欄の隣に示す。
- サインインの段：`getOAuthConfig` と `exchangeOAuthCode` でサインインし、`finishSetup` を呼ぶ。
  - 終わったら、`/settings?tab=repository` を開く。
- どの呼び出しにも、`Logiforge-Setup-Code` を付ける（`getSetup` を除く）。`setup_done` が返ったら、repository の一覧を出す。

### [設定](../../docs/webui/settings.md)

- メニューに出すか：サインインしたあと、`getSettings` を呼ぶ。`forbidden` なら、メニューに出さない。
  - `/settings` を直接開いて `forbidden` なら、管理者でないことを示す。
- repository のタブ：`listRegisteredRepositories`、`registerRepository`、`unregisterRepository`。
  - `registerRepository` が `validation_failed` を返し、`detail` に install の URL があれば、そのリンクを示す。
- 利用者のタブ：`getSettings` の `user` の一覧を示し、`setUsers` で置き換える。
- 管理者のタブ：`listAdmins`、`addAdmin`、`removeAdmin`。管理者が 1 人なら、外すボタンを押せなくする。
- GitForge のタブ：`getSettings` と `updateGitForgeSettings`。秘密は、置いたかどうかだけを示し、入れたときだけ送る。
- 記録の鍵のタブ：`listRecordKeys`、`createRecordKey`。
- 最後に変えた管理者と時刻は、各応答の `updated_by` と `updated_at` を示す。
