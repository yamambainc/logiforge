# repository の用意のコミット

worker が、`main` のコミットを repository の用意のコミットと決める条件と、その署名の形を確かめるときに読む。
判定の全体は [反映かの判定](README.md) に書く。

## repository の用意のコミット

`main` のコミット C（1 つ前の `main` のコミットを P とする）は、次のどれかなら、用意のコミットである。

- C が根のコミットで、trailer `Logiforge-Init:` と `Logiforge-Init-Signature:` があり、種類 `init-root` の署名が合う（下の「用意のコミットの署名」）。
  - なぜ根も署名で決めるか（位置で決めないか）：GitHub では、管理者が空のリポジトリを作って Write を与えてから、利用者が用意するまでのあいだに、Write の主体が、`Logiforge-Init:` を付けた好きな中身の根を push できるから。Forgejo でも同じ決まりにする。
- C が根の次のコミットで、trailer `Logiforge-Init-Signature:` があり、種類 `init-second` の署名が合う。
  - なぜ：GitHub の空のリポジトリには、ファイルを 1 つずつしか置けないので、背景情報を 2 つ目のコミットで置くから（[GitHub アダプタ](../../adapters/github.md)）。
- ディレクトリの repository では、そのディレクトリの[版](../../directory/history.md#版)の 1 番で、種類 `init-directory` の署名が合うもの（[ディレクトリ方式の履歴](../../directory/history.md) の「用意のコミット」）。

- どれでも、C の差分が、ファイルを加えることだけであることも確かめる。
- 根のコミットが用意のコミットでなければ、worker は、その repository を取り込まない。
  - なぜ：最後に正しかった時点がないから。
  - 管理者が、repository を作り直す。

## 用意のコミットの署名

用意のコミットの trailer `Logiforge-Init-Signature:` は、どの種類でも、1 つの形で作り、確かめる。

- 値は、`<鍵の ID>:<署名>` である。
- 署名は、バックエンドが記録と同じ秘密鍵（Ed25519）で作り、worker は公開鍵で確かめる。
- 署名する中身は、次の欄を順に並べたものである。どの欄も、長さを前に付ける。
  - なぜ長さを付けるか：空の欄が多いので、区切りだけで並べると、欄がずれても同じバイト列になりうるから。

| 欄 | 中身 |
|---|---|
| 種類 | 下に書く 3 つのどれか |
| repository の識別子 | 記録の署名の場所と同じもの（Forgejo の id、GitHub の node_id） |
| ディレクトリ | ディレクトリの repository のパス。git リポジトリ全体の repository では空 |
| 1 つ前のコミット | `init-second` では P の SHA。`init-root` と `init-directory` では空（[ディレクトリ方式の履歴](../../directory/history.md) の「用意のコミット」） |
| 移した元 | `Logiforge-Migrated-From:` の値。移したときだけ。ほかは空 |
| ファイル | C で加わったファイルの、パスと sha256（パスの順） |

- 種類は、次の 3 つである。
  - `init-root`：根のコミット
  - `init-second`：GitHub で、根の次に背景情報を置くコミット
  - `init-directory`：ディレクトリの repository の用意のコミット
- なぜ種類を含めるか：ある種類の署名を、ほかの種類の用意のコミットに使い回せないようにするため。
