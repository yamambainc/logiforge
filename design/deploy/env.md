# 環境変数

バックエンドと worker を起動するときに渡す環境変数を確かめるときに読む。
起動時に渡すものと、設定の DB に置くものの分け方は、[設定](../settings/README.md#起動時に渡すもの) に書く。

## 起動時に渡すもの

両方の構成（GitHub を使う構成と Forgejo を使う構成）で同じである。
「渡す先」の「両方」は、バックエンド（`logiforge serve`）と worker（`logiforge worker`）である。

| 名前 | 渡す先 | 秘密 | 中身 |
|---|---|---|---|
| `LOGIFORGE_DB` | 両方 | | 設定の DB のファイルのパス（[設定の DB](../settings/database.md)） |
| `LOGIFORGE_BACKEND_KEY` | バックエンド | ✓ | バックエンドの鍵（[秘密の暗号化](../settings/secrets.md#2-つの鍵)） |
| `LOGIFORGE_SHARED_KEY` | 両方 | ✓ | 共有の鍵（[秘密の暗号化](../settings/secrets.md#2-つの鍵)） |
| `LOGIFORGE_ADDR` | バックエンド | | 待ち受けるアドレス。既定は `127.0.0.1:8080`。コンテナでは `0.0.0.0:8080` にする |
| `LOGIFORGE_STORE_DIR` | 両方 | | 置き場（手元のディレクトリ）のパス。worker は読み書きし、バックエンドは読むだけである |
| `LOGIFORGE_WORKER_DIR` | worker | | clone を置く、永続のボリュームのパス |
| `LOGIFORGE_GIT_CACHE_DIR` | バックエンド | | 作業用の git の複製を置く、一時的なディレクトリ。既定は OS の一時ディレクトリ（`/tmp`）。プロセスごとに別の場所にする（[例：Kubernetes の Pod](kubernetes.md)） |
| `LOGIFORGE_GIT_ALLOW_PROTOCOL` | 両方 | | git で使える転送の方式。既定は `https:file`。`http` は、テストのときだけ足す |

- 秘密は、名前に `_FILE` を付けて、ファイルで渡す（例：`LOGIFORGE_SHARED_KEY_FILE=/run/secrets/shared_key`）。
  - なぜ：秘密を、コンテナの定義や `docker inspect` の出力に書かずに済むから。
  - 鍵は、設定の DB と別の場所に置く（[鍵を置く場所](../settings/secrets.md#鍵を置く場所)）。
- 置き場を静的ファイルストレージにするときは、`LOGIFORGE_STORE_DIR` のかわりに、ストレージの接続先と鍵を起動時に渡す（[スナップショットの置き場のアダプタ](../adapters/snapshot-store.md#設定)）。
- worker には、`LOGIFORGE_BACKEND_KEY` を渡さない（[秘密の暗号化](../settings/secrets.md#2-つの鍵)）。
- `logiforge worker-health` には、worker と同じ環境変数を渡す（[ハートビート](../backend/worker/operation.md#ハートビート)）。
- `logiforge settings load` と `logiforge settings rekey` には、バックエンドと同じ `LOGIFORGE_DB` と 2 つの鍵を渡す（[設定のコマンド](../settings/README.md#設定のコマンド)）。

## 設定の DB に置くもの

上の表のほかの設定は、環境変数では渡さない。設定の DB に置き、利用者が設定の画面で変える。

- 何を置くかは、[設定の DB に置くもの](../settings/README.md#設定の-db-に置くもの) に書く。
