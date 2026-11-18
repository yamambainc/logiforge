# インストール

logiforge のバイナリを取り、動かすときに読む。

## バイナリ

logiforge は、1 つのバイナリである。次のものが入っている。

- CLI
- バックエンドの `serve`
- `worker`
- WebUI（`serve` が配る）

動かす OS は、次のとおり。

- CLI（opener と reviewer が使う）は、Linux・macOS・Windows で動く。
- `serve` と `worker` は、Linux で動かす。
  - コンテナで動かす：[compose で動かす](development/compose.md)、[動かし方（コンテナ）](../design/deploy/README.md)
  - systemd のサービスで動かす：[1 台の Linux に置く](adoption/linux-host/README.md)
- ほかの OS で `serve` と `worker` を動かすことは、前提としない。

## 取り方

[GitHub Releases](https://github.com/yamambainc/logiforge/releases) に、Linux・macOS・Windows × amd64・arm64 の版を置く。

- この repository は private である。取るには、yamambainc/logiforge を読む権限のある GitHub のアカウントが要る。
- [gh](https://cli.github.com/) で取る。
  - 先に `gh auth login` で、読む権限のあるアカウントにサインインしておく。

```sh
# 最新の版。Linux の amd64 の例（ほかは darwin・windows、arm64）
gh release download -R yamambainc/logiforge --pattern 'logiforge_*_linux_amd64.tar.gz' --pattern checksums.txt
sha256sum --ignore-missing -c checksums.txt
tar xzf logiforge_*_linux_amd64.tar.gz logiforge
./logiforge version

# 版を決めて取る
gh release download v0.3.0 -R yamambainc/logiforge --pattern 'logiforge_*_darwin_arm64.tar.gz' --pattern checksums.txt
```

- Windows の版は zip である。
- macOS では、`sha256sum` のかわりに `shasum -a 256 --ignore-missing -c checksums.txt` を使う。
- `logiforge version` は、版、コミット、ビルドした日付を示す。
  - `--json` を付けると、JSON で示す。

## macOS の隔離

- macOS の版のバイナリには、署名と公証をしていない。
- ブラウザで取ったファイルは、初めて動かすときに macOS に止められる。
- チェックサムを確かめたうえで、隔離の印を外す。

```sh
xattr -d com.apple.quarantine ./logiforge
```

## worker のロック

- `logiforge worker` は、次の 2 つのディレクトリに、`.worker.lock` のロックを取る。
  - clone のディレクトリ
  - 置き場の根
- unix では、worker が止まればロックは外れる。
- unix でない環境では、ロックはファイルである。worker が落ちると、ファイルが残る。
  - そのときは、ほかの worker が動いていないことを確かめてから、2 つのディレクトリの `.worker.lock` を手で消す。
