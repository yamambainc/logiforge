# 1 台の Linux に置く

GitHub を使う構成の Logiforge を、1 台の Linux のマシンで動かし、tailnet の中の人とエージェントだけに公開するときに読む。
[Logiforge の repository を GitHub に置く](../github.md) の「Logiforge を動かす」の、本番の手順である。

- 形は、[動かし方（コンテナ）](../../../design/deploy.md) の「タスクの形」と同じである。コンテナのかわりに、systemd のサービスを 2 つ動かす。
- 環境変数の正本は、その「環境変数と秘密」である。このページには、置き場所と渡し方だけを書く。

## 構成

```mermaid
flowchart LR
  B["ブラウザと CLI（tailnet の中）"]
  subgraph H["Linux のマシン"]
    TS["tailscale serve"]
    S["logiforge-serve"]
    W["logiforge-worker"]
    ST[("/var/lib/logiforge/store")]
    WD[("/var/lib/logiforge/worker")]
  end
  GH["GitHub"]
  B -->|https| TS --> S
  S -->|読む| ST
  W -->|書く| ST
  W --> WD
  S --> GH
  W --> GH
```

- `logiforge-serve` は、`127.0.0.1:8080` だけで待ち受ける。外から届くのは、`tailscale serve` を通した `https://<host>.<tailnet>.ts.net` だけである（[Tailscale で公開する](tailscale.md)）。
  - 証明書は、Tailscale が出す。tailnet の外からは届かない（外に出すのは `tailscale funnel` で、このページでは使わない）。
- マシンからは、`github.com` と `api.github.com` に、外向きに届けばよい。受け付けるポートは要らない。
- 要るもの：systemd 251 以上（ユニットが、資格情報のディレクトリを `%d` で指すため。Debian 12、Ubuntu 24.04、RHEL 9 は満たす）、git、Tailscale。

## 入れる

1. Releases から linux のバイナリを取り、`/usr/local/bin/logiforge` に置く（取り方は [README](../../../README.md#インストール)）。GitHub を使う構成は v0.2.0 からである。
2. git を入れる（例：`apt install git`）。worker とバックエンドが使う。
3. 専用のユーザーを作る。ログインはさせない。

```sh
sudo install -m 0755 logiforge /usr/local/bin/logiforge
sudo useradd --system --home-dir /var/lib/logiforge --no-create-home --shell /usr/sbin/nologin logiforge
```

## 設定と秘密

| ファイル | 持ち主と権限 | 中身 |
|---|---|---|
| `/etc/logiforge/logiforge.env` | root、`0644` | 秘密でない設定。見本は [logiforge.env.example](../../../deploy/systemd/logiforge.env.example) |
| `/etc/logiforge/secrets/read_token` | root、`0600` | 読むだけのトークン（[用意するもの](../github.md#用意するもの)） |
| `/etc/logiforge/secrets/record_keys` | root、`0600` | 署名の秘密（`k1=<秘密>`）。バックエンドだけが読む |
| `/etc/logiforge/secrets/oauth_client_secret` | root、`0600` | OAuth App の client secret |

```sh
sudo install -d -m 0700 /etc/logiforge/secrets
sudo install -m 0644 deploy/systemd/logiforge.env.example /etc/logiforge/logiforge.env   # 開いて、値を書き換える
# 秘密は、標準入力から貼る（シェルの履歴に残さない）
sudo sh -c 'umask 077; cat > /etc/logiforge/secrets/read_token'
sudo sh -c 'umask 077; cat > /etc/logiforge/secrets/oauth_client_secret'
sudo sh -c 'umask 077; echo "k1=$(openssl rand -hex 20)" > /etc/logiforge/secrets/record_keys'
sudo env LOGIFORGE_RECORD_KEYS_FILE=/etc/logiforge/secrets/record_keys logiforge record-public-keys
# 出力を、logiforge.env の LOGIFORGE_RECORD_PUBLIC_KEYS に書く
```

- 秘密は、ユニットの `LoadCredential=` で渡し、`_FILE` の環境変数でそのパスを指す。
  - なぜ `EnvironmentFile=` でないか：秘密が、プロセスの環境変数になり、子の git にも渡るから。`_FILE` で渡す方針は、[動かし方](../../../design/deploy.md#イメージ) と同じである。
  - なぜ `LoadCredential=` か：`logiforge` のユーザーは `/etc/logiforge/secrets/` を読めないまま、サービスごとに要る秘密だけを受け取れるから。worker は、署名の秘密を受け取らない。
- 秘密でない設定は、2 つのサービスが同じ `logiforge.env` を読む。`LOGIFORGE_GITHUB_REPOSITORIES` などを、両方で同じ値にするためである。
- 見回りの間隔は `10s`、記録を読む間隔は `30s` にする（[GitHub アダプタ](../../../design/adapters/github.md) の「見回りの間隔」）。

## サービス

ユニットの見本は [deploy/systemd/](../../../deploy/systemd/) にある。

```sh
sudo cp deploy/systemd/logiforge-*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now logiforge-worker logiforge-serve
```

- 書ける場所は、ユニットごとに絞る。ほかは読むだけである（`ProtectSystem=strict`）。
  - worker：`/var/lib/logiforge/worker`（clone）と `/var/lib/logiforge/store`（置き場）
  - バックエンド：`/var/cache/logiforge-serve`（作業用の git の複製）。置き場は読むだけである。
- root では動かさない。capability は持たせない。
- 落ちたら、5 秒あとに起動し直す（`Restart=on-failure`）。
- worker は、1 つのユニットだけで動かす。2 つ目の worker は、clone のディレクトリのロックが取れずに止まる（[worker を 1 つだけ動かす](../../../design/backend/worker.md#worker-を-1-つだけ動かす)）。
- systemd は、ハートビートを見ない。プロセスが落ちたときだけ、起動し直す。

## 確かめる

```sh
curl -fsS http://127.0.0.1:8080/healthz
sudo systemd-run --pipe --wait -p User=logiforge -p EnvironmentFile=/etc/logiforge/logiforge.env /usr/local/bin/logiforge worker-health
journalctl -u logiforge-worker -u logiforge-serve
```

- `worker-health` は、ハートビートが見回りの間隔の 10 倍より古いと、0 以外で終わる（[ヘルスチェック](../../../design/deploy.md#ヘルスチェック)）。

## 更新

```sh
sudo install -m 0755 logiforge /usr/local/bin/logiforge
sudo systemctl restart logiforge-worker logiforge-serve
```

- `restart` は、古いプロセスを止めてから、新しいプロセスを起こす。そのため、worker が 2 つ同時に動くことはない。

## バックアップ

- 置き場の `/var/lib/logiforge/store/keep/` を、写して取る。ブランチの履歴と記録の控えは、git から作り直せないからである（[スナップショットの置き場](../../../design/backend/ports/snapshot-store.md)）。
- ほかの場所は、取らなくてよい。
  - `/var/lib/logiforge/worker` の clone と、置き場の `keep/` の外は、worker が作り直す。
  - `/var/cache/logiforge-serve` は、使い捨ての複製である。
- `/etc/logiforge/secrets/record_keys` は、マシンの外にも控える。この秘密は変えないからである（[Logiforge を動かす](../github.md#logiforge-を動かす)）。
