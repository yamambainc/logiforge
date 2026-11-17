# 用語

仕様を読むときに、言葉の正本のページを引くために使う。
正本のページがある語は、リンクだけを示す。

## 木

- repository：1 つの問い（根の仮説）とその木を収める単位。[GitForge](#この用語集で定義する語) の 1 つの git リポジトリか、git リポジトリの中の 1 つのディレクトリに対応する。→ [仮説とストーリーの木](../tree/README.md)
- 仮説 → [仮説とストーリーの木](../tree/README.md)
- ストーリー → [仮説とストーリーの木](../tree/README.md)
- 親・子・孫 → [仮説とストーリーの木](../tree/README.md)
- 葉、未決の葉 → [仮説とストーリーの木](../tree/README.md)
- 要再検討 → [真偽値の伝播](../tree/propagation.md)
- 根拠部分木 → [真偽値の伝播](../tree/propagation.md)
- inactive → [inactive 化](../tree/inactive.md)

## 根拠

- 検証結果、レポート、artifact、引用、流用、引用元の変化 → [検証結果と引用](../tree/verdict.md)

## 変更とやりとり

- apply request → [apply request](../apply-request/README.md)
- 論理レビュー、反映の条件、レビューの材料 → [論理レビュー](../apply-request/review.md)
- 人への依頼（human request）→ [人への依頼](../human-requests/README.md)
- PVR（物理検証リクエスト。種類が `pvr` の人への依頼）→ [人への依頼](../human-requests/README.md)
- 着手できる作業 → [着手できる作業](../work/README.md)

## 主体

- opener、reviewer、利用者 → [役割](../roles/README.md)

## この用語集で定義する語

- **Logiforge**：この仕様が定めるシステム全体。
  - 位置づけは、[目的と範囲](README.md) に書く。
- **GitForge**：git のホスティング、pull request、レビュー、issue、主体とトークンを持つ、外のサービス。Logiforge は、木の git リポジトリとやりとりの正本を GitForge に置く。どの GitForge を使うかは、アダプタで差し替える。
- **コミット**：木の git リポジトリのコミット。SHA で指す。利用者に見せる番号は付けない。どのコミットかは、場所で言う。
  - `main` のコミット：木のある時点。apply request が 1 件反映されるたびに、1 つ増える。
  - apply request の先頭のコミット（ブランチの先頭のコミット）：opener が push したブランチの先頭。push するたびに、新しい先頭のコミットになる。レビューと反映は、この SHA を指す。
