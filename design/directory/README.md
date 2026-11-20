# ディレクトリ方式

既存のプロジェクトのリポジトリの中のディレクトリで、スナップショットツリーを運用するとき（ディレクトリ方式）に、何が変わるかを確かめるために読む。
使い方から見た置き方の選び方は、[既存のプロジェクトへの導入](../../docs/adoption/README.md) に書く。

## 形

- repository（[用語](../../docs/concepts/glossary.md)）は、git リポジトリ全体か、git リポジトリの中の 1 つのディレクトリにあたる。
  - ディレクトリにあたる repository を、ここでは「ディレクトリの repository」と書く。
- 1 つの git リポジトリに、ディレクトリの repository をいくつも置ける（例：`logiforge/cache-strategy/`、`logiforge/index-layout/`）。
- ディレクトリの repository の中のファイルの配置は、ディレクトリを根として、[ファイルの配置](../files/layout.md) と同じである（`<ディレクトリ>/tree/`、`<ディレクトリ>/artifacts/`）。
- ディレクトリの外のファイル（プロジェクトのコード）は、Logiforge は読まない。
- 前提：GitForge の動作は止められない。Logiforge が検査し反映するのは、`main` 向きの apply request だけである（[向き先](../backend/apply-request/target.md#向き先)）。

## 登録

- 利用者が、設定の画面で、repository の名前と「git リポジトリとディレクトリのパス」の組を、設定の DB に登録する（例：名前 `cache-strategy`、`acme/myapp:logiforge/cache-strategy`。[設定の DB に置くもの](../settings/README.md#設定の-db-に置くもの)）。
  - 守る設定を使うときは、repository の用意のあとに、そのディレクトリのパターン（例：`logiforge/cache-strategy/**`）を守る設定に足す（[main の守り](main.md)）。足すまでは、人の PR でディレクトリを変えられる。そのときは、worker が見つけて、壊れた状態にする。
    - なぜ用意のあとか：用意のコミットは、利用者のトークンで `main` に直接置くから（[履歴](history.md) の「用意のコミット」）。
  - gitforge_ref は、`owner/name:パス` である。GitForge の API と git には、`owner/name` の部分を使う。
  - Forgejo では、ディレクトリを置く git リポジトリ（プロジェクトのリポジトリ）を、git リポジトリ全体の repository としては数えない。
  - なぜ Logiforge の側か：プロジェクトのリポジトリに設定のファイルを置くと、コードの PR でディレクトリを足したり外したりでき、apply request を通らずに repository の範囲が変わるから。
- 登録したディレクトリは、ほかの登録したディレクトリの中にも外にも重ならない。
  - なぜ：1 つのファイルが、2 つの repository のものになるのを防ぐため。
- GitHub と Forgejo のどちらの構成でも登録できる。git リポジトリ全体の repository と、同じサーバに並べられる。
- 利用者が repository を作ると、バックエンドは、利用者のトークンで、根のファイルをディレクトリに置く用意のコミットを、`main` に置く（[履歴](history.md) の「用意のコミット」）。
  - 用意のコミットには、署名（`Logiforge-Init-Signature:`）を付ける。worker は、署名で用意のコミットと決める（[repository の用意のコミット](../backend/verify/init.md#repository-の用意のコミット)）。
- API の `Repository` は、ディレクトリのパス（`directory`）を持つ。git リポジトリ全体の repository では空である。
  - CLI は、`check`、ひな形、`ar create` で、このパスを根として扱う（[CLI](../cli/README.md)）。

## apply request とみなす PR

- apply request は、git リポジトリ全体の repository と同じく、署名の合う `apply_request` の記録のコメントがある pull request だけである（[GitForge のポートを pull request と issue で実装する](../adapters/pull-requests.md)）。だれのトークンで作ったかは問わない。
  - 記録のない PR は、ディレクトリを変えていても、apply request ではない。Logiforge は反映しない。人が merge すれば、反映によらないコミットになる（[履歴](history.md) の「反映かの判定」）。
  - コードだけの PR は、Logiforge は読まない。
- 記録のコメントに、trailer `Logiforge-Repository:`（ディレクトリのパス。例：`logiforge/cache-strategy`）を足す。署名に含める。
  - なぜ名前でなくパスか：名前は Logiforge の設定の呼び名で、変えられるから。パスは、git リポジトリの中のディレクトリそのもので、同じ git リポジトリの中では重ならない。
  - なぜ：1 つの git リポジトリに repository が複数あるので、どの repository の apply request かを、書き換えられない形で決めるため。
  - パスが、問い合わせた repository と違う記録は、数えない。git リポジトリ全体の repository は、`Logiforge-Repository:` のない記録だけを数える。
- apply request の差分（base から先頭のコミットまで）は、そのディレクトリの中だけにする。
  - ディレクトリの外も変える差分は、作成と反映の検査で拒む（[差分の検査](../backend/checks.md) の規則 `outside_directory`）。
  - なぜ：Logiforge の反映で、コードを変えないため。コードの変更は、プロジェクトの PR とレビューに任せる。
- `main` 向きでない apply request の扱いは、git リポジトリ全体の repository と同じである。

## 人への依頼

- 人への依頼の issue は、git リポジトリ全体の repository と同じく、署名の合う `human_request` の記録で見分ける。コードの issue は、記録がないので数えない。
- 記録には、`Logiforge-Repository:` を足す（上と同じ）。
- 画面で見分けるために、ラベル `logiforge` と `logiforge/<repository の名前>` を足す。タイトルは `[Logiforge <repository の名前>] <識別子>` にする。
  - ラベルとタイトルは、読まない（[署名の合う記録だけを信じる](../adapters/records.md#署名の合う記録だけを信じる)）。

## 別リポジトリ方式から移すこと

- 別リポジトリ方式の repository を、ディレクトリの repository に移せる。移すのは、`main` の先頭のスナップショットツリーだけで、履歴は移さない（[別リポジトリ方式から移す](migrate.md)）。

## 読む順

1. [main の守り](main.md)：人がコードを merge するリポジトリで、ディレクトリを Logiforge の反映だけで変える仕組み
2. [履歴](history.md)：スナップショットツリーの版の数え方、記録、worker
3. [別リポジトリ方式から移す](migrate.md)：移すもの、条件、手順、元の repository、引用
