# subtree 方式の導入

Logiforge の repository のスナップショットツリーを、プロジェクトのリポジトリに読むための複製として取り込むときに読む。
先に [別リポジトリ方式の導入](separate-repository.md) を済ませる。選び方は、[既存のプロジェクトへの導入](README.md) に書く。

## 形

- 正本は、Logiforge の repository である。プロジェクトのリポジトリの `logiforge/` に、`git subtree` で複製を置く。
- 複製を直接書き換えない。スナップショットツリーを変えるときは、Logiforge の repository で apply request を出す。
  - なぜ：複製への変更は、apply request を通らない変更になるから（[apply request](../apply-request/README.md)）。
  - 書き換えた PR は、CI で止める（[複製を書き換えた PR を止める](#複製を書き換えた-pr-を止める)）。
- `git subtree push` で、複製から Logiforge の repository に戻すことはしない。同期は、Logiforge の repository から複製への一方向だけである。
  - なぜ：Logiforge の repository の `main` に入るのは、反映だけだから。
- 複製に入るのは、`main` のファイル（[ファイルの配置](../../design/files/layout.md)）だけである。
  - 値（真偽）は入らない。値はファイルに保存せず、Logiforge が計算するから。複製の版の値は、WebUI で `commit` を付けて見る（[別リポジトリ方式の導入](separate-repository.md) の「プロジェクトとつなぐ」）。
  - apply request、レビュー、人への依頼は入らない。Logiforge の repository の GitForge に残る。そのため、検証の途中を見せずに、結論だけを公開のリポジトリに出せる。

## 取り込む

取り込む人は、Logiforge の repository を読める GitForge のユーザーで、CLI を用意しておく（[CLI を用意する](separate-repository.md#cli-を用意する)）。

1. Logiforge の repository の git の URL を確かめる。`logiforge repo view myapp-cache-strategy --json` の `git_url` である。
2. プロジェクトのリポジトリで、ブランチを作って取り込む。

```sh
git switch -c logiforge-import
git subtree add --prefix=logiforge https://forgejo.example.com/logiforge/myapp-cache-strategy.git main --squash
git push -u origin logiforge-import
```

3. プロジェクトのリポジトリで PR を作り、merge のコミットで merge する（GitHub の「Create a merge commit」）。

- 取り込みと更新の PR は、merge のコミットで merge する。squash merge と rebase merge にしない。
  - なぜ：squash のコミット（下の 2 行の入ったもの）が、そのままプロジェクトの履歴に残るから。下の「取り込んでいる版の確かめ方」は、そのコミットを読む。
  - squash merge にすると、そのコミットはなくなり、PR の 1 つのコミットにまとめられる。GitHub の squash の本文を PR のタイトルだけにする設定では、2 行も消え、版を確かめられなくなる。
  - rebase merge にすると、複製の中身だけを持つ squash のコミットが、`main` の上に並べ直され、プロジェクトの木が壊れる。
- `--squash` を必ず付ける。
  - なぜ：Logiforge の `main` のコミットを、プロジェクトの履歴に 1 つずつ入れないため。
  - なぜ：取り込みのたびに、複製の中身と同じ tree の squash のコミットができ、CI がそれと照らせるから。
- squash のコミットのメッセージには、次の 2 行が入る。
  - `git-subtree-dir: logiforge`
  - `git-subtree-split: <Logiforge の main のコミットの SHA>`

## 更新する

結論を取り込む時点は、プロジェクトの側で決める。取り込みたいときに、同じ手順で `pull` する。

```sh
git switch -c logiforge-update
git subtree pull --prefix=logiforge https://forgejo.example.com/logiforge/myapp-cache-strategy.git main --squash -m "Logiforge の結論を取り込む"
git push -u origin logiforge-update
```

- 取り込んでいる版は、次のコマンドで確かめる。

```sh
git log -1 --grep='^git-subtree-dir: logiforge$' --format=%B | sed -n 's/^git-subtree-split: //p'
```

## 複製を書き換えた PR を止める

プロジェクトのリポジトリに、次の workflow を置く（例：`.github/workflows/logiforge-replica.yml`）。

```yaml
name: logiforge-replica
on: pull_request
jobs:
  replica:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: logiforge/ が、取り込んだ版のままか
        env:
          BASE: ${{ github.event.pull_request.base.sha }}
        run: |
          dir=logiforge
          if git diff --quiet "$BASE" HEAD -- "$dir"; then
            echo "$dir/ は変わっていない"
            exit 0
          fi
          cur=$(git rev-parse "HEAD:$dir" 2>/dev/null) || cur=
          if [ -n "$cur" ] && git log --format=%T --grep="^git-subtree-dir: $dir\$" "$BASE..HEAD" | grep -qx "$cur"; then
            echo "$dir/ は、この PR の git subtree の取り込みのとおりである"
          else
            echo "::error::$dir/ が、取り込んだ版と違う。Logiforge の repository で apply request を出し、git subtree pull で取り込む"
            exit 1
          fi
```

- PR が `logiforge/` を変えていなければ、通す。変えていれば、この PR の中に、`logiforge/` の今の tree と同じ tree の squash のコミットがあるときだけ通す。違えば落とす。
  - 取り込みと更新の PR は、通る。複製の中のファイルを変えた PR、取り込みに書き換えを混ぜた PR、複製を消した PR は、落ちる。
  - なぜ PR の中の squash のコミットとだけ照らすか：取り込みの PR が squash merge されると、squash のコミットは `main` の履歴に残らないから。`main` の履歴と照らす形では、そのあとのすべての PR が落ちる。また、前に取り込んだ古い版に戻す書き換えも、落とせる。
- `fetch-depth: 0` は必須である。なぜ：浅い clone では、base のコミットと squash のコミットが見えないから。
- base の SHA は、`env` で渡す。`run` の中に `${{ }}` で埋め込まない。なぜ：workflow への注入を避けるため。
- この例は、GitHub Actions の上で確かめた（2026-11-17。[試験の repository](https://github.com/yamambainc/logiforge-subtree-test-project/pulls?q=is%3Apr)）。取り込み、コードだけ、`git subtree pull` の PR は通り、複製を書き換えた PR、複製を消した PR、取り込みに書き換えを混ぜた PR は落ちる。更新の PR を squash merge したあとのコードだけの PR も通る。
- `paths` で絞らない。なぜ：この job を必須の check にしたとき、`logiforge/` に触れない PR で check が来ないまま待ち続けるから。
- プロジェクトのリポジトリの `main` を守る設定で、この job を必須の check にする。
- この CI が止めるのは、誤った書き換えである。squash のコミットを手で作る偽装は止めない。
  - 偽装まで止めるには、`git-subtree-split` の SHA を Logiforge の repository から fetch し、その tree と比べる。そのためには、GitHub Actions に Logiforge の repository を読むトークンを置き、Actions から GitForge に届く必要がある。

## 別案：submodule

版を SHA で固定することだけが目的なら、submodule も使える。

```sh
git submodule add https://forgejo.example.com/logiforge/myapp-cache-strategy.git logiforge
```

- プロジェクトのリポジトリには、Logiforge の `main` のコミットの SHA だけが入る。中身は入らない。
  - そのため、複製を書き換える PR がそもそも作れない。上の CI は要らない。
- 中身を読む人は、Logiforge の repository を読む権限が要る。clone のときに `--recurse-submodules` も要る。
  - そのため、Logiforge を読めない人に結論を見せる用途（公開のリポジトリなど）には使えない。
