# 別リポジトリ方式から移す

別リポジトリ方式の repository を、ディレクトリの repository に移すときに、何を移し、元の repository と引用をどう扱うかを確かめるために読む。

## 移すもの

- 移すのは、元の repository の `main` の先頭のコミット（以下、移したコミット）の `tree/` と `artifacts/` だけである。
  - ノードの識別子、検証結果、artifact のファイルは、そのまま移る。値は、移した先で計算し直す（同じファイルなので、同じ値になる）。
- 移さないもの：`main` の履歴、apply request、レビュー、人への依頼、ブランチの履歴。どれも、元の repository に残る。
  - なぜ：移した先は、別の git リポジトリなので、元のコミットと pull request と issue を持ち込めないから。
- 移した先では、移したファイルを、1 つの用意のコミットとして置く（[履歴](history.md) の「用意のコミット」）。そのコミットが、移した先の版の 1 番である。
  - 用意のコミットに、trailer `Logiforge-Migrated-From: <元の repository の名前> <移したコミットの SHA>` を書く。この値も、用意のコミットの署名に含める。
    - なぜ：引用を元の repository の版で探すのに使うので、書き換えられないようにするため（下の「引用」）。

## 移す前の条件

元の repository が、次のすべてを満たすときだけ移す。満たさなければ、`validation_failed` で断り、満たさないものの一覧を返す。

- open の apply request がない。
- open の人への依頼がない。
- 壊れていない（[壊れた repository](../backend/broken.md)）。
- 移した先の名前で、ディレクトリが登録されていて、そのディレクトリがまだない（[ディレクトリ方式](README.md) の「登録」）。

- なぜ open のものを断るか：open の apply request と人への依頼は、元の repository の pull request と issue で、移した先に持ち込めないから。片付けずに移すと、反映も回答もできないまま残る。

## 手順

利用者が、WebUI の repository の作成で、「元の repository」を選んで作る。API は、`createRepository` に `migrate_from`（元の repository の名前）を付ける（[openapi.yaml](../api/openapi.yaml)）。根の主文は、渡さない。

1. 管理者が、移す前に、元のリポジトリを GitForge で archive にしておく（読むだけにする）。バックエンドは、利用者のトークンで元のリポジトリを読み、archive になっていなければ断る。
   - なぜ先に archive にするか：移すあいだに、元の repository の `main` が進まないようにするため。archive のリポジトリには、pull request を作れず、merge もできない。
   - なぜ管理者が行うか：Logiforge は、GitForge に書くための自分のアカウントを持たず（[GitForge アダプタの共通の書き方](../adapters/pull-requests.md)）、archive にはリポジトリの管理者の権限が要るから。移す利用者が、その権限を持つとは限らない。
2. 元の repository の `main` の先頭のスナップショットから、`tree/` と `artifacts/` のファイルを読み、利用者のトークンで、ディレクトリの下に置く用意のコミットを置く（[履歴](history.md) の「用意のコミット」）。
3. 送り直しでは、用意のコミットがまだなければ、もう一度置く。

## 元の repository

- 元の repository は、消さない。移したあとは、読むことだけができる。
  - 書く操作は、GitForge の archive で断られる。バックエンドは、`not_open` で返す。
- API の `Repository` は、移した先では `migrated_from`（元の repository の名前と、移したコミット）を持ち、元の repository では `migrated_to`（移した先の名前）を持つ。
  - `migrated_to` は、worker が、移した先の用意のコミット（`Logiforge-Migrated-From:` のあるもの）を取り込んだときに、元の repository の置き場に書く。バックエンドは、それを読む。
  - なぜ worker が書くか：用意のコミットから求めると、元の repository を読むたびに、すべての repository を読むことになるから。

## 引用

- 移したファイルの引用は、書き換えない。元の repository のコミットの SHA を、そのまま持つ。
  - なぜ書き換えないか：検証結果のファイルは、加えたあと変えない（[差分の検査](../backend/checks.md)）。書き換えると、レビューした中身と違うものになるから。
- 移した先の repository で、引用の `commit` は、次の順に探す。
  1. 移した先の版
  2. `migrated_from` の repository の版
- 新しく書く引用は、移した先の版だけを指せる。元の repository の版を指す新しい引用は、差分の検査で拒む。
- 引用元の変化（[検証結果と引用](../../docs/tree/verdict.md)）は、元の repository の版を指す引用では、次のどちらかで変わったとみなす。
  - 元の repository で、引用したコミットから移したコミットまでのあいだに、部分木が変わった（元の repository の、移したコミットのスナップショットツリーの `last_changed` で求める）。
  - 移した先で、版の 1 番のあとに、部分木が変わった。

## ノードの履歴

- 移した先のノードの履歴は、版の 1 番から始まる。
- それより前の履歴は、`migrated_from` の repository で読む。WebUI は、ノードの履歴の終わりに、元の repository の、そのノードの履歴へのリンクを示す。
