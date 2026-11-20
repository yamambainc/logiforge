# repository の用意のコミット

worker が、`main` のコミットを repository の用意のコミットと決める条件を確かめるときに読む。
判定の全体は [反映かの判定](README.md) に書く。

## repository の用意のコミット

`main` のコミット C（1 つ前の `main` のコミットを P とする）は、次のどれかなら、用意のコミットである。

- C が根のコミットで、trailer `Logiforge-Init:` がある。
- C が根の次のコミットで、trailer `Logiforge-Init:` があり、P が用意のコミットである。
  - なぜ：GitHub の空のリポジトリには、ファイルを 1 つずつしか置けないので、背景情報を 2 つ目のコミットで置くから（[GitHub アダプタ](../../adapters/github.md)）。
- ディレクトリの repository では、そのディレクトリの[版](../../directory/history.md#版)の 1 番で、trailer `Logiforge-Init:` があるもの（[ディレクトリ方式の履歴](../../directory/history.md) の「用意のコミット」）。

- どれでも、C の差分が、ファイルを加えることだけであることも確かめる。
- 根のコミットが用意のコミットでなければ、worker は、その repository を取り込まない。
  - なぜ：最後に正しかった時点がないから。
  - GitForge で、repository を作り直す。
- 用意のコミットは、位置と trailer と中身で決める。署名は付けない。
  - そのため、GitHub で空のリポジトリが作られてから、利用者が用意するまでのあいだに、Write を持つ主体が、`Logiforge-Init:` を付けた好きな中身の根を push できる。
  - これは受け入れる危険である。なぜ：Write を持つ主体を信じるから（[信頼の境界](../../adapters/pull-requests.md#信頼の境界)）。用意の前のリポジトリに push する主体は、その Write をすでに持っている。
