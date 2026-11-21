# 別リポジトリ方式から移す

別リポジトリ方式の repository を、ディレクトリの repository に移すときに、何を移し、元の repository と引用をどう扱うかを確かめるために読む。

## 移すもの

- 移すのは、元の repository の `main` の先頭のコミット（以下、移したコミット）の `tree/` と `artifacts/` だけである。
  - ノードの識別子、検証結果、artifact のファイルは、そのまま移る。値は、移した先で計算し直す（同じファイルなので、同じ値になる）。
- 移さないもの：`main` の履歴、apply request、レビュー、人への依頼、ブランチの履歴。どれも、元の repository に残る。
  - なぜ：移した先は、別の git リポジトリなので、元のコミットと pull request と issue を持ち込めないから。
- 移した先では、移したファイルを、1 つの用意のコミットとして置く（[履歴](history.md) の「用意のコミット」）。そのコミットが、移した先の[版](history.md#版)の 1 番である。
  - 用意のコミットに、trailer `Logiforge-Migrated-From: <元の repository の名前> <移したコミットの SHA>` を書く。
    - なぜ：元の repository のノードの履歴へのリンクに使うため（下の「ノードの履歴」）。
  - trailer は、用意のコミットを置ける人ならだれでも書ける。そのため、worker は、次の 3 つを確かめてから、元の repository に `migrated_to` を書く。
    - 移したコミットが、元の repository の `latest` と同じである（置き場）。
    - 用意のコミットの `tree/` と `artifacts/` が、元の repository のそのスナップショットと同じである（置き場）。
    - 元の git リポジトリが、GitForge で archive されている。worker の読むだけのトークンで読む。
      - なぜ：本物の移行では、移す前に元を archive にするから（下の「移す前の条件」）。archive にはリポジトリの admin の権限が要るので、移した先の Write を持つだけの主体は、これを満たせない。
    - なぜ：確かめないと、移した先の Write を持つだけの主体が、ほかの repository を「移した」と見せ、本物の移行を先回りして止められるから。repository ごとの Write の外に効く。
  - バックエンドは、元の repository の `migrated_to` が移した先のときだけ、移した先に `migrated_from` を返す。

## 移す前の条件

元の repository が、次のすべてを満たすときだけ移す。満たさなければ、`validation_failed` で断り、満たさないものの一覧を返す。

- open の apply request がない。
- open の人への依頼がない。
- 移した先の名前で、ディレクトリが登録されていて、そのディレクトリがまだない（[ディレクトリ方式](README.md) の「登録」）。
- 最新のコミットが valid である（[最新のコミットが valid でないとき](../backend/invalid-main.md)）。

- なぜ valid でないものを断るか：移した先の最初の版が、valid でない木になるから。元を直してから移す。
- なぜ open のものを断るか：open の apply request と人への依頼は、元の repository の pull request と issue で、移した先に持ち込めないから。片付けずに移すと、反映も回答もできないまま残る。

## 手順

利用者が、WebUI の repository の作成で、「元の repository」を選んで作る。API は、`createRepository` に `migrate_from`（元の repository の名前）を付ける（[openapi.yaml](../api/openapi.yaml)）。根の主文は、渡さない。

1. リポジトリの admin が、移す前に、元のリポジトリを GitForge で archive にしておく（読むだけにする）。バックエンドは、利用者のトークンで元のリポジトリを読み、archive になっていなければ断る。
   - なぜ先に archive にするか：移すあいだに、元の repository の `main` が進まないようにするため。archive のリポジトリには、pull request を作れず、merge もできない。
   - なぜリポジトリの admin が行うか：Logiforge は、GitForge に書くための自分のアカウントを持たず（[GitForge のポートを pull request と issue で実装する](../adapters/pull-requests.md)）、archive にはリポジトリの admin の権限が要るから。移す利用者が、その権限を持つとは限らない。
2. 元の repository の `main` の先頭のスナップショットから、`tree/` と `artifacts/` のファイルを読み、利用者のトークンで、ディレクトリの下に置く用意のコミットを置く（[履歴](history.md) の「用意のコミット」）。
3. 送り直しでは、用意のコミットがまだなければ、もう一度置く。

## 元の repository

- 元の repository は、消さない。移したあとは、読むことだけができる。
  - 書く操作は、GitForge の archive で断られる。バックエンドは、`not_open` で返す。
- API の `Repository` は、移した先では `migrated_from`（元の repository の名前と、移したコミット）を持ち、元の repository では `migrated_to`（移した先の名前）を持つ。
  - `migrated_to` は、worker が、移した先の用意のコミット（`Logiforge-Migrated-From:` のあるもの）を取り込んだときに、元の repository の置き場に書く。バックエンドは、それを読む。
  - なぜ worker が書くか：用意のコミットから求めると、元の repository を読むたびに、すべての repository を読むことになるから。

## 引用

- 移したファイルの引用は、書き換えない。
  - なぜ：検証結果のファイルは、加えたあと変えない（[差分の検査](../backend/checks.md)）。書き換えると、レビューした中身と違うものになるから。
  - 前の形で書いた引用の `commit`（元の repository のコミットの SHA）は、ほかの引用と同じく、読むときに無視する（[検証結果のファイル](../files/layout.md)）。
- 移した検証結果の[引用した時点](../files/revisions.md#引用した時点)は、移した先の版の 1 番である。
  - なぜ：移した先では、検証結果のファイルが、用意のコミットで加わるから。
- そのため、引用元の変化（[検証結果と引用](../../docs/tree/verdict.md#引用元の変化)）は、版の 1 番のあとに部分木が変わったかで決まる。
- 元の repository で、引用してから移すまでのあいだに部分木が変わっていても、移した先では、引用元の変化にならない。
  - なぜ受け入れるか：その変化は、移す前に、元の repository の作業の一覧（`citation_changed`）に出ているから。移す前に見直しておく。
  - なぜ元の repository まで辿らないか：引用した時点を、移した先の git の履歴だけで決められるようにするため。

## ノードの履歴

- 移した先のノードの履歴は、版の 1 番から始まる。
- それより前の履歴は、`migrated_from` の repository で読む。WebUI は、ノードの履歴の終わりに、元の repository の、そのノードの履歴へのリンクを示す。
