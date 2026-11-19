# セットアップと設定の画面と API

WebUI のセットアップと設定の画面が、BE のどの API を使うかを確かめるときに読む。
ほかの画面は、[画面と API](screens.md) に書く。

## 画面ごとの API

### [セットアップ](../../docs/webui/setup.md)

- 画面を出すか：`getSetup`。WebUI を開いたときに、サインインの前に呼ぶ（[セットアップが終わっていないとき](signin-flow.md#セットアップが終わっていないとき)）。
  - 終わっていなければ、どの URL を開いても `/setup` を出す。終わっていれば、`/setup` は repository の一覧を出す。
  - 応答の、進んだ段と選んだ構成から、どの段から続けるかを決める。
- 構成の段：`setSetupGitForge`。
- GitHub App の段：`createSetupGitHubManifest` で manifest と送り先を受け取り、フォームで GitHub に送る。
  - `/setup/github/callback` に戻ったら、`state` を照らし合わせ、`convertSetupGitHubManifest` に `code` を送る（[GitHub を使う構成](../settings/setup.md#github-を使う構成app-を-manifest-flow-で作る)）。交換の前に、`history.replaceState` で URL から `code` を消す。
- install の段：`convertSetupGitHubManifest` の応答の install の URL を開く。
- repository の段：`setSetupRepository`。`validation_failed` なら、App が install されていないことと、install の URL を示す。
- Forgejo の段：`setSetupForgejo`。`validation_failed` なら、`detail` を、その欄の隣に示す。
- サインインの段：`getOAuthConfig` と `exchangeOAuthCode` でサインインし、`finishSetup` を呼ぶ。
  - 終わったら、`/settings?tab=repository` を開く。
- どの呼び出しでも、`setup_done` が返ったら、repository の一覧を出す。
- 構成の段の応答の `setup_token` を `localStorage` に置き、以後の呼び出しに `Logiforge-Setup-Token` で付ける（[だれがセットアップできるか](../settings/setup.md#だれがセットアップできるか)）。
  - `getSetup` の `started` が `true` なのにトークンを持っていないとき、または `setup_in_progress` が返ったときは、段のかわりに「セットアップは別のブラウザで進行中です。最初からやり直すときは、設定の DB を消して起動し直してください」と示す。
  - セットアップが終わったら、トークンを消す。

### [設定](../../docs/webui/settings.md)

- メニューに出すか：サインインしたあと、`getSettings` を呼ぶ。`forbidden` なら、メニューに出さない。
  - `/settings` を直接開いて `forbidden` なら、利用者でないことを示す。
- repository のタブ：`listRegisteredRepositories`、`registerRepository`、`unregisterRepository`。
  - `registerRepository` が `validation_failed` を返し、`detail` に install の URL があれば、そのリンクを示す。
- GitForge のタブ：`getSettings` と `updateGitForgeSettings`。秘密は、置いたかどうかだけを示し、入れたときだけ送る。
- 記録の鍵のタブ：`listRecordKeys`、`createRecordKey`。
- 最後に変えた利用者と時刻は、各応答の `updated_by` と `updated_at` を示す。
