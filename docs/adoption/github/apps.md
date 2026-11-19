# GitHub App

[Logiforge の repository を GitHub に置く](README.md) ときに、Logiforge の GitHub App を作り、install し、秘密を入れ直すために読む。
App の権限とトークンの正本は、[GitHub App とトークン](../../../design/adapters/github-app.md) である。

## 1 つの App

- Logiforge が GitHub に持つものは、1 つの GitHub App だけである。OAuth App と personal access token は使わない。
- App は、次の 2 つに使う。
  - worker とバックエンドが、repository を読む（installation token。Read だけ）。
  - 利用者が、WebUI でサインインし、本人の権限で書く（user access token）。
- なぜ 1 つにまとめるか：作り、install し、秘密を守るものを、1 つにするため。

## 作る

- App は、[セットアップ](../../webui/setup.md) の画面から、manifest flow で作る。GitHub の画面で手で作らない。
  - 権限、Webhook を使わないこと、callback の URL は、manifest に入っている（[初回のセットアップ](../../../design/settings/setup.md#github-を使う構成app-を-manifest-flow-で作る)）。
- App を作る owner（organization かアカウント）の admin の権限が要る（organization なら organization の owner）。
- 登録するリポジトリの owner が複数なら、セットアップの画面で「ほかの owner にも install する」を選ぶ。
  - なぜ：選ばない App は、App を作った owner にしか install できないから。
  - 選ぶと、App は public になり、だれでも自分のアカウントに install できる。Logiforge は、登録した owner の分しかトークンを作らない。
- App の ID、秘密鍵、client secret は、設定の DB に暗号化して置く。手で控える必要はない。

## install する

- App を、登録するリポジトリの owner ごとに install する。その owner の admin の権限が要る（organization なら organization の owner）。
- Repository access は、「Only select repositories」にし、Logiforge の repository を選ぶのを勧める。
  - repository を足したら、install の設定でも選び足す。選んでいないリポジトリは、worker が読めず、ログに出す。
  - App が install されていないリポジトリでは、Write があっても利用者にならない。
- 設定の画面で repository を足すとき、install されていない owner なら、画面が install の画面へのリンクを示す（[設定](../../webui/settings.md)）。

## 秘密を入れ直す

- App の client secret と秘密鍵は、[設定](../../webui/settings.md) の画面の「GitForge」で入れ直す。サーバは起動し直さない。
  1. GitHub の App の設定で、新しい client secret か秘密鍵を作る。
  2. 設定の画面に入れる。
  3. GitHub の App の設定で、古いものを消す。
