# 初回のセットアップ

設定の DB が空のときに、最初の設定と最初の利用者を決める流れを確かめるときに読む。
画面は、[セットアップの画面](../../docs/webui/setup.md) に書く。API は、[セットアップの API](../api/endpoints/setup.md) に書く。

## だれがセットアップできるか

- セットアップが終わるまでは、だれでも `/setup` を開き、最初の段から始められる。使う人が入れるものはない。
- 続けられるのは、セットアップを始めたブラウザだけである。
  - 最初の段（構成を選ぶ `setSetupGitForge`）で、バックエンドは乱数のトークンを作り、ブラウザに返す。設定の DB の `setup` には、トークンの SHA-256 だけを置く。
  - ブラウザは、トークンを `localStorage` に置き、以後のセットアップの要求に `Logiforge-Setup-Token` ヘッダーで付ける。そのため、途中でページを閉じても、同じブラウザなら続きから進められる。
  - トークンがないか違う要求は、`setup_in_progress`（409）で断る。画面は「セットアップは別のブラウザで進行中です。最初からやり直すときは、設定の DB を消して起動し直してください」と示す。
  - なぜ：トークンがないと、始めた人が App を作ってから最後の段に進むまでのあいだに、ほかの人が、自分の App の code で `convertSetupGitHubManifest` を呼び、設定の App を差し替えられるから。差し替わった App を install すると、ほかの人が、その GitHub リポジトリに書ける。Forgejo の構成でも、`setSetupForgejo` で、つなぎ方を差し替えられる。
- 最後の段でサインインした人が、最初の利用者になる（[流れ](#流れ) の 5）。
- セットアップが終わったら、トークンを消す。セットアップの API は `setup_done`（409）を返す。画面は使えなくなる。

### 受け入れる危険

- ほかの人が、先にセットアップを始めることがある。そのときは、始めた人だけが続けられ、ほかの人は続けられない。
- そうなったら、設定の DB を消し、バックエンドを起動し直して、セットアップをやり直す。
- なぜ受け入れるか：セットアップの前は、サーバにデータがなく、失うものがないから。始めた人を、入れてもらうコードで確かめるのは、守るものに比べて重すぎる。

## 流れ

1. サーバを起動する人が、設定の DB の場所と 2 つの鍵だけを渡して、バックエンドと worker を起動する。
2. WebUI のセットアップの画面で、構成（GitHub か Forgejo）を選び、GitForge の設定を入れる（下の 2 つの節）。
3. GitHub の構成では、最初の repository を登録する（名前と `owner/name`。ディレクトリの repository ならパスも。[repository の登録](../adapters/README.md#repository-の登録)）。
4. その GitForge でサインインする（[サインイン](../webui/auth.md)）。
5. バックエンドは、サインインした主体が `user` であることを確かめる（[利用者の決め方](users.md#利用者の決め方)）。セットアップを終わりにする。
   - GitHub の構成では、3 の repository に Write 以上がなければ、断る（`forbidden`）。
   - Forgejo の構成では、organization の team で `write` 以上がなければ、断る。
6. 利用者が、[設定の画面](../../docs/webui/settings.md) で、ほかの repository を登録する。

- 3 までに入れた設定は、そのつど設定の DB に置く。途中でページを閉じても、バックエンドを起動し直しても、続きから進める。

## GitHub を使う構成：App を manifest flow で作る

```mermaid
sequenceDiagram
  autonumber
  participant B as ブラウザ（WebUI）
  participant BE as BE
  participant G as GitHub
  B->>BE: manifest を求める
  BE-->>B: manifest と送り先
  B->>G: manifest の送信
  G-->>B: code
  B->>BE: code
  BE->>G: conversions
  G-->>BE: App の ID と鍵
  B->>G: install
```

1. App を作る owner（organization かアカウント）と、WebUI の URL を入れる。
2. BE は、manifest と、送り先の URL を返す。
   - organization なら `https://github.com/organizations/<org>/settings/apps/new`、アカウントなら `https://github.com/settings/apps/new` である。
3. ブラウザは、manifest をフォームで GitHub に送る。GitHub の画面で App を作る。
   - App を作る owner の admin の権限が要る（organization なら organization の owner）。
   - 送り先の URL に `state` を付ける。`state` は、ブラウザが作る乱数で、`sessionStorage` に置く。
4. GitHub は、manifest の `redirect_url`（WebUI の `/setup/github/callback`）に、`code` と `state` を付けて戻す。
   - ブラウザは、`state` が置いたものと合わなければ、`code` を BE に送らない。
5. ブラウザは、`code` を BE に送る。
6. BE は、`POST /app-manifests/{code}/conversions` を呼び、App の ID、slug、client_id、client secret、秘密鍵を、設定の DB に置く。
   - code は 1 時間で切れる。切れたら、2 からやり直す。
   - webhook の secret は、置かない。Webhook を使わないから。
7. App を、登録する GitHub リポジトリの owner に install する。
   - install する owner の admin の権限が要る（organization なら organization の owner）。

manifest の中身：

- 権限は、[GitHub App](../adapters/github-app.md#github-app) の表のとおりにする。
- `default_events` は空にする。Webhook は使わない。
- Webhook の設定（`hook_attributes`）は manifest に入れない。
  - なぜ：GitHub は、`active` が `false` でも hook の URL を確かめ、`127.0.0.1` のようなインターネットから届かない URL を断るから。
- `callback_urls` は、WebUI の `/oauth/callback` にする（[サインインの流れ](../webui/signin-flow.md)）。
- `public` は、ほかの owner にも install するときだけ `true` にする。画面で選ぶ。
  - なぜ：`public` が `false` の App は、App を作った owner にしか install できないから。

## Forgejo を使う構成

1. Forgejo の URL（BE から、ブラウザから）と organization を入れる。
2. Forgejo で OAuth2 のアプリケーション（confidential client）を作り、client_id と client secret を入れる。
   - 画面は、登録する `redirect_uri`（WebUI の `/oauth/callback`）を示す。
3. worker のユーザーとトークン（`read:repository`、`read:organization`、`read:issue`）を入れる。worker のユーザーは、organization の読むだけの team にだけ入れる（[worker のトークンを確かめる](../adapters/forgejo.md#worker-のトークンを確かめる)）。
4. BE は、置く前に、トークンでそのユーザーと organization を読めることを確かめる。読めなければ `validation_failed` を返す。
