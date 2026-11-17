# main の守り（ディレクトリ方式）

ディレクトリの repository を置く git リポジトリで、`main` をどう守るかを確かめるときに読む。
git リポジトリ全体の repository の守り方は、[main を守る](../adapters/pull-requests.md#main-を守る) に書く。

## 守ること

- プロジェクトの人は、コードの PR を、今までどおり merge する。
- 登録したディレクトリは、apply request の反映でだけ変わる。反映によるものかは、だれが merge したかでなく、中身で決める（[反映かの判定](../backend/verify.md)、[履歴](history.md) の「反映かの判定」）。
  - Logiforge は、GitForge に書くための自分のアカウント（書き込み主体）を持たない。反映は、`approve` をした reviewer のトークンで merge する（[反映の手順](../backend/merge.md)）。
- 守る設定は、git リポジトリ全体の repository と同じく、勧めるが求めない（[main を守る](../adapters/pull-requests.md#main-を守る)）。
  - 守る設定がなくても、ディレクトリを変える反映によらないコミットは、worker が見つけ、その repository を壊れた状態にする（[壊れた repository](../backend/broken.md)）。
  - 守る設定は、壊れる前に止めるためのものである。
- 書き込み主体がないので、「ディレクトリを変える PR を merge できるのは、Logiforge だけ」という守り方はできない。守る設定でできるのは、ディレクトリを変える PR を merge できる人を、Logiforge の reviewer に絞ることまでである。
  - 絞れないときは、守りは「後から検査」（worker が見つけて、壊れた状態にする）だけでよい。

## GitHub の守る設定

使えるときは、プロジェクトのリポジトリに、`main` のブランチのルールセットを作る。使わなければ、後から検査だけで守る。

- Rules
  - Require a pull request before merging
    - Required reviewers：登録したディレクトリ（例：`logiforge/**`）に、Logiforge の承認の team を 1 人の承認で付ける
  - Block force pushes
- Bypass list：Logiforge の reviewer の team（Logiforge で `approve` し、merge する人とエージェント）
- 「Require branches to be up to date before merging」は使わない（[衝突](../backend/merge.md#衝突)）。

Logiforge の承認の team（例：`logiforge-gate`）は、メンバーのいない team にし、リポジトリの Write の権限を与える。

- ディレクトリを変える PR は、その team の承認が要るが、承認できる人がいないので、reviewer の team のほかは、だれも merge できない。
  - reviewer の team の人は、bypass で merge できる。バックエンドは、反映の手順 4 で、`approve` をした reviewer のトークンで merge する。
  - reviewer の team の人が、承認されていない PR を GitHub の画面で merge しても、守る設定では止まらない。そのときは、worker が、反映によらないコミットとして見つける。
- ディレクトリを変えない PR（コードの PR）には、この規則は効かない。
- なぜ必須の check にしないか：PR の側の workflow は、PR が書き換えたり、同じ名前の job を足したりして通せるから。ルールセットは、リポジトリのファイルの外にあるので、PR からは変えられない。
- 使えるのは、organization のリポジトリだけである。public なら Free で、private なら Team 以上で使える。
  - 個人のアカウントのリポジトリには team がなく、Free の private のリポジトリではリポジトリのルールセットが効かない。そのときは、守る設定なしで使う。
  - organization のルールセットと「Require workflows to pass」は、Enterprise だけの機能なので、使わない（GitHub の [About rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets) と [Available rules for rulesets](https://docs.github.com/en/enterprise-cloud@latest/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets)）。
- yamambainc（Free）の public のリポジトリで、次のことを確かめた（bypass を GitHub App にしていたとき）。
  - ディレクトリを変える PR の merge は、リポジトリの管理者でも、`Waiting on required approvals` で拒まれる（405）。
  - ディレクトリを変えない PR は、そのまま merge できる。
  - パスの境目の PR も拒まれる：ディレクトリの中のファイルを外へ rename する PR、ディレクトリを消す PR、外のファイルをディレクトリの中へ移す PR。

## Forgejo の守る設定

Forgejo では、ディレクトリを変える PR の merge を、特定の人に絞る方法が、リポジトリの管理者の `force_merge` しかない。reviewer を管理者にすると、リポジトリの設定まで変えられるので、勧めない。

- そのため、Forgejo では、ディレクトリの守りは、後から検査だけにする。`protected_file_patterns` は使わない。
  - なぜ：`protected_file_patterns` を掛けると、管理者でない reviewer も、ディレクトリを変える apply request を merge できなくなり、反映できないから。
- `main` のブランチの保護は、次のものだけを設定する。

| 項目 | 値 | 理由 |
|---|---|---|
| `enable_push` | `false` | 直接の push を禁じる |
| `enable_merge_whitelist` | プロジェクトの決まりのまま | コードの PR は、人が merge する。reviewer も merge できるようにしておく |
| `block_on_outdated_branch` | `false` | [衝突](../backend/merge.md#衝突) と同じ |

- Forgejo 17.0.1 で、次のことを確かめた（書き込み主体を管理者にしていたとき）。
  - `protected_file_patterns` を掛けると、Write の権限の人の、ディレクトリにファイルを足す PR、ディレクトリを消す PR、外のファイルをディレクトリの中へ移す PR は、`Changed protected files` で拒まれる。
  - ディレクトリの中のファイルを外へ rename する PR は、merge できてしまう（Forgejo は、rename の新しいパスだけを見る）。
  - 管理者は、`force_merge: true` でだけ merge できる。
  - `main` への直接の push は、拒まれる。

## 管理者

- リポジトリと organization の管理者は、信頼の境界の中に入る（[信頼の境界](../adapters/pull-requests.md#信頼の境界)）。どちらの GitForge でも同じである。
  - 管理者は、守る設定を外すか越えて、ディレクトリを変えられる。既存のリポジトリでは、organization の owner がいるので、管理者をなくせない。
  - 管理者がディレクトリを変えたら、worker が見つけ、repository を壊れた状態にする。
- GitHub では、Logiforge の承認の team に、メンバーを足さない。足された人は、ディレクトリを変える PR を承認して、merge できるようになる。team のメンバーを変えられる人（organization の owner と team の maintainer）も、信頼の境界の中に入る。
- GitHub の reviewer の team の人は、bypass で、`main` のコードの PR も、承認なしに merge できる。
  - これを受け入れる。なぜ：コードの PR の承認は、プロジェクトの決まりで、bypass をだれに与えるかもプロジェクトが決めるから。受け入れられなければ、ディレクトリの守りを使わず、後から検査だけにする。

## 反映の順番

- `main` は、コードの merge でも進む。Logiforge は、`main` に入る順番を決めない。
- ディレクトリを変えるのは、反映だけなので、ディレクトリの中の順番は、反映の merge の順である。
- 反映の手順（[反映の手順](../backend/merge.md)）は、次のところだけが変わる。
  - 手順 2 の「worker が追いつくのを待つ」は、待ち始めに読んだ GitForge の `main` の先頭が、`scanned`（worker が読み終えた `main` の先頭。[履歴](history.md)）と同じか、その祖先になったら終わる。
    - なぜ `latest` でないか：コードの merge では、ディレクトリの版は進まないので、`latest` は GitForge の `main` に追いつかないから。
    - なぜ「同じになるまで」でないか：コードの merge が続くリポジトリでは、`main` の先頭がいつまでも先に進み、待ちが終わらないから。
  - 手順 3 の検査に、差分がディレクトリの中だけであること（`outside_directory`）を足す。
- 手順 3 と手順 4 のすき間に、コードの PR が merge されても、ディレクトリの中は変わらない。検査の結果は変わらない。
- 別の apply request とのすき間は、今までどおりふさがない（[検査と merge のすき間](../backend/merge.md#検査と-merge-のすき間)）。

## 確かめること

実装するときに、次のことを確かめる。

- GitHub：ルールセットの bypass に team を入れられるか（Free の organization で）。入れた team の人のトークンで、承認の規則で止まった PR を、API で squash merge できるか
- GitHub：private のリポジトリ（Team）でも、同じルールセットが効くか
