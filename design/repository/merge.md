# 反映と git のマージ

git のマージと、要件の「対象ノードが変わっていたら反映しない」の関係を確かめるときに読む。
要件は、[反映](../../docs/apply-request/README.md) が正本である。

## git の衝突だけでは足りない

git が衝突を出すのは、同じファイルの同じ箇所を変えたときだけである。
要件の「対象ノードが変わった」は、それより広い。

- 衝突しないが、要件では「変わった」にあたる例
  - 同じ仮説の下に、2 つの apply request が別々のストーリーを加えた。
  - 対象の仮説の祖先のストーリーが、別の apply request で inactive になった。
- 衝突するが、要件でも「変わった」にあたる例
  - 2 つの apply request が、同じ `background.md` の末尾に文章を加えた。

そのため、反映の判定は git の衝突に頼らず、core が行う。

## core の判定

- 各操作の対象ノードについて、「変わった」とみなすパスを決める。
  - 仮説：自分の `hypothesis.md`、`background.md`、`verdicts/`、子ストーリーの `story.md` と `inactivated.md`、祖先のストーリーの `inactivated.md`
  - ストーリー：自分の `story.md` と `inactivated.md`、祖先のストーリーの `inactivated.md`
- apply request の base（ブランチを切った `main` のコミット）から最新の `main` までに、そのパスを変えたコミットがあれば、反映しない。
  - 調べ方：`git log <base>..main -- <パス>`
- 伝播で値だけが変わることは、ファイルに現れないので、「変わった」に含まれない。
  - なぜ：値は保存しないから（[保存しないもの](layout.md)）。

## 反映できなかったとき

- apply request は、open のまま残る。
- opener は、ブランチを最新の `main` に合わせて差分を作り直す。
  - 作り直した差分は、新しい版である。
  - それまでの approve は、新しい版には効かない（[反映の条件](../../docs/apply-request/review.md)）。

## 反映の順番

- 反映は、1 件ずつ順に行う。
- 反映の直前に、最新の `main` に対して [core の検査](checks.md) と上の判定を行う。
  - なぜ：検査と反映の間に、別の反映が割り込まないようにするため。
