# 更新

Logiforge を 1 台の Linux に置いた構成（[1 台の Linux に置く](README.md)）で、バイナリを新しくするときに読む。

更新のやり方は 2 つある。どちらも、バイナリを入れ替えて、serve と worker を起動し直す。

- `restart` は、古いプロセスを止めてから、新しいプロセスを起こす。そのため、worker が 2 つ同時に動くことはない。

## Releases のバイナリに入れ替える

Releases から新しい linux のバイナリを取り（[インストール](../../install.md)）、サーバで次を行う。

```sh
sudo install -m 0755 logiforge /usr/local/bin/logiforge
sudo systemctl restart logiforge-worker logiforge-serve
```

## 手元でビルドして入れ替える

リリースの前のコミットを入れるときは、手元の checkout から [scripts/deploy-linux.sh](../../../scripts/deploy-linux.sh) で入れ替える。

```sh
git switch main && git pull
scripts/deploy-linux.sh admin@logiforge-host
```

- 引数は、`ssh` の送り先である。`~/.ssh/config` の名前も使える。
- 手元に要るもの：Go、Node.js（[開発](../../development/README.md)）、`ssh` と `scp`。
- 送り先に要るもの：`sudo` を使えるアカウント。パスワードを聞かれたら入れる。
- スクリプトは、次の順に行う。
  1. 今のブランチの、今のコミットでビルドする。git の switch や pull はしない。ビルドするコミットと版を、最初に示す。
  2. 送り先の CPU（amd64 か arm64）に合わせて、WebUI を入れたバイナリを作る。版とコミットは、リリースと同じく埋め込む（`logiforge version` で見える）。
  3. 送り先に送り、`/usr/local/bin/logiforge` を入れ替えて、serve と worker を起動し直す。
  4. 3 秒待って、serve と worker が動いているかを確かめる。動いていなければ、ログの見方を示し、0 以外で終わる。
- 手元と送り先の一時ファイルは、成功しても失敗しても消す。
- コミットしていない変更も、ビルドに入る。そのときは、注意を示す。
