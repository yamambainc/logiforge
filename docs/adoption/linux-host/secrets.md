# 設定と秘密を置く

[1 台の Linux に置く](README.md) で、サービスが読む設定のファイルと秘密を置くときに読む。

## ファイル

| ファイル | 持ち主と権限 | 中身 |
|---|---|---|
| `/etc/logiforge/logiforge.env` | root、`0644` | 秘密でない設定。見本は [logiforge.env.example](../../../deploy/systemd/logiforge.env.example) |
| `/etc/logiforge/secrets/github_app_key.pem` | root、`0600` | 読むだけの GitHub App の秘密鍵（[読むだけの GitHub App](../github/apps.md#読むだけの-github-app)） |
| `/etc/logiforge/secrets/record_keys` | root、`0600` | 署名の秘密（`k1=<秘密>`）。バックエンドだけが読む |
| `/etc/logiforge/secrets/oauth_client_secret` | root、`0600` | OAuth App の client secret |

## 置き方

```sh
sudo install -d -m 0700 /etc/logiforge/secrets
sudo install -m 0644 deploy/systemd/logiforge.env.example /etc/logiforge/logiforge.env   # 開いて、値を書き換える
sudo install -m 0600 <ダウンロードした>.private-key.pem /etc/logiforge/secrets/github_app_key.pem   # 置いたら、ダウンロードしたものは消す
# 秘密は、標準入力から貼る（シェルの履歴に残さない）
sudo sh -c 'umask 077; cat > /etc/logiforge/secrets/oauth_client_secret'
```

## 秘密の渡し方

- 秘密は、ユニットの `LoadCredential=` で渡し、`_FILE` の環境変数でそのパスを指す。
  - なぜ `EnvironmentFile=` でないか：秘密が、プロセスの環境変数になり、子の git にも渡るから。`_FILE` で渡す方針は、[動かし方](../../../design/deploy/README.md#イメージ) と同じである。
  - なぜ `LoadCredential=` か：`logiforge` のユーザーは `/etc/logiforge/secrets/` を読めないまま、サービスごとに要る秘密だけを受け取れるから。worker は、署名の秘密を受け取らない。
- 秘密でない設定は、2 つのサービスが同じ `logiforge.env` を読む。`LOGIFORGE_GITHUB_REPOSITORIES` などを、両方で同じ値にするためである。
