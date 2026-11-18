# 複製を書き換えた PR を止める

subtree 方式で、プロジェクトのリポジトリの `logiforge/`（複製）を書き換えた PR を、CI で止めるときに読む。
複製の形と取り込み方は、[subtree 方式の導入](README.md) に書く。

## workflow

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
          # 比べる基点は、base と HEAD の分かれた点にする。checkout の ref を PR の先頭に変えても、base の側の変更を混ぜないため。
          base=$(git merge-base "$BASE" HEAD)
          if git diff --quiet "$base" HEAD -- "$dir"; then
            echo "$dir/ は変わっていない"
            exit 0
          fi
          cur=$(git rev-parse "HEAD:$dir" 2>/dev/null) || cur=
          if [ -n "$cur" ] && git log --format=%T --grep="^git-subtree-dir: $dir\$" "$base..HEAD" | grep -qx "$cur"; then
            echo "$dir/ は、この PR の git subtree の取り込みのとおりである"
          else
            echo "::error::$dir/ が、取り込んだ版と違う。Logiforge の repository で apply request を出し、git subtree pull で取り込む"
            exit 1
          fi
```

## 通す PR と落とす PR

- PR が `logiforge/` を変えていなければ、通す。変えていれば、この PR の中に、`logiforge/` の今の tree と同じ tree の squash のコミットがあるときだけ通す。違えば落とす。
  - 取り込みと更新の PR は、通る。複製の中のファイルを変えた PR、取り込みに書き換えを混ぜた PR、複製を消した PR は、落ちる。
  - なぜ PR の中の squash のコミットとだけ照らすか：取り込みの PR が squash merge されると、squash のコミットは `main` の履歴に残らないから。`main` の履歴と照らす形では、そのあとのすべての PR が落ちる。また、前に取り込んだ古い版に戻す書き換えも、落とせる。
## workflow の書き方

- `fetch-depth: 0` は必須である。なぜ：浅い clone では、base のコミットと squash のコミットが見えないから。
- base の SHA は、`env` で渡す。`run` の中に `${{ }}` で埋め込まない。なぜ：workflow への注入を避けるため。
- 比べる基点は、base の SHA でなく、`git merge-base` で求めた分かれた点にする。なぜ：checkout の既定の ref（`refs/pull/<番号>/merge`）では、どちらでも同じだが、`ref` を PR の先頭に変えると、PR を作ったあとに base に入った変更（ほかの取り込みなど）が、逆向きの差分として混ざり、`logiforge/` に触れない PR まで落ちうるから。
- `paths` で絞らない。なぜ：この job を必須の check にしたとき、`logiforge/` に触れない PR で check が来ないまま待ち続けるから。
- プロジェクトのリポジトリの `main` を守る設定で、この job を必須の check にする。
## 止めないもの

- この CI が止めるのは、誤った書き換えである。squash のコミットを手で作る偽装は止めない。
  - 偽装まで止めるには、`git-subtree-split` の SHA を Logiforge の repository から fetch し、その tree と比べる。そのためには、GitHub Actions に Logiforge の repository を読むトークンを置き、Actions から GitForge に届く必要がある。
