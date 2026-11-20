# 設定と鍵を置く

[1 台の Linux に置く](README.md) で、サービスが起動時に読むファイルと鍵を置くときに読む。
ほかの設定と秘密（GitHub App の秘密鍵と client secret など）は、設定の DB に暗号化して置く。WebUI のセットアップの画面で入れる（[秘密の暗号化](../../../design/settings/secrets.md)）。

## ファイル

| ファイル | 持ち主と権限 | 中身 |
|---|---|---|
| `/etc/logiforge/logiforge.env` | root、`0644` | 設定の DB の場所と、置き場と clone のディレクトリ。見本は [logiforge.env.example](../../../deploy/systemd/logiforge.env.example) |
| `/etc/logiforge/secrets/backend_key` | root、`0600` | バックエンドの鍵。バックエンドだけが読む |
| `/etc/logiforge/secrets/shared_key` | root、`0600` | 共有の鍵。バックエンドと worker が読む |

- 鍵は、32 バイトの乱数を base64 にしたものである。2 つは別の乱数にする（[2 つの鍵](../../../design/settings/secrets.md#2-つの鍵)）。

## 置き方

```sh
sudo install -d -m 0700 /etc/logiforge/secrets
sudo install -m 0644 deploy/systemd/logiforge.env.example /etc/logiforge/logiforge.env   # 開いて、パスを確かめる
sudo sh -c 'umask 077; head -c 32 /dev/urandom | base64 > /etc/logiforge/secrets/backend_key'
sudo sh -c 'umask 077; head -c 32 /dev/urandom | base64 > /etc/logiforge/secrets/shared_key'
```

- 2 つの鍵は、マシンの外にも控える（[バックアップ](README.md#バックアップ)）。設定の DB と同じ場所には控えない。

## 鍵の渡し方

- 鍵は、ユニットの `LoadCredential=` で渡し、`_FILE` の環境変数でそのパスを指す。
  - なぜ `EnvironmentFile=` でないか：鍵が、プロセスの環境変数になり、子の git にも渡るから。`_FILE` で渡す方針は、[動かし方](../../../design/deploy/README.md#イメージ) と同じである。
  - なぜ `LoadCredential=` か：`logiforge` のユーザーは `/etc/logiforge/secrets/` を読めないまま、サービスごとに要る鍵だけを受け取れるから。worker は、バックエンドの鍵を受け取らない。
- `logiforge.env` は、2 つのサービスが同じものを読む。設定の DB の場所を、両方で同じにするためである。
