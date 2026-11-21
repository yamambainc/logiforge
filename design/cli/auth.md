# CLI の認証

CLI がトークンをどう得て、どこに置き、バックエンドの API と git にどう渡すかを確かめるときに読む。
CLI の全体は、[CLI](README.md) に書く。

## トークン

- CLI は、GitForge のトークンを、バックエンドの API と git の両方で使う。

### Forgejo のスコープ

- Forgejo を使う構成では、トークンのスコープを `read:user`、`read:organization`、`write:repository`、`write:issue` にする。
  - `read:user` と `read:organization`：バックエンドが、`GET /user` と `GET /user/teams` で主体を確かめるため（[Forgejo アダプタの認証](../adapters/forgejo.md#認証)）。
  - `write:repository`：git でブランチを push し、バックエンドが pull request を作り、レビューを書き、merge するため。
    - バックエンドは、主体のトークンで GitForge に書く（[GitForge のポートを pull request と issue で実装する](../adapters/pull-requests.md)）。
  - `write:issue`：バックエンドが、人への依頼の issue、pull request と issue のコメントとラベルを、主体のトークンで書くため。
- このトークンは、利用者が Forgejo で書けるすべての repository に書ける。この範囲の広さは受け入れる。
  - なぜ：Forgejo のスコープは、repository ごとに絞れないから。
  - 範囲を絞りたいときは、Logiforge のためだけの Forgejo のユーザーを作り、そのトークンを使う。

### GitHub のトークン

- GitHub を使う構成では、CLI の主体は `client` になる（[GitHub アダプタの認証](../adapters/github.md#認証)）。
- 人もエージェント（機械用アカウント）も、`gh auth login`（OAuth）で得たトークンを使う（`gh auth token`）。
  - personal access token は使わない。
- スコープは `repo` が要る。
  - なぜ：バックエンドが、主体のトークンで、pull request、review、コメント、ラベルを書き、reviewer のトークンで merge するから（[GitForge のポートを pull request と issue で実装する](../adapters/pull-requests.md)）。
  - git の push も、同じトークンで行う。
- `repo` は、そのアカウントが書けるすべてのリポジトリに及ぶ。
  - 範囲を絞りたいエージェントは、Logiforge のためだけの機械用アカウントにする（[GitHub App とトークン](../adapters/github-app.md#アカウントとトークン)）。

## 置き場所

- トークンの置き場所は、次の順に探す。
  1. 環境変数 `LOGIFORGE_TOKEN`
  2. 設定ファイル（`~/.config/logiforge/config.yaml`）の `token`
- `logiforge auth login --with-token` は、トークンを標準入力から読んで、設定ファイルに書く。
  - 設定ファイルは、本人だけが読めるようにする。
  - `--token <トークン>` でも渡せる。ただし、コマンドの履歴に残るので、`--with-token` を勧める。
- バックエンドの URL は、環境変数 `LOGIFORGE_HOST` か、設定ファイルの `host` に置く。

## git の credential helper

- git には、CLI の credential helper で同じトークンを渡す（`logiforge auth setup-git`）。
- GitForge には、git の HTTP の Basic 認証のパスワードにトークンを入れて、読み書きできることを求める（[アダプタ](../adapters/README.md)）。
  - ユーザー名は `logiforge` にする。
- helper は、GitForge の URL にだけ設定する（`credential.<URL>.helper`）。
  - なぜ：ほかの URL に、トークンを渡さないため。
  - URL は、`--url` か、repository の `git_url` から求める。
- URL が https でなければ、`--insecure` なしでは設定しない。
  - なぜ：トークンが平文で流れるため。
- helper の本体は、`logiforge auth git-credential` である。
