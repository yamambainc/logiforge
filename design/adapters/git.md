# git アダプタ

[木のリポジトリ](../backend/ports/tree-repository.md) のポートを、git でどう実装するかを確かめるときに読む。
ファイルの形式は、[木のファイル配置](../files/README.md) に従う。

## 2 つの実装

- GitHub のリモート：GitHub リポジトリを、手元のクローンと GitHub の API で操作する。
- ローカルの bare リポジトリ：サーバの上の bare の git リポジトリを、直接操作する。

## 操作と git の対応

| 操作 | git での実装 |
|---|---|
| 木を読む | `git ls-tree` と `git cat-file`（リビジョンを指定する） |
| 変更を列挙する | `git log <a>..<b> --first-parent -- <パス>` |
| 共通の祖先を求める | `git merge-base <a> <b>` |
| リビジョンの並びを読む | `git rev-list --first-parent --reverse main` |
| リビジョンを作る | `git commit-tree` で作り、`refs/logiforge/` の下の ref に置く |
| main を進める | 下の「main を進める」 |

## main を進める

- ローカルの bare リポジトリ：`git update-ref refs/heads/main <新しいコミット> <期待する親>`
  - 今の `main` が期待する親と違えば、`git update-ref` が失敗する。
- GitHub のリモート：`PATCH /repos/{owner}/{repo}/git/refs/heads/main` を、`force: false` で呼ぶ。
  - 新しいコミットの親は、期待する親である。
  - そのため、`main` が動いていれば fast-forward にならず、GitHub が拒む。

## 版のブランチ

- 版のコミットは、`refs/logiforge/apply-requests/<識別子>` に置く。
- GitHub のリモートでは、pull request の head ブランチにも、同じコミットを置く（[GitHub アダプタ](github.md)）。

## 制約

- Windows でクローンするときは、git の `core.longpaths` を有効にする必要がある（[深さの上限](../files/parent-child.md)）。
