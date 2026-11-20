# CLI の認証

CLI がトークンをどう得て、どこに置き、バックエンドの API と git にどう渡すかを確かめるときに読む。
CLI の全体は、[CLI](README.md) に書く。

## トークン

- CLI は、GitForge のトークンを、バックエンドの API と git の両方で使う。

### Forgejo のスコープ

- Forgejo を使う構成では、トークンのスコープを `read:user`、`read:organization`、`write:repository`、`write:issue` にする。
  - `read:user` と `read:organization`：バックエンドが、`GET /user` と `GET /user/teams` で主体を確かめるため（[Forgejo アダプタの認証](../adapters/forgejo.md#認証)）。
  - `write:repository`：git でブランチを push し、バックエンドが pull request を作り、レビューを書き、merge するため。
    - バックエンドは、主体のトークンで GitForge に書く（[GitForge アダプタの共通の書き方](../adapters/pull-requests.md)）。
  - `write:issue`：バックエンドが、記録のコメントと人への依頼を、主体のトークンで書くため。
- このトークンは、利用者が Forgejo で書けるすべての repository に書ける。この範囲の広さは受け入れる。
  - なぜ：Forgejo のスコープは、repository ごとに絞れないから。
  - 範囲を絞りたいときは、Logiforge のためだけの Forgejo のユーザーを作り、そのトークンを使う。

### GitHub のトークン

- GitHub を使う構成では、CLI の主体は `client` になる（[GitHub アダプタの認証](../adapters/github.md#認証)）。
- 人もエージェント（機械用アカウント）も、`gh auth login`（OAuth）で得たトークンを使う（`gh auth token`）。
  - personal access token は使わない。
- スコープは `repo` が要る。
  - なぜ：バックエンドが、主体のトークンで、pull request、記録のコメント、レビューを書き、reviewer のトークンで merge するから（[GitForge アダプタの共通の書き方](../adapters/pull-requests.md)）。
  - git の push も、同じトークンで行う。
- `repo` は、そのアカウントが書けるすべてのリポジトリに及ぶ。
  - 範囲を絞りたいエージェントは、Logiforge のためだけの機械用アカウントにする（[GitHub App とトークン](../adapters/github-app.md#アカウントとトークン)）。

## 置き場所

- トークンの置き場所は、次の順に探す。
  1. 環境変数 `LOGIFORGE_TOKEN`
  2. 設定ファイル（`~/.config/logiforge/config.yaml`）の `token`
  3. `gh auth token --hostname <GitHub のホスト>` の出力。バックエンドが GitHub を使う構成のときだけ（[gh のトークン](#gh-のトークン)）。
- 1 回の実行では、1 度だけ探す。
- どこにもなければ、`logiforge auth login --with-token` か `LOGIFORGE_TOKEN` で渡すように示して、誤りにする。
- `logiforge auth login --with-token` は、トークンを標準入力から読んで、設定ファイルに書く。
  - 設定ファイルは、本人だけが読めるようにする。
  - `--token <トークン>` でも渡せる。ただし、コマンドの履歴に残るので、`--with-token` を勧める。
- バックエンドの URL は、環境変数 `LOGIFORGE_HOST` か、設定ファイルの `host` に置く。

### gh のトークン

- 想定用途：GitHub を使う構成で、`gh auth login` だけで CLI と git の push を使えるようにする。
  - なぜ：トークンを設定ファイルに写すと、gh のトークンが変わるたびに写し直すことになるから。
- CLI は、トークンが環境変数にも設定ファイルにもないときだけ、バックエンドの `GET /v1/oauth/config`（認証を求めない）を呼んで、構成を確かめる（[openapi.yaml](../api/openapi.yaml) の `getOAuthConfig`）。
- 次のすべてを満たすときだけ、`gh auth token --hostname <ホスト>` を呼ぶ。
  - 応答が 200 で、`gitforge` が `github` である。
  - `authorize_url` が https である。
- ホストは、`authorize_url` のホストにする。バックエンドの設定の GitHub の URL（既定は `github.com`）から作られる。
  - なぜ `--hostname` を付けるか：gh がほかのホストにもログインしていても、その GitHub のトークンだけを使うため。
- 次のときは、gh を呼ばない。トークンがないときの誤りにする。
  - Forgejo を使う構成。
  - バックエンドの URL が https でない。構成も確かめない。
    - なぜ：gh のトークンは広いスコープを持つので、平文で送らないため。
  - バックエンドの URL がない、バックエンドにつながらない、応答が 200 でない（セットアップの前、中継を動かしていないなど）、応答が読めない。
  - なぜ：GitHub の構成だと確かめられないときに、gh のトークンをほかに渡さないため。Forgejo の構成の動きを、gh の有無で変えないため。
- gh がないか、そのホストにログインしていなければ、`gh auth login --hostname <ホスト>` か `logiforge auth login --with-token` を示して、誤りにする。
- 構成の確かめは、覚えない。トークンが要る実行のたびに、1 回呼ぶ。
  - なぜ：構成を変えたときに、古い構成で gh のトークンを渡さないため。増えるのは、認証のない要求 1 回だけなので、git の credential helper から呼ぶときも受け入れる。
  - 待つのは 3 秒までにする。つながらなければ、トークンがないときの誤りにする。

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
  - トークンは、[置き場所](#置き場所) と同じ順に探す。そのため、GitHub を使う構成では、`gh auth login` だけで push できる。
  - gh のトークンは、git が求めるホストが、その GitHub のホストと同じときだけ渡す。
