# main の守り（ディレクトリ方式）

ディレクトリの repository を置く git リポジトリで、`main` をどう守るかを確かめるときに読む。
git リポジトリ全体の repository の守り方は、[main を守る](../adapters/pull-requests.md#main-を守る) に書く。

## 守ること

- プロジェクトの人は、コードの PR を、プロジェクトの決まりで merge する。
- 登録したディレクトリは、apply request の反映で変える。
- Logiforge は、ディレクトリを変えた `main` のコミットを、どう入ったかによらず受け入れる（[main はいつも正しい](../backend/invalid-main.md#main-はいつも正しい)、[履歴](history.md) の「valid」）。
  - Logiforge は、GitForge に書くための自分のアカウント（書き込み主体）を持たない。Logiforge からの merge は、merge の操作を呼んだ主体のトークンで行う（[merge](../backend/merge.md#merge)）。
- 守る設定は、git リポジトリ全体の repository と同じく、勧めるが求めない（[main を守る](../adapters/pull-requests.md#main-を守る)）。
  - 守る設定がなければ、apply request を通らずにディレクトリを変えられる。そのコミットも受け入れる。木が不変条件を破れば、valid でないコミットとして示す（[最新のコミットが valid でないとき](../backend/invalid-main.md)）。
  - 守る設定は、論理レビューを通らない変更を、`main` に入る前に止めるためのものである。
- 書き込み主体がないので、「ディレクトリを変える PR を merge できるのは、Logiforge だけ」という守り方はできない。守る設定でできるのは、ディレクトリを変える PR を merge できる人を、Logiforge の reviewer に絞ることまでである。
  - 絞れないときは、守る設定なしで使う。

## GitHub の守る設定

使えるときは、プロジェクトのリポジトリに、`main` のブランチのルールセットを 2 つ作る。使わなければ、守る設定なしで使う。

| ルールセット | Rules | Bypass list |
|---|---|---|
| (A) main の基本 | git リポジトリ全体の repository と同じ（[main への書き込みを守る](../adapters/github-main.md#main-への書き込みを守る)） | だれも入れない |
| (B) ディレクトリ | Require a pull request before merging の Required reviewers：登録したディレクトリ（例：`logiforge/**`）に、Logiforge の承認の team を 1 人の承認で付ける | Logiforge の reviewer の team（Logiforge で `approve` し、merge する人とエージェント） |

- なぜ 2 つに分けるか：bypass は、そのルールセットのすべての規則に効くから。1 つにまとめると、reviewer の team が、`main` への直接の push と force push もできてしまう。
- (B) の bypass は、(A) には効かない。reviewer の team の人も、`main` への直接の push と force push はできない。
- 「Require branches to be up to date before merging」は使わない（[衝突](../backend/merge.md#衝突)）。

Logiforge の承認の team（例：`logiforge-gate`）は、メンバーのいない team にし、リポジトリの Write の権限を与える。

- ディレクトリを変える PR は、その team の承認が要るが、承認できる人がいないので、reviewer の team のほかは、だれも merge できない。
  - reviewer の team の人は、bypass で merge できる。バックエンドは、merge の操作を呼んだ主体のトークンで merge する（[merge](../backend/merge.md#merge)）。
  - reviewer の team の人が、承認されていない PR を GitHub の画面で merge しても、守る設定では止まらない。そのコミットも、Logiforge は受け入れる。
- ディレクトリを変えない PR（コードの PR）には、この規則は効かない。
- なぜ必須の check にしないか：PR の側の workflow は、PR が書き換えたり、同じ名前の job を足したりして通せるから。ルールセットは、リポジトリのファイルの外にあるので、PR からは変えられない。
- 使えるのは、organization のリポジトリだけである。public なら Free で、private なら Team 以上で使える。
  - 個人のアカウントのリポジトリには team がなく、Free の private のリポジトリではリポジトリのルールセットが効かない。そのときは、守る設定なしで使う。
  - organization のルールセットと「Require workflows to pass」は、Enterprise だけの機能なので、使わない（GitHub の [About rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets) と [Available rules for rulesets](https://docs.github.com/en/enterprise-cloud@latest/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets)）。
- (B) の bypass には、team を `actor_type: Team`、`bypass_mode: always` で入れる。
- (B) の Required reviewers は、パスの境目の PR にも効く。
  - ディレクトリの中のファイルを外へ rename する PR
  - ディレクトリを消す PR
  - 外のファイルをディレクトリの中へ移す PR
- (B) の bypass にいない人が、ディレクトリを変える PR を merge すると、GitHub は断る。アダプタは、これを承認が足りない `not_approved` にする。承認の team の人に approve してもらうと merge できる（[GitHub アダプタ](../adapters/github.md#merge-を断られたとき) の「merge を断られたとき」）。

## Forgejo の守る設定

Forgejo では、ディレクトリを変える PR の merge を、特定の人に絞る方法が、リポジトリの admin の `force_merge` しかない。reviewer をリポジトリの admin にすると、リポジトリの設定まで変えられるので、勧めない。

- そのため、Forgejo では、ディレクトリの守る設定は使わない。`protected_file_patterns` は使わない。
  - なぜ：`protected_file_patterns` を掛けると、リポジトリの admin でない reviewer も、ディレクトリを変える apply request を merge できなくなり、反映できないから。
  - Forgejo は、`protected_file_patterns` に掛かる PR を、リポジトリの admin のほかは `Changed protected files` で断る。リポジトリの admin は、`force_merge: true` でだけ merge できる。
  - Forgejo は、rename の新しいパスだけを見る。そのため、ディレクトリの中のファイルを外へ rename する PR は、`protected_file_patterns` でも止まらない。
- `main` のブランチの保護は、次のものだけを設定する。

| 項目 | 値 | 理由 |
|---|---|---|
| `enable_push` | `false` | 直接の push を禁じる |
| `enable_merge_whitelist` | プロジェクトの決まりのまま | コードの PR は、人が merge する。reviewer も merge できるようにしておく |
| `block_on_outdated_branch` | `false` | [衝突](../backend/merge.md#衝突) と同じ |

## リポジトリの admin と organization の owner

- リポジトリの admin と organization の owner は、信頼の境界の中に入る（[信頼の境界](../adapters/pull-requests.md#信頼の境界)）。どちらの GitForge でも同じである。
  - この人たちは、守る設定を外すか越えて、ディレクトリを変えられる。既存のリポジトリでは、organization の owner がいるので、この人たちをなくせない。
  - この人たちがディレクトリを変えても、Logiforge は受け入れる。木が不変条件を破れば、valid でないコミットとして示す。
- GitHub では、Logiforge の承認の team に、メンバーを足さない。足された人は、ディレクトリを変える PR を承認して、merge できるようになる。team のメンバーを変えられる人（organization の owner と team の maintainer）も、信頼の境界の中に入る。
- GitHub の reviewer の team の人は、bypass で、`main` のコードの PR も、承認なしに merge できる。
  - これを受け入れる。なぜ：コードの PR の承認は、プロジェクトの決まりで、bypass をだれに与えるかもプロジェクトが決めるから。受け入れられなければ、ディレクトリの守る設定を使わない。

## 反映の順番

- `main` は、コードの merge でも進む。Logiforge は、`main` に入る順番を決めない。
- ディレクトリを変えるのは、反映だけなので、ディレクトリの中の順番は、反映の merge の順である。
- 反映の手順（[反映の手順](../backend/merge.md)）は、次のところだけが変わる。
  - 手順 2 の「worker が追いつくのを待つ」は、待ち始めに読んだ GitForge の `main` の先頭が、`scanned`（worker が読み終えた `main` の先頭。[履歴](history.md)）と同じか、その祖先になったら終わる。
    - なぜ `latest` でないか：コードの merge では、ディレクトリの[版](history.md#版)は進まないので、`latest` は GitForge の `main` に追いつかないから。
    - なぜ「同じになるまで」でないか：コードの merge が続くリポジトリでは、`main` の先頭がいつまでも先に進み、待ちが終わらないから。
  - 手順 3 の検査に、差分がディレクトリの中だけであること（`outside_directory`）を足す。
- 手順 3 と手順 4 のすき間に、コードの PR が merge されても、ディレクトリの中は変わらない。検査の結果は変わらない。
- 別の apply request とのすき間は、git リポジトリ全体の repository と同じく、ふさがない（[検査と merge のすき間](../backend/merge.md#検査と-merge-のすき間)）。
