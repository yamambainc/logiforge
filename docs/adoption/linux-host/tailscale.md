# Tailscale で公開する

[1 台の Linux に置く](README.md) で動かしたバックエンドを、tailnet の中のブラウザと CLI に公開するときに読む。

## tailscale serve

1. Tailscale の管理画面の「DNS」で、MagicDNS と HTTPS Certificates を有効にする。
   - マシンの名前は、証明書の公開の記録（Certificate Transparency）に載る。名前に、知られたくないことを入れない。
2. マシンで、`sudo tailscale serve --bg 8080` を動かす。`https://<host>.<tailnet>.ts.net` が、`http://127.0.0.1:8080` につながる。
   - `--bg` の設定は、再起動のあとも残る。やめるときは `sudo tailscale serve --https=443 off`。
3. tailnet のだれが届くかは、Tailscale のアクセス制御で絞る。

## OAuth App

- Authorization callback URL と `LOGIFORGE_OAUTH_REDIRECT_URI` を、`https://<host>.<tailnet>.ts.net/oauth/callback` にする。
- GitHub は、この URL に自分からつながらない。ブラウザを、この URL にリダイレクトするだけである。そのため、tailnet の中のブラウザなら、サインインできる。
  - code をトークンに換えるのは、バックエンドから `github.com` への外向きの要求である（[サインインの中継](../../../design/webui/oauth-relay.md)）。
- WebUI は、`https://` の名前で開く。tailnet の IP の `http://` で開くと、ブラウザが PKCE の計算（Web Crypto）を許さず、サインインできない。
- Client ID か client secret が正しくないと、バックエンドは主体のトークンを確かめられず、API が `internal`（500）を返す。ログに `github: 401 Bad credentials` が出たら、この 2 つを見直す。

## CLI とエージェント

- CLI を使うマシンも、tailnet に入れる。エージェントのマシンやコンテナも同じである（例：タグを付けた auth key で `tailscale up`）。

```sh
gh auth token | logiforge auth login --with-token --host https://<host>.<tailnet>.ts.net
```

- トークンの選び方は、[利用者とエージェント](../github/README.md#利用者とエージェント) に従う。
