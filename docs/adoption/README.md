# 既存のプロジェクトへの導入

GitHub にある既存のプロジェクトで Logiforge を使い始めるときに、自分の使い方に合う置き方を知り、導入するために読む。
使い方（ユースケース）から引く。置き方は、使い方の結果として決まる。

## ML の実験のリポジトリで、問いを立てながらコードを書く

- 研究者とエージェントが、実験のリポジトリで、問い（仮説）とコードを一緒に育てる。
- apply request は、実験のリポジトリで出す。コードの PR と並ぶ。
- 実験のコードと結果は、同じリポジトリのコミットとして、検証結果から引用する。
- 問いに答えることが、実験を進めることになる。スナップショットツリーは、いつも最新のまま読む。
- 1 つのリポジトリに、問いを複数置く（`logiforge/<問い>/`）。

置き方：ディレクトリ方式

- ディレクトリ方式の決まりは、[ディレクトリ方式](../../design/directory/README.md) に書く。
- ディレクトリ方式は、GitHub と Forgejo のどちらの構成でも使える。`main` を守る設定は勧めるが、なくても使える。守る設定がなくても、Logiforge は `main` のコミットをそのまま受け入れる（[main への直接の変更](../apply-request/README.md#main-への直接の変更)、[main の守り](../../design/directory/main.md)）。
- 別リポジトリ方式の repository は、あとでディレクトリ方式に移せる（[別リポジトリ方式から移す](../../design/directory/migrate.md)）。
  - 移すのは、移す時点のスナップショットツリーだけである。履歴、apply request、人への依頼は、元の repository に残り、元の repository は読むだけになる。
  - 移す前に、open の apply request と人への依頼を片付けておく。

## 設計判断を残す（ADR の代わり）

- 開発者が、設計の問いと、その答えの根拠を、コードのリポジトリに残す。
- apply request は、コードのリポジトリで出す。判断の議論は、その PR で行う。
- 判断のもとになったコードや測定は、同じリポジトリのコミットとして引用する。
- 判断ごとに問いを分け、1 つのリポジトリに並べる。

置き方：ディレクトリ方式

- 決まりは、[ML の実験のリポジトリ](#ml-の実験のリポジトリで問いを立てながらコードを書く) と同じである。

## 1 つのプロジェクトのための調査を、コードと分けて進める

- 調査する人（プロジェクトの人とは限らない）が、Logiforge の repository で apply request を出す。
- プロジェクトのコードや実験の結果は、GitHub の URL に SHA を付けて、検証結果から引用する。
- プロジェクトの PR や issue からは、結論を WebUI の URL（SHA 付き）で指す。
- プロジェクトのリポジトリには、何も足さない。

置き方：別リポジトリ方式（[導入](separate-repository.md)）

## 複数のリポジトリにまたがる調査

- 調査は、1 つの Logiforge の repository で行う。apply request と議論は、そこに集まる。
- 結論を使う各リポジトリは、それぞれの都合のよい時点で、結論を取り込む。
- 各リポジトリの中で、取り込んだ結論のファイルを、コードと並べて読める。

置き方：subtree 方式（[導入](subtree/README.md)）。リポジトリの中で読む必要がなければ、別リポジトリ方式でよい。

## 非公開で検証し、結論だけを公開する

- OSS などで、検証の途中（apply request、レビュー、人への依頼）は、非公開の Logiforge で行う。
- 公開のリポジトリには、反映された結論のファイルだけを取り込む。
- 公開のリポジトリを読む人は、Logiforge を読む権限が要らない。

置き方：subtree 方式（[導入](subtree/README.md)）

- Logiforge の repository を GitHub に置くときも、private のリポジトリにできる。GitHub Free の organization でもよい（[Logiforge の repository を GitHub に置く](github/README.md)）。

## 結論を、ライブラリのように版を固定して使う

- プロジェクトは、どの時点の結論を前提にしたかを、コミットで固定する。
- 結論が新しくなっても、プロジェクトの側が取り込むまでは変わらない。
- 取り込みは、プロジェクトの PR で行う。ほかの依存の更新と同じく、差分を見て merge する。

置き方：subtree 方式（[導入](subtree/README.md)）。中身をリポジトリに入れず、SHA だけを固定するなら、submodule（[subtree 方式の導入](subtree/README.md#別案submodule)）。

## プロジェクトとは関係なく、問いだけを扱う

- 問いに取り組む人が、Logiforge の repository で apply request を出す。
- コードのリポジトリとはつながない。

置き方：別リポジトリ方式（[導入](separate-repository.md)）。「プロジェクトとつなぐ」の手順は要らない。

## 置き方の比較

| 置き方 | スナップショットツリーの正本 | プロジェクトのリポジトリに置くもの |
|---|---|---|
| 別リポジトリ方式 | Logiforge の repository | なし |
| subtree 方式 | Logiforge の repository | 読むための複製（`logiforge/`） |
| ディレクトリ方式 | プロジェクトのリポジトリの中のディレクトリ | スナップショットツリーそのもの |

- どの置き方でも、apply request を通らない変更は、スナップショットツリーに入れない（[apply request](../apply-request/README.md)）。
- 導入の手順：
  1. [別リポジトリ方式の導入](separate-repository.md)
     - Logiforge の repository を GitHub に置くときは、[Logiforge の repository を GitHub に置く](github/README.md) も読む。
  2. [subtree 方式の導入](subtree/README.md)（別リポジトリ方式の導入を済ませてから）
