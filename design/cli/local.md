# ひな形と check

CLI が、手元の clone の中で、ファイルのひな形を作り、検査するコマンドを確かめるときに読む。
ファイルの形は、[木のファイル配置](../files/README.md) が正本である。

## ひな形

| コマンド | 作るもの | 対応する操作 |
|---|---|---|
| `story new --parent <仮説> --title --summary --hypothesis <主文>...` | ストーリーと子仮説のディレクトリ、`story.md`、`hypothesis.md` | `story.create` |
| `story inactivate <ストーリー> --reason` | `inactivated.md` | `story.inactivate` |
| `verdict new --hypothesis <仮説> --value true\|false --report <ファイル> [--artifact <ファイル>]... [--link <名前>=<URL>]... [--cite subtree:<ノード>@<SHA>\|artifact:<sha256>\|answer:<人への依頼>]... [--supersedes <検証結果>]...` | `verdicts/v-*.md`、artifact のファイル | `verdict.submit` |
| `node edit <仮説> --append-background <ファイル>` | `background.md` の末尾への追加 | `node.edit` |

- 新しいノードの識別子は、CLI が乱数で決める（[識別子](../files/layout.md)）。
  - ほかのノードとぶつかったら、`check` とバックエンドの検査で拒まれる。作り直せばよい。
- `--parent` には、まだ push していない、同じブランチで作った仮説も指定できる。
- artifact のファイルは、中身の sha256 を求め、`artifacts/sha256/` の正しい名前に写す。
- `--cite answer:<人への依頼>` は、回答と添付を、API の URL の `link` の artifact として書く（[人への依頼の回答](../files/artifacts.md)）。
- ひな形を作ったあとに、ファイルを手で編集してよい。

## check

`logiforge check` は、手元の作業ツリーを、`origin/main` と比べて検査する。

- 木の形が正しいか（交互に並ぶこと、深さ、文字数、front matter）
- `origin/main` との差分が、4 種類の操作として読めるか
- 新しいノードと検証結果の識別子が、`origin/main` の木とぶつかっていないか
- 普通のファイル（100644）だけか
- ブランチの先頭のコミットに、セッションの trailer があるか
  - なければ警告し、付け方を示す（`git commit --amend --trailer "Logiforge-Session-Id: <ID>"`）。
- artifact の大きさが上限を超えていないか、ファイル名が中身のハッシュと合うか

- `check` は、バックエンドの [差分の検査](../backend/checks.md) と同じコードを使う。ただし、引用したリビジョンの確認など、手元でできないものは省く。
  - なぜ：push の前に、ほとんどの誤りを手元で見つけられるようにするため。最後に確かめるのは、バックエンドである。
- `check` は、git を読むだけで、API を呼ばない。
- `check` が見るファイルは、git が追跡しているものと、追跡していないが無視もしていないものである。消したファイルは、消えたものとして扱う。
- `--json` では、`ok`、`base`（比べた `origin/main` の SHA）、`rejected`（誤り）、`warnings`、`operations`（復元した操作）を書く。誤りがあれば、終了コードは 1 である。
